package desklogin

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Launch opens the desktop app, once a switch is done and baton's desktop
// lists are in line. It is opened as Windows opens a Store app
// (shell:AppsFolder\<its app id>), never by starting its Claude.exe, which
// would hand it baton's environment. In a test, or with BATON_DESKTOP_DATA
// set, it does nothing. As administrator, for an install that is not the
// Store app, or while a switch is cut off, it refuses: the user opens it.
func (l *Logins) Launch() error {
	if testing.Testing() || os.Getenv("BATON_DESKTOP_DATA") != "" {
		return nil
	}
	if l.Elevated != nil && l.Elevated() {
		return fail(CodeElevated, "")
	}
	data, err := l.dataDir()
	if err != nil {
		return fail(CodeLaunchUnsupported, "")
	}
	if len(stages(data, true)) > 0 {
		return fail(CodeSwitchUnrestored, "")
	}
	if l.Running != nil && l.Running() {
		return nil // open already
	}
	id := appUserModelID(data)
	if id == "" {
		return fail(CodeLaunchUnsupported, "")
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(`shell:AppsFolder\` + id)
	if err != nil {
		return fail(CodeLaunchUnsupported, "")
	}
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fail(CodeLaunchUnsupported, "")
	}
	return nil
}

// The Store app keeps its data in Claude-Data\claude_<publisher id>, after
// its package family Claude_<publisher id>.
var dataNameRx = regexp.MustCompile(`^claude_([a-z0-9]{13})$`)

// family is the package family of the app whose data folder is data ("" for
// one that is not the Store app's).
func family(data string) string {
	m := dataNameRx.FindStringSubmatch(strings.ToLower(filepath.Base(data)))
	if m == nil {
		return ""
	}
	return "Claude_" + m[1]
}

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetPackagesByPackageFamily = kernel32.NewProc("GetPackagesByPackageFamily")
	procGetPackagePathByFullName   = kernel32.NewProc("GetPackagePathByFullName")
)

// packages are the full names of the installed packages of family fam.
func packages(fam string) []string {
	if fam == "" || procGetPackagesByPackageFamily.Find() != nil {
		return nil
	}
	f, err := windows.UTF16PtrFromString(fam)
	if err != nil {
		return nil
	}
	var count, size uint32
	r, _, _ := procGetPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(&count)), 0,
		uintptr(unsafe.Pointer(&size)), 0)
	if r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || count == 0 || size == 0 {
		return nil
	}
	names := make([]*uint16, count)
	buf := make([]uint16, size)
	r, _, _ = procGetPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&names[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return nil
	}
	var out []string
	for _, n := range names[:count] {
		if n != nil {
			out = append(out, windows.UTF16PtrToString(n))
		}
	}
	return out
}

func packagePath(full string) string {
	if procGetPackagePathByFullName.Find() != nil {
		return ""
	}
	f, err := windows.UTF16PtrFromString(full)
	if err != nil {
		return ""
	}
	var n uint32
	r, _, _ := procGetPackagePathByFullName.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(&n)), 0)
	if r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	r, _, _ = procGetPackagePathByFullName.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// appUserModelID is how Windows names the desktop app to open it:
// <family>!<the app of its package whose program is app\Claude.exe>. The
// package has other apps (its ssh helpers) that must never be opened.
func appUserModelID(data string) string {
	fam := family(data)
	pk := packages(fam)
	if len(pk) == 0 {
		return ""
	}
	return fam + "!" + appID(packagePath(pk[0]))
}

func appID(pkgDir string) string {
	const fallback = "Claude"
	if pkgDir == "" {
		return fallback
	}
	raw, err := os.ReadFile(filepath.Join(pkgDir, "AppxManifest.xml"))
	if err != nil {
		return fallback
	}
	var m struct {
		Apps []struct {
			ID  string `xml:"Id,attr"`
			Exe string `xml:"Executable,attr"`
		} `xml:"Applications>Application"`
	}
	if xml.Unmarshal(raw, &m) != nil {
		return fallback
	}
	for _, a := range m.Apps {
		if strings.EqualFold(filepath.Clean(a.Exe), `app\Claude.exe`) && a.ID != "" {
			return a.ID
		}
	}
	return fallback
}

// appVersion is the installed desktop app's version, from its package
// name (Claude_<version>_<arch>__<publisher id>), for a kept sign-in's
// meta; "" when not known (tests, another install).
func appVersion(data string) string {
	if os.Getenv("BATON_DESKTOP_DATA") != "" {
		return ""
	}
	for _, full := range packages(family(data)) {
		if parts := strings.Split(full, "_"); len(parts) >= 2 && parts[1] != "" {
			return parts[1]
		}
	}
	return ""
}
