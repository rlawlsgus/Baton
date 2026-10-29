package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"baton/internal/fsx"
)

// UserStatusLine says whether the user's own settings for this profile and
// folder set a status line. baton's (StatusLine) would replace it: settings
// passed with --settings outrank user and project settings, and a status
// line is a single value, so baton leaves a session with its own alone.
func UserStatusLine(profileDir, cwd string) bool {
	files := []string{
		filepath.Join(profileDir, "settings.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(cwd, ".claude", "settings.local.json"),
	}
	if root, _ := gitRoots(cwd); root != "" && root != cwd {
		files = append(files, filepath.Join(root, ".claude", "settings.json"), filepath.Join(root, ".claude", "settings.local.json"))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(fsx.StripBOM(data), &m) == nil && m["statusLine"] != nil {
			return true
		}
	}
	return false
}

// StatusLine is the statusLine setting that runs `baton statusline`.
func StatusLine(batonExe string) map[string]any {
	return map[string]any{"type": "command", "command": StatusLineCommand(batonExe)}
}

// StatusLineCommand is the command line for `baton statusline`. Claude Code
// runs a status line as one string through Git Bash (PowerShell without it),
// with no exec form, and Git Bash drops backslashes. So: forward slashes, and
// no quotes, which the two shells read differently, unless the path has
// characters they treat specially even in its short 8.3 form.
func StatusLineCommand(batonExe string) string {
	p := batonExe
	if shellSpecial(p) {
		if sp := shortPath(p); sp != "" && !shellSpecial(sp) {
			p = sp
		}
	}
	p = filepath.ToSlash(p)
	if shellSpecial(p) {
		p = `"` + p + `"` // Git Bash's quoting; PowerShell needs Git Bash then
	}
	return p + " statusline"
}

func shellSpecial(p string) bool {
	return strings.ContainsAny(p, " \t'\"`$&()[]{};|<>*?#@,!")
}

func shortPath(p string) string {
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
