"""Probe for e7_switchloop: which hook-entry shapes fire in claude 2.1.308 (print mode, mock API).

    python -I e7_switchloop_probe.py <run_root>
"""
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, read_jsonl  # noqa: E402
from mockapi import MockServer, read_log, write_scenario  # noqa: E402

PY = sys.executable
HOOK = os.path.join(HERE, "e7_switchloop_hook.py")


def main(root: str) -> int:
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-L": {"mode": "limit"}})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = MockServer(mock_log, scen)
    srv.start()
    rep = {"python": PY}
    try:
        a = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        lim = run.profile("L", "sk-mock-L", srv.base_url, trust=[run.proj])
        forms = {
            "shell_string": lambda ev, f: {"type": "command",
                                           "command": f'"{PY}" -I "{HOOK}" {ev} "{f}"'},
            "command_plus_args": lambda ev, f: {"type": "command", "command": PY,
                                                "args": ["-I", HOOK, ev, f]},
            "command_array": lambda ev, f: {"type": "command", "command": [PY, "-I", HOOK, ev, f]},
            "shell_string_fwd": lambda ev, f: {"type": "command",
                                               "command": f'"{PY.replace(os.sep, "/")}" -I "{HOOK.replace(os.sep, "/")}" {ev} "{f.replace(os.sep, "/")}"'},
        }
        for name, mk in forms.items():
            for prof in (a, lim):
                ev_file = os.path.join(run.logs, f"events-{name}-{prof.name}.jsonl")
                settings = {"hooks": {
                    "SessionStart": [{"hooks": [mk("SessionStart", ev_file)]}],
                    "StopFailure": [{"matcher": "rate_limit|authentication_failed",
                                     "hooks": [mk("StopFailure", ev_file)]}],
                    "Stop": [{"hooks": [mk("Stop", ev_file)]}],
                }}
                sp = os.path.join(run.root, f"settings-{name}.json")
                with open(sp, "w", encoding="utf-8") as f:
                    json.dump(settings, f, indent=1)
                dbg = os.path.join(run.logs, f"debug-{name}-{prof.name}.txt")
                t0 = time.time()
                cp = run.run_print(prof, ["-p", f"probe {name}", "--settings", sp,
                                          "--debug", "hooks", "--debug-file", dbg], timeout=90)
                time.sleep(1.0)
                evs = read_jsonl(ev_file)
                rep[f"{name}/{prof.name}"] = {
                    "rc": cp.returncode, "secs": round(time.time() - t0, 1),
                    "stdout": cp.stdout[-400:], "stderr": cp.stderr[-600:],
                    "events": [{"event": e.get("event"),
                                "keys": sorted((e.get("payload") or {}).keys()),
                                "payload": e.get("payload") if e.get("event") == "StopFailure" else None}
                               for e in evs],
                }
    finally:
        srv.stop()
    rep["mock"] = [{k: r.get(k) for k in ("account", "status", "main_loop", "user_text")} for r in read_log(mock_log)]
    print(json.dumps(rep, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
