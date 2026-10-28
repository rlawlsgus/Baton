// Package claude wraps everything baton knows about claude.exe: where it is,
// which version it is, how to read a profile's login status, the settings file
// baton injects, and the files claude keeps in a profile.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// MinVersion: exec-form hooks, StopFailure and `auth status` configDirectory
// are all present from here on. TestedMax is the newest version Phase 0 ran.
const (
	MinVersion = "2.1.268"
	TestedMax  = "2.1.309"
)

type Install struct {
	Path    string
	Version string
	Source  string // config | native | path | npm | winget | desktop
}

// Resolve finds claude.exe: the first of Candidates. It never returns a
// .cmd/.ps1 shim: those put cmd.exe in the process group, which turns
// CTRL_BREAK into a "Terminate batch job" prompt.
func Resolve(configured string, allowDesktop bool) (Install, error) {
	c, err := Candidates(configured, allowDesktop)
	if err != nil {
		return Install{}, err
	}
	return c[0], nil
}

// Candidates lists every claude.exe baton may use, in the order it prefers
// them: a configured path alone; otherwise the Claude Code CLI wherever it
// is installed, then the Claude desktop app's own copy (when allowed).
func Candidates(configured string, allowDesktop bool) ([]Install, error) {
	if configured != "" {
		if !filepath.IsAbs(configured) {
			return nil, fmt.Errorf("claude_path must be a full path to claude.exe, not %s "+
				"(set it again with: baton config claude_path <full path>, or reset with: baton config claude_path auto)", configured)
		}
		if !strings.EqualFold(filepath.Ext(configured), ".exe") {
			return nil, fmt.Errorf("claude_path must point to claude.exe, not %s "+
				"(a .cmd/.ps1 wrapper breaks the clean stop baton relies on; reset with: baton config claude_path auto)", configured)
		}
		if !isFile(configured) {
			return nil, fmt.Errorf("claude_path %s does not exist (reset with: baton config claude_path auto)", configured)
		}
		return []Install{{Path: unshim(configured), Source: "config"}}, nil
	}
	var out []Install
	add := func(p, source string) {
		fi, err := os.Stat(p)
		for _, c := range out {
			// The same file under another name too: WinGet's link to its
			// package, or a short 8.3 path on PATH.
			if strings.EqualFold(c.Path, p) {
				return
			}
			if ci, cerr := os.Stat(c.Path); err == nil && cerr == nil && os.SameFile(fi, ci) {
				return
			}
		}
		out = append(out, Install{Path: p, Source: source})
	}
	home, _ := os.UserHomeDir()
	if p := filepath.Join(home, ".local", "bin", "claude.exe"); home != "" && isFile(p) {
		add(p, "native")
	}
	if p := lookPathExe("claude.exe"); p != "" {
		add(unshim(p), "path")
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		p := filepath.Join(ad, "npm", "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe")
		if isFile(p) {
			add(p, "npm")
		}
	}
	la := os.Getenv("LOCALAPPDATA")
	bundle := ""
	if la != "" {
		if p := filepath.Join(la, "Microsoft", "WinGet", "Links", "claude.exe"); isFile(p) {
			add(p, "winget")
		}
		if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "Anthropic.ClaudeCode_*", "claude.exe")); len(m) > 0 {
			add(m[0], "winget")
		}
		bundle = newestDesktopBundle(la)
	}
	if bundle != "" && allowDesktop {
		add(bundle, "desktop")
	}
	if len(out) > 0 {
		return out, nil
	}
	if bundle != "" {
		return nil, fmt.Errorf("claude.exe not found on PATH. Only the Claude desktop app's bundled copy exists (%s). "+
			"Install the CLI (PowerShell: irm https://claude.ai/install.ps1 | iex) or allow the bundled copy with: "+
			"baton config allow_desktop_bundle true", bundle)
	}
	return nil, errors.New("Claude Code not found. Install the Claude desktop app (https://claude.ai/download), " +
		"which includes it, or the Claude Code CLI (PowerShell: irm https://claude.ai/install.ps1 | iex), " +
		"or set its path with: baton config claude_path <path to claude.exe>")
}

// ResolveUsable is the first of Candidates that runs and is new enough, with
// its version: a Claude Code CLI installed long ago and never updated must
// not hide the desktop app's newer copy. skipped are the ones passed over,
// with their versions where known. When none can be used, the first comes
// back with its error (a *TooOldError when it is too old).
func ResolveUsable(configured string, allowDesktop bool, env []string) (inst Install, skipped []Install, err error) {
	cands, err := Candidates(configured, allowDesktop)
	if err != nil {
		return Install{}, nil, err
	}
	var firstErr error
	for i, c := range cands {
		v, verr := Version(c.Path, env)
		c.Version = v
		if verr == nil {
			_, verr = CheckVersion(v)
		}
		if verr == nil {
			return c, skipped, nil
		}
		if i == 0 {
			firstErr = verr
		}
		skipped = append(skipped, c)
	}
	return skipped[0], skipped[1:], firstErr
}

