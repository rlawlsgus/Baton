package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The test binary also plays claude.exe for the resolve tests: with
// BATON_FAKE_CLAUDE=1 it answers --version with the version written next to
// it (<exe>.ver; without one it fails, as a copy that does not start), and
// notes every run in BATON_FAKE_LOG so tests can count them.
func TestMain(m *testing.M) {
	if os.Getenv("BATON_FAKE_CLAUDE") == "1" && len(os.Args) == 2 && os.Args[1] == "--version" {
		self, _ := os.Executable()
		if log := os.Getenv("BATON_FAKE_LOG"); log != "" {
			if f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				fmt.Fprintln(f, self)
				f.Close()
			}
		}
		v, err := os.ReadFile(self + ".ver")
		if err != nil {
			os.Exit(3)
		}
		fmt.Printf("%s (Claude Code)\n", strings.TrimSpace(string(v)))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakePC is a PC of its own (home, AppData, PATH) to put copies of Claude
// Code in.
type fakePC struct {
	t    *testing.T
	root string
	exe  []byte
	env  []string
	log  string
}

func newPC(t *testing.T) *fakePC {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("PATH", filepath.Join(root, "empty"))
	t.Setenv("SCOOP", "")
	UseVersionCache("")
	forgetAllVersions()
	t.Cleanup(func() { UseVersionCache(""); forgetAllVersions() })
	log := filepath.Join(root, "runs.log")
	return &fakePC{t: t, root: root, exe: exe, log: log,
		env: append(os.Environ(), "BATON_FAKE_CLAUDE=1", "BATON_FAKE_LOG="+log)}
}

func forgetAllVersions() {
	versions.Lock()
	defer versions.Unlock()
	versions.ok, versions.failed, versions.loaded = map[string]string{}, map[string]time.Time{}, false
}

// place writes a claude.exe, last changed an hour ago (an app update in
// progress is newer than 30 s).
func (pc *fakePC) place(p string) string {
	pc.t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, pc.exe, 0o700); err != nil {
		pc.t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p, old, old)
	return p
}

// cli places a Claude Code CLI that says it is version v.
func (pc *fakePC) cli(p, v string) string {
	pc.place(p)
	if err := os.WriteFile(p+".ver", []byte(v), 0o600); err != nil {
		pc.t.Fatal(err)
	}
	return p
}

func (pc *fakePC) native(v string) string {
	return pc.cli(filepath.Join(pc.root, ".local", "bin", "claude.exe"), v)
}

// desktopRoot is where the desktop app keeps its copies now.
func (pc *fakePC) desktopRoot() string {
	return filepath.Join(pc.root, "local", "Claude-Data", "claude_x", "claude-code")
}

// desktop places one of the desktop app's copies, finished as the app
// marks it (.payload with its size, .verified) when marked.
func (pc *fakePC) desktop(root, v string, marked bool) string {
	p := pc.place(filepath.Join(root, v, "abc123", "claude.exe"))
	if marked {
		dir := filepath.Dir(p)
		_ = os.WriteFile(filepath.Join(dir, ".payload"), []byte(fmt.Sprintf(`{"sha256":"abc123","size":%d}`, len(pc.exe))), 0o600)
		_ = os.WriteFile(filepath.Join(dir, ".verified"), []byte("abc123"), 0o600)
	}
	return p
}

func (pc *fakePC) runs() int {
	b, _ := os.ReadFile(pc.log)
	return strings.Count(string(b), "\n")
}

func (pc *fakePC) resolve(pref string, allowDesktop bool) (Install, []Install, *Fallback, error) {
	pc.t.Helper()
	return ResolveUsable(pref, allowDesktop, pc.env)
}

func find(all []Install, p string) *Install {
	for i := range all {
		if strings.EqualFold(all[i].Path, p) {
			return &all[i]
		}
	}
	return nil
}

