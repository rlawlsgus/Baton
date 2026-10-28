"""e4_ctrlbreak mini supervisor ("e4_sup"). Runs INSIDE the harness's ConPTY and shares the
console with the claude.exe it spawns -- the way the real launcher would share the user's terminal.

    python -I e4_ctrlbreak_sup.py <cfg.json>

cfg.json: {"claude": exe, "env": {...}, "cwd": proj, "ctl": dir, "results": path, "events": path,
           "profile_dir": dir}

Protocol (files in cfg["ctl"], written by the harness):
  go_<i>     JSON {"args": [...], "restore_modes": bool, "flush_input": bool}: spawn launch i
  break_<i>  JSON {"method": "ctrl_break" | "inject_ctrl_c"}: stop launch i
  quit       end the supervisor

Per launch it records: pid, console modes (baseline / running / after), <profile>/sessions/<pid>.json
presence, child processes, exit code and seconds from the break to exit (or "self" exit), and the
console-ctrl events the supervisor itself received. It never reads console input, so all keystrokes
reach claude. After each child exit it writes "E4SUP-EXIT-<i>" to CONOUT$ as a marker.
"""
from __future__ import annotations

import ctypes
import ctypes.wintypes as wt
import json
import os
import subprocess
import sys
import time
import traceback

k32 = ctypes.WinDLL("kernel32", use_last_error=True)

CTRL_C_EVENT, CTRL_BREAK_EVENT = 0, 1
GENERIC_READ, GENERIC_WRITE = 0x80000000, 0x40000000
FILE_SHARE_READ, FILE_SHARE_WRITE = 1, 2
OPEN_EXISTING = 3
INVALID_HANDLE = wt.HANDLE(-1).value
TH32CS_SNAPPROCESS = 0x2

HANDLER_T = ctypes.WINFUNCTYPE(wt.BOOL, wt.DWORD)
k32.SetConsoleCtrlHandler.argtypes = [HANDLER_T, wt.BOOL]
k32.SetConsoleCtrlHandler.restype = wt.BOOL
k32.GenerateConsoleCtrlEvent.argtypes = [wt.DWORD, wt.DWORD]
k32.GenerateConsoleCtrlEvent.restype = wt.BOOL
k32.CreateFileW.argtypes = [wt.LPCWSTR, wt.DWORD, wt.DWORD, ctypes.c_void_p, wt.DWORD, wt.DWORD, wt.HANDLE]
k32.CreateFileW.restype = wt.HANDLE
k32.GetConsoleMode.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
k32.GetConsoleMode.restype = wt.BOOL
k32.SetConsoleMode.argtypes = [wt.HANDLE, wt.DWORD]
k32.SetConsoleMode.restype = wt.BOOL
k32.GetStdHandle.argtypes = [wt.DWORD]
k32.GetStdHandle.restype = wt.HANDLE
k32.GetFileType.argtypes = [wt.HANDLE]
k32.GetFileType.restype = wt.DWORD
k32.GetNumberOfConsoleInputEvents.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
k32.GetNumberOfConsoleInputEvents.restype = wt.BOOL
k32.FlushConsoleInputBuffer.argtypes = [wt.HANDLE]
k32.FlushConsoleInputBuffer.restype = wt.BOOL
k32.WriteConsoleW.argtypes = [wt.HANDLE, wt.LPCWSTR, wt.DWORD, ctypes.POINTER(wt.DWORD), ctypes.c_void_p]
k32.WriteConsoleW.restype = wt.BOOL
k32.CreateToolhelp32Snapshot.argtypes = [wt.DWORD, wt.DWORD]
k32.CreateToolhelp32Snapshot.restype = wt.HANDLE
k32.OpenProcess.argtypes = [wt.DWORD, wt.BOOL, wt.DWORD]
k32.OpenProcess.restype = wt.HANDLE
k32.CloseHandle.argtypes = [wt.HANDLE]
k32.GetExitCodeProcess.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]


class CONSOLE_CURSOR_INFO(ctypes.Structure):
    _fields_ = [("dwSize", wt.DWORD), ("bVisible", wt.BOOL)]


