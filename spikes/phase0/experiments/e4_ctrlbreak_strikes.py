"""e4_ctrlbreak follow-up: what does a HARD kill (the supervisor's fallback) cost the next launch?

    python -I spikes/phase0/experiments/e4_ctrlbreak_strikes.py <empty run_root>

Same supervisor/ConPTY setup as e4_ctrlbreak.py. Chain in one console, one profile, one session:
  K0  fresh session, one turn, TerminateProcess ~6 s after spawn   (restore_modes=True)
  K1  -r S, one turn, kill again                                    (restore_modes=True)
  K2  -r S, one turn, kill again                                    (restore_modes=True)
  K3  -r S, one turn, CTRL_BREAK
  K4  -r S, one turn, wait until ~20 s after spawn, then kill        (restore_modes=True)
  K5  -r S, one turn, CTRL_BREAK
Per launch: fullscreen lines from claude --debug-file, alt-screen enter at startup, the profile's
.claude.json fullscreenBootStrikes after exit, console modes after exit and after the supervisor's restore.
"""
from __future__ import annotations

import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from e4_ctrlbreak import E4  # noqa: E402


class Strikes(E4):
    def one(self, i: int, kill: bool, min_age: float = 0.0) -> dict:
        if i == 0:
            m = self.go(0, ["--session-id", self.sid], restore_modes=kill)
            ready = self.wscreen(r"auto mode on|for shortcuts", m, 60)
            time.sleep(1.5)
            m2 = self.pc.mark()
            self.pc.submit("strike probe")
            got = self.wait_reply(r"MOCK-REPLY\[A\]\s*#\d+:\s*strike", m2, 60)
        else:
            m = self.go(i, ["-r", self.sid, f"turn {i}"], restore_modes=kill)
            ready = self.wscreen(r"auto mode on|for shortcuts", m, 60)
            got = self.wait_reply(rf"MOCK-REPLY\[A\]\s*#\d+:\s*turn\s*{i}", m, 60)
        rec = self.launch_rec(i)
        if min_age:
            time.sleep(max(0.0, rec.get("spawned_at", time.time()) + min_age - time.time()))
        fs_at_ready = self.fs_debug(i)
        raw_now = len(self.pc.raw)
        res_b = self.break_and_collect(i, f"K{i}", method="kill" if kill else "ctrl_break")
        rec = self.launch_rec(i)
        out = {"kill": kill, "ready": bool(ready), "reply": got,
               "secs_alive_before_stop": round(rec.get("t0", 0) - rec.get("spawned_at", 0), 1),
               "alt_screen_entered": "alt_screen_on ?1049h" in self.startup_seqs(i, raw_now)["enter_seqs"],
               "fullscreen_debug": self.fs_debug(i), "fullscreen_debug_at_ready": fs_at_ready,
               "strikes_after": self.strikes(),
               "exit_code": rec.get("exit_code"), "modes_before_spawn": (rec.get("modes_before_spawn") or {}).get("in"),
               "modes_after": (rec.get("modes_after") or {}).get("in"),
               "modes_after_restore": (rec.get("modes_after_restore") or {}).get("in"),
               "cp_after_restore": (rec.get("modes_after_restore") or {}).get("cp_out"),
               "restore": rec.get("restore"), "sessions_dir_after": res_b["sessions_dir_after"],
               "hooks": res_b["hooks"]}
        self.step(f"K{i}", kill=kill, strikes=out["strikes_after"], alt=out["alt_screen_entered"])
        return out

    def _chain(self) -> None:
        pc, S = self.pc, self.report["scenarios"]
        if not pc.wait_for(r"E4SUP-READY", 20):
            self.report["fatal"] = "supervisor did not start"
            return
        plan = [(0, True, 0.0), (1, True, 0.0), (2, True, 0.0), (3, False, 0.0), (4, True, 20.0), (5, False, 0.0)]
        for i, kill, age in plan:
            S[f"K{i}"] = self.one(i, kill, age)
            time.sleep(1.0)


def main(root: str) -> int:
    e = Strikes(root)
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