// The newest copy that works is used: a CLI installed long ago and never
// updated must not win over the desktop app's newer copy (or the other way
// round).
func TestNewestCopyWins(t *testing.T) {
	pc := newPC(t)
	cli := pc.native("2.1.270")
	bundle := pc.desktop(pc.desktopRoot(), "2.1.317", true)
	inst, all, fb, err := pc.resolve("", true)
	if err != nil || fb != nil || !strings.EqualFold(inst.Path, bundle) || inst.Version != "2.1.317" || !inst.Desktop() {
		t.Fatalf("got %+v %+v %v, want the desktop copy", inst, fb, err)
	}
	if c := find(all, cli); c == nil || c.Version != "2.1.270" || c.Problem != "" || c.Source != "native" {
		t.Fatalf("the CLI is listed too: %+v", all)
	}
	// The desktop app's copy turned off: the CLI.
	if inst, _, _, err := pc.resolve("", false); err != nil || !strings.EqualFold(inst.Path, cli) {
		t.Fatalf("without the desktop copy: %+v %v", inst, err)
	}
	// The CLI updated past the desktop app.
	pc.cli(cli, "2.1.320")
	if inst, _, _, err := pc.resolve("", true); err != nil || !strings.EqualFold(inst.Path, cli) || inst.Version != "2.1.320" {
		t.Fatalf("newer CLI: %+v %v", inst, err)
	}
}

// Two copies of the same version: the CLI, whose path does not change with
// every app update.
func TestTieGoesToTheCLI(t *testing.T) {
	pc := newPC(t)
	pc.desktop(pc.desktopRoot(), "2.1.317", true)
	cli := pc.native("2.1.317")
	if inst, _, _, err := pc.resolve("", true); err != nil || !strings.EqualFold(inst.Path, cli) {
		t.Fatalf("got %+v %v, want the CLI", inst, err)
	}
}

// A CLI too old for baton is passed over for the desktop app's copy, and
// listed with what to do about it.
func TestOldCLIPassedOver(t *testing.T) {
	pc := newPC(t)
	cli := pc.native("2.0.10")
	bundle := pc.desktop(pc.desktopRoot(), "2.1.309", true)
	inst, all, fb, err := pc.resolve("", true)
	if err != nil || fb != nil || !strings.EqualFold(inst.Path, bundle) {
		t.Fatalf("got %+v %v", inst, err)
	}
	if c := find(all, cli); c == nil || c.Problem != ProblemTooOld || !strings.Contains(c.Fix(), `"`+cli+`" update`) {
		t.Fatalf("old CLI: %+v", c)
	}
	// Without the desktop copy nothing works: the message says why, and it
	// still counts as too old.
	_, _, _, err = pc.resolve("", false)
	var old *TooOldError
	var none *NoUsableError
	if !errors.As(err, &none) || !errors.As(err, &old) || old.Version != "2.0.10" ||
		!strings.Contains(err.Error(), "allow_desktop_bundle true") {
		t.Fatalf("got %v", err)
	}
}

// When every copy is too old, the message names each with its own fix, and
// the desktop app's copy is still seen (it is too old, not missing).
func TestOnlyTooOldCopies(t *testing.T) {
	pc := newPC(t)
	cli := pc.native("2.1.200")
	pc.desktop(pc.desktopRoot(), "2.1.201", true)
	_, all, _, err := pc.resolve("", true)
	var old *TooOldError
	if !errors.As(err, &old) || old.Version != "2.1.201" || len(all) != 2 {
		t.Fatalf("got %v %+v", err, all)
	}
	for _, want := range []string{"needs " + MinVersion, `run "` + cli + `" update`, "update the Claude desktop app"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q:\n%v", want, err)
		}
	}
	if DesktopBundle() == "" {
		t.Error("an old desktop copy is still the desktop app's copy")
	}
	if i, ok := AnyRunnable(all); !ok || i.Version != "2.1.201" {
		t.Errorf("AnyRunnable: %+v %v", i, ok)
	}
}

