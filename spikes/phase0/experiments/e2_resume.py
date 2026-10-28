"""e2_resume: resuming a session created under one CLAUDE_CONFIG_DIR from another.

    python -I spikes/phase0/experiments/e2_resume.py <empty run_root>

Profiles A and B (both echo), separate config dirs, no junction, fixed session id S.
Prints one JSON report. Evidence (pty logs, mock logs incl. a full-body log, transcript
summaries, event log) stays in the run root.
"""
from __future__ import annotations

import glob
import json
import os
import re
import sys
import time
import uuid

HARNESS = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "harness")
sys.path.insert(0, HARNESS)
from cc import Run, read_jsonl, session_ids  # noqa: E402
from mockapi import MockServer, read_log, write_scenario  # noqa: E402

MARKERS = ["turn one from A", "turn two from B", "turn three from A", "turn four via print",
           "fork turn", "concurrent from B", "after concurrent from A", "final check",
           "MOCK-REPLY[A]", "MOCK-REPLY[B]"]

READY = r"auto mode on|for shortcuts"


# ---- mock with a full-body log (harness copy is not edited) ------------------

class BodyLogMock(MockServer):
    """MockServer that also writes a compact copy of every /v1/messages body."""

    def __init__(self, log_path: str, scenario: str, body_log: str):
        super().__init__(log_path, scenario)
        base = self.httpd.RequestHandlerClass
        state = self.state

        def _messages(h, account, body):
            try:
                msgs = body.get("messages") or []
                blob = json.dumps(msgs, ensure_ascii=False)
                compact = []
                for m in msgs:
                    c = m.get("content")
                    texts = []
                    if isinstance(c, str):
                        texts.append(c[:120])
                    else:
                        for b in c or []:
                            if not isinstance(b, dict):
                                continue
                            if b.get("type") == "text":
                                t = b.get("text") or ""
                                texts.append("<sysrem>" if t.lstrip().startswith("<system-reminder>")
                                             else " ".join(t.split())[:120])
                            else:
                                texts.append(b.get("type"))
                    compact.append({"role": m.get("role"), "blocks": texts})
                entry = {"ts": time.time(), "account": "..." + account[-4:],
                         "main_loop": bool(body.get("tools")), "n_messages": len(msgs),
                         "session_hdr": h.headers.get("x-claude-code-session-id"),
                         "markers": [k for k in MARKERS if k in blob],
                         "messages": compact}
                line = json.dumps(entry, ensure_ascii=False)
                with state.lock:
                    with open(body_log, "a", encoding="utf-8") as f:
                        f.write(line + "\n")
            except Exception as e:  # never break the mock
                with open(body_log, "a", encoding="utf-8") as f:
                    f.write(json.dumps({"body_log_error": repr(e)}) + "\n")
            return base._messages(h, account, body)

        self.httpd.RequestHandlerClass = type("BodyLogHandler", (base,), {"_messages": _messages})


# ---- helpers -------------------------------------------------------------------

class Ctx:
    def __init__(self, root: str):
        self.run = Run(root)
        self.events: list[dict] = []
        self.ev_path = os.path.join(self.run.logs, "events.jsonl")

    def ev(self, step: str, what: str, **kw):
        e = {"t": round(time.time(), 2), "step": step, "what": what, **kw}
        self.events.append(e)
        with open(self.ev_path, "a", encoding="utf-8") as f:
            f.write(json.dumps(e, ensure_ascii=False) + "\n")

    def rel(self, p: str) -> str:
        try:
            return os.path.relpath(p, self.run.root)
        except ValueError:
            return p


def all_jsonl(run: Run) -> list[str]:
    return sorted(glob.glob(os.path.join(run.root, "profiles", "**", "*.jsonl"), recursive=True))


def snapshot(ctx: Ctx) -> dict:
    out = {}
    for p in all_jsonl(ctx.run):
        if p.endswith("history.jsonl"):
            continue
        rows = read_jsonl(p)
        out[ctx.rel(p)] = {"rows": len(rows), "sids": session_ids(rows)}
    return out


def diff_snap(a: dict, b: dict) -> dict:
    d = {}
    for k, v in b.items():
        if k not in a:
            d[k] = {"new_file": True, **v}
        elif v["rows"] != a[k]["rows"]:
            d[k] = {"rows_before": a[k]["rows"], "rows_after": v["rows"], "sids": v["sids"]}
    return d


def text_of(msg: dict) -> str:
    c = (msg or {}).get("content")
    if isinstance(c, str):
        return c
    parts = []
    for b in c or []:
        if isinstance(b, dict):
            if b.get("type") == "text":
                parts.append(b.get("text") or "")
            else:
                parts.append("[" + str(b.get("type")) + "]")
    return " | ".join(parts)


