"""e7_switchloop: end-to-end account switch on a usage limit, with no user input.

    python -I e7_switchloop.py <empty run_root> [--resume-arg path|id] [--junction] [--type-during-switch]

Starts e7_switchloop_sup.py (prototype of baton's switch loop) inside a ConPTY. The supervisor runs
the real interactive claude.exe on profile A (mock key sk-mock-A) with hooks injected via
--settings. The harness types "task one", flips A to "limit", types "task two", and then types
nothing until profile B's reply is on screen; then it types "task three" into B's TUI. Then the
same again one level deeper (B was itself resumed by path): flip B to "limit", "task four",
no typing until C replies, "task five" into C's TUI. Prints one JSON report.

--resume-arg id: pass the session id to -r instead of the absolute transcript path.
--junction: profiles B/C projects dirs are junctions to profile A's projects dir (fallback design).
--type-during-switch: as soon as the switch notice is on screen, type text (no Enter) to see
  where keystrokes typed during the switch window end up.
--untrusted-B: profile B is NOT seeded with folder trust for the project (dialog check); after the
  switch the harness waits 25 s, records the screen, presses Enter once, then exits.
"""
from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid
from http.server import ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, junction, read_jsonl, session_ids, summarize_rows  # noqa: E402
from conpty import PseudoConsole, strip_ansi  # noqa: E402
from mockapi import Handler, MockServer, MockState, read_log, write_scenario  # noqa: E402

SUP = os.path.join(HERE, "e7_switchloop_sup.py")
HOOK = os.path.join(HERE, "e7_switchloop_hook.py")
PY = sys.executable
CONTINUE = ("The previous session stopped at a usage limit. Continue the task you were working on; "
            "do not repeat work that is already complete.")


# ---- mock that also records request bodies (harness mock only logs snippets) ----------------
class BodyHandler(Handler):
    def _messages(self, account, body):
        msgs = []
        for m in body.get("messages") or []:
            c = m.get("content")
            blocks = []
            if isinstance(c, str):
                blocks.append({"type": "text", "text": c[:600]})
            else:
                for b in c or []:
                    if not isinstance(b, dict):
                        continue
                    if b.get("type") == "text":
                        blocks.append({"type": "text", "text": (b.get("text") or "")[:600]})
                    else:
                        blocks.append({"type": b.get("type")})
            msgs.append({"role": m.get("role"), "blocks": blocks})
        rec = {"ts": time.time(), "account": account[-1:], "main_loop": bool(body.get("tools")),
               "n_messages": len(body.get("messages") or []), "messages": msgs,
               "session_header": self.headers.get("x-claude-code-session-id")}
        line = json.dumps(rec, ensure_ascii=False) + "\n"
        with self.state.lock:
            with open(self.state.bodies_path, "a", encoding="utf-8") as f:
                f.write(line)
        super()._messages(account, body)


class BodyMockServer(MockServer):
    def __init__(self, log_path, scenario_path, bodies_path):  # no super(): own handler class
        self.state = MockState(log_path, scenario_path)
        self.state.bodies_path = bodies_path
        handler = type("BoundBodyHandler", (BodyHandler,), {"state": self.state})
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.httpd.daemon_threads = True
        self.httpd.handle_error = lambda request, client_address: None  # mute client-reset tracebacks
        self.thread = None


def wait_text(pc: PseudoConsole, pattern: str, timeout: float, since: int = 0, interval: float = 0.05):
    """Like pc.wait_for but polls faster, returns (match, ts_when_seen)."""
    rx = re.compile(pattern, re.I)
    deadline = time.time() + timeout
    while time.time() < deadline:
        t = pc.text[since:]
        for cand in (strip_ansi(t), strip_ansi(t, " ")):
            m = rx.search(cand)
            if m:
                return m, time.time()
        if not pc.is_alive():
            break
        time.sleep(interval)
    return None, None


