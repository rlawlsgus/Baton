"""e6_errors: how claude 2.1.308 surfaces a 429 / 529 in -p stream-json, the TUI, the transcript
and StopFailure hooks, and how often it retries.

    python -I e6_errors.py <empty run_root> [comma-separated scenario list]

Scenarios (one dummy key = one account, so mock hit counts never mix):
  S1   -p stream-json, "limit" (429 + unified headers, x-should-retry false, retry-after 3600)
  S1h  same as S1 with Stop/StopFailure exec hooks injected via --settings
  S3   -p stream-json, "transient429" (count 2, retry-after 1, x-should-retry true)
  S4   -p stream-json, "limit" with x-should-retry ABSENT and retry-after "2"
  S5   -p stream-json, "overloaded" (529, x-should-retry false)
  S2   TUI, "limit", with hooks
  S3t  TUI, "transient429", with hooks
  S4t  TUI, "limit" without x-should-retry, retry-after 2, with hooks
  S5t  TUI, "overloaded", with hooks
  S6w  -p stream-json, 200 echo reply carrying unified headers (status allowed_warning) -> rate_limit_event?
  S6wt TUI, same as S6w, with hooks
  S7   -p stream-json, 429 WITHOUT unified headers, x-should-retry false (transient429, count 99)
  S7t  TUI, same as S7, with hooks
  S8   -p stream-json, 429 WITHOUT unified headers, x-should-retry ABSENT, retry-after 1 (count 99)
Prints one JSON report on stdout; evidence stays under <run_root>/logs.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, read_jsonl, summarize_rows, transcripts  # noqa: E402
from conpty import strip_ansi  # noqa: E402
from mockapi import Handler, MockServer, read_log, write_scenario  # noqa: E402

HOOK = os.path.join(HERE, "e6_errors_hook.py")
ALL = ["S1", "S1h", "S3", "S4", "S5", "S6w", "S7", "S8", "S2", "S3t", "S4t", "S5t", "S6wt", "S7t"]
NO_RETRY_HDR = {"x-should-retry": None, "retry-after": "2"}
_R = int(time.time() + 3600)
WARN_HDR = {
    "anthropic-ratelimit-unified-status": "allowed_warning",
    "anthropic-ratelimit-unified-reset": str(_R),
    "anthropic-ratelimit-unified-representative-claim": "five_hour",
    "anthropic-ratelimit-unified-5h-status": "allowed_warning",
    "anthropic-ratelimit-unified-5h-utilization": "0.92",
    "anthropic-ratelimit-unified-5h-reset": str(_R),
    "anthropic-ratelimit-unified-7d-status": "allowed",
    "anthropic-ratelimit-unified-7d-utilization": "0.42",
    "anthropic-ratelimit-unified-7d-reset": str(_R + 86400 * 3),
    "anthropic-ratelimit-unified-fallback-percentage": "0.5",
}

# scenario -> (api key, mock account config, kind, hooks?, timeout seconds)
SCEN = {
    "S1": ("sk-mock-1", {"mode": "limit"}, "print", False, 150),
    "S1h": ("sk-mock-7", {"mode": "limit"}, "print", True, 150),
    "S3": ("sk-mock-3", {"mode": "transient429", "count": 2}, "print", False, 120),
    "S4": ("sk-mock-4", {"mode": "limit", "headers": NO_RETRY_HDR}, "print", False, 200),
    "S5": ("sk-mock-5", {"mode": "overloaded"}, "print", False, 150),
    "S2": ("sk-mock-2", {"mode": "limit"}, "tui", True, 90),
    "S3t": ("sk-mock-8", {"mode": "transient429", "count": 2}, "tui", True, 90),
    "S4t": ("sk-mock-9", {"mode": "limit", "headers": NO_RETRY_HDR}, "tui", True, 200),
    "S5t": ("sk-mock-6", {"mode": "overloaded"}, "tui", True, 120),
    "S6w": ("sk-mock-a", {"mode": "echo", "sse_headers": WARN_HDR}, "print", False, 120),
    "S6wt": ("sk-mock-b", {"mode": "echo", "sse_headers": WARN_HDR}, "tui", True, 90),
    "S7": ("sk-mock-c", {"mode": "transient429", "count": 99, "headers": {"x-should-retry": "false"}},
           "print", False, 120),
    "S7t": ("sk-mock-d", {"mode": "transient429", "count": 99, "headers": {"x-should-retry": "false"}},
            "tui", True, 90),
    "S8": ("sk-mock-e", {"mode": "transient429", "count": 99, "headers": {"x-should-retry": None}},
           "print", False, 220),
}

TUI_READY = r"auto mode on|for shortcuts|\? for"
TUI_PATTERNS = {
    "api_error": r"API Error",
    "rate_limit": r"rate.?limit",
    "usage_limit": r"usage limit|limit reached|limit will reset|resets? (at|in)",
    "session_limit": r"Session limit",
    "pct_used": r"\d+% (of|used)",
    "exceed": r"exceed your account",
    "retrying": r"Retrying",
    "attempt": r"attempt\s*\d+",
    "overloaded": r"overloaded",
    "s429": r"\b429\b",
    "s529": r"\b529\b",
    "esc_to_interrupt": r"esc to interrupt",
    "reply": r"MOCK-REPLY",
    "slash_hint": r"/upgrade|/extra-usage|/login|/rate-limit-options|/usage",
}
SNIPPET_RX = re.compile(r"API Error|rate.?limit|usage limit|limit|Retrying|attempt\s*\d+|overloaded|exceed|429|529|"
                        r"MOCK-REPLY|error", re.I)


def fs(p: str) -> str:
    return os.path.abspath(p).replace("\\", "/")


def redact(key: str) -> str:
    return "..." + key[-4:]


class E6Handler(Handler):
    """Harness handler plus: a header override whose value is null is dropped (the harness would send
    the string "None"), and every JSON response's status + headers are logged as an 'e6_resp' row."""

    def _send_json(self, status, obj, extra_headers=None):
        hdrs = {k: v for k, v in (extra_headers or {}).items() if v is not None}
        if status >= 400:
            self.state.log({"ts": time.time(), "event": "e6_resp", "account": redact(self._account()),
                            "status": status, "headers": hdrs})
        super()._send_json(status, obj, hdrs)

    def _sse_start(self, extra_headers=None):
        hdrs = dict(extra_headers or {})
        sc = self.state.scenario_for(self._account())
        hdrs.update({k: v for k, v in (sc.get("sse_headers") or {}).items() if v is not None})
        if sc.get("sse_headers"):
            self.state.log({"ts": time.time(), "event": "e6_resp", "account": redact(self._account()),
                            "status": 200, "headers": hdrs})
        super()._sse_start(hdrs)