// claude_path "desktop" follows the desktop app's copy; while there is none
// that works, another copy is used, and baton says so.
func TestDesktopChoiceFallsBack(t *testing.T) {
	pc := newPC(t)
	cli := pc.native("2.1.300")
	inst, _, fb, err := pc.resolve(DesktopChoice, true)
	if err != nil || !strings.EqualFold(inst.Path, cli) || fb == nil || fb.Wanted != DesktopChoice || fb.Why != ProblemNotSetUp {
		t.Fatalf("no desktop copy: %+v %+v %v", inst, fb, err)
	}
	old := pc.desktop(pc.desktopRoot(), "2.1.200", true)
	if inst, _, fb, err = pc.resolve(DesktopChoice, true); err != nil || !strings.EqualFold(inst.Path, cli) || fb == nil || fb.Why != ProblemTooOld {
		t.Fatalf("old desktop copy: %+v %+v %v", inst, fb, err)
	}
	_ = os.RemoveAll(filepath.Dir(filepath.Dir(old)))
	bundle := pc.desktop(pc.desktopRoot(), "2.1.299", true) // older than the CLI, still the one chosen
	if inst, _, fb, err = pc.resolve(DesktopChoice, false); err != nil || !strings.EqualFold(inst.Path, bundle) || fb != nil {
		t.Fatalf("desktop copy: %+v %+v %v", inst, fb, err)
	}
}

// A chosen claude.exe that is gone or too old gives way to the best other
// copy until it is back. One of the desktop app's versions gives way to the
// app's newest copy, and once the app removed it (it does when it updates),
// the app's copies are simply followed: that version never comes back.
func TestChosenPathFallsBack(t *testing.T) {
	pc := newPC(t)
	pinned := pc.cli(filepath.Join(pc.root, "tools", "claude.exe"), "2.1.300")
	bundle := pc.desktop(pc.desktopRoot(), "2.1.317", true)
	inst, all, fb, err := pc.resolve(pinned, true)
	if err != nil || fb != nil || !strings.EqualFold(inst.Path, pinned) || inst.Source != "config" {
		t.Fatalf("chosen copy, though older: %+v %+v %v", inst, fb, err)
	}
	if find(all, pinned) == nil {
		t.Fatalf("the chosen copy is listed: %+v", all)
	}
	pc.cli(pinned, "2.1.100")
	if inst, _, fb, err = pc.resolve(pinned, true); err != nil || !strings.EqualFold(inst.Path, bundle) || fb == nil || fb.Why != ProblemTooOld {
		t.Fatalf("too old: %+v %+v %v", inst, fb, err)
	}
	_ = os.Remove(pinned)
	inst, all, fb, err = pc.resolve(pinned, true)
	if err != nil || !strings.EqualFold(inst.Path, bundle) || fb == nil || fb.Wanted != pinned || fb.Why != ProblemMissing {
		t.Fatalf("gone: %+v %+v %v", inst, fb, err)
	}
	if c := find(all, pinned); c == nil || c.Problem != ProblemMissing {
		t.Fatalf("a gone choice is listed as missing: %+v", all)
	}
	// Back again: used again.
	pc.cli(pinned, "2.1.300")
	if inst, _, fb, _ = pc.resolve(pinned, true); !strings.EqualFold(inst.Path, pinned) || fb != nil {
		t.Fatalf("back: %+v %+v", inst, fb)
	}

	// A desktop copy chosen, then removed by an app update.
	older := pc.desktop(pc.desktopRoot(), "2.1.309", true)
	pc.native("2.1.320")
	if inst, _, fb, err = pc.resolve(older, true); err != nil || !strings.EqualFold(inst.Path, older) || fb != nil {
		t.Fatalf("chosen desktop copy: %+v %+v %v", inst, fb, err)
	}
	// Still there but not usable (being written again): the app's newest
	// copy, and baton says so.
	_ = os.WriteFile(filepath.Join(filepath.Dir(older), ".payload"), []byte(`{"size":1}`), 0o600)
	if inst, _, fb, err = pc.resolve(older, true); err != nil || !strings.EqualFold(inst.Path, bundle) || fb == nil ||
		fb.Wanted != older || fb.Why != ProblemInstalling {
		t.Fatalf("chosen desktop copy not usable: %+v %+v %v", inst, fb, err)
	}
	_ = os.RemoveAll(filepath.Dir(filepath.Dir(older)))
	inst, all, fb, err = pc.resolve(older, true)
	if err != nil || !strings.EqualFold(inst.Path, bundle) || fb != nil {
		t.Fatalf("removed desktop copy: %+v %+v %v", inst, fb, err)
	}
	if c := find(all, older); c != nil {
		t.Fatalf("the removed copy is listed: %+v", c)
	}
	if _, ok := Chosen(older, all); ok {
		t.Fatal("Chosen names a removed copy")
	}
	// Followed as DesktopChoice is: while the app has no copy that works,
	// the CLI, for the app's reason.
	_ = os.WriteFile(filepath.Join(filepath.Dir(bundle), ".payload"), []byte(`{"size":1}`), 0o600)
	inst, _, fb, err = pc.resolve(older, true)
	if err != nil || inst.Source != "native" || fb == nil || fb.Wanted != DesktopChoice || fb.Why != ProblemInstalling {
		t.Fatalf("removed desktop copy, the app's copy not usable: %+v %+v %v", inst, fb, err)
	}
}

