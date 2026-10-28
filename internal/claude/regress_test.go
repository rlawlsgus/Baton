package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Regression tests for the first review round.

func TestTrustStopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "src", "repo")
	sub := filepath.Join(repo, "pkg")
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o700)
	_ = os.MkdirAll(sub, 0o700)
	other := filepath.Join(root, "src", "other")
	_ = os.MkdirAll(other, 0o700)
	profile := t.TempDir()
	writeProjects(t, profile, filepath.Join(root, "src"))
	if IsTrusted(profile, sub) || IsTrusted(profile, repo) {
		t.Fatal("trust above the git root must not count inside the repo")
	}
	if !IsTrusted(profile, other) {
		t.Fatal("outside a repo, parent trust is inherited")
	}
	if res, err := SeedProfile(profile, sub, false, true); err != nil || !res.Trust || !IsTrusted(profile, sub) {
		t.Fatalf("seed: %+v %v", res, err)
	}
	writeProjects(t, profile, repo)
	if !IsTrusted(profile, filepath.Join(repo, "x")) {
		t.Fatal("trust at the git root covers the repo")
	}
}

func TestTrustKeysWorktreeIncludesMainRoot(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	wt := filepath.Join(root, "wt")
	_ = os.MkdirAll(filepath.Join(main, ".git", "worktrees", "wt"), 0o700)
	_ = os.MkdirAll(wt, 0o700)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(main, ".git", "worktrees", "wt")+"\n"), 0o600)
	if keys := TrustKeys(wt); !reflect.DeepEqual(keys, []string{TrustKey(wt), TrustKey(main)}) {
		t.Fatalf("keys = %q", keys)
	}
}

