"""verify_e2_resume_extra: adversarial follow-ups for e2_resume (reviewer script, not part of e2).

    python -I spikes/phase0/experiments/verify_e2_resume_extra.py <empty run_root>

Probes the scope of e2's claims:
  T1  -r <id> within the SAME config dir but from a different cwd (is lookup cwd-slug scoped?)
  T2  concurrency, same config dir: A TUI idle on S + A `-p -r S`
  T3  concurrency, TUI vs TUI across config dirs: A TUI idle on S + B TUI `-r P "<prompt>"`
  T4  auto-submit of the positional prompt when the resuming profile has NOT trusted the cwd
  T5  `-p -c` from a fresh profile (no fork of its own) after it resumed P by path
  T6  -r <path> when the config dir and cwd contain spaces and non-ASCII (Korean) characters
Uses cc.Run for every claude process; evidence stays in the run root.
"""
from __future__ import annotations

import glob
import json
import os
import re
import sys
import time
import uuid

EXP = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, EXP)
from e2_resume import (BodyLogMock, Ctx, body_since, compact_mock, conv, diff_snap, exit_tui,  # noqa: E402
                       forks, mock_since, parse_print, screen_has, sessions_dir, snapshot)
from cc import read_jsonl  # noqa: E402
from mockapi import write_scenario  # noqa: E402

READY = r"auto mode on|for shortcuts"
WARN_RX = r"another (process|session|instance|window)|already (open|running|in use|active)|in use|locked|lock file|being used|conflict"


def warn_hits(text: str) -> list[str]:
    return sorted({m.group(0) for m in re.finditer(WARN_RX, text, re.I)})


