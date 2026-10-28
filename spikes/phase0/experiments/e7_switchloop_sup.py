"""e7_switchloop supervisor: prototype of baton's account-switch loop.

    <python> -I e7_switchloop_sup.py <input.json>

Runs the real interactive claude.exe on the console this process is attached to (inherited),
in its own process group. Watches a hook events file; on StopFailure error=rate_limit from the
current child it waits for the child's sessions/<pid>.json to show idle, sends CTRL_BREAK, waits
for exit (kills after a timeout), restores the console modes saved at startup, writes a DEC reset,
prints one notice line and relaunches claude on the next profile with
`claude -r <transcript_path> "<continue prompt>"`.

input.json:
  {"claude": str, "cwd": str, "settings": str, "events": str, "log": str,
   "session_id": str, "continue_prompt": str,
   "profiles": [{"name": str, "dir": str, "env": {..}}, ...],
   "resume_arg": "path" | "id"        (default "path": pass the absolute transcript path to -r)
   "idle_timeout": 10, "exit_timeout": 10}
Every step goes to <log> (JSON, rewritten after each step).
"""
from __future__ import annotations

import ctypes
import ctypes.wintypes as wt
import json
import os
import signal
import subprocess
import sys
import time

k32 = ctypes.WinDLL("kernel32", use_last_error=True)
k32.CreateFileW.argtypes = [wt.LPCWSTR, wt.DWORD, wt.DWORD, ctypes.c_void_p, wt.DWORD, wt.DWORD, wt.HANDLE]
k32.CreateFileW.restype = wt.HANDLE
k32.GetConsoleMode.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
k32.GetConsoleMode.restype = wt.BOOL
k32.SetConsoleMode.argtypes = [wt.HANDLE, wt.DWORD]
k32.SetConsoleMode.restype = wt.BOOL
k32.GenerateConsoleCtrlEvent.argtypes = [wt.DWORD, wt.DWORD]
k32.GenerateConsoleCtrlEvent.restype = wt.BOOL
k32.GetStdHandle.argtypes = [wt.DWORD]
k32.GetStdHandle.restype = wt.HANDLE
k32.GetFileType.argtypes = [wt.HANDLE]
k32.GetFileType.restype = wt.DWORD
k32.WriteFile.argtypes = [wt.HANDLE, ctypes.c_void_p, wt.DWORD, ctypes.POINTER(wt.DWORD), ctypes.c_void_p]
k32.WriteFile.restype = wt.BOOL
k32.FlushConsoleInputBuffer.argtypes = [wt.HANDLE]
k32.FlushConsoleInputBuffer.restype = wt.BOOL
k32.GetNumberOfConsoleInputEvents.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
k32.GetNumberOfConsoleInputEvents.restype = wt.BOOL
k32.GetProcessTimes.argtypes = [wt.HANDLE] + [ctypes.POINTER(wt.FILETIME)] * 4
k32.GetProcessTimes.restype = wt.BOOL


def _creation_time(handle: int) -> float | None:
    """Process creation time (unix seconds) from a handle we hold, so cleanup can tell
    our child apart from an unrelated process that later reuses its pid."""
    ft = (wt.FILETIME * 4)()
    if not k32.GetProcessTimes(handle, ctypes.byref(ft[0]), ctypes.byref(ft[1]),
                               ctypes.byref(ft[2]), ctypes.byref(ft[3])):
        return None
    return ((ft[0].dwHighDateTime << 32) | ft[0].dwLowDateTime) / 1e7 - 11644473600

GENERIC_READ, GENERIC_WRITE = 0x80000000, 0x40000000
FILE_SHARE_RW = 0x1 | 0x2
OPEN_EXISTING = 3
INVALID_HANDLE = wt.HANDLE(-1).value
CTRL_BREAK_EVENT = 1
CREATE_NEW_PROCESS_GROUP = 0x00000200
ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004

DEC_RESET = ("\x1b[?1049l"   # leave alternate screen
             "\x1b[?25h"     # show cursor
             "\x1b[?2004l"   # bracketed paste off
             "\x1b[?1004l"   # focus reporting off
             "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l"  # mouse tracking off
             "\x1b[<u"       # pop kitty keyboard flags
             "\x1b[>4;0m"    # modifyOtherKeys off
             "\x1b[0m"       # SGR reset
             "\r\n")