def kill_leftovers(sup_log: str, exe: str, not_before: float) -> list:
    """Kill claude children the supervisor started (pids from its log) that are still alive.
    Only pids whose image is our claude.exe and that were created after this run started."""
    import ctypes
    import ctypes.wintypes as wt
    k32 = ctypes.WinDLL("kernel32", use_last_error=True)
    k32.OpenProcess.restype = wt.HANDLE
    k32.OpenProcess.argtypes = [wt.DWORD, wt.BOOL, wt.DWORD]
    k32.WaitForSingleObject.argtypes = [wt.HANDLE, wt.DWORD]
    k32.QueryFullProcessImageNameW.argtypes = [wt.HANDLE, wt.DWORD, wt.LPWSTR, ctypes.POINTER(wt.DWORD)]
    k32.GetProcessTimes.argtypes = [wt.HANDLE] + [ctypes.POINTER(wt.FILETIME)] * 4
    k32.TerminateProcess.argtypes = [wt.HANDLE, wt.UINT]
    k32.CloseHandle.argtypes = [wt.HANDLE]
    out = []
    try:
        steps = json.load(open(sup_log, encoding="utf-8")).get("steps", [])
    except (OSError, ValueError):
        return out
    for s in steps:
        if s.get("step") != "spawned":
            continue
        pid = s["pid"]
        h = k32.OpenProcess(0x1000 | 0x0001 | 0x00100000, False, pid)  # QUERY_LIMITED|TERMINATE|SYNCHRONIZE
        if not h:
            continue
        try:
            if k32.WaitForSingleObject(h, 0) == 0:
                continue  # already exited
            buf = ctypes.create_unicode_buffer(1024)
            n = wt.DWORD(1024)
            k32.QueryFullProcessImageNameW(h, 0, buf, ctypes.byref(n))
            ft = (wt.FILETIME * 4)()
            k32.GetProcessTimes(h, ctypes.byref(ft[0]), ctypes.byref(ft[1]), ctypes.byref(ft[2]), ctypes.byref(ft[3]))
            created = ((ft[0].dwHighDateTime << 32) | ft[0].dwLowDateTime) / 1e7 - 11644473600
            # Exact creation-time match only: a reused pid (another test's or the user's own
            # claude.exe) has the same image and may also be newer than this run.
            recorded = s.get("created")
            if (os.path.normcase(buf.value) == os.path.normcase(exe) and recorded is not None
                    and abs(created - recorded) < 0.01):
                k32.TerminateProcess(h, 1)
                out.append(pid)
        finally:
            k32.CloseHandle(h)
    return out


def text_blocks(msg: dict) -> str:
    return " | ".join(b.get("text", "") for b in msg.get("blocks", []) if b.get("type") == "text")


def match_idx(flat: list, s: str) -> list:
    return [i for i, (_, t) in enumerate(flat) if s in t]