def main(root: str) -> int:
    ctx = Ctx(root)
    run = ctx.run
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {})  # every key echoes
    mock_log = os.path.join(run.logs, "mock.jsonl")
    body_log = os.path.join(run.logs, "mock_bodies.jsonl")
    srv = BodyLogMock(mock_log, scen, body_log)
    srv.start()
    rep: dict = {"run_root": run.root, "claude": run.claude, "tests": {}}
    T = rep["tests"]
    try:
        proj2 = os.path.join(run.root, "proj2")
        os.makedirs(proj2, exist_ok=True)
        A = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj, proj2])
        B = run.profile("B", "sk-mock-B", srv.base_url, trust=[run.proj])
        S = str(uuid.uuid4())
        rep["S"] = S
        cp = run.run_print(A, ["-p", "--session-id", S, "turn one from A", "--output-format", "json"], timeout=90)
        P = (glob.glob(os.path.join(A.dir, "projects", "*", S + ".jsonl")) or [None])[0]
        rep["setup"] = {"rc": cp.returncode, **parse_print(cp), "P": ctx.rel(P) if P else None}
        if not P:
            rep["fatal"] = "no P"
            return finish(rep, ctx, srv)

        # T1: same config dir, other cwd -----------------------------------------
        s = T["T1_same_dir_other_cwd_r_id"] = {}
        snap0 = snapshot(ctx)
        cp = run.run_print(A, ["-p", "-r", S, "t1 from proj2", "--output-format", "json"], timeout=90, cwd=proj2)
        s["rc"] = cp.returncode
        s.update(parse_print(cp))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))

        # T2 + T3: concurrency -----------------------------------------------------
        s = T["T2_T3_concurrency"] = {}
        P_rows0 = len(read_jsonl(P))
        pa = run.pty(A, ["-r", S], name="x_A_tui_idle")
        pa.start()
        s["A_ready"] = bool(pa.wait_for(READY, 60))
        time.sleep(2.0)
        s["A_sessions"] = [x.get("json", {}).get("sessionId") for x in sessions_dir(A.dir) if "json" in x]
        # T2: same config dir -p
        cp = run.run_print(A, ["-p", "-r", S, "samedir print X1", "--output-format", "json"], timeout=90)
        s["T2_rc"] = cp.returncode
        s["T2"] = parse_print(cp)
        s["T2_warn_in_output"] = warn_hits(cp.stdout + cp.stderr)
        s["P_rows_after_T2"] = len(read_jsonl(P))
        # T3: B TUI resumes by path while A TUI is open
        pb = run.pty(B, ["-r", P, "cross tui X2"], name="x_B_tui_cross")
        pb.start()
        s["B_ready"] = bool(pb.wait_for(READY, 60))
        s["B_reply"] = bool(pb.wait_for(r"MOCK-REPLY\[B\]\s*#\d+:\s*cross\s*tui", 45))
        time.sleep(1.5)
        s["B_screen_shows_X1"] = screen_has(pb, "samedir print X1")
        s["B_warn_on_screen"] = warn_hits(pb.plain())
        s["B_screen"] = " ".join(pb.plain().split())[-900:]
        s["sessions_while_both_open"] = {
            "A": [x.get("json", {}).get("sessionId") for x in sessions_dir(A.dir) if "json" in x],
            "B": [x.get("json", {}).get("sessionId") for x in sessions_dir(B.dir) if "json" in x]}
        s.update({"B_" + k: v for k, v in exit_tui(pb, "T3B", ctx).items()})
        s["P_rows_after_T3"] = len(read_jsonl(P))
        s["A_alive"] = pa.is_alive()
        s["A_warn_on_screen_so_far"] = warn_hits(pa.plain())
        m = pa.mark()
        t2 = time.time()
        time.sleep(1.0)
        pa.submit("after both X3")
        s["A_reply"] = bool(pa.wait_for(r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*both", 45, since=m))
        time.sleep(1.5)
        s["A_screen_after"] = " ".join(pa.plain(m).split())[-600:]
        s["A_screen_shows_X1"] = screen_has(pa, "samedir print X1")
        s["A_screen_shows_X2"] = screen_has(pa, "cross tui X2")
        s.update({"A_" + k: v for k, v in exit_tui(pa, "T2A", ctx).items()})
        s["A_bodies"] = [{k: b.get(k) for k in ("account", "n_messages", "markers")}
                         for b in body_since(body_log, t2) if b.get("main_loop")]
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["P_forks"] = forks(P)

        # T4: untrusted profile, positional prompt ---------------------------------
        s = T["T4_untrusted_autosubmit"] = {}
        U = run.profile("U", "sk-mock-U", srv.base_url, trust=None)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        pu = run.pty(U, ["-r", P, "untrusted Y1"], name="x_U_tui_untrusted")
        pu.start()
        hit = pu.wait_for(r"trust|safety check|MOCK-REPLY\[U\]|auto mode on|for shortcuts", 40)
        s["first_hit"] = hit.group(0) if hit else None
        time.sleep(6.0)  # give an auto-submit time to happen with no keys
        s["mock_before_any_key"] = compact_mock(mock_since(mock_log, t0))
        s["screen_before_any_key"] = " ".join(pu.plain().split())[-1200:]
        s["reply_before_any_key"] = screen_has(pu, "MOCK-REPLY[U]")
        if not s["reply_before_any_key"] and pu.is_alive():
            m = pu.mark()
            pu.send("\r")  # accept the default choice of whatever dialog is up (sandbox profile only)
            s["sent_enter"] = True
            s["reply_after_enter"] = bool(pu.wait_for(r"MOCK-REPLY\[U\]\s*#\d+:\s*untrusted", 40))
            time.sleep(1.5)
            s["screen_after_enter"] = " ".join(pu.plain(m).split())[-900:]
        s["mock_all"] = compact_mock(mock_since(mock_log, t0))
        if pu.is_alive():
            s.update(exit_tui(pu, "T4", ctx))
        else:
            s["exit_code"] = pu.wait_exit(1)
            pu.close()
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["U_trust_after"] = {k[-30:]: v.get("hasTrustDialogAccepted")
                              for k, v in (U.read_claude_json().get("projects") or {}).items()}

        # T5: fresh profile, -r path then -c ---------------------------------------
        s = T["T5_fresh_profile_continue"] = {}
        C = run.profile("C", "sk-mock-C", srv.base_url, trust=[run.proj])
        cp = run.run_print(C, ["-p", "-r", P, "c resume Z1", "--output-format", "json"], timeout=90)
        s["resume"] = {"rc": cp.returncode, **parse_print(cp)}
        snap0 = snapshot(ctx)
        t0 = time.time()
        cp = run.run_print(C, ["-p", "-c", "c continue Z2", "--output-format", "json"], timeout=90)
        s["continue"] = {"rc": cp.returncode, **parse_print(cp)}
        s["continue_files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["continue_bodies"] = [{k: b.get(k) for k in ("account", "n_messages", "session_hdr", "markers")}
                                for b in body_since(body_log, t0) if b.get("main_loop")]
        s["C_lastSessionId"] = {k[-30:]: v.get("lastSessionId")
                                for k, v in (C.read_claude_json().get("projects") or {}).items()}
        s["C_projects"] = sorted(ctx.rel(p) for p in glob.glob(os.path.join(C.dir, "projects", "**", "*.jsonl"),
                                                               recursive=True))

        # T6: spaces + Korean in config dir and cwd --------------------------------
        s = T["T6_space_korean_path"] = {}
        projK = os.path.join(run.root, "proj 한글 dir")
        os.makedirs(projK, exist_ok=True)
        K = run.profile("K", "sk-mock-K", srv.base_url, trust=[projK],
                        dir=os.path.join(run.root, "profiles", "K 공백 프로필"))
        B2 = run.profile("B", "sk-mock-B", srv.base_url, trust=[run.proj, projK])  # rewrite B seed: add trust
        S2 = str(uuid.uuid4())
        cp = run.run_print(K, ["-p", "--session-id", S2, "turn one from K", "--output-format", "json"],
                           timeout=90, cwd=projK)
        s["create"] = {"rc": cp.returncode, **parse_print(cp)}
        P2 = (glob.glob(os.path.join(K.dir, "projects", "*", S2 + ".jsonl")) or [None])[0]
        s["P2"] = ctx.rel(P2) if P2 else None
        s["P2_slug"] = os.path.basename(os.path.dirname(P2)) if P2 else None
        s["K_projects_dirs"] = [os.path.basename(d) for d in glob.glob(os.path.join(K.dir, "projects", "*"))]
        if P2:
            rows0 = len(read_jsonl(P2))
            t0 = time.time()
            pk = run.pty(B2, ["-r", P2, "turn two Korean path"], name="x_B_tui_korean", cwd=projK)
            pk.start()
            s["tui_ready"] = bool(pk.wait_for(READY, 60))
            s["tui_auto_reply"] = bool(pk.wait_for(r"MOCK-REPLY\[B\]\s*#\d+:\s*turn\s*two\s*Korean", 45))
            time.sleep(1.0)
            s["tui_screen"] = " ".join(pk.plain().split())[-500:]
            s.update({"tui_" + k: v for k, v in exit_tui(pk, "T6", ctx).items()})
            cp = run.run_print(B2, ["-p", "-r", P2, "turn three Korean print", "--output-format", "json"],
                               timeout=90, cwd=projK)
            s["print"] = {"rc": cp.returncode, **parse_print(cp)}
            s["P2_rows_before_after"] = [rows0, len(read_jsonl(P2))]
            s["P2_conv"] = [{k: r[k] for k in ("type", "sid", "text")} for r in conv(P2)]
            s["mock"] = compact_mock(mock_since(mock_log, t0))
            s["B_projects"] = sorted(ctx.rel(p) for p in glob.glob(os.path.join(B2.dir, "projects", "**", "*.jsonl"),
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
