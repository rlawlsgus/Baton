package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary also plays claude.exe for the resolve tests: with
// BATON_FAKE_CLAUDE=1 it answers --version, as an old CLI when it runs from
// a .local folder and as a current version elsewhere.
func TestMain(m *testing.M) {
	if os.Getenv("BATON_FAKE_CLAUDE") == "1" && len(os.Args) == 2 && os.Args[1] == "--version" {
		if strings.Contains(strings.ToLower(os.Args[0]), ".local") {
			fmt.Println("2.0.10 (Claude Code)")
		} else {
			fmt.Println(TestedMax + " (Claude Code)")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A Claude Code CLI installed long ago and never updated must not stop a
// desktop app user: the desktop app's own copy is used instead.
func TestResolveUsablePassesOverAnOldCLI(t *testing.T) {
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
	place := func(p string) string {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, exe, 0o700); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour) // an app update in progress is newer than 30 s
		_ = os.Chtimes(p, old, old)
		return p
	}
	cli := place(filepath.Join(root, ".local", "bin", "claude.exe"))
	bundle := place(filepath.Join(root, "local", "Claude-Data", "x", "claude-code", TestedMax, "abc", "claude.exe"))
	env := append(os.Environ(), "BATON_FAKE_CLAUDE=1")

	inst, skipped, err := ResolveUsable("", true, env)
	if err != nil || !strings.EqualFold(inst.Path, bundle) || inst.Source != "desktop" || inst.Version != TestedMax {
		t.Fatalf("got %+v %v, want the desktop copy", inst, err)
	}
	if len(skipped) != 1 || !strings.EqualFold(skipped[0].Path, cli) || skipped[0].Version != "2.0.10" {
		t.Fatalf("skipped %+v", skipped)
	}
	// Without the desktop copy, the old CLI is reported as too old.
	inst, _, err = ResolveUsable("", false, env)
	var old *TooOldError
	if !errors.As(err, &old) || old.Version != "2.0.10" || !strings.EqualFold(inst.Path, cli) {
		t.Fatalf("got %+v %v, want the old CLI as too old", inst, err)
	}
	// A configured path is the only one tried.
	if _, _, err := ResolveUsable(cli, true, env); !errors.As(err, &old) {
		t.Fatalf("configured old claude: %v", err)
	}
}

// One claude.exe reached by two names (WinGet's link to its package) is one
// candidate, run once.
func TestCandidatesSameFileOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("PATH", filepath.Join(root, "empty"))
	pkg := filepath.Join(root, "local", "Microsoft", "WinGet", "Packages", "Anthropic.ClaudeCode_x", "claude.exe")
	link := filepath.Join(root, "local", "Microsoft", "WinGet", "Links", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(pkg), 0o700)
	_ = os.MkdirAll(filepath.Dir(link), 0o700)
	_ = os.WriteFile(pkg, []byte("MZ"), 0o700)
	if err := os.Link(pkg, link); err != nil {
		t.Skip("cannot make a hard link here:", err)
	}
	c, err := Candidates("", true)
	if err != nil || len(c) != 1 || !strings.EqualFold(c[0].Path, link) {
		t.Fatalf("got %+v %v", c, err)
	}
}
