"""End-to-end test: real baton.exe + real claude.exe against the local mock API.

No real account is used. Each "account" is a baton profile whose settings.json
points claude at the mock with a dummy key (apiKeyHelper), exactly like the
Phase 0 spikes. baton runs inside a ConPTY so claude's TUI has a real console.

    python -I test/e2e/e2e_baton.py <empty run dir> [--baton bin/baton.exe] [--baton-test bin/baton-test.exe]

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
  statusline baton's status line runs in the session and shows the account; a profile's own
             status line is left alone
  remotectl  a session started with Remote Control (--rc) keeps working where Remote Control is
             not available (API-key accounts), and keeps the flag after a switch
  remotetwo  two sessions at once, Remote Control on one only: only its launches carry --rc (its
             switch included), only its run records it and only its row in the window shows it
  busytrust  a second session started from the window on Automatic while one runs on the top account,
             in a new folder: it starts on that account too, with the trust written into the profile
             in use, and the running claude keeps that trust when it saves its settings
  rules      a folder rule keeps the sessions under a folder on the accounts it names
  restore    a session cut off mid-response (as by a power cut) reopens with `baton run --restore`
             and goes on with its task
  office     the window's office (GET /api/office, from the test build `bin/baton-test.exe`)
             shows a scripted turn live: rooms per tool, a subagent coming and going, a failing
             command, then the coffee break

Windows notifications go to a file instead of the screen (BATON_NOTIFY_FILE): auto, confirm,
exhausted and billing check the ones they would show.
"""
from __future__ import annotations

import argparse
import glob
import http.client
import itertools
import json
import os
import queue
import re
import subprocess
import sys
import threading
import time
import urllib.parse

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.join(REPO, "spikes", "phase0", "harness"))
from cc import Run, read_jsonl, fwd, wait_until  # noqa: E402
from conpty import PseudoConsole  # noqa: E402
from mockapi import Handler, MockServer, _last_user_text, _redact, read_log, write_scenario  # noqa: E402

KEY_A, KEY_B = "sk-mock-A", "sk-mock-B"
CONTINUE_MARK = "previous session stopped at a usage limit"


class Fail(Exception):
    pass


def check(cond: bool, what: str, results: list) -> None:
    results.append({"check": what, "ok": bool(cond)})
    if not cond:
        raise Fail(what)


class World:
    def __init__(self, root: str, baton: str, baton_test: str | None = None):
        self.run = Run(root)
        self.baton = os.path.abspath(baton)
        # The window's test build (`gui --no-window`), next to baton.exe unless given.
        self.baton_test = os.path.abspath(baton_test or os.path.join(os.path.dirname(self.baton), "baton-test.exe"))
        self.home = os.path.join(self.run.root, "baton-home")
        os.makedirs(self.home, exist_ok=True)
        self.scen = os.path.join(self.run.root, "scenario.json")
        self.notes_file = os.path.join(self.run.logs, "notifications.jsonl")
        write_scenario(self.scen, {KEY_A: {"mode": "echo"}, KEY_B: {"mode": "echo"}})
        self.mock_log = os.path.join(self.run.logs, "mock.jsonl")
        self.srv = MockServer(self.mock_log, self.scen)
        # Every other mode is the harness's own; "office" is scripted in this file.
        self.srv.httpd.RequestHandlerClass = type("BoundOfficeHandler", (OfficeHandler,), {"state": self.srv.state})
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
        return self.run.env(None, {"BATON_HOME": self.home, "BATON_NOTIFY_FILE": self.notes_file})

    def notes(self, run_dir: str) -> list[dict]:
        """The notifications a run would have shown."""
        return [n for n in read_jsonl(self.notes_file) if n.get("group") == os.path.basename(run_dir)]

    def run_log(self, run_dir: str) -> str:
        try:
            with open(os.path.join(run_dir, "baton.log"), encoding="utf-8", errors="replace") as f:
                return f.read()
        except OSError:
            return ""

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

    def run_dirs(self) -> set[str]:
        return set(glob.glob(os.path.join(self.home, "runs", "*")))

    def wait_session_starts(self, n: int, timeout: float, old: set[str] | None = None) -> str | None:
        """Wait until the newest run has n SessionStart events; return its dir.
        old: run dirs from before the session was started, never the answer
        (baton makes the new run's dir a moment after it starts)."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            d = self.newest_run_dir()
            if d and d not in (old or set()) and sum(1 for e in self.events(d) if e.get("event") == "SessionStart") >= n:
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
    notes = w.notes(run_dir)
    check([n["tag"] for n in notes] == ["switched"] and "acct-a" in notes[0]["body"] and "acct-b" in notes[0]["body"],
          f"auto: one notification says the session went on from acct-a on acct-b ({notes})", res)
    check(bool(journal.get("console_window")), "auto: the run records its console window", res)
    log = w.run_log(run_dir)
    check("keep awake: true" in log and log.rfind("keep awake: false") > log.find("keep awake: true"),
          "auto: the PC is kept awake while the session switches, and let go after", res)
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
    notes = w.notes(w.newest_run_dir())
    check([n["tag"] for n in notes] == ["asking"] and "acct-b" in notes[0]["body"],
          f"confirm: a notification asks for an answer in the session window, and none follows the answer ({notes})", res)
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
    notes = w.notes(w.newest_run_dir())
    check([n["tag"] for n in notes] == ["switched", "limited"],
          f"exhausted: notifications say it switched, then that every account is limited, and nothing more ({notes})", res)
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
    notes = w.notes(w.newest_run_dir())
    check([n["tag"] for n in notes] == ["problem"] and "acct-a" in notes[0]["body"],
          f"billing: a notification says acct-a has a problem ({notes})", res)
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
                   "entrypoint": "claude-desktop", "startedAt": process_started_ms(os.getpid())}, f)
    try:
        listed = w.baton_cmd("sessions")
        check(sid in listed.stdout and "desktop work" in listed.stdout and "open in the Claude desktop app" in listed.stdout,
              f"import: baton sessions lists it, named and open in the desktop app ({listed.stdout[:300]!r})", res)
        refused = w.baton_cmd("resume", sid)
        check(refused.returncode == 1 and "the Claude desktop app" in refused.stderr and "--copy" in refused.stderr,
              f"import: resume without --copy refuses an open conversation and says where ({refused.stderr.strip()[:200]!r})", res)
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


def scenario_statusline(w: World, res: list) -> dict:
    """baton's status line (`baton statusline`) runs in a real session and shows the account.
    The mock accounts sign in with API keys, so Claude Code sends no plan usage (it does only
    for a Claude subscription); the unit tests cover recording it. A profile that has its own
    status line keeps it."""
    pc = start_on_a_and_answer(w, res, "statusline", "sl one")
    try:
        d = w.newest_run_dir()
        with open(os.path.join(d, "settings.json"), encoding="utf-8") as f:
            sl = (json.load(f).get("statusLine") or {}).get("command", "")
        check(sl.endswith(" statusline") and "\\" not in sl, f"statusline: baton's status line is set, with forward slashes ({sl!r})", res)
        check(pc.wait_for(r"baton\W{1,6}acct-a", 15) is not None, "statusline: the session shows 'baton · acct-a' under the prompt", res)
        try:
            with open(os.path.join(d, "status.json"), encoding="utf-8") as f:
                model = json.load(f).get("model", "")
        except (OSError, ValueError):
            model = ""
        check(bool(model), f"statusline: the run's status.json has the model ({model!r})", res)
        check(quit_claude(pc) == 0, "statusline: first run exits", res)
    finally:
        pc.close()
    sp = os.path.join(w.a.dir, "settings.json")
    with open(sp, encoding="utf-8") as f:
        before = f.read()
    own = json.loads(before)
    own["statusLine"] = {"type": "command", "command": "echo MY-OWN-LINE"}
    with open(sp, "w", encoding="utf-8") as f:
        json.dump(own, f)
    try:
        pc = start_on_a_and_answer(w, res, "statusline-own", "sl two")
        try:
            with open(os.path.join(w.newest_run_dir(), "settings.json"), encoding="utf-8") as f:
                check("statusLine" not in json.load(f), "statusline: a profile's own status line is not replaced", res)
            check(pc.wait_for("MY-OWN-LINE", 15) is not None, "statusline: the profile's own status line shows", res)
            check(quit_claude(pc) == 0, "statusline: second run exits", res)
        finally:
            pc.close()
    finally:
        with open(sp, "w", encoding="utf-8") as f:
            f.write(before)
    return {"command": sl}


def scenario_remotectl(w: World, res: list) -> dict:
    """`baton run -- --rc` (what the window's Remote Control option passes): the mock accounts
    sign in with API keys, where Remote Control is not available, so this checks that the session
    still works, that the run records Remote Control, and that --rc survives a switch."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto")
    pc = w.start("remotectl", ["--", "--rc"])
    try:
        submit_when_ready(w, pc, 1, "rc one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "rc one"), 60) is not None, "remotectl: A answers with --rc", res)
        d = w.newest_run_dir()
        journal = json.load(open(os.path.join(d, "run.json"), encoding="utf-8"))
        check(journal.get("remote_control") is True, "remotectl: the run records Remote Control", res)
        time.sleep(1.5)
        w.accounts_flip("limit", "echo")
        mark = len(w.mock_rows())
        submit(w, pc, "rc two hits the limit")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark) is not None,
              "remotectl: the session switches to B", res)
        log = open(os.path.join(d, "baton.log"), encoding="utf-8", errors="replace").read()
        launches = [l for l in log.splitlines() if "launch account=" in l]
        rc = [l for l in launches if '"--rc"' in l]
        check(len(launches) >= 2 and len(rc) == len(launches),
              f"remotectl: --rc is passed on every launch, the switch included ({len(rc)}/{len(launches)})", res)
        time.sleep(4.0)
        check(quit_claude(pc) == 0, "remotectl: baton exits 0", res)
    finally:
        pc.close()
    return {}


