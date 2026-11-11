// Package claude wraps everything baton knows about claude.exe: where it is,
// which version it is, how to read a profile's login status, the settings file
// baton injects, and the files claude keeps in a profile.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"baton/internal/fsx"
)

// MinVersion is the oldest Claude Code baton uses. What baton relies on, with
// the Claude Code version that brought it (from Claude Code's changelog):
//   - hooks in exec form (a hook's `args`, run without a shell): 2.1.139
//   - the StopFailure hook, which is how baton sees a usage limit: 2.1.78
//   - rate_limits in the status line's input (usage in the window): 2.1.80
//   - claude auth login/status/logout: 2.1.41
//   - --settings: 1.0.61
//   - claude.exe as a native program in every install: 2.1.113
//   - no Git Bash needed: 2.1.120
//
// `auth status` configDirectory (2.1.268) is read but not used. So 2.1.139
// would do on paper, but no version older than 2.1.308 has run baton's E2E
// suite: lower this only after that suite passes on the builds in between.
const MinVersion = "2.1.268"

// TestedWith is the newest Claude Code baton's E2E suite passed on. It is
// only shown (baton doctor): Claude Code ships about once a day and newer
// versions are used as they come. One that breaks baton goes in knownBad.
const TestedWith = "2.1.317"

// knownBad lists Claude Code versions that break baton (version: why). Such
// a copy is passed over while another one works, and used only when there is
// nothing else.
var knownBad = map[string]string{}

// Why a copy of Claude Code cannot be used (Install.Problem, Fallback.Why).
const (
	ProblemTooOld      = "too_old"
	ProblemNotRunnable = "not_runnable"
	ProblemInstalling  = "installing" // the desktop app is still writing it
	ProblemWrapper     = "wrapper"    // a .cmd/.ps1: never run, see Discover
	ProblemKnownBad    = "known_bad"
	ProblemMissing     = "missing" // a chosen path that is not there
	// ProblemNotSetUp is only a Fallback.Why: claude_path is DesktopChoice and
	// the desktop app has no Claude Code (its Code tab was never opened).
	ProblemNotSetUp = "not_set_up"
)

// Install is one copy of Claude Code.
type Install struct {
	Path    string
	Version string // "" when unknown (a wrapper, or a copy that does not start)
	Source  string // native | path | scoop | npm | winget | desktop | config
	// Problem is why it cannot be used ("" when it can); Detail says more
	// (the error, or the reason a version is known to be bad).
	Problem string
	Detail  string
}

// Desktop reports whether this is one of the Claude desktop app's copies.
func (i Install) Desktop() bool { return i.Source == "desktop" }

// DesktopChoice is the claude_path value that always means the Claude
// desktop app's own Claude Code (its folder changes with every app update).
const DesktopChoice = "desktop"

