"""e4_ctrlbreak: can a supervisor that shares claude's console stop it gracefully with CTRL_BREAK
and relaunch it cleanly in the same console?

    python -I spikes/phase0/experiments/e4_ctrlbreak.py <empty run_root>

The harness opens ONE ConPTY running e4_ctrlbreak_sup.py (the mini supervisor). The supervisor
spawns the real claude.exe (CREATE_NEW_PROCESS_GROUP, inherited console, no std redirection,
isolated env from cc.Run.env) and stops it on request with GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid).

Launch chain (one console, one session id S, profile A):
  L0  claude --session-id S             S1  one echo turn, CTRL_BREAK while idle
  L1  claude -r S "after break"         S4  relaunch after the idle break -> reply
                                        S2  slow 20 s stream, CTRL_BREAK ~3 s into it
  L2  claude -r S "after midturn break" S4b relaunch after mid-turn break -> reply (+ history it sent)
                                        S3  "\\x03" typed into the PTY, then a second "\\x03"
      gap                               S3g "\\x03" and text typed while only the supervisor runs
  L3  claude -r S "after ctrl-c exit"   S4c relaunch after a Ctrl+C exit
                                        S6  account hits "limit" -> StopFailure hook -> CTRL_BREAK
  L4  claude -r S "after limit break"   S4d relaunch after the StopFailure break; S1c idle break
  L5  claude -r S                       S7b CTRL_BREAK 1.5 s after spawn (during startup)
  L6  claude -r S                       S7a CTRL_BREAK 0.3 s after spawn
  L7  claude -r S "after early breaks"  S4f relaunch; S5 hard kill (TerminateProcess) baseline
  L8  claude -r S "after hard kill"     S4e relaunch after the hard kill (no restore by supervisor)
                                        S1b CTRL_BREAK while idle with a 1.5 s SessionEnd hook
  L9  claude -r S "persistence check"   S8  is the fullscreen fallback still on? then idle break
The supervisor clears an inherited "ignore Ctrl+C" flag first (a real launcher starts from a
terminal where Ctrl+C is live), so S3g exercises its Ctrl+C/Ctrl+Break handler.
Exec hooks (SessionStart/SessionEnd/Stop/StopFailure) are injected with --settings.
Prints one JSON report; evidence stays in <run_root>/logs.
"""
from __future__ import annotations

import ctypes
import json
import os
import re
import sys
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, read_jsonl, summarize_rows, transcripts, wait_until  # noqa: E402
from conpty import PseudoConsole, strip_ansi  # noqa: E402
from mockapi import Handler, MockServer, read_log, write_scenario  # noqa: E402

SUP = os.path.join(HERE, "e4_ctrlbreak_sup.py")
HOOK = os.path.join(HERE, "e4_ctrlbreak_hook.py")
KEY = "sk-mock-A"
READY = r"auto mode on|for shortcuts"

EXIT_SEQS = {
    "alt_screen_off ?1049l": b"\x1b[?1049l",
    "cursor_show ?25h": b"\x1b[?25h",
    "bracketed_paste_off ?2004l": b"\x1b[?2004l",
    "focus_events_off ?1004l": b"\x1b[?1004l",
    "win32_input_off ?9001l": b"\x1b[?9001l",
    "kitty_kbd_pop <u": b"\x1b[<u",
    "modifyOtherKeys_reset >4m": b"\x1b[>4m",
    "mouse_off ?1000l": b"\x1b[?1000l",
    "mouse_off ?1002l": b"\x1b[?1002l",
    "mouse_off ?1003l": b"\x1b[?1003l",
    "mouse_sgr_off ?1006l": b"\x1b[?1006l",
    "mouse_pixel_off ?1016l": b"\x1b[?1016l",
    "theme_notify_off ?2031l": b"\x1b[?2031l",
    "title_reset OSC0": b"\x1b]0;\x07",
    "resume_hint text": b"Resume this session with",
}
ENTER_SEQS = {
    "alt_screen_on ?1049h": b"\x1b[?1049h",
    "bracketed_paste_on ?2004h": b"\x1b[?2004h",
    "focus_events_on ?1004h": b"\x1b[?1004h",
    "win32_input_on ?9001h": b"\x1b[?9001h",
    "kitty_kbd_push >1u": b"\x1b[>1u",
    "modifyOtherKeys_on >4;2m": b"\x1b[>4;2m",
    "modifyOtherKeys_on >4;1m": b"\x1b[>4;1m",
    "cursor_hide ?25l": b"\x1b[?25l",
    "mouse_on ?1000h": b"\x1b[?1000h",
    "mouse_on ?1002h": b"\x1b[?1002h",
    "mouse_on ?1003h": b"\x1b[?1003h",
    "mouse_sgr_on ?1006h": b"\x1b[?1006h",
    "mouse_pixel_on ?1016h": b"\x1b[?1016h",
    "theme_notify_on ?2031h": b"\x1b[?2031h",
}


