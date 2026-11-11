package winui

var procIsVisible = user32.NewProc("IsWindowVisible")

// Shown reports whether the user can see a session's console window (or the
// terminal window that holds it). A pseudo console with no terminal around
// it, such as one a test or another program drives, is a hidden window.
func Shown(hwnd uintptr) bool {
	w := shownWindow(hwnd)
	if w == 0 {
		return false
	}
	vis, _, _ := procIsVisible.Call(w)
	return vis != 0
}

// FocusShown is FocusWindow, which already refuses a hidden window
// (ErrNoWindow): bringing one forward gives the user nothing to look at, and
// could only take the focus from the window they are in, or make a blank
// window pop up.
func FocusShown(hwnd uintptr) error { return FocusWindow(hwnd) }
