package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"baton/internal/winproc"
)

// appUpdatePC is a PC of its own whose desktop app has just updated to
// 2.1.317: versions is the app's Claude Code folder and newer its new copy.
func appUpdatePC(t *testing.T) (versions, newer string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("PATH", filepath.Join(root, "empty"))
	t.Setenv("SCOOP", "")
	versions = filepath.Join(root, "local", "Claude-Data", "claude_x", "claude-code")
	newer = filepath.Join(versions, "2.1.317", "abc", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(newer), 0o700)
	if err := os.WriteFile(newer, []byte("MZ"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(newer, old, old)
	return versions, newer
}

// The desktop app removes the old version's folder when it updates: the next
// launch uses the app's new copy instead of failing to start.
func TestFindClaudeAgainAfterAnAppUpdate(t *testing.T) {
	s := newTest(t)
	var out strings.Builder
	s.opt.Out = &out
	versions, newer := appUpdatePC(t)
	s.opt.ClaudeExe = filepath.Join(versions, "2.1.309", "abc", "claude.exe") // removed by the update
	if !s.findClaudeAgain() || !strings.EqualFold(s.opt.ClaudeExe, newer) || !strings.Contains(out.String(), "Claude Code moved") {
		t.Fatalf("got %s, %q", s.opt.ClaudeExe, out.String())
	}
	if s.findClaudeAgain() {
		t.Fatal("looked again while the copy in use is there")
	}
	// Nothing else to use: it stays as it is, and the start says why.
	_ = os.RemoveAll(versions)
	if s.findClaudeAgain() || !strings.EqualFold(s.opt.ClaudeExe, newer) {
		t.Fatalf("with nothing found: %s", s.opt.ClaudeExe)
	}
}

// The app removes the copy in use between launch's check and the start: the
// start is tried once more, with the app's new copy, run with the app's
// updater off as the app runs it. A start that fails with the copy still
// there is not tried again.
func TestStartClaudeOnceMoreWhenItsCopyWentAway(t *testing.T) {
	s := newTest(t)
	var out strings.Builder
	s.opt.Out = &out
	versions, newer := appUpdatePC(t)
	gone := filepath.Join(versions, "2.1.309", "abc", "claude.exe")
	_ = os.MkdirAll(filepath.Dir(gone), 0o700)
	if err := os.WriteFile(gone, []byte("MZ"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.opt.ClaudeExe = gone
	type call struct {
		exe string
		env []string
	}
	var calls []call
	removeFirst := true
	orig := startProcess
	t.Cleanup(func() { startProcess = orig })
	startProcess = func(exe string, args, env []string, dir string) (*winproc.Child, error) {
		calls = append(calls, call{exe, env})
		if len(calls) == 1 {
			if removeFirst {
				_ = os.RemoveAll(filepath.Dir(filepath.Dir(gone))) // the app updated just now
			}
			return nil, errors.New("The system cannot find the file specified.")
		}
		return &winproc.Child{}, nil
	}
	child, err := s.startClaude([]string{"--settings", "x"}, []string{"A=1"})
	if err != nil || child == nil || len(calls) != 2 || calls[0].exe != gone || !strings.EqualFold(calls[1].exe, newer) ||
		!slices.Contains(calls[1].env, "DISABLE_AUTOUPDATER=1") || !strings.Contains(out.String(), "Claude Code moved") {
		t.Fatalf("got %v %v %+v %q", child, err, calls, out.String())
	}
	calls, removeFirst = nil, false
	if _, err := s.startClaude([]string{"--settings", "x"}, []string{"A=1"}); err == nil || len(calls) != 1 {
		t.Fatalf("a start that fails with the copy there: %v %+v", err, calls)
	}
}
