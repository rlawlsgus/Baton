"""Minimal Windows ConPTY driver (ctypes only, no third-party packages).

Gives a child process (e.g. claude.exe) a real pseudo-console so its interactive
TUI runs, while the test reads its VT output and types into it.

    with PseudoConsole([exe, "--settings", path], cwd=proj, env=env, log_dir=d) as pc:
        pc.wait_for(r"for shortcuts", timeout=60)
        pc.submit("hello")
        pc.wait_for(r"MOCK-REPLY", timeout=60)

Output is kept raw (bytes) and as text with escape sequences stripped. Because
the TUI repositions the cursor, words from different screen regions can end up
adjacent in the stripped text; search for short distinctive strings.
"""
from __future__ import annotations

import codecs
import ctypes
import ctypes.wintypes as wt
import os
import re
import subprocess
import threading
import time

kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)

HPCON = wt.HANDLE
PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x00020016
EXTENDED_STARTUPINFO_PRESENT = 0x00080000
CREATE_UNICODE_ENVIRONMENT = 0x00000400
STARTF_USESTDHANDLES = 0x00000100
INFINITE = 0xFFFFFFFF
WAIT_OBJECT_0 = 0
STILL_ACTIVE = 259


class COORD(ctypes.Structure):
    _fields_ = [("X", wt.SHORT), ("Y", wt.SHORT)]


class STARTUPINFOW(ctypes.Structure):
    _fields_ = [
        ("cb", wt.DWORD), ("lpReserved", wt.LPWSTR), ("lpDesktop", wt.LPWSTR),
        ("lpTitle", wt.LPWSTR), ("dwX", wt.DWORD), ("dwY", wt.DWORD),
        ("dwXSize", wt.DWORD), ("dwYSize", wt.DWORD), ("dwXCountChars", wt.DWORD),
        ("dwYCountChars", wt.DWORD), ("dwFillAttribute", wt.DWORD), ("dwFlags", wt.DWORD),
        ("wShowWindow", wt.WORD), ("cbReserved2", wt.WORD),
        ("lpReserved2", ctypes.c_void_p), ("hStdInput", wt.HANDLE),
        ("hStdOutput", wt.HANDLE), ("hStdError", wt.HANDLE),
    ]


class STARTUPINFOEXW(ctypes.Structure):
    _fields_ = [("StartupInfo", STARTUPINFOW), ("lpAttributeList", ctypes.c_void_p)]


class PROCESS_INFORMATION(ctypes.Structure):
    _fields_ = [("hProcess", wt.HANDLE), ("hThread", wt.HANDLE),
                ("dwProcessId", wt.DWORD), ("dwThreadId", wt.DWORD)]


kernel32.CreatePseudoConsole.argtypes = [COORD, wt.HANDLE, wt.HANDLE, wt.DWORD, ctypes.POINTER(HPCON)]
kernel32.CreatePseudoConsole.restype = ctypes.c_long
kernel32.ResizePseudoConsole.argtypes = [HPCON, COORD]
kernel32.ResizePseudoConsole.restype = ctypes.c_long
kernel32.ClosePseudoConsole.argtypes = [HPCON]
kernel32.ClosePseudoConsole.restype = None
kernel32.CreatePipe.argtypes = [ctypes.POINTER(wt.HANDLE), ctypes.POINTER(wt.HANDLE), ctypes.c_void_p, wt.DWORD]
kernel32.CreatePipe.restype = wt.BOOL
kernel32.InitializeProcThreadAttributeList.argtypes = [ctypes.c_void_p, wt.DWORD, wt.DWORD, ctypes.POINTER(ctypes.c_size_t)]
kernel32.InitializeProcThreadAttributeList.restype = wt.BOOL
kernel32.UpdateProcThreadAttribute.argtypes = [ctypes.c_void_p, wt.DWORD, ctypes.c_size_t, ctypes.c_void_p,
                                               ctypes.c_size_t, ctypes.c_void_p, ctypes.c_void_p]
kernel32.UpdateProcThreadAttribute.restype = wt.BOOL
kernel32.DeleteProcThreadAttributeList.argtypes = [ctypes.c_void_p]
kernel32.DeleteProcThreadAttributeList.restype = None
kernel32.CreateProcessW.argtypes = [wt.LPCWSTR, wt.LPWSTR, ctypes.c_void_p, ctypes.c_void_p, wt.BOOL, wt.DWORD,
                                    ctypes.c_void_p, wt.LPCWSTR, ctypes.POINTER(STARTUPINFOEXW),
                                    ctypes.POINTER(PROCESS_INFORMATION)]