class KEY_EVENT_RECORD(ctypes.Structure):
    _fields_ = [("bKeyDown", wt.BOOL), ("wRepeatCount", wt.WORD), ("wVirtualKeyCode", wt.WORD),
                ("wVirtualScanCode", wt.WORD), ("uChar", wt.WCHAR), ("dwControlKeyState", wt.DWORD)]


class _EV(ctypes.Union):
    _fields_ = [("KeyEvent", KEY_EVENT_RECORD), ("_pad", ctypes.c_byte * 16)]


class INPUT_RECORD(ctypes.Structure):
    _fields_ = [("EventType", wt.WORD), ("Event", _EV)]


class PROCESSENTRY32W(ctypes.Structure):
    _fields_ = [("dwSize", wt.DWORD), ("cntUsage", wt.DWORD), ("th32ProcessID", wt.DWORD),
                ("th32DefaultHeapID", ctypes.c_void_p), ("th32ModuleID", wt.DWORD),
                ("cntThreads", wt.DWORD), ("th32ParentProcessID", wt.DWORD),
                ("pcPriClassBase", ctypes.c_long), ("dwFlags", wt.DWORD), ("szExeFile", ctypes.c_wchar * 260)]


k32.GetConsoleCursorInfo.argtypes = [wt.HANDLE, ctypes.POINTER(CONSOLE_CURSOR_INFO)]
k32.WriteConsoleInputW.argtypes = [wt.HANDLE, ctypes.POINTER(INPUT_RECORD), wt.DWORD, ctypes.POINTER(wt.DWORD)]
k32.WriteConsoleInputW.restype = wt.BOOL
k32.Process32FirstW.argtypes = [wt.HANDLE, ctypes.POINTER(PROCESSENTRY32W)]
k32.Process32NextW.argtypes = [wt.HANDLE, ctypes.POINTER(PROCESSENTRY32W)]

IN_FLAGS = {0x1: "PROCESSED_INPUT", 0x2: "LINE_INPUT", 0x4: "ECHO_INPUT", 0x8: "WINDOW_INPUT",
            0x10: "MOUSE_INPUT", 0x20: "INSERT_MODE", 0x40: "QUICK_EDIT_MODE", 0x80: "EXTENDED_FLAGS",
            0x100: "AUTO_POSITION", 0x200: "VIRTUAL_TERMINAL_INPUT"}
OUT_FLAGS = {0x1: "PROCESSED_OUTPUT", 0x2: "WRAP_AT_EOL_OUTPUT", 0x4: "VIRTUAL_TERMINAL_PROCESSING",
             0x8: "DISABLE_NEWLINE_AUTO_RETURN", 0x10: "LVB_GRID_WORLDWIDE"}

CTRL_EVENTS: list[dict] = []


def _on_ctrl(ev: int) -> bool:
    CTRL_EVENTS.append({"ts": time.time(), "type": int(ev)})
    return ev in (CTRL_C_EVENT, CTRL_BREAK_EVENT)  # swallow Ctrl+C / Ctrl+Break for ourselves


HANDLER = HANDLER_T(_on_ctrl)  # keep a reference for the life of the process


def ctrl_c_ignored() -> bool | None:
    """Read RTL_USER_PROCESS_PARAMETERS.ConsoleFlags bit 0 (the inherited "ignore Ctrl+C" flag, x64 offsets)."""
    try:
        ntdll = ctypes.WinDLL("ntdll")

        class PBI(ctypes.Structure):
            _fields_ = [("ExitStatus", ctypes.c_void_p), ("PebBaseAddress", ctypes.c_void_p),
                        ("AffinityMask", ctypes.c_void_p), ("BasePriority", ctypes.c_void_p),
                        ("UniqueProcessId", ctypes.c_void_p), ("InheritedFromUniqueProcessId", ctypes.c_void_p)]
        pbi = PBI()
        ntdll.NtQueryInformationProcess(wt.HANDLE(-1), 0, ctypes.byref(pbi), ctypes.sizeof(pbi), None)
        params = ctypes.c_void_p.from_address(pbi.PebBaseAddress + 0x20).value
        return bool(ctypes.c_uint32.from_address(params + 0x18).value & 1)
    except Exception:  # noqa: BLE001
        return None


def open_con(name: str):
    h = k32.CreateFileW(name, GENERIC_READ | GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE, None,
                        OPEN_EXISTING, 0, None)
    return None if h in (None, 0, INVALID_HANDLE) else h


