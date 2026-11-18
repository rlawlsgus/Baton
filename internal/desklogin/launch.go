package desklogin

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"baton/internal/winui"

	"golang.org/x/sys/windows"
)

// Launch opens the desktop app, once a switch is done and baton's desktop
// lists are in line. The packaged app is opened as Windows opens a Store
// app (shell:AppsFolder\<its app id>), never by starting its Claude.exe,
// which would hand it baton's environment; the app from the older
// installer as its Start menu entry opens it (its Update.exe
// --processStart), with the user's own environment. In a test, or with
// BATON_DESKTOP_DATA set, it does nothing. As administrator, for another
// install, or while a switch is cut off, it refuses: the user opens it.
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
		up := olderUpdater(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), data)
		if up == "" || winui.StartAsUser(up, "--processStart", "claude.exe") != nil {
			return fail(CodeLaunchUnsupported, "")
		}
		return nil
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

// appVersion is the installed desktop app's version, for a kept sign-in's
// meta: from its package name (Claude_<version>_<arch>__<publisher id>),
// or for the app from the older installer from its newest app-<version>
// folder; "" when not known (tests, another install).
func appVersion(data string) string {
	if os.Getenv("BATON_DESKTOP_DATA") != "" {
		return ""
	}
	for _, full := range packages(family(data)) {
		if parts := strings.Split(full, "_"); len(parts) >= 2 && parts[1] != "" {
			return parts[1]
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" && olderInstall(os.Getenv("APPDATA"), data) {
		_, v := newestApp(filepath.Join(la, "AnthropicClaude"))
		return v
	}
	return ""
}

// olderUpdater is the Update.exe that opens the app from the older
// installer, as its Start menu entry does (it starts the newest version it
// installed), when data is that app's data folder and the app is
// installed; "" otherwise.
func olderUpdater(localAppData, appData, data string) string {
	if localAppData == "" || !olderInstall(appData, data) {
		return ""
	}
	root := filepath.Join(localAppData, "AnthropicClaude")
	up := filepath.Join(root, "Update.exe")
	if !isFile(up) {
		return ""
	}
	if dir, _ := newestApp(root); dir == "" {
		return ""
	}
	return up
}

// newestApp is the newest app-<version> folder of the older installer's
// folder root that holds the app (claude.exe), and its version.
func newestApp(root string) (dir, version string) {
	ents, _ := os.ReadDir(root)
	var best []int
	for _, e := range ents {
		v, ok := strings.CutPrefix(strings.ToLower(e.Name()), "app-")
		nums := versionNums(v)
		if !ok || !e.IsDir() || nums == nil || !isFile(filepath.Join(root, e.Name(), "claude.exe")) {
			continue
		}
		if best == nil || slices.Compare(nums, best) > 0 {
			best, dir, version = nums, filepath.Join(root, e.Name()), v
		}
	}
	return dir, version
}

// versionNums is a version's numbers (5.11492.5), or nil when it is not
// only numbers and dots.
func versionNums(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || p[0] == '+' {
			return nil
		}
		out = append(out, n)
	}
	return out
}