// Discover lists every Claude Code on this PC, in the order baton prefers
// them when two have the same version: the CLI wherever it is installed, then
// each copy the Claude desktop app keeps, newest first. env is what
// `claude --version` runs with (versions are cached, see UseVersionCache).
//
// A .cmd/.ps1 on PATH is listed as a wrapper and never used: it puts cmd.exe
// in the process group, which turns CTRL_BREAK into a "Terminate batch job"
// prompt.
func Discover(env []string) []Install {
	var out []Install
	add := func(i Install) {
		for _, c := range out {
			// The same file under another name too: WinGet's link to its
			// package, or a short 8.3 path on PATH.
			if sameFile(c.Path, i.Path) {
				return
			}
		}
		out = append(out, i)
	}
	home, _ := os.UserHomeDir()
	if p := filepath.Join(home, ".local", "bin", "claude.exe"); home != "" && isFile(p) {
		add(Install{Path: p, Source: "native"})
	}
	for _, p := range lookPathAll() {
		if !strings.EqualFold(filepath.Ext(p), ".exe") {
			add(Install{Path: p, Source: "path", Problem: ProblemWrapper})
			continue
		}
		real := unshim(p)
		add(Install{Path: real, Source: pathSource(real)})
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		if p := filepath.Join(ad, "npm", "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe"); isFile(p) {
			// npm's claude.cmd only starts this claude.exe: one install, not
			// a second copy that cannot be used.
			out = slices.DeleteFunc(out, func(i Install) bool {
				return i.Problem == ProblemWrapper && strings.EqualFold(filepath.Dir(i.Path), filepath.Join(ad, "npm"))
			})
			add(Install{Path: p, Source: "npm"})
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		if p := filepath.Join(la, "Microsoft", "WinGet", "Links", "claude.exe"); isFile(p) {
			add(Install{Path: p, Source: "winget"})
		}
		m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "Anthropic.ClaudeCode_*", "claude.exe"))
		for _, p := range m {
			add(Install{Path: p, Source: "winget"})
		}
	}
	scoop := os.Getenv("SCOOP")
	if scoop == "" && home != "" {
		scoop = filepath.Join(home, "scoop")
	}
	if p := filepath.Join(scoop, "shims", "claude.exe"); scoop != "" && isFile(p) {
		add(Install{Path: unshim(p), Source: "scoop"})
	}
	for i := range out {
		if out[i].Problem == "" {
			v, err := cachedVersion(out[i].Path, env)
			if err != nil {
				out[i].Problem, out[i].Detail = ProblemNotRunnable, err.Error()
			}
			out[i].Version = v
		}
	}
	for _, d := range desktopCopies() {
		i := Install{Path: d.path, Version: d.version, Source: "desktop"}
		if !d.finished {
			i.Problem = ProblemInstalling
		}
		add(i)
	}
	for i := range out {
		judge(&out[i])
	}
	return out
}

// judge marks a copy whose version baton cannot use.
func judge(i *Install) {
	if i.Problem != "" || i.Version == "" {
		return
	}
	if CheckVersion(i.Version) != nil {
		i.Problem = ProblemTooOld
	} else if why, bad := knownBad[i.Version]; bad {
		i.Problem, i.Detail = ProblemKnownBad, why
	}
}

// names reports whether claude_path pref names a copy: the same file, or the
// claude.exe that a Scoop shim at pref leads to (`where claude` prints the
// shim, so that is the path people paste).
func names(pref string) func(Install) bool {
	real := unshim(pref)
	return func(i Install) bool { return sameFile(i.Path, pref) || sameFile(i.Path, real) }
}

// Chosen is the copy in found that claude_path pref names.
func Chosen(pref string, found []Install) (Install, bool) {
	if n := slices.IndexFunc(found, names(pref)); n >= 0 {
		return found[n], true
	}
	return Install{}, false
}

// withChosen adds a claude_path that Discover did not find (a copy somewhere
// else, or one that is gone), so it can be shown and chosen like the others.
// One of the desktop app's versions that is gone is left out: the app removed
// it when it updated, and Choose follows the app's copies instead.
func withChosen(found []Install, path string, env []string) []Install {
	if _, ok := Chosen(path, found); ok {
		return found
	}
	if IsDesktopCopy(path) && !isFile(path) {
		return found
	}
	i := Install{Path: path, Source: "config"}
	if IsDesktopCopy(path) {
		i.Source = "desktop"
		i.Version = versionRx.FindString(filepath.Base(filepath.Dir(filepath.Dir(path))))
	}
	switch {
	case !filepath.IsAbs(path) || !isFile(path):
		i.Problem = ProblemMissing
	case !strings.EqualFold(filepath.Ext(path), ".exe"):
		i.Problem = ProblemWrapper
	case !i.Desktop():
		i.Path = unshim(path)
		v, err := cachedVersion(i.Path, env)
		if err != nil {
			i.Problem, i.Detail = ProblemNotRunnable, err.Error()
		}
		i.Version = v
	}
	judge(&i)
	return append(found, i)
}

// Fallback says that the Claude Code chosen in claude_path cannot be used
// right now, so another one is used until it can again.
type Fallback struct {
	Wanted string // claude_path: DesktopChoice or a full path
	// Why is a Problem of the chosen copy: ProblemMissing when it is gone,
	// ProblemNotSetUp when the desktop app has no copy at all.
	Why string
}

