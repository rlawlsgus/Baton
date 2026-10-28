"""End-to-end test: real baton.exe + real claude.exe against the local mock API.

No real account is used. Each "account" is a baton profile whose settings.json
points claude at the mock with a dummy key (apiKeyHelper), exactly like the
Phase 0 spikes. baton runs inside a ConPTY so claude's TUI has a real console.

    python -I test/e2e/e2e_baton.py <empty run dir> [--baton bin/baton.exe]

Scenarios:
  auto       A hits the limit -> baton switches to B by itself, same session continues
  manual     `baton switch acct-a` moves a running session
  confirm    confirm mode asks "[Y/n]" and switches after "y"
  exhausted  every account limited + on_exhausted=stop -> baton stops with a resume hint
  transient  a retryable throttle that recovers -> no switch
  overloaded 529 overload (server_error) -> no switch, session stays usable
  auth       invalid key (authentication_failed) -> account marked needs_login, switch
  billing    billing_error -> account marked blocked, NO switch
  clear      /clear starts a new session; a later limit continues that new session
  resumeid   `baton run --account acct-b -- -r <id>` finds a session stored under acct-a
  share      the user's ~/.claude skills/CLAUDE.md reach every profile
  git, flags, confirmn, wait, writer, bypass, hooksoff: see each scenario's docstring
  confirmc   Ctrl+C at the confirm question stops baton instead of reopening claude
  stopping   a manual switch in confirm mode waits for the running turn ("stopping")
  stop       `baton stop` ends a session without switching
  stopask    `baton stop` during the confirm question ends the session after the answer
  stopswitch `baton stop` while a manual switch waits for a turn ends the session
  import     a conversation from the user's own Claude Code, open in the desktop app, goes on
             in a copy (`baton resume <id> --copy`) on an account and switches on a limit
"""
from __future__ import annotations

import argparse
import glob
import json
import os
import re
import subprocess
import sys
import time

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.join(REPO, "spikes", "phase0", "harness"))
from cc import Run, read_jsonl, fwd  # noqa: E402
from conpty import PseudoConsole  # noqa: E402
from mockapi import MockServer, read_log, write_scenario  # noqa: E402

KEY_A, KEY_B = "sk-mock-A", "sk-mock-B"
CONTINUE_MARK = "previous session stopped at a usage limit"


class Fail(Exception):
    pass


def check(cond: bool, what: str, results: list) -> None:
    results.append({"check": what, "ok": bool(cond)})
    if not cond:
        raise Fail(what)


