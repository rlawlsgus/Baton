package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// sessionsDir is the folder of the desktop app's data folder that holds one
// list of Code sessions per account and organization:
// claude-code-sessions\<accountUuid>\<orgUuid>\local_<id>.json.
const sessionsDir = "claude-code-sessions"

// DataDir is the Claude desktop app's own data folder: the one holding its
// per-account session lists and its config.json. It is "" when there is
// none, and then baton does not offer to show sessions there.
// BATON_DESKTOP_DATA names it instead (tests and the end-to-end test); a Go
// test binary never looks for the real one.
func DataDir() string {
	if d, set := dataOverride(); set {
		return d
	}
	if testing.Testing() {
		return ""
	}
	return findDataDir(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"))
}

// dataOverride is the data folder BATON_DESKTOP_DATA names instead of the
// real one. "none" names none: there is no desktop app (the test build's
// default, where a folder that is not there would still count as the app's,
// and offer what then fails). set: the variable is set, to a folder or to
// none.
func dataOverride() (dir string, set bool) {
	switch v := os.Getenv("BATON_DESKTOP_DATA"); {
	case v == "":
		return "", false
	case strings.EqualFold(v, "none"):
		return "", true
	default:
		return filepath.Clean(v), true
	}
}

// findDataDir looks where the desktop app keeps its data: a folder of
// %LOCALAPPDATA%\Claude-Data (the Store app names it after its package
// family), that folder itself, or the older %APPDATA%\Claude, unless the app
// moved its data from there (it leaves a .user-data-left.* note behind).
// Claude-3p, the build for cloud providers, has no claude.ai accounts.
func findDataDir(localAppData, appData string) string {
	holds := func(d string) bool {
		return isDir(filepath.Join(d, sessionsDir)) || isFile(filepath.Join(d, "config.json"))
	}
	if localAppData != "" {
		root := filepath.Join(localAppData, "Claude-Data")
		ents, _ := os.ReadDir(root)
		best, bestSessions, bestMod := "", false, time.Time{}
		for _, e := range ents {
			d := filepath.Join(root, e.Name())
			if !e.IsDir() || !holds(d) {
				continue
			}
			// The one with session lists first, then the one used last.
			sessions := isDir(filepath.Join(d, sessionsDir))
			mod := modTime(filepath.Join(d, "config.json"))
			if m := modTime(filepath.Join(d, sessionsDir)); m.After(mod) {
				mod = m
			}
			if best == "" || sessions && !bestSessions || sessions == bestSessions && mod.After(bestMod) {
				best, bestSessions, bestMod = d, sessions, mod
			}
		}
		if best != "" {
			return best
		}
		if holds(root) {
			return root
		}
	}
	if appData != "" {
		d := filepath.Join(appData, "Claude")
		if left, _ := filepath.Glob(filepath.Join(d, ".user-data-left.*")); len(left) == 0 && holds(d) {
			return d
		}
	}
	return ""
}

// allData are every data folder of the desktop app whose lists a delete
// looks through: the one baton shows sessions in (Data), and every other
// one the app may read lists from again (another build, or an older
// install it may go back to). BATON_DESKTOP_DATA names the only other one
// instead, or none ("none"; tests and the end-to-end test); a Go test binary
// never looks for the real ones. unread: a folder that holds data folders
// could not be read right now.
func (d *Desk) allData() (dirs []string, unread bool) {
	add := func(p string) {
		if p != "" && !slices.ContainsFunc(dirs, func(q string) bool { return samePath(q, p) }) {
			dirs = append(dirs, p)
		}
	}
	add(d.Data())
	if v, set := dataOverride(); set {
		add(v)
		return dirs, false
	}
	if testing.Testing() {
		return dirs, false
	}
	more, unread := dataDirs(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"))
	for _, p := range more {
		add(p)
	}
	return dirs, unread
}

// dataDirs are the folders the desktop app may keep lists in, whichever
// findDataDir picks: every folder of %LOCALAPPDATA%\Claude-Data, that folder
// itself, and %APPDATA%\Claude, also after the app moved its data from there
// (what it left there is still on disk). A folder that holds no lists is
// harmless to name.
func dataDirs(localAppData, appData string) (out []string, unread bool) {
	if localAppData != "" {
		root := filepath.Join(localAppData, "Claude-Data")
		ents, err := os.ReadDir(root)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			unread = true
		}
		for _, e := range ents {
			if d := filepath.Join(root, e.Name()); isDir(d) {
				out = append(out, d)
			}
		}
		out = append(out, root)
	}
	if appData != "" {
		out = append(out, filepath.Join(appData, "Claude"))
	}
	return out, unread
}

// Active is the account the desktop app is signed in to now
// (lastKnownAccountUuid in its config.json, the only key read there), or "".
// It reads the way a delete still can replace the file meanwhile: it is
// asked every second, and a switch of the app's sign-in writes that file.
func Active(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	data, err := fsx.ReadFileShared(filepath.Join(dataDir, "config.json"))
	if err != nil || len(data) > 16<<20 {
		return ""
	}
	var c struct {
		Account string `json:"lastKnownAccountUuid"`
	}
	if json.Unmarshal(fsx.StripBOM(data), &c) != nil {
		return ""
	}
	if a := strings.ToLower(strings.TrimSpace(c.Account)); validID(a) {
		return a
	}
	return ""
}

var uuidRx = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// validID accepts an account, organization or conversation id: a lower
// case uuid, which is safe as a folder or file name.
func validID(s string) bool { return uuidRx.MatchString(s) }

// ValidID is validID, for the packages beside this one that name folders by
// account id (internal/desklogin).
func ValidID(s string) bool { return validID(s) }

// AppRunning reports whether the Claude desktop app runs now. While it does,
// it keeps the list of the account it is signed in to in memory: a record
// written there shows only after it is opened again, and one removed can be
// written back. BATON_DESKTOP_RUNNING=0 or 1 says instead (tests).
func AppRunning() bool {
	switch os.Getenv("BATON_DESKTOP_RUNNING") {
	case "1":
		return true
	case "0":
		return false
	}
	if testing.Testing() {
		return false
	}
	for _, p := range processImages("claude.exe") {
		if IsAppImage(p) {
			return true
		}
	}
	return false
}

// IsAppImage tells the desktop app's own claude.exe from the copies of
// Claude Code it keeps (Claude-Data\...\claude-code\<version>\...) and from
// the Claude Code CLI: the Store app runs from WindowsApps\Claude_*\app, the
// older installer from AnthropicClaude\app-<version>, and either has its
// resources\app.asar beside it.
func IsAppImage(p string) bool {
	low := strings.ToLower(filepath.Clean(p))
	if filepath.Base(low) != "claude.exe" || strings.Contains(low, `\claude-code\`) {
		return false
	}
	dir := filepath.Dir(low)
	switch {
	case strings.Contains(low, `\windowsapps\claude_`) && filepath.Base(dir) == "app":
		return true
	case strings.HasPrefix(filepath.Base(dir), "app-") && filepath.Base(filepath.Dir(dir)) == "anthropicclaude":
		return true
	}
	return isFile(filepath.Join(filepath.Dir(p), "resources", "app.asar"))
}

// processImages lists the full paths of the running processes named name.
func processImages(name string) []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []string
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
			out = append(out, windows.UTF16ToString(buf[:n]))
		}
		windows.CloseHandle(h)
	}
	return out
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func modTime(p string) time.Time {
	if fi, err := os.Stat(p); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}