def fs(p: str) -> str:
    return p.replace("\\", "/")


class E4Handler(Handler):
    """Mock handler that also logs a compact view of the history each request carries."""

    def _messages(self, account, body):
        hist = []
        for m in body.get("messages") or []:
            c = m.get("content")
            blocks = []
            if isinstance(c, str):
                blocks.append("str:" + " ".join(c.split())[:120])
            else:
                for b in c or []:
                    if not isinstance(b, dict):
                        continue
                    t = b.get("type")
                    if t == "text":
                        tx = b.get("text") or ""
                        blocks.append("text:<system-reminder>" if tx.lstrip().startswith("<system-reminder>")
                                      else "text:" + " ".join(tx.split())[:160])
                    else:
                        blocks.append(t)
            hist.append({"role": m.get("role"), "blocks": blocks})
        self.state.log({"ts": time.time(), "e4_history": hist, "n_tools": len(body.get("tools") or [])})
        super()._messages(account, body)


def scan(raw: bytes, table: dict) -> dict:
    return {name: raw.count(seq) for name, seq in table.items() if raw.count(seq)}


def kill_pid(pid: int, exe: str, spawned_at: float | None) -> bool:
    """Terminate a claude.exe this run started. The pid alone is not enough: if our child
    already exited, the pid may now belong to another test's or the user's own claude.exe,
    so require the same image and a creation time matching our spawn."""
    import ctypes.wintypes as wt
    if spawned_at is None:
        return False
    k = ctypes.WinDLL("kernel32", use_last_error=True)
    k.OpenProcess.restype = ctypes.c_void_p
    h = k.OpenProcess(0x0001 | 0x1000, False, pid)
    if not h:
        return False
    try:
        buf = ctypes.create_unicode_buffer(1024)
        n = wt.DWORD(1024)
        k.QueryFullProcessImageNameW(ctypes.c_void_p(h), 0, buf, ctypes.byref(n))
        ft = (wt.FILETIME * 4)()
        k.GetProcessTimes(ctypes.c_void_p(h), ctypes.byref(ft[0]), ctypes.byref(ft[1]),
                          ctypes.byref(ft[2]), ctypes.byref(ft[3]))
        created = ((ft[0].dwHighDateTime << 32) | ft[0].dwLowDateTime) / 1e7 - 11644473600
        if os.path.normcase(buf.value) != os.path.normcase(exe) or not (spawned_at - 3 <= created <= spawned_at + 0.5):
            return False
        return bool(k.TerminateProcess(ctypes.c_void_p(h), 1))
    finally:
        k.CloseHandle(ctypes.c_void_p(h))


def row_view(r: dict) -> dict:
    v = summarize_rows([r])[0]
    for k in ("timestamp", "uuid", "parentUuid"):
        if r.get(k):
            v[k] = r[k][:23] if k == "timestamp" else r[k][:8]
    return v