class Sup:
    def __init__(self, cfg: dict):
        self.cfg = cfg
        self.t0 = time.time()
        self.steps: list[dict] = []
        self.log_path = cfg["log"]
        self.events_path = cfg["events"]
        self.ev_off = 0
        self.ev_buf = b""
        self.events_seen: list[dict] = []
        self.child: subprocess.Popen | None = None
        self.child_profile: dict | None = None
        self.child_started = 0.0
        self.idx = 0
        self.switched_pids: set[int] = set()
        self.conin = k32.CreateFileW("CONIN$", GENERIC_READ | GENERIC_WRITE, FILE_SHARE_RW, None,
                                     OPEN_EXISTING, 0, None)
        self.conout = k32.CreateFileW("CONOUT$", GENERIC_READ | GENERIC_WRITE, FILE_SHARE_RW, None,
                                      OPEN_EXISTING, 0, None)
        self.saved_modes = self.modes()

    # ---- logging --------------------------------------------------------
    def step(self, name: str, **kw) -> None:
        rec = {"ts": time.time(), "t": round(time.time() - self.t0, 3), "step": name, **kw}
        self.steps.append(rec)
        tmp = self.log_path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump({"steps": self.steps, "events_seen": self.events_seen}, f, ensure_ascii=False, indent=1)
        os.replace(tmp, self.log_path)

    # ---- console --------------------------------------------------------
    def modes(self) -> dict:
        out = {}
        for name, h in (("in", self.conin), ("out", self.conout)):
            m = wt.DWORD()
            ok = h not in (None, 0, INVALID_HANDLE) and k32.GetConsoleMode(h, ctypes.byref(m))
            out[name] = hex(m.value) if ok else None
        return out

    def std_handles(self) -> dict:
        out = {}
        for name, n in (("stdin", -10), ("stdout", -11), ("stderr", -12)):
            h = k32.GetStdHandle(wt.DWORD(n & 0xFFFFFFFF))
            out[name] = {"handle": h, "file_type": k32.GetFileType(h) if h not in (None, 0, INVALID_HANDLE) else None}
        return out

    def restore_modes(self) -> dict:
        res = {}
        for name, h in (("in", self.conin), ("out", self.conout)):
            v = self.saved_modes.get(name)
            if v is not None:
                res[name] = bool(k32.SetConsoleMode(h, int(v, 16)))
        return res

    def write_out(self, s: str) -> None:
        data = s.encode("utf-8")
        n = wt.DWORD()
        k32.WriteFile(self.conout, data, len(data), ctypes.byref(n), None)

    def pending_input(self) -> int | None:
        n = wt.DWORD()
        return int(n.value) if k32.GetNumberOfConsoleInputEvents(self.conin, ctypes.byref(n)) else None

    # ---- events ---------------------------------------------------------
    def new_events(self) -> list[dict]:
        try:
            with open(self.events_path, "rb") as f:
                f.seek(self.ev_off)
                data = f.read()
        except OSError:
            return []
        self.ev_off += len(data)
        self.ev_buf += data
        out = []
        while b"\n" in self.ev_buf:
            line, self.ev_buf = self.ev_buf.split(b"\n", 1)
            if not line.strip():
                continue
            try:
                out.append(json.loads(line.decode("utf-8", "replace")))
            except ValueError:
                pass
        return out

    # ---- child ----------------------------------------------------------
    def spawn(self, prof: dict, extra: list[str]) -> None:
        argv = [self.cfg["claude"], "--settings", self.cfg["settings"], *extra]
        pending_before = self.pending_input()
        self.child = subprocess.Popen(argv, cwd=self.cfg["cwd"], env=prof["env"],
                                      creationflags=CREATE_NEW_PROCESS_GROUP)
        self.child_profile = prof
        self.child_started = time.time()
        self.step("spawned", profile=prof["name"], pid=self.child.pid, argv=argv[1:],
                  created=_creation_time(int(self.child._handle)),
                  pending_input_before_spawn=pending_before)

    def session_file(self) -> str:
        return os.path.join(self.child_profile["dir"], "sessions", f"{self.child.pid}.json")

    def read_session(self) -> dict | None:
        try:
            with open(self.session_file(), encoding="utf-8") as f:
                return json.load(f)
        except (OSError, ValueError):
            return None

    @staticmethod
    def is_idle(d: dict | None) -> bool:
        if not d:
            return False
        for k in ("status", "state", "activity"):
            v = d.get(k)
            if isinstance(v, str) and v.lower() == "idle":
                return True
            if isinstance(v, dict) and str(v.get("status") or v.get("state") or "").lower() == "idle":
                return True
        return False

    def switch(self, ev: dict) -> None:
        payload = ev.get("payload") or {}
        old = self.child
        old_prof = self.child_profile
        self.switched_pids.add(old.pid)
        self.step("switch_begin", from_profile=old_prof["name"], pid=old.pid, hook_ts=ev.get("ts"),
                  error=payload.get("error"), transcript_path=payload.get("transcript_path"),
                  session_id=payload.get("session_id"), pending_input=self.pending_input())
        # 1. wait for idle in sessions/<pid>.json
        deadline = time.time() + float(self.cfg.get("idle_timeout", 10))
        last = None
        snapshots = []
        idle = False
        while time.time() < deadline:
            d = self.read_session()
            if d != last:
                snapshots.append({"t": round(time.time() - self.t0, 3), "data": d})
                last = d
            if self.is_idle(d):
                idle = True
                break
            if old.poll() is not None:
                break
            time.sleep(0.05)
        self.step("idle_wait_done", idle=idle, session_file=self.session_file(), snapshots=snapshots)
        # 2. CTRL_BREAK to the child's process group
        ok = bool(k32.GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, old.pid))
        self.step("ctrl_break_sent", ok=ok, err=ctypes.get_last_error() if not ok else 0)
        try:
            code = old.wait(timeout=float(self.cfg.get("exit_timeout", 10)))
            killed = False
        except subprocess.TimeoutExpired:
            old.kill()
            code = old.wait(5)
            killed = True
        self.step("old_exited", code=code, killed=killed, modes_after_exit=self.modes(),
                  session_file_left=os.path.exists(self.session_file()), pending_input=self.pending_input())
        # 3. console back to how we found it
        restored = self.restore_modes()
        cur = self.modes()
        if cur.get("out") and not (int(cur["out"], 16) & ENABLE_VIRTUAL_TERMINAL_PROCESSING):
            # DEC sequences need VT processing on, or conhost prints them literally
            k32.SetConsoleMode(self.conout, int(cur["out"], 16) | ENABLE_VIRTUAL_TERMINAL_PROCESSING)
            self.write_out(DEC_RESET)
            k32.SetConsoleMode(self.conout, int(cur["out"], 16))
            vt_tmp = True
        else:
            self.write_out(DEC_RESET)
            vt_tmp = False
        self.idx += 1
        prof = self.cfg["profiles"][self.idx]
        notice = (f"[baton] account {old_prof['name']} hit its usage limit; "
                  f"continuing this conversation on account {prof['name']}...")
        self.write_out(notice + "\r\n")
        self.step("console_restored", restore_ok=restored, modes=self.modes(), vt_enabled_for_reset=vt_tmp,
                  notice=notice)
        # 4. relaunch on the next profile with the same transcript
        tp = payload.get("transcript_path")
        target = tp if self.cfg.get("resume_arg", "path") == "path" else payload.get("session_id")
        self.spawn(prof, ["-r", target, self.cfg["continue_prompt"]])

    # ---- main loop ------------------------------------------------------
    def run(self) -> int:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        if hasattr(signal, "SIGBREAK"):
            signal.signal(signal.SIGBREAK, signal.SIG_IGN)
        self.step("start", saved_modes=self.saved_modes, std=self.std_handles(),
                  conin_ok=self.conin not in (None, 0, INVALID_HANDLE),
                  conout_ok=self.conout not in (None, 0, INVALID_HANDLE), sup_pid=os.getpid())
        self.spawn(self.cfg["profiles"][0], ["--session-id", self.cfg["session_id"]])
        last_mode_sample = 0.0
        while True:
            code = self.child.poll()
            if code is not None:
                # drain late events once more for the log
                for ev in self.new_events():
                    self.note_event(ev, acted=False, why="after child exit")
                self.step("child_exited", profile=self.child_profile["name"], pid=self.child.pid, code=code,
                          modes=self.modes())
                restored = self.restore_modes()
                self.write_out(DEC_RESET)
                self.step("final_restore", restore_ok=restored)
                return code
            for ev in self.new_events():
                payload = ev.get("payload") or {}
                if ev.get("event") == "StopFailure" and payload.get("error") == "rate_limit":
                    if self.child.pid in self.switched_pids:
                        self.note_event(ev, acted=False, why="duplicate: already switching this pid")
                    elif ev.get("ts", 0) < self.child_started:
                        self.note_event(ev, acted=False, why="stale: before current child started")
                    elif ev.get("ppid") in self.switched_pids:
                        self.note_event(ev, acted=False, why=f"stale: hook parent {ev.get('ppid')} already replaced")
                    elif self.idx + 1 >= len(self.cfg["profiles"]):
                        self.note_event(ev, acted=False, why="no more profiles")
                    else:
                        self.note_event(ev, acted=True, why="switch")
                        self.switch(ev)
                else:
                    self.note_event(ev, acted=False, why="observe")
            if time.time() - last_mode_sample > (0.25 if time.time() - self.child_started < 6 else 2.0):
                last_mode_sample = time.time()
                m = self.modes()
                pend = self.pending_input()
                if (not self.steps or self.steps[-1].get("step") != "mode_sample"
                        or self.steps[-1].get("modes") != m or self.steps[-1].get("pending_input") != pend):
                    self.step("mode_sample", profile=self.child_profile["name"], modes=m, pending_input=pend)
            time.sleep(0.05)

    def note_event(self, ev: dict, acted: bool, why: str) -> None:
        p = ev.get("payload") or {}
        self.events_seen.append({"seen_ts": time.time(), "hook_ts": ev.get("ts"), "event": ev.get("event"),
                                 "hook_pid": ev.get("pid"), "hook_ppid": ev.get("ppid"),
                                 "child_pid": self.child.pid if self.child else None,
                                 "session_id": p.get("session_id"), "source": p.get("source"),
                                 "error": p.get("error"), "reason": p.get("reason"),
                                 "acted": acted, "why": why})
        self.step("event", event=ev.get("event"), acted=acted, why=why)


def main() -> int:
    with open(sys.argv[1], encoding="utf-8") as f:
        cfg = json.load(f)
    sup = Sup(cfg)
    try:
        return sup.run()
    except BaseException as e:  # noqa: BLE001
        sup.step("crash", error=repr(e))
        if sup.child and sup.child.poll() is None:
            sup.child.kill()
        raise


if __name__ == "__main__":
    sys.exit(main())
