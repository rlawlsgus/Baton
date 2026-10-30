package winui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestShellWordKeepsAPathOneWord(t *testing.T) {
	if got := ShellWord(`C:\baton\baton.exe`); got != `C:\baton\baton.exe` {
		t.Fatalf("plain path changed: %q", got)
	}
	dir := filepath.Join(t.TempDir(), "my programs")
	exe := filepath.Join(dir, "baton.exe")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// However the shell splits it, the path comes back whole.
	got := ShellWord(exe)
	words, err := windows.DecomposeCommandLine(got + " https://claude.ai/oauth/authorize")
	if err != nil || len(words) != 2 {
		t.Fatalf("%q split into %q (%v)", got, words, err)
	}
	want, _ := os.Stat(exe)
	if fi, err := os.Stat(words[0]); err != nil || !os.SameFile(fi, want) {
		t.Fatalf("%q does not name %s (%v)", got, exe, err)
	}
	if strings.Contains(got, " ") && !strings.HasPrefix(got, `"`) {
		t.Fatalf("unquoted spaces: %q", got)
	}
}