class World:
    def __init__(self, root: str, baton: str):
        self.run = Run(root)
        self.baton = os.path.abspath(baton)
        self.home = os.path.join(self.run.root, "baton-home")
        os.makedirs(self.home, exist_ok=True)
        self.scen = os.path.join(self.run.root, "scenario.json")
        write_scenario(self.scen, {KEY_A: {"mode": "echo"}, KEY_B: {"mode": "echo"}})
        self.mock_log = os.path.join(self.run.logs, "mock.jsonl")
        self.srv = MockServer(self.mock_log, self.scen)
        self.srv.start()
        prof = os.path.join(self.home, "profiles")
        # acct-a trusts the project; acct-b has never seen it, so baton must
        # seed trust before relaunching there (otherwise the trust dialog blocks).
        self.a = self.run.profile("A", KEY_A, self.srv.base_url, trust=[self.run.proj],
                                  dir=os.path.join(prof, "acct-a"))
        self.b = self.run.profile("B", KEY_B, self.srv.base_url, trust=None,
                                  dir=os.path.join(prof, "acct-b"))
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        accounts = {"schema": 1, "accounts": [
            {"id": "acct-a", "profile_dir": self.a.dir, "priority": 20, "enabled": True,
             "logged_in": True, "auth_method": "apiKey", "added": now},
            {"id": "acct-b", "profile_dir": self.b.dir, "priority": 10, "enabled": True,
             "logged_in": True, "auth_method": "apiKey", "added": now},
        ]}
        self.write_json("accounts.json", accounts)
        self.config(switch_mode="auto", on_exhausted="stop", cooldown_seconds=1, max_switches_per_hour=20)
        # The user's normal Claude setup (fake home), which baton shares into profiles.
        self.user_claude = os.path.join(self.run.home, ".claude")
        skill = os.path.join(self.user_claude, "skills", "e2e-skill")
        os.makedirs(skill, exist_ok=True)
        with open(os.path.join(skill, "SKILL.md"), "w", encoding="utf-8") as f:
            f.write("---\nname: e2e-skill\ndescription: test skill shared by baton\n---\nSay hi.\n")
        with open(os.path.join(self.user_claude, "CLAUDE.md"), "w", encoding="utf-8") as f:
            f.write("E2E shared memory\n")
        with open(os.path.join(self.user_claude, "settings.json"), "w", encoding="utf-8") as f:
            json.dump({"env": {"BATON_E2E_SHARED": "1"}}, f)

    def write_json(self, name: str, obj: dict) -> None:
        with open(os.path.join(self.home, name), "w", encoding="utf-8") as f:
            json.dump(obj, f, indent=1)

    def config(self, **kw) -> None:
        cfg = {"claude_path": self.run.claude, "exhausted_retry_minutes": 60, "risk_acknowledged": 2,
               "continue_prompt": "The previous session stopped at a usage limit and is now continuing on another account. "
                                  "Continue the task you were working on; do not repeat work that is already complete."}
        p = os.path.join(self.home, "config.json")
        if os.path.exists(p):
            with open(p, encoding="utf-8") as f:
                cfg.update(json.load(f))
        cfg.update(kw)
        self.write_json("config.json", cfg)

    def env(self) -> dict:
        return self.run.env(None, {"BATON_HOME": self.home})

    def state(self) -> dict:
        try:
            with open(os.path.join(self.home, "state.json"), encoding="utf-8") as f:
                return json.load(f)
        except (OSError, ValueError):
            return {}

    def reset_state(self) -> None:
        p = os.path.join(self.home, "state.json")
        if os.path.exists(p):
            os.remove(p)

    def accounts_flip(self, a: str, b: str) -> None:
        write_scenario(self.scen, {KEY_A: {"mode": a}, KEY_B: {"mode": b}})

    def start(self, name: str, args: list[str] | None = None, extra_env: dict | None = None,
              cwd: str | None = None) -> PseudoConsole:
        env = self.env()
        env.update(extra_env or {})
        pc = PseudoConsole([self.baton, "run", *(args or [])], cwd=cwd or self.run.proj, env=env,
                           log_dir=self.run.logs, name=name)
        pc.start()
        return pc

    def baton_cmd(self, *args: str) -> subprocess.CompletedProcess:
        return subprocess.run([self.baton, *args], env=self.env(), cwd=self.run.proj, capture_output=True,
                              text=True, encoding="utf-8", errors="replace", timeout=60, stdin=subprocess.DEVNULL)

    # ---- waiting helpers (judge progress from the mock / hook files, not the redrawn screen)
    def mock_rows(self) -> list[dict]:
        return [r for r in read_log(self.mock_log) if r.get("n")]

    def wait_mock(self, pred, timeout: float, since: int = 0) -> dict | None:
        deadline = time.time() + timeout
        while time.time() < deadline:
            for r in self.mock_rows()[since:]:
                if pred(r):
                    return r
            time.sleep(0.2)
        return None

    def newest_run_dir(self) -> str | None:
        dirs = sorted(glob.glob(os.path.join(self.home, "runs", "*")))
        return dirs[-1] if dirs else None

    def events(self, run_dir: str) -> list[dict]:
        return read_jsonl(os.path.join(run_dir, "events.jsonl"))

    def wait_session_starts(self, n: int, timeout: float) -> str | None:
        """Wait until the newest run has n SessionStart events; return its dir."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            d = self.newest_run_dir()
            if d and sum(1 for e in self.events(d) if e.get("event") == "SessionStart") >= n:
                return d
            time.sleep(0.2)
        return None

    def transcripts(self) -> list[str]:
        return glob.glob(os.path.join(self.home, "profiles", "*", "projects", "*", "*.jsonl"))


def process_started_ms(pid: int) -> int:
    """When a process started, in ms since 1970 (what claude writes as startedAt)."""
    import ctypes
    from ctypes import wintypes
    k32 = ctypes.WinDLL("kernel32", use_last_error=True)
    k32.OpenProcess.restype = wintypes.HANDLE
    h = k32.OpenProcess(0x1000, False, pid)  # PROCESS_QUERY_LIMITED_INFORMATION
    times = [wintypes.FILETIME() for _ in range(4)]
    k32.GetProcessTimes(h, *[ctypes.byref(t) for t in times])
    k32.CloseHandle(h)
    t = (times[0].dwHighDateTime << 32) | times[0].dwLowDateTime
    return (t - 116444736000000000) // 10000


def acct(r: dict, key: str) -> bool:
    return r.get("account") == "..." + key[-4:]


def main_turn(r: dict, key: str, text: str) -> bool:
    return acct(r, key) and r.get("main_loop") and text.lower() in (r.get("user_text") or "").lower() and r.get("status") == 200


def submit_when_ready(w: World, pc: PseudoConsole, starts: int, text: str) -> None:
    if not w.wait_session_starts(starts, 60):
        raise Fail(f"no SessionStart #{starts}")
    time.sleep(3.0)  # TUI is ready ~2-3 s after SessionStart
    submit(w, pc, text)


def submit(w: World, pc: PseudoConsole, text: str) -> None:
    """Type and send a prompt. While claude is still booting it can keep the
    typed text but drop the Enter, so press Enter again if no request shows up."""
    mark = len(w.mock_rows())
    pc.submit(text)
    for _ in range(3):
        if w.wait_mock(lambda r: r.get("main_loop") and text.lower() in (r.get("user_text") or "").lower(), 8, since=mark):
            return
        pc.send("\r")


def baton_noticed(w: World, pc: PseudoConsole, pattern: str, timeout: float, since: int = 0) -> bool:
    """A baton notice on the screen, or in the run's baton.log: ConPTY only
    repaints changed cells, so a notice can reach the screen in pieces."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if pc.wait_for(pattern, 1, since=since) is not None:
            return True
        d = w.newest_run_dir()
        if d:
            try:
                with open(os.path.join(d, "baton.log"), encoding="utf-8", errors="replace") as f:
                    if re.search(r"notice: .*" + pattern, f.read(), re.I):
                        return True
            except OSError:
                pass
    return False


def final_output_has(pc: PseudoConsole, pattern: str) -> bool:
    """baton's last lines are written just before it exits; give the console
    reader a moment to drain them before searching."""
    time.sleep(1.5)
    return pc.wait_for(pattern, 3) is not None


def quit_claude(pc: PseudoConsole) -> int | None:
    """Ctrl+C twice exits claude; if a turn was still finishing (e.g. its Stop
    hook running), the first press only interrupts it, so try again."""
    for _ in range(3):
        pc.send("\x03")
        time.sleep(0.4)
        pc.send("\x03")
        code = pc.wait_exit(8)
        if code is not None:
            return code
    return pc.wait_exit(15)