def conv(path: str) -> list[dict]:
    """user/assistant rows of a transcript, in file order."""
    out = []
    for i, r in enumerate(read_jsonl(path)):
        if r.get("type") not in ("user", "assistant"):
            continue
        t = text_of(r.get("message"))
        if t.lstrip().startswith("<"):
            t = t[:40]
        out.append({"i": i, "type": r["type"], "sid": r.get("sessionId"), "uuid": (r.get("uuid") or "")[:8],
                    "parent": (r.get("parentUuid") or "")[:8], "text": " ".join(t.split())[:90],
                    **({"isMeta": True} if r.get("isMeta") else {}),
                    **({"forkedFrom": r.get("forkedFrom")} if r.get("forkedFrom") else {})})
    return out


def row_types(path: str) -> dict:
    d: dict = {}
    for r in read_jsonl(path):
        k = f'{r.get("type")}@{(r.get("sessionId") or "-")[:8]}'
        d[k] = d.get(k, 0) + 1
    return d


def forks(path: str) -> list[dict]:
    """parents that have >1 user/assistant child (branching in the uuid tree)."""
    kids: dict = {}
    for r in read_jsonl(path):
        if r.get("type") in ("user", "assistant") and r.get("uuid"):
            kids.setdefault(r.get("parentUuid"), []).append(
                (r["uuid"][:8], r["type"], " ".join(text_of(r.get("message")).split())[:50]))
    return [{"parent": (p or "")[:8], "children": v} for p, v in kids.items() if len(v) > 1]


def sessions_dir(pdir: str) -> list[dict]:
    out = []
    for p in glob.glob(os.path.join(pdir, "sessions", "*")):
        try:
            with open(p, encoding="utf-8") as f:
                txt = f.read()
            try:
                out.append({"file": os.path.basename(p), "json": json.loads(txt)})
            except ValueError:
                out.append({"file": os.path.basename(p), "raw": txt[:300]})
        except OSError as e:
            out.append({"file": os.path.basename(p), "err": repr(e)})
    return out


def mock_since(path: str, ts: float, account: str | None = None) -> list[dict]:
    rows = [r for r in read_log(path) if r.get("ts", 0) >= ts]
    if account:
        rows = [r for r in rows if r.get("account") == "..." + account[-4:]]
    return rows


def compact_mock(rows: list[dict]) -> list[dict]:
    return [{k: r.get(k) for k in ("n", "account", "status", "main_loop", "n_messages", "user_text", "text")
             if r.get(k) is not None} | {"sess_hdr": (r.get("headers") or {}).get("x-claude-code-session-id")}
            for r in rows if r.get("path", "").startswith("/v1/messages") and r.get("kind") != "count_tokens"]


def body_since(path: str, ts: float) -> list[dict]:
    return [r for r in read_log(path) if r.get("ts", 0) >= ts]


def exit_tui(pc, step: str, ctx: Ctx) -> dict:
    m = pc.mark()
    pc.send("\x03")
    time.sleep(0.6)
    pc.send("\x03")
    code = pc.wait_exit(15)
    tail = pc.plain(m)
    rid = re.findall(r"claude\s+--resume\s+([0-9a-f-]{36})", tail) or re.findall(
        r"--resume\s+([0-9a-f-]{36})", pc.plain())
    pc.close()
    ctx.ev(step, "tui_exit", code=code, resume_hint=rid[-1:] if rid else None)
    return {"exit_code": code, "resume_hint": rid[-1] if rid else None,
            "exit_tail": " ".join(tail.split())[-300:]}


def screen_has(pc, s: str, since: int = 0) -> bool:
    flat = re.sub(r"\s+", "", pc.plain(since))
    return re.sub(r"\s+", "", s) in flat


# ---- experiment ----------------------------------------------------------------