def scenario_remotetwo(w: World, res: list) -> dict:
    """Two sessions in the folder at once, Remote Control on A only (B starts on the other
    account, so A's limit leaves it alone): only A's launches carry --rc, its switch included,
    only A's run records it, and the window's state marks only A's row."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto")
    old = w.run_dirs()
    pa = w.start("remotetwo-a", ["--", "--rc"])
    pb = None
    try:
        da = w.wait_session_starts(1, 60, old=old)
        if not da:
            raise Fail("remotetwo: A did not start")
        time.sleep(3.0)
        submit(w, pa, "rt a one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "rt a one"), 60) is not None, "remotetwo: A answers", res)
        pb = w.start("remotetwo-b", ["--account", "acct-b"])
        db = w.wait_session_starts(1, 60, old=old | {da})
        if not db:
            raise Fail("remotetwo: B did not start")
        time.sleep(3.0)
        submit(w, pb, "rt b one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, "rt b one"), 60) is not None, "remotetwo: B answers", res)
        ja, jb = (json.load(open(os.path.join(d, "run.json"), encoding="utf-8")) for d in (da, db))
        check(ja.get("remote_control") is True and not jb.get("remote_control"),
              f"remotetwo: only A's run records Remote Control (A {ja.get('remote_control')}, B {jb.get('remote_control')})", res)
        win = BatonWindow(w)
        try:
            status, st = win.call("GET", "state")
            rc = {r.get("run_id"): r.get("remote_control") for r in st.get("runs") or []}
            ida, idb = os.path.basename(da), os.path.basename(db)
            check(status == 200 and rc.get(ida) is True and rc.get(idb) is False,
                  f"remotetwo: the window marks only A's row ({rc})", res)
        finally:
            win.close()
        w.accounts_flip("limit", "echo")
        mark = len(w.mock_rows())
        submit(w, pa, "rt a two hits the limit")
        check(w.wait_mock(lambda r: main_turn(r, KEY_B, CONTINUE_MARK), 90, since=mark) is not None,
              "remotetwo: A switches to the other account", res)
        time.sleep(2.0)
        la = [l for l in w.run_log(da).splitlines() if "launch account=" in l]
        lb = [l for l in w.run_log(db).splitlines() if "launch account=" in l]
        check(len(la) >= 2 and all('"--rc"' in l for l in la), f"remotetwo: every launch of A carries --rc ({len(la)})", res)
        check(len(lb) >= 1 and not any("--rc" in l or "--remote-control" in l for l in lb),
              f"remotetwo: no launch of B carries it ({len(lb)})", res)
        w.accounts_flip("echo", "echo")
        time.sleep(3.0)
        check(quit_claude(pa) == 0, "remotetwo: A's baton exits 0", res)
        check(quit_claude(pb) == 0, "remotetwo: B's baton exits 0", res)
    finally:
        pa.close()
        if pb:
            pb.close()
    return {}


def scenario_busytrust(w: World, res: list) -> dict:
    """A second session started from the window on Automatic while a session runs on the top
    account, in a folder that account never trusted: the window leaves the choice to baton (no
    other account), the trust the user gives reaches the profile in use, the new session starts
    there without Claude Code's trust question, and the running claude keeps that trust when it
    saves its own settings on exit."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto")
    new = os.path.join(w.run.root, "busytrust-proj")
    os.makedirs(new, exist_ok=True)
    cj = os.path.join(w.a.dir, ".claude.json")

    def project(folder: str) -> dict:
        try:
            with open(cj, encoding="utf-8") as f:
                return (json.load(f).get("projects") or {}).get(fwd(folder)) or {}
        except (OSError, ValueError):
            return {}

    if project(new).get("hasTrustDialogAccepted"):
        raise Fail("busytrust: acct-a already trusts the new folder")
    old = w.run_dirs()
    pa = w.start("busytrust-a")
    pn = None
    try:
        da = w.wait_session_starts(1, 60, old=old)
        if not da:
            raise Fail("busytrust: the first session did not start")
        time.sleep(3.0)
        submit(w, pa, "bt first one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "bt first one"), 60) is not None,
              "busytrust: the first session runs on acct-a", res)
        acc = os.path.join(w.home, "accounts.json")
        with open(acc, encoding="utf-8") as f:
            keep = f.read()
        win = BatonWindow(w, test_spawn=True)
        try:
            status, info = win.call("GET", "folder/info?path=" + urllib.parse.quote(new))
            check(status == 200 and info.get("trusted") is False, f"busytrust: the window asks to trust the new folder ({info})", res)
            status, body = win.call("POST", "sessions/start", {"folder": new, "account": "", "mode": "new", "trust": True})
            check(status == 202, f"busytrust: the window starts the session ({status} {body})", res)
            line = win.spawned() or ""
            check('["run"]' in line, f"busytrust: Automatic leaves the choice to baton, no other account ({line})", res)
        finally:
            win.close()
        # The window's sign-in check may mark the mock accounts signed out.
        with open(acc, encoding="utf-8") as f:
            if f.read() != keep:
                with open(acc, "w", encoding="utf-8") as g:
                    g.write(keep)
        check(project(new).get("hasTrustDialogAccepted") is True, "busytrust: the trust reached acct-a's profile while it is in use", res)
        old2 = w.run_dirs()
        pn = w.start("busytrust-new", cwd=new)
        dn = w.wait_session_starts(1, 60, old=old2)
        check(dn is not None and pn.wait_for("trust this folder", 1) is None,
              "busytrust: the second session starts without Claude Code's trust question", res)
        time.sleep(3.0)
        submit(w, pn, "bt second one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "bt second one"), 60) is not None,
              "busytrust: the second session runs on acct-a too", res)
        sid_a = json.load(open(os.path.join(da, "run.json"), encoding="utf-8"))["session_id"]
        mark = len(w.mock_rows())
        submit(w, pa, "bt first two")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "bt first two"), 60, since=mark) is not None,
              "busytrust: the first session goes on", res)
        time.sleep(2.0)
        check(quit_claude(pa) == 0, "busytrust: the first session's baton exits 0", res)
        time.sleep(1.5)
        mine = project(w.run.proj)
        check(mine.get("lastSessionId") == sid_a, f"busytrust: the first claude saved its settings on exit ({mine.get('lastSessionId')} vs {sid_a})", res)
        check(project(new).get("hasTrustDialogAccepted") is True and mine.get("hasTrustDialogAccepted") is True,
              "busytrust: acct-a still trusts both folders after that save", res)
        check(quit_claude(pn) == 0, "busytrust: the second session's baton exits 0", res)
        time.sleep(1.5)
        check(project(new).get("hasTrustDialogAccepted") is True, "busytrust: and after the second one exits", res)
    finally:
        pa.close()
        if pn:
            pn.close()
    return {}


