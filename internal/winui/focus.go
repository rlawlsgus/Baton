package winui

import (
	"errors"
	"time"
	"unsafe"
)

var (
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procIsWindow         = user32.NewProc("IsWindow")
	procIsIconic         = user32.NewProc("IsIconic")
	procGetAncestor      = user32.NewProc("GetAncestor")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSendInput        = user32.NewProc("SendInput")
	procFlashWindowEx    = user32.NewProc("FlashWindowEx")
)

// ErrNoWindow: the session's window is gone (or never was recorded).
var ErrNoWindow = errors.New("the session's window is not open")

// ErrFocusRefused: Windows did not let baton bring the window to the front;
// its taskbar button flashes instead.
var ErrFocusRefused = errors.New("Windows did not let baton bring the session's window to the front")

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

// mouseInput and input are Windows' MOUSEINPUT and INPUT (40 bytes on 64-bit
// Windows, 28 on 32-bit).
type mouseInput struct {
	Dx, Dy                 int32
	MouseData, Flags, Time uint32
	ExtraInfo              uintptr
}

type input struct {
	Type uint32
	Mi   mouseInput
}

// Nudge sends Windows one empty mouse input: it moves nothing and clicks
// nothing, but makes this process the one the user last used, which Windows
// requires before it lets a program bring a window to the front. Call it only
// in direct answer to the user's click (on a notification, say), never on
// baton's own initiative.
func Nudge() {
	in := input{} // INPUT_MOUSE, all zero
	_, _, _ = procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// inFrontWithin waits up to d for w to become the window in front.
func inFrontWithin(w uintptr, d time.Duration) bool {
	for end := time.Now().Add(d); ; {
		if fg, _, _ := procGetForegroundWindow.Call(); fg == w {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// FocusWindow brings a session's console window (or the terminal window that
// holds it) to the front, restoring it if it is minimized. In Windows
// Terminal it brings the terminal's window, which may be showing another tab.
func FocusWindow(hwnd uintptr) error {
	_, err := FocusWindowHow(hwnd)
	return err
}

// FocusWindowHow is FocusWindow, also saying which step worked (for the log).
//
// Windows lets only the program the user is using bring a window to the
// front. The baton window they just clicked in is such a program; a
// baton-app.exe started by a click on a notification is not (it starts in
// the background), and borrowing the front window's input no longer helps on
// current Windows 11. An empty input event does, and as a last resort
// minimizing and restoring the window does too. Each step is checked against
// the window actually in front rather than trusted.
//
// A window the user cannot see (a pseudo console with no terminal around it,
// as a test or another program drives) is ErrNoWindow: bringing it forward
// would only take the focus from the user's window, and minimizing and
// restoring it would pop up a blank window. A minimized window is still one
// the user can see.
func FocusWindowHow(hwnd uintptr) (string, error) {
	w := shownWindow(hwnd)
	if w == 0 {
		return "", ErrNoWindow
	}
	if vis, _, _ := procIsVisible.Call(w); vis == 0 {
		return "", ErrNoWindow
	}
	const swShowMinNoActive, swRestore = 7, 9
	if min, _, _ := procIsIconic.Call(w); min != 0 {
		_, _, _ = procShowWindow.Call(w, swRestore)
		if inFrontWithin(w, 300*time.Millisecond) {
			return "restored", nil
		}
	}
	if fg, _, _ := procGetForegroundWindow.Call(); fg == w {
		return "already in front", nil
	}
	_, _, _ = procSetForegroundWindow.Call(w)
	if inFrontWithin(w, 300*time.Millisecond) {
		return "SetForegroundWindow", nil
	}
	Nudge()
	_, _, _ = procSetForegroundWindow.Call(w)
	if inFrontWithin(w, time.Second) {
		return "SetForegroundWindow after the empty input", nil
	}
	_, _, _ = procShowWindow.Call(w, swShowMinNoActive)
	_, _, _ = procShowWindow.Call(w, swRestore)
	if inFrontWithin(w, time.Second) {
		return "minimize and restore", nil
	}
	flash(w)
	return "", ErrFocusRefused
}

// flash makes the window's taskbar button flash until it comes to the front.
func flash(w uintptr) {
	type flashInfo struct {
		Size    uint32
		Hwnd    uintptr
		Flags   uint32
		Count   uint32
		Timeout uint32
	}
	const flashTray, flashTimerNoFG = 0x2, 0xC
	fi := flashInfo{Hwnd: w, Flags: flashTray | flashTimerNoFG}
	fi.Size = uint32(unsafe.Sizeof(fi))
	_, _, _ = procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
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
