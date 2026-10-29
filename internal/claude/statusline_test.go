package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusLineCommand(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "baton.exe")
	_ = os.WriteFile(plain, []byte("MZ"), 0o700)
	if got := StatusLineCommand(plain); got != filepath.ToSlash(plain)+" statusline" || strings.Contains(got, `\`) {
		t.Fatalf("plain path: %q", got)
	}
	// A path with a space: its short form if there is one, else quoted.
	dir := filepath.Join(t.TempDir(), "my apps")
	_ = os.MkdirAll(dir, 0o700)
	spaced := filepath.Join(dir, "baton.exe")
	_ = os.WriteFile(spaced, []byte("MZ"), 0o700)
	got := StatusLineCommand(spaced)
	exe := strings.TrimSuffix(got, " statusline")
	if strings.Contains(got, `\`) || !strings.HasSuffix(got, " statusline") ||
		strings.Contains(exe, " ") && !(strings.HasPrefix(exe, `"`) && strings.HasSuffix(exe, `"`)) {
		t.Fatalf("spaced path: %q", got)
	}
}

func TestUserStatusLine(t *testing.T) {
	profile, cwd := t.TempDir(), t.TempDir()
	if UserStatusLine(profile, cwd) {
		t.Fatal("no settings, but a status line found")
	}
	_ = os.WriteFile(filepath.Join(profile, "settings.json"), []byte(`{"env":{}}`), 0o600)
	if UserStatusLine(profile, cwd) {
		t.Fatal("settings without a status line")
	}
	_ = os.MkdirAll(filepath.Join(cwd, ".claude"), 0o700)
	_ = os.WriteFile(filepath.Join(cwd, ".claude", "settings.local.json"), []byte("\xef\xbb\xbf"+`{"statusLine":{"type":"command","command":"x"}}`), 0o600)
	if !UserStatusLine(profile, cwd) {
		t.Fatal("the project's own status line was missed")
	}
}