// claude_path set to a Scoop shim (what `where claude` prints) names the
// claude.exe the shim leads to: used with nothing to say, listed once, and
// never the shim itself (a launcher in front of claude).
func TestChosenScoopShim(t *testing.T) {
	pc := newPC(t)
	real := pc.cli(filepath.Join(pc.root, "scoop", "apps", "claude-code", "current", "claude.exe"), "2.1.320")
	shims := filepath.Join(pc.root, "scoop", "shims")
	shim := pc.place(filepath.Join(shims, "claude.exe"))
	_ = os.WriteFile(filepath.Join(shims, "claude.shim"), []byte("path = \""+real+"\"\n"), 0o600)
	pc.desktop(pc.desktopRoot(), "2.1.317", true)
	for _, onPath := range []bool{true, false} {
		if onPath {
			t.Setenv("PATH", shims)
		} else {
			// Found only through claude_path.
			t.Setenv("PATH", filepath.Join(pc.root, "empty"))
			t.Setenv("SCOOP", filepath.Join(pc.root, "no-scoop"))
		}
		inst, all, fb, err := pc.resolve(shim, true)
		if err != nil || fb != nil || !strings.EqualFold(inst.Path, real) || len(all) != 2 {
			t.Fatalf("on PATH %v: %+v %+v %v %+v", onPath, inst, fb, err, all)
		}
		if c, ok := Chosen(shim, all); !ok || !strings.EqualFold(c.Path, real) {
			t.Fatalf("on PATH %v: Chosen %+v %v", onPath, c, ok)
		}
		if find(all, shim) != nil {
			t.Fatalf("on PATH %v: the shim is listed: %+v", onPath, all)
		}
	}
}

// An old CLI and the desktop app whose Code tab was never opened: the
// message also says that opening it is enough (not when using the desktop
// app's copy is turned off).
func TestOldCLIAndAppWithoutItsCopy(t *testing.T) {
	pc := newPC(t)
	cli := pc.native("2.1.200")
	_ = os.MkdirAll(filepath.Join(pc.root, "local", "Claude-Data", "claude_x"), 0o700)
	const hint = "\nor open the Claude desktop app's Code tab once: baton then uses the Claude Code that comes with it"
	_, _, _, err := pc.resolve("", true)
	if err == nil || !strings.Contains(err.Error(), `run "`+cli+`" update`) || !strings.Contains(err.Error(), hint) {
		t.Fatalf("got %v", err)
	}
	if _, _, _, err = pc.resolve(DesktopChoice, false); err == nil || !strings.Contains(err.Error(), hint) {
		t.Fatalf("desktop chosen: %v", err)
	}
	if _, _, _, err = pc.resolve("", false); err == nil || strings.Contains(err.Error(), "Code tab") {
		t.Fatalf("desktop copy turned off: %v", err)
	}
	// The app has a copy, too old: its own fix instead.
	pc.desktop(pc.desktopRoot(), "2.1.201", true)
	if _, _, _, err = pc.resolve("", true); err == nil || strings.Contains(err.Error(), hint) || !strings.Contains(err.Error(), "update the Claude desktop app") {
		t.Fatalf("old desktop copy: %v", err)
	}
}