# ---- scenarios ----------------------------------------------------------------

def scenario_auto(w: World, res: list) -> dict:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto")
    pc = w.start("auto")
    try:
        submit_when_ready(w, pc, 1, "task one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "task one"), 60) is not None, "auto: A answers task one", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "task two hits the limit")
        t_limit = time.time()
        b_first = w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark)
        check(b_first is not None, "auto: B gets the continue prompt without any typing", res)
        switch_secs = round(time.time() - t_limit, 1)
        check(b_first["n_messages"] >= 4, f"auto: B's first request carries the history (n_messages={b_first['n_messages']})", res)
        time.sleep(4.0)
        submit(w, pc, "task three")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, "task three"), 60) is not None, "auto: B's TUI is usable after the switch", res)
        time.sleep(1.0)
        code = quit_claude(pc)
        check(code == 0, f"auto: baton exits 0 after the user quits claude (got {code})", res)
    finally:
        pc.close()
    st = w.state()
    a_state = st.get("accounts", {}).get("acct-a", {})
    check(a_state.get("status") == "exhausted", f"auto: ledger marks acct-a exhausted ({a_state.get('status')})", res)
    sw = st.get("switches", [])
    check(bool(sw) and sw[-1]["from"] == "acct-a" and sw[-1]["to"] == "acct-b" and sw[-1]["reason"] == "usage_limit",
          "auto: switch log has acct-a -> acct-b (usage_limit)", res)
    run_dir = w.newest_run_dir()
    journal = json.load(open(os.path.join(run_dir, "run.json"), encoding="utf-8"))
    sid = journal["session_id"]
    files = [p for p in w.transcripts() if os.path.basename(p) == sid + ".jsonl"]
    check(len(files) == 1 and os.sep + "acct-a" + os.sep in files[0], f"auto: one transcript file, in acct-a ({files})", res)
    sids = {r.get("sessionId") for r in read_jsonl(files[0]) if r.get("sessionId")}
    check(sids == {sid}, f"auto: every transcript row has the same session id ({sids})", res)
    trusted = w.b.read_claude_json().get("projects", {}).get(fwd(w.run.proj), {}).get("hasTrustDialogAccepted")
    check(trusted is True, "auto: baton seeded folder trust into acct-b before relaunching", res)
    check(journal["state"] == "done", "auto: run journal ends in state done", res)
    return {"switch_seconds_from_limit_prompt_to_B_request": switch_secs, "session": sid}


def scenario_manual(w: World, res: list) -> dict:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    pc = w.start("manual", ["--account", "acct-b"])
    try:
        submit_when_ready(w, pc, 1, "manual one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, "manual one"), 60) is not None, "manual: B answers", res)
        time.sleep(1.5)
        cp = w.baton_cmd("switch", "acct-a")
        check(cp.returncode == 0, f"manual: `baton switch acct-a` accepted ({cp.stderr.strip()})", res)
        submit_when_ready(w, pc, 2, "manual two")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "manual two"), 60) is not None, "manual: session continues on A", res)
        rows = [r for r in w.mock_rows() if acct(r, KEY_A) and r.get("main_loop")]
        check(not any(CONTINUE_MARK in (r.get("user_text") or "").lower() for r in rows),
              "manual: no continue prompt is injected on a manual switch", res)
        time.sleep(1.0)
        code = quit_claude(pc)
        check(code == 0, f"manual: baton exits 0 ({code})", res)
    finally:
        pc.close()
    sw = w.state().get("switches", [])
    check(bool(sw) and sw[-1]["reason"] == "manual" and sw[-1]["to"] == "acct-a", "manual: switch logged as manual", res)
    return {}


def scenario_confirm(w: World, res: list) -> dict:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm")
    pc = w.start("confirm")
    try:
        submit_when_ready(w, pc, 1, "confirm one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "confirm one"), 60) is not None, "confirm: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark_text = pc.mark()
        submit(w, pc, "confirm two hits the limit")
        check(pc.wait_for(r"Continue this session on", 60, since=mark_text) is not None, "confirm: baton asks before switching", res)
        time.sleep(0.5)
        mark = len(w.mock_rows())
        pc.send("y\r")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 60, since=mark) is not None, "confirm: switches after 'y'", res)
        time.sleep(4.0)
        code = quit_claude(pc)
        check(code == 0, f"confirm: baton exits 0 ({code})", res)
    finally:
        pc.close()
        w.config(switch_mode="auto")
    return {}


def scenario_exhausted(w: World, res: list) -> dict:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto", on_exhausted="stop")
    pc = w.start("exhausted")
    try:
        submit_when_ready(w, pc, 1, "ex one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "ex one"), 60) is not None, "exhausted: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "limit")
        mark_text = pc.mark()
        submit(w, pc, "ex two hits the limit")
        # A limits -> switch to B with the continue prompt -> B limits -> nothing left.
        check(baton_noticed(w, pc, r"every account is limited", 120, since=mark_text), "exhausted: baton says every account is limited", res)
        code = pc.wait_exit(30)
        check(code == 0, f"exhausted: baton stops cleanly ({code})", res)
        check(final_output_has(pc, r"continue later with"), "exhausted: prints a resume command", res)
    finally:
        pc.close()
    st = w.state().get("accounts", {})
    check(st.get("acct-a", {}).get("status") == "exhausted" and st.get("acct-b", {}).get("status") == "exhausted",
          "exhausted: both accounts marked exhausted", res)
    return {}


