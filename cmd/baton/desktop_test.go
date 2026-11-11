package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/claude"
	"baton/internal/desktop"
)

// `baton desktop <id>` says where a conversation shows in the Claude
// desktop app and changes it, by account name. Every folder is a temporary
// one: the user's home, the desktop app's data folder and baton's.
func TestDesktopCommand(t *testing.T) {
	c := newCLI(t)
	root := t.TempDir()
	data := filepath.Join(root, "Claude-Data", "claude_test")
	home := filepath.Join(root, "user")
	env := []string{"USERPROFILE=" + home, "LOCALAPPDATA=" + filepath.Join(root, "local"), "APPDATA=" + filepath.Join(root, "roaming"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"), "BATON_DESKTOP_DATA=" + data, "BATON_DESKTOP_RUNNING=0"}
	if out, code := c.run(env, "add", "personal"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	a := c.registry().Get("personal")
	if a == nil || len(a.AccountUUID) != 36 || len(a.OrgID) != 36 {
		t.Fatalf("the account's ids were not read at sign-in: %+v", a)
	}
	place := filepath.Join(data, "claude-code-sessions", a.AccountUUID, a.OrgID)
	if err := os.MkdirAll(place, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(`{"lastKnownAccountUuid":"`+a.AccountUUID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const sid = "d35c0000-0000-4000-8000-000000000001"
	proj := filepath.Join(root, "Proj")
	file := filepath.Join(a.ProfileDir, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(proj, 0o700)
	_ = os.MkdirAll(filepath.Dir(file), 0o700)
	if err := os.WriteFile(file, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"tidy the docs"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(place, "local_"+sid+".json")
	second := filepath.Join(home, ".claude", "projects", claude.ProjectSlug(proj), sid+".jsonl")

	out, code := c.run(env, "desktop", sid)
	if code != 0 || !strings.Contains(out, "tidy the docs") || !strings.Contains(out, "[ ] personal (personal@example.com)") ||
		!strings.Contains(out, "signed in to the desktop app now") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", sid, "--show", "personal")
	if code != 0 || !strings.Contains(out, "Done. It shows the next time you open the Claude desktop app.") || !isFile(record) || !isFile(second) {
		t.Fatalf("show: %d\n%s", code, out)
	}
	if out, _ := c.run(env, "desktop", sid); !strings.Contains(out, "[x] personal") {
		t.Fatalf("list after show:\n%s", out)
	}
	if out, code := c.run(env, "desktop", sid, "--show", "nobody"); code == 0 || !strings.Contains(out, `no account "nobody"`) {
		t.Fatalf("an unknown account: %d\n%s", code, out)
	}
	if out, code := c.run(append(env, "BATON_RUN=x"), "desktop", sid, "--hide", "personal"); code == 0 || !strings.Contains(out, "inside a baton session") || !isFile(record) {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", sid, "--hide", "personal")
	if code != 0 || isFile(record) || isFile(second) || !isFile(file) {
		t.Fatalf("hide: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "d35c0000-0000-4000-8000-000000000009"); code == 0 || !strings.Contains(out, "first message") {
		t.Fatalf("a conversation with no file: %d\n%s", code, out)
	}
}

// The message after a change says when each account's list shows it.
func TestDesktopDoneLine(t *testing.T) {
	t.Setenv("BATON_LANG", "")
	for _, c := range []struct {
		r    desktop.Result
		want string
	}{
		{desktop.Result{Added: []string{"x"}, OtherAdded: true}, "Done. It shows when you switch to that account in the desktop app."},
		{desktop.Result{Added: []string{"x"}, ActiveAdded: true}, "Done. It shows the next time you open the Claude desktop app."},
		{desktop.Result{Added: []string{"x", "y"}, ActiveAdded: true, OtherAdded: true, AppRunning: true},
			"Done. Quit the Claude desktop app (also from its tray icon) and open it again to see the change. It shows when you switch to that account in the desktop app."},
		{desktop.Result{Removed: []string{"x"}, ActiveRemoved: true, AppRunning: true},
			"Done. It disappears from the desktop app once you quit the app while baton's window is open."},
		{desktop.Result{Removed: []string{"x"}}, "Done. It is no longer listed for those accounts."},
		{desktop.Result{}, "Nothing changed."},
	} {
		if got := doneLine(c.r); got != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c.r, got, c.want)
		}
	}
	t.Setenv("BATON_LANG", "ko")
	r := desktop.View{Rows: []desktop.Row{{Accounts: []string{"laptop", "work"}, Key: "k"}}}
	if got := placeNote(r, r.Rows[0]); !strings.Contains(got, "laptop, work은(는) 같은 Claude 계정입니다") {
		t.Errorf("korean: %s", got)
	}
	if got := placeNote(r, desktop.Row{Why: "cloud", Provider: "bedrock"}); !strings.Contains(got, "Amazon Bedrock을(를)") {
		t.Errorf("provider: %s", got)
	}
}
