package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A Claude Code baton started only to ask for the account's usage is in the
// profile's session registry while it runs, as every Claude Code is; it is
// no session, so the profile never counts as in use for it.
func TestReadSessionsLeavesOutUsageChecks(t *testing.T) {
	dir := t.TempDir()
	write := func(pid int, entry string) {
		p := filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"s`+strconv.Itoa(pid)+`","entrypoint":"`+entry+`","status":"idle"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(os.Getpid(), UsageEntrypoint)
	if got := ReadSessions(dir); len(got) != 0 {
		t.Fatalf("a usage check read as a session: %+v", got)
	}
	if BusyProfile(dir) {
		t.Fatal("the profile is in use for a usage check")
	}
	write(os.Getpid()+1, "cli")
	if got := ReadSessions(dir); len(got) != 1 || got[0].Entrypoint != "cli" {
		t.Fatalf("sessions: %+v", got)
	}
}