// A CLI installed with a package manager is updated with it: that is what
// the message says to run. WinGet's Links folder on PATH counts as WinGet.
func TestUpdateThroughThePackageManager(t *testing.T) {
	pc := newPC(t)
	links := filepath.Join(pc.root, "local", "Microsoft", "WinGet", "Links")
	pc.cli(filepath.Join(links, "claude.exe"), "2.1.200")
	t.Setenv("PATH", links)
	all := Discover(pc.env)
	if len(all) != 1 || all[0].Source != "winget" || all[0].Fix() != "too old (needs "+MinVersion+"+): run winget upgrade Anthropic.ClaudeCode" {
		t.Fatalf("winget: %+v", all)
	}
	for _, c := range []struct{ path, source, update string }{
		{filepath.Join(pc.root, "scoop", "apps", "claude-code-x", "current", "claude.exe"), "scoop", "scoop update claude-code-x"},
		{filepath.Join(pc.root, "roaming", "npm", "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe"), "npm", "npm install -g @anthropic-ai/claude-code"},
		{filepath.Join(pc.root, "tools", "claude.exe"), "path", ""},
	} {
		i := Install{Path: c.path, Source: pathSource(c.path)}
		if i.Source != c.source || i.PackageUpdate() != c.update {
			t.Errorf("%s: %s %q", c.path, i.Source, i.PackageUpdate())
		}
	}
	if fix := (Install{Path: `C:\x\claude.exe`, Source: "native", Problem: ProblemTooOld}).Fix(); !strings.HasSuffix(fix, `run "C:\x\claude.exe" update`) {
		t.Errorf("native: %s", fix)
	}
}

