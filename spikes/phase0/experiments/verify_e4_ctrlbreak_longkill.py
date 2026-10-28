"""Verifier probe for e4_ctrlbreak claim C6 (hard kill -> fullscreen strike).

    python -I spikes/phase0/experiments/verify_e4_ctrlbreak_longkill.py <empty run_root>

claude.exe 2.1.308 arms a "fullscreen boot canary" (fullscreenBootPending[pid] in the profile's
.claude.json) and settles it as healthy healthyAfterMs (1e4 in the bundle) after the first frame.
This probe checks, with the same supervisor/ConPTY setup as e4_ctrlbreak.py:
  P0  fresh session, one turn, stay alive ~25 s, then TerminateProcess   -> strike or not?
  P1  -r S, one turn, TerminateProcess ~5 s after spawn                    -> strike expected
  P2  -r S, one turn (expect non-fullscreen launch), CTRL_BREAK
  P3  -r S, one turn, stay alive ~20 s (canary healthy?), CTRL_BREAK      -> strikes cleared?
  P4  -r S, one turn, CTRL_BREAK                                           -> fullscreen again?
Reuses E4 from e4_ctrlbreak.py unchanged.
"""
from __future__ import annotations

import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from e4_ctrlbreak import E4  # noqa: E402


class LongKill(E4):
    def pending(self):
        return self.prof.read_claude_json().get("fullscreenBootPending")

    def auto_disabled(self):
        return self.prof.read_claude_json().get("fullscreenAutoDisabled")

    def one(self, i: int, method: str, alive_s: float) -> dict:
        if i == 0:
            m = self.go(0, ["--session-id", self.sid])
            ready = self.wscreen(r"auto mode on|for shortcuts", m, 60)
            time.sleep(1.5)
            m2 = self.pc.mark()
            self.pc.submit("longkill probe")
            got = self.wait_reply(r"MOCK-REPLY\[A\]\s*#\d+:\s*longkill", m2, 60)
        else:
            m = self.go(i, ["-r", self.sid, f"turn {i}"])
            ready = self.wscreen(r"auto mode on|for shortcuts", m, 60)
            got = self.wait_reply(rf"MOCK-REPLY\[A\]\s*#\d+:\s*turn\s*{i}", m, 60)
        rec = self.launch_rec(i)
        pending_at_ready = self.pending()
        fs_at_ready = self.fs_debug(i)
        time.sleep(max(0.0, rec.get("spawned_at", time.time()) + alive_s - time.time()))
        fs_before_stop = self.fs_debug(i)
        pending_before_stop = self.pending()
        strikes_before_stop = self.strikes()
        raw_now = len(self.pc.raw)
        res_b = self.break_and_collect(i, f"P{i}", method=method)
        rec = self.launch_rec(i)
        out = {"method": method, "ready": bool(ready), "reply": got,
               "secs_alive_before_stop": round(rec.get("t0", 0) - rec.get("spawned_at", 0), 1),
               "alt_screen_entered": "alt_screen_on ?1049h" in self.startup_seqs(i, raw_now)["enter_seqs"],
               "fullscreen_debug_at_ready": fs_at_ready, "fullscreen_debug_before_stop": fs_before_stop,
               "fullscreen_debug_after": self.fs_debug(i),
               "pending_pids_at_ready": sorted((pending_at_ready or {}).keys()),
               "pending_pids_before_stop": sorted((pending_before_stop or {}).keys()),
               "own_pid": rec.get("pid"),
               "strikes_before_stop": strikes_before_stop, "strikes_after": self.strikes(),
               "auto_disabled_after": self.auto_disabled(), "pending_after": self.pending(),
               "exit_code": rec.get("exit_code"), "hooks": res_b["hooks"],
               "sessions_dir_after": res_b["sessions_dir_after"]}
        self.step(f"P{i}", method=method, strikes=out["strikes_after"], alt=out["alt_screen_entered"])
        return out

    def _chain(self) -> None:
        pc, S = self.pc, self.report["scenarios"]
        if not pc.wait_for(r"E4SUP-READY", 20):
            self.report["fatal"] = "supervisor did not start"
            return
        plan = [(0, "kill", 25.0), (1, "kill", 5.0), (2, "ctrl_break", 5.0), (3, "ctrl_break", 20.0),
                (4, "ctrl_break", 5.0)]
        for i, method, alive in plan:
            S[f"P{i}"] = self.one(i, method, alive)
            time.sleep(1.0)


def main(root: str) -> int:
    e = LongKill(root)
    rep = e.main()
    rep.pop("transcript", None)
    rep.pop("mock_requests", None)
    out = json.dumps(rep, ensure_ascii=False, indent=1, default=str)
    with open(os.path.join(e.logs, "report.json"), "w", encoding="utf-8") as f:
        f.write(out)
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