// Choose picks the copy to use from found (see Discover) for claude_path pref:
// "" is automatic, the newest copy that works (the desktop app's only when
// allowDesktop); DesktopChoice is the desktop app's newest; anything else a
// full path. A choice that cannot be used now gives way to another copy, with
// a Fallback saying so: baton never fails while any copy works.
//
// A path to one of the desktop app's versions gives way to the app's newest
// copy first. Once that version is gone (the app removes old versions when
// it updates), it simply follows the app's copies as DesktopChoice does: a
// version that will never come back is not reported on every start.
func Choose(pref string, allowDesktop bool, found []Install) (Install, *Fallback, error) {
	auto := func(i Install) bool { return allowDesktop || !i.Desktop() }
	want := auto
	var instead []func(Install) bool
	var fb *Fallback
	follow := strings.EqualFold(pref, DesktopChoice)
	if !follow && pref != "" && IsDesktopCopy(pref) {
		_, there := Chosen(pref, found)
		follow = !there
	}
	switch {
	case pref == "":
	case follow:
		want = Install.Desktop
		instead = []func(Install) bool{auto}
		fb = &Fallback{Wanted: DesktopChoice, Why: ProblemNotSetUp}
		for _, i := range found {
			if i.Desktop() {
				fb.Why = i.Problem // the newest copy's
				break
			}
		}
	default:
		want = names(pref)
		instead = []func(Install) bool{auto}
		if IsDesktopCopy(pref) {
			instead = []func(Install) bool{Install.Desktop, auto}
		}
		fb = &Fallback{Wanted: pref, Why: ProblemMissing}
		for _, i := range found {
			if want(i) {
				fb.Why = i.Problem
				break
			}
		}
	}
	// A copy with a known problem is better than none.
	for _, level := range []string{"", ProblemKnownBad} {
		if i, ok := newest(found, want, level); ok {
			return i, nil, nil
		}
		for _, alt := range instead {
			if i, ok := newest(found, alt, level); ok {
				return i, fb, nil
			}
		}
	}
	return Install{}, nil, &NoUsableError{Found: found, Pref: pref, AllowDesktop: allowDesktop}
}

// newest is the newest copy that keep accepts and whose Problem is problem.
// On a tie the CLI wins (its path does not change with app updates), then
// the one found first.
func newest(found []Install, keep func(Install) bool, problem string) (Install, bool) {
	var best Install
	ok := false
	for _, i := range found {
		if i.Problem != problem || !keep(i) {
			continue
		}
		if !ok {
			best, ok = i, true
			continue
		}
		if c := CompareVersions(i.Version, best.Version); c > 0 || c == 0 && best.Desktop() && !i.Desktop() {
			best = i
		}
	}
	return best, ok
}

// ResolveUsable finds every Claude Code (Discover) and picks the one to use
// (Choose). all is everything found, the chosen claude_path included.
func ResolveUsable(configured string, allowDesktop bool, env []string) (inst Install, all []Install, fb *Fallback, err error) {
	all = Discover(env)
	if configured != "" && !strings.EqualFold(configured, DesktopChoice) {
		all = withChosen(all, configured, env)
	}
	inst, fb, err = Choose(configured, allowDesktop, all)
	return inst, all, fb, err
}

// AnyRunnable is the newest copy that starts, however old: enough for
// `claude auth logout` when an account is removed.
func AnyRunnable(found []Install) (Install, bool) {
	for _, p := range []string{"", ProblemKnownBad, ProblemTooOld} {
		if i, ok := newest(found, func(Install) bool { return true }, p); ok {
			return i, true
		}
	}
	return Install{}, false
}

// NoUsableError: Claude Code was not found, or no copy of it can be used.
type NoUsableError struct {
	Found        []Install
	Pref         string // claude_path
	AllowDesktop bool
}

