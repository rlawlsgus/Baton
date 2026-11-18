package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/run"
)

// A conversation being deleted starts nothing: not as it is, not in a copy
// (which would start from its files), not as the folder's newest. Once the
// delete lets go, it starts again.
func TestStartRefusedWhileDeleting(t *testing.T) {
	r := newRig(t)
	r.login()
	prof := filepath.Join(r.paths.ProfilesDir(), "a")
	if _, err := accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		return reg.Add(accounts.Account{ID: "a", ProfileDir: prof, Enabled: true})
	}); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	const sid = "11111111-2222-4333-8444-555555555555"
	tr := filepath.Join(prof, "projects", "x", sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	_ = os.WriteFile(tr, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"hi"}}`+"\n"), 0o600)
	start := func(v map[string]any) (int, string) {
		b, _ := json.Marshal(v)
		return r.do("POST", "/api/sessions/start", string(b), nil)
	}

	release, err := run.MarkDeleting(r.paths.Root, sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []map[string]any{
		{"folder": proj, "mode": "resume", "session_id": sid, "where": "a"},
		{"folder": proj, "mode": "resume", "session_id": strings.ToUpper(sid), "where": "a", "copy": true},
		{"folder": proj, "mode": "continue"},
	} {
		if code, body := start(req); code != 409 || !strings.Contains(body, `"deleting"`) || len(r.spawn.console) != 0 {
			t.Fatalf("%v while deleting: %d %s %q", req, code, body, r.spawn.console)
		}
	}
	// A new conversation in the folder has nothing to do with it.
	if code, body := start(map[string]any{"folder": proj}); code != 202 || !slices.Equal(r.spawn.console[0], []string{"run"}) {
		t.Fatalf("new while deleting another: %d %s %q", code, body, r.spawn.console)
	}
	release()
	if code, body := start(map[string]any{"folder": proj, "mode": "resume", "session_id": sid, "where": "a"}); code != 202 ||
		!slices.Equal(r.spawn.console[1], []string{"run", "--", "--resume", tr}) {
		t.Fatalf("after the delete let go: %d %s %q", code, body, r.spawn.console)
	}
}

// The uninstall moves conversations to the user's own Claude Code: the
// window refuses it while a conversation is being deleted (here or in a
// terminal), before it closes, and lets it go once the delete is done.
func TestUninstallWaitsForADelete(t *testing.T) {
	r := newRig(t)
	called := make(chan struct{}, 1)
	r.srv.opt.Uninstall = func() error { called <- struct{}{}; return nil }
	r.login()
	release, err := run.MarkDeleting(r.paths.Root, "de1e7e00-0000-4000-8000-0000000000aa")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != http.StatusConflict || !strings.Contains(body, `"code":"deleting_any"`) {
		t.Fatalf("uninstall while deleting: %d %s", code, body)
	}
	select {
	case <-called:
		t.Fatal("the uninstall started while a conversation was being deleted")
	case <-time.After(500 * time.Millisecond):
	}
	release()
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != http.StatusOK {
		t.Fatalf("uninstall after the delete: %d %s", code, body)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("the uninstall did not start")
	}
}
