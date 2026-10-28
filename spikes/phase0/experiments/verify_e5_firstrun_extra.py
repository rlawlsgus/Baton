"""verify_e5_firstrun_extra: adversarial follow-ups to e5_firstrun (reviewer script; does not edit e5).

    python -I verify_e5_firstrun_extra.py <empty run_root> [k w t]

k  trust-key variants e5 did not isolate:
     - upper-case drive but the rest of the path lower-cased (e5's all-lowercase key also
       lower-cased the drive, so it could not tell drive-case from path-case)
     - drive-root key in the other drive-letter case ('c:/' with cwd 'C:\\...', and vice versa)
     - seeded 'c:/...' key with cwd 'c:\\...' (positive control for "keyed by cwd as given")
     - both drive-case forms seeded, cwd 'c:\\...'
w  warm relaunch: the same trusted+onboarded profile launched 3x with `-r <path> "<prompt>"`;
   time from spawn to the prompt reaching the mock (first launch of a profile dir vs later ones)
t  fresh profile (no .claude.json) with Windows-Terminal-like env (WT_SESSION, COLORTERM):
   does any extra first-run dialog appear?  (e5's env allowlist strips these.)

All claude processes go through cc.Run (via e5_firstrun.Exp).
"""
from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
sys.path.insert(0, HERE)
import e5_firstrun as E  # noqa: E402
from cc import fwd  # noqa: E402


class TuiEnv(E.Tui):
    extra_env: dict | None = None

    def __init__(self, run, profile, args, name, cwd=None):
        self.run, self.profile, self.args, self.name = run, profile, args, name
        self.pc = run.pty(profile, args, name=name, cwd=cwd, extra_env=TuiEnv.extra_env)
        self.scr = E.Screen(self.pc.cols, self.pc.rows)
        self._fed = 0
        self.snapshots = []


def step_k(e: E.Exp) -> dict:
    proj = e.run.proj
    f = fwd(proj)
    d = f[0]
    up, lo = d.upper(), d.lower()
    cwd_lo = lo + proj[1:]
    variants = {
        "upper_drive_rest_lower": ([up + f[1:].lower()], None),
        "lower_root_key_upper_cwd": ([lo + ":/"], None),
        "upper_root_key_upper_cwd_control": ([up + ":/"], None),
        "lower_key_lower_cwd": ([lo + f[1:]], cwd_lo),
        "lower_root_key_lower_cwd": ([lo + ":/"], cwd_lo),
        "upper_root_key_lower_cwd": ([up + ":/"], cwd_lo),
        "both_drive_forms_lower_cwd": ([up + f[1:], lo + f[1:]], cwd_lo),
        "both_drive_forms_upper_cwd": ([up + f[1:], lo + f[1:]], None),
    }
    out = {}
    for i, (vname, (keys, cwd)) in enumerate(variants.items()):
        cj = {"hasCompletedOnboarding": True,
              "projects": {k: {"hasTrustDialogAccepted": True} for k in keys}}
        p = e.profile(f"K{i:02d}", cj)
        r = e.tui_case(f"k_{vname}", p, [], explore=False, cwd=cwd, timeout=60)
        names = [x["name"] for x in r["dialogs"]]
        out[vname] = {"seed_keys": keys, "cwd": cwd or proj, "outcome": r["outcome"], "dialogs": names,
                      "honored": r["outcome"] == "ready" and "trust" not in names,
                      "projects_after": {k: (v or {}).get("hasTrustDialogAccepted")
                                         for k, v in (p.read_claude_json().get("projects") or {}).items()}}
    return out