// A copy the desktop app is still writing is not used: once the app marks
// finished copies (.verified), one without the mark that is newer than those
// is not finished; an app that marks nothing wrote its copy more than 30 s ago.
func TestDesktopCopyFinished(t *testing.T) {
	pc := newPC(t)
	done := pc.desktop(pc.desktopRoot(), "2.1.309", true)
	writing := pc.place(filepath.Join(pc.desktopRoot(), "2.1.317", "def456", "claude.exe"))
	inst, all, _, err := pc.resolve("", true)
	if err != nil || !strings.EqualFold(inst.Path, done) {
		t.Fatalf("got %+v %v", inst, err)
	}
	if c := find(all, writing); c == nil || c.Problem != ProblemInstalling {
		t.Fatalf("unfinished copy: %+v", c)
	}
	if DesktopBundle() != done {
		t.Fatalf("DesktopBundle = %s", DesktopBundle())
	}
	// Marked, but not all of it written yet.
	_ = os.WriteFile(filepath.Join(filepath.Dir(writing), ".verified"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(filepath.Dir(writing), ".payload"), []byte(`{"size":1}`), 0o600)
	if inst, _, _, _ := pc.resolve("", true); !strings.EqualFold(inst.Path, done) {
		t.Fatalf("size not reached yet: %+v", inst)
	}
	// Without the mark but older than the marked ones: from before the app
	// marked its copies, so finished (written long ago), and usable when chosen.
	before := pc.place(filepath.Join(pc.desktopRoot(), "2.1.300", "aaa111", "claude.exe"))
	inst, all, fb, err := pc.resolve(before, true)
	if err != nil || fb != nil || !strings.EqualFold(inst.Path, before) || find(all, before).Problem != "" {
		t.Fatalf("an old copy without the mark: %+v %+v %v", inst, fb, err)
	}

	// The older place, without any marks: by time.
	legacy := filepath.Join(pc.root, "roaming", "Claude", "claude-code")
	pc.place(filepath.Join(legacy, "2.1.330", "abc", "claude.exe"))
	inst, _, _, _ = pc.resolve("", true)
	if inst.Version != "2.1.330" || !inst.Desktop() {
		t.Fatalf("legacy copy: %+v", inst)
	}
	fresh := pc.place(filepath.Join(legacy, "2.1.331", "abc", "claude.exe"))
	now := time.Now()
	_ = os.Chtimes(fresh, now, now)
	if inst, _, _, _ = pc.resolve("", true); inst.Version != "2.1.330" {
		t.Fatalf("a copy written just now: %+v", inst)
	}
}

// The desktop app kept its Claude Code in %APPDATA%\Claude\claude-code
// before, which the Store app keeps inside its own folder.
func TestLegacyDesktopLocations(t *testing.T) {
	pc := newPC(t)
	a := pc.desktop(filepath.Join(pc.root, "roaming", "Claude", "claude-code"), "2.1.295", true)
	b := pc.desktop(filepath.Join(pc.root, "local", "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude", "claude-code"), "2.1.299", true)
	inst, all, _, err := pc.resolve("", true)
	if err != nil || !strings.EqualFold(inst.Path, b) || find(all, a) == nil || !find(all, a).Desktop() {
		t.Fatalf("got %+v %v %+v", inst, err, all)
	}
	if !IsDesktopCopy(a) || !IsDesktopCopy(b) || !IsDesktopCopy(pc.desktop(pc.desktopRoot(), "2.1.300", false)) || IsDesktopCopy(pc.native("2.1.300")) {
		t.Fatal("IsDesktopCopy")
	}
}

// A version known to break baton is passed over while another copy works,
// and used when it is the only one.
func TestKnownBadVersion(t *testing.T) {
	pc := newPC(t)
	knownBad["2.1.317"] = "test"
	t.Cleanup(func() { delete(knownBad, "2.1.317") })
	bad := pc.desktop(pc.desktopRoot(), "2.1.317", true)
	cli := pc.native("2.1.300")
	inst, all, _, err := pc.resolve("", true)
	if err != nil || !strings.EqualFold(inst.Path, cli) || find(all, bad).Problem != ProblemKnownBad {
		t.Fatalf("got %+v %v", inst, err)
	}
	_ = os.Remove(cli)
	if inst, _, _, err = pc.resolve("", true); err != nil || !strings.EqualFold(inst.Path, bad) || inst.Problem != ProblemKnownBad {
		t.Fatalf("only the bad one: %+v %v", inst, err)
	}
}

// claude --version runs once per file: again only when the file changes or
// its version is forgotten (after an update), and not at all in another
// process that shares the cache file.
func TestVersionCache(t *testing.T) {
	pc := newPC(t)
	UseVersionCache(filepath.Join(pc.root, "claude-versions.json"))
	cli := pc.native("2.1.300")
	for i := 0; i < 3; i++ {
		if _, _, _, err := pc.resolve("", true); err != nil {
			t.Fatal(err)
		}
	}
	if n := pc.runs(); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}
	forgetAllVersions() // another process: only the file
	if inst, _, _, _ := pc.resolve("", true); inst.Version != "2.1.300" || pc.runs() != 1 {
		t.Fatalf("from the file: %+v, %d runs", inst, pc.runs())
	}
	pc.cli(cli, "2.1.301")
	later := time.Now().Add(-time.Minute)
	_ = os.Chtimes(cli, later, later) // a new file
	if inst, _, _, _ := pc.resolve("", true); inst.Version != "2.1.301" || pc.runs() != 2 {
		t.Fatalf("changed file: %+v, %d runs", inst, pc.runs())
	}
	_ = os.WriteFile(cli+".ver", []byte("2.1.302"), 0o600) // updated in place, same file
	ForgetVersion(cli)
	if inst, _, _, _ := pc.resolve("", true); inst.Version != "2.1.302" || pc.runs() != 3 {
		t.Fatalf("forgotten: %+v, %d runs", inst, pc.runs())
	}
	// A copy that does not start is not asked again at once.
	_ = os.Remove(cli + ".ver")
	ForgetVersion(cli)
	pc.resolve("", true)
	_, all, _, _ := pc.resolve("", true)
	if c := find(all, cli); c == nil || c.Problem != ProblemNotRunnable || pc.runs() != 4 {
		t.Fatalf("not runnable: %+v, %d runs", c, pc.runs())
	}
}