def main(argv: list[str]) -> int:
    root = argv[0]
    resume_arg = argv[argv.index("--resume-arg") + 1] if "--resume-arg" in argv else "path"
    use_junction = "--junction" in argv
    type_during = "--type-during-switch" in argv
    untrusted_b = "--untrusted-B" in argv
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    modes = {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}, "sk-mock-C": {"mode": "echo"}}
    write_scenario(scen, modes)
    mock_log = os.path.join(run.logs, "mock.jsonl")
    bodies_log = os.path.join(run.logs, "mock-bodies.jsonl")
    srv = BodyMockServer(mock_log, scen, bodies_log)
    srv.start()
    rep: dict = {"claude": run.claude, "resume_arg": resume_arg, "junction": use_junction,
                 "type_during_switch": type_during, "untrusted_B": untrusted_b, "run_root": run.root}
    H: dict = {}  # harness timestamps
    pc = None
    raw = b""
    sup_log = os.path.join(run.logs, "sup-steps.json")
    events = os.path.join(run.logs, "events.jsonl")
    profs: list = []
    cfg: dict = {"profiles": []}
    typed_keys: list = []  # every keystroke batch the harness sent, with ts

    def submit(text):
        typed_keys.append({"ts": time.time(), "text": text, "enter": True})
        pc.submit(text)

    def screen(name, since):
        with open(os.path.join(run.logs, f"screen-{name}.txt"), "w", encoding="utf-8") as f:
            f.write(strip_ansi(pc.text[since:], " "))

    def do_switch(n, frm, to, task_text):
        """flip <frm> to limit, submit task_text, then type nothing until <to> replies."""
        modes[f"sk-mock-{frm}"] = {"mode": "limit"}
        write_scenario(scen, modes)
        mk = pc.mark()
        H[f"s{n}_submit"] = time.time()
        submit(task_text)
        r = {}
        m, H[f"s{n}_limit_msg_visible"] = wait_text(pc, r"429|rate limit", 30, since=mk)
        r["limit_msg_on_screen"] = m.group(0) if m else None
        m, H[f"s{n}_notice_visible"] = wait_text(pc, rf"\[baton\] account {frm} hit", 60, since=mk)
        r["notice_on_screen"] = bool(m)
        mk_notice = pc.mark()
        if type_during and m:
            typed_keys.append({"ts": time.time(), "text": "zzTYPEDzz", "enter": False, "during_switch": n})
            pc.send("zzTYPEDzz")
        m, H[f"s{n}_reply_visible"] = wait_text(pc, rf"MOCK-REPLY\[{to}\]", 25 if untrusted_b else 90, since=mk)
        r["auto_reply"] = m.group(0) if m else None
        end = H[f"s{n}_reply_visible"] or time.time()
        r["keys_typed_after_task_submit_until_reply"] = [
            k for k in typed_keys if H[f"s{n}_submit"] + 0.01 < k["ts"] <= end]
        m, H[f"s{n}_new_footer_seen"] = wait_text(pc, r"auto mode on", 30, since=mk_notice)
        r["new_tui_footer_seen"] = bool(m)
        if type_during:
            time.sleep(2.0)
            r["typed_text_visible_in_new_tui"] = "zzTYPEDzz" in strip_ansi(pc.text[mk_notice:])
        screen(f"switch{n}", mk)
        return r

    try:
        A = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        B = run.profile("B", "sk-mock-B", srv.base_url, trust=None if untrusted_b else [run.proj])
        C = run.profile("C", "sk-mock-C", srv.base_url, trust=[run.proj])
        profs = [A, B, C]
        if use_junction:
            os.makedirs(os.path.join(A.dir, "projects"), exist_ok=True)
            for p in (B, C):
                junction(os.path.join(p.dir, "projects"), os.path.join(A.dir, "projects"))

        def hook(ev):
            return {"type": "command", "command": PY, "args": ["-I", HOOK, ev, events]}

        settings = {"hooks": {
            "SessionStart": [{"hooks": [hook("SessionStart")]}],
            "StopFailure": [{"matcher": "rate_limit|authentication_failed", "hooks": [hook("StopFailure")]}],
            # observer only (the supervisor ignores it): did CTRL_BREAK give a graceful shutdown?
            "SessionEnd": [{"hooks": [hook("SessionEnd")]}],
        }}
        settings_path = os.path.join(run.root, "run-settings.json")
        with open(settings_path, "w", encoding="utf-8") as f:
            json.dump(settings, f, indent=1)
        sid = str(uuid.uuid4())
        rep["session_id"] = sid
        cfg = {"claude": run.claude, "cwd": run.proj, "settings": settings_path, "events": events,
               "log": sup_log, "session_id": sid, "continue_prompt": CONTINUE, "resume_arg": resume_arg,
               "idle_timeout": 10, "exit_timeout": 10,
               "profiles": [{"name": p.name, "dir": p.dir, "env": run.env(p)} for p in profs]}
        cfg_path = os.path.join(run.root, "sup-input.json")
        with open(cfg_path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, indent=1)

        pc = PseudoConsole([PY, "-I", SUP, cfg_path], cwd=run.proj, env=run.env(None),
                           cols=120, rows=40, log_dir=run.logs, name="sup-pty")
        pc.start()
        H["pty_start"] = pc.started_at
        m, H["tui_ready"] = wait_text(pc, r"auto mode on|for shortcuts", 60)
        rep["tui_ready"] = bool(m)
        time.sleep(1.5)
        mk = pc.mark()
        H["submit_task1"] = time.time()
        submit("task one")
        m, H["reply_A_task1_visible"] = wait_text(pc, r"MOCK-REPLY\[A\][^\n]{0,20}task one", 60, since=mk)
        rep["task1_reply_A"] = m.group(0) if m else None
        time.sleep(1.5)

        rep["switch1"] = do_switch(1, "A", "B", "task two (should hit the limit)")
        if untrusted_b:
            mk = pc.mark()
            rep["untrusted_screen_tail"] = strip_ansi(pc.text)[-1500:]
            H["untrusted_enter"] = time.time()
            typed_keys.append({"ts": time.time(), "text": "<Enter>", "enter": True})
            pc.send("\r")
            m, H["untrusted_reply_B_after_enter"] = wait_text(pc, r"MOCK-REPLY\[B\]", 30, since=mk)
            rep["untrusted_reply_after_enter"] = m.group(0) if m else None
            time.sleep(2)
            screen("untrusted-after-enter", mk)
            pc.send("\x03")
            time.sleep(0.6)
            pc.send("\x03")
            rep["sup_exit_code"] = pc.wait_exit(25)
            raise SystemExit(0)
        time.sleep(1.5)
        mk = pc.mark()
        H["submit_task3"] = time.time()
        submit("task three")
        m, H["reply_B_task3_visible"] = wait_text(pc, r"MOCK-REPLY\[B\][^\n]{0,20}task three", 60, since=mk)
        rep["task3_reply_B"] = m.group(0) if m else None
        screen("task3", mk)
        time.sleep(1.5)

        rep["switch2"] = do_switch(2, "B", "C", "task four (B should hit the limit)")
        time.sleep(1.5)
        mk = pc.mark()
        H["submit_task5"] = time.time()
        submit("task five")
        m, H["reply_C_task5_visible"] = wait_text(pc, r"MOCK-REPLY\[C\][^\n]{0,20}task five", 60, since=mk)
        rep["task5_reply_C"] = m.group(0) if m else None
        screen("task5", mk)
        rep["screen_tail_after_task5"] = strip_ansi(pc.text[mk:])[-900:]
        time.sleep(1.5)
        pc.send("\x03")
        time.sleep(0.6)
        pc.send("\x03")
        rep["sup_exit_code"] = pc.wait_exit(25)
        H["sup_exit"] = time.time()
    except SystemExit:
        pass
    finally:
        if pc is not None:
            pc.close()
            raw = pc.raw
        rep["leftover_children_killed"] = kill_leftovers(sup_log, run.claude, H.get("pty_start", 0))
        srv.stop()

    # ---------------- analysis ----------------
    evs = read_jsonl(events)
    sup = json.load(open(sup_log, encoding="utf-8")) if os.path.exists(sup_log) else {}
    steps = sup.get("steps", [])
    mock = read_log(mock_log)
    bodies = read_jsonl(bodies_log)

    def rel(p):
        return os.path.relpath(p, run.root) if p else p

    rep["events"] = [{"ts": e.get("ts"), "event": e.get("event"), "ppid": e.get("ppid"),
                      "session_id": (e.get("payload") or {}).get("session_id"),
                      "source": (e.get("payload") or {}).get("source"),
                      "reason": (e.get("payload") or {}).get("reason"),
                      "error": (e.get("payload") or {}).get("error"),
                      "transcript_path": rel((e.get("payload") or {}).get("transcript_path")),
                      "transcript_path_exists_now": os.path.exists((e.get("payload") or {}).get("transcript_path") or "")}
                     for e in evs]
    out_steps = []
    for s in steps:
        if s.get("step") == "event":
            continue
        d = {k: v for k, v in s.items() if k not in ("snapshots", "argv", "transcript_path", "session_file")}
        if s.get("argv"):
            d["argv_tail"] = [rel(a) if os.path.isabs(a) else a for a in s["argv"][2:]]
        if s.get("transcript_path"):
            d["transcript_path"] = rel(s["transcript_path"])
        out_steps.append(d)
    rep["sup_steps"] = out_steps
    rep["idle_snapshots"] = [[{"t": x["t"], "status": (x.get("data") or {}).get("status"),
                               "statusUpdatedAt": (x.get("data") or {}).get("statusUpdatedAt"),
                               "keys": sorted((x.get("data") or {}).keys())} for x in s.get("snapshots") or []]
                             for s in steps if s.get("step") == "idle_wait_done"]
    rep["sup_events_seen"] = sup.get("events_seen")
    rep["mock_requests"] = [{k: r.get(k) for k in ("ts", "n", "account", "status", "main_loop", "n_messages",
                                                    "user_text", "text")} for r in mock if r.get("method") == "POST"]
    # per switch
    sw_begins = [i for i, s in enumerate(steps) if s.get("step") == "switch_begin"]
    switches = []
    for k, i in enumerate(sw_begins):
        to = cfg["profiles"][k + 1]["name"]
        seg = steps[i:(sw_begins[k + 1] if k + 1 < len(sw_begins) else len(steps))]
        st: dict = {}
        for s in seg:
            st.setdefault(s["step"], s["ts"])
        t0 = steps[i]["hook_ts"]
        nxt_start = [e for e in evs if e.get("event") == "SessionStart" and e["ts"] > t0]
        main_new = [b for b in bodies if b.get("account") == to and b.get("main_loop")]
        first = main_new[0] if main_new else None
        chain = {"hook_StopFailure": t0, "sup_saw_event": st.get("switch_begin"),
                 "sup_idle_seen": st.get("idle_wait_done"), "sup_ctrl_break": st.get("ctrl_break_sent"),
                 "sup_old_exited": st.get("old_exited"), "sup_console_restored": st.get("console_restored"),
                 "sup_new_spawned": st.get("spawned"),
                 "hook_new_SessionStart": nxt_start[0]["ts"] if nxt_start else None,
                 "new_first_main_request": first["ts"] if first else None,
                 "new_reply_visible_on_screen": H.get(f"s{k + 1}_reply_visible")}
        lat = {kk: (None if v is None else round(v - t0, 3)) for kk, v in chain.items()}
        lat["task_submit_to_hook"] = round(t0 - H[f"s{k + 1}_submit"], 3) if H.get(f"s{k + 1}_submit") else None
        tp = steps[i].get("transcript_path")
        info = {"from": cfg["profiles"][k]["name"], "to": to, "latency_s_from_StopFailure_hook": lat,
                "hook_transcript_path": rel(tp), "hook_transcript_path_exists": bool(tp) and os.path.exists(tp),
                "old_exit": {kk: v for kk, v in next((s for s in seg if s["step"] == "old_exited"), {}).items()
                             if kk in ("code", "killed", "modes_after_exit", "session_file_left")}}
        if first:
            flat = [(m["role"], text_blocks(m)) for m in first["messages"]]
            info["new_first_main_request"] = {
                "n_messages": first["n_messages"], "session_header": first.get("session_header"),
                "roles": [r for r, _ in flat],
                "user_texts_tail": [t[-170:] for r, t in flat if r == "user"],
                "assistant_texts": [t[:80] for r, t in flat if r == "assistant"],
                "idx": {w: match_idx(flat, w) for w in ("task one", "task two", "task three", "task four",
                                                         "The previous session stopped at a usage limit",
                                                         "zzTYPEDzz")},
                "contains_429_text": any("429" in t or "rate limit" in t for _, t in flat)}
        switches.append(info)
    rep["switches"] = switches
    # transcripts: every session .jsonl under the profiles (history.jsonl reported separately)
    tinfo, real_seen, hist = [], set(), {}
    for p in profs:
        hist[p.name] = [{"display": (r.get("display") or "")[:50], "sessionId": r.get("sessionId")}
                        for r in read_jsonl(os.path.join(p.dir, "history.jsonl"))]
        for dirpath, _, files in os.walk(p.dir):
            for fn in files:
                if not fn.endswith(".jsonl") or fn == "history.jsonl":
                    continue
                t = os.path.join(dirpath, fn)
                rows = read_jsonl(t)
                realp = os.path.realpath(t)
                types = [r.get("type") for r in rows]
                err_i = [i for i, r in enumerate(rows) if r.get("isApiErrorMessage")]
                convo = [s for s in summarize_rows(rows, 1000) if s.get("type") in ("user", "assistant")]
                tinfo.append({"path": rel(t), "realpath": rel(realp),
                              "same_file_as_earlier_entry": realp in real_seen,
                              "n_rows": len(rows), "session_ids": session_ids(rows),
                              "type_counts": {kk: types.count(kk) for kk in sorted(set(filter(None, types)))},
                              "api_error_row_indices": err_i,
                              "conversation": [{"role": c.get("role"), "err": c.get("error"),
                                                "content": c.get("content")} for c in convo]})
                if realp not in real_seen:
                    with open(os.path.join(run.logs, f"transcript-{p.name}-{fn}.summary.json"), "w",
                              encoding="utf-8") as f:
                        json.dump(summarize_rows(rows, 1000), f, ensure_ascii=False, indent=1)
                real_seen.add(realp)
    rep["transcripts"] = tinfo
    rep["n_distinct_transcript_files"] = len(real_seen)
    rep["history_jsonl"] = hist
    rep["harness_ts_rel"] = {k: (round(v - H["pty_start"], 3) if isinstance(v, float) else v)
                             for k, v in H.items()}
    rtxt = raw.decode("utf-8", "replace")
    plain_all = strip_ansi(rtxt)
    rep["pty_stream"] = {
        "bytes": len(raw),
        "alt_screen_enter": rtxt.count("\x1b[?1049h"), "alt_screen_leave": rtxt.count("\x1b[?1049l"),
        "mouse_on": rtxt.count("\x1b[?1000h"), "mouse_off": rtxt.count("\x1b[?1000l"),
        "bracketed_paste_on": rtxt.count("\x1b[?2004h"), "bracketed_paste_off": rtxt.count("\x1b[?2004l"),
        "literal_escape_leak": bool(re.search(r"\[\?(1049|2004|1004|1000|25)[lh]", plain_all)),
        "resume_hint_count": len(re.findall(r"claude --resume [0-9a-f-]+", plain_all)),
        "notice_occurrences": {n: plain_all.count(f"[baton] account {n} hit") for n in ("A", "B")},
    }
    print(json.dumps(rep, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