def step_w(e: E.Exp) -> dict:
    sid, path = e._seed_transcript()
    if not path:
        return {"error": "seed transcript missing"}
    p = e.profile("R1", {"hasCompletedOnboarding": True,
                         "projects": {fwd(e.run.proj): {"hasTrustDialogAccepted": True}}})
    out = {}
    for i in range(3):
        prompt = f"warm relaunch {i}"
        r = e.tui_case(f"w_launch{i}", p, ["-r", path, prompt], expect_auto=prompt)
        out[f"launch{i}"] = {"outcome": r["outcome"], "dialogs": [x["name"] for x in r["dialogs"]],
                             "auto_submitted": r.get("auto_submitted"),
                             "auto_submit_t": (r.get("auto_submit_request") or {}).get("t"),
                             "n_messages": (r.get("auto_submit_request") or {}).get("n_messages"),
                             "blank_screens": r.get("blank_screens"), "t_ready": r.get("t_ready"),
                             "helper_calls": r.get("helper_calls")}
        time.sleep(1)
    out["marketplace_dir_exists"] = os.path.isdir(os.path.join(p.dir, "plugins", "marketplaces"))
    return out


def step_t(e: E.Exp) -> dict:
    TuiEnv.extra_env = {"WT_SESSION": str(uuid.uuid4()), "WT_PROFILE_ID": "{" + str(uuid.uuid4()) + "}",
                        "COLORTERM": "truecolor"}
    orig = E.Tui
    E.Tui = TuiEnv
    # Under WT_SESSION claude draws the pointer/prompt as U+276F instead of '>'; e5's READY_RX and
    # trust_yes_keys only know '>' (run extra-1 showed: REPL taken for an unknown screen, and the trust
    # answer degraded to a bare Enter = "No, exit").  Patch both to accept either glyph.
    orig_rx, orig_tyk = E.READY_RX, E.trust_yes_keys
    E.READY_RX = re.compile(r"^─{20,}\n[>❯].*\n─{20,}", re.M)

    def tyk(screen: str) -> list[str]:
        opts = [l.strip() for l in screen.splitlines()
                if re.match(r"^\s*([>❯]\s+)?(No, exit|Yes, I trust|Yes, proceed)", l)]
        cur = next((i for i, l in enumerate(opts) if l[:1] in (">", "❯")), 0)
        yes = next((i for i, l in enumerate(opts) if "Yes" in l), 1)
        moves = yes - cur
        return (["\x1b[B"] * moves if moves > 0 else ["\x1b[A"] * (-moves)) + ["\r"]

    E.trust_yes_keys = tyk
    try:
        p = e.profile("F1", None)
        r = e.tui_case("t_wt_fresh", p, [], probe="wt fresh probe")
        p2 = e.profile("F2", {"hasCompletedOnboarding": True,
                              "projects": {fwd(e.run.proj): {"hasTrustDialogAccepted": True}}})
        r2 = e.tui_case("t_wt_seeded", p2, [], probe="wt seeded probe")
    finally:
        E.Tui = orig
        E.READY_RX, E.trust_yes_keys = orig_rx, orig_tyk
        TuiEnv.extra_env = None
    return {
        "fresh": {"outcome": r["outcome"], "dialogs": [x["name"] for x in r["dialogs"]],
                  "probe_reply": r.get("probe_reply_on_screen"), "probe_mock": r.get("probe_mock_requests"),
                  "claude_json_keys": sorted((r.get("claude_json_final") or {}).keys())},
        "seeded": {"outcome": r2["outcome"], "dialogs": [x["name"] for x in r2["dialogs"]],
                   "probe_reply": r2.get("probe_reply_on_screen"), "probe_mock": r2.get("probe_mock_requests")},
    }


def main(argv):
    root = argv[1]
    steps = argv[2:] or ["k", "w", "t"]
    e = E.Exp(root)
    rep = {"claude": e.run.claude, "run_root": e.run.root}
    try:
        for s in steps:
            t = time.time()
            try:
                rep[s] = {"k": step_k, "w": step_w, "t": step_t}[s](e)
            except Exception:
                import traceback
                rep[f"{s}_exception"] = traceback.format_exc()[-2000:]
            rep.setdefault("step_secs", {})[s] = round(time.time() - t, 1)
    finally:
        e.close()
    js = json.dumps(rep, ensure_ascii=False, indent=1)
    with open(os.path.join(e.run.root, "report.json"), "w", encoding="utf-8") as fh:
        fh.write(js)
    print(js)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
