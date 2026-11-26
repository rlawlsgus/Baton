package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/desktop"
)

// Every conversation on this PC in every account's desktop app: the setting,
// what the window's pass lists, the badges on the list of every
// conversation, and turning it off. The desktop app's data folder and the
// user's own Claude Code folder are temporary.
func TestDesktopShowAll(t *testing.T) {
	const (
		acct  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		org   = "a0000000-0000-4000-8000-00000000000a"
		inBat = "d35c0000-0000-4000-8000-0000000000a1" // on baton's account
		mine  = "d35c0000-0000-4000-8000-0000000000a2" // in the user's own Claude Code folder
	)
	r := newRig(t)
	data := filepath.Join(r.paths.Root, "Claude-Data", "claude_test")
	place := filepath.Join(data, "claude-code-sessions", acct, org)
	if err := os.MkdirAll(place, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(`{"lastKnownAccountUuid":"`+acct+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BATON_DESKTOP_DATA", data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	prof := filepath.Join(r.paths.Root, "profiles", "work")
	reg := accounts.Registry{Accounts: []accounts.Account{
		{ID: "work", ProfileDir: prof, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "w@example.com", AccountUUID: acct, OrgID: org},
	}}
	if err := accounts.Save(r.paths.Accounts(), reg); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	write := func(dir, id, prompt string) {
		file := filepath.Join(dir, "projects", claude.ProjectSlug(proj), id+".jsonl")
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		row := `{"type":"user","cwd":` + strconv.Quote(proj) + `,"message":{"role":"user","content":"` + prompt + `"}}` + "\n"
		if err := os.WriteFile(file, []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(prof, inBat, "tidy the docs")
	write(filepath.Join(r.paths.Root, "userclaude"), mine, "plan the release")
	r.login()
	t.Cleanup(r.srv.waitBackground) // before the settings above are put back

	// Off: the window's pass lists nothing.
	if res, err := r.srv.ShowAllDesktop(); err != nil || res.Added != 0 {
		t.Fatalf("off: %+v %v", res, err)
	}
	if code, body := r.do("POST", "/api/config", `{"key":"desktop_show_all","value":"true"}`, nil); code != 200 {
		t.Fatalf("on: %d %s", code, body)
	}
	if !r.srv.showAllSoon.Load() {
		t.Fatal("the window's loop was not asked for a pass")
	}
	res, err := r.srv.ShowAllDesktop()
	if err != nil || res.Added != 2 || res.Conversations != 2 {
		t.Fatalf("pass: %+v %v", res, err)
	}
	for _, id := range []string{inBat, mine} {
		if _, err := os.Stat(filepath.Join(place, "local_"+id+".json")); err != nil {
			t.Fatalf("no record of %s", id)
		}
	}

	// The list of every conversation says where each shows.
	code, body := r.do("GET", "/api/conversations?all=1&max=1000", "", nil)
	var list []struct {
		SessionID string `json:"session_id"`
		Where     string `json:"where"`
		Desktop   *struct {
			Places []string `json:"places"`
		} `json:"desktop"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &list) != nil || len(list) != 2 {
		t.Fatalf("conversations: %d %s", code, body)
	}
	for _, c := range list {
		if c.Desktop == nil || len(c.Desktop.Places) != 1 || c.Desktop.Places[0] != "work" {
			t.Fatalf("badge of %s: %s", c.SessionID, body)
		}
	}
	// Listed only because every conversation is, they are not baton's recent
	// sessions (the list of every conversation has them).
	code, body = r.do("GET", "/api/state", "", nil)
	if code != 200 || !strings.Contains(body, `"desktop_show_all":true`) || !strings.Contains(body, `"recent_sessions":[]`) {
		t.Fatalf("state: %d %s", code, body)
	}

	// Off: what the setting listed goes, and the window says how many. One
	// another program holds stays for later: the window's loop takes it off
	// once it can.
	held := filepath.Join(place, "local_"+mine+".json")
	if err := os.Remove(held); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(held, "held"), 0o700); err != nil { // cannot be removed as a file
		t.Fatal(err)
	}
	code, body = r.do("POST", "/api/config", `{"key":"desktop_show_all","value":"false"}`, nil)
	if code != 200 || !strings.Contains(body, `"removed":1`) || !strings.Contains(body, `"left":1`) {
		t.Fatalf("off: %d %s", code, body)
	}
	if err := os.RemoveAll(held); err != nil {
		t.Fatal(err)
	}
	r.srv.hideAllPass()
	if ix, err := desktop.LoadIndex(r.paths.Desktop()); err != nil || ix.HasAuto() {
		t.Fatalf("left after the loop's pass: %v", err)
	}
	for _, id := range []string{inBat, mine} {
		if _, err := os.Stat(filepath.Join(place, "local_"+id+".json")); err == nil {
			t.Fatalf("record of %s left", id)
		}
	}
	if res, err := r.srv.ShowAllDesktop(); err != nil || res.Added != 0 {
		t.Fatalf("off again: %+v %v", res, err)
	}
	if code, body := r.do("POST", "/api/config", `{"key":"desktop_show_all","value":"maybe"}`, nil); code != 400 {
		t.Fatalf("bad value: %d %s", code, body)
	}
}