kernel32.CreateProcessW.restype = wt.BOOL
kernel32.ReadFile.argtypes = [wt.HANDLE, ctypes.c_void_p, wt.DWORD, ctypes.POINTER(wt.DWORD), ctypes.c_void_p]
kernel32.ReadFile.restype = wt.BOOL
kernel32.WriteFile.argtypes = [wt.HANDLE, ctypes.c_void_p, wt.DWORD, ctypes.POINTER(wt.DWORD), ctypes.c_void_p]
kernel32.WriteFile.restype = wt.BOOL
kernel32.CloseHandle.argtypes = [wt.HANDLE]
kernel32.CloseHandle.restype = wt.BOOL
kernel32.WaitForSingleObject.argtypes = [wt.HANDLE, wt.DWORD]
kernel32.WaitForSingleObject.restype = wt.DWORD
kernel32.GetExitCodeProcess.argtypes = [wt.HANDLE, ctypes.POINTER(wt.DWORD)]
kernel32.GetExitCodeProcess.restype = wt.BOOL
kernel32.TerminateProcess.argtypes = [wt.HANDLE, wt.UINT]
kernel32.TerminateProcess.restype = wt.BOOL

_ANSI = re.compile(
    r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)"      # OSC ... BEL / ST
    r"|\x1b\[[0-?]*[ -/]*[@-~]"                # CSI
    r"|\x1b[PX^_][^\x1b]*\x1b\\"               # DCS/SOS/PM/APC ... ST
    r"|\x1b[()*+][0-9A-Za-z]"                  # charset designation
    r"|\x1b[@-Z\\-_=>78]"                      # other 2-byte escapes
)


def strip_ansi(s: str, repl: str = "") -> str:
    return _ANSI.sub(repl, s).replace("\r", "")


def _env_block(env: dict[str, str]) -> ctypes.Array:
    items = sorted(env.items(), key=lambda kv: kv[0].upper())
    joined = "".join(f"{k}={v}\0" for k, v in items) + "\0"
    return ctypes.create_unicode_buffer(joined, len(joined))


