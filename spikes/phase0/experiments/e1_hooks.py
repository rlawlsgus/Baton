"""e1_hooks: do hooks and a statusLine supplied ONLY via `--settings <file>` register and fire in
the interactive TUI on Windows? Do exec-form hooks run without a shell? What do payloads contain?

    python -I spikes/phase0/experiments/e1_hooks.py <empty run_root>

Prints one JSON report on stdout. Evidence stays in the run root:
  events.jsonl            every hook / statusLine invocation (see e1_hooks_hooklog.py)
  logs/<session>.txt      stripped TUI output per session, logs/mock.jsonl mock requests
  launcher/*.json         the --settings files that were passed
  report.json             copy of the printed report

Sessions
  S1   normal paths: run hooks (exec + one shell form) + statusLine via --settings, an extra hook and
       statusLine in the PROFILE settings.json; echo -> 429 limit -> /clear -> echo -> Ctrl-C x2
  S2   profile dir, hook script and settings file under "<run>/경로 테스트/..." (space + Hangul);
       exec form vs 4 shell-form quoting styles; echo -> limit -> exit
  S3-6 statusLine variants (exec-form attempt, command+args, 8.3 short path with \\ and /, quoted long \\ path)
  S7   shell-form hooks forced to PowerShell ("shell":"powershell"), 5 quoting styles vs exec form
  S8   Git removed from PATH: is Git Bash still used for shell-form hooks / statusLine?
  S9   profile WITHOUT project trust: does a trust dialog appear, do --settings hooks fire before it
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import sys
import time
import traceback
from http.server import ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, read_jsonl, session_ids, summarize_rows, transcripts, fwd, short_path, wait_until  # noqa: E402
from mockapi import Handler, MockServer, MockState, read_log, write_scenario  # noqa: E402

PY = sys.executable
HOOKLOG_SRC = os.path.join(HERE, "e1_hooks_hooklog.py")
# Tricky literal argv elements for exec form; the last one checks placeholder substitution.
PROBE = ["sp ace", "한글 인자", 'dq"inner"q', "$HOME", "%PATH%", "back\\slash\\", "a&b|c>d", "`bt`", "",
         "${CLAUDE_PROJECT_DIR}"]
READY_RX = r"for shortcuts|auto mode on|SLTAG\[|\? for|>\s"
DIALOG_RX = {
    "trust": r"trust this folder|Do you trust|trust the files|Yes, I trust|Is this a project you",
    "hook_approval": r"hooks?.{0,40}(approv|review|trust|allow|confirm)|(approv|review|allow|confirm).{0,40}hooks?",
    "do_you_want": r"Do you want to",
    "settings_changed": r"settings.{0,30}(changed|modified)",
    "hook_error": r"hook.{0,30}(error|failed)|(error|failed).{0,30}hook",
}


# ---- mock: add unified rate-limit headers to 200 streams (harness Handler is not edited) -------------
class E1Handler(Handler):
    def _sse_start(self, extra_headers=None):
        try:
            sc = self.state.scenario_for(self._account())
        except Exception:  # noqa: BLE001
            sc = {}
        h = dict(sc.get("headers200") or {})
        h.update(extra_headers or {})
        Handler._sse_start(self, h)


class E1Mock(MockServer):
    def __init__(self, log_path, scenario_path=None, port=0):  # noqa: D401 - copy of MockServer.__init__
        self.state = MockState(log_path, scenario_path)
        handler = type("E1BoundHandler", (E1Handler,), {"state": self.state})
        self.httpd = ThreadingHTTPServer(("127.0.0.1", port), handler)
        self.httpd.daemon_threads = True
        self.thread = None


def h200() -> dict:
    now = int(time.time())
    return {"anthropic-ratelimit-unified-status": "allowed",
            "anthropic-ratelimit-unified-reset": str(now + 3600),
            "anthropic-ratelimit-unified-representative-claim": "five_hour",
            "anthropic-ratelimit-unified-5h-status": "allowed",
            "anthropic-ratelimit-unified-5h-utilization": "0.31",
            "anthropic-ratelimit-unified-5h-reset": str(now + 3600),
            "anthropic-ratelimit-unified-7d-status": "allowed",
            "anthropic-ratelimit-unified-7d-utilization": "0.12",
            "anthropic-ratelimit-unified-7d-reset": str(now + 86400 * 3)}


def sha(p: str) -> str:
    with open(p, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()[:16]


def trunc(v, n: int = 240):
    if isinstance(v, str):
        return v if len(v) <= n else v[:n] + f"...<{len(v)}>"
    if isinstance(v, dict):
        return {k: trunc(x, n) for k, x in v.items()}
    if isinstance(v, list):
        return [trunc(x, n) for x in v[:20]]
    return v


class Exp:
    def __init__(self, root: str):
        self.run = Run(root)
        self.R = self.run.root
        self.events = os.path.join(self.R, "events.jsonl")
        self.scen = os.path.join(self.R, "scenario.json")
        self.accounts: dict[str, dict] = {}
        self.H200 = h200()
        for k in ("A", "H", "V", "N", "U"):
            self.accounts[f"sk-mock-{k}"] = {"mode": "echo"}
        self.accounts["sk-mock-A"]["headers200"] = self.H200
        write_scenario(self.scen, self.accounts)
        self.mock_log = os.path.join(self.run.logs, "mock.jsonl")
        self.srv = E1Mock(self.mock_log, self.scen)
        self.srv.start()
        # hook logger copies: plain path and space+Hangul path
        self.script = os.path.join(self.R, "bin", "hooklog.py")
        os.makedirs(os.path.dirname(self.script), exist_ok=True)
        shutil.copyfile(HOOKLOG_SRC, self.script)
        self.hg = os.path.join(self.R, "경로 테스트")
        self.hg_script = os.path.join(self.hg, "스크립트 폴더", "훅 로거.py")
        os.makedirs(os.path.dirname(self.hg_script), exist_ok=True)
        shutil.copyfile(HOOKLOG_SRC, self.hg_script)
        self.report: dict = {"claude": self.run.claude, "python": PY, "run_root": self.R,
                             "mock_port": self.srv.port, "sessions": {}, "errors": []}
        self.report["mock_headers200_selfcheck"] = self.selfcheck()

    def selfcheck(self) -> dict:
        """Prove the 200 stream for sk-mock-A carries unified rate-limit headers (sent by our subclass)."""
        import urllib.request
        body = json.dumps({"model": "selfcheck", "max_tokens": 5, "stream": True,
                           "messages": [{"role": "user", "content": "selfcheck"}]}).encode()
        req = urllib.request.Request(self.srv.base_url + "/v1/messages", data=body, method="POST",
                                     headers={"x-api-key": "sk-mock-A", "content-type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=10) as r:
                hdrs = {k.lower(): v for k, v in r.getheaders()}
                r.read()
            return {"status": 200, "unified_headers": {k: v for k, v in hdrs.items()
                                                       if k.startswith("anthropic-ratelimit-unified")}}
        except Exception as ex:  # noqa: BLE001
            return {"error": repr(ex)}

    # ---- scenario ----
    def set_mode(self, key: str, sc: dict) -> None:
        self.accounts[key] = sc
        write_scenario(self.scen, self.accounts)

    # ---- hook builders ----
    def exec_hook(self, script: str, sess: str, tag: str, probe: bool = True, timeout: int = 30) -> dict:
        args = ["-I", script, "--sess", sess, "--tag", tag, "--out", self.events]
        if probe:
            args += ["--probe", *PROBE]
        return {"type": "command", "command": PY, "args": args, "timeout": timeout}

    @staticmethod
    def styled(p: str, style: str) -> str:
        if style == "fwdq":
            return f'"{fwd(p)}"'
        if style == "amp_fwdq":  # PowerShell call operator form
            return f'"{fwd(p)}"'
        if style == "bsq":
            return f'"{os.path.abspath(p)}"'
        if style == "short_bs":
            return short_path(p)
        if style == "short_fwd":
            return short_path(p).replace("\\", "/")
        raise ValueError(style)

    def shell_cmd(self, script: str, sess: str, tag: str, style: str, sl: bool = False) -> str:
        parts = [self.styled(PY, style), "-I", self.styled(script, style), "--sess", sess, "--tag", tag,
                 "--out", f'"{fwd(self.events)}"']
        if sl:
            parts.append("--sl")
        parts += ["--probe", '"sp ace"', "$HOME", "'$HOME'"]
        if style == "amp_fwdq":
            parts.insert(0, "&")
        return " ".join(parts)

    def shell_hook(self, script: str, sess: str, tag: str, style: str, shell: str | None = None) -> dict:
        h = {"type": "command", "command": self.shell_cmd(script, sess, tag, style), "timeout": 30}
        if shell:
            h["shell"] = shell
        return h

    def write_settings(self, name: str, obj: dict, dir: str | None = None) -> str:
        d = dir or os.path.join(self.R, "launcher")
        os.makedirs(d, exist_ok=True)
        p = os.path.join(d, name)
        with open(p, "w", encoding="utf-8") as f:
            json.dump(obj, f, indent=1, ensure_ascii=False)
        return p

    # ---- events ----
    def evs(self, sess: str | None = None) -> list[dict]:
        out = []
        for e in read_jsonl(self.events):
            if sess and e.get("sess") != sess:
                continue
            out.append(e)
        return out

    def wait_ev(self, sess: str, pred, timeout: float, n: int = 1) -> list[dict]:
        r = wait_until(lambda: (lambda xs: xs if len(xs) >= n else None)([e for e in self.evs(sess) if pred(e)]),
                       timeout)
        return r or []

    # ---- TUI ----
    def tui(self, profile, settings_path: str | None, name: str, extra_env: dict | None = None,
            args: tuple = ()):
        argv = (["--settings", settings_path] if settings_path else []) + list(args)
        pc = self.run.pty(profile, argv, name=name, extra_env=extra_env)
        pc.start()
        m = pc.wait_for(READY_RX, 60)
        info = {"argv_tail": argv, "pid": pc.pid, "ready": bool(m),
                "ready_secs": round(time.time() - pc.started_at, 1)}
        time.sleep(1.5)
        return pc, info

    @staticmethod
    def prompt(pc, text: str, expect: str | None, timeout: float = 60):
        m = pc.mark()
        pc.submit(text)
        got = pc.wait_for(expect, timeout, since=m) if expect else None
        return m, bool(got)

    @staticmethod
    def exit_tui(pc) -> dict:
        pc.send("\x03")
        time.sleep(0.6)
        pc.send("\x03")
        code = pc.wait_exit(20)
        extra = None
        if code is None:
            pc.send("\x03")
            time.sleep(0.6)
            pc.send("\x03")
            code = pc.wait_exit(10)
            extra = "needed 2nd ctrl-c pair"
        return {"exit_code": code, "note": extra}

    @staticmethod
    def dialog_flags(text: str) -> dict:
        """The TUI often moves the cursor instead of printing spaces, so match on squashed text too."""
        out = {}
        squashed = re.sub(r"\s+", "", text)
        for k, rx in DIALOG_RX.items():
            for t, r in ((text, rx), (squashed, rx.replace(" ", ""))):
                m = re.search(r, t, re.I | re.S)
                if m:
                    s = max(0, m.start() - 80)
                    out[k] = " ".join(t[s:m.end() + 80].split())
                    break
        return out

    # ---- summaries ----
    @staticmethod
    def ev_row(e: dict, t0: float) -> dict:
        p = e.get("stdin") or {}
        row = {"t": round(e["ts"] - t0, 2), "tag": e.get("tag"), "kind": e.get("kind"),
               "event": p.get("hook_event_name")}
        for k in ("source", "reason", "error", "session_id", "prompt"):
            if k in p:
                row[k] = trunc(p[k], 60)
        row["parent"] = [x.get("exe") for x in (e.get("parents") or [])[1:4]]
        return row

    def session_summary(self, sess: str, t0: float, claude_pid: int | None = None) -> dict:
        evs = self.evs(sess)
        hooks = [e for e in evs if e.get("kind") == "hook"]
        sls = [e for e in evs if e.get("kind") == "statusline"]
        out = {"n_hook_events": len(hooks), "n_statusline": len(sls),
               "hook_order": [self.ev_row(e, t0) for e in hooks],
               "tags_fired": sorted({e.get("tag") for e in hooks}),
               "statusline_tags": sorted({e.get("tag") for e in sls})}
        if claude_pid:
            exec_h = [e for e in hooks if not ("-sh-" in e["tag"] or e["tag"].endswith("-shell"))]
            out["pid_checks"] = {
                "claude_pid": claude_pid,
                "exec_hooks_direct_child_of_claude": sorted({(e.get("parents") or [{}, {}])[1].get("pid") == claude_pid
                                                             for e in exec_h if len(e.get("parents") or []) > 1}),
                "env_CLAUDE_PID_equals_claude_pid": sorted({(e.get("env") or {}).get("CLAUDE_PID") == str(claude_pid)
                                                            for e in hooks}),
            }
        pdirs = {}
        for e in hooks:
            form = "shell" if ("-sh-" in e["tag"] or e["tag"].endswith("-shell")) else "exec"
            pdirs.setdefault(form, set()).add((e.get("env") or {}).get("CLAUDE_PROJECT_DIR"))
        out["CLAUDE_PROJECT_DIR_by_form"] = {k: sorted(x for x in v if x) for k, v in pdirs.items()}
        tstate = []
        for e in hooks:
            ta = e.get("transcript_at_hook")
            if ta is None or e["tag"].startswith("profile-") or "-sh-" in e["tag"] or e["tag"].endswith("-shell"):
                continue
            p = e.get("stdin") or {}
            tp = p.get("transcript_path") or ""
            tstate.append({"tag": e["tag"], "exists": ta.get("exists"), "size": ta.get("size"),
                           "basename_is_session_id": os.path.splitext(os.path.basename(tp))[0] == p.get("session_id"),
                           "exists_at_end": os.path.exists(tp),
                           "last_rows": ta.get("last_rows")})
        out["transcript_at_hook"] = tstate
        if sls:
            ts = [round(e["ts"] - t0, 2) for e in sls]
            out["statusline_times"] = ts
            out["statusline_keys_union"] = sorted({k for e in sls for k in (e.get("stdin") or {})})
            rl = [e["stdin"].get("rate_limits") for e in sls if isinstance(e.get("stdin"), dict)
                  and "rate_limits" in e["stdin"]]
            out["statusline_rate_limits_values"] = rl[:5]
            out["statusline_parent_chain"] = [x.get("exe") for x in (sls[0].get("parents") or [])[:5]]
            out["statusline_raw_cmdline"] = sls[0].get("raw_cmdline")
            out["statusline_argv"] = sls[0].get("argv")
        return out

    # =====================================================================================
    def s1(self):
        s = "S1"
        info: dict = {}
        prof_hooks = {"UserPromptSubmit": [{"hooks": [self.exec_hook(self.script, s, "profile-UPS")]}],
                      "SessionStart": [{"hooks": [self.exec_hook(self.script, s, "profile-SS")]}]}
        A = self.run.profile("A", "sk-mock-A", self.srv.base_url, trust=[self.run.proj], settings_extra={
            "hooks": prof_hooks,
            "statusLine": {"type": "command", "command": self.shell_cmd(self.script, s, "profile-SL", "fwdq", sl=True)}})
        X = lambda tag: self.exec_hook(self.script, s, tag)  # noqa: E731
        settings = {
            "hooks": {
                "SessionStart": [{"hooks": [X("run-SS")]}],
                "UserPromptSubmit": [{"hooks": [X("run-UPS"), self.shell_hook(self.script, s, "run-UPS-shell", "fwdq")]}],
                "Stop": [{"hooks": [X("run-Stop")]}],
                "StopFailure": [{"matcher": "rate_limit", "hooks": [X("run-SF-rate_limit")]},
                                {"hooks": [X("run-SF-nomatcher")]},
                                {"matcher": "*", "hooks": [X("run-SF-star")]},
                                {"matcher": "server_error", "hooks": [X("run-SF-server_error")]}],
                "SessionEnd": [{"hooks": [X("run-SE")]}],
            },
            "statusLine": {"type": "command", "command": self.shell_cmd(self.script, s, "run-SL", "fwdq", sl=True),
                           "padding": 0},
        }
        sp = self.write_settings("S1-settings.json", settings)
        info["settings_file"] = sp
        h0 = sha(sp)
        pc, ti = self.tui(A, sp, s)
        info.update(ti)
        t0 = pc.started_at
        info["t0"] = t0
        try:
            info["ss_startup_seen"] = bool(self.wait_ev(s, lambda e: e["tag"] == "run-SS", 15))
            time.sleep(1.0)
            scr = pc.plain()
            info["dialog_flags_startup"] = self.dialog_flags(scr)
            info["screen_startup_tail"] = scr[-1200:]
            info["events_before_any_keypress"] = [e["tag"] for e in self.evs(s)]

            _, ok = self.prompt(pc, "e1 first prompt", r"MOCK-REPLY\[A\]")
            info["reply1"] = ok
            info["stop1_seen"] = bool(self.wait_ev(s, lambda e: e["tag"] == "run-Stop", 15))
            time.sleep(2.0)

            self.set_mode("sk-mock-A", {"mode": "limit"})
            m, _ = self.prompt(pc, "e1 second prompt", None)
            info["sf_seen"] = bool(self.wait_ev(s, lambda e: e["tag"].startswith("run-SF"), 60))
            time.sleep(4.0)
            info["screen_after_limit"] = pc.plain(m)[-1500:]
            info["dialog_flags_after_limit"] = self.dialog_flags(pc.plain(m))

            self.set_mode("sk-mock-A", {"mode": "echo", "headers200": self.H200})
            m, _ = self.prompt(pc, "/clear", None)
            info["ss_clear_seen"] = bool(self.wait_ev(
                s, lambda e: e["tag"] == "run-SS" and (e.get("stdin") or {}).get("source") == "clear", 20))
            time.sleep(2.0)
            info["screen_after_clear"] = pc.plain(m)[-800:]
            _, ok = self.prompt(pc, "e1 third prompt", r"MOCK-REPLY\[A\]")
            info["reply3"] = ok
            info["stop2_seen"] = bool(self.wait_ev(s, lambda e: e["tag"] == "run-Stop", 15, n=2))
            time.sleep(2.0)
            t_exit = time.time()
            info["exit"] = self.exit_tui(pc)
            info["exit_secs"] = round(time.time() - t_exit, 2)
            info["se_after_exit"] = [self.ev_row(e, t0) for e in self.wait_ev(
                s, lambda e: e["tag"] == "run-SE" and e["ts"] >= t_exit, 10)]
        finally:
            info["close_code"] = pc.close()
        info["settings_file_unchanged"] = sha(sp) == h0
        info["launcher_dir_listing"] = sorted(os.listdir(os.path.dirname(sp)))
        info["profile_transcripts"] = [os.path.relpath(p, A.dir) for p in transcripts(A.dir)]
        info["profile_transcript_sessions"] = {os.path.relpath(p, A.dir): session_ids(read_jsonl(p))
                                               for p in transcripts(A.dir)}
        info.update(self.session_summary(s, t0, pc.pid))
        return info

    # =====================================================================================
    def s2(self):
        s = "S2"
        info: dict = {}
        pdir = os.path.join(self.hg, "프로필 A 계정")
        H = self.run.profile("H", "sk-mock-H", self.srv.base_url, trust=[self.run.proj], dir=pdir)
        sc = self.hg_script
        X = lambda tag: self.exec_hook(sc, s, tag)  # noqa: E731
        settings = {
            "hooks": {
                "SessionStart": [{"hooks": [X("hg-SS")]}],
                "UserPromptSubmit": [{"hooks": [X("hg-UPS"),
                                                self.shell_hook(sc, s, "hg-UPS-sh-fwdq", "fwdq"),
                                                self.shell_hook(sc, s, "hg-UPS-sh-bsq", "bsq"),
                                                self.shell_hook(sc, s, "hg-UPS-sh-short_bs", "short_bs"),
                                                self.shell_hook(sc, s, "hg-UPS-sh-short_fwd", "short_fwd")]}],
                "Stop": [{"hooks": [X("hg-Stop")]}],
                "StopFailure": [{"matcher": "rate_limit", "hooks": [X("hg-SF-rate_limit")]}],
                "SessionEnd": [{"hooks": [X("hg-SE")]}],
            },
            "statusLine": {"type": "command", "command": self.shell_cmd(sc, s, "hg-SL-fwdq", "fwdq", sl=True)},
        }
        sp = self.write_settings("run settings 설정.json", settings, dir=os.path.join(self.hg, "설정 폴더"))
        info["settings_file"] = sp
        info["profile_dir"] = pdir
        info["script"] = sc
        info["short_script"] = short_path(sc)
        info["short_python"] = short_path(PY)
        info["short_has_no_space_or_hangul"] = not re.search(r"[ ㄱ-힣]", short_path(sc))
        info["shell_commands"] = {h.get("command")[:60] + "...": h["command"] for h in
                                  settings["hooks"]["UserPromptSubmit"][0]["hooks"][1:]}
        pc, ti = self.tui(H, sp, s)
        info.update(ti)
        t0 = pc.started_at
        try:
            info["ss_seen"] = bool(self.wait_ev(s, lambda e: e["tag"] == "hg-SS", 15))
            _, ok = self.prompt(pc, "e1 hangul prompt", r"MOCK-REPLY\[H\]")
            info["reply1"] = ok
            info["stop_seen"] = bool(self.wait_ev(s, lambda e: e["tag"] == "hg-Stop", 15))
            time.sleep(3.0)  # let slower shell-form hooks finish
            self.set_mode("sk-mock-H", {"mode": "limit"})
            m, _ = self.prompt(pc, "e1 hangul second", None)
            info["sf_seen"] = bool(self.wait_ev(s, lambda e: e["tag"].startswith("hg-SF"), 60))
            time.sleep(2.0)
            info["screen_after_limit"] = pc.plain(m)[-800:]
            info["dialog_flags"] = self.dialog_flags(pc.plain())
            t_exit = time.time()
            info["exit"] = self.exit_tui(pc)
            info["se_after_exit"] = bool(self.wait_ev(s, lambda e: e["tag"] == "hg-SE", 10))
        finally:
            info["close_code"] = pc.close()
        info["screen_tail"] = pc.plain()[-1500:]
        info["profile_transcripts"] = [os.path.relpath(p, H.dir) for p in transcripts(H.dir)]
        info.update(self.session_summary(s, t0, pc.pid))
        return info

    # =====================================================================================
    def sl_session(self, s: str, profile, statusline: dict, hooks: dict | None = None,
                   extra_env: dict | None = None, extra_settings: dict | None = None) -> dict:
        info: dict = {"statusLine": statusline}
        settings = {"statusLine": statusline}
        if hooks:
            settings["hooks"] = hooks
        if extra_settings:
            settings.update(extra_settings)
            info["extra_settings"] = extra_settings
        sp = self.write_settings(f"{s}-settings.json", settings)
        pc, ti = self.tui(profile, sp, s, extra_env=extra_env)
        info.update(ti)
        t0 = pc.started_at
        try:
            info["sl_seen_before_prompt"] = bool(self.wait_ev(s, lambda e: e["kind"] == "statusline", 8))
            _, ok = self.prompt(pc, f"e1 {s} prompt", r"MOCK-REPLY")
            info["reply"] = ok
            time.sleep(3.0)
            info["exit"] = self.exit_tui(pc)
            time.sleep(1.0)
        finally:
            info["close_code"] = pc.close()
        scr = pc.plain()
        info["screen_has_SLTAG"] = sorted(set(re.findall(r"SLTAG\[([^\]]*)\]", scr)))
        info["dialog_flags"] = self.dialog_flags(scr)
        info["screen_tail"] = scr[-900:]
        info.update(self.session_summary(s, t0, pc.pid))
        return info

    def s3_6(self):
        V = self.run.profile("V", "sk-mock-V", self.srv.base_url, trust=[self.run.proj])
        out = {}
        # S3: exec form (command + args) for statusLine; command uses forward slashes so a shell could run it.
        # A SessionStart hook in the same file proves the settings file itself was accepted.
        out["S3_exec_attempt"] = self.sl_session("S3", V, {
            "type": "command", "command": fwd(PY),
            "args": ["-I", self.script, "--sess", "S3", "--tag", "SL-exec", "--out", self.events, "--sl"]},
            hooks={"SessionStart": [{"hooks": [self.exec_hook(self.script, "S3", "S3-SS", probe=False)]}]})
        # S3b: full command line in `command` AND different args: which one runs?
        out["S3b_cmd_plus_args"] = self.sl_session("S3b", V, {
            "type": "command", "command": self.shell_cmd(self.script, "S3b", "SL-cmdline", "fwdq", sl=True),
            "args": ["-I", self.script, "--sess", "S3b", "--tag", "SL-args", "--out", self.events, "--sl"]})
        out["S4_short_bs"] = self.sl_session("S4", V, {
            "type": "command", "command": self.shell_cmd(self.hg_script, "S4", "SL-short_bs", "short_bs", sl=True)})
        out["S5_short_fwd"] = self.sl_session("S5", V, {
            "type": "command", "command": self.shell_cmd(self.hg_script, "S5", "SL-short_fwd", "short_fwd", sl=True)})
        out["S6_bsq_hangul"] = self.sl_session("S6", V, {
            "type": "command", "command": self.shell_cmd(self.hg_script, "S6", "SL-bsq", "bsq", sl=True)})
        # S6b: does settings.defaultShell=powershell change the shell of statusLine / shell-form hooks?
        sc = self.hg_script
        out["S6b_defaultShell_powershell"] = self.sl_session("S6b", V, {
            "type": "command", "command": self.shell_cmd(sc, "S6b", "SL-ds-short_fwd", "short_fwd", sl=True)},
            hooks={"UserPromptSubmit": [{"hooks": [self.shell_hook(sc, "S6b", "ds-UPS-sh-fwdq", "fwdq"),
                                                   self.shell_hook(sc, "S6b", "ds-UPS-sh-amp_fwdq", "amp_fwdq")]}]},
            extra_settings={"defaultShell": "powershell"})
        return out

    def s7_8(self):
        N = self.run.profile("N", "sk-mock-N", self.srv.base_url, trust=[self.run.proj])
        path = os.environ.get("PATH", "")
        kept = [p for p in path.split(os.pathsep) if p and "\\git\\" not in p.lower() + "\\"
                and not p.lower().rstrip("\\").endswith("\\git")]
        env = {"PATH": os.pathsep.join(kept)}
        removed = [p for p in path.split(os.pathsep) if p and p not in kept]
        sc = self.hg_script
        out = {"path_entries_removed": removed}
        # S7: shell-form hooks forced to PowerShell via the per-hook "shell" field (Space+Hangul script path)
        s = "S7"
        styles = ("fwdq", "amp_fwdq", "bsq", "short_bs", "short_fwd")
        hooks = {"SessionStart": [{"hooks": [self.exec_hook(sc, s, "ps-SS")]}],
                 "UserPromptSubmit": [{"hooks": [self.exec_hook(sc, s, "ps-UPS")] + [
                     self.shell_hook(sc, s, f"ps-UPS-sh-{st}", st, shell="powershell") for st in styles]}]}
        out["S7_powershell_hooks"] = self.sl_session(s, N, {
            "type": "command", "command": self.shell_cmd(sc, s, "ps-SL-short_fwd", "short_fwd", sl=True)},
            hooks=hooks)
        # S8: Git removed from PATH: is Git Bash still found (shell-form hook + statusLine)?
        s = "S8"
        hooks = {"UserPromptSubmit": [{"hooks": [self.exec_hook(sc, s, "ng-UPS"),
                                                 self.shell_hook(sc, s, "ng-UPS-sh-fwdq", "fwdq")]}]}
        out["S8_nogit_path"] = self.sl_session(s, N, {
            "type": "command", "command": self.shell_cmd(sc, s, "ng-SL-fwdq", "fwdq", sl=True)},
            hooks=hooks, extra_env=env)
        return out

    def s9(self):
        s = "S9"
        info: dict = {}
        U = self.run.profile("U", "sk-mock-U", self.srv.base_url, trust=None)
        X = lambda tag: self.exec_hook(self.script, s, tag, probe=False)  # noqa: E731
        settings = {"hooks": {"SessionStart": [{"hooks": [X("ut-SS")]}],
                              "UserPromptSubmit": [{"hooks": [X("ut-UPS")]}],
                              "Stop": [{"hooks": [X("ut-Stop")]}]},
                    "statusLine": {"type": "command",
                                   "command": self.shell_cmd(self.script, s, "ut-SL", "fwdq", sl=True)}}
        sp = self.write_settings("S9-settings.json", settings)
        pc = self.run.pty(U, ["--settings", sp], name=s)
        pc.start()
        t0 = pc.started_at
        try:
            time.sleep(9.0)
            scr = pc.plain()
            info["screen_first_9s_tail"] = scr[-1500:]
            info["dialog_flags_first_9s"] = self.dialog_flags(scr)
            info["events_before_accept"] = [self.ev_row(e, t0) for e in self.evs(s)]
            trust_shown = "trust" in info["dialog_flags_first_9s"]
            info["trust_dialog_shown"] = trust_shown
            sq = re.sub(r"\s+", "", scr)
            info["trust_default_pointer"] = ("No, exit" if ">No,exit" in sq else
                                             "Yes, I trust" if ">Yes,Itrust" in sq else None)
            if trust_shown:
                # move the pointer to "Yes, I trust this folder" (Enter on the default would exit)
                m1 = pc.mark()
                if info["trust_default_pointer"] != "Yes, I trust":
                    pc.send("\x1b[B")
                    time.sleep(0.8)
                info["raw_redraw_after_down"] = pc.text[m1:][-300:]
                t_acc = time.time()
                m0 = pc.mark()
                pc.send("\r")
                info["ready_after_accept"] = bool(pc.wait_for(READY_RX, 20, since=m0))
                time.sleep(3.0)
                info["events_after_accept"] = [self.ev_row(e, t0) for e in self.evs(s) if e["ts"] >= t_acc]
            _, ok = self.prompt(pc, "e1 untrusted prompt", r"MOCK-REPLY\[U\]")
            info["reply"] = ok
            self.wait_ev(s, lambda e: e["tag"] == "ut-Stop", 10)
            time.sleep(1.5)
            info["exit"] = self.exit_tui(pc)
        finally:
            info["close_code"] = pc.close()
        cj = U.read_claude_json()
        info["profile_projects_after"] = {k: {kk: vv for kk, vv in v.items() if kk == "hasTrustDialogAccepted"}
                                          for k, v in (cj.get("projects") or {}).items()}
        info.update(self.session_summary(s, t0, pc.pid))
        return info

    # =====================================================================================
    def payload_catalog(self) -> dict:
        cat: dict = {}
        for e in self.evs():
            p = e.get("stdin")
            if not isinstance(p, dict):
                continue
            key = p.get("hook_event_name") or ("statusLine" if e.get("kind") == "statusline" else "?")
            c = cat.setdefault(key, {"n": 0, "keys": set(), "sample": None, "values": {}})
            c["n"] += 1
            c["keys"].update(p.keys())
            if c["sample"] is None:
                c["sample"] = trunc(p, 160)
            for k in ("source", "reason", "error", "error_details", "last_assistant_message", "stop_hook_active",
                      "permission_mode", "model", "agent_type"):
                if k in p:
                    vals = c["values"].setdefault(k, [])
                    v = trunc(p[k], 200)
                    if v not in vals and len(vals) < 6:
                        vals.append(v)
        for c in cat.values():
            c["keys"] = sorted(c["keys"])
        return cat

    def argv_checks(self) -> dict:
        """exec-form hooks: argv received vs configured; parent chain; raw command line."""
        rows = []
        for e in self.evs():
            if e.get("kind") != "hook":
                continue
            argv = e.get("argv") or []
            if "--probe" not in argv:
                continue
            got = argv[argv.index("--probe") + 1:]
            tag = e.get("tag", "")
            is_shell = "-sh-" in tag or tag.endswith("-shell")
            row = {"sess": e.get("sess"), "tag": tag, "form": "shell" if is_shell else "exec",
                   "parents": [x.get("exe") for x in (e.get("parents") or [])[:5]]}
            if is_shell:
                row["probe_received"] = got
            else:
                row["probe_exact_except_placeholder"] = got[:-1] == PROBE[:-1]
                row["probe_received"] = got if got[:-1] != PROBE[:-1] else "(exact)"
                row["placeholder_arg"] = {"sent": PROBE[-1], "received": got[-1] if got else None}
                row["script_argv0_exact"] = os.path.normcase(argv[0]) == os.path.normcase(
                    self.hg_script if e.get("sess") in ("S2", "S7", "S8") else self.script)
            row["raw_cmdline"] = trunc(e.get("raw_cmdline"), 400)
            rows.append(row)
        # one row per (sess, tag) is enough
        seen, uniq = set(), []
        for r in rows:
            k = (r["sess"], r["tag"])
            if k not in seen:
                seen.add(k)
                uniq.append(r)
        return {"rows": uniq}

    def claims(self) -> dict:
        """Mechanical verdict hints from the evidence (the analyst still reads the evidence)."""
        S = self.report["sessions"]
        out: dict = {}
        s1 = S.get("S1") or {}
        tags = set(s1.get("tags_fired") or [])
        if s1:
            need = {"run-SS", "run-UPS", "run-Stop", "run-SE"}
            out["C1_settings_hooks_fire_in_tui"] = {"ok": need <= tags, "missing": sorted(need - tags)}
            rows = self.report["argv_checks"]["rows"]
            ex = [r for r in rows if r["form"] == "exec"]
            out["C2_exec_form_no_shell"] = {
                "n_exec_rows": len(ex),
                "all_argv_exact": all(r.get("probe_exact_except_placeholder") for r in ex),
                "all_direct_child_of_claude": all(len(r["parents"]) > 1 and r["parents"][1] == "claude.exe" for r in ex),
                "hangul_space_rows_ok": [r["tag"] for r in ex if r["sess"] in ("S2", "S7", "S8")
                                         and r.get("probe_exact_except_placeholder") and r.get("script_argv0_exact")],
                "shell_rows_parents": sorted({tuple(r["parents"][1:3]) for r in rows if r["form"] == "shell"}),
            }
            sf = [e for e in self.evs("S1") if e.get("tag", "").startswith("run-SF")]
            out["C3_stopfailure_rate_limit"] = {
                "fired_tags": sorted({e["tag"] for e in sf}),
                "not_fired": sorted({"run-SF-rate_limit", "run-SF-nomatcher", "run-SF-star", "run-SF-server_error"}
                                    - {e["tag"] for e in sf}),
                "error_values": sorted({(e.get("stdin") or {}).get("error") for e in sf}),
                "has_transcript_path_and_session_id": all(
                    (e.get("stdin") or {}).get("transcript_path") and (e.get("stdin") or {}).get("session_id") for e in sf),
                "error_details_present": any("error_details" in (e.get("stdin") or {}) for e in sf),
            }
            out["C4_merge_with_profile_hooks"] = {
                "profile_hooks_fired": sorted(t for t in tags if t.startswith("profile-")),
                "run_hooks_fired": sorted(t for t in tags if t.startswith("run-")),
                "statusline_tags": s1.get("statusline_tags"),
            }
            out["C5_statusline"] = {"tags": s1.get("statusline_tags"), "times": s1.get("statusline_times"),
                                    "keys": s1.get("statusline_keys_union"),
                                    "rate_limits_present": bool(s1.get("statusline_rate_limits_values"))}
            ss = [e for e in self.evs("S1") if e.get("tag") == "run-SS"]
            srcs = [((e.get("stdin") or {}).get("source"), (e.get("stdin") or {}).get("session_id")) for e in ss]
            out["C6_sessionstart_after_clear"] = {"run_SS_events": srcs,
                                                  "new_session_id": len({x[1] for x in srcs}) == len(srcs) > 1}
            out["C7_no_approval_prompt"] = {"S1_events_before_any_keypress": s1.get("events_before_any_keypress"),
                                            "S1_dialog_flags": s1.get("dialog_flags_startup"),
                                            "S9_untrusted": {k: (S.get("S9") or {}).get(k) for k in (
                                                "trust_dialog_shown", "trust_default_pointer", "events_before_accept",
                                                "events_after_accept")}}
        return out

    def finish(self) -> dict:
        self.report["payload_catalog"] = self.payload_catalog()
        self.report["argv_checks"] = self.argv_checks()
        try:
            self.report["claims"] = self.claims()
        except Exception as ex:  # noqa: BLE001
            self.report["claims"] = {"error": repr(ex)}
        env_names = sorted({n for e in self.evs() for n in (e.get("env_names") or [])})
        env_vals = {}
        for e in self.evs():
            for k, v in (e.get("env") or {}).items():
                env_vals.setdefault(k, set()).add(v)
        self.report["hook_env_names_union"] = env_names
        self.report["hook_env_values"] = {k: sorted(v)[:4] for k, v in env_vals.items()}
        self.report["hook_cwds"] = sorted({e.get("cwd") for e in self.evs()})
        mock = read_log(self.mock_log)
        self.report["mock_requests"] = [{k: r.get(k) for k in ("n", "account", "mode", "status", "main_loop",
                                                               "user_text", "reply")} for r in mock if r.get("n")]
        return self.report


def main(root: str) -> int:
    if os.path.exists(root) and os.listdir(root):
        print(json.dumps({"error": f"run_root not empty: {root}"}))
        return 2
    exp = Exp(root)
    try:
        for name, fn in (("S1", exp.s1), ("S2", exp.s2), ("S3_6", exp.s3_6), ("S7_8", exp.s7_8), ("S9", exp.s9)):
            if os.environ.get("E1_ONLY") and name not in os.environ["E1_ONLY"].split(","):
                continue
            t = time.time()
            try:
                exp.report["sessions"][name] = fn()
            except Exception as ex:  # noqa: BLE001
                exp.report["errors"].append({"session": name, "error": repr(ex), "tb": traceback.format_exc()[-2000:]})
            exp.report.setdefault("session_secs", {})[name] = round(time.time() - t, 1)
    finally:
        exp.srv.stop()
    rep = exp.finish()
    txt = json.dumps(rep, ensure_ascii=False, indent=1, default=str)
    with open(os.path.join(exp.R, "report.json"), "w", encoding="utf-8") as f:
        f.write(txt)
    print(txt)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
