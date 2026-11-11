package winui

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A window that is not visible (as a pseudo console with no terminal is) is
// never brought forward: FocusShown says there is no window instead.
func TestFocusShownSkipsHiddenWindows(t *testing.T) {
	if Shown(0) || FocusShown(0) != ErrNoWindow {
		t.Fatal("no window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	hidden, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hidden == 0 {
		t.Skip("cannot create a window here")
	}
	defer user32.NewProc("DestroyWindow").Call(hidden)
	if Shown(hidden) {
		t.Fatal("a window created without WS_VISIBLE counts as shown")
	}
	if err := FocusShown(hidden); err != ErrNoWindow {
		t.Fatalf("focus of a hidden window: %v", err)
	}
}