// unshim follows a Scoop shim (claude.exe next to claude.shim holding
// `path = "<real exe>"`) to the real claude.exe, so baton supervises claude
// itself rather than a launcher process in front of it.
func unshim(exe string) string {
	data, err := os.ReadFile(strings.TrimSuffix(exe, filepath.Ext(exe)) + ".shim")
	if err != nil {
		return exe
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != "path" {
			continue
		}
		target := strings.Trim(strings.TrimSpace(v), `"`)
		if strings.EqualFold(filepath.Ext(target), ".exe") && isFile(target) {
			return target
		}
	}
	return exe
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// lookPathExe searches PATH for an exact .exe name, skipping the WindowsApps
// app-execution aliases (zero-byte reparse points that are not the real CLI).
func lookPathExe(name string) string {
	self, _ := os.Executable()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || strings.Contains(strings.ToLower(dir), `\microsoft\windowsapps`) {
			continue
		}
		p := filepath.Join(dir, name)
		if isFile(p) && !strings.EqualFold(p, self) {
			return p
		}
	}
	return ""
}

func newestDesktopBundle(localAppData string) string {
	m, _ := filepath.Glob(filepath.Join(localAppData, "Claude-Data", "*", "claude-code", "*", "*", "claude.exe"))
	sort.Slice(m, func(i, j int) bool {
		vi := filepath.Base(filepath.Dir(filepath.Dir(m[i])))
		vj := filepath.Base(filepath.Dir(filepath.Dir(m[j])))
		return CompareVersions(vi, vj) > 0
	})
	// The newest copy that is new enough and finished: an app update writes
	// the next version's folder while the old one is still in use.
	for _, p := range m {
		v := filepath.Base(filepath.Dir(filepath.Dir(p)))
		fi, err := os.Stat(p)
		if err == nil && fi.Size() > 0 && time.Since(fi.ModTime()) > 30*time.Second && CompareVersions(v, MinVersion) >= 0 {
			return p
		}
	}
	return ""
}

// DesktopBundle is the Claude desktop app's own Claude Code, or "".
func DesktopBundle() string {
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		return newestDesktopBundle(la)
	}
	return ""
}

// DesktopAppInstalled reports whether the Claude desktop app is installed,
// whether or not it has set up its Claude Code yet (it does so the first
// time the Code tab is opened).
func DesktopAppInstalled() bool {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return false
	}
	for _, p := range []string{
		filepath.Join(la, "Claude-Data"),
		filepath.Join(la, "AnthropicClaude"),
	} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return true
		}
	}
	m, _ := filepath.Glob(filepath.Join(la, "Packages", "Claude_*"))
	return len(m) > 0
}

// UserClaudeJSONDir is the folder holding the .claude.json of the user's own
// Claude Code setup (also used by the desktop app's Code tab): the home
// folder for the default ~/.claude, or a custom CLAUDE_CONFIG_DIR itself.
func UserClaudeJSONDir(userHome string) string {
	if strings.EqualFold(filepath.Base(userHome), ".claude") {
		return filepath.Dir(userHome)
	}
	return userHome
}

var versionRx = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Version runs `claude --version`.
func Version(exe string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	cmd.Env = env
	cmd.SysProcAttr = hidden()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", exe, err)
	}
	m := versionRx.FindString(string(out))
	if m == "" {
		return "", fmt.Errorf("%s --version: unexpected output %q", exe, strings.TrimSpace(string(out)))
	}
	return m, nil
}

// hidden runs a console program without a console window. The baton window
// (baton-app.exe) has no console of its own, so without this every check it
// makes would flash a black window.
func hidden() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// CompareVersions compares dotted numeric versions (missing parts count as 0).
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	m := versionRx.FindStringSubmatch(v)
	if m == nil {
		return out
	}
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// TooOldError: the Claude Code found is older than MinVersion.
type TooOldError struct{ Version string }

func (e *TooOldError) Error() string {
	return fmt.Sprintf("claude %s is too old; baton needs %s or newer (run: claude update)", e.Version, MinVersion)
}

// CheckVersion returns an error below MinVersion and a warning above TestedMax.
func CheckVersion(v string) (warning string, err error) {
	if CompareVersions(v, MinVersion) < 0 {
		return "", &TooOldError{Version: v}
	}
	if CompareVersions(v, TestedMax) > 0 {
		return fmt.Sprintf("claude %s is newer than the newest version tested with this baton (%s)", v, TestedMax), nil
	}
	return "", nil
}