def main(root: str) -> int:
    ctx = Ctx(root)
    run = ctx.run
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    body_log = os.path.join(run.logs, "mock_bodies.jsonl")
    srv = BodyLogMock(mock_log, scen, body_log)
    srv.start()
    S = str(uuid.uuid4())
    rep: dict = {"claude": run.claude, "run_root": run.root, "S": S, "steps": {}}
    st = rep["steps"]
    try:
        A = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        B = run.profile("B", "sk-mock-B", srv.base_url, trust=[run.proj])
        rep["profiles"] = {"A": ctx.rel(A.dir), "B": ctx.rel(B.dir)}

        # 1. TUI in A with --session-id S --------------------------------------
        s = st["1_A_new"] = {}
        snap0 = snapshot(ctx)
        t0 = time.time()
        pc = run.pty(A, ["--session-id", S], name="s1_A_tui")
        pc.start()
        s["ready"] = bool(pc.wait_for(READY, 60))
        time.sleep(1.5)
        m = pc.mark()
        pc.submit("turn one from A")
        s["reply"] = bool(pc.wait_for(r"MOCK-REPLY\[A\]\s*#\d+:\s*turn\s*one", 60, since=m))
        time.sleep(1.0)
        s["sessions_dir_while_open"] = sessions_dir(A.dir)
        s.update(exit_tui(pc, "1", ctx))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        P_cands = glob.glob(os.path.join(A.dir, "projects", "*", S + ".jsonl"))
        P = P_cands[0] if P_cands else None
        rep["P"] = ctx.rel(P) if P else None
        rep["P_len"] = len(P) if P else None
        if not P:
            rep["fatal"] = "P not found after step 1"
            return finish(rep, ctx, srv, mock_log)
        s["P_conv"] = conv(P)

        # 2a. TUI from B: -r S (by id) -------------------------------------------
        s = st["2a_B_tui_r_id"] = {}
        snap0 = snapshot(ctx)
        t0 = time.time()
        pc = run.pty(B, ["-r", S], name="s2a_B_tui_r_id")
        pc.start()
        # wait for exit, an error, a picker, or a ready prompt
        pc.wait_for(r"No conversation|not found|No sessions|Resume Session|Search|auto mode on|for shortcuts",
                    30)
        time.sleep(3.0)
        s["alive_after_wait"] = pc.is_alive()
        s["screen"] = " ".join(pc.plain().split())[-900:]
        if pc.is_alive():
            s.update(exit_tui(pc, "2a", ctx))
        else:
            s["exit_code"] = pc.wait_exit(1)
            pc.close()
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["sessions_B"] = sessions_dir(B.dir)

        # 2b. -p from B: -r S (by id) ---------------------------------------------
        s = st["2b_B_print_r_id"] = {}
        snap0 = snapshot(ctx)
        t0 = time.time()
        cp = run.run_print(B, ["-p", "-r", S, "print by id from B", "--output-format", "json"], timeout=90)
        s["rc"] = cp.returncode
        s["stdout"] = cp.stdout[-600:]
        s["stderr"] = cp.stderr[-600:]
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["mock"] = compact_mock(mock_since(mock_log, t0))

        # 3. TUI from B: -r <P abs> "turn two from B" ----------------------------
        s = st["3_B_tui_r_path"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        pc = run.pty(B, ["-r", P, "turn two from B"], name="s3_B_tui_r_path")
        pc.start()
        s["ready"] = bool(pc.wait_for(READY, 60))
        # NO keystrokes: wait for the auto-submitted prompt's reply
        got = pc.wait_for(r"MOCK-REPLY\[B\]\s*#\d+:\s*turn\s*two", 45)
        s["auto_submitted_reply_without_keypress"] = bool(got)
        s["secs_to_reply"] = round(time.time() - pc.started_at, 1)
        time.sleep(1.5)
        s["screen_shows_turn_one"] = screen_has(pc, "turn one from A")
        s["screen_shows_replyA"] = screen_has(pc, "MOCK-REPLY[A]")
        s["screen_excerpt"] = " ".join(pc.plain().split())[-700:]
        s["sessions_B_while_open"] = sessions_dir(B.dir)
        s.update(exit_tui(pc, "3", ctx))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["bodies_main"] = [b for b in body_since(body_log, t0) if b.get("main_loop")]

        # 4. TUI from A: -r S "turn three from A" -------------------------------
        s = st["4_A_tui_r_id"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        pc = run.pty(A, ["-r", S, "turn three from A"], name="s4_A_tui_r_id")
        pc.start()
        s["ready"] = bool(pc.wait_for(READY, 60))
        got = pc.wait_for(r"MOCK-REPLY\[A\]\s*#\d+:\s*turn\s*three", 45)
        s["auto_submitted_reply_without_keypress"] = bool(got)
        time.sleep(1.5)
        s["screen_shows_turn_two"] = screen_has(pc, "turn two from B")
        s["screen_shows_replyB"] = screen_has(pc, "MOCK-REPLY[B]")
        s.update(exit_tui(pc, "4", ctx))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["bodies_main"] = [b for b in body_since(body_log, t0) if b.get("main_loop")]

        # 5. -p from B: -r <P> "turn four via print" -------------------------------
        s = st["5_B_print_r_path"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        cp = run.run_print(B, ["-p", "-r", P, "turn four via print", "--output-format", "json"], timeout=90)
        s["rc"] = cp.returncode
        s.update(parse_print(cp))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["bodies_main"] = [b for b in body_since(body_log, t0) if b.get("main_loop")]

        # 6. --fork-session from B -----------------------------------------------
        s = st["6_B_print_fork"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        cp = run.run_print(B, ["-p", "-r", P, "--fork-session", "fork turn", "--output-format", "json"],
                           timeout=90)
        s["rc"] = cp.returncode
        s.update(parse_print(cp))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        new_files = [k for k, v in s["files_changed"].items() if v.get("new_file")]
        s["new_files"] = new_files
        s["new_file_conv"] = {k: conv(os.path.join(run.root, k)) for k in new_files}
        s["new_file_types"] = {k: row_types(os.path.join(run.root, k)) for k in new_files}
        s["mock"] = compact_mock(mock_since(mock_log, t0))
        s["bodies_main"] = [b for b in body_since(body_log, t0) if b.get("main_loop")]

        # 7. concurrency: A TUI open on S, B -p -r P ---------------------------------
        s = st["7_concurrent"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        pc = run.pty(A, ["-r", S], name="s7_A_tui_idle")
        pc.start()
        s["A_ready"] = bool(pc.wait_for(READY, 60))
        time.sleep(2.0)
        s["A_sessions_dir_while_open"] = sessions_dir(A.dir)
        s["P_rows_after_A_open"] = len(read_jsonl(P))
        t1 = time.time()
        cp = run.run_print(B, ["-p", "-r", P, "concurrent from B", "--output-format", "json"], timeout=90)
        s["B_rc"] = cp.returncode
        s["B"] = parse_print(cp)
        s["P_rows_after_B"] = len(read_jsonl(P))
        s["B_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["B_mock"] = compact_mock(mock_since(mock_log, t1))
        s["A_alive_after_B"] = pc.is_alive()
        m = pc.mark()
        t2 = time.time()
        time.sleep(1.0)
        pc.submit("after concurrent from A")
        s["A_reply"] = bool(pc.wait_for(r"MOCK-REPLY\[A\]\s*#\d+:\s*after\s*concurrent", 45, since=m))
        time.sleep(1.5)
        s["A_screen_after"] = " ".join(pc.plain(m).split())[-500:]
        s["A_screen_shows_B_turn"] = screen_has(pc, "concurrent from B")
        s.update({"A_" + k: v for k, v in exit_tui(pc, "7", ctx).items()})
        s["A_bodies_main"] = [b for b in body_since(body_log, t2) if b.get("main_loop")]
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_rows_before_after"] = [P_rows0, len(read_jsonl(P))]
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["P_forks"] = forks(P)

        # 8. which branch does a later resume follow? ------------------------------
        s = st["8_B_print_after_fork"] = {}
        snap0 = snapshot(ctx)
        P_rows0 = len(read_jsonl(P))
        t0 = time.time()
        cp = run.run_print(B, ["-p", "-r", P, "final check", "--output-format", "json"], timeout=90)
        s["rc"] = cp.returncode
        s.update(parse_print(cp))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["P_new_conv"] = [r for r in conv(P) if r["i"] >= P_rows0]
        s["bodies_main"] = [b for b in body_since(body_log, t0) if b.get("main_loop")]

        # 9. does B's own `-c` (continue latest in cwd) find S? ---------------------
        s = st["9_B_print_continue"] = {}
        snap0 = snapshot(ctx)
        t0 = time.time()
        cp = run.run_print(B, ["-p", "-c", "continue check", "--output-format", "json"], timeout=90)
        s["rc"] = cp.returncode
        s.update(parse_print(cp))
        s["files_changed"] = diff_snap(snap0, snapshot(ctx))
        s["bodies_main"] = [{k: b.get(k) for k in ("account", "n_messages", "session_hdr", "markers")}
                            for b in body_since(body_log, t0) if b.get("main_loop")]

        rep["final"] = {
            "history": {p.name: [{k: j.get(k) for k in ("display", "sessionId", "timestamp")}
                                 for j in read_jsonl(os.path.join(p.dir, "history.jsonl"))] for p in (A, B)},
            "all_transcripts": {ctx.rel(p): {"types": row_types(p)} for p in all_jsonl(run)
                                if not p.endswith("history.jsonl")},
            "P_conv": conv(P),
            "P_forks": forks(P),
            "B_projects": sorted(ctx.rel(p) for p in glob.glob(os.path.join(B.dir, "projects", "**", "*"),
                                                               recursive=True)),
            "A_projects": sorted(ctx.rel(p) for p in glob.glob(os.path.join(A.dir, "projects", "**", "*"),
                                                               recursive=True)),
        }
    except Exception as e:  # keep evidence on failure
        import traceback
        rep["exception"] = traceback.format_exc()[-2000:]
    return finish(rep, ctx, srv, mock_log)


def parse_print(cp) -> dict:
    out = {"stderr": cp.stderr[-400:]}
    try:
        j = json.loads(cp.stdout.strip().splitlines()[-1])
        out.update({k: j.get(k) for k in ("session_id", "result", "is_error", "subtype", "num_turns")})
    except (ValueError, IndexError):
        out["stdout"] = cp.stdout[-600:]
    return out


def finish(rep: dict, ctx: Ctx, srv, mock_log: str) -> int:
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
