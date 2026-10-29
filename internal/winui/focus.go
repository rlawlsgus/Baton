package winui

import (
	"errors"
	"runtime"
)

var (
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procIsWindow         = user32.NewProc("IsWindow")
	procIsIconic         = user32.NewProc("IsIconic")
	procGetAncestor      = user32.NewProc("GetAncestor")
	procShowWindow       = user32.NewProc("ShowWindow")
)

// ErrNoWindow: the session's window is gone (or never was recorded).
var ErrNoWindow = errors.New("the session's window is not open")

// ConsoleWindow is this process's console window, 0 without one. In Windows
// Terminal it is a hidden stand-in that the terminal's own window owns.
func ConsoleWindow() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

// shownWindow is the window the user sees for a console window: the window
// that owns it (Windows Terminal's), or itself.
func shownWindow(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	if ok, _, _ := procIsWindow.Call(hwnd); ok == 0 {
		return 0
	}
	const gaRootOwner = 3
	if owner, _, _ := procGetAncestor.Call(hwnd, gaRootOwner); owner != 0 {
		return owner
	}
	return hwnd
}

// FocusWindow brings a session's console window (or the terminal window that
// holds it) to the front, restoring it if it is minimized. Windows lets only
// the program the user is using do this, such as the baton window they just
// clicked in. In Windows Terminal it brings the terminal's window, which may
// be showing another tab.
func FocusWindow(hwnd uintptr) error {
	w := shownWindow(hwnd)
	if w == 0 {
		return ErrNoWindow
	}
	const swRestore = 9
	if min, _, _ := procIsIconic.Call(w); min != 0 {
		_, _, _ = procShowWindow.Call(w, swRestore)
	}
	if ok, _, _ := procSetForegroundWindow.Call(w); ok != 0 {
		return nil
	}
	// Started by a click on a notification, baton is not the program in front
	// yet: borrow the front window's input, as the folder dialog does.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		fgThread, _, _ := procGetWindowThreadPID.Call(fg, 0)
		me, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && fgThread != me {
			procAttachThreadInput.Call(me, fgThread, 1)
			ok, _, _ := procSetForegroundWindow.Call(w)
			procAttachThreadInput.Call(me, fgThread, 0)
			if ok != 0 {
				return nil
			}
		}
	}
	return errors.New("Windows did not let baton bring the session's window to the front")
}

// ShuttingDown reports that Windows is shutting down, restarting or signing
// the user out.
func ShuttingDown() bool {
	const smShuttingDown = 0x2000
	r, _, _ := procGetSystemMetrics.Call(smShuttingDown)
	return r != 0
}

// InFront reports whether a session's console window is the one in front,
// i.e. the user is looking at it.
func InFront(hwnd uintptr) bool {
	w := shownWindow(hwnd)
	if w == 0 {
		return false
	}
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == w || fg == hwnd
}
