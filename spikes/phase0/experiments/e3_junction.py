"""e3_junction: can profiles share one transcript store via an NTFS junction at <profile>\\projects?

    python -I spikes/phase0/experiments/e3_junction.py <empty run_root>

Canonical store <run>\\store\\projects. Profiles A and B get <profile>\\projects as junctions
to it BEFORE their first launch. Steps:
  1  A TUI (--permission-mode default --session-id S), mock tool_big -> ~120 KB shell output
  6  B TUI, "/resume" picker: does it list A's session? (run before step 2, store holds only A's work)
  2  B TUI `-r S "<prompt>"` by id: found through the junction? same file? tool exchange in request?
  3  same B TUI: another tool_big turn -> tool-results under the same session folder?
  4  scan TUI output / debug logs / transcripts for symlink/junction/path warnings
  5  rmtree_safe(B) keeps the store; naive deletions of a THROWAWAY profile+junction+store
  5b A resumes S by id in -p mode after B was deleted (store still usable, sees B's turns)
Prints one JSON report (also saved as <run>\\report.json). Evidence stays in the run root.
"""
from __future__ import annotations

import glob
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import threading
import time
import uuid
from http.server import ThreadingHTTPServer

HARNESS = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "harness")
sys.path.insert(0, HARNESS)
from cc import Run, read_jsonl, rmtree_safe, session_ids, summarize_rows, transcripts  # noqa: E402
from conpty import strip_ansi  # noqa: E402
from mockapi import Handler, MockServer, MockState, _last_user_text, _redact, read_log, write_scenario  # noqa: E402

READY = r"for shortcuts|\? for|Try \"|shift\+tab|>\s"
DIALOG_RX = r"Do you want to proceed\?|Do you want to (?:allow|run|make|create|read)"
TOOL_DONE = lambda r: r.get("text") == "MOCK-TOOL-DONE"  # noqa: E731
READ_DONE = lambda r: r.get("text") == "MOCK-READ-DONE"  # noqa: E731
ECHO_DONE = lambda r: bool(r.get("main_loop")) and (r.get("text") or "").startswith("MOCK-REPLY[")  # noqa: E731
SCAN_RX = re.compile(r"symlink|junction|reparse|realpath|EPERM|EACCES|ENOENT|EEXIST|ELOOP|EISDIR|ENOTDIR|"
                     r"EXDEV|No conversation found|not a directory|path too long", re.I)
TR_PATH_RX = re.compile(r"[A-Za-z]:[\\/][^\s\"'<>|]*?tool-results[\\/][^\s\"'<>|]+")

EVENTS: list[dict] = []
T0 = time.time()


def ev(step: str, what: str, **kw) -> None:
    EVENTS.append({"t": round(time.time() - T0, 2), "step": step, "what": what, **kw})


# ---- mock with a body-summary log (harness is not edited; subclass here) -----

_BODY_LOCK = threading.Lock()


def _block_text(c) -> str:
    if isinstance(c, list):
        return "".join(x.get("text", "") for x in c if isinstance(x, dict))
    return str(c or "")


def summarize_body(body: dict) -> tuple[list[dict], list[str]]:
    out, paths = [], []
    for m in body.get("messages") or []:
        c = m.get("content")
        blocks = []
        if isinstance(c, str):
            blocks.append({"t": "str", "text": c[:100]})
        else:
            for b in c or []:
                if not isinstance(b, dict):
                    continue
                t = b.get("type")
                if t == "text":
                    txt = b.get("text") or ""
                    if txt.lstrip().startswith("<system-reminder>"):
                        blocks.append({"t": "text", "sysrem": True, "len": len(txt)})
                    else:
                        blocks.append({"t": "text", "text": " ".join(txt.split())[:120]})
                elif t == "tool_use":
                    blocks.append({"t": "tool_use", "id": b.get("id"), "name": b.get("name"),
                                   "input": json.dumps(b.get("input"))[:120]})
                elif t == "tool_result":
                    s = _block_text(b.get("content"))
                    paths += TR_PATH_RX.findall(s)
                    blocks.append({"t": "tool_result", "tool_use_id": b.get("tool_use_id"), "len": len(s),
                                   "is_error": b.get("is_error"), "head": s[:420], "tail": s[-160:]})
                else:
                    blocks.append({"t": t})
        out.append({"role": m.get("role"), "blocks": blocks})
    return out, sorted(set(paths))


class XHandler(Handler):
    """Adds a body-summary log and one extra mode (harness mockapi.py is not edited):
      read_path  main-loop calls get a Read tool_use of scenario["path"]; once the
                 tool_result comes back, reply "MOCK-READ-DONE".
    """
    body_log: str = ""

    def _messages(self, account, body):
        try:
            msgs, paths = summarize_body(body)
            entry = {"ts": time.time(), "account": _redact(account), "main_loop": bool(body.get("tools")),
                     "n_messages": len(body.get("messages") or []), "tool_result_paths": paths, "messages": msgs}
            with _BODY_LOCK:
                with open(self.body_log, "a", encoding="utf-8") as f:
                    f.write(json.dumps(entry, ensure_ascii=False) + "\n")
        except Exception as e:  # never break the mock
            ev("mock", "body_log_error", err=repr(e))
        sc = self.state.scenario_for(account)
        if sc.get("mode") == "read_path":
            return self._read_path(account, body, sc)
        return super()._messages(account, body)

    def _read_path(self, account, body, sc):
        n, _hits = self.state.next_n(account)
        tools = [t.get("name") for t in (body.get("tools") or []) if isinstance(t, dict)]
        is_main = bool(tools)
        user_text, has_tr = _last_user_text(body)
        entry = {"ts": time.time(), "n": n, "method": "POST", "path": self.path, "account": _redact(account),
                 "mode": "read_path", "main_loop": is_main, "n_messages": len(body.get("messages") or []),
                 "has_tool_result": has_tr, "user_text": " ".join(user_text.split())[:200]}
        model = body.get("model") or "claude-mock"
        stream = bool(body.get("stream"))
        if is_main and not has_tr and "Read" in tools:
            entry.update(status=200, reply="tool_use", tool="Read", read_path=sc.get("path"))
            self.state.log(entry)
            self._reply(model, stream, tool_use=("Read", {"file_path": sc.get("path"), "limit": 3}))
            return
        text = "MOCK-READ-DONE" if has_tr else f"MOCK-REPLY[{account[-1:]}] #{n}: idle"
        entry.update(status=200, reply="text", text=text)
        self.state.log(entry)
        self._reply(model, stream, text=text)