class PseudoConsole:
    def __init__(self, argv: list[str], cwd: str | None = None, env: dict[str, str] | None = None,
                 cols: int = 120, rows: int = 40, log_dir: str | None = None, name: str = "pty"):
        self.argv = argv
        self.cwd = cwd
        self.env = env
        self.cols, self.rows = cols, rows
        self.log_dir = log_dir
        self.name = name
        self._raw = bytearray()
        self._text_parts: list[str] = []
        self._lock = threading.Lock()
        self._decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self._reader: threading.Thread | None = None
        self.hpc = HPCON()
        self.pi = PROCESS_INFORMATION()
        self._in_w = wt.HANDLE()
        self._out_r = wt.HANDLE()
        self._closed = False
        self.started_at = 0.0

    # ---- lifecycle ------------------------------------------------------
    def start(self) -> "PseudoConsole":
        in_r, in_w, out_r, out_w = wt.HANDLE(), wt.HANDLE(), wt.HANDLE(), wt.HANDLE()
        if not kernel32.CreatePipe(ctypes.byref(in_r), ctypes.byref(in_w), None, 0):
            raise ctypes.WinError(ctypes.get_last_error())
        if not kernel32.CreatePipe(ctypes.byref(out_r), ctypes.byref(out_w), None, 0):
            raise ctypes.WinError(ctypes.get_last_error())
        hr = kernel32.CreatePseudoConsole(COORD(self.cols, self.rows), in_r, out_w, 0, ctypes.byref(self.hpc))
        if hr != 0:
            raise OSError(f"CreatePseudoConsole failed hr=0x{hr & 0xFFFFFFFF:08x}")

        size = ctypes.c_size_t(0)
        kernel32.InitializeProcThreadAttributeList(None, 1, 0, ctypes.byref(size))
        attr_buf = ctypes.create_string_buffer(size.value)
        if not kernel32.InitializeProcThreadAttributeList(attr_buf, 1, 0, ctypes.byref(size)):
            raise ctypes.WinError(ctypes.get_last_error())
        if not kernel32.UpdateProcThreadAttribute(attr_buf, 0, PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
                                                  self.hpc, ctypes.sizeof(HPCON), None, None):
            raise ctypes.WinError(ctypes.get_last_error())

        si = STARTUPINFOEXW()
        si.StartupInfo.cb = ctypes.sizeof(STARTUPINFOEXW)
        # Without this the child can inherit our (redirected) std handles instead of the PTY.
        si.StartupInfo.dwFlags = STARTF_USESTDHANDLES
        si.lpAttributeList = ctypes.cast(attr_buf, ctypes.c_void_p)
        cmdline = ctypes.create_unicode_buffer(subprocess.list2cmdline(self.argv))
        env_buf = _env_block(self.env) if self.env is not None else None
        flags = EXTENDED_STARTUPINFO_PRESENT | (CREATE_UNICODE_ENVIRONMENT if env_buf is not None else 0)
        ok = kernel32.CreateProcessW(None, cmdline, None, None, False, flags,
                                     ctypes.cast(env_buf, ctypes.c_void_p) if env_buf is not None else None,
                                     self.cwd, ctypes.byref(si), ctypes.byref(self.pi))
        err = ctypes.get_last_error()
        kernel32.DeleteProcThreadAttributeList(attr_buf)
        # The PTY owns these ends now.
        kernel32.CloseHandle(in_r)
        kernel32.CloseHandle(out_w)
        if not ok:
            kernel32.ClosePseudoConsole(self.hpc)
            raise ctypes.WinError(err)
        self._in_w, self._out_r = in_w, out_r
        self.started_at = time.time()
        self._reader = threading.Thread(target=self._read_loop, daemon=True, name=f"{self.name}-reader")
        self._reader.start()
        return self

    def _read_loop(self) -> None:
        buf = ctypes.create_string_buffer(65536)
        n = wt.DWORD(0)
        while True:
            ok = kernel32.ReadFile(self._out_r, buf, len(buf), ctypes.byref(n), None)
            if not ok or n.value == 0:
                break
            chunk = buf.raw[: n.value]
            with self._lock:
                self._raw.extend(chunk)
                self._text_parts.append(self._decoder.decode(chunk))
            self._answer_queries(chunk)

    def _answer_queries(self, chunk: bytes) -> None:
        # Reply to a few common terminal queries so TUIs don't wait on them.
        if b"\x1b[6n" in chunk:
            self.send("\x1b[1;1R")
        if b"\x1b[c" in chunk or b"\x1b[0c" in chunk:
            self.send("\x1b[?1;2c")

    @property
    def pid(self) -> int:
        return int(self.pi.dwProcessId)

    def is_alive(self) -> bool:
        if not self.pi.hProcess:
            return False
        return kernel32.WaitForSingleObject(self.pi.hProcess, 0) != WAIT_OBJECT_0

    def wait_exit(self, timeout: float | None = None) -> int | None:
        ms = INFINITE if timeout is None else int(timeout * 1000)
        if kernel32.WaitForSingleObject(self.pi.hProcess, ms) != WAIT_OBJECT_0:
            return None
        code = wt.DWORD()
        kernel32.GetExitCodeProcess(self.pi.hProcess, ctypes.byref(code))
        return int(code.value)

    def exit_code(self) -> int | None:
        code = wt.DWORD()
        kernel32.GetExitCodeProcess(self.pi.hProcess, ctypes.byref(code))
        return None if code.value == STILL_ACTIVE else int(code.value)

    def terminate(self) -> None:
        if self.is_alive():
            kernel32.TerminateProcess(self.pi.hProcess, 1)

    def close(self, wait: float = 5.0) -> int | None:
        """Close the pseudo-console (the child sees its console go away), keep draining."""
        if self._closed:
            return self.exit_code()
        self._closed = True
        closer = threading.Thread(target=kernel32.ClosePseudoConsole, args=(self.hpc,), daemon=True)
        closer.start()
        closer.join(wait)
        code = self.wait_exit(wait)
        if code is None:
            self.terminate()
            code = self.wait_exit(2)
        if self._reader:
            self._reader.join(2)
        for h in (self._in_w, self._out_r, self.pi.hThread, self.pi.hProcess):
            if h:
                kernel32.CloseHandle(h)
        self.save_logs()
        return code

    def __enter__(self) -> "PseudoConsole":
        return self.start()

    def __exit__(self, *exc) -> None:
        self.close()

    # ---- I/O ------------------------------------------------------------
    def send(self, s: str | bytes) -> None:
        data = s.encode("utf-8") if isinstance(s, str) else s
        n = wt.DWORD(0)
        kernel32.WriteFile(self._in_w, data, len(data), ctypes.byref(n), None)

    def submit(self, text: str, settle: float = 0.4) -> None:
        """Type text, pause (so it isn't treated as one paste with the Enter), press Enter."""
        self.send(text)
        time.sleep(settle)
        self.send("\r")

    def resize(self, cols: int, rows: int) -> None:
        self.cols, self.rows = cols, rows
        kernel32.ResizePseudoConsole(self.hpc, COORD(cols, rows))

    @property
    def raw(self) -> bytes:
        with self._lock:
            return bytes(self._raw)

    @property
    def text(self) -> str:
        with self._lock:
            return "".join(self._text_parts)

    def plain(self, since: int = 0) -> str:
        return strip_ansi(self.text[since:])

    def mark(self) -> int:
        """Offset into .text; pass to wait_for(since=...) to ignore earlier output."""
        return len(self.text)

    def wait_for(self, pattern: str, timeout: float = 30.0, since: int = 0, flags: int = re.I) -> re.Match | None:
        rx = re.compile(pattern, flags)
        deadline = time.time() + timeout
        while time.time() < deadline:
            t = self.text[since:]
            for candidate in (strip_ansi(t), strip_ansi(t, " ")):
                m = rx.search(candidate)
                if m:
                    return m
            if not self.is_alive():
                t = self.text[since:]
                return rx.search(strip_ansi(t)) or rx.search(strip_ansi(t, " "))
            time.sleep(0.2)
        return None

    def save_logs(self) -> None:
        if not self.log_dir:
            return
        os.makedirs(self.log_dir, exist_ok=True)
        with open(os.path.join(self.log_dir, f"{self.name}.raw.bin"), "wb") as f:
            f.write(self.raw)
        with open(os.path.join(self.log_dir, f"{self.name}.txt"), "w", encoding="utf-8") as f:
            f.write(strip_ansi(self.text, " "))