func writeProjects(t *testing.T, profile string, trusted ...string) {
	t.Helper()
	projects := map[string]any{}
	for _, p := range trusted {
		projects[TrustKey(p)] = map[string]any{"hasTrustDialogAccepted": true}
	}
	b, _ := json.Marshal(map[string]any{"projects": projects})
	if err := os.WriteFile(filepath.Join(profile, ".claude.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSplitArgsHiddenAndUnknownOptions(t *testing.T) {
	a, err := SplitArgs([]string{"--fast-mode", "on", "--channels", "a", "b", "--rc", "name", "--verbose", "go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Persistent, []string{"--fast-mode", "on", "--channels", "a", "b", "--rc", "name", "--verbose"}) ||
		!reflect.DeepEqual(a.FirstOnly, []string{"go"}) {
		t.Fatalf("got %+v", a)
	}
	if _, err := SplitArgs([]string{"--brand-new-option", "value"}); err == nil {
		t.Error("an unknown option followed by a value must be refused (its value would become the prompt)")
	}
	if a, err := SplitArgs([]string{"--brand-new-flag"}); err != nil || !reflect.DeepEqual(a.Persistent, []string{"--brand-new-flag"}) {
		t.Errorf("a lone unknown flag should pass through: %+v %v", a, err)
	}
	if a, err := SplitArgs([]string{"--brand-new-option=v", "prompt"}); err != nil || a.Persistent[0] != "--brand-new-option=v" {
		t.Errorf("inline value form must work: %+v %v", a, err)
	}
	for _, bad := range [][]string{{"--remote", "x"}, {"--environment", "e"}, {"--pool"}, {"--help"}} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) should be refused", bad)
		}
	}
	a, _ = SplitArgs([]string{"--file", "f1", "f2", "--model", "m", "--", "-x", "--model"})
	if !reflect.DeepEqual(a.FirstOnly, []string{"--file", "f1", "f2", "--", "-x", "--model"}) ||
		!reflect.DeepEqual(a.Persistent, []string{"--model", "m"}) {
		t.Errorf("--file / -- handling: %+v", a)
	}
}

func TestReplaceResume(t *testing.T) {
	for _, in := range [][]string{{"-r", "abc", "hi"}, {"--resume=abc", "hi"}, {"-c", "hi"}} {
		a, err := SplitArgs(in)
		if err != nil {
			t.Fatal(err)
		}
		a.ReplaceResume(`C:\p\abc.jsonl`)
		if !reflect.DeepEqual(a.FirstOnly, []string{"--resume", `C:\p\abc.jsonl`, "hi"}) || a.Continue {
			t.Errorf("%q -> %q", in, a.FirstOnly)
		}
	}
}

func TestResolveRejectsShimsAndFollowsScoop(t *testing.T) {
	dir := t.TempDir()
	cmdShim := filepath.Join(dir, "claude.cmd")
	_ = os.WriteFile(cmdShim, []byte("@echo off"), 0o600)
	if _, err := Resolve(cmdShim, false); err == nil {
		t.Fatal("a .cmd claude_path must be refused")
	}
	real := filepath.Join(dir, "app", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(real), 0o700)
	_ = os.WriteFile(real, []byte("MZ"), 0o700)
	shim := filepath.Join(dir, "shims", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(shim), 0o700)
	_ = os.WriteFile(shim, []byte("MZ"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "shims", "claude.shim"), []byte("path = \""+real+"\"\n"), 0o600)
	inst, err := Resolve(shim, false)
	if err != nil || inst.Path != real {
		t.Fatalf("got %+v %v, want %s", inst, err, real)
	}
}

func TestLatestTranscriptFor(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	old := filepath.Join(a, "projects", "x", "old.jsonl")
	newer := filepath.Join(b, "projects", "y", "new.jsonl")
	elsewhere := filepath.Join(b, "projects", "z", "zz.jsonl")
	for _, f := range []struct{ p, cwd string }{{old, `C:\proj`}, {newer, `C:\proj`}, {elsewhere, `C:\other`}} {
		_ = os.MkdirAll(filepath.Dir(f.p), 0o700)
		row, _ := json.Marshal(map[string]string{"cwd": f.cwd})
		_ = os.WriteFile(f.p, append(row, '\n'), 0o600)
		time.Sleep(20 * time.Millisecond)
	}
	if got := LatestTranscriptFor([]string{a, b}, `c:\proj`); got != newer {
		t.Fatalf("got %q want %q", got, newer)
	}
}

func TestRound3Args(t *testing.T) {
	if a, err := SplitArgs([]string{"-d2e"}); err != nil || !reflect.DeepEqual(a.Persistent, []string{"-d2e"}) {
		t.Errorf("-d2e must pass through whole: %+v %v", a, err)
	}
	if _, err := SplitArgs([]string{"--verbose=true"}); err == nil {
		t.Error("a boolean with =value must be refused, as claude refuses it")
	}
	a, err := SplitArgs([]string{"-rabc", "hi"})
	if err != nil || a.ResumeArg != "abc" || !reflect.DeepEqual(a.Original, []string{"-r", "abc", "hi"}) {
		t.Errorf("-rabc: %+v %v", a, err)
	}
	if _, err := SplitArgs([]string{"-pc"}); err == nil {
		t.Error("-pc (print mode) must be refused")
	}
}

func TestPermissionModeAndBypassHelpers(t *testing.T) {
	d := t.TempDir()
	tr := filepath.Join(d, "t.jsonl")
	_ = os.WriteFile(tr, []byte(`{"type":"user"}`+"\n"+
		`{"type":"permission-mode","permissionMode":"plan"}`+"\n"+
		`{"type":"permission-mode","permissionMode":"acceptEdits"}`+"\n"), 0o600)
	if m := LastPermissionMode(tr); m != "acceptEdits" {
		t.Errorf("LastPermissionMode = %q", m)
	}
	p1, p2 := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(p2, "settings.json"), []byte("\xef\xbb\xbf"+`{"skipDangerousModePermissionPrompt": true}`), 0o600)
	if BypassAccepted(p1) || !BypassAccepted(p1, p2) {
		t.Error("BypassAccepted wrong (must also read a BOM-prefixed file)")
	}
	s := RunSettings("b.exe", `C:\r`, map[string]any{"skipDangerousModePermissionPrompt": true})
	if s["skipDangerousModePermissionPrompt"] != true {
		t.Error("extra settings not merged")
	}
}

func TestHooksBlocked(t *testing.T) {
	profile, cwd := t.TempDir(), t.TempDir()
	if why := HooksBlocked(profile, cwd); why != "" {
		t.Fatalf("nothing set, got %q", why)
	}
	_ = os.MkdirAll(filepath.Join(cwd, ".claude"), 0o700)
	_ = os.WriteFile(filepath.Join(cwd, ".claude", "settings.local.json"), []byte(`{"disableAllHooks": true}`), 0o600)
	if why := HooksBlocked(profile, cwd); why == "" {
		t.Fatal("disableAllHooks in project settings not detected")
	}
}