// A .cmd/.ps1 claude on PATH (npm's) is listed, never run or used; a Scoop
// shim leads to the real claude.exe.
func TestWrappersAndShims(t *testing.T) {
	pc := newPC(t)
	npm := filepath.Join(pc.root, "roaming", "npm")
	_ = os.MkdirAll(npm, 0o700)
	_ = os.WriteFile(filepath.Join(npm, "claude.cmd"), []byte("@echo off"), 0o600)
	_ = os.WriteFile(filepath.Join(npm, "claude.ps1"), []byte(""), 0o600)
	real := pc.cli(filepath.Join(pc.root, "scoop", "apps", "claude-code", "current", "claude.exe"), "2.1.300")
	shims := filepath.Join(pc.root, "scoop", "shims")
	pc.place(filepath.Join(shims, "claude.exe"))
	_ = os.WriteFile(filepath.Join(shims, "claude.shim"), []byte("path = \""+real+"\"\n"), 0o600)
	t.Setenv("PATH", npm+string(os.PathListSeparator)+shims)
	inst, all, _, err := pc.resolve("", true)
	if err != nil || !strings.EqualFold(inst.Path, real) || inst.Source != "scoop" {
		t.Fatalf("got %+v %v", inst, err)
	}
	w := find(all, filepath.Join(npm, "claude.cmd"))
	if w == nil || w.Problem != ProblemWrapper || len(all) != 2 {
		t.Fatalf("wrapper: %+v", all)
	}
	if b, _ := os.ReadFile(pc.log); strings.Contains(strings.ToLower(string(b)), "npm") || pc.runs() != 1 {
		t.Fatalf("runs: %s", b)
	}
	// A wrapper chosen by hand gives way too.
	inst, _, fb, err := pc.resolve(w.Path, true)
	if err != nil || !strings.EqualFold(inst.Path, real) || fb == nil || fb.Why != ProblemWrapper {
		t.Fatalf("chosen wrapper: %+v %+v %v", inst, fb, err)
	}
	// Only the wrapper: nothing works.
	t.Setenv("PATH", npm)
	t.Setenv("SCOOP", filepath.Join(pc.root, "no-scoop"))
	if _, _, _, err := pc.resolve("", true); err == nil || !strings.Contains(err.Error(), "wrapper") {
		t.Fatalf("only a wrapper: %v", err)
	}
	// npm's own claude.exe is there: its claude.cmd is not a copy of its own.
	exe := pc.cli(filepath.Join(npm, "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe"), "2.1.310")
	if _, all, _, _ := pc.resolve("", true); len(all) != 1 || !strings.EqualFold(all[0].Path, exe) || all[0].Source != "npm" {
		t.Fatalf("npm: %+v", all)
	}
}

// One claude.exe reached by two names (WinGet's link to its package) is
// listed once.
func TestSameFileOnce(t *testing.T) {
	pc := newPC(t)
	pkg := pc.cli(filepath.Join(pc.root, "local", "Microsoft", "WinGet", "Packages", "Anthropic.ClaudeCode_x", "claude.exe"), "2.1.300")
	link := filepath.Join(pc.root, "local", "Microsoft", "WinGet", "Links", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(link), 0o700)
	if err := os.Link(pkg, link); err != nil {
		t.Skip("cannot make a hard link here:", err)
	}
	_ = os.WriteFile(link+".ver", []byte("2.1.300"), 0o600)
	all := Discover(pc.env)
	if len(all) != 1 || !strings.EqualFold(all[0].Path, link) || all[0].Source != "winget" || all[0].Version != "2.1.300" {
		t.Fatalf("got %+v", all)
	}
}

// Nothing at all: the message says how to get Claude Code.
func TestNothingFound(t *testing.T) {
	pc := newPC(t)
	_, all, _, err := pc.resolve("", true)
	if len(all) != 0 || err == nil || !strings.Contains(err.Error(), "claude.ai/download") {
		t.Fatalf("got %+v %v", all, err)
	}
	_ = os.MkdirAll(filepath.Join(pc.root, "local", "Claude-Data"), 0o700) // the app, before its Code tab was opened
	if _, _, _, err = pc.resolve("", true); err == nil || !strings.Contains(err.Error(), "Code tab") {
		t.Fatalf("app without its copy: %v", err)
	}
}

// The desktop app's copy runs with its updater off, as the app runs it; a
// CLI copy updates itself as usual.
func TestExeEnv(t *testing.T) {
	base := []string{"A=1"}
	desk := ExeEnv(`C:\Users\x\AppData\Local\Claude-Data\claude_x\claude-code\2.1.317\abc\claude.exe`, base)
	if !slices.Contains(desk, "DISABLE_AUTOUPDATER=1") || len(base) != 1 {
		t.Fatalf("desktop: %q", desk)
	}
	if cli := ExeEnv(`C:\Users\x\.local\bin\claude.exe`, base); slices.Contains(cli, "DISABLE_AUTOUPDATER=1") {
		t.Fatalf("CLI: %q", cli)
	}
}