func (e *NoUsableError) Error() string {
	if len(e.Found) == 0 {
		if DesktopAppInstalled() {
			return "the Claude desktop app has not set up its Claude Code yet: open the app's Code tab once " +
				"(or install the Claude Code CLI: PowerShell: irm https://claude.ai/install.ps1 | iex)"
		}
		return "Claude Code not found. Install the Claude desktop app (https://claude.ai/download), " +
			"which includes it, or the Claude Code CLI (PowerShell: irm https://claude.ai/install.ps1 | iex), " +
			"or set its path with: baton config claude_path <path to claude.exe>"
	}
	if !e.AllowDesktop {
		for _, i := range e.Found {
			if i.Desktop() && (i.Problem == "" || i.Problem == ProblemKnownBad) {
				return fmt.Sprintf("the only Claude Code baton can use is the Claude desktop app's own (%s), and using it is turned off: "+
					"turn it on with: baton config allow_desktop_bundle true, or install the Claude Code CLI "+
					"(PowerShell: irm https://claude.ai/install.ps1 | iex)", i.Path)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no Claude Code on this PC can be used by baton (it needs %s or newer):", MinVersion)
	for _, i := range e.Found {
		v := i.Version
		if v == "" {
			v = "?"
		}
		fmt.Fprintf(&b, "\n  %s (%s, %s): %s", v, i.Source, i.Path, i.Fix())
	}
	// The desktop app is there but its Code tab was never opened: that gives
	// a Claude Code new enough, without touching the CLI.
	if !slices.ContainsFunc(e.Found, Install.Desktop) && DesktopAppInstalled() && (e.AllowDesktop || strings.EqualFold(e.Pref, DesktopChoice)) {
		b.WriteString("\nor open the Claude desktop app's Code tab once: baton then uses the Claude Code that comes with it")
	}
	if e.Pref != "" {
		fmt.Fprintf(&b, "\n(claude_path is %s; to let baton choose, run: baton config claude_path auto)", e.Pref)
	}
	return b.String()
}

// Unwrap is a *TooOldError when a copy was found that is only too old.
func (e *NoUsableError) Unwrap() error {
	if i, ok := newest(e.Found, func(Install) bool { return true }, ProblemTooOld); ok {
		return &TooOldError{Version: i.Version}
	}
	return nil
}

// Fix says, in English, why a copy cannot be used and what to do about it.
func (i Install) Fix() string {
	switch i.Problem {
	case "":
		return "ok"
	case ProblemTooOld:
		if i.Desktop() {
			return fmt.Sprintf("too old (needs %s+): update the Claude desktop app (Help → Check for updates) and open its Code tab once", MinVersion)
		}
		how := i.PackageUpdate()
		if how == "" {
			how = `"` + i.Path + `" update`
		}
		return fmt.Sprintf("too old (needs %s+): run %s", MinVersion, how)
	case ProblemNotRunnable:
		return "does not start: " + i.Detail
	case ProblemInstalling:
		return "the Claude desktop app is still installing it"
	case ProblemWrapper:
		return "a .cmd/.ps1 wrapper; baton needs claude.exe"
	case ProblemKnownBad:
		return "has a known problem with baton (" + i.Detail + "): update it"
	case ProblemMissing:
		return "no longer there"
	}
	return i.Problem
}

// PackageUpdate is the command that updates this copy through the package
// manager that installed it, or "" for a copy that updates itself (claude
// update). `claude update --help`: "package-manager installs update through
// the package manager".
func (i Install) PackageUpdate() string {
	switch i.Source {
	case "winget":
		return "winget upgrade Anthropic.ClaudeCode"
	case "scoop":
		app := "claude-code"
		if m := scoopAppRx.FindStringSubmatch(i.Path); m != nil {
			app = m[1]
		}
		return "scoop update " + app
	case "npm":
		return "npm install -g @anthropic-ai/claude-code"
	}
	return ""
}

// scoopAppRx finds the app's name in <scoop>\apps\<name>\current\claude.exe.
var scoopAppRx = regexp.MustCompile(`(?i)\\apps\\([^\\]+)\\[^\\]+\\[^\\]*$`)

// pathSource says which installer put a claude.exe found on PATH there.
func pathSource(p string) string {
	l := strings.ToLower(p)
	switch {
	case strings.Contains(l, `\scoop\`):
		return "scoop"
	case strings.Contains(l, `\microsoft\winget\`):
		return "winget"
	case strings.Contains(l, `\npm\node_modules\`):
		return "npm"
	}
	return "path"
}

// IsDesktopCopy reports whether a path is inside the Claude desktop app's
// own Claude Code folders (now or before).
func IsDesktopCopy(p string) bool {
	l := strings.ToLower(filepath.Clean(p))
	return strings.Contains(l, `\claude-data\`) && strings.Contains(l, `\claude-code\`) ||
		strings.Contains(l, `\claude\claude-code\`)
}

// ExeEnv adds to env what claude.exe at exe needs: the desktop app's copy is
// updated by the app, which runs it with its own updater turned off, and so
// must baton (it would otherwise update the app's folder behind its back).
// A CLI copy keeps updating itself.
func ExeEnv(exe string, env []string) []string {
	if IsDesktopCopy(exe) {
		return append(env[:len(env):len(env)], "DISABLE_AUTOUPDATER=1")
	}
	return env
}

func sameFile(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
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

// lookPathAll is every claude on PATH, one per folder: claude.exe, else a
// .cmd/.bat/.ps1 wrapper. It skips the WindowsApps app-execution aliases
// (zero-byte reparse points that are not the real CLI).
func lookPathAll() []string {
	self, _ := os.Executable()
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || strings.Contains(strings.ToLower(dir), `\microsoft\windowsapps`) {
			continue
		}
		for _, name := range []string{"claude.exe", "claude.cmd", "claude.bat", "claude.ps1"} {
			p := filepath.Join(dir, name)
			if isFile(p) && !strings.EqualFold(p, self) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// desktopRoots are the folders where the Claude desktop app keeps its Claude
// Code, one folder per version: Claude-Data now, %APPDATA%\Claude\claude-code
// until 2026-10 (inside the Store app's own folder when Windows redirects it).
func desktopRoots() []string {
	var roots []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		m, _ := filepath.Glob(filepath.Join(la, "Claude-Data", "*", "claude-code"))
		roots = append(roots, m...)
		m, _ = filepath.Glob(filepath.Join(la, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude", "claude-code"))
		roots = append(roots, m...)
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		roots = append(roots, filepath.Join(ad, "Claude", "claude-code"))
	}
	return roots
}

type desktopCopy struct {
	path, version string
	finished      bool
}

// desktopCopies are the desktop app's copies (<root>\<version>\<hash>\claude.exe),
// newest first.
func desktopCopies() []desktopCopy {
	var all []desktopCopy
	for _, root := range desktopRoots() {
		m, _ := filepath.Glob(filepath.Join(root, "*", "*", "claude.exe"))
		// The app marks a copy it has checked with .verified: once it does,
		// a copy without the mark that is newer than every marked one is still
		// being written. An older one without it is from before the app
		// marked its copies, and is judged as an app that marks nothing.
		var newestMarked string
		for _, p := range m {
			v := versionRx.FindString(filepath.Base(filepath.Dir(filepath.Dir(p))))
			if v != "" && isFile(filepath.Join(filepath.Dir(p), ".verified")) && (newestMarked == "" || CompareVersions(v, newestMarked) > 0) {
				newestMarked = v
			}
		}
		for _, p := range m {
			v := versionRx.FindString(filepath.Base(filepath.Dir(filepath.Dir(p))))
			if v == "" {
				continue
			}
			pending := newestMarked != "" && CompareVersions(v, newestMarked) > 0
			all = append(all, desktopCopy{path: p, version: v, finished: desktopFinished(p, pending)})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return CompareVersions(all[i].version, all[j].version) > 0 })
	return all
}

// desktopFinished: the app has written all of claude.exe (the size it noted
// in .payload) and checked it (.verified). A copy without the check mark is
// finished when it was written more than 30 s ago, unless pending: newer than
// every copy the app marked, so the app is still at work on it.
func desktopFinished(exe string, pending bool) bool {
	fi, err := os.Stat(exe)
	if err != nil || fi.Size() == 0 {
		return false
	}
	dir := filepath.Dir(exe)
	if b, err := os.ReadFile(filepath.Join(dir, ".payload")); err == nil {
		var p struct {
			Size int64 `json:"size"`
		}
		if json.Unmarshal(fsx.StripBOM(b), &p) == nil && p.Size > 0 && p.Size != fi.Size() {
			return false
		}
	}
	if isFile(filepath.Join(dir, ".verified")) {
		return true
	}
	return !pending && time.Since(fi.ModTime()) > 30*time.Second
}

// DesktopBundle is the Claude desktop app's newest finished Claude Code,
// however old, or "".
func DesktopBundle() string {
	for _, c := range desktopCopies() {
		if c.finished {
			return c.path
		}
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

// Each `claude --version` takes about a second, and the window looks for
// Claude Code every minute: a version is kept for as long as the file is the
// same (path, size and time), in memory and in a file other baton processes
// share. A copy that does not start is asked again after 10 minutes.
var versions = struct {
	sync.Mutex
	file   string
	loaded bool
	ok     map[string]string    // key -> version
	failed map[string]time.Time // key -> when it failed
}{ok: map[string]string{}, failed: map[string]time.Time{}}

// UseVersionCache keeps the versions found in file as well (best effort).
func UseVersionCache(file string) {
	versions.Lock()
	defer versions.Unlock()
	if versions.file != file {
		versions.file, versions.loaded = file, false
	}
}

// ForgetVersion drops what is known about exe's version (it was just
// updated, perhaps in place).
func ForgetVersion(exe string) {
	versions.Lock()
	defer versions.Unlock()
	prefix := strings.ToLower(filepath.Clean(exe)) + "|"
	for k := range versions.ok {
		if strings.HasPrefix(k, prefix) {
			delete(versions.ok, k)
		}
	}
	for k := range versions.failed {
		if strings.HasPrefix(k, prefix) {
			delete(versions.failed, k)
		}
	}
	saveVersionsLocked()
}

func versionKey(exe string) (string, bool) {
	fi, err := os.Stat(exe)
	if err != nil {
		return "", false
	}
	return strings.ToLower(filepath.Clean(exe)) + "|" + strconv.FormatInt(fi.Size(), 10) + "|" +
		strconv.FormatInt(fi.ModTime().UnixNano(), 10), true
}

func cachedVersion(exe string, env []string) (string, error) {
	key, ok := versionKey(exe)
	if !ok {
		return Version(exe, env)
	}
	versions.Lock()
	if !versions.loaded {
		versions.loaded = true
		if versions.file != "" {
			var disk map[string]string
			if fsx.ReadJSON(versions.file, &disk) == nil {
				for k, v := range disk {
					versions.ok[k] = v
				}
			}
		}
	}
	if v, ok := versions.ok[key]; ok {
		versions.Unlock()
		return v, nil
	}
	if at, ok := versions.failed[key]; ok && time.Since(at) < 10*time.Minute {
		versions.Unlock()
		return "", fmt.Errorf("%s --version failed a moment ago", exe)
	}
	versions.Unlock()
	v, err := Version(exe, env)
	versions.Lock()
	defer versions.Unlock()
	if err != nil {
		versions.failed[key] = time.Now()
		return "", err
	}
	versions.ok[key] = v
	saveVersionsLocked()
	return v, nil
}

// saveVersionsLocked writes the versions of files that are still the same.
func saveVersionsLocked() {
	if versions.file == "" {
		return
	}
	keep := map[string]string{}
	for k, v := range versions.ok {
		path, _, _ := strings.Cut(k, "|")
		if cur, ok := versionKey(path); ok && cur == k {
			keep[k] = v
		}
	}
	_ = fsx.WriteJSONAtomic(versions.file, keep)
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

// CheckVersion fails below MinVersion. There is no upper limit: see TestedWith.
func CheckVersion(v string) error {
	if CompareVersions(v, MinVersion) < 0 {
		return &TooOldError{Version: v}
	}
	return nil
}