def decode(mode: int | None, table: dict) -> list[str] | None:
    if mode is None:
        return None
    return [n for bit, n in table.items() if mode & bit] + ([hex(mode & ~sum(table))] if mode & ~sum(table) else [])


class Sup:
    def __init__(self, cfg: dict):
        self.cfg = cfg
        self.ctl = cfg["ctl"]
        self.results_path = cfg["results"]
        self.events_path = cfg["events"]
        self.hin = open_con("CONIN$")
        self.hout = open_con("CONOUT$")
        self.state: dict = {"sup_pid": os.getpid(), "started": time.time(), "launches": [], "errors": []}

    # ---- bookkeeping -----------------------------------------------------
    def event(self, kind: str, **kw) -> None:
        line = json.dumps({"ts": time.time(), "kind": kind, **kw}, ensure_ascii=False)
        with open(self.events_path, "a", encoding="utf-8") as f:
            f.write(line + "\n")

    def save(self) -> None:
        self.state["ctrl_events"] = list(CTRL_EVENTS)
        tmp = self.results_path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(self.state, f, indent=1, ensure_ascii=False)
        os.replace(tmp, self.results_path)

    def say(self, text: str) -> None:
        if self.hout:
            n = wt.DWORD(0)
            k32.WriteConsoleW(self.hout, text, len(text), ctypes.byref(n), None)

    # ---- console state ---------------------------------------------------
    def modes(self) -> dict:
        def gm(h):
            if not h:
                return None
            m = wt.DWORD(0)
            return int(m.value) if k32.GetConsoleMode(h, ctypes.byref(m)) else None
        mi, mo = gm(self.hin), gm(self.hout)
        cur = CONSOLE_CURSOR_INFO()
        vis = bool(cur.bVisible) if (self.hout and k32.GetConsoleCursorInfo(self.hout, ctypes.byref(cur))) else None
        return {"in": mi, "in_flags": decode(mi, IN_FLAGS), "out": mo, "out_flags": decode(mo, OUT_FLAGS),
                "cp_in": int(k32.GetConsoleCP()), "cp_out": int(k32.GetConsoleOutputCP()), "cursor_visible": vis}

    def pending_input(self) -> int | None:
        n = wt.DWORD(0)
        return int(n.value) if (self.hin and k32.GetNumberOfConsoleInputEvents(self.hin, ctypes.byref(n))) else None

    @staticmethod
    def diff(a: dict, b: dict) -> dict:
        return {k: {"before": a.get(k), "after": b.get(k)} for k in ("in", "out", "cp_in", "cp_out", "cursor_visible")
                if a.get(k) != b.get(k)}

    def std_handles(self) -> dict:
        out = {}
        for name, n in (("stdin", -10), ("stdout", -11), ("stderr", -12)):
            h = k32.GetStdHandle(n & 0xFFFFFFFF)
            out[name] = {"handle": int(h or 0), "file_type": int(k32.GetFileType(h)) if h and h != INVALID_HANDLE else None}
        return out

    @staticmethod
    def children_of(pid: int) -> list[dict]:
        snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
        out = []
        try:
            e = PROCESSENTRY32W()
            e.dwSize = ctypes.sizeof(PROCESSENTRY32W)
            ok = k32.Process32FirstW(snap, ctypes.byref(e))
            while ok:
                if int(e.th32ParentProcessID) == pid:
                    out.append({"pid": int(e.th32ProcessID), "exe": e.szExeFile})
                ok = k32.Process32NextW(snap, ctypes.byref(e))
        finally:
            k32.CloseHandle(snap)
        return out

    @staticmethod
    def pid_alive(pid: int) -> bool:
        h = k32.OpenProcess(0x1000, False, pid)  # PROCESS_QUERY_LIMITED_INFORMATION
        if not h:
            return False
        try:
            code = wt.DWORD(0)
            k32.GetExitCodeProcess(h, ctypes.byref(code))
            return code.value == 259
        finally:
            k32.CloseHandle(h)

    def session_file(self, pid: int) -> str:
        return os.path.join(self.cfg["profile_dir"], "sessions", f"{pid}.json")

    def inject_ctrl_c(self, times: int = 2, gap: float = 0.6) -> list:
        """Type Ctrl+C into the shared console input buffer (as a keyboard would)."""
        res = []
        for _ in range(times):
            recs = (INPUT_RECORD * 2)()
            for j, down in enumerate((True, False)):
                recs[j].EventType = 1  # KEY_EVENT
                ke = recs[j].Event.KeyEvent
                ke.bKeyDown, ke.wRepeatCount, ke.wVirtualKeyCode, ke.wVirtualScanCode = down, 1, 0x43, 0x2E
                ke.uChar, ke.dwControlKeyState = "\x03", 0x0008  # LEFT_CTRL_PRESSED
            n = wt.DWORD(0)
            ok = k32.WriteConsoleInputW(self.hin, recs, 2, ctypes.byref(n))
            res.append({"ok": bool(ok), "written": int(n.value), "err": ctypes.get_last_error()})
            time.sleep(gap)
        return res

    # ---- main loop -------------------------------------------------------
    def save_if_ctrl(self) -> None:
        if len(CTRL_EVENTS) != len(self.state.get("ctrl_events") or []):
            self.save()

    def wait_file(self, names: list[str], timeout: float) -> tuple[str | None, dict]:
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.save_if_ctrl()
            for nm in names:
                p = os.path.join(self.ctl, nm)
                if os.path.exists(p):
                    time.sleep(0.05)
                    try:
                        with open(p, encoding="utf-8") as f:
                            txt = f.read()
                        return nm, (json.loads(txt) if txt.strip() else {})
                    except (OSError, ValueError):
                        return nm, {}
            time.sleep(0.05)
        return None, {}

    def run(self) -> int:
        # A real launcher starts from a terminal where Ctrl+C is not ignored; our harness parent may have
        # left the inherited "ignore Ctrl+C" flag set, so record it and (cfg) clear it before installing.
        self.state["ctrl_c_ignored_inherited"] = ctrl_c_ignored()
        if self.cfg.get("reenable_ctrl_c", True):
            self.state["reenable_ctrl_c_ok"] = bool(k32.SetConsoleCtrlHandler(HANDLER_T(0), False))
            self.state["ctrl_c_ignored_after_reenable"] = ctrl_c_ignored()
        self.state["handler_installed"] = bool(k32.SetConsoleCtrlHandler(HANDLER, True))
        self.state["std_handles"] = self.std_handles()
        self.state["con_handles_ok"] = {"CONIN$": bool(self.hin), "CONOUT$": bool(self.hout)}
        self.state["modes_baseline"] = self.modes()
        self.save()
        self.event("ready", modes=self.state["modes_baseline"])
        self.say("E4SUP-READY\r\n")
        i = 0
        while True:
            nm, go = self.wait_file([f"go_{i}", "quit"], timeout=600)
            if nm != f"go_{i}":
                self.event("quit", why=nm or "timeout")
                break
            self.launch(i, go)
            i += 1
        self.state["ended"] = time.time()
        self.save()
        return 0

    def launch(self, i: int, go: dict) -> None:
        rec: dict = {"i": i, "args": go.get("args", []), "pending_input_before_spawn": self.pending_input(),
                     "modes_before_spawn": self.modes()}
        if go.get("flush_input"):
            rec["flushed_input"] = bool(k32.FlushConsoleInputBuffer(self.hin))
        self.state["launches"].append(rec)
        argv = [self.cfg["claude"], *rec["args"]]
        ctrl_mark = len(CTRL_EVENTS)
        child = subprocess.Popen(argv, creationflags=subprocess.CREATE_NEW_PROCESS_GROUP,
                                 env=self.cfg["env"], cwd=self.cfg["cwd"])
        rec.update(pid=child.pid, spawned_at=time.time())
        self.save()
        self.event("spawned", i=i, pid=child.pid, args=rec["args"])
        sfile = self.session_file(child.pid)
        trig = None
        while True:
            rc = child.poll()
            if rc is not None:
                rec.update(exit_reason="self", exit_code=rc, exited_at=time.time())
                break
            if not rec.get("session_file_seen") and os.path.exists(sfile):
                rec["session_file_seen"] = time.time()
                try:
                    with open(sfile, encoding="utf-8") as f:
                        rec["session_file_content"] = json.load(f)
                except (OSError, ValueError) as ex:
                    rec["session_file_content"] = repr(ex)
            self.save_if_ctrl()
            if go.get("break_after_s") is not None and time.time() - rec["spawned_at"] >= float(go["break_after_s"]):
                trig = {"method": go.get("break_method", "ctrl_break"), "auto_after_s": go["break_after_s"]}
                break
            p = os.path.join(self.ctl, f"break_{i}")
            if os.path.exists(p):
                time.sleep(0.02)
                try:
                    with open(p, encoding="utf-8") as f:
                        trig = json.loads(f.read() or "{}")
                except (OSError, ValueError):
                    trig = {}
                break
            time.sleep(0.05)
        if trig is not None:
            method = trig.get("method", "ctrl_break")
            rec.update(trigger=trig, trigger_seen_at=time.time(), modes_running=self.modes(),
                       session_file_exists_at_trigger=os.path.exists(sfile),
                       children_at_trigger=self.children_of(child.pid))
            t0 = time.time()
            rec["secs_spawn_to_break"] = round(t0 - rec["spawned_at"], 3)
            if method == "ctrl_break":
                ok = k32.GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, child.pid)
                rec.update(gce_ok=bool(ok), gce_err=None if ok else ctypes.get_last_error())
            elif method == "inject_ctrl_c":
                rec["inject"] = self.inject_ctrl_c(int(trig.get("times", 2)), float(trig.get("gap", 0.6)))
            elif method == "kill":  # hard stop baseline: TerminateProcess on our own child pid
                child.kill()
            rec["t0"] = t0
            self.event("break_sent", i=i, pid=child.pid, method=method)
            try:
                rc = child.wait(float(trig.get("wait", 10)))
                rec.update(exit_reason=method, exit_code=rc, exited_at=time.time(), killed=False)
            except subprocess.TimeoutExpired:
                rec.update(still_alive_after_s=float(trig.get("wait", 10)))
                child.kill()
                rc = child.wait(5)
                rec.update(exit_reason=method + "+kill", exit_code=rc, exited_at=time.time(), killed=True)
            rec["secs_break_to_exit"] = round(rec["exited_at"] - t0, 3)
        rec["exit_code_hex"] = hex(rec["exit_code"] & 0xFFFFFFFF) if rec.get("exit_code") is not None else None
        rec["modes_after"] = self.modes()
        rec["modes_diff_vs_baseline"] = self.diff(self.state["modes_baseline"], rec["modes_after"])
        if rec.get("modes_running"):
            rec["modes_diff_running_vs_after"] = self.diff(rec["modes_running"], rec["modes_after"])
        rec["session_file_exists_after"] = os.path.exists(sfile)
        time.sleep(1.0)
        rec["session_file_exists_after_1s"] = os.path.exists(sfile)
        rec["children_alive_after"] = [c for c in rec.get("children_at_trigger", []) if self.pid_alive(c["pid"])]
        rec["pending_input_after"] = self.pending_input()
        rec["sup_ctrl_events_during_launch"] = CTRL_EVENTS[ctrl_mark:]
        if go.get("restore_modes"):
            b = self.state["modes_baseline"]
            rec["restore"] = {"in_ok": bool(k32.SetConsoleMode(self.hin, b["in"])),
                              "out_ok": bool(k32.SetConsoleMode(self.hout, b["out"])),
                              "cp_in_ok": bool(k32.SetConsoleCP(b["cp_in"])),
                              "cp_out_ok": bool(k32.SetConsoleOutputCP(b["cp_out"]))}
            rec["modes_after_restore"] = self.modes()
        self.save()
        self.event("exited", i=i, pid=child.pid, code=rec.get("exit_code"), reason=rec.get("exit_reason"))
        self.say(f"\r\nE4SUP-EXIT-{i} code={rec.get('exit_code')} reason={rec.get('exit_reason')}\r\n")


def main() -> int:
    with open(sys.argv[1], encoding="utf-8") as f:
        cfg = json.load(f)
    sup = Sup(cfg)
    try:
        return sup.run()
    except BaseException:  # noqa: BLE001 - record and keep the evidence
        sup.state["errors"].append(traceback.format_exc())
        sup.save()
        return 3


if __name__ == "__main__":
    sys.exit(main())
