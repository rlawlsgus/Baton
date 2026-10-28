"""Smoke test: claude.exe talks to the mock in -p mode and in the interactive TUI.

    python -I spikes/phase0/smoke.py <run_root>
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "harness"))
from cc import Run, read_jsonl, summarize_rows, transcripts  # noqa: E402
from mockapi import MockServer, read_log, write_scenario  # noqa: E402


def main(root: str) -> int:
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {"sk-mock-A": {"mode": "echo"}})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = MockServer(mock_log, scen)
    srv.start()
    report = {"claude": run.claude, "port": srv.port}
    try:
        a = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])

        t0 = time.time()
        cp = run.run_print(a, ["-p", "hello from print mode", "--output-format", "json"], timeout=120)
        report["print"] = {"rc": cp.returncode, "secs": round(time.time() - t0, 1),
                           "stdout": cp.stdout[-1500:], "stderr": cp.stderr[-1500:]}

        pc = run.pty(a, [], name="tui")
        pc.start()
        ready = pc.wait_for(r"for shortcuts|\? for|Try \"|>\s", timeout=60)
        report["tui_ready"] = bool(ready)
        report["tui_ready_secs"] = round(time.time() - pc.started_at, 1)
        time.sleep(1.5)
        mark = pc.mark()
        pc.submit("hello from the tui")
        got = pc.wait_for(r"MOCK-REPLY\[A\]", timeout=60, since=mark)
        report["tui_reply"] = bool(got)
        time.sleep(1)
        report["tui_tail"] = pc.plain()[-1500:]
        pc.send("\x03")
        time.sleep(0.5)
        pc.send("\x03")
        report["tui_exit"] = pc.wait_exit(15)
        pc.close()

        report["transcripts"] = [os.path.relpath(p, a.dir) for p in transcripts(a.dir)]
        report["rows"] = [summarize_rows(read_jsonl(p), 8) for p in transcripts(a.dir)]
    finally:
        srv.stop()
        report["mock_requests"] = [{k: r.get(k) for k in ("method", "path", "status", "mode", "model",
                                                          "main_loop", "n_tools", "user_text", "reply")}
                                   for r in read_log(mock_log)]
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