def start_on_a_and_answer(w: World, res: list, name: str, text: str, extra_env: dict | None = None) -> PseudoConsole:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto", on_exhausted="stop")
    pc = w.start(name, extra_env=extra_env)
    submit_when_ready(w, pc, 1, text)
    check(w.wait_mock(lambda r: main_turn(r, KEY_A, text), 60) is not None, f"{name}: A answers", res)
    time.sleep(1.5)
    return pc


def no_switch_logged(w: World) -> bool:
    return not w.state().get("switches")


def scenario_transient(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "transient", "tr one")
    try:
        write_scenario(w.scen, {KEY_A: {"mode": "transient429", "count": 2}, KEY_B: {"mode": "echo"}})
        submit(w, pc, "tr two retried")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "tr two retried"), 60) is not None, "transient: A answers after claude's own retries", res)
        time.sleep(2.0)
        check(no_switch_logged(w), "transient: no account switch", res)
        check(w.state().get("accounts", {}).get("acct-a", {}).get("status", "ok") == "ok", "transient: acct-a stays ok", res)
        check(quit_claude(pc) == 0, "transient: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_overloaded(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "overloaded", "ov one")
    try:
        w.accounts_flip("overloaded", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "ov two overloaded")
        time.sleep(8.0)  # claude retries 529 twice, then StopFailure(server_error)
        check(no_switch_logged(w), "overloaded: no account switch on server_error", res)
        check(not any(acct(r, KEY_B) for r in w.mock_rows()[mark:]), "overloaded: B is never used", res)
        w.accounts_flip("echo", "echo")
        submit(w, pc, "ov three")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "ov three"), 60) is not None, "overloaded: session stays usable on A", res)
        check(quit_claude(pc) == 0, "overloaded: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_auth(w: World, res: list) -> dict:
    # claude retries a 401 up to 10 times (~3+ min) before giving up; cap it so
    # the test does not wait that long. baton passes the variable through.
    pc = start_on_a_and_answer(w, res, "auth", "au one", {"CLAUDE_CODE_MAX_RETRIES": "1"})
    try:
        w.accounts_flip("auth401", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "au two bad key")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark) is not None,
              "auth: login failure moves the session to B", res)
        time.sleep(4.0)
        check(quit_claude(pc) == 0, "auth: baton exits 0", res)
    finally:
        pc.close()
    st = w.state()
    check(st.get("accounts", {}).get("acct-a", {}).get("status") == "needs_login", "auth: acct-a marked needs_login", res)
    sw = st.get("switches", [])
    check(bool(sw) and sw[-1]["reason"] == "authentication_failed", "auth: switch logged as authentication_failed", res)
    return {}


def scenario_billing(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "billing", "bi one")
    try:
        w.accounts_flip("billing", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "bi two billing")
        deadline = time.time() + 30
        while time.time() < deadline and w.state().get("accounts", {}).get("acct-a", {}).get("status") != "blocked":
            time.sleep(0.5)
        check(w.state().get("accounts", {}).get("acct-a", {}).get("status") == "blocked", "billing: acct-a marked blocked", res)
        time.sleep(3.0)
        check(no_switch_logged(w) and not any(acct(r, KEY_B) for r in w.mock_rows()[mark:]),
              "billing: baton does NOT move a blocked account's session", res)
        check(pc.is_alive(), "billing: claude keeps running so the user sees the error", res)
        check(quit_claude(pc) == 0, "billing: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_clear(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "clear", "cl one before clear")
    try:
        first_run = w.newest_run_dir()
        pc.submit("/clear")
        if not w.wait_session_starts(2, 30):
            raise Fail("clear: no SessionStart after /clear")
        new_sid = [e for e in w.events(first_run) if e.get("event") == "SessionStart"][-1]["payload"]["session_id"]
        time.sleep(3.0)
        submit(w, pc, "cl two after clear")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "cl two after clear"), 60) is not None, "clear: A answers in the new session", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "cl three hits the limit")
        b_first = w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark)
        check(b_first is not None, "clear: switch happens after /clear", res)
        check(b_first["n_messages"] <= 6, f"clear: B continues the post-/clear session, not the old one (n_messages={b_first['n_messages']})", res)
        time.sleep(4.0)
        check(quit_claude(pc) == 0, "clear: baton exits 0", res)
    finally:
        pc.close()
    journal = json.load(open(os.path.join(w.newest_run_dir(), "run.json"), encoding="utf-8"))
    check(journal["session_id"] == new_sid, f"clear: baton followed the new session id ({journal['session_id']} vs {new_sid})", res)
    return {}