class E4:
    def __init__(self, root: str):
        self.run = Run(root)
        self.logs = self.run.logs
        self.scen = os.path.join(self.run.root, "scenario.json")
        write_scenario(self.scen, {KEY: {"mode": "echo"}})
        self.mock_log = os.path.join(self.logs, "mock.jsonl")
        self.srv = MockServer(self.mock_log, self.scen)
        self.srv.httpd.RequestHandlerClass = type("E4Bound", (E4Handler,), {"state": self.srv.state})
        self.srv.start()
        self.prof = self.run.profile("A", KEY, self.srv.base_url, trust=[self.run.proj])
        self.ctl = os.path.join(self.run.root, "ctl")
        os.makedirs(self.ctl, exist_ok=True)
        self.results = os.path.join(self.logs, "sup_results.json")
        self.hooks_out = os.path.join(self.logs, "hooks.jsonl")
        self.hook_settings = self._hook_settings("hook-settings.json", None)
        self.hook_settings_slow = self._hook_settings("hook-settings-slow-sessionend.json", 1.5)
        self.sid = str(uuid.uuid4())
        self.report: dict = {"claude": self.run.claude, "session_id": self.sid, "run_root": self.run.root,
                             "steps": [], "scenarios": {}}
        self.pc: PseudoConsole | None = None
        self.go_raw: dict[int, int] = {}
        self.cur = 0
        self.t_start = time.time()

    def _hook_settings(self, name: str, session_end_sleep: float | None) -> str:
        base = f'"{fs(sys.executable)}" -I "{fs(HOOK)}" "{fs(self.hooks_out)}"'
        hooks = {}
        for ev in ("SessionStart", "SessionEnd", "Stop", "StopFailure"):
            cmd = f"{base} {ev}" + (f" {session_end_sleep}" if ev == "SessionEnd" and session_end_sleep else "")
            hooks[ev] = [{"hooks": [{"type": "command", "command": cmd, "timeout": 20}]}]
        path = os.path.join(self.run.root, name)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"hooks": hooks}, f, indent=1)
        return path

    # ---- plumbing ----------------------------------------------------------
    def step(self, msg: str, **kw) -> None:
        self.report["steps"].append({"t": round(time.time() - self.t_start, 2), "msg": msg, **kw})
        if self.pc:
            self.pc.save_logs()  # keep PTY evidence even if the run is cut short
        if time.time() - self.t_start > 420:
            raise TimeoutError("e4 time budget exceeded")

    def ctl_write(self, name: str, obj: dict) -> None:
        tmp = os.path.join(self.ctl, name + ".tmp")
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(obj, f)
        os.replace(tmp, os.path.join(self.ctl, name))

    def sup_state(self) -> dict:
        for _ in range(5):
            try:
                with open(self.results, encoding="utf-8") as f:
                    return json.load(f)
            except (OSError, ValueError):
                time.sleep(0.05)
        return {}

    def ctrl_events(self) -> list:
        return self.sup_state().get("ctrl_events") or []

    def launch_rec(self, i: int) -> dict:
        ls = self.sup_state().get("launches", [])
        return ls[i] if i < len(ls) else {}

    def wait_exit_rec(self, i: int, timeout: float) -> dict:
        return wait_until(lambda: (lambda r: r if "pending_input_after" in r else None)(self.launch_rec(i)),
                          timeout, 0.2) or self.launch_rec(i)

    def transcript_path(self) -> str | None:
        for p in transcripts(self.prof.dir):
            if os.path.basename(p) == f"{self.sid}.jsonl":
                return p
        return None

    def rows(self) -> list[dict]:
        p = self.transcript_path()
        return read_jsonl(p) if p else []

    def mock_entries(self) -> list[dict]:
        return read_log(self.mock_log)

    def hook_rows(self) -> list[dict]:
        return read_jsonl(self.hooks_out)

    def hooks(self, t_from: float, t_to: float | None = None, t0: float | None = None,
              t_exit: float | None = None) -> list[dict]:
        out = []
        for h in self.hook_rows():
            ts = h.get("ts", 0)
            if ts >= t_from and (t_to is None or ts <= t_to):
                s = h.get("stdin") or {}
                v = {"event": h.get("event"), "reason": s.get("reason"), "source": s.get("source"),
                     "session_id": s.get("session_id")}
                if t0:
                    v["dt_from_break"] = round(ts - t0, 3)
                if t_exit:
                    v["dt_from_claude_exit"] = round(ts - t_exit, 3)
                out.append({k: x for k, x in v.items() if x is not None})
        return out

    def sessions_dir(self) -> list[str]:
        d = os.path.join(self.prof.dir, "sessions")
        return sorted(os.listdir(d)) if os.path.isdir(d) else []

    def go(self, i: int, args: list[str], settings: str | None = None, **kw) -> int:
        m = self.pc.mark()
        self.go_raw[i] = len(self.pc.raw)
        self.cur = i
        dbg = os.path.join(self.logs, f"claude-debug-L{i}.txt")
        self.ctl_write(f"go_{i}", {"args": ["--settings", settings or self.hook_settings, "--debug-file", dbg,
                                            *args], **kw})
        self.step(f"go_{i}", args=args)
        return m

    def window(self, label: str, raw_from: int, marker: bytes) -> dict:
        """Raw PTY bytes from the trigger up to the supervisor's exit marker."""
        raw = self.pc.raw
        end = raw.find(marker, raw_from)
        win = raw[raw_from: end if end >= 0 else len(raw)]
        with open(os.path.join(self.logs, f"window_{label}.txt"), "w", encoding="utf-8") as f:
            f.write(repr(win))
        return {"bytes": len(win), "marker_found": end >= 0, "exit_seqs": scan(win, EXIT_SEQS),
                "enter_seqs": scan(win, ENTER_SEQS), "tail_bytes": repr(win[-260:]),
                "text": strip_ansi(win.decode("utf-8", "replace"), " ")[-400:]}

    def startup_seqs(self, i: int, raw_to: int) -> dict:
        win = self.pc.raw[self.go_raw.get(i, 0): raw_to]
        return {"enter_seqs": scan(win, ENTER_SEQS), "exit_seqs": scan(win, EXIT_SEQS)}

    def summarize_launch(self, rec: dict) -> dict:
        keys = ("i", "pid", "exit_reason", "exit_code", "exit_code_hex", "secs_break_to_exit", "killed",
                "still_alive_after_s", "gce_ok", "gce_err", "session_file_exists_at_trigger",
                "session_file_exists_after", "session_file_exists_after_1s", "children_at_trigger",
                "children_alive_after", "pending_input_before_spawn", "pending_input_after",
                "modes_before_spawn", "modes_running", "modes_after", "modes_diff_vs_baseline",
                "modes_diff_running_vs_after", "sup_ctrl_events_during_launch", "inject", "flushed_input",
                "restore", "modes_after_restore")
        out = {k: rec.get(k) for k in keys if k in rec}
        for k in ("modes_before_spawn", "modes_running", "modes_after", "modes_after_restore"):
            if isinstance(out.get(k), dict):
                out[k] = {kk: out[k].get(kk) for kk in ("in", "in_flags", "out", "cp_in", "cp_out", "cursor_visible")}
        out["args"] = [a for a in rec.get("args", []) if not a.endswith((".json", ".txt"))]
        sc = rec.get("session_file_content")
        if isinstance(sc, dict):
            out["session_file_content_keys"] = sorted(sc)
            out["session_file_sessionId"] = sc.get("sessionId")
        return out

    # ---- scenario pieces ---------------------------------------------------
    def fs_debug(self, i: int) -> list[str]:
        """claude --debug-file lines about the fullscreen boot canary / fallback for launch i."""
        out = []
        try:
            with open(os.path.join(self.logs, f"claude-debug-L{i}.txt"), encoding="utf-8", errors="replace") as f:
                for line in f:
                    if "fullscreen" in line.lower():
                        out.append(line.strip()[25:200])
        except OSError:
            pass
        return out

    def strikes(self):
        return self.prof.read_claude_json().get("fullscreenBootStrikes")

    def wscreen(self, pattern: str, since: int, timeout: float = 60) -> bool:
        """Wait for a screen pattern; give up early once the current claude has exited."""
        rx = re.compile(pattern, re.I)
        deadline = time.time() + timeout
        gone_at = None
        while time.time() < deadline:
            t = self.pc.text[since:]
            if rx.search(strip_ansi(t)) or rx.search(strip_ansi(t, " ")):
                return True
            if gone_at is None and "exit_reason" in self.launch_rec(self.cur):
                gone_at = time.time()
            if gone_at and time.time() - gone_at > 1.5:
                return False
            time.sleep(0.2)
        return False

    def wait_reply(self, pattern: str, since: int, timeout: float = 60) -> bool:
        return self.wscreen(pattern, since, timeout)

    def relaunch(self, i: int, prompt: str, pattern: str, settings: str | None = None, **kw) -> dict:
        sess_before = self.sessions_dir()
        m = self.go(i, ["-r", self.sid, prompt], settings=settings, **kw)
        self.last_go_mark = m
        t_go = time.time()
        ready = self.wscreen(READY, m, 60)
        t_ready = time.time()
        got = self.wait_reply(pattern, m, 60)
        t_rep = time.time()
        time.sleep(1.0)
        rec = self.launch_rec(i)
        hist = [r for r in self.mock_entries() if r.get("ts", 0) >= t_go and "e4_history" in r and r.get("n_tools")]
        res = {"tui_ready": bool(ready), "secs_go_to_ready": round(t_ready - t_go, 1),
               "reply_seen": got, "secs_go_to_reply": round(t_rep - t_go, 1), "pid": rec.get("pid"),
               "pending_input_before_spawn": rec.get("pending_input_before_spawn"),
               "modes_before_spawn": {k: (rec.get("modes_before_spawn") or {}).get(k)
                                      for k in ("in", "out", "cp_in", "cp_out", "cursor_visible")},
               "sessions_dir_before": sess_before, "sessions_dir_after_ready": self.sessions_dir(),
               "main_loop_requests": [{k: r.get(k) for k in ("n", "status", "n_messages", "user_text")}
                                      for r in self.mock_entries() if r.get("ts", 0) >= t_go and r.get("main_loop")],
               "history_tail_sent": hist[0]["e4_history"][-4:] if hist else None,
               "hooks": self.hooks(t_go),
               "startup_seqs_so_far": self.startup_seqs(i, len(self.pc.raw)),
               "fullscreen_debug": self.fs_debug(i), "strikes_now": self.strikes(),
               "screen_after_go": self.pc.plain(m)[-500:]}
        self.step(f"L{i} relaunch", ready=bool(ready), reply=got)
        return res

    def break_and_collect(self, i: int, label: str, method: str = "ctrl_break", **kw) -> dict:
        rows_before = self.rows()
        raw_off = len(self.pc.raw)
        t_trig = time.time()
        self.ctl_write(f"break_{i}", {"method": method, **kw})
        self.step(f"break_{i}", method=method)
        rec = self.wait_exit_rec(i, 30)
        time.sleep(2.0)  # let a slow SessionEnd hook finish writing
        rows_after = self.rows()
        res = {"launch": self.summarize_launch(rec),
               "startup": self.startup_seqs(i, raw_off),
               "window": self.window(label, raw_off, f"E4SUP-EXIT-{i}".encode()),
               "transcript_rows_before": len(rows_before), "transcript_rows_after": len(rows_after),
               "rows_added_by_shutdown": [row_view(r) for r in rows_after[len(rows_before):]][:30],
               "hooks": self.hooks(t_trig - 0.2, None, rec.get("t0"), rec.get("exited_at")),
               "sessions_dir_after": self.sessions_dir(),
               "fullscreen_debug": self.fs_debug(i), "strikes_after": self.strikes(),
               "sup_alive": self.pc.is_alive()}
        return res

    def early_break(self, i: int, after_s: float, label: str) -> dict:
        """Launch `claude -r S` (no prompt) and let the supervisor CTRL_BREAK it after_s after spawn."""
        sess_before = self.sessions_dir()
        self.go(i, ["-r", self.sid], break_after_s=after_s)
        t_go = time.time()
        rec = self.wait_exit_rec(i, 30)
        time.sleep(1.0)
        res = {"launch": self.summarize_launch(rec),
               "secs_spawn_to_break": rec.get("secs_spawn_to_break"),
               "window_whole_launch": self.window(label, self.go_raw[i], f"E4SUP-EXIT-{i}".encode()),
               "hooks": self.hooks(t_go, None, rec.get("t0"), rec.get("exited_at")),
               "sessions_dir_before": sess_before, "sessions_dir_after": self.sessions_dir(),
               "fullscreen_debug": self.fs_debug(i), "strikes_after": self.strikes(),
               "sup_alive": self.pc.is_alive()}
        self.step(f"L{i} early break", after_s=after_s, code=rec.get("exit_code"))
        return res

    # ---- main --------------------------------------------------------------
    def main(self) -> dict:
        self.t_start = time.time()
        run = self.run
        cfg = {"claude": run.claude, "env": run.env(self.prof), "cwd": run.proj, "ctl": self.ctl,
               "results": self.results, "events": os.path.join(self.logs, "sup_events.jsonl"),
               "profile_dir": self.prof.dir}
        cfg_path = os.path.join(run.root, "sup_cfg.json")
        with open(cfg_path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, indent=1)
        self.pc = PseudoConsole([sys.executable, "-I", SUP, cfg_path], cwd=run.proj, env=run.env(self.prof),
                                cols=120, rows=40, log_dir=self.logs, name="sup")
        self.pc.start()
        try:
            self._chain()
        except Exception as ex:  # noqa: BLE001 - keep the evidence gathered so far
            import traceback
            self.report["harness_error"] = traceback.format_exc()
            self.report["steps"].append({"t": round(time.time() - self.t_start, 2), "msg": "harness_error",
                                         "err": repr(ex)})
        finally:
            self.ctl_write("quit", {})
            time.sleep(1.0)
            st = self.sup_state()
            self.report["sup_alive_at_end"] = self.pc.is_alive()
            self.report["sup_exit_code"] = self.pc.wait_exit(5)
            for rec in st.get("launches", []):
                pid = rec.get("pid")
                if pid and rec.get("exit_code") is None:
                    self.report.setdefault("cleanup_killed", []).append(
                        {"pid": pid, "ok": kill_pid(pid, self.run.claude, rec.get("spawned_at"))})
            self.pc.close()
            self.srv.stop()
            st = self.sup_state()
            self.report["sup"] = {k: st.get(k) for k in ("sup_pid", "handler_installed", "std_handles",
                                                         "con_handles_ok", "modes_baseline", "ctrl_events",
                                                         "errors")}
        self.report["mock_requests"] = [
            {k: r.get(k) for k in ("n", "status", "mode", "main_loop", "n_messages", "user_text", "event")
             if r.get(k) is not None}
            for r in self.mock_entries() if "e4_history" not in r]
        rows = self.rows()
        self.report["transcript"] = {"path": self.transcript_path(), "n_rows": len(rows),
                                     "session_ids": sorted({r.get("sessionId") for r in rows if r.get("sessionId")}),
                                     "conversation": [row_view(r) for r in rows
                                                      if r.get("type") in ("user", "assistant", "system")],
                                     "all_transcripts": [os.path.relpath(p, self.prof.dir)
                                                         for p in transcripts(self.prof.dir)]}
        self.report["hooks_all"] = self.hooks(0)
        self.report["sessions_dir_at_end"] = self.sessions_dir()
        return self.report

    def _chain(self) -> None:
        pc, S = self.pc, self.report["scenarios"]
        ok = pc.wait_for(r"E4SUP-READY", 20)
        self.step("sup_ready", ok=bool(ok))
        st = self.sup_state()
        self.report["sup_pid_at_start"] = st.get("sup_pid")
        self.report["sup_ctrl_c_flag"] = {k: st.get(k) for k in ("ctrl_c_ignored_inherited", "reenable_ctrl_c_ok",
                                                                  "ctrl_c_ignored_after_reenable", "handler_installed")}
        if not ok:
            self.report["fatal"] = "supervisor did not start"
            return

        # ---- L0 / S1: idle break --------------------------------------------------------
        m = self.go(0, ["--session-id", self.sid])
        ready = self.wscreen(READY, m, 60)
        self.step("L0 ready", ok=bool(ready))
        time.sleep(1.5)
        m = pc.mark()
        pc.submit("first turn idle test")
        got = self.wait_reply(r"MOCK-REPLY\[A\]\s*#\d+:\s*first\s*turn", m)
        self.step("L0 reply", ok=got)
        time.sleep(2.0)
        s1 = self.break_and_collect(0, "S1_idle")
        s1["completed_turn_in_transcript"] = any(
            r.get("type") == "assistant" and "MOCK-REPLY[A]" in json.dumps(r.get("message", {}))
            and "first turn idle test" in json.dumps(r.get("message", {})) for r in self.rows())
        S["S1_idle_break"] = s1

        # ---- L1 / S4 relaunch; S2 mid-turn break ----------------------------------------
        S["S4_relaunch_after_idle_break"] = self.relaunch(1, "after break", r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*break")
        time.sleep(1.0)
        words = " ".join(f"PARTIAL-{k:02d}\n" for k in range(1, 41))  # newline per word so the TUI shows it
        write_scenario(self.scen, {KEY: {"mode": "slow", "seconds": 20, "text": words}})
        m = pc.mark()
        rows_pre = len(self.rows())
        t_sub = time.time()
        pc.submit("slow turn please")
        req = wait_until(lambda: next((r for r in self.mock_entries() if r.get("ts", 0) >= t_sub
                                       and r.get("main_loop") and "slow turn" in (r.get("user_text") or "")),
                                      None), 30, 0.1)
        if req:
            time.sleep(max(0.0, req["ts"] + 3.0 - time.time()))
        seen_before = sorted(set(re.findall(r"PARTIAL-\d\d", pc.plain(m))))
        s2 = self.break_and_collect(1, "S2_midturn")
        write_scenario(self.scen, {KEY: {"mode": "echo"}})
        tail = self.rows()[rows_pre:]
        t0 = self.launch_rec(1).get("t0")
        s2.update({
            "secs_stream_start_to_break": round(t0 - req["ts"], 2) if (req and t0) else None,
            "partial_words_on_screen_before_break": seen_before,
            "rows_since_prompt": [row_view(r) for r in tail][:30],
            "partial_words_in_transcript": sorted(set(re.findall(r"PARTIAL-\d\d", json.dumps(tail)))),
            "rows_with_interruptedByShutdown": [row_view(r) for r in tail if "interruptedByShutdown" in r],
            "rows_mentioning_interrupt": [row_view(r) for r in tail
                                          if "interrupt" in json.dumps(r.get("message", {})).lower()],
            "mock_disconnect_events": [r for r in self.mock_entries()
                                       if r.get("event") == "client_disconnected_mid_stream"]})
        S["S2_midturn_break"] = s2

        # ---- L2 / S4b relaunch after mid-turn break; S3 Ctrl+C keystrokes ---------------
        rows_pre = len(self.rows())
        s4b = self.relaunch(2, "after midturn break", r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*midturn")
        s4b["rows_written_by_resume"] = [row_view(r) for r in self.rows()[rows_pre:]
                                         if r.get("type") in ("user", "assistant", "system")]
        S["S4b_relaunch_after_midturn_break"] = s4b
        time.sleep(1.5)
        ev0 = len(self.ctrl_events())
        m = pc.mark()
        raw_off = len(pc.raw)
        pc.send("\x03")
        t_c1 = time.time()
        hint = self.wscreen(r"Press Ctrl-C again", m, 3)
        t_hint = time.time()
        rec_mid = self.launch_rec(2)
        s3 = {"hint_after_first_ctrl_c": bool(hint), "secs_to_hint": round(t_hint - t_c1, 2),
              "claude_alive_after_first": "exit_reason" not in rec_mid,
              "sup_alive_after_first": pc.is_alive()}
        time.sleep(0.15)
        pc.send("\x03")
        t_c2 = time.time()
        rec = self.wait_exit_rec(2, 20)
        time.sleep(1.0)
        s3.update({"secs_between_ctrl_c": round(t_c2 - t_c1, 2), "launch": self.summarize_launch(rec),
                   "secs_second_ctrl_c_to_exit": round(rec["exited_at"] - t_c2, 3) if rec.get("exited_at") else None,
                   "sup_alive_after_second": pc.is_alive(),
                   "sup_ctrl_events_during_S3": self.ctrl_events()[ev0:],
                   "window": self.window("S3_ctrlc", raw_off, b"E4SUP-EXIT-2"),
                   "hooks": self.hooks(t_c1 - 0.2, None, None, rec.get("exited_at")),
                   "sessions_dir_after": self.sessions_dir()})
        S["S3_ctrl_c_keystrokes"] = s3
        if rec.get("exit_code") is None:  # claude did not exit: stop it so the chain can continue
            self.ctl_write("break_2", {"method": "ctrl_break"})
            self.wait_exit_rec(2, 20)

        # ---- gap: only the supervisor is attached to the console ------------------------
        ev0 = len(self.ctrl_events())
        pc.send("\x03")
        time.sleep(1.2)
        st = self.sup_state()
        S["S3g_ctrl_c_in_gap"] = {"sup_ctrl_events": (st.get("ctrl_events") or [])[ev0:], "sup_alive": pc.is_alive(),
                                  "sup_pid_unchanged": st.get("sup_pid") == self.report.get("sup_pid_at_start")}
        pc.send("GAPTEXT")
        time.sleep(0.8)

        # ---- L3 / S4c relaunch after Ctrl+C exit; S6 limit -> StopFailure -> break ------
        s4c = self.relaunch(3, "after ctrl-c exit", r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*ctrl")
        s4c["gap_text_on_screen"] = "GAPTEXT" in pc.plain(self.last_go_mark)
        s4c["gap_text_sent_to_api"] = [r.get("user_text") for r in self.mock_entries()
                                       if "GAPTEXT" in (r.get("user_text") or "")]
        S["S4c_relaunch_after_ctrl_c_exit"] = s4c
        time.sleep(1.0)
        write_scenario(self.scen, {KEY: {"mode": "limit"}})
        m = pc.mark()
        rows_pre = len(self.rows())
        t_sub = time.time()
        pc.submit("limit turn")
        sf = wait_until(lambda: next((h for h in self.hook_rows() if h.get("ts", 0) >= t_sub
                                      and h.get("event") == "StopFailure"), None), 60, 0.1)
        t_sf = time.time()
        time.sleep(0.5)
        screen = pc.plain(m)
        s6 = self.break_and_collect(3, "S6_after_stopfailure")
        write_scenario(self.scen, {KEY: {"mode": "echo"}})
        s6.update({"stopfailure_seen": bool(sf), "secs_submit_to_stopfailure": round(t_sf - t_sub, 2) if sf else None,
                   "stopfailure_payload_keys": sorted((sf or {}).get("stdin") or {}),
                   "stopfailure_error": ((sf or {}).get("stdin") or {}).get("error"),
                   "screen_before_break": screen[-500:],
                   "limit_requests": [{k: r.get(k) for k in ("n", "status", "main_loop", "user_text")}
                                      for r in self.mock_entries() if r.get("ts", 0) >= t_sub and r.get("status") == 429],
                   "rows_since_prompt": [row_view(r) for r in self.rows()[rows_pre:]][:30]})
        S["S6_break_after_stopfailure"] = s6

        # ---- L4 / S4d relaunch after StopFailure break; S1c plain idle break -------------
        rows_pre = len(self.rows())
        s4d = self.relaunch(4, "after limit break", r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*limit")
        s4d["rows_written_by_resume"] = [row_view(r) for r in self.rows()[rows_pre:]
                                         if r.get("type") in ("user", "assistant", "system")]
        S["S4d_relaunch_after_stopfailure_break"] = s4d
        time.sleep(1.5)
        S["S1c_idle_break"] = self.break_and_collect(4, "S1c_idle")

        # ---- L5/L6 / S7: CTRL_BREAK during startup (1.5 s, then 0.3 s after spawn) -------
        S["S7b_break_1_5s_after_spawn"] = self.early_break(5, 1.5, "S7b_early")
        S["S7a_break_0_3s_after_spawn"] = self.early_break(6, 0.3, "S7a_early")

        # ---- L7 / S4f relaunch after early breaks; S5 hard kill baseline ----------------
        S["S4f_relaunch_after_early_breaks"] = self.relaunch(7, "after early breaks",
                                                             r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*early")
        time.sleep(1.5)
        S["S5_hard_kill_baseline"] = self.break_and_collect(7, "S5_kill", method="kill")

        # ---- L8 / S4e relaunch after hard kill (no restore); S1b idle break, slow hook --
        S["S4e_relaunch_after_hard_kill"] = self.relaunch(8, "after hard kill",
                                                          r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*hard",
                                                          settings=self.hook_settings_slow)
        time.sleep(1.5)
        S["S1b_idle_break_slow_sessionend_hook"] = self.break_and_collect(8, "S1b_idle")

        # ---- L9 / S8: does the fullscreen fallback persist after a clean exit? ---------
        S["S8_relaunch_persistence_check"] = self.relaunch(9, "persistence check",
                                                           r"MOCK-REPLY\[A\]\s*#\d+:\s*persistence")
        time.sleep(1.5)
        S["S8_idle_break"] = self.break_and_collect(9, "S8_idle")


def main(root: str) -> int:
    e = E4(root)
    rep = e.main()
    out = json.dumps(rep, ensure_ascii=False, indent=1, default=str)
    with open(os.path.join(e.logs, "report.json"), "w", encoding="utf-8") as f:
        f.write(out)
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