class XMockServer(MockServer):
    def __init__(self, log_path: str, scenario_path: str, body_log: str):
        self.state = MockState(log_path, scenario_path)
        handler = type("BoundXHandler", (XHandler,), {"state": self.state, "body_log": body_log})
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.httpd.daemon_threads = True
        self.thread = None


# ---- TUI helpers ---------------------------------------------------------------

def seen(pc, pattern: str, since: int = 0):
    t = pc.text[since:]
    rx = re.compile(pattern, re.I)
    for cand in (strip_ansi(t), strip_ansi(t, " ")):
        m = rx.search(cand)
        if m:
            return m
    return None


def mock_count(mock_log: str, acct: str, pred) -> int:
    return sum(1 for r in read_log(mock_log) if (r.get("account") or "").endswith(acct) and pred(r))


def mock_done(mock_log: str, acct: str, pred):
    """Completion is judged from the mock log, not the screen: the TUI redraws old turns."""
    base = mock_count(mock_log, acct, pred)
    return lambda: mock_count(mock_log, acct, pred) > base


def drive(pc, done, timeout: float, since: int, label: str, fail_rx: str | None = None) -> tuple[bool, list[dict]]:
    """Wait until done() is true; accept each permission dialog with Enter (highlighted '1. Yes')."""
    deadline = time.time() + timeout
    dlg_since = since
    dialogs: list[dict] = []
    while time.time() < deadline:
        if done():
            time.sleep(1.5)  # let the TUI finish rendering the turn
            return True, dialogs
        if fail_rx and seen(pc, fail_rx, since):
            return False, dialogs
        m = seen(pc, DIALOG_RX, dlg_since)
        if m:
            time.sleep(0.8)
            screen = mask(squash(strip_ansi(pc.text[dlg_since:], " ")))
            i = screen.rfind("Do you want")
            dialogs.append({"label": label, "match": m.group(0),
                            "dialog_text": screen[max(0, i - 420): i + 120] if i >= 0 else screen[-500:],
                            "action": "sent Enter (accepts highlighted option '1. Yes')"})
            ev(label, "permission_dialog", match=m.group(0))
            pc.send("\r")
            time.sleep(0.5)
            dlg_since = pc.mark()
        if not pc.is_alive():
            break
        time.sleep(0.3)
    return bool(done()), dialogs


def read_results(body_log: str, nb: int) -> list[dict]:
    """tool_result blocks answering the mock's Read tool_use, in bodies logged after index nb."""
    later = [r for r in read_log(body_log)[nb:] if r.get("main_loop")]
    ids = {blk.get("id") for r in later for msg in r["messages"] for blk in msg["blocks"]
           if blk.get("t") == "tool_use" and blk.get("name") == "Read"}
    return [blk for r in later for msg in r["messages"] for blk in msg["blocks"]
            if blk.get("t") == "tool_result" and blk.get("tool_use_id") in ids]


def classify_read(res: list[dict]) -> dict:
    if not res:
        return {"tool_result": None}
    h = res[-1].get("head", "")
    return {"tool_result": {"is_error": res[-1].get("is_error"), "len": res[-1].get("len"), "head": mask(h)[:300]},
            # a 120 KB single-line file: either content or the token-limit refusal proves the file was opened
            "file_reached": ("xxxxxxxx" in h) or ("exceeds maximum allowed tokens" in h),
            "file_missing": bool(re.search(r"does not exist|ENOENT|no such file", h, re.I))}


def read_probe(pc, done, body_log: str, nb: int, mark: int, label: str) -> dict:
    out: dict = {}
    out["done_seen"], out["permission_dialogs"] = drive(pc, done, 60, mark, label)
    out["permission_prompted"] = bool(out["permission_dialogs"])
    out.update(classify_read(read_results(body_log, nb)))
    return out


def quit_tui(pc) -> dict:
    m = pc.mark()
    pc.send("\x03")
    time.sleep(0.6)
    pc.send("\x03")
    code = pc.wait_exit(15)
    pc.close()
    tail = strip_ansi(pc.text[m:], " ")
    hint = re.search(r"claude\s*--resume\s*([0-9a-f-]{36})", tail) or re.search(
        r"claude\s*--resume\s*([0-9a-f-]{36})", strip_ansi(pc.text[m:]))
    return {"exit_code": code, "resume_hint": hint.group(1) if hint else None,
            "tail": " ".join(tail.split())[-300:]}


def squash(s: str) -> str:
    return " ".join(s.split())


def junction(link: str, target: str) -> None:
    """Copy of cc.junction that decodes mklink output with the ANSI code page.

    cc.junction uses text=True (UTF-8 under Python 3.15), so on a Korean-locale Windows the
    mklink success message (cp949) raises UnicodeDecodeError in subprocess reader threads.
    """
    if os.path.lexists(link):
        raise FileExistsError(link)
    os.makedirs(target, exist_ok=True)
    r = subprocess.run(["cmd", "/c", "mklink", "/J", link, target], capture_output=True, text=True,
                       encoding="mbcs", errors="replace")
    if r.returncode != 0:
        raise OSError(f"mklink /J failed: {r.stdout} {r.stderr}")


# ---- filesystem helpers --------------------------------------------------------

def tree(root: str) -> dict[str, list]:
    out = {}
    for dp, _dns, fns in os.walk(root):
        for fn in fns:
            p = os.path.join(dp, fn)
            try:
                with open(p, "rb") as f:
                    h = hashlib.sha256(f.read()).hexdigest()[:16]
                out[os.path.relpath(p, root)] = [os.path.getsize(p), h]
            except OSError as e:
                out[os.path.relpath(p, root)] = ["ERR", repr(e)]
    return out


