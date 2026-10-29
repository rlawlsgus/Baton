package winui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestFocusWindowGone(t *testing.T) {
	if err := FocusWindow(0); err != ErrNoWindow {
		t.Fatalf("got %v", err)
	}
	if InFront(0) {
		t.Fatal("no window cannot be in front")
	}
}

// Opens a real console window (in Windows Terminal when that is the default
// terminal) and reports what baton would record and bring forward:
//
//	BATON_LIVE_FOCUS=1 go test ./internal/winui -run LiveConsole -v
func TestLiveConsoleWindow(t *testing.T) {
	if out := os.Getenv("BATON_FOCUS_CHILD"); out != "" {
		// The child, in its own console window: report, wait, exit cleanly.
		hwnd := ConsoleWindow()
		_ = os.WriteFile(out, []byte(strconv.FormatUint(uint64(hwnd), 10)), 0o644)
		for i := 0; i < 300; i++ {
			if _, err := os.Stat(out + ".done"); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		return
	}
	if os.Getenv("BATON_LIVE_FOCUS") == "" {
		t.Skip("opens a console window; set BATON_LIVE_FOCUS=1")
	}
	out := filepath.Join(t.TempDir(), "hwnd")
	cmd := exec.Command(os.Args[0], "-test.run", "TestLiveConsoleWindow")
	cmd.Env = append(os.Environ(), "BATON_FOCUS_CHILD="+out)
	cmd.SysProcAttr = NewConsoleProcAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.WriteFile(out+".done", nil, 0o644); _ = cmd.Wait() }()
	var hwnd uintptr
	for i := 0; i < 100 && hwnd == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			n, _ := strconv.ParseUint(string(b), 10, 64)
			hwnd = uintptr(n)
		}
	}
	if hwnd == 0 {
		t.Fatal("the child reported no console window")
	}
	shown := shownWindow(hwnd)
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(shown)
	t.Logf("console window %#x (%s), shown window %#x (%s), visible %v", hwnd, className(hwnd), shown, className(shown), visible != 0)
	if shown == 0 || visible == 0 {
		t.Fatal("no visible window for the session")
	}
	err := FocusWindow(hwnd)
	time.Sleep(300 * time.Millisecond)
	t.Logf("focus: %v; in front: %v", err, InFront(hwnd))
}

func className(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := user32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return strings.TrimSpace(string(utf16ToString(buf[:n])))
}

func utf16ToString(u []uint16) []rune {
	r := make([]rune, len(u))
	for i, c := range u {
		r[i] = rune(c)
	}
	return r
}
