"""verify_e6_errors_extra: adversarial follow-ups for e6_errors (reviewer script, does not edit e6).

    python -I verify_e6_errors_extra.py <empty run_root> [comma-separated list]

  X1  -p, "limit" (unified rejected) with x-should-retry ABSENT and the default retry-after 3600:
      does claude honour a 1-hour retry-after? (first api_retry.retry_delay_ms; killed after 45 s)
  X2  -p, same with x-should-retry "true"
  X3  TUI, "limit" with x-should-retry absent, retry-after 2, hooks: is anything written to the
      transcript in real time while the TUI retries? then Esc mid-retry: which hook fires?
  X4  -p, S4 config (x-should-retry absent, retry-after 2) with CLAUDE_CODE_MAX_RETRIES=1: is the retry cap honoured?
  R<n> -p, "limit" with x-should-retry absent and retry-after <n> s (killed at 15 s): retry-after cut-off
  R3600n -p, 429 without unified headers, x-should-retry absent, retry-after 3600
  Q1  -p S1 config, but WITHOUT CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC (QUIET_ENV check)
  Q6  -p S6w config (200 + allowed_warning unified headers) WITHOUT that variable
  Q2  TUI S2 config WITHOUT that variable, hooks
  Q6t TUI S6wt config WITHOUT that variable, hooks
Every claude process goes through cc.Run (run.env / run.pty): fake home, per-profile CLAUDE_CONFIG_DIR.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
import e6_errors as E  # noqa: E402
from cc import Run, read_jsonl, transcripts  # noqa: E402
from conpty import strip_ansi  # noqa: E402
from mockapi import read_log, write_scenario  # noqa: E402

NOQ = {"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": None}
SC = {
    "X1": ("sk-mock-x1", {"mode": "limit", "headers": {"x-should-retry": None}}, "print", 45, None),
    "X2": ("sk-mock-x2", {"mode": "limit", "headers": {"x-should-retry": "true"}}, "print", 45, None),
    "X3": ("sk-mock-x3", {"mode": "limit", "headers": {"x-should-retry": None, "retry-after": "2"}}, "tui3", 25, None),
    "X4": ("sk-mock-x4", {"mode": "limit", "headers": {"x-should-retry": None, "retry-after": "2"}}, "print", 60,
           {"CLAUDE_CODE_MAX_RETRIES": "1"}),
    # retry-after threshold probes: x-should-retry absent, unified 'rejected' headers, retry-after N (killed at 15 s)
    **{f"R{n}": (f"sk-mock-r{n}", {"mode": "limit", "headers": {"x-should-retry": None, "retry-after": str(n)}},
                 "print", 15, None) for n in (10, 30, 59, 61, 120, 300)},
    # same with NO unified headers (transient429) and retry-after 3600
    "R3600n": ("sk-mock-rn36", {"mode": "transient429", "count": 99,
                                "headers": {"x-should-retry": None, "retry-after": "3600"}}, "print", 15, None),
    "Q1": ("sk-mock-q1", {"mode": "limit"}, "print", 90, NOQ),
    "Q6": ("sk-mock-q6", {"mode": "echo", "sse_headers": E.WARN_HDR}, "print", 90, NOQ),
    "Q2": ("sk-mock-q2", {"mode": "limit"}, "tui", 12, NOQ),
    "Q6t": ("sk-mock-q7", {"mode": "echo", "sse_headers": E.WARN_HDR}, "tui", 12, NOQ),
}
ALL = list(SC)


def hits(mock_log, key):
    acct = E.redact(key)
    return [r for r in read_log(mock_log) if r.get("account") == acct and r.get("n")]


def other_rows(mock_log, keys):
    accts = {E.redact(k) for k in keys}
    return [r for r in read_log(mock_log) if r.get("account") not in accts and r.get("event") != "e6_resp"]


def run_print(run, prof, timeout, extra, label):
    t0 = time.time()
    args = ["-p", "hi", "--output-format", "stream-json", "--verbose"]
    p = subprocess.Popen([run.claude, *args], cwd=run.proj, env=run.env(prof, extra), stdin=subprocess.DEVNULL,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out, err = [], []

    def rd(stream, sink):
        for raw in iter(stream.readline, b""):
            sink.append((time.time() - t0, raw.decode("utf-8", "replace").rstrip("\r\n")))

    th = [threading.Thread(target=rd, args=(p.stdout, out), daemon=True),
          threading.Thread(target=rd, args=(p.stderr, err), daemon=True)]
    for t in th:
        t.start()
    timed_out = False
    try:
        rc = p.wait(timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        p.kill()  # our own child, by handle
        rc = p.wait(15)
    for t in th:
        t.join(5)
    with open(os.path.join(run.logs, f"{label}.stdout.jsonl"), "w", encoding="utf-8") as f:
        for _, line in out:
            f.write(line + "\n")
    st = E.analyze_stream(out)
    return {"rc": rc, "timed_out": timed_out, "secs": round(time.time() - t0, 2),
            "event_type_counts": st["event_type_counts"],
            "api_retry": [{"t": e["t"], **{k: e["obj"].get(k) for k in ("attempt", "retry_delay_ms", "error_status",
                                                                         "error", "max_retries")}}
                          for e in st["api_retry_events"]],
            "rate_limit_events": st["rate_limit_events"], "other_events": st["other_events"],
            "result": {k: (st["result"] or {}).get(k) for k in ("subtype", "is_error", "terminal_reason",
                                                                "api_error_status", "result")} if st["result"] else None,
            "stderr_tail": [l for _, l in err][-10:]}


def tx_rows(prof):
    rows = []
    for p in transcripts(prof.dir):
        rows += read_jsonl(p)
    return rows


def run_tui(run, prof, label, key, mock_log, extra, watch, esc_mid_retry=False):
    settings, hooks_out = E.hook_settings(run, label)
    pc = run.pty(prof, ["--settings", settings], name=label, extra_env=extra)
    pc.start()
    rep = {}
    try:
        rep["ready"] = bool(pc.wait_for(E.TUI_READY, 60))
        time.sleep(1.5)
        mark = pc.mark()
        pc.send("hi")
        time.sleep(0.4)
        t_sub = time.time()
        pc.send("\r")
        timeline = []
        last = None
        while time.time() - t_sub < watch:
            rows = tx_rows(prof)
            n_req = sum(1 for r in rows if r.get("type") == "api-request")
            n_err = sum(1 for r in rows if r.get("type") == "system" and r.get("subtype") == "api_error")
            n_asst = sum(1 for r in rows if r.get("type") == "assistant")
            n_hits = len(hits(mock_log, key))
            hk = [h.get("label") for h in read_jsonl(hooks_out)]
            retrying = bool(re.search(r"Retrying", strip_ansi(pc.text[mark:], " ")))
            cur = (n_hits, n_req, n_err, n_asst, tuple(hk), retrying)
            if cur != last:
                timeline.append({"t": round(time.time() - t_sub, 2), "mock_hits": n_hits, "tx_api_request_rows": n_req,
                                 "tx_system_api_error_rows": n_err, "tx_assistant_rows": n_asst, "hooks": hk,
                                 "screen_retrying": retrying})
                last = cur
            time.sleep(0.25)
        rep["timeline"] = timeline
        if esc_mid_retry:
            m2 = pc.mark()
            t_esc = time.time()
            pc.send("\x1b")
            time.sleep(8)
            rows = tx_rows(prof)
            rep["after_esc"] = {
                "hooks": [{"label": h.get("label"), "t_after_esc": round(h.get("ts", 0) - t_esc, 2),
                           "error": (h.get("payload") or {}).get("error"),
                           "last_assistant_message": ((h.get("payload") or {}).get("last_assistant_message") or "")[:120]}
                          for h in read_jsonl(hooks_out)],
                "mock_hits_total": len(hits(mock_log, key)),
                "screen": " ".join(strip_ansi(pc.text[m2:], " ").split())[-600:],
                "tx_tail_types": [str(r.get("type")) + ("/" + str(r.get("subtype")) if r.get("subtype") else "")
                                  for r in rows[-8:]],
                "alive": pc.is_alive(),
            }
            time.sleep(4)
            rep["mock_hits_12s_after_esc"] = len(hits(mock_log, key))
        after = strip_ansi(pc.text[mark:], " ")
        rep["patterns"] = {n: bool(re.search(rx, after, re.I)) for n, rx in E.TUI_PATTERNS.items()}
        rep["screen_snippets"] = E.snippets(after, 15)
        rep["hooks"] = [{"label": h.get("label"), "t": round(h.get("ts", 0) - t_sub, 2),
                         "keys": sorted((h.get("payload") or {}).keys()), "error": (h.get("payload") or {}).get("error")}
                        for h in read_jsonl(hooks_out)]
    finally:
        pc.send("\x03")
        time.sleep(0.6)
        pc.send("\x03")
        rep["exit_code"] = pc.wait_exit(15)
        pc.close()
    return rep


def main(root, which):
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {k: v for (k, v, *_r) in SC.values()})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = E.make_mock(mock_log, scen)
    report = {"run_root": run.root, "claude": run.claude, "scenarios": {}}
    try:
        for label in which:
            key, cfg, kind, timeout, extra = SC[label]
            prof = run.profile(label, key, srv.base_url, trust=[run.proj])
            entry = {"mock_cfg": cfg, "kind": kind, "env_removed": list(extra or {})}
            try:
                t0 = time.time()
                if kind == "print":
                    entry["run"] = run_print(run, prof, timeout, extra, label)
                else:
                    entry["run"] = run_tui(run, prof, label, key, mock_log, extra, timeout,
                                           esc_mid_retry=(kind == "tui3"))
                hs = hits(mock_log, key)
                entry["mock_hits"] = [{"t": round(r["ts"] - t0, 2), "status": r.get("status"),
                                       "main_loop": r.get("main_loop"), "n_tools": r.get("n_tools")} for r in hs]
            except Exception as e:
                entry["exception"] = repr(e)
            report["scenarios"][label] = entry
            with open(os.path.join(run.logs, f"{label}.report.json"), "w", encoding="utf-8") as f:
                json.dump(entry, f, ensure_ascii=False, indent=1)
        report["non_scenario_mock_rows"] = other_rows(mock_log, [v[0] for v in SC.values()])[:50]
    finally:
        srv.stop()
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sel = sys.argv[2].split(",") if len(sys.argv) > 2 else ALL
    sys.exit(main(sys.argv[1], sel))