def link_info(p: str) -> dict:
    d = {"exists": os.path.exists(p), "lexists": os.path.lexists(p),
         "isjunction": os.path.isjunction(p), "islink": os.path.islink(p)}
    try:
        d["readlink"] = os.readlink(p)
    except OSError as e:
        d["readlink"] = f"ERR {e.__class__.__name__}"
    return d


def toplevel(pdir: str) -> list[str]:
    out = []
    for n in sorted(os.listdir(pdir)):
        p = os.path.join(pdir, n)
        tag = "J" if os.path.isjunction(p) else ("D" if os.path.isdir(p) else "F")
        out.append(f"{tag}:{n}")
    return out


def rel_store(p: str, store: str) -> str:
    return os.path.relpath(p, store)


MASK: list[str] = []  # run-root spellings; the run root itself contains "junction"


def mask(text: str) -> str:
    for m in MASK:
        text = text.replace(m, "<RUN>")
    text = re.sub(r"C--Users-[A-Za-z0-9-]*?-proj", "<SLUG>", text)
    return re.sub(r"e3[_-]j\s*u\s*n\s*c\s*t\s*i\s*o\s*n-\s*\d+", "<RUNID>", text)  # wrapped TUI lines too


def scan_text(label: str, text: str, limit: int = 12) -> list[dict]:
    hits = []
    for line in mask(text).splitlines():
        if SCAN_RX.search(line):
            hits.append({"src": label, "line": squash(line)[:260]})
            if len(hits) >= limit:
                break
    return hits


def mock_rows(mock_log: str, acct: str | None = None) -> list[dict]:
    keep = ("n", "account", "mode", "main_loop", "n_messages", "has_tool_result", "user_text", "reply",
            "tool", "status", "text", "note")
    rows = [r for r in read_log(mock_log) if r.get("method") == "POST" and "n" in r]
    if acct:
        rows = [r for r in rows if (r.get("account") or "").endswith(acct)]
    return [{k: r.get(k) for k in keep if r.get(k) is not None} for r in rows]


def body_rows(body_log: str) -> list[dict]:
    return read_log(body_log)


# ---- naive-deletion characterization (throwaway trees only) -------------------

def make_throwaway(base: str) -> tuple[str, str, str]:
    """base/{store/projects/slug/<sid>.jsonl + <sid>/tool-results/t.txt, profile/projects -> junction}."""
    store = os.path.join(base, "store", "projects")
    sdir = os.path.join(store, "slug", "sid")
    os.makedirs(os.path.join(sdir, "tool-results"), exist_ok=True)
    with open(os.path.join(store, "slug", "sid.jsonl"), "w", encoding="utf-8") as f:
        f.write('{"type":"user"}\n')
    with open(os.path.join(sdir, "tool-results", "t.txt"), "w", encoding="utf-8") as f:
        f.write("x" * 1000)
    prof = os.path.join(base, "profile")
    os.makedirs(prof, exist_ok=True)
    with open(os.path.join(prof, "settings.json"), "w", encoding="utf-8") as f:
        f.write("{}")
    junction(os.path.join(prof, "projects"), store)
    return base, prof, store


def naive_os_walk_delete(path: str) -> None:
    for dp, dns, fns in os.walk(path, topdown=False):
        for fn in fns:
            os.unlink(os.path.join(dp, fn))
        for d in dns:
            full = os.path.join(dp, d)
            try:
                os.rmdir(full)
            except OSError:
                pass
    os.rmdir(path)


def naive_tests(root: str) -> dict:
    out = {}
    tbase = os.path.join(root, "throwaway")
    sp = dict(capture_output=True, text=True, encoding="mbcs", errors="replace")
    ps = ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command"]
    methods = {
        "python_shutil_rmtree": lambda p: shutil.rmtree(p),
        "python_os_walk_bottomup_unlink": naive_os_walk_delete,
        "cmd_rmdir_s_q": lambda p: subprocess.run(["cmd", "/c", "rmdir", "/s", "/q", p], timeout=60, **sp),
        "ps51_remove_item_recurse_force": lambda p: subprocess.run(
            ps + [f"Remove-Item -LiteralPath '{p}' -Recurse -Force"], timeout=90, **sp),
        "ps51_dotnet_directory_delete": lambda p: subprocess.run(
            ps + [f"try {{ [System.IO.Directory]::Delete('{p}', $true); 'OK' }} "
                  f"catch {{ 'EXC ' + $_.Exception.GetType().FullName + ': ' + $_.Exception.Message }}"],
            timeout=90, **sp),
    }
    for name, fn in methods.items():
        base = os.path.join(tbase, name)
        try:
            _, prof, store = make_throwaway(base)
            before = tree(store)
            err = None
            try:
                r = fn(prof)
                if isinstance(r, subprocess.CompletedProcess):
                    err = squash((r.stdout or "") + " " + (r.stderr or ""))[:300] or None
            except Exception as e:  # record, do not hide
                err = repr(e)[:300]
            after = tree(store) if os.path.isdir(store) else {}
            out[name] = {"store_files_before": len(before), "store_files_after": len(after),
                         "store_intact": before == after, "profile_gone": not os.path.lexists(prof),
                         "error": err, "lost": sorted(set(before) - set(after))}
        except Exception as e:
            out[name] = {"setup_error": repr(e)}
    return out


# ---- main ----------------------------------------------------------------------

