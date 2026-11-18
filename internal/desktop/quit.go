package desktop

import (
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Why QuitApp could not quit the desktop app.
var (
	// ErrQuitRefused: the app answered that it must not end now.
	ErrQuitRefused = errors.New("the desktop app would not quit now")
	// ErrQuitTimeout: the app was asked to quit, and still runs.
	ErrQuitTimeout = errors.New("the desktop app did not quit in time")
)

// QuitApp asks the Claude desktop app to quit, the way Windows asks a
// program to when it must close it (an update, the Restart Manager): each
// window of the app's main process is sent WM_QUERYENDSESSION, and then
// WM_ENDSESSION, both saying ENDSESSION_CLOSEAPP. The app then quits as it
// does when Windows ends the session: it saves what it keeps, stops what
// runs in it, and ends. QuitApp waits up to wait for every process of the
// app to end. An app that is not running is quit already.
//
// It never decides whether the app may quit: the caller has made sure
// nothing in it is working. BATON_DESKTOP_RUNNING=file:<path> (tests, the
// end-to-end test) stands for the app: it runs while that file is there,
// and quitting it deletes the file. Otherwise a test binary, or one with
// BATON_DESKTOP_DATA set, never reaches the real app.
func QuitApp(wait time.Duration) error {
	if p, ok := runningFile(); ok {
		guard("remove", p)
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if testing.Testing() || os.Getenv("BATON_DESKTOP_DATA") != "" {
		return errors.New("desktop: only a stand-in for the desktop app is quit here")
	}
	mains := appMains()
	if len(mains) == 0 {
		return nil
	}
	return endSession(mains, wait, func() bool { return len(appProcs()) == 0 })
}

// runningFile is the file BATON_DESKTOP_RUNNING=file:<path> names.
func runningFile() (string, bool) {
	p, ok := strings.CutPrefix(os.Getenv("BATON_DESKTOP_RUNNING"), "file:")
	return p, ok && p != ""
}

// appProc is a running process of the desktop app.
type appProc struct{ pid, parent uint32 }

// appProcs are the desktop app's own processes (IsAppImage), not the
// Claude Code it runs.
func appProcs() []appProc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []appProc
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "claude.exe") {
			continue
		}
		if p := imageOf(e.ProcessID); p != "" && IsAppImage(p) {
			out = append(out, appProc{pid: e.ProcessID, parent: e.ParentProcessID})
		}
	}
	return out
}

func imageOf(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// appMains are the app's main processes: those of its processes that no
// other one of them started (the rest are its helpers: renderers, the GPU
// process...).
func appMains() []uint32 {
	procs := appProcs()
	var out []uint32
	for _, p := range procs {
		if !slices.ContainsFunc(procs, func(q appProc) bool { return q.pid == p.parent }) {
			out = append(out, p.pid)
		}
	}
	return out
}

const (
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	endSessionClose   = 0x00000001 // ENDSESSION_CLOSEAPP
	smtoAbortIfHung   = 0x0002
	askTimeout        = 5000 // ms, for each window to answer
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

// endSession asks the processes pids to end as Windows does before it
// closes a program, then waits up to wait for gone to say they did. Each
// top-level window of theirs is asked first whether it may end
// (WM_QUERYENDSESSION); one that says no, or gives no answer within
// askTimeout, stops it all (its late no would come too late), and the
// windows already asked are told the session goes on. Then each is told
// it ends (WM_ENDSESSION).
func endSession(pids []uint32, wait time.Duration, gone func() bool) error {
	wins := windowsOf(pids)
	if len(wins) == 0 {
		return ErrQuitTimeout
	}
	for i, w := range wins {
		answer, ok := sendTimeout(w, wmQueryEndSession, 0, endSessionClose)
		if !ok || answer == 0 {
			for _, asked := range wins[:i+1] {
				sendTimeout(asked, wmEndSession, 0, endSessionClose)
			}
			if !ok {
				return ErrQuitTimeout
			}
			return ErrQuitRefused
		}
	}
	for _, w := range wins {
		sendTimeout(w, wmEndSession, 1, endSessionClose)
	}
	for end := time.Now().Add(wait); ; time.Sleep(200 * time.Millisecond) {
		if gone() {
			return nil
		}
		if !time.Now().Before(end) {
			return ErrQuitTimeout
		}
	}
}

// windowsOf are the top-level windows of the processes pids, shown or not.
func windowsOf(pids []uint32) []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPids, enumOut = pids, nil
	_ = windows.EnumWindows(enumCB, nil)
	out := enumOut
	enumPids, enumOut = nil, nil
	return out
}

// One callback for every look (Go makes only so many in a process's life).
var (
	enumMu   sync.Mutex
	enumPids []uint32
	enumOut  []windows.HWND
	enumCB   = syscall.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(h, &pid); err == nil && slices.Contains(enumPids, pid) {
			enumOut = append(enumOut, h)
		}
		return 1
	})
)

// sendTimeout sends a message and waits for the window's answer: ok is
// false when it gave none in time.
func sendTimeout(w windows.HWND, msg uint32, wparam, lparam uintptr) (answer uintptr, ok bool) {
	r, _, _ := procSendMessageTimeoutW.Call(uintptr(w), uintptr(msg), wparam, lparam, smtoAbortIfHung,
		askTimeout, uintptr(unsafe.Pointer(&answer)))
	return answer, r != 0
}