def scenario_rules(w: World, res: list) -> dict:
    """A rule for the folder that holds the project allows only acct-b: the
    session starts there although acct-a comes first, `baton switch acct-a`
    is refused, a limit does not move it to acct-a (every allowed account is
    limited, so it stops), and `--account acct-a` is refused."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    parent = os.path.dirname(w.run.proj)  # a rule covers the folders under it
    w.config(switch_mode="auto", on_exhausted="stop", folder_rules=[{"folder": parent, "accounts": ["acct-b"]}])
    mark = len(w.mock_rows())
    try:
        cp = w.baton_cmd("rules")
        check(parent in cp.stdout and "acct-b" in cp.stdout, f"rules: `baton rules` lists the rule ({cp.stdout.strip()})", res)
        pc = w.start("rules")
        try:
            submit_when_ready(w, pc, 1, "ru one")
            check(w.wait_mock(lambda r: main_turn(r, KEY_B, "ru one"), 60) is not None,
                  "rules: the session starts on acct-b, the only account the rule allows", res)
            time.sleep(1.5)
            cp = w.baton_cmd("switch", "acct-a")
            check(cp.returncode != 0 and "rule" in cp.stderr, f"rules: `baton switch acct-a` is refused ({cp.stderr.strip()})", res)
            w.accounts_flip("echo", "limit")
            mark_text = pc.mark()
            submit(w, pc, "ru two hits the limit")
            check(baton_noticed(w, pc, r"every account is limited", 90, since=mark_text),
                  "rules: at the limit baton does not look past the rule's accounts", res)
            code = pc.wait_exit(30)
            check(code == 0, f"rules: baton stops cleanly ({code})", res)
        finally:
            pc.close()
        run_dir = w.newest_run_dir()
        check("folder rule" in w.run_log(run_dir), "rules: the session says which rule it keeps to", res)
        check([n["tag"] for n in w.notes(run_dir)] == ["limited"], f"rules: no notification of a switch ({w.notes(run_dir)})", res)
        pc = w.start("rules-account", ["--account", "acct-a"])
        try:
            check(baton_noticed(w, pc, r"rule allows only", 30), "rules: `--account acct-a` is refused in that folder", res)
            code = pc.wait_exit(30)
            check(code not in (0, None), f"rules: ... and baton exits with an error ({code})", res)
        finally:
            pc.close()
        check(not any(acct(r, KEY_A) for r in w.mock_rows()[mark:]) and no_switch_logged(w),
              "rules: acct-a was never used and nothing switched", res)
    finally:
        w.config(folder_rules=[])
    return {}


def scenario_restore(w: World, res: list) -> dict:
    """baton and Claude Code are killed in the middle of a response (as by a
    power cut). `baton status` lists the run as cut off and working;
    `baton run --restore <run>` reopens the same conversation on its account
    and goes on with the task without any typing. Once reopened, the run is no
    longer offered."""
    w.reset_state()
    w.accounts_flip("echo", "echo")
    w.config(switch_mode="auto", on_exhausted="stop")
    pc = w.start("restore")
    try:
        submit_when_ready(w, pc, 1, "re one")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "re one"), 60) is not None, "restore: A answers", res)
        time.sleep(1.5)
        write_scenario(w.scen, {KEY_A: {"mode": "slow", "seconds": 40}, KEY_B: {"mode": "echo"}})
        submit(w, pc, "re two runs long")
        run_dir = w.newest_run_dir()
        journal = os.path.join(run_dir, "run.json")
        deadline, owed = time.time() + 20, False
        while time.time() < deadline and not owed:
            time.sleep(0.5)
            try:
                owed = bool(json.load(open(journal, encoding="utf-8")).get("owed"))
            except (OSError, ValueError):
                pass
        check(owed, "restore: the run records that it has work going", res)
        kill = subprocess.run(["taskkill", "/F", "/T", "/PID", str(pc.pid)], capture_output=True)
        said = (kill.stdout or b"").decode("mbcs", "replace") + (kill.stderr or b"").decode("mbcs", "replace")
        check(kill.returncode == 0, f"restore: baton and Claude Code are killed ({said.strip()})", res)
        time.sleep(1.0)
    finally:
        pc.close()
    old = json.load(open(journal, encoding="utf-8"))
    run_id = os.path.basename(run_dir)
    check(old["state"] != "done", f"restore: the killed run never said it was done ({old['state']})", res)
    st = w.baton_cmd("status")
    check(run_id in st.stdout and "was working" in st.stdout, f"restore: baton status lists it as cut off while working ({st.stdout.strip()[-300:]})", res)

    w.accounts_flip("echo", "echo")
    mark = len(w.mock_rows())
    pc = w.start("restore-again", ["--restore", run_id])
    try:
        first = w.wait_mock(lambda r: main_turn(r, KEY_A, "restarted while you were working"), 90, since=mark)
        check(first is not None, "restore: reopened on acct-a, it goes on with the task by itself", res)
        check(first is not None and first["n_messages"] >= 4,
              f"restore: ... with the whole conversation (n_messages={first and first['n_messages']})", res)
        time.sleep(4.0)
        submit(w, pc, "re three")
        check(w.wait_mock(lambda r: main_turn(r, KEY_A, "re three"), 60, since=mark) is not None, "restore: the reopened session is usable", res)
        time.sleep(1.0)
        check(quit_claude(pc) == 0, "restore: baton exits 0", res)
    finally:
        pc.close()
    new_dir = w.newest_run_dir()
    new = json.load(open(os.path.join(new_dir, "run.json"), encoding="utf-8"))
    check(new_dir != run_dir and new.get("session_id") == old.get("session_id"),
          f"restore: a new run with the same conversation ({new.get('session_id')} / {old.get('session_id')})", res)
    check(json.load(open(journal, encoding="utf-8")).get("restored_by") == os.path.basename(new_dir),
          "restore: the old run says which run reopened it", res)
    st = w.baton_cmd("status")
    check(run_id not in st.stdout, "restore: once reopened it is no longer offered", res)
    return {}


# ---- the office: a scripted turn, watched through the window's office API -----------

OFFICE_GO, OFFICE_SUB, OFFICE_DONE = "OFFICE-GO", "OFFICE-SUB", "OFFICE-DONE"
_office_ids = itertools.count(1)


def office_script(sc: dict, who: str, offered: list[str]) -> list[dict]:
    """The replies of the mock's "office" mode, one per request: a tool call
    (tool + input) or a text. "delay" holds a reply back, so a step lasts long
    enough for the office to show it; "thinking" puts a thinking block first;
    "wake" answers the subagent's task notification. sc["thinking"] (text) and
    sc["ask"] (a question the test answers) add the optional steps."""
    readme = os.path.join(sc["proj"], "README.md")
    if who == "sub":
        # Subagents of the desktop app hand their report back with a tool; the
        # CLI's Explore agent has none, and its last text is the report.
        last = ({"tool": "SubagentHandback", "input": {"message": OFFICE_SUB + "-DONE"}, "delay": 1.5}
                if "SubagentHandback" in offered else {"text": OFFICE_SUB + "-DONE", "delay": 1.5})
        return [{"tool": "Read", "input": {"file_path": readme}, "delay": 1.5}, last, {"text": OFFICE_SUB + "-DONE"}]
    woken = [{"tool": "Bash", "input": {"command": "exit 3", "description": "Fail on purpose"}}]
    if sc.get("ask"):
        woken.insert(0, {"tool": "AskUserQuestion", "input": {"questions": [{
            "question": "Which room should I go to next?", "header": "Next room", "multiSelect": False,
            "options": [{"label": "Server room", "description": "Run the last command"},
                        {"label": "Lounge", "description": "Take a break"}]}]}})
    woken[0]["wake"] = True
    return [
        {"tool": "Read", "input": {"file_path": readme}, "thinking": sc.get("thinking")},
        # ping, not sleep: Claude Code refuses a command that starts with `sleep 2`.
        {"tool": "Bash", "input": {"command": "ping -n 3 127.0.0.1 > /dev/null; echo OFFICE-CMD",
                                   "description": "Wait two seconds"}},
        {"tool": "Grep", "input": {"pattern": "Phase", "path": sc["proj"]}},
        {"tool": "Agent", "input": {"description": "office helper", "prompt": OFFICE_SUB + " read the readme",
                                    "subagent_type": "Explore"}},
        # Claude Code runs the subagent in the background: the Agent call is
        # answered at once ("launched"), so Claude ends its turn until the
        # subagent's task notification wakes it.
        {"text": "Waiting for the helper."},
        *woken,
        {"text": OFFICE_DONE},
    ]


def _last_tool_result(body: dict) -> dict | None:
    for m in reversed(body.get("messages") or []):
        if m.get("role") != "user":
            continue
        found = [b for b in (m.get("content") if isinstance(m.get("content"), list) else [])
                 if isinstance(b, dict) and b.get("type") == "tool_result"]
        return found[-1] if found else None
    return None


class OfficeHandler(Handler):
    """The mock with one more mode, "office": Claude goes through office_script.
    The mock keeps no state. Each tool call's id carries its step
    (toolu_office_<main|sub>_<step>_<n>), so the tool_result that comes back
    says which step is next, and a task notification naming the Agent call's
    id gets the "wake" step; the main script starts on a prompt with OFFICE-GO.
    A subagent's requests are told apart by their first message, the prompt
    the Agent call gave it. Requests without tools (the title) get the echo."""

    def _messages(self, account, body):
        sc = self.state.scenario_for(account)
        offered = [t.get("name") for t in (body.get("tools") or []) if isinstance(t, dict)]
        if sc.get("mode") != "office" or not offered:
            return super()._messages(account, body)
        msgs = body.get("messages") or []
        who = "sub" if msgs and OFFICE_SUB in json.dumps(msgs[0].get("content"), ensure_ascii=False) else "main"
        script = office_script(sc, who, offered)
        user_text, has_tr = _last_user_text(body)
        result = _last_tool_result(body) or {}
        m = re.match(r"toolu_office_(main|sub)_(\d+)_", result.get("tool_use_id") or "")
        last_user = next((json.dumps(x.get("content"), ensure_ascii=False) for x in reversed(msgs) if x.get("role") == "user"), "")
        if m:
            step = int(m.group(2)) + 1
        elif who == "main" and re.search(r"task-notification.*toolu_office_main_\d+_", last_user, re.S):
            step = next(i for i, s in enumerate(script) if s.get("wake"))
        elif who == "sub" or OFFICE_GO.lower() in user_text.lower():
            step = 0
        else:
            return super()._messages(account, body)  # e.g. a prompt after the script
        reply = script[step] if step < len(script) else {"text": "OFFICE-END"}
        n, _ = self.state.next_n(account)
        tool_id = f"toolu_office_{who}_{step}_{next(_office_ids)}"
        self.state.log({
            "ts": time.time(), "n": n, "method": "POST", "path": self.path, "account": _redact(account),
            "mode": "office", "model": body.get("model"), "stream": bool(body.get("stream")), "main_loop": True,
            "n_messages": len(msgs), "n_tools": len(offered), "has_tool_result": has_tr,
            "user_text": " ".join(user_text.split())[:200], "status": 200, "office_step": f"{who}:{step}",
            "result_of": result.get("tool_use_id"), "result_error": result.get("is_error"),
            "reply": "tool_use" if "tool" in reply else "text", "tool": reply.get("tool"),
            "text": reply.get("text"), "offered": offered,
        })
        blocks = []
        if reply.get("thinking"):
            blocks.append({"type": "thinking", "thinking": reply["thinking"], "signature": "mock-signature"})
        if "tool" in reply:
            blocks.append({"type": "tool_use", "id": tool_id, "name": reply["tool"], "input": reply["input"]})
        else:
            blocks.append({"type": "text", "text": reply["text"]})
        self._blocks(body.get("model") or "claude-mock", bool(body.get("stream")), blocks,
                     "tool_use" if "tool" in reply else "end_turn", float(reply.get("delay") or 0))

    def _blocks(self, model: str, stream: bool, blocks: list[dict], stop: str, delay: float) -> None:
        """One reply of several content blocks (the harness's _reply sends one)."""
        msg_id = f"msg_office_{next(_office_ids)}"
        usage = {"input_tokens": 12, "output_tokens": 1, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
        if not stream:
            time.sleep(delay)
            self._send_json(200, {"id": msg_id, "type": "message", "role": "assistant", "model": model,
                                  "content": blocks, "stop_reason": stop, "stop_sequence": None,
                                  "usage": {**usage, "output_tokens": 20}})
            return
        try:
            self._sse_start()
            self._event("message_start", {"type": "message_start", "message": {
                "id": msg_id, "type": "message", "role": "assistant", "model": model,
                "content": [], "stop_reason": None, "stop_sequence": None, "usage": usage}})
            time.sleep(delay)
            for i, b in enumerate(blocks):
                if b["type"] == "thinking":
                    start = {"type": "thinking", "thinking": "", "signature": ""}
                    deltas = [{"type": "thinking_delta", "thinking": b["thinking"]},
                              {"type": "signature_delta", "signature": b["signature"]}]
                elif b["type"] == "tool_use":
                    start = {**b, "input": {}}
                    deltas = [{"type": "input_json_delta", "partial_json": json.dumps(b["input"])}]
                else:
                    start = {"type": "text", "text": ""}
                    deltas = [{"type": "text_delta", "text": b["text"]}]
                self._event("content_block_start", {"type": "content_block_start", "index": i, "content_block": start})
                for d in deltas:
                    self._event("content_block_delta", {"type": "content_block_delta", "index": i, "delta": d})
                self._event("content_block_stop", {"type": "content_block_stop", "index": i})
            self._event("message_delta", {"type": "message_delta", "delta": {"stop_reason": stop, "stop_sequence": None},
                                          "usage": {"output_tokens": 20}})
            self._event("message_stop", {"type": "message_stop"})
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            self.state.log({"ts": time.time(), "event": "client_disconnected_mid_stream", "msg": msg_id})


class BatonWindow:
    """The baton window's server, from the test build: `gui --no-window` serves
    the page and prints its address instead of opening a window. Calls go in
    as the page makes them: the address's one-time code buys a session token,
    and every API call carries it with a page id and a request number."""

    def __init__(self, w: World, test_spawn: bool = False):
        """test_spawn: session starts are printed ("test-spawn: [args] in dir",
        read with spawned()) instead of opening console windows."""
        if not os.path.isfile(w.baton_test):
            raise Fail(f"no test build at {w.baton_test}: go build -tags batontest -o {w.baton_test} ./cmd/baton")
        self.err = open(os.path.join(w.run.logs, "office-gui.log"), "wb")
        self.proc = subprocess.Popen([w.baton_test, "gui", "--no-window", *(["--test-spawn"] if test_spawn else [])],
                                     env=w.env(), cwd=w.run.root,
                                     stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=self.err)
        self.lines = lines = queue.Queue()
        threading.Thread(target=lambda: [lines.put(line) for line in self.proc.stdout], daemon=True).start()
        self.page, self.seq, self.token = os.urandom(8).hex(), itertools.count(1), ""
        try:
            url = urllib.parse.urlsplit(lines.get(timeout=30).decode("utf-8", "replace").strip())
            self.port, code = url.port, urllib.parse.parse_qs(url.query)["k"][0]
        except (queue.Empty, KeyError, ValueError):
            self.close()
            raise Fail("office: the window's server printed no address (one GUI per BATON_HOME: is another running?)")
        status, body = self.call("POST", "session", {"code": code})
        if status != 200 or not body.get("token"):
            self.close()
            raise Fail(f"office: the one-time code was not taken ({status} {body})")
        self.token = body["token"]

    def spawned(self, timeout: float = 20) -> str | None:
        """The next session start the test build printed instead of opening it."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                line = self.lines.get(timeout=max(0.1, deadline - time.time())).decode("utf-8", "replace").strip()
            except queue.Empty:
                return None
            if line.startswith("test-spawn:"):
                return line
        return None

    def call(self, method: str, path: str, body: dict | None = None, timeout: float = 20) -> tuple[int, dict]:
        headers = {"X-Baton-Page": self.page, "X-Baton-Seq": str(next(self.seq))}
        if self.token:
            headers["X-Baton-Token"] = self.token
        data = None
        if body is not None:
            data, headers["Content-Type"] = json.dumps(body).encode("utf-8"), "application/json"
        # http.client sends Host: 127.0.0.1:<port>, the only host the server answers.
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=timeout)
        try:
            conn.request(method, "/api/" + path, body=data, headers=headers)
            r = conn.getresponse()
            raw = r.read()
        finally:
            conn.close()
        try:
            return r.status, (json.loads(raw) if raw else {})
        except ValueError:
            return r.status, {"raw": raw[:200].decode("utf-8", "replace")}

    def close(self) -> None:
        """Say bye as a closing page does; the server then ends by itself (3 s
        later). Otherwise end it, and the hidden commands it started, by pid."""
        if getattr(self, "token", "") and self.proc.poll() is None:
            try:
                self.call("POST", "bye", {}, timeout=5)
            except OSError:
                pass
        try:
            self.proc.wait(10)
        except subprocess.TimeoutExpired:
            subprocess.run(["taskkill", "/F", "/T", "/PID", str(self.proc.pid)], capture_output=True)
            self.proc.wait(5)
        self.err.close()


class OfficeWatch(threading.Thread):
    """Long-polls GET /api/office for one session as the office page does, and
    keeps every event it is sent (by seq) and the newest answer. It stops when
    done(events) says the story is complete, at its deadline, or when the
    server answers 404 office_off."""

    def __init__(self, win: BatonWindow, session: str, log_path: str, done, timeout: float):
        super().__init__(daemon=True, name="office-watch")
        self.win, self.session, self.log_path, self.done = win, session, log_path, done
        self.deadline = time.time() + timeout
        self.lock = threading.Lock()
        self.by_seq: dict[int, dict] = {}
        self.last: dict | None = None
        self.off: dict | None = None
        self.answers, self.resets, self.errors, self.times = 0, 0, [], []

    def events(self) -> list[dict]:
        with self.lock:
            return [self.by_seq[k] for k in sorted(self.by_seq)]

    def run(self) -> None:
        since = 0
        with open(self.log_path, "a", encoding="utf-8") as log:
            while time.time() < self.deadline:
                q = urllib.parse.urlencode({"session": self.session, "since": since, "wait": 1})
                try:
                    status, body = self.win.call("GET", "office?" + q, timeout=30)
                except OSError as e:
                    status, body = 0, {"error": repr(e)}
                view = body.get("view") or {}
                log.write(json.dumps({"t": round(time.time(), 3), "since": since, "status": status,
                                      "code": body.get("code"), "seq": body.get("seq"), "reset": body.get("reset"),
                                      "events": body.get("events"), "view": {k: v for k, v in view.items() if k != "actors"},
                                      "actors": view.get("actors"),
                                      "floors": [(f.get("floor"), f.get("path")) for f in (body.get("building") or {}).get("floors") or []]},
                                     ensure_ascii=False) + "\n")
                log.flush()
                if status == 404 and body.get("code") == "office_off":
                    self.off = body
                    return
                if status != 200:
                    self.errors.append(f"{status} {body}"[:200])
                    time.sleep(0.5)
                    continue
                with self.lock:
                    self.last = body
                    self.answers += 1
                    self.times.append(time.time())
                    self.resets += bool(body.get("reset"))
                    for e in body.get("events") or []:
                        self.by_seq.setdefault(e.get("seq"), e)
                since = max(since, int(body.get("seq") or 0))
                if self.done(self.events()):
                    return


def office_story(events: list[dict], sc: dict) -> dict:
    """Finds the scripted turn (office_script with sc) in the office's events:
    the steps below in this order (others may come between), the subagent's
    report or leaving after its Read, Claude's closing words after the
    failure, the Agent call, the thinking and the wait for the user's answer."""
    def ev(kind: str, actor: str | None = None, **want):
        def pred(e: dict) -> bool:
            if e.get("kind") != kind or any(e.get(k) != v for k, v in want.items()):
                return False
            return (actor is None or (actor == "main" and e.get("actor") == "main")
                    or (actor == "agent" and e.get("actor") not in (None, "", "main", "me")))
        return pred

    steps = [
        ("Claude reads in the library", ev("tool", "main", tool="Read", room="library")),
        ("Claude runs a command in the server room", ev("tool", "main", tool="Bash", room="server")),
        ("Claude searches in the library", ev("tool", "main", tool="Grep", room="library")),
        ("an Explore subagent is hired at the elevator", ev("hire", type="Explore", room="elevator")),
        ("the subagent reads in the library", ev("tool", "agent", tool="Read", room="library")),
        ("Claude runs the failing command in the server room", ev("tool", "main", tool="Bash", room="server")),
        ("the command fails", ev("fail", "main")),
        ("Claude goes idle", ev("idle", "main")),
    ]
    if sc.get("ask"):
        steps.insert(5, ("Claude comes to my desk to ask", ev("tool", "main", tool="AskUserQuestion", room="desk")))
    out: dict = {"found": {}, "missing": None}
    i = 0
    for label, pred in steps:
        while i < len(events) and not pred(events[i]):
            i += 1
        if i == len(events):
            out["missing"] = label
            break
        out["found"][label] = (i, events[i])
        i += 1
    after = lambda label: out["found"][label][0] if label in out["found"] else len(events)  # noqa: E731
    sub = out["found"].get("the subagent reads in the library")
    out["report"] = next((e for e in events[after("the subagent reads in the library"):]
                          if sub and e.get("kind") in ("report", "leave") and e.get("actor") == sub[1].get("actor")), None)
    out["say"] = next((e for e in events[after("the command fails"):]
                       if e.get("kind") == "say" and e.get("actor") == "main" and OFFICE_DONE in (e.get("detail") or "")), None)
    out["agent_tool"] = next((e for e in events[after("Claude searches in the library"):]
                              if e.get("kind") == "tool" and e.get("actor") == "main" and e.get("tool") == "Agent"), None)
    out["think"] = next((e for e in events[:after("Claude reads in the library")] if e.get("kind") == "think"
                         and e.get("actor") == "main" and sc.get("thinking") and sc["thinking"] in (e.get("detail") or "")), None)
    # The registry's "waiting" is read about once a second, so the ask event
    # may come just before or after the tool's own event.
    out["ask"] = next((e for e in events[after("the subagent reads in the library"):after("the command fails")]
                       if e.get("kind") == "ask" and e.get("actor") == "main"), None)
    out["complete"] = (out["missing"] is None and out["report"] is not None and out["say"] is not None
                       and (out["ask"] is not None or not sc.get("ask")))
    return out


def short_events(events: list[dict]) -> str:
    def one(e: dict) -> str:
        actor = e.get("actor") or "?"
        return ":".join(str(x) for x in ("main" if actor == "main" else actor[:6], e.get("kind"), e.get("tool") or e.get("type"),
                                         e.get("room")) if x)
    return " ".join(one(e) for e in events)[-1500:]


def tool_uses(path: str) -> list[dict]:
    return [b for r in read_jsonl(path) if r.get("type") == "assistant"
            for b in ((r.get("message") or {}).get("content") or []) if isinstance(b, dict) and b.get("type") == "tool_use"]


def tool_results(path: str) -> dict[str, dict]:
    return {b.get("tool_use_id"): b for r in read_jsonl(path) if r.get("type") == "user"
            for b in ((r.get("message") or {}).get("content") or []) if isinstance(b, dict) and b.get("type") == "tool_result"}


def office_checks(w: World, res: list, watch: OfficeWatch, sc: dict, sid: str, run_id: str) -> dict:
    """The office API's side of scenario_office, from what the watch collected."""
    # The most answers within one second: a long-poll answers on news (the
    # office ticks every 250 ms) or after 15 s, never in a tight loop.
    busiest = max((sum(1 for u in watch.times if t <= u < t + 1) for t in watch.times), default=0)
    out = {"office_api": "checked", "office_answers": watch.answers, "office_resets": watch.resets,
           "office_busiest_second": busiest, "office_errors": watch.errors[:5]}
    check(watch.off is None, f"office: the window's server answers while office is on ({watch.off})", res)
    evs = watch.events()
    story = office_story(evs, sc)
    view = (watch.last or {}).get("view") or {}
    check(view.get("id") == sid and view.get("run_id") == run_id and view.get("source") == "baton"
          and view.get("account") == "acct-a",
          "office: the view is this baton session on acct-a "
          f"(id={view.get('id')}, run_id={view.get('run_id')}, source={view.get('source')}, account={view.get('account')})", res)
    floors = ((watch.last or {}).get("building") or {}).get("floors") or []
    paths = [os.path.normcase(os.path.normpath(f.get("path") or "")) for f in floors]
    check(os.path.normcase(os.path.normpath(w.run.proj)) in paths,
          f"office: the project is a floor ({[(f.get('floor'), f.get('name')) for f in floors]})", res)
    check(story["missing"] is None,
          f"office: the turn's events come in order, in their rooms (missing: {story['missing']}; "
          f"events: {short_events(evs)})", res)
    fail_ev = story["found"]["the command fails"][1]
    check("Exit code 3" in (fail_ev.get("detail") or ""), f"office: the failure says why ({fail_ev.get('detail')!r})", res)
    found = {k: v[1].get("detail") for k, v in story["found"].items()}
    # Details are short and name files by their base name (never contents, full commands or paths).
    long = [e.get("detail") for e in evs if len(e.get("detail") or "") > 160
            or os.path.normcase(w.run.root) in os.path.normcase(e.get("detail") or "")]
    check(found["Claude reads in the library"] == "README.md" and found["Claude searches in the library"] == "Phase"
          and found["Claude runs a command in the server room"] == "Wait two seconds" and not long,
          f"office: tool details are short, with file base names only ({found}; too long or with paths: {long[:3]})", res)
    check(story["report"] is not None, f"office: the subagent reports or leaves ({short_events(evs)})", res)
    check(story["say"] is not None, "office: Claude's closing words are said after the failure", res)
    check(story["agent_tool"] is not None and story["agent_tool"].get("room") == "seat",
          f"office: Claude phones the subagent from its seat ({story['agent_tool']})", res)
    if sc.get("thinking"):
        check(story["think"] is not None, f"office: Claude's thought shows before it reads ({short_events(evs[:8])})", res)
    if sc.get("ask"):
        check(story["ask"] is not None, "office: Claude waits for the user's answer (an ask event)", res)
    out["office_events"] = len(evs)
    check(busiest <= 10, f"office: the long-poll waits for news ({busiest} answers in the busiest second, "
                         f"{watch.answers} in all, {watch.resets} resets)", res)
    return out


def scenario_office(w: World, res: list) -> dict:
    """The office (`office` on in Settings) shows Claude's work live. The mock
    walks Claude through a thought, Read, a two-second Bash, Grep, an Explore
    subagent (which reads too; Claude waits for it), a question the test
    answers, a Bash that fails (exit 3) and a closing text. The window's server
    (the test build) is long-polled for the session's office events, which
    must tell that story in order and in the right rooms, as a baton session
    on acct-a. The transcript is checked first, so a failure says whether
    Claude Code or baton went wrong."""
    t0 = time.time()
    sc = {"mode": "office", "proj": w.run.proj, "thinking": "Looking around the office", "ask": True}
    script = office_script(sc, "main", [])
    accounts_json = os.path.join(w.home, "accounts.json")
    # The window checks every account's sign-in when it starts, and rewrites accounts.json.
    with keep_file(w.a.settings_json), keep_file(accounts_json):
        with open(w.a.settings_json, encoding="utf-8") as f:
            s = json.load(f)
        s["skipDangerousModePermissionPrompt"] = True  # bypass mode: no permission dialogs
        with open(w.a.settings_json, "w", encoding="utf-8") as f:
            json.dump(s, f)
        w.reset_state()
        write_scenario(w.scen, {KEY_A: sc, KEY_B: {"mode": "echo"}})
        w.config(switch_mode="auto", on_exhausted="stop", office=True)
        win, watch, pc = BatonWindow(w), None, None
        try:
            status, st = win.call("GET", "state")
            check(status == 200 and (st.get("config") or {}).get("office") is True,
                  f"office: the window's state has the office on ({status})", res)
            deadline = time.time() + 90
            while st.get("checking") and time.time() < deadline:  # let its sign-in check finish first
                time.sleep(0.5)
                st = win.call("GET", "state")[1]
            old_runs = w.run_dirs()
            pc = w.start("office", ["--account", "acct-a", "--", "--dangerously-skip-permissions"])
            run_dir = w.wait_session_starts(1, 60, old=old_runs)
            if not run_dir:
                raise Fail("office: no SessionStart")
            run_id = os.path.basename(run_dir)
            sid = [e for e in w.events(run_dir) if e.get("event") == "SessionStart"][-1]["payload"]["session_id"]
            watch = OfficeWatch(win, sid, os.path.join(w.run.logs, "office-poll.jsonl"),
                                lambda evs: office_story(evs, sc)["complete"], 240)
            watch.start()
            time.sleep(3.0)  # TUI is ready ~2-3 s after SessionStart
            t_go = time.time()
            submit(w, pc, OFFICE_GO + " tour the office")
            ask = next((i for i, s in enumerate(script) if s.get("tool") == "AskUserQuestion"), None)
            if ask is not None:
                check(w.wait_mock(lambda r: r.get("office_step") == f"main:{ask}", 90) is not None,
                      "office: Claude asks a question once the subagent is done", res)
                waiting = wait_until(lambda: next((e.get("waitingFor") or "?" for e in w.a.sessions()
                                                   if e.get("sessionId") == sid and e.get("status") == "waiting"), None), 20)
                check(bool(waiting), f"office: claude says it waits for the user ({waiting!r})", res)
                time.sleep(2.5)  # the office reads that about once a second
                mark = len(w.mock_rows())
                for _ in range(3):
                    pc.send("\r")  # the first answer is highlighted
                    if w.wait_mock(lambda r: r.get("office_step") == f"main:{ask + 1}", 8, since=mark):
                        break
            done = w.wait_mock(lambda r: r.get("office_step") == f"main:{len(script) - 1}", 120)
            check(done is not None, "office: Claude goes through the whole script to " + OFFICE_DONE, res)
            turn_secs = round(time.time() - t_go, 1)
            turns, deadline = sum(1 for s in script if "text" in s), time.time() + 30
            while time.time() < deadline and sum(1 for e in w.events(run_dir) if e.get("event") == "Stop") < turns:
                time.sleep(0.3)
            t_done = time.time()
            watch.join(30)  # the office may lag the transcript by a moment
            watch.deadline = 0
            watch.join(35)
            office_lag = round(time.time() - t_done, 1)

            # What Claude Code did, from its own transcript.
            journal = json.load(open(os.path.join(run_dir, "run.json"), encoding="utf-8"))
            calls = tool_uses(journal["transcript_path"])
            names = [c.get("name") for c in calls]
            check(names == [s["tool"] for s in script if "tool" in s],
                  f"office: claude made the scripted tool calls in order ({names})", res)
            results = tool_results(journal["transcript_path"])
            last = results.get(calls[-1].get("id")) or {}
            check(last.get("is_error") is True and "Exit code 3" in json.dumps(last.get("content")),
                  f"office: claude reports the last Bash as failed ({str(last.get('content'))[:80]!r})", res)
            check(all(results.get(c.get("id"), {}).get("is_error") is not True for c in calls[:-1]),
                  "office: claude reports the other tool calls as done", res)
            if ask is not None:
                said = json.dumps(results.get(calls[names.index("AskUserQuestion")].get("id"), {}).get("content"))
                check("Server room" in said, f"office: the question was answered with the first choice ({said[:100]})", res)
            thinking = [b for r in read_jsonl(journal["transcript_path"]) if r.get("type") == "assistant"
                        for b in (r.get("message") or {}).get("content") or [] if b.get("type") == "thinking"]
            check(any(b.get("thinking") == sc["thinking"] for b in thinking),
                  f"office: claude keeps the mock's thinking block ({len(thinking)} blocks)", res)
            subs = glob.glob(os.path.join(w.home, "profiles", "*", "projects", "*", sid, "subagents", "agent-*.jsonl"))
            sub_names = [c.get("name") for c in tool_uses(subs[0])] if len(subs) == 1 else []
            check(len(subs) == 1 and sub_names[:1] == ["Read"],
                  f"office: claude ran one subagent, and it read ({len(subs)} files, {sub_names})", res)
            with open(subs[0][:-len(".jsonl")] + ".meta.json", encoding="utf-8") as f:
                meta = json.load(f)
            check(meta.get("agentType") == "Explore" and meta.get("toolUseId") == calls[names.index("Agent")].get("id"),
                  f"office: the subagent is the Agent call's Explore agent ({meta.get('agentType')})", res)
            check(meta.get("finished") is True, "office: claude marks the subagent finished in its meta file", res)

            # What the office made of it.
            office = office_checks(w, res, watch, sc, sid, run_id)
            office["office_lag_after_turn_seconds"] = office_lag
            time.sleep(1.0)
            check(quit_claude(pc) == 0, "office: baton exits 0", res)
        finally:
            if watch is not None:
                watch.deadline = 0
                watch.join(35)
            if pc is not None:
                pc.close()
            win.close()
            w.config(office=False)
    return {"session": sid, "run_id": run_id, "turn_seconds": turn_secs, "scenario_seconds": round(time.time() - t0, 1),
            "tool_calls": names, "subagent_tool_calls": sub_names, "gui": w.baton_test, **office}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("root")
    ap.add_argument("--baton", default=os.path.join(REPO, "bin", "baton.exe"))
    ap.add_argument("--baton-test", help="the window's test build (default: baton-test.exe next to --baton)")
    ap.add_argument("--only", default="auto,manual,confirm,exhausted,transient,overloaded,auth,billing,clear,resumeid,share,"
                                      "git,flags,confirmn,wait,writer,bypass,hooksoff,confirmc,stopping,stop,stopask,stopswitch,import,statusline,remotectl,"
                                      "remotetwo,busytrust,rules,restore,office")
    a = ap.parse_args()
    w = World(a.root, a.baton, a.baton_test)
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