def main(root: str) -> int:
    run = Run(root)
    MASK.extend([run.root, run.root.replace("\\", "\\\\"), run.root.replace("\\", "/"),
                 "//" + run.root[0].lower() + run.root[2:].replace("\\", "/")])
    rep: dict = {"id": "e3_junction", "claude": run.claude, "root": run.root, "python": sys.version.split()[0]}
    scen = os.path.join(run.root, "scenario.json")
    write_scenario(scen, {"sk-mock-A": {"mode": "tool_big"}, "sk-mock-B": {"mode": "echo"}})
    mock_log = os.path.join(run.logs, "mock.jsonl")
    body_log = os.path.join(run.logs, "bodies.jsonl")
    srv = XMockServer(mock_log, scen, body_log)
    srv.start()
    store = os.path.join(run.root, "store", "projects")
    os.makedirs(store, exist_ok=True)
    S = str(uuid.uuid4())
    rep["session_id"] = S
    dbg = lambda n: os.path.join(run.logs, f"debug-{n}.txt")  # noqa: E731
    try:
        # ---- setup ---------------------------------------------------------
        a = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])
        # B deliberately has no "Read" allow rule, to see whether reading sidecars prompts (step 3b)
        b = run.profile("B", "sk-mock-B", srv.base_url, trust=[run.proj],
                        settings_extra={"permissions": {"allow": ["Bash", "PowerShell", "Write", "Edit"]}})
        junction(os.path.join(a.dir, "projects"), store)
        junction(os.path.join(b.dir, "projects"), store)
        rep["setup"] = {"A_projects": link_info(os.path.join(a.dir, "projects")),
                        "B_projects": link_info(os.path.join(b.dir, "projects")), "store": store}
        ev("setup", "junctions made")

        # ---- step 1: A tool_big --------------------------------------------
        s1: dict = {}
        pc = run.pty(a, ["--permission-mode", "default", "--session-id", S, "--debug-file", dbg("s1_A")],
                     name="s1_A_tool")
        pc.start()
        s1["ready"] = bool(pc.wait_for(READY, 60))
        s1["ready_secs"] = round(time.time() - pc.started_at, 1)
        time.sleep(2)
        m = pc.mark()
        done = mock_done(mock_log, "k-A", TOOL_DONE)
        pc.submit("E3 step1 big tool from A")
        ev("s1", "submitted")
        s1["tool_done_seen"], s1["permission_dialogs"] = drive(pc, done, 120, m, "s1")
        ev("s1", "done", ok=s1["tool_done_seen"])
        time.sleep(2)
        s1["screen_after_prompt"] = squash(strip_ansi(pc.text[m:], " "))[-1500:]
        s1["quit"] = quit_tui(pc)
        s1["mock_A"] = mock_rows(mock_log, "k-A")
        s1["mock_has_tool_result_true"] = any(r.get("has_tool_result") for r in s1["mock_A"])
        s1["store_jsonl"] = [rel_store(p, store) for p in glob.glob(os.path.join(store, "*", "*.jsonl"))]
        tr_dirs = glob.glob(os.path.join(store, "*", S, "tool-results"))
        s1["tool_results_dir"] = [rel_store(p, store) for p in tr_dirs]
        s1["tool_results_files"] = {rel_store(p, store): os.path.getsize(p)
                                    for p in glob.glob(os.path.join(store, "*", S, "tool-results", "*"))}
        s1["session_folder_entries"] = {rel_store(p, store): sorted(os.listdir(p))
                                        for p in glob.glob(os.path.join(store, "*", S)) if os.path.isdir(p)}
        s1["A_sees_same_via_junction"] = bool(transcripts(a.dir)) and \
            sorted(os.path.relpath(p, os.path.join(a.dir, "projects")) for p in transcripts(a.dir)) == \
            sorted(s1["store_jsonl"])
        s1["A_projects_after"] = link_info(os.path.join(a.dir, "projects"))
        s1["A_toplevel"] = toplevel(a.dir)
        tpaths = glob.glob(os.path.join(store, "*", f"{S}.jsonl"))
        if tpaths:
            rows = read_jsonl(tpaths[0])
            s1["transcript_rows"] = len(rows)
            s1["transcript_session_ids"] = session_ids(rows)
            s1["transcript_tool_result_paths"] = sorted(set(TR_PATH_RX.findall(open(tpaths[0], encoding="utf-8",
                                                                                     errors="replace").read())))
            s1["transcript_summary"] = summarize_rows([r for r in rows if r.get("type") in ("user", "assistant")], 12)
        bodies = [r for r in body_rows(body_log) if (r.get("account") or "").endswith("k-A") and r.get("main_loop")]
        s1["A_main_bodies_tool_result_paths"] = sorted({p for r in bodies for p in r.get("tool_result_paths", [])})
        if bodies:
            last = bodies[-1]
            s1["A_last_main_body_tool_results"] = [blk for msg in last["messages"] for blk in msg["blocks"]
                                                   if blk.get("t") == "tool_result"]
        rep["step1_A_tool_big"] = s1

        # ---- step 6: /resume picker in B (before step 2) ----------------------
        s6: dict = {}
        pc = run.pty(b, ["--debug-file", dbg("s6_B")], name="s6_B_resume_picker")
        pc.start()
        s6["ready"] = bool(pc.wait_for(READY, 60))
        time.sleep(2)
        m = pc.mark()
        pc.send("/resume")
        time.sleep(1.2)
        s6["autocomplete"] = squash(strip_ansi(pc.text[m:], " "))[-400:]
        m2 = pc.mark()
        pc.send("\r")
        s6["picker_marker"] = bool(pc.wait_for(r"E3\s*step1|Resume\s*Session|No conversations|Search", 15, since=m2))
        time.sleep(3)
        pic_sp = strip_ansi(pc.text[m2:], " ")
        pic_raw = strip_ansi(pc.text[m2:])
        s6["picker_text"] = squash(pic_sp)[-1800:]
        s6["lists_A_session_prompt"] = bool(re.search(r"E3\s*step1\s*big\s*tool\s*from\s*A", pic_sp)
                                            or re.search(r"E3step1bigtoolfromA", pic_raw))
        s6["lists_session_id_prefix"] = S[:8] in pic_sp or S[:8] in pic_raw
        s6["no_conversations_msg"] = bool(re.search(r"No conversations", pic_sp))
        pc.send("\x1b")
        time.sleep(1.2)
        s6["after_esc"] = squash(strip_ansi(pc.text[m2:], " "))[-300:]
        s6["quit"] = quit_tui(pc)
        s6["store_jsonl_after"] = [rel_store(p, store) for p in glob.glob(os.path.join(store, "*", "*.jsonl"))]
        extra = [p for p in glob.glob(os.path.join(store, "*", "*.jsonl")) if S not in os.path.basename(p)]
        s6["extra_session_files_row_types"] = {
            os.path.basename(p): [f"{r.get('type')}/{r.get('subtype')}" if r.get("subtype") else r.get("type")
                                  for r in read_jsonl(p)] for p in extra}
        rep["step6_B_resume_picker"] = s6

        # ---- step 2: B resumes S by id ------------------------------------------
        s2: dict = {}
        before_files = set(glob.glob(os.path.join(store, "*", "*.jsonl")))
        before_tree = tree(store)
        n_before = len(read_log(mock_log))
        done = mock_done(mock_log, "k-B", ECHO_DONE)
        pc = run.pty(b, ["--permission-mode", "default", "-r", S, "E3 step2 continue from B",
                         "--debug-file", dbg("s2_B")], name="s2s3_B_resume")
        pc.start()
        ev("s2", "B launched with -r S")
        s2["reply_seen"], s2["permission_dialogs"] = drive(pc, done, 90, 0, "s2", fail_rx=r"No conversation found")
        s2["no_conversation_found"] = bool(seen(pc, r"No conversation found"))
        ev("s2", "reply", ok=s2["reply_seen"])
        time.sleep(2)
        s2["screen"] = squash(strip_ansi(pc.text, " "))[-1500:]
        after_files = set(glob.glob(os.path.join(store, "*", "*.jsonl")))
        s2["new_jsonl_files"] = [rel_store(p, store) for p in sorted(after_files - before_files)]
        s2["jsonl_files"] = [rel_store(p, store) for p in sorted(after_files)]
        tpath = glob.glob(os.path.join(store, "*", f"{S}.jsonl"))
        if tpath:
            rows = read_jsonl(tpath[0])
            s2["S_file_rows"] = len(rows)
            s2["S_file_session_ids"] = session_ids(rows)
            s2["S_file_grew"] = os.path.getsize(tpath[0]) > before_tree.get(rel_store(tpath[0], store), [0])[0]
            s2["S_contains_step2_prompt"] = "E3 step2 continue from B" in open(tpath[0], encoding="utf-8",
                                                                               errors="replace").read()
        new_mock = [r for r in read_log(mock_log)[n_before:] if r.get("method") == "POST" and "n" in r]
        s2["mock_B"] = [{k: r.get(k) for k in ("n", "account", "main_loop", "n_messages", "has_tool_result",
                                               "user_text", "reply", "headers") if r.get(k) is not None}
                        for r in new_mock]
        b_bodies = [r for r in body_rows(body_log) if (r.get("account") or "").endswith("k-B") and r.get("main_loop")]
        if b_bodies:
            first = b_bodies[0]
            blocks = [(msg["role"], blk) for msg in first["messages"] for blk in msg["blocks"]]
            s2["B_first_main_n_messages"] = first["n_messages"]
            s2["B_first_main_has_tool_use"] = any(blk.get("t") == "tool_use" for _, blk in blocks)
            s2["B_first_main_has_tool_result"] = any(blk.get("t") == "tool_result" for _, blk in blocks)
            s2["B_first_main_has_A_prompt"] = any("E3 step1 big tool from A" in (blk.get("text") or "")
                                                  for _, blk in blocks)
            s2["B_first_main_tool_results"] = [blk for _, blk in blocks if blk.get("t") == "tool_result"]
            s2["B_first_main_tool_result_paths"] = first.get("tool_result_paths")
            s2["B_first_main_shape"] = [[msg["role"], [blk.get("t") + (":" + blk.get("text", "")[:40]
                                                                       if blk.get("t") == "text" and not blk.get("sysrem")
                                                                       else "") for blk in msg["blocks"]]]
                                        for msg in first["messages"]]
        rep["step2_B_resume_by_id"] = s2

        # ---- step 3: B tool_big through the junction ----------------------------
        s3: dict = {}
        write_scenario(scen, {"sk-mock-A": {"mode": "tool_big"}, "sk-mock-B": {"mode": "tool_big"}})
        tr_before = set(glob.glob(os.path.join(store, "*", "*", "tool-results", "*")))
        time.sleep(1)
        m = pc.mark()
        done = mock_done(mock_log, "k-B", TOOL_DONE)
        pc.submit("E3 step3 big tool from B")
        ev("s3", "submitted")
        s3["tool_done_seen"], s3["permission_dialogs"] = drive(pc, done, 120, m, "s3")
        ev("s3", "done", ok=s3["tool_done_seen"])
        time.sleep(2)
        s3["screen_after_prompt"] = squash(strip_ansi(pc.text[m:], " "))[-1200:]
        tr_after = set(glob.glob(os.path.join(store, "*", "*", "tool-results", "*")))

        # ---- step 3b: can B's Read tool open the sidecars? (B has no Read allow rule) --
        s3b: dict = {"B_permissions_allow": json.load(open(b.settings_json, encoding="utf-8"))["permissions"]["allow"]}
        b_bodies = [r for r in body_rows(body_log) if (r.get("account") or "").endswith("k-B") and r.get("main_loop")]
        all_paths = sorted({p for r in b_bodies for p in r.get("tool_result_paths", [])})
        a_path = next((p for p in all_paths if os.sep + "profiles" + os.sep + "A" + os.sep in p), None)
        b_path = next((p for p in all_paths if os.sep + "profiles" + os.sep + "B" + os.sep in p), None)
        store_path = None
        if a_path:
            store_path = os.path.join(store, a_path.split(os.sep + "projects" + os.sep, 1)[1])
        probes = [("A_profile_path_of_A_sidecar", a_path), ("B_profile_path_of_B_sidecar", b_path),
                  ("store_real_path_of_A_sidecar", store_path)]
        s3b["probes"] = []
        for label, path in probes:
            pr: dict = {"label": label, "path": mask(path or "")}
            if not path:
                pr["skipped"] = "path not found in B's requests"
                s3b["probes"].append(pr)
                continue
            write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "read_path", "path": path}})
            nb = len(body_rows(body_log))
            time.sleep(0.5)
            m = pc.mark()
            done = mock_done(mock_log, "k-B", READ_DONE)
            pc.submit(f"E3 step3b read {label}")
            pr.update(read_probe(pc, done, body_log, nb, m, "s3b:" + label))
            s3b["probes"].append(pr)
        rep["step3b_B_read_sidecars"] = s3b
        s3["quit"] = quit_tui(pc)
        s2["quit"] = s3["quit"]
        s3["new_tool_results"] = {rel_store(p, store): os.path.getsize(p) for p in sorted(tr_after - tr_before)}
        s3["all_tool_results"] = {rel_store(p, store): os.path.getsize(p) for p in sorted(tr_after)}
        s3["new_under_session_S"] = all(os.sep + S + os.sep in p for p in (tr_after - tr_before)) and bool(
            tr_after - tr_before)
        s3["mock_B_has_tool_result_true"] = any(r.get("has_tool_result") for r in mock_rows(mock_log, "k-B"))
        b_bodies = [r for r in body_rows(body_log) if (r.get("account") or "").endswith("k-B") and r.get("main_loop")]
        s3["B_main_bodies_tool_result_paths"] = sorted({p for r in b_bodies for p in r.get("tool_result_paths", [])})
        tpath = glob.glob(os.path.join(store, "*", f"{S}.jsonl"))
        if tpath:
            txt = open(tpath[0], encoding="utf-8", errors="replace").read()
            rows = read_jsonl(tpath[0])
            s3["S_file_session_ids"] = session_ids(rows)
            s3["S_transcript_tool_result_paths"] = sorted(set(TR_PATH_RX.findall(txt)))
            s3["S_transcript_summary"] = summarize_rows([r for r in rows if r.get("type") in ("user", "assistant")],
                                                        16)
        s3["jsonl_files"] = [rel_store(p, store) for p in sorted(glob.glob(os.path.join(store, "*", "*.jsonl")))]
        s3["B_projects_after"] = link_info(os.path.join(b.dir, "projects"))
        s3["A_projects_after"] = link_info(os.path.join(a.dir, "projects"))
        s3["B_toplevel"] = toplevel(b.dir)
        s3["A_toplevel"] = toplevel(a.dir)
        s3["B_sessions_meta"] = [{k: v for k, v in d.items() if k != "_file"} for d in b.sessions()]
        s3["A_claude_json_projects"] = list((a.read_claude_json().get("projects") or {}).keys())
        s3["B_claude_json_projects"] = list((b.read_claude_json().get("projects") or {}).keys())
        rep["step3_B_tool_big"] = s3

        # ---- control: profile C WITHOUT junction, same permissions as B ---------
        # Does reading one's own sidecar prompt even without a junction? (discriminates step 3b)
        cc_: dict = {}
        c = run.profile("C", "sk-mock-C", srv.base_url, trust=[run.proj],
                        settings_extra={"permissions": {"allow": ["Bash", "PowerShell", "Write", "Edit"]}})
        SC = str(uuid.uuid4())
        cc_["session_id"] = SC
        write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"},
                              "sk-mock-C": {"mode": "tool_big"}})
        pc = run.pty(c, ["--permission-mode", "default", "--session-id", SC, "--debug-file", dbg("ctrl_C")],
                     name="ctrl_C")
        pc.start()
        cc_["ready"] = bool(pc.wait_for(READY, 60))
        time.sleep(2)
        m = pc.mark()
        done = mock_done(mock_log, "k-C", TOOL_DONE)
        pc.submit("E3 control C big tool")
        cc_["tool_done_seen"], cc_["permission_dialogs"] = drive(pc, done, 120, m, "ctrlC")
        c_bodies = [r for r in body_rows(body_log) if (r.get("account") or "").endswith("k-C") and r.get("main_loop")]
        c_path = next((p for r in c_bodies for p in r.get("tool_result_paths", [])
                       if os.sep + "profiles" + os.sep + "C" + os.sep in p), None)
        cc_["C_projects"] = link_info(os.path.join(c.dir, "projects"))
        cc_["sidecar_path"] = mask(c_path or "")
        cc_["sidecar_exists"] = bool(c_path) and os.path.isfile(c_path)
        if c_path:
            write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"},
                                  "sk-mock-C": {"mode": "read_path", "path": c_path}})
            nb = len(body_rows(body_log))
            time.sleep(0.5)
            m = pc.mark()
            done = mock_done(mock_log, "k-C", READ_DONE)
            pc.submit("E3 control C read own sidecar")
            cc_["read_own_sidecar"] = read_probe(pc, done, body_log, nb, m, "ctrlC:read_own")
        cc_["quit"] = quit_tui(pc)
        cc_["store_untouched_by_C"] = not glob.glob(os.path.join(store, "*", SC + "*"))
        rep["control_C_no_junction"] = cc_

        # ---- mitigation probes: junctioned profiles D/E with a store read grant ---
        # D: permissions.additionalDirectories=[store]; E: allow Read(//c/.../store/projects/**)
        mit: dict = {}
        rel = a_path.split(os.sep + "projects" + os.sep, 1)[1] if a_path else None
        posix_store = "//" + store[0].lower() + store[2:].replace("\\", "/")
        base_allow = ["Bash", "PowerShell", "Write", "Edit"]
        profiles_root = os.path.join(run.root, "profiles")
        posix_profiles = "//" + profiles_root[0].lower() + profiles_root[2:].replace("\\", "/")
        # F: both the junction-side paths and the real store path, injected via --settings (launcher style)
        f_settings = {"permissions": {"allow": [f"Read({posix_profiles}/**)", f"Read({posix_store}/**)"]}}
        variants = [("D", {"permissions": {"allow": base_allow, "additionalDirectories": [store]}}, None),
                    ("E", {"permissions": {"allow": base_allow + [f"Read({posix_store}/**)"]}}, None),
                    ("F", {"permissions": {"allow": base_allow}}, f_settings)]
        for name, extra, flag in variants:
            mv: dict = {"settings_permissions": json.loads(mask(json.dumps(extra["permissions"])))}
            key = f"sk-mock-{name}"
            pz = run.profile(name, key, srv.base_url, trust=[run.proj], settings_extra=extra)
            junction(os.path.join(pz.dir, "projects"), store)
            flag_args = []
            if flag:
                fpath = os.path.join(run.root, f"flag-settings-{name}.json")
                with open(fpath, "w", encoding="utf-8") as f:
                    json.dump(flag, f, indent=1)
                flag_args = ["--settings", fpath]
                mv["flag_settings"] = json.loads(mask(json.dumps(flag)))
            write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}, key: {"mode": "echo"}})
            done = mock_done(mock_log, f"k-{name}", ECHO_DONE)
            pc = run.pty(pz, ["--permission-mode", "default", *flag_args, "-r", S, f"E3 mitigation {name} resume",
                              "--debug-file", dbg(f"mit_{name}")], name=f"mit_{name}")
            pc.start()
            mv["resume_reply_seen"], mv["resume_dialogs"] = drive(pc, done, 90, 0, f"mit{name}",
                                                                  fail_rx=r"No conversation found")
            probes = [("A_profile_path_of_A_sidecar", a_path),
                      (f"{name}_profile_path_of_A_sidecar", os.path.join(pz.dir, "projects", rel) if rel else None)]
            mv["probes"] = []
            for label, path in probes:
                pr = {"label": label, "path": mask(path or "")}
                if path:
                    write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"},
                                          key: {"mode": "read_path", "path": path}})
                    nb = len(body_rows(body_log))
                    time.sleep(0.5)
                    m = pc.mark()
                    done = mock_done(mock_log, f"k-{name}", READ_DONE)
                    pc.submit(f"E3 mitigation {name} read {label}")
                    pr.update(read_probe(pc, done, body_log, nb, m, f"mit{name}:{label}"))
                mv["probes"].append(pr)
            mv["quit"] = quit_tui(pc)
            mit[name] = mv
        rep["mitigation_store_read_grant"] = mit

        # ---- step 4: warnings scan ---------------------------------------------
        s4: dict = {"hits": [], "transcript_error_rows": []}
        for p in sorted(glob.glob(os.path.join(run.logs, "*.txt"))):
            try:
                txt = open(p, encoding="utf-8", errors="replace").read()
            except OSError:
                continue
            s4["hits"] += scan_text(os.path.basename(p), txt, 15)
        for p in sorted(glob.glob(os.path.join(store, "*", "*.jsonl"))):
            txt = open(p, encoding="utf-8", errors="replace").read()
            s4["hits"] += scan_text(rel_store(p, store)[-60:], txt, 8)
            for r in read_jsonl(p):
                if r.get("isApiErrorMessage") or (r.get("type") == "system" and (r.get("level") in ("error", "warning")
                                                                                or "error" in str(r.get("subtype")))):
                    s4["transcript_error_rows"].append({k: r.get(k) for k in ("type", "subtype", "level", "content")
                                                        if r.get(k) is not None})
        s4["debug_files"] = {os.path.basename(p): os.path.getsize(p) for p in glob.glob(os.path.join(run.logs, "debug-*"))}
        # debug lines that mention where tool results / transcripts are written
        dl = []
        for p in sorted(glob.glob(os.path.join(run.logs, "debug-*"))):
            for line in open(p, encoding="utf-8", errors="replace"):
                if re.search(r"tool-results|persist|projects[\\/]|/resume:|FileHistory|memory index", line, re.I):
                    dl.append(os.path.basename(p) + ": " + mask(squash(line))[25:300])
        s4["debug_path_lines"] = dl[:30]
        # which paths claude checks for a Read through the junction (it suggests one rule per checked path)
        sugg = []
        for p in sorted(glob.glob(os.path.join(run.logs, "debug-*"))):
            t = mask(open(p, encoding="utf-8", errors="replace").read())
            for mm in re.finditer(r"Permission suggestions for Read:(.{0,1500}?)executePermission", t, re.S):
                sugg.append({"file": os.path.basename(p),
                             "rules": re.findall(r'ruleContent\\?"\s*:\s*\\?"([^"\\]+)', mm.group(1))})
        s4["read_permission_suggestions"] = sugg
        s4["read_permission_decision_ms"] = []
        for p in sorted(glob.glob(os.path.join(run.logs, "debug-*"))):
            for line in open(p, encoding="utf-8", errors="replace"):
                mm = re.search(r"tool_dispatch_start tool=Read \S+ permissionDecisionMs=(\d+)", line)
                if mm:
                    s4["read_permission_decision_ms"].append(f"{os.path.basename(p)}: {mm.group(1)}")
        # Known-benign lines (also present in control C, which has no junction): missing optional
        # settings/agents/commands dirs, the debug banner, auto-mode gate, job watch, empty memory dir.
        benign = re.compile(r"Broken symlink or missing file encountered for settings\.json|Failed to stat directory|"
                            r"jobStateNameSync|verifyAutoModeGateAccess|memory index backfill|plugins\\\\?cache|"
                            r"Debug mode enabled|logging to|Reading |Read\(|tool-results", re.I)
        s4["n_hits_total"] = len(s4["hits"])
        s4["unexplained_hits"] = [h for h in s4["hits"] if not benign.search(h["line"])]
        s4["benign_hit_kinds"] = sorted({m.group(0) for h in s4["hits"] for m in [benign.search(h["line"])] if m})
        del s4["hits"]
        rep["step4_warnings"] = s4

        # ---- step 5: delete profile B safely; store intact? --------------------
        s5: dict = {}
        before = tree(store)
        s5["store_files_before"] = len(before)
        s5["B_projects_before_delete"] = link_info(os.path.join(b.dir, "projects"))
        rmtree_safe(b.dir)
        after = tree(store)
        s5["B_dir_gone"] = not os.path.lexists(b.dir)
        s5["store_files_after"] = len(after)
        s5["store_identical"] = before == after
        s5["A_projects_after_B_delete"] = link_info(os.path.join(a.dir, "projects"))
        s5["A_transcripts_via_junction"] = len(transcripts(a.dir))
        s5["naive_deletion_throwaway"] = naive_tests(run.root)
        rep["step5_delete"] = s5

        # ---- step 5b: A resumes S by id after B deleted (print mode) -----------
        s5b: dict = {}
        write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}})
        n_before = len(read_log(body_log))
        cp = run.run_print(a, ["-r", S, "-p", "E3 step5 A after B deleted", "--output-format", "json"], timeout=120)
        s5b["rc"] = cp.returncode
        try:
            res = json.loads(cp.stdout.strip().splitlines()[-1])
            s5b["result"] = res.get("result")
            s5b["session_id"] = res.get("session_id")
        except Exception:
            s5b["stdout_tail"] = cp.stdout[-400:]
        s5b["stderr_tail"] = cp.stderr[-400:]
        nb = [r for r in body_rows(body_log)[n_before:] if r.get("main_loop")]
        if nb:
            blocks = [blk for msg in nb[0]["messages"] for blk in msg["blocks"]]
            s5b["A_request_n_messages"] = nb[0]["n_messages"]
            s5b["A_request_tool_results"] = sum(1 for blk in blocks if blk.get("t") == "tool_result")
            s5b["A_request_has_B_prompts"] = [p for p in ("E3 step2 continue from B", "E3 step3 big tool from B")
                                              if any(p in (blk.get("text") or "") for blk in blocks)]
            s5b["A_request_tool_result_paths"] = nb[0].get("tool_result_paths")
        s5b["jsonl_files"] = [rel_store(p, store) for p in sorted(glob.glob(os.path.join(store, "*", "*.jsonl")))]
        s5b["paths_in_context_exist_now"] = {mask(p): os.path.exists(p) for p in s5b.get("A_request_tool_result_paths")
                                             or []}
        rep["step5b_A_resume_after_B_deleted"] = s5b

        # ---- step 5c: A's model follows the (now dangling) B-profile sidecar path ----
        s5c: dict = {}
        dangling = next((p for p in (s5b.get("A_request_tool_result_paths") or [])
                         if os.sep + "profiles" + os.sep + "B" + os.sep in p), None)
        if dangling:
            write_scenario(scen, {"sk-mock-A": {"mode": "read_path", "path": dangling}, "sk-mock-B": {"mode": "echo"}})
            n_before = len(read_log(body_log))
            cp = run.run_print(a, ["-r", S, "-p", "E3 step5c A reads B sidecar path", "--permission-mode", "default",
                                   "--output-format", "json"], timeout=120)
            s5c["rc"] = cp.returncode
            try:
                s5c["result"] = json.loads(cp.stdout.strip().splitlines()[-1]).get("result")
            except Exception:
                s5c["stdout_tail"] = cp.stdout[-300:]
            s5c["path"] = mask(dangling)
            s5c.update(classify_read(read_results(body_log, n_before)))
            same_file_via_A = os.path.join(a.dir, "projects", dangling.split(os.sep + "projects" + os.sep, 1)[1])
            s5c["same_file_exists_via_A_junction"] = os.path.exists(same_file_via_A)
        rep["step5c_dangling_sidecar_path"] = s5c

        # ---- compact per-claim summary (raw evidence is in the step sections) ----
        g = lambda sec, k: (rep.get(sec) or {}).get(k)  # noqa: E731
        rep["summary"] = {
            "C1_junction_accepted": bool(g("step1_A_tool_big", "tool_done_seen") and g("step1_A_tool_big", "store_jsonl")
                                         and (g("step3_B_tool_big", "A_projects_after") or {}).get("isjunction")
                                         and (g("step3_B_tool_big", "B_projects_after") or {}).get("isjunction")),
            "C2_sidecar_in_store_A": list((g("step1_A_tool_big", "tool_results_files") or {}).values()),
            "C2_sidecar_in_store_B_same_session_dir": g("step3_B_tool_big", "new_under_session_S"),
            "C2_path_in_context_is_writer_profile_path": True if g("step3_B_tool_big", "B_main_bodies_tool_result_paths")
            and all((os.sep + "profiles" + os.sep) in p for p in g("step3_B_tool_big", "B_main_bodies_tool_result_paths"))
            else False,
            "C2_read_prompt_junction_vs_control": {
                "B_own_sidecar_prompted": next((p.get("permission_prompted") for p in
                                                (g("step3b_B_read_sidecars", "probes") or [])
                                                if p["label"].startswith("B_")), None),
                "C_no_junction_own_sidecar_prompted": (g("control_C_no_junction", "read_own_sidecar") or {}).get(
                    "permission_prompted"),
                "F_flag_settings_rules_prompted": [p.get("permission_prompted") for p in
                                                   ((rep.get("mitigation_store_read_grant") or {}).get("F") or {}).get(
                                                       "probes", [])]},
            "C3_resume_by_id": {k: g("step2_B_resume_by_id", k) for k in (
                "reply_seen", "no_conversation_found", "new_jsonl_files", "S_file_session_ids",
                "B_first_main_has_tool_use", "B_first_main_has_tool_result")},
            "C4_rmtree_safe_store_identical": g("step5_delete", "store_identical"),
            "C4_naive": {k: v.get("store_intact") for k, v in (g("step5_delete", "naive_deletion_throwaway") or {}).items()},
            "C4_dangling_path_after_delete": (g("step5c_dangling_sidecar_path", "file_missing"),
                                              g("step5c_dangling_sidecar_path", "same_file_exists_via_A_junction")),
            "C5_picker_lists_A_session": g("step6_B_resume_picker", "lists_A_session_prompt"),
        }
    finally:
        srv.stop()
        rep["events"] = EVENTS
        rep["elapsed_s"] = round(time.time() - T0, 1)
    out = json.dumps(rep, ensure_ascii=False, indent=1)
    with open(os.path.join(run.root, "report.json"), "w", encoding="utf-8") as f:
        f.write(out)
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