def scenario_resumeid(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "resumeid-a", "ri one on A")
    try:
        sid = json.load(open(os.path.join(w.newest_run_dir(), "run.json"), encoding="utf-8"))["session_id"]
        check(quit_claude(pc) == 0, "resumeid: first run exits", res)
    finally:
        pc.close()
    pc = w.start("resumeid-b", ["--account", "acct-b", "--", "-r", sid])
    try:
        submit_when_ready(w, pc, 1, "ri two on B")
        row = w.wait_mock(lambda r: main_turn(r, KEY_B, "ri two on B"), 60)
        check(row is not None, "resumeid: B resumes a session stored in acct-a's profile by id", res)
        check(row["n_messages"] >= 4, f"resumeid: B sees the earlier turn (n_messages={row and row['n_messages']})", res)
        check(quit_claude(pc) == 0, "resumeid: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_share(w: World, res: list) -> dict:
    pc = start_on_a_and_answer(w, res, "share", "sh one")
    try:
        check(quit_claude(pc) == 0, "share: baton exits 0", res)
    finally:
        pc.close()
    skills = os.path.join(w.a.dir, "skills")
    check(os.path.isfile(os.path.join(skills, "e2e-skill", "SKILL.md")), "share: user skills visible in the profile", res)
    check(os.path.realpath(skills).lower() == os.path.realpath(os.path.join(w.user_claude, "skills")).lower(),
          "share: skills is a junction to ~/.claude/skills", res)
    with open(os.path.join(w.a.dir, "CLAUDE.md"), encoding="utf-8") as f:
        check(f.read() == "E2E shared memory\n", "share: CLAUDE.md copied", res)
    with open(w.a.settings_json, encoding="utf-8") as f:
        check("apiKeyHelper" in f.read(), "share: the profile's own settings.json is not overwritten", res)
    return {}


class keep_file:
    """Restore a file's content after a scenario that edits it."""

    def __init__(self, path: str):
        self.path = path

    def __enter__(self):
        with open(self.path, "rb") as f:
            self.data = f.read()
        return self

    def __exit__(self, *exc):
        with open(self.path, "wb") as f:
            f.write(self.data)


def scenario_git(w: World, res: list) -> dict:
    """Inside a git repo claude ignores trust above the repo root. acct-b only
    trusts a parent folder, so baton must seed the exact folder before the switch."""
    repo = os.path.join(w.run.root, "gitrepo")
    os.makedirs(repo, exist_ok=True)
    subprocess.run(["git", "init", "-q", repo], capture_output=True)
    if not os.path.isdir(os.path.join(repo, ".git")):
        os.makedirs(os.path.join(repo, ".git"))
    with keep_file(w.a.claude_json), keep_file(w.b.claude_json):
        for prof, key in ((w.a, fwd(repo)), (w.b, fwd(w.run.root))):
            cj = prof.read_claude_json()
            cj.setdefault("projects", {})[key] = {"hasTrustDialogAccepted": True}
            with open(prof.claude_json, "w", encoding="utf-8") as f:
                json.dump(cj, f)
        w.reset_state()
        w.accounts_flip("echo", "echo")
        w.config(switch_mode="auto", on_exhausted="stop")
        pc = w.start("git", cwd=repo)
        try:
            submit_when_ready(w, pc, 1, "git one")
            check(w.wait_mock(lambda r: main_turn(r, KEY_A, "git one"), 60) is not None, "git: A answers in the repo", res)
            time.sleep(1.5)
            w.accounts_flip("limit", "echo")
            mark = len(w.mock_rows())
            submit(w, pc, "git two hits the limit")
            check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark) is not None,
                  "git: B continues without the trust dialog", res)
            trusted = w.b.read_claude_json().get("projects", {}).get(fwd(repo), {}).get("hasTrustDialogAccepted")
            check(trusted is True, "git: baton wrote trust for the exact repo folder into acct-b", res)
            time.sleep(4.0)
            check(quit_claude(pc) == 0, "git: baton exits 0", res)
        finally:
            pc.close()
    return {}


