"""verify_e7_switchloop_typelate: reviewer check for e7 claim C6(b) ("keys typed during the ~5 s switch
window are dropped").

    python -I verify_e7_switchloop_typelate.py <empty run_root>

e7 run -7 only typed in the ~1 s gap between the old claude's exit and the new claude's spawn (no
process was reading the console then). This script types at LATER points of the switch window:
  switch 1 (A->B): "zzLATE1zz" 1.5 s after the supervisor spawned B (B alive, before SessionStart)
  switch 2 (B->C): "zzLATE2zz" 0.2 s after C's SessionStart(resume) hook fired (before C's request)
After each new account's reply is on screen it waits 2 s, records whether the typed text is visible,
then submits a task and checks the next main-loop request's user text for the typed marker.
Uses e7_switchloop.py's supervisor, hook and mock subclass unchanged.
"""
from __future__ import annotations

import json
import os
import sys
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
sys.path.insert(0, HERE)
from cc import Run, read_jsonl  # noqa: E402
from conpty import PseudoConsole, strip_ansi  # noqa: E402
from mockapi import read_log, write_scenario  # noqa: E402
import e7_switchloop as e7  # noqa: E402


def main(argv: list[str]) -> int:
    root = argv[0]
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    modes = {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}, "sk-mock-C": {"mode": "echo"}}
    write_scenario(scen, modes)
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = e7.BodyMockServer(mock_log, scen, os.path.join(run.logs, "mock-bodies.jsonl"))
    srv.start()
    sup_log = os.path.join(run.logs, "sup-steps.json")
    events = os.path.join(run.logs, "events.jsonl")
    rep: dict = {"run_root": run.root}
    pc = None
    H: dict = {}

    def steps():
        try:
            return json.load(open(sup_log, encoding="utf-8")).get("steps", [])
        except (OSError, ValueError):
            return []

    def wait_resume_start(after_ts, timeout=30):
        dl = time.time() + timeout
        while time.time() < dl:
            for e in read_jsonl(events):
                if e.get("event") == "SessionStart" and e.get("ts", 0) > after_ts:
                    return e
            time.sleep(0.02)
        return None

    def do_switch(n, frm, to, task_text, marker, when, follow_task):
        r: dict = {"marker": marker, "typed_when": when}
        modes[f"sk-mock-{frm}"] = {"mode": "limit"}
        write_scenario(scen, modes)
        mk = pc.mark()
        t_submit = time.time()
        pc.submit(task_text)
        # NOTE: never read sup-steps.json while the supervisor runs: an open read handle makes its
        # os.replace() fail (WinError 5) and crashes it (first attempt of this script did exactly that).
        # The notice is written right before Popen (which takes ~0.9-1.1 s), so notice+2.5 s ~ spawn+1.5 s;
        # the exact offset to the spawn step is computed after the run.
        m, t_notice = e7.wait_text(pc, rf"\[baton\] account {frm} hit", 60, since=mk)
        r["notice_seen"] = bool(m)
        if when == "spawn+1.5":
            time.sleep(max(0.0, (t_notice or time.time()) + 2.5 - time.time()))
        else:
            ev = wait_resume_start(t_submit)
            r["session_start_seen"] = bool(ev)
            time.sleep(max(0.0, (ev["ts"] if ev else time.time()) + 0.2 - time.time()))
        t_type = time.time()
        pc.send(marker)
        r["t_type"] = t_type
        m, t_reply = e7.wait_text(pc, rf"MOCK-REPLY\[{to}\]", 90, since=mk)
        r["auto_reply"] = m.group(0) if m else None
        r["typed_before_reply_s"] = round(t_reply - t_type, 3) if t_reply else None
        ss = [e for e in read_jsonl(events) if e.get("event") == "SessionStart" and e.get("ts", 0) > t_submit]
        r["typed_rel_session_start_s"] = round(t_type - ss[0]["ts"], 3) if ss else None
        time.sleep(2.0)
        r["marker_visible_on_screen_before_next_task"] = marker in strip_ansi(pc.text[mk:])
        mk2 = pc.mark()
        t2 = time.time()
        pc.submit(follow_task)
        m, _ = e7.wait_text(pc, rf"MOCK-REPLY\[{to}\][^\n]{{0,40}}{follow_task}", 30, since=mk2)
        r["follow_reply"] = m.group(0) if m else None
        time.sleep(0.5)
        nxt = [x for x in read_log(mock_log) if x.get("method") == "POST" and x.get("ts", 0) > t2
               and x.get("main_loop")]
        r["follow_request_user_text"] = nxt[0].get("user_text") if nxt else None
        r["marker_in_follow_request"] = bool(nxt) and marker in (nxt[0].get("user_text") or "")
        r["to"] = to
        return r

    def post(r):
        """after the supervisor exited: offsets relative to the spawn step + pending-input samples."""
        st = steps()
        sp = next((s for s in st if s.get("step") == "spawned" and s.get("profile") == r["to"]), None)
        r["typed_rel_spawn_s"] = round(r["t_type"] - sp["ts"], 3) if sp else None
        r["pending_input_samples_rel_spawn"] = [
            (round(s["ts"] - sp["ts"], 3), s.get("pending_input"), (s.get("modes") or {}).get("in"))
            for s in st if s.get("step") == "mode_sample" and s.get("profile") == r["to"]][:12] if sp else None

    try:
        A = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        B = run.profile("B", "sk-mock-B", srv.base_url, trust=[run.proj])
        C = run.profile("C", "sk-mock-C", srv.base_url, trust=[run.proj])
        profs = [A, B, C]

        def hook(ev):
            return {"type": "command", "command": e7.PY, "args": ["-I", e7.HOOK, ev, events]}

        settings = {"hooks": {
            "SessionStart": [{"hooks": [hook("SessionStart")]}],
            "StopFailure": [{"matcher": "rate_limit|authentication_failed", "hooks": [hook("StopFailure")]}],
            "SessionEnd": [{"hooks": [hook("SessionEnd")]}],
        }}
        settings_path = os.path.join(run.root, "run-settings.json")
        with open(settings_path, "w", encoding="utf-8") as f:
            json.dump(settings, f, indent=1)
        cfg = {"claude": run.claude, "cwd": run.proj, "settings": settings_path, "events": events,
               "log": sup_log, "session_id": str(uuid.uuid4()), "continue_prompt": e7.CONTINUE,
               "resume_arg": "path", "idle_timeout": 10, "exit_timeout": 10,
               "profiles": [{"name": p.name, "dir": p.dir, "env": run.env(p)} for p in profs]}
        cfg_path = os.path.join(run.root, "sup-input.json")
        with open(cfg_path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, indent=1)
        pc = PseudoConsole([e7.PY, "-I", e7.SUP, cfg_path], cwd=run.proj, env=run.env(None),
                           cols=120, rows=40, log_dir=run.logs, name="sup-pty")
        pc.start()
        H["pty_start"] = pc.started_at
        m, _ = e7.wait_text(pc, r"auto mode on|for shortcuts", 60)
        rep["tui_ready"] = bool(m)
        time.sleep(1.5)
        mk = pc.mark()
        pc.submit("task one")
        m, _ = e7.wait_text(pc, r"MOCK-REPLY\[A\][^\n]{0,20}task one", 60, since=mk)
        rep["task1_reply_A"] = m.group(0) if m else None
        time.sleep(1.5)
        rep["switch1"] = do_switch(1, "A", "B", "task two (should hit the limit)", "zzLATE1zz",
                                   "spawn+1.5", "task three")
        time.sleep(1.5)
        rep["switch2"] = do_switch(2, "B", "C", "task four (B should hit the limit)", "zzLATE2zz",
                                   "session_start+0.2", "task five")
        time.sleep(1.5)
        pc.send("\x03")
        time.sleep(0.6)
        pc.send("\x03")
        rep["sup_exit_code"] = pc.wait_exit(25)
    finally:
        if pc is not None:
            pc.close()
        rep["leftover_children_killed"] = e7.kill_leftovers(sup_log, run.claude, H.get("pty_start", 0))
        srv.stop()
    for k in ("switch1", "switch2"):
        if isinstance(rep.get(k), dict) and "t_type" in rep[k]:
            post(rep[k])
    rep["mock_main_requests"] = [{k: x.get(k) for k in ("n", "account", "status", "user_text")}
                                 for x in read_log(mock_log) if x.get("method") == "POST" and x.get("main_loop")]
    print(json.dumps(rep, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
