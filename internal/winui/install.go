package winui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Files that make up an installed baton (the first two are required).
var installFiles = []string{"baton-app.exe", "baton.exe", "README.md", "README.ko.md", "LICENSE", "THIRD_PARTY_NOTICES.txt"}

var uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\baton` // a var for tests

// InstallDir is where baton installs itself for the current user (no admin).
func InstallDir() string {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return ""
	}
	return filepath.Join(la, "Programs", "baton")
}

// ShortcutPath is the Start menu entry.
func ShortcutPath() string {
	ad := os.Getenv("APPDATA")
	if ad == "" {
		return ""
	}
	return filepath.Join(ad, "Microsoft", "Windows", "Start Menu", "Programs", "baton.lnk")
}

// Installed reports whether exe runs from the install folder.
func Installed(exe string) bool {
	d := InstallDir()
	return d != "" && strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(d))
}

// Install copies baton from srcDir to the install folder, adds a Start menu
// entry with the window's taskbar identity (so a pinned icon keeps working)
// and an entry in Windows' installed apps list.
func Install(srcDir, version, appID string) (string, error) {
	dst := InstallDir()
	if dst == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	// Copy everything next to the old files first, then swap them in. A
	// running program cannot be overwritten, but it can be renamed: the
	// installed baton.exe of a running session becomes baton.exe.old and is
	// deleted the next time (or by the uninstall).
	var names []string
	for i, name := range installFiles {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err != nil {
			if i < 2 {
				return "", fmt.Errorf("%s is missing next to baton-app.exe", name)
			}
			continue
		}
		removeOld(dst, name)
		if err := copyFile(src, filepath.Join(dst, name+".new")); err != nil {
			removeStaged(dst, names)
			_ = os.Remove(filepath.Join(dst, name+".new"))
			return "", fmt.Errorf("copy %s: %w", name, err)
		}
		names = append(names, name)
	}
	if err := swapIn(dst, names); err != nil {
		return "", fmt.Errorf("%w (is the installed baton still open?)", err)
	}
	app := filepath.Join(dst, "baton-app.exe")
	if err := CreateShortcut(ShortcutPath(), app, dst, "baton - Claude Code across your Claude accounts", appID); err != nil {
		return "", fmt.Errorf("Start menu entry: %w", err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	for name, v := range map[string]string{
		"DisplayName":     "baton",
		"DisplayVersion":  version,
		"Publisher":       "baton",
		"InstallLocation": dst,
		"DisplayIcon":     app + ",0",
		"UninstallString": `"` + filepath.Join(dst, "baton.exe") + `" uninstall`,
	} {
		if err := k.SetStringValue(name, v); err != nil {
			return "", err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return app, nil
}

// RemoveInstallEntries removes the Start menu entry and the installed-apps
// entry (the install folder itself goes after the program exits).
func RemoveInstallEntries() {
	if p := ShortcutPath(); p != "" {
		_ = os.Remove(p)
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// swapIn moves each staged <name>.new into place, keeping the old file as
// <name>.old until the end. If any step fails, the old files come back.
func swapIn(dir string, names []string) error {
	aside := map[string]string{} // name -> where its old file went
	var placed []string
	undo := func() {
		for _, n := range placed {
			_ = os.Remove(filepath.Join(dir, n))
		}
		for n, old := range aside {
			_ = os.Rename(old, filepath.Join(dir, n))
		}
		removeStaged(dir, names)
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Lstat(p); err == nil {
			// .old may still be a running program set aside by an earlier
			// update: then this one gets a name of its own.
			old := p + ".old"
			if _, err := os.Lstat(old); err == nil {
				old = fmt.Sprintf("%s.old-%d", p, time.Now().UnixNano())
			}
			if err := renameRetry(p, old); err != nil {
				undo()
				return fmt.Errorf("replace %s: %w", n, err)
			}
			aside[n] = old
		}
		if err := renameRetry(p+".new", p); err != nil {
			undo()
			return fmt.Errorf("replace %s: %w", n, err)
		}
		placed = append(placed, n)
	}
	for _, old := range aside {
		_ = os.Remove(old) // fails while it runs; removed next time
	}
	return nil
}

// removeOld deletes the files earlier updates set aside for name, where it can.
func removeOld(dir, name string) {
	olds, _ := filepath.Glob(filepath.Join(dir, name+".old*"))
	for _, o := range olds {
		_ = os.Remove(o)
	}
}

// renameRetry renames, trying again for a few seconds: a file written moments
// ago is often still open in a virus scanner.
func renameRetry(from, to string) error {
	var err error
	for i := 0; i < 25; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return err
}

func removeStaged(dir string, names []string) {
	for _, n := range names {
		_ = os.Remove(filepath.Join(dir, n+".new"))
	}
}

// ---- shortcut (COM) ------------------------------------------------------------------

var (
	clsidShellLink   = windows.GUID{Data1: 0x00021401, Data2: 0, Data3: 0, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW    = windows.GUID{Data1: 0x000214F9, Data2: 0, Data3: 0, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile   = windows.GUID{Data1: 0x0000010B, Data2: 0, Data3: 0, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPropertyStore = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446, Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}
	pkeyAppUserModel = struct {
		fmtid windows.GUID
		pid   uint32
	}{windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}, 5}
)

const (
	slQueryInterface  = 0
	slSetDescription  = 7
	slSetWorkingDir   = 9
	slSetIconLocation = 17
	slSetPath         = 20
	pfSave            = 6
	psSetValue        = 6
	psCommit          = 7
)

// CreateShortcut writes a .lnk to target, carrying the app's taskbar id.
func CreateShortcut(lnk, target, workDir, description, appID string) error {
	runtimeLock := make(chan error, 1)
	go func() { // COM needs an apartment of its own on a locked thread
		runtimeLock <- createShortcut(lnk, target, workDir, description, appID)
	}()
	return <-runtimeLock
}

func createShortcut(lnk, target, workDir, description, appID string) error {
	lockThread()
	defer unlockThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, windows.Errno(1)) {
		return err
	}
	defer windows.CoUninitialize()
	var sl *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&sl))); hr != 0 || sl == nil {
		return fmt.Errorf("ShellLink: 0x%08x", uint32(hr))
	}
	defer comCall(sl, vtRelease)
	str := func(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }
	targetW, lnkW := str(target), str(lnk)
	workW, descW := str(workDir), str(description)
	comCall(sl, slSetPath, uintptr(unsafe.Pointer(targetW)))
	comCall(sl, slSetWorkingDir, uintptr(unsafe.Pointer(workW)))
	comCall(sl, slSetDescription, uintptr(unsafe.Pointer(descW)))
	comCall(sl, slSetIconLocation, uintptr(unsafe.Pointer(targetW)), 0)
	defer runtime.KeepAlive([]*uint16{targetW, lnkW, workW, descW})

	var ps *comObject
	if hr := comCall(sl, slQueryInterface, uintptr(unsafe.Pointer(&iidPropertyStore)), uintptr(unsafe.Pointer(&ps))); hr == 0 && ps != nil {
		id, _ := windows.UTF16PtrFromString(appID)
		var pv [24]byte // PROPVARIANT: vt=VT_LPWSTR, then the string pointer at offset 8
		pv[0] = 31
		*(*uintptr)(unsafe.Pointer(&pv[8])) = uintptr(unsafe.Pointer(id))
		comCall(ps, psSetValue, uintptr(unsafe.Pointer(&pkeyAppUserModel)), uintptr(unsafe.Pointer(&pv)))
		comCall(ps, psCommit)
		comCall(ps, vtRelease)
		runtime.KeepAlive(id) // only a uintptr in pv points at it
	}
	var pf *comObject
	if hr := comCall(sl, slQueryInterface, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&pf))); hr != 0 || pf == nil {
		return fmt.Errorf("IPersistFile: 0x%08x", uint32(hr))
	}
	defer comCall(pf, vtRelease)
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	if hr := comCall(pf, pfSave, uintptr(unsafe.Pointer(lnkW)), 1); hr != 0 {
		return fmt.Errorf("save shortcut: 0x%08x", uint32(hr))
	}
	return nil
}

// ---- finishing an uninstall --------------------------------------------------------

// FinishUninstall deletes the install folder once process pid has exited.
// It runs from a copy of baton.exe outside that folder.
func FinishUninstall(dir string, pid int) error {
	want := InstallDir()
	if want == "" || !strings.EqualFold(filepath.Clean(dir), filepath.Clean(want)) {
		return errors.New("refusing to delete a folder that is not baton's install folder")
	}
	if h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)); err == nil {
		// However long its window stays open: this helper is invisible.
		_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
		windows.CloseHandle(h)
	}
	_ = os.Chdir(os.TempDir()) // a folder cannot be removed while it is the current one
	for _, name := range installFiles {
		_ = os.Remove(filepath.Join(dir, name))
		_ = os.Remove(filepath.Join(dir, name+".new"))
		removeOld(dir, name)
	}
	return os.Remove(dir) // only if nothing else was put there
}

// StartDetached starts a program that outlives this one, without a window,
// in exe's own folder.
func StartDetached(exe string, args ...string) error {
	cmd, err := startWithFallback(func() *exec.Cmd {
		cmd := exec.Command(exe, args...)
		cmd.Dir = filepath.Dir(exe)
		return cmd
	}, windows.DETACHED_PROCESS)
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}

// DeleteAfterExit deletes file a moment after this process has exited (a
// running program cannot delete itself).
func DeleteAfterExit(file string) error {
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	cmd, err := startWithFallback(func() *exec.Cmd {
		cmd := exec.Command(cmdExe)
		cmd.Dir = os.TempDir()
		// A raw command line: cmd.exe does its own quoting.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /c ping -n 3 127.0.0.1 >nul & del /f /q "` + file + `"`}
		return cmd
	}, windows.CREATE_NO_WINDOW)
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}

func lockThread()   { runtime.LockOSThread() }
func unlockThread() { runtime.UnlockOSThread() }