def scenario_flags(w: World, res: list) -> dict:
    """User options survive a switch; the first prompt is not sent again."""
    d1, d2 = os.path.join(w.run.root, "extra1"), os.path.join(w.run.root, "extra2")
    os.makedirs(d1, exist_ok=True)
    os.makedirs(d2, exist_ok=True)
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto", on_exhausted="stop")
    # The prompt goes first: --add-dir (like claude itself) takes every following word.
    pc = w.start("flags", ["--", "flags first prompt", "--permission-mode", "acceptEdits",
                           "--model", "claude-sonnet-5-5", "--add-dir", d1, d2])
    try:
        if not w.wait_session_starts(1, 60):
            raise Fail("flags: no SessionStart")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "flags first prompt"), 60) is not None,
              "flags: the first prompt is submitted on A", res)
        time.sleep(2.0)
        w.accounts_flip("limit", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "flags two hits the limit")
        b_first = w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark)
        check(b_first is not None, "flags: B resumes with the continue prompt", res)
        check(b_first is not None and b_first.get("model") == "claude-sonnet-5-5",
              f"flags: --model is kept after the switch ({b_first and b_first.get('model')})", res)
        later = [r for r in w.mock_rows()[mark:] if acct(r, KEY_B) and r.get("main_loop")]
        check(not any("flags first prompt" == (r.get("user_text") or "") for r in later),
              "flags: the original prompt is not re-submitted on B", res)
        with open(os.path.join(w.newest_run_dir(), "baton.log"), encoding="utf-8") as f:
            b_launch = [l for l in f if "launch account=acct-b" in l]
        check(bool(b_launch) and '"--permission-mode" "acceptEdits"' in b_launch[-1],
              "flags: the permission mode is carried to B (claude does not restore it on resume)", res)
        time.sleep(3.0)
        check(quit_claude(pc) == 0, "flags: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_confirmn(w: World, res: list) -> dict:
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm", on_exhausted="stop")
    pc = w.start("confirmn")
    try:
        submit_when_ready(w, pc, 1, "cn one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "cn one"), 60) is not None, "confirmn: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark_text = pc.mark()
        submit(w, pc, "cn two hits the limit")
        check(pc.wait_for(r"Continue this session on", 60, since=mark_text) is not None, "confirmn: baton asks", res)
        time.sleep(0.5)
        mark = len(w.mock_rows())
        pc.send("n\r")
        check(w.wait_session_starts(2, 60) is not None, "confirmn: claude is relaunched on the same account", res)
        time.sleep(6.0)
        rows = w.mock_rows()[mark:]
        check(not any(acct(r, KEY_B) for r in rows), "confirmn: B is not used after 'n'", res)
        check(not any(CONTINUE_MARK in (r.get("user_text") or "").lower() for r in rows),
              "confirmn: no continue prompt is sent after 'n'", res)
        check(not w.state().get("switches"), "confirmn: no switch logged", res)
        check(quit_claude(pc) == 0, "confirmn: baton exits 0", res)
    finally:
        pc.close()
        w.config(switch_mode="auto")
    return {}


def scenario_wait(w: World, res: list) -> dict:
    """Every account limited + on_exhausted=wait; `baton switch` ends the wait."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto", on_exhausted="wait")
    pc = w.start("wait")
    try:
        submit_when_ready(w, pc, 1, "wt one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "wt one"), 60) is not None, "wait: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "limit")
        mark_text = pc.mark()
        submit(w, pc, "wt two hits the limit")
        check(baton_noticed(w, pc, r"waiting for", 120, since=mark_text), "wait: baton waits when every account is limited", res)
        # A limited, then B (which got the session) limited too; the session now
        # waits on acct-b. The user knows acct-a is free again and asks for it.
        w.accounts_flip("echo", "limit")
        mark = len(w.mock_rows())
        cp = w.baton_cmd("switch", "acct-a")
        check(cp.returncode == 0, f"wait: `baton switch acct-a` accepted while waiting ({cp.stderr.strip()})", res)
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, CONTINUE_MARK), 60, since=mark) is not None,
              "wait: the session continues on A at once, with the continue prompt", res)
        time.sleep(4.0)
        check(quit_claude(pc) == 0, "wait: baton exits 0", res)
    finally:
        pc.close()
        w.config(on_exhausted="stop")
    return {}


def scenario_writer(w: World, res: list) -> dict:
    """A second baton run must not open a session another claude is writing."""
    pc = start_on_a_and_answer(w, res, "writer-1", "wr one")
    try:
        sid = json.load(open(os.path.join(w.newest_run_dir(), "run.json"), encoding="utf-8"))["session_id"]
        path = [p for p in w.transcripts() if os.path.basename(p) == sid + ".jsonl"][0]
        pc2 = w.start("writer-2", ["--account", "acct-b", "--", "--resume", path])
        try:
            code = pc2.wait_exit(30)
            check(code not in (None, 0), f"writer: the second run refuses (exit {code})", res)
            check(final_output_has(pc2, r"already open in another claude"), "writer: it says why", res)
        finally:
            pc2.close()
        check(quit_claude(pc) == 0, "writer: the first run still exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_bypass(w: World, res: list) -> dict:
    """--dangerously-skip-permissions: the user accepted the warning on A only.
    B must not stop on claude's "Bypass Permissions mode" warning (whose
    default answer exits) when the session moves there."""
    with keep_file(w.a.settings_json), keep_file(w.b.settings_json):
        with open(w.a.settings_json, encoding="utf-8") as f:
            s = json.load(f)
        s["skipDangerousModePermissionPrompt"] = True
        with open(w.a.settings_json, "w", encoding="utf-8") as f:
            json.dump(s, f)
        w.reset_state()
        w.accounts_flip("echo", "echo")
        w.config(switch_mode="auto", on_exhausted="stop")
        pc = w.start("bypass", ["--", "--dangerously-skip-permissions"])
        try:
            submit_when_ready(w, pc, 1, "by one")
            check(w.wait_mock(lambda r: main_turn(r, KEY_A, "by one"), 60) is not None, "bypass: A answers", res)
            time.sleep(1.5)
            w.accounts_flip("limit", "echo")
            mark = len(w.mock_rows())
            submit(w, pc, "by two hits the limit")
            check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark) is not None,
                  "bypass: B continues without stopping on the bypass warning", res)
            time.sleep(4.0)
            check(quit_claude(pc) == 0, "bypass: baton exits 0", res)
        finally:
            pc.close()
    return {}


def scenario_hooksoff(w: World, res: list) -> dict:
    with keep_file(w.a.settings_json):
        with open(w.a.settings_json, encoding="utf-8") as f:
            s = json.load(f)
        s["disableAllHooks"] = True
        with open(w.a.settings_json, "w", encoding="utf-8") as f:
            json.dump(s, f)
        w.reset_state()
        w.accounts_flip("echo", "echo")
        pc = w.start("hooksoff", ["--account", "acct-a"])
        try:
            check(pc.wait_for(r"sets disableAllHooks", 30) is not None, "hooksoff: baton warns before starting", res)
            time.sleep(8.0)
            code = quit_claude(pc)
            check(code == 0, f"hooksoff: baton exits 0 ({code})", res)
            check(final_output_has(pc, r"never reported to baton"), "hooksoff: baton says it saw no hook events", res)
        finally:
            pc.close()
    return {}


def scenario_confirmc(w: World, res: list) -> dict:
    """Ctrl+C at the confirm question stops baton, as Ctrl+C during a wait does,
    instead of reopening claude on the limited account."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm", on_exhausted="stop")
    pc = w.start("confirmc")
    try:
        submit_when_ready(w, pc, 1, "cc one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "cc one"), 60) is not None, "confirmc: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark_text = pc.mark()
        submit(w, pc, "cc two hits the limit")
        check(pc.wait_for(r"Continue this session on", 60, since=mark_text) is not None, "confirmc: baton asks", res)
        time.sleep(0.5)
        pc.send("\x03")
        code = pc.wait_exit(20)
        check(code == 0, f"confirmc: Ctrl+C stops baton (exit {code})", res)
        check(final_output_has(pc, r"continue later with"), "confirmc: it prints how to resume", res)
        starts = sum(1 for e in w.events(w.newest_run_dir()) if e.get("event") == "SessionStart")
        check(starts == 1, f"confirmc: claude is not reopened on the limited account ({starts} starts)", res)
    finally:
        pc.close()
        w.config(switch_mode="auto")
    return {}


def scenario_stopping(w: World, res: list) -> dict:
    """In confirm mode a `baton switch` lets the running turn finish: the run
    shows "stopping", a second untargeted switch is told it is already under
    way, and the question comes once the turn has ended."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm", on_exhausted="stop")
    pc = w.start("stopping")
    try:
        submit_when_ready(w, pc, 1, "st one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "st one"), 60) is not None, "stopping: A answers", res)
        time.sleep(1.5)
        write_scenario(w.scen, {KEY_A: {"mode": "slow", "seconds": 15}, KEY_B: {"mode": "echo"}})
        submit(w, pc, "st two runs long")
        mark_text = pc.mark()
        t0 = time.time()
        cp = w.baton_cmd("switch")
        check(cp.returncode == 0, f"stopping: `baton switch` accepted during a turn ({cp.stderr.strip()})", res)
        time.sleep(1.5)
        st = w.baton_cmd("status")
        check("state=stopping" in st.stdout, f"stopping: status shows the run as stopping ({st.stdout.strip()[-200:]})", res)
        cp2 = w.baton_cmd("switch")
        check(cp2.returncode != 0 and "already switching" in cp2.stderr,
              f"stopping: a second `baton switch` is told it is under way ({cp2.stderr.strip()})", res)
        check(pc.wait_for(r"Continue this session on", 60, since=mark_text) is not None, "stopping: baton asks after the turn", res)
        waited = round(time.time() - t0, 1)
        check(waited >= 8, f"stopping: the turn was not cut short (asked after {waited}s of a 15s reply)", res)
        time.sleep(0.5)
        pc.send("y\r")
        submit_when_ready(w, pc, 2, "st three")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, "st three"), 60) is not None, "stopping: the session continues on B", res)
        time.sleep(1.0)
        check(quit_claude(pc) == 0, "stopping: baton exits 0", res)
    finally:
        pc.close()
        w.accounts_flip("echo", "echo")
        w.config(switch_mode="auto")
    return {}


def scenario_stop(w: World, res: list) -> dict:
    """`baton stop` ends a session cleanly: no switch, exit 0, a resume hint."""
    pc = start_on_a_and_answer(w, res, "stop", "sp one")
    try:
        cp = w.baton_cmd("stop")
        check(cp.returncode == 0, f"stop: `baton stop` accepted ({cp.stderr.strip()})", res)
        code = pc.wait_exit(40)
        check(code == 0, f"stop: baton exits 0 ({code})", res)
        check(baton_noticed(w, pc, r"session ended as requested", 5), "stop: baton says the session ended", res)
        check(final_output_has(pc, r"continue later with"), "stop: prints a resume command", res)
        check(no_switch_logged(w), "stop: no switch logged", res)
    finally:
        pc.close()
    return {}


def scenario_stopask(w: World, res: list) -> dict:
    """`baton stop` while baton asks before a switch (confirm mode): answering
    the question then ends the session; claude is not reopened on the next
    account with the continue prompt."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm", on_exhausted="stop")
    pc = w.start("stopask")
    try:
        submit_when_ready(w, pc, 1, "sa one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "sa one"), 60) is not None, "stopask: A answers", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark_text = pc.mark()
        submit(w, pc, "sa two hits the limit")
        check(pc.wait_for(r"Continue this session on", 60, since=mark_text) is not None, "stopask: baton asks", res)
        cp = w.baton_cmd("stop")
        check(cp.returncode == 0, f"stopask: `baton stop` accepted while asking ({cp.stderr.strip()})", res)
        time.sleep(0.5)
        pc.send("y\r")
        code = pc.wait_exit(30)
        check(code == 0, f"stopask: baton exits 0 ({code})", res)
        check(baton_noticed(w, pc, r"session ended as requested", 5), "stopask: baton says the session ended", res)
        starts = sum(1 for e in w.events(w.newest_run_dir()) if e.get("event") == "SessionStart")
        check(starts == 1, f"stopask: claude is not reopened on the next account ({starts} starts)", res)
        check(no_switch_logged(w), "stopask: no switch logged", res)
    finally:
        pc.close()
        w.accounts_flip("echo", "echo")
        w.config(switch_mode="auto")
    return {}


def scenario_stopswitch(w: World, res: list) -> dict:
    """`baton stop` while a manual switch waits for a long turn (confirm mode):
    the run shows "ending", the turn is cut off after the stop's short wait,
    and the session ends without a switch."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="confirm", on_exhausted="stop")
    pc = w.start("stopswitch")
    try:
        submit_when_ready(w, pc, 1, "ss one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "ss one"), 60) is not None, "stopswitch: A answers", res)
        time.sleep(1.5)
        write_scenario(w.scen, {KEY_A: {"mode": "slow", "seconds": 40}, KEY_B: {"mode": "echo"}})
        submit(w, pc, "ss two runs long")
        time.sleep(1.0)
        cp = w.baton_cmd("switch")
        check(cp.returncode == 0, f"stopswitch: `baton switch` accepted during a turn ({cp.stderr.strip()})", res)
        time.sleep(1.5)
        t0 = time.time()
        cp = w.baton_cmd("stop")
        check(cp.returncode == 0, f"stopswitch: `baton stop` accepted while stopping ({cp.stderr.strip()})", res)
        time.sleep(1.0)
        st = w.baton_cmd("status")
        check("state=ending" in st.stdout, f"stopswitch: status shows the run as ending ({st.stdout.strip()[-200:]})", res)
        code = pc.wait_exit(40)
        took = round(time.time() - t0, 1)
        check(code == 0, f"stopswitch: baton exits 0 ({code})", res)
        check(took < 30, f"stopswitch: the stop did not wait for the 40s turn ({took}s)", res)
        check(baton_noticed(w, pc, r"session ended as requested", 5), "stopswitch: baton says the session ended", res)
        check(pc.wait_for(r"Continue this session on", 1) is None, "stopswitch: no switch question", res)
        check(no_switch_logged(w), "stopswitch: no switch logged", res)
    finally:
        pc.close()
        w.accounts_flip("echo", "echo")
        w.config(switch_mode="auto")
    return {}


def scenario_import(w: World, res: list) -> dict:
    """A conversation kept in the user's own Claude Code and open there (as the desktop
    app keeps its sessions): `baton resume` refuses to write to it, and `--copy` goes on in
    a fork on an account, started from another folder, while the original stays as it was.
    A later limit continues the fork, not the original."""
    pc = start_on_a_and_answer(w, res, "import-first", "im one in the desktop app")
    try:
        sid = json.load(open(os.path.join(w.newest_run_dir(), "run.json"), encoding="utf-8"))["session_id"]
        check(quit_claude(pc) == 0, "import: first run exits", res)
    finally:
        pc.close()
    src = [p for p in w.transcripts() if os.path.basename(p) == sid + ".jsonl"][0]
    dst = os.path.join(w.user_claude, "projects", os.path.basename(os.path.dirname(src)), sid + ".jsonl")
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    os.replace(src, dst)
    with open(dst, "a", encoding="utf-8") as f:
        f.write(json.dumps({"type": "custom-title", "customTitle": "desktop work", "sessionId": sid}) + "\n")
    with open(dst, "rb") as f:
        before = f.read()
    entry = os.path.join(w.user_claude, "sessions", f"{os.getpid()}.json")
    os.makedirs(os.path.dirname(entry), exist_ok=True)
    with open(entry, "w", encoding="utf-8") as f:
        json.dump({"pid": os.getpid(), "sessionId": sid, "cwd": w.run.proj, "kind": "interactive", "status": "idle",
                   "startedAt": process_started_ms(os.getpid())}, f)
    try:
        listed = w.baton_cmd("sessions")
        check(sid in listed.stdout and "desktop work" in listed.stdout and "open in another window" in listed.stdout,
              f"import: baton sessions lists it, named and open ({listed.stdout[:300]!r})", res)
        refused = w.baton_cmd("resume", sid)
        check(refused.returncode == 1 and "--copy" in refused.stderr,
              f"import: resume without --copy refuses an open conversation ({refused.stderr.strip()[:200]!r})", res)
        w.reset_state()
        w.accounts_flip("echo", "echo")
        mark = len(w.mock_rows())
        pc = PseudoConsole([w.baton, "resume", sid, "--copy", "--account", "acct-a"], cwd=w.run.root, env=w.env(),
                           log_dir=w.run.logs, name="import-copy")
        pc.start()
        try:
            submit_when_ready(w, pc, 1, "im two in baton")
            row = w.wait_mock(lambda r: main_turn(r, KEY_A, "im two in baton"), 60, since=mark)
            check(row is not None, "import: the copy goes on on acct-a", res)
            check(row["n_messages"] >= 4, f"import: the copy carries the earlier turn (n_messages={row and row['n_messages']})", res)
            time.sleep(1.5)
            w.accounts_flip("limit", "echo")
            mark = len(w.mock_rows())
            submit(w, pc, "im three hits the limit")
            b_first = w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark)
            check(b_first is not None, "import: a limit continues the copy on acct-b", res)
            check(b_first["n_messages"] >= 6, f"import: acct-b reads the copy's own turns (n_messages={b_first['n_messages']})", res)
            time.sleep(4.0)
            check(quit_claude(pc) == 0, "import: baton exits 0", res)
        finally:
            pc.close()
    finally:
        os.remove(entry)
    with open(dst, "rb") as f:
        check(f.read() == before, "import: the original conversation is untouched", res)
    journal = json.load(open(os.path.join(w.newest_run_dir(), "run.json"), encoding="utf-8"))
    fork = journal["session_id"]
    files = [p for p in w.transcripts() if os.path.basename(p) == fork + ".jsonl"]
    check(fork != sid and len(files) == 1, f"import: the copy is a conversation of its own ({fork}, {files})", res)
    check(os.path.normcase(journal["cwd"]) == os.path.normcase(w.run.proj), f"import: it ran in the conversation's folder ({journal['cwd']})", res)
    return {"original": sid, "copy": fork}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("root")
    ap.add_argument("--baton", default=os.path.join(REPO, "bin", "baton.exe"))
    ap.add_argument("--only", default="auto,manual,confirm,exhausted,transient,overloaded,auth,billing,clear,resumeid,share,"
                                      "git,flags,confirmn,wait,writer,bypass,hooksoff,confirmc,stopping,stop,stopask,stopswitch,import")
    a = ap.parse_args()
    w = World(a.root, a.baton)
    report = {"claude": w.run.claude, "baton": w.baton, "scenarios": {}}
    ok_all = True
    for name in a.only.split(","):
        res: list = []
        fn = globals()["scenario_" + name]
        try:
            extra = fn(w, res)
            report["scenarios"][name] = {"ok": True, "checks": res, **extra}
        except Exception as e:  # noqa: BLE001 - report every failure
            ok_all = False
            report["scenarios"][name] = {"ok": False, "error": f"{type(e).__name__}: {e}", "checks": res}
    w.srv.stop()
    report["ok"] = ok_all
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 0 if ok_all else 1


if __name__ == "__main__":
    sys.exit(main())
