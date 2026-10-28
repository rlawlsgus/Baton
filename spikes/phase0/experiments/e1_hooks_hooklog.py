"""e1_hooks hook / statusLine logger (stdlib only).

    <python> -I e1_hooks_hooklog.py --sess S1 --tag run-Stop --out <events.jsonl> [--sl] [--probe <arg>...]

Appends one JSON line to --out with: argv as received, the raw Windows command
line, cwd, CLAUDE_* / ANTHROPIC_* env names (values only for a safe allowlist),
the parsed stdin JSON, and the parent-process chain (to see whether a shell sat
between claude.exe and this process). With --sl it also prints a status line.
Never prints anything for hooks (stdout of some hooks is fed to the model).
"""
import ctypes
import ctypes.wintypes as wt
import json
import msvcrt
import os
import sys
import time

SAFE_VALUES = {"CLAUDE_PROJECT_DIR", "CLAUDE_CONFIG_DIR", "CLAUDE_ENV_FILE", "CLAUDE_CODE_ENTRYPOINT",
               "CLAUDECODE", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID",
               "CLAUDE_PLUGIN_ROOT", "CLAUDE_CODE_SHELL", "CLAUDE_CODE_GIT_BASH_PATH", "SHELL", "MSYSTEM",
               "CLAUDE_PID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_EFFORT"}


def transcript_state(path):
    """What the transcript looks like at the moment the hook runs (exists, size, last rows)."""
    if not isinstance(path, str) or not path:
        return None
    st = {"exists": os.path.exists(path)}
    if not st["exists"]:
        return st
    try:
        st["size"] = os.path.getsize(path)
        with open(path, "rb") as f:
            f.seek(max(0, st["size"] - 65536))
            tail = f.read().decode("utf-8", "replace").splitlines()[-6:]
        rows = []
        for line in tail:
            try:
                r = json.loads(line)
            except ValueError:
                continue
            msg = r.get("message") or {}
            c = msg.get("content")
            if isinstance(c, list):
                c = " | ".join((b.get("text") or b.get("type") or "")[:50] for b in c if isinstance(b, dict))
            rows.append({"type": r.get("type"), "subtype": r.get("subtype"),
                         "isApiErrorMessage": r.get("isApiErrorMessage"),
                         "content": (c or "")[:70] if isinstance(c, str) else None})
        st["last_rows"] = rows
    except OSError as ex:
        st["error"] = str(ex)
    return st

TH32CS_SNAPPROCESS = 0x2


class PROCESSENTRY32W(ctypes.Structure):
    _fields_ = [("dwSize", wt.DWORD), ("cntUsage", wt.DWORD), ("th32ProcessID", wt.DWORD),
                ("th32DefaultHeapID", ctypes.c_void_p), ("th32ModuleID", wt.DWORD),
                ("cntThreads", wt.DWORD), ("th32ParentProcessID", wt.DWORD),
                ("pcPriClassBase", ctypes.c_long), ("dwFlags", wt.DWORD), ("szExeFile", ctypes.c_wchar * 260)]


def parent_chain(depth: int = 5) -> list:
    k32 = ctypes.WinDLL("kernel32", use_last_error=True)
    k32.CreateToolhelp32Snapshot.restype = wt.HANDLE
    snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
    procs = {}
    try:
        e = PROCESSENTRY32W()
        e.dwSize = ctypes.sizeof(PROCESSENTRY32W)
        ok = k32.Process32FirstW(snap, ctypes.byref(e))
        while ok:
            procs[int(e.th32ProcessID)] = (int(e.th32ParentProcessID), e.szExeFile)
            ok = k32.Process32NextW(snap, ctypes.byref(e))
    finally:
        k32.CloseHandle(snap)
    chain = []
    pid = os.getpid()
    for _ in range(depth):
        if pid not in procs:
            break
        ppid, exe = procs[pid]
        chain.append({"pid": pid, "exe": exe})
        if ppid == pid or ppid == 0:
            break
        pid = ppid
    return chain


def arg_after(flag: str, default=None):
    a = sys.argv
    if flag in a:
        i = a.index(flag)
        if i + 1 < len(a):
            return a[i + 1]
    return default


def main() -> int:
    t0 = time.time()
    out = arg_after("--out")
    tag = arg_after("--tag", "?")
    sess = arg_after("--sess", "?")
    is_sl = "--sl" in sys.argv
    raw = b""
    try:
        raw = sys.stdin.buffer.read()
    except Exception as ex:  # noqa: BLE001
        raw = f"<stdin error {ex!r}>".encode()
    txt = raw.decode("utf-8", "replace")
    try:
        payload = json.loads(txt) if txt.strip() else None
        perr = None
    except ValueError as ex:
        payload, perr = None, str(ex)
    env_names = sorted(k for k in os.environ if k.upper().startswith(("CLAUDE", "ANTHROPIC")))
    env_vals = {k: os.environ[k] for k in os.environ if k.upper() in SAFE_VALUES}
    try:
        cmdline = ctypes.windll.kernel32.GetCommandLineW
        cmdline.restype = ctypes.c_wchar_p
        raw_cmdline = cmdline()
    except Exception:  # noqa: BLE001
        raw_cmdline = None
    rec = {
        "ts": t0, "sess": sess, "tag": tag, "kind": "statusline" if is_sl else "hook",
        "pid": os.getpid(), "argv": sys.argv, "raw_cmdline": raw_cmdline, "cwd": os.getcwd(),
        "env_names": env_names, "env": env_vals, "parents": parent_chain(),
        "stdin": payload, "stdin_parse_error": perr, "stdin_len": len(raw),
        "stdin_raw_head": None if payload is not None else txt[:500],
        "transcript_at_hook": (transcript_state(payload.get("transcript_path"))
                               if isinstance(payload, dict) and not is_sl else None),
    }
    if out:
        os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
        line = (json.dumps(rec, ensure_ascii=False) + "\n").encode("utf-8")
        lock_path = out + ".lock"
        with open(lock_path, "a+b") as lk:
            lk.seek(0)
            for _ in range(200):
                try:
                    msvcrt.locking(lk.fileno(), msvcrt.LK_NBLCK, 1)
                    break
                except OSError:
                    time.sleep(0.02)
            try:
                with open(out, "ab") as f:
                    f.write(line)
            finally:
                try:
                    lk.seek(0)
                    msvcrt.locking(lk.fileno(), msvcrt.LK_UNLCK, 1)
                except OSError:
                    pass
    if is_sl:
        sys.stdout.write(f"SLTAG[{tag}]")
        sys.stdout.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
