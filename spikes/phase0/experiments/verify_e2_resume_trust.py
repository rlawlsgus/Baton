"""verify_e2_resume_trust: reviewer follow-up for e2_resume C3 (not part of e2).

    python -I spikes/phase0/experiments/verify_e2_resume_trust.py <empty run_root>

  T4b  resuming profile has NOT trusted the cwd: after choosing "Yes, I trust this folder",
       is the positional `-r <path> "<prompt>"` still auto-submitted?
  T5b  interactive `claude -c` in a fresh profile that only ever resumed P by path.
Uses cc.Run for every claude process; evidence stays in the run root.
"""
from __future__ import annotations

import glob
import json
import os
import sys
import time
import uuid

EXP = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, EXP)
from e2_resume import BodyLogMock, Ctx, compact_mock, conv, exit_tui, mock_since, parse_print  # noqa: E402
from cc import read_jsonl  # noqa: E402
from mockapi import write_scenario  # noqa: E402

READY = r"auto mode on|for shortcuts"


def main(root: str) -> int:
    ctx = Ctx(root)
    run = ctx.run
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = BodyLogMock(mock_log, scen, os.path.join(run.logs, "mock_bodies.jsonl"))
    srv.start()
    rep: dict = {"run_root": run.root, "tests": {}}
    T = rep["tests"]
    try:
        A = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        S = str(uuid.uuid4())
        cp = run.run_print(A, ["-p", "--session-id", S, "turn one from A", "--output-format", "json"], timeout=90)
        P = (glob.glob(os.path.join(A.dir, "projects", "*", S + ".jsonl")) or [None])[0]
        rep["setup"] = {"rc": cp.returncode, **parse_print(cp)}
        if not P:
            rep["fatal"] = "no P"
            return finish(rep, ctx, srv)

        # T4b ------------------------------------------------------------------
        s = T["T4b_untrusted_accept_then_prompt"] = {}
        U = run.profile("U", "sk-mock-U", srv.base_url, trust=None)
        rows0 = len(read_jsonl(P))
        t0 = time.time()
        pu = run.pty(U, ["-r", P, "untrusted Y1"], name="y_U_tui_trust")
        pu.start()
        s["dialog"] = bool(pu.wait_for(r"Yes,\s*I\s*trust\s*this\s*folder", 40))
        time.sleep(1.0)
        s["mock_before_keys"] = compact_mock(mock_since(mock_log, t0))
        m = pu.mark()
        pu.send("\x1b[B")  # move selection from "No, exit" to "Yes, I trust this folder"
        time.sleep(0.6)
        s["screen_after_down"] = " ".join(pu.plain(m).split())[-300:]
        pu.send("\r")
        t_accept = time.time()
        s["reply_after_accept"] = bool(pu.wait_for(r"MOCK-REPLY\[U\]\s*#\d+:\s*untrusted", 40, since=m))
        s["secs_accept_to_reply"] = round(time.time() - t_accept, 1)
        time.sleep(1.5)
        s["screen_after_accept"] = " ".join(pu.plain(m).split())[-700:]
        s["mock_after"] = compact_mock(mock_since(mock_log, t0))
        if pu.is_alive():
            s.update(exit_tui(pu, "T4b", ctx))
        else:
            s["exit_code"] = pu.wait_exit(1)
            pu.close()
        s["P_rows_before_after"] = [rows0, len(read_jsonl(P))]
        s["P_tail_conv"] = [{k: r[k] for k in ("type", "sid", "text")} for r in conv(P)][-2:]
        s["U_trust_after"] = {k[-30:]: v.get("hasTrustDialogAccepted")
                              for k, v in (U.read_claude_json().get("projects") or {}).items()}

        # T5b ------------------------------------------------------------------
        s = T["T5b_fresh_profile_tui_continue"] = {}
        C = run.profile("C", "sk-mock-C", srv.base_url, trust=[run.proj])
        cp = run.run_print(C, ["-p", "-r", P, "c resume Z1", "--output-format", "json"], timeout=90)
        s["resume"] = {"rc": cp.returncode, **parse_print(cp)}
        t0 = time.time()
        pc = run.pty(C, ["-c"], name="y_C_tui_continue")
        pc.start()
        hit = pc.wait_for(r"No conversation|not found|auto mode on|for shortcuts", 40)
        s["first_hit"] = hit.group(0) if hit else None
        time.sleep(2.5)
        s["alive"] = pc.is_alive()
        s["screen"] = " ".join(pc.plain().split())[-700:]
        s["screen_shows_history"] = "turn one from A" in pc.plain()
        if pc.is_alive():
            s.update(exit_tui(pc, "T5b", ctx))
        else:
            s["exit_code"] = pc.wait_exit(1)
            pc.close()
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["C_projects"] = sorted(ctx.rel(p) for p in glob.glob(os.path.join(C.dir, "projects", "**", "*.jsonl"),
                                                               recursive=True))
    except Exception:
        import traceback
        rep["exception"] = traceback.format_exc()[-3000:]
    return finish(rep, ctx, srv)


def finish(rep: dict, ctx: Ctx, srv) -> int:
    try:
        srv.stop()
    except Exception:
        pass
    rep["events"] = ctx.events
    with open(os.path.join(ctx.run.logs, "report.json"), "w", encoding="utf-8") as f:
        json.dump(rep, f, ensure_ascii=False, indent=1)
    print(json.dumps(rep, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
