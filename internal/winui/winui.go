// Package winui holds the Windows pieces of baton's GUI around its window:
// the native folder dialog, single-instance signalling and shell helpers.
package winui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	kernel32                          = windows.NewLazySystemDLL("kernel32.dll")
	user32                            = windows.NewLazySystemDLL("user32.dll")
	procGetUserDefaultLocaleName      = kernel32.NewProc("GetUserDefaultLocaleName")
	procGetUserDefaultUILanguage      = kernel32.NewProc("GetUserDefaultUILanguage")
	procGetConsoleProcessList         = kernel32.NewProc("GetConsoleProcessList")
	procSetConsoleTitleW              = kernel32.NewProc("SetConsoleTitleW")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
)

// UserLang is "ko" when Windows is set to Korean (region format or display
// language), else "en".
func UserLang() string {
	var buf [85]uint16
	if r, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r != 0 {
		if strings.HasPrefix(strings.ToLower(windows.UTF16ToString(buf[:])), "ko") {
			return "ko"
		}
	}
	if r, _, _ := procGetUserDefaultUILanguage.Call(); r&0x3ff == 0x12 { // LANG_KOREAN
		return "ko"
	}
	return "en"
}

// OwnConsole reports whether this process is alone on its console, i.e. it
// was started by double-clicking it rather than from a terminal.
func OwnConsole() bool {
	var pids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// SetConsoleTitle names the console window.
func SetConsoleTitle(title string) {
	if p, err := windows.UTF16PtrFromString(title); err == nil {
		_, _, _ = procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
	}
}

// EnableDPIAwareness keeps native dialogs sharp on high-DPI screens.
func EnableDPIAwareness() {
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	if procSetProcessDpiAwarenessContext.Find() == nil {
		_, _, _ = procSetProcessDpiAwarenessContext.Call(perMonitorV2)
	}
}

// OpenFolder shows a folder in Explorer.
func OpenFolder(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error { return OpenFolder(url) }

// ---- single instance ------------------------------------------------------------

// Instance is this GUI's claim on its data folder: one GUI per BATON_HOME.
type Instance struct {
	mutex windows.Handle
	show  windows.Handle
}

func instanceNames(home string) (string, string) {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(home))))
	id := hex.EncodeToString(sum[:8])
	return `Local\baton-gui-` + id, `Local\baton-gui-show-` + id
}

// ClaimInstance returns the instance, or ok=false after asking the GUI that
// already runs for home to bring up a window.
func ClaimInstance(home string) (inst *Instance, ok bool, err error) {
	mName, eName := instanceNames(home)
	mp, _ := windows.UTF16PtrFromString(mName)
	ep, _ := windows.UTF16PtrFromString(eName)
	m, err := windows.CreateMutex(nil, false, mp)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, err
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(m)
		LetOthersComeForward() // so the running GUI may bring its window to the front
		if e, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ep); err == nil {
			_ = windows.SetEvent(e)
			windows.CloseHandle(e)
		}
		return nil, false, nil
	}
	e, err := windows.CreateEvent(nil, 0, 0, ep)
	if err != nil {
		windows.CloseHandle(m)
		return nil, false, err
	}
	return &Instance{mutex: m, show: e}, true, nil
}

// ShowRequests delivers a value each time another launch asked for a window.
func (i *Instance) ShowRequests() <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		for {
			ev, err := windows.WaitForSingleObject(i.show, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch
}

func (i *Instance) Close() {
	windows.CloseHandle(i.show)
	windows.CloseHandle(i.mutex)
}

// ---- the app window ----------------------------------------------------------------

// FindEdge returns msedge.exe (for an InPrivate sign-in window). Only the
// machine-wide install locations count, not per-user ones another program
// running as the user could point elsewhere.
func FindEdge() string {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe`, registry.QUERY_VALUE); err == nil {
		p, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && fileExists(p) {
			return p
		}
	}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if d := os.Getenv(env); d != "" {
			if p := filepath.Join(d, "Microsoft", "Edge", "Application", "msedge.exe"); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

var procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")

// LetOthersComeForward lets the next window another process opens (a sign-in
// or session console, the running GUI) take the foreground.
func LetOthersComeForward() {
	const asfwAny = ^uintptr(0)
	_, _, _ = procAllowSetForegroundWindow.Call(asfwAny)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// HiddenProcAttr starts a console program without showing a window.
func HiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
}

// NewConsoleProcAttr starts a console program in a console window of its own.
func NewConsoleProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
}

var procMessageBoxW = user32.NewProc("MessageBoxW")

// MessageBox shows an error where there is no console to print it.
func MessageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	const mbIconError = 0x10
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}
