package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The way baton asks the desktop app to quit, tried on a stand-in: this
// test binary run again as a program with one hidden window, which answers
// as BATON_FAKE_WINDOW says. The real app is never reached from a test.
func TestEndSession(t *testing.T) {
	for _, mode := range []string{"quit", "refuse", "ignore", "slow"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestFakeAppWindow$")
			cmd.Env = append(os.Environ(), "BATON_FAKE_WINDOW="+mode, "BATON_FAKE_WINDOW_DIR="+dir)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }()
			t.Cleanup(func() {
				select {
				case <-done:
				default:
					cmd.Process.Kill() // the stand-in this test started
					<-done
				}
			})
			waitFile(t, filepath.Join(dir, "ready"))
			gone := func() bool {
				select {
				case <-done:
					return true
				default:
					return false
				}
			}
			err := endSession([]uint32{uint32(cmd.Process.Pid)}, 3*time.Second, gone)
			switch mode {
			case "quit":
				if err != nil || !gone() || !isFile(filepath.Join(dir, "ended")) {
					t.Fatalf("err %v, gone %v", err, gone())
				}
			case "refuse":
				if !errors.Is(err, ErrQuitRefused) || gone() || isFile(filepath.Join(dir, "ended")) {
					t.Fatalf("err %v, gone %v", err, gone())
				}
				waitFile(t, filepath.Join(dir, "goes-on")) // told the session goes on
			case "ignore":
				if !errors.Is(err, ErrQuitTimeout) || gone() {
					t.Fatalf("err %v, gone %v", err, gone())
				}
			case "slow":
				// No answer in time is no yes: it is never told it ends.
				if !errors.Is(err, ErrQuitTimeout) || gone() || isFile(filepath.Join(dir, "ended")) {
					t.Fatalf("err %v, gone %v", err, gone())
				}
				waitFile(t, filepath.Join(dir, "goes-on"))
			}
		})
	}
	if err := endSession([]uint32{0x7ffffff0}, time.Second, func() bool { return false }); !errors.Is(err, ErrQuitTimeout) {
		t.Fatalf("no window: %v", err)
	}
}

// With the stand-in file, quitting the app deletes it; without one, a test
// never quits anything.
func TestQuitAppStandIn(t *testing.T) {
	f := filepath.Join(t.TempDir(), "running")
	must(t, os.WriteFile(f, nil, 0o600))
	t.Setenv("BATON_DESKTOP_RUNNING", "file:"+f)
	if !AppRunning() {
		t.Fatal("not running while the file is there")
	}
	if err := QuitApp(time.Second); err != nil || AppRunning() || isFile(f) {
		t.Fatalf("quit: %v", err)
	}
	if err := QuitApp(time.Second); err != nil {
		t.Fatalf("quit again: %v", err)
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	if err := QuitApp(time.Second); err == nil {
		t.Fatal("a test reached for the real app")
	}
}

func waitFile(t *testing.T, p string) {
	t.Helper()
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if isFile(p) {
			return
		}
	}
	t.Fatalf("%s never came", p)
}

// TestFakeAppWindow is the stand-in, when run as one.
func TestFakeAppWindow(t *testing.T) {
	mode, dir := os.Getenv("BATON_FAKE_WINDOW"), os.Getenv("BATON_FAKE_WINDOW_DIR")
	if mode == "" || dir == "" {
		t.Skip("run by TestEndSession")
	}
	runtime.LockOSThread()
	note := func(name string) { os.WriteFile(filepath.Join(dir, name), nil, 0o600) }
	user := windows.NewLazySystemDLL("user32.dll")
	register, create := user.NewProc("RegisterClassExW"), user.NewProc("CreateWindowExW")
	def, getMsg := user.NewProc("DefWindowProcW"), user.NewProc("GetMessageW")
	dispatch, postQuit := user.NewProc("DispatchMessageW"), user.NewProc("PostQuitMessage")
	proc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		switch msg {
		case wmQueryEndSession:
			switch mode {
			case "refuse":
				return 0
			case "slow":
				time.Sleep(askTimeout*time.Millisecond + 2*time.Second)
			}
			return 1
		case wmEndSession:
			if wp == 0 {
				note("goes-on")
				return 0
			}
			if mode == "quit" {
				note("ended")
				postQuit.Call(0)
			}
			return 0
		}
		r, _, _ := def.Call(h, msg, wp, lp)
		return r
	})
	name, _ := windows.UTF16PtrFromString("BatonFakeApp")
	type wndClassEx struct {
		Size, Style                        uint32
		WndProc                            uintptr
		ClsExtra, WndExtra                 int32
		Instance, Icon, Cursor, Background windows.Handle
		MenuName, ClassName                *uint16
		IconSm                             windows.Handle
	}
	wc := wndClassEx{WndProc: proc, ClassName: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := register.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatal(err)
	}
	if h, _, err := create.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 100, 100, 0, 0, 0, 0); h == 0 {
		t.Fatal(err)
	}
	note("ready")
	var msg [48]byte
	for {
		r, _, _ := getMsg.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		dispatch.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
	os.Exit(0)
}
