package supervisor

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/claude"
	"baton/internal/winproc"
)

// busy makes a profile look like a claude runs on it: a session file that
// holds this test's own pid.
func busy(t *testing.T, profileDir string) {
	t.Helper()
	pid := os.Getpid()
	dir := filepath.Join(profileDir, "sessions")
	_ = os.MkdirAll(dir, 0o700)
	data := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"x","cwd":"C:\\elsewhere","startedAt":` +
		strconv.FormatInt(winproc.ProcessCreated(pid).UnixMilli(), 10) + `}`
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if !claude.BusyProfile(profileDir) {
		t.Skip("cannot fake a running claude here")
	}
}

// Folder trust the user gave on one account reaches an account another
// claude runs on, when a session starts there and when one switches there:
// only the trust is written, and nothing asks the user to answer Claude
// Code's own question.
func TestTrustReachesAnAccountInUse(t *testing.T) {
	s := newTest(t)
	var out bytes.Buffer
	s.opt.Out = &out
	aDir, bDir := s.reg.Accounts[0].ProfileDir, s.reg.Accounts[1].ProfileDir
	busy(t, bDir)

	// A switch onto b, in a folder a trusts.
	if _, err := claude.TrustFolder(aDir, s.opt.Cwd); err != nil {
		t.Fatal(err)
	}
	s.prepareProfile(&s.reg.Accounts[1])
	if !claude.IsTrusted(bDir, s.opt.Cwd) {
		t.Fatal("switch: b does not trust the folder")
	}
	if data, _ := os.ReadFile(filepath.Join(bDir, ".claude.json")); strings.Contains(string(data), "hasCompletedOnboarding") {
		t.Fatalf("switch: onboarding written into a profile in use: %s", data)
	}

	// A first start on b in another folder a trusts.
	other := t.TempDir()
	if _, err := claude.TrustFolder(aDir, other); err != nil {
		t.Fatal(err)
	}
	s.opt.Cwd, s.acct = other, s.reg.Accounts[1]
	s.copyTrustForFirstLaunch()
	if !claude.IsTrusted(bDir, other) {
		t.Fatal("first start: b does not trust the folder")
	}
	if out.Len() != 0 {
		t.Fatalf("notices: %s", out.String())
	}

	// A folder trusted nowhere: b is left as it is, and the user is told what
	// Claude Code will ask.
	s.opt.Cwd = t.TempDir()
	s.prepareProfile(&s.reg.Accounts[1])
	if claude.IsTrusted(bDir, s.opt.Cwd) || !strings.Contains(out.String(), "Yes, I trust this folder") {
		t.Fatalf("untrusted folder: trusted=%v notices=%q", claude.IsTrusted(bDir, s.opt.Cwd), out.String())
	}
}