def make_mock(log_path: str, scen_path: str) -> MockServer:
    srv = MockServer(log_path, scen_path)
    srv.httpd.RequestHandlerClass = type("E6BoundHandler", (E6Handler,), {"state": srv.state})
    srv.start()
    return srv


def hook_settings(run: Run, label: str) -> tuple[str, str]:
    out = os.path.join(run.logs, f"hooks-{label}.jsonl")
    base = f'"{fs(sys.executable)}" -I "{fs(HOOK)}" "{fs(out)}"'
    settings = {"hooks": {ev: [{"hooks": [{"type": "command", "command": f"{base} {ev}"}]}]
                          for ev in ("Stop", "StopFailure")}}
    path = os.path.join(run.root, f"hook-settings-{label}.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(settings, f, indent=1)
    return path, out


# ---- evidence helpers ------------------------------------------------------

def mock_hits(mock_log: str, key: str, t0: float) -> dict:
    rows = read_log(mock_log)
    acct = redact(key)
    hits = [r for r in rows if r.get("n") and r.get("account") == acct]
    resp = [r for r in rows if r.get("event") == "e6_resp" and r.get("account") == acct]
    out = []
    prev = None
    for r in hits:
        t = r["ts"] - t0
        out.append({"t": round(t, 2), "dt": None if prev is None else round(t - prev, 2),
                    "status": r.get("status"), "main_loop": r.get("main_loop"), "n_tools": r.get("n_tools"),
                    "stream": r.get("stream"), "model": r.get("model"),
                    "retry_count_hdr": (r.get("headers") or {}).get("x-stainless-retry-count"),
                    "user_text": (r.get("user_text") or "")[:50]})
        prev = t
    return {"count": len(hits), "main_loop_count": sum(1 for r in hits if r.get("main_loop")),
            "by_status": dict(Counter(str(r.get("status")) for r in hits)), "hits": out,
            "response_headers_first": resp[0]["headers"] if resp else None}


def trunc(o, n: int = 2500):
    s = json.dumps(o, ensure_ascii=False)
    return o if len(s) <= n else {"_truncated": s[:n]}


def analyze_stream(lines: list[tuple[float, str]]) -> dict:
    seq, events, unparsed = [], [], []
    init = result = None
    for t, line in lines:
        if not line.strip():
            continue
        try:
            obj = json.loads(line)
        except ValueError:
            unparsed.append(line[:300])
            continue
        ev = str(obj.get("type")) + ("/" + str(obj["subtype"]) if obj.get("subtype") else "")
        seq.append({"t": round(t, 2), "ev": ev})
        if ev == "system/init":
            init = obj
            continue
        if obj.get("type") == "result":
            result = obj
        events.append({"t": round(t, 2), "obj": trunc(obj, 6000)})
    return {
        "n_lines": len(lines), "event_sequence": seq,
        "event_type_counts": dict(Counter(e["ev"] for e in seq)),
        "init_fields": {k: (init or {}).get(k) for k in ("apiKeySource", "model", "claude_code_version",
                                                        "permissionMode")} if init else None,
        "api_retry_events": [e for e in events if e["obj"].get("subtype") == "api_retry"],
        "assistant_events": [e for e in events if e["obj"].get("type") == "assistant"],
        "rate_limit_events": [e for e in events if e["obj"].get("type") == "rate_limit_event"],
        "other_events": [e for e in events if e["obj"].get("type") not in ("assistant", "result", "rate_limit_event")
                         and e["obj"].get("subtype") != "api_retry"],
        "result_keys": sorted(result.keys()) if result else None,
        "result": result,
        "unparsed": unparsed,
    }


def transcript_report(prof) -> list[dict]:
    out = []
    for p in transcripts(prof.dir):
        rows = read_jsonl(p)
        err_rows = []
        for r in rows:
            if r.get("type") in ("api-request-blob", "api-request"):
                continue
            if r.get("type") == "system" or any("rror" in k for k in r.keys()):
                err_rows.append(r)
        out.append({
            "file": os.path.relpath(p, prof.dir), "n_rows": len(rows),
            "row_types": [str(r.get("type")) + ("/" + str(r["subtype"]) if r.get("subtype") else "") for r in rows],
            "error_or_system_rows": [trunc(r, 5000) for r in err_rows],
            "error_row_keys": [sorted(r.keys()) for r in err_rows],
            "summary": summarize_rows(rows, 25),
        })
    return out


def transcript_has_error(prof) -> bool:
    for p in transcripts(prof.dir):
        for r in read_jsonl(p):
            if r.get("isApiErrorMessage") or (r.get("type") == "assistant" and r.get("error")):
                return True
    return False


def read_hooks(path: str, t0: float) -> list[dict]:
    out = []
    for r in read_jsonl(path):
        out.append({"t": round(r.get("ts", 0) - t0, 2), "label": r.get("label"), "payload": trunc(r.get("payload"), 4000)})
    return out


# ---- runners -----------------------------------------------------------------

def run_print_stream(run: Run, prof, args: list[str], timeout: float, label: str) -> dict:
    t0 = time.time()
    p = subprocess.Popen([run.claude, *args], cwd=run.proj, env=run.env(prof), stdin=subprocess.DEVNULL,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out: list[tuple[float, str]] = []
    err: list[tuple[float, str]] = []

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
    secs = time.time() - t0
    with open(os.path.join(run.logs, f"{label}.stdout.jsonl"), "w", encoding="utf-8") as f:
        for _, line in out:
            f.write(line + "\n")
    with open(os.path.join(run.logs, f"{label}.stdout.timed.jsonl"), "w", encoding="utf-8") as f:
        for t, line in out:
            f.write(json.dumps({"t": round(t, 3), "line": line}, ensure_ascii=False) + "\n")
    with open(os.path.join(run.logs, f"{label}.stderr.txt"), "w", encoding="utf-8") as f:
        for t, line in err:
            f.write(f"[{t:7.2f}] {line}\n")
    return {"rc": rc, "secs": round(secs, 2), "timed_out": timed_out, "pid": p.pid, "t0": t0,
            "stream": analyze_stream(out), "stderr_tail": [l for _, l in err][-15:]}


def snippets(text: str, limit: int = 40) -> list[str]:
    seen, out = set(), []
    for m in SNIPPET_RX.finditer(text):
        s = " ".join(text[max(0, m.start() - 90): m.end() + 110].split())
        key = s[:60]
        if key in seen:
            continue
        seen.add(key)
        out.append(s)
        if len(out) >= limit:
            break
    return out


def run_tui(run: Run, prof, label: str, key: str, mock_log: str, settings: str | None, hooks_out: str | None,
            max_watch: float, quiet: float = 8.0) -> dict:
    args = ["--settings", settings] if settings else []
    pc = run.pty(prof, args, name=label)
    pc.start()
    rep: dict = {"pid": pc.pid}
    try:
        ready = pc.wait_for(TUI_READY, 60)
        rep["ready_secs"] = round(time.time() - pc.started_at, 1) if ready else None
        time.sleep(1.5)
        mark = pc.mark()
        pc.send("hi")
        time.sleep(0.4)
        t_sub = time.time()  # t=0 is the Enter key
        pc.send("\r")
        first: dict[str, float] = {}
        hook_seen: dict[str, float] = {}
        t_err_row = None
        last_hits = 0
        t_last_hit_change = time.time()
        done_at = None
        while time.time() - t_sub < max_watch:
            txt = strip_ansi(pc.text[mark:], " ")
            now = time.time() - t_sub
            for name, rx in TUI_PATTERNS.items():
                if name not in first and re.search(rx, txt, re.I):
                    first[name] = round(now, 2)
            if hooks_out:
                for h in read_jsonl(hooks_out):
                    lab = h.get("label")
                    if lab not in hook_seen:
                        hook_seen[lab] = round(h.get("ts", 0) - t_sub, 2)
            if t_err_row is None and transcript_has_error(prof):
                t_err_row = round(now, 2)
            n = sum(1 for r in read_log(mock_log) if r.get("n") and r.get("account") == redact(key))
            if n != last_hits:
                last_hits = n
                t_last_hit_change = time.time()
            if done_at is None and (hook_seen or t_err_row is not None or "reply" in first):
                done_at = time.time()
            if done_at and time.time() - done_at >= quiet and time.time() - t_last_hit_change >= quiet:
                break
            time.sleep(0.1)
        rep["watched_secs"] = round(time.time() - t_sub, 1)
        rep["first_seen_after_submit"] = first
        rep["hook_first_seen_after_submit"] = hook_seen
        rep["transcript_error_row_after_submit"] = t_err_row
        after = strip_ansi(pc.text[mark:], " ")
        rep["screen_snippets"] = snippets(after)
        rep["screen_tail"] = " ".join(after.split())[-1800:]
        rep["t_submit"] = t_sub
    finally:
        pc.send("\x03")
        time.sleep(0.6)
        pc.send("\x03")
        rep["exit_code"] = pc.wait_exit(15)
        pc.close()
    return rep


# ---- main ----------------------------------------------------------------------

def main(root: str, which: list[str]) -> int:
    run = Run(root)
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {k: v for (k, v, *_rest) in SCEN.values()})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    srv = make_mock(mock_log, scen)
    report: dict = {"claude": run.claude, "port": srv.port, "run_root": run.root, "scenarios": {}}
    try:
        for label in which:
            key, cfg, kind, hooks, timeout = SCEN[label]
            prof = run.profile(label, key, srv.base_url, trust=[run.proj])
            settings = hooks_out = None
            if hooks:
                settings, hooks_out = hook_settings(run, label)
            entry: dict = {"key": redact(key), "mock_cfg": cfg, "kind": kind, "hooks": hooks}
            try:
                if kind == "print":
                    args = ["-p", "hi", "--output-format", "stream-json", "--verbose"]
                    if settings:
                        args += ["--settings", settings]
                    r = run_print_stream(run, prof, args, timeout, label)
                    t0 = r.pop("t0")
                else:
                    r = run_tui(run, prof, label, key, mock_log, settings, hooks_out, timeout)
                    t0 = r.pop("t_submit", time.time())
                entry["run"] = r
                entry["mock"] = mock_hits(mock_log, key, t0)
                entry["transcripts"] = transcript_report(prof)
                if hooks_out:
                    entry["hooks_fired"] = read_hooks(hooks_out, t0)
            except Exception as e:  # keep going with other scenarios
                entry["exception"] = repr(e)
            with open(os.path.join(run.logs, f"{label}.report.json"), "w", encoding="utf-8") as f:
                json.dump(entry, f, ensure_ascii=False, indent=1)
            report["scenarios"][label] = entry
    finally:
        srv.stop()
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sel = sys.argv[2].split(",") if len(sys.argv) > 2 else ALL
    sys.exit(main(sys.argv[1], sel))
