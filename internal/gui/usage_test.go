package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/usage"
)

// The window asks Claude Code for accounts' plan usage between sessions:
// when the Accounts tab shows (only accounts whose numbers are old, a
// signed-in subscription, never twice within a minute), and after a reply
// of the user's own Claude Code ended (the baton accounts that are its
// Claude account, whatever their numbers). The state says which account is
// being asked for.
func TestUsageChecks(t *testing.T) {
	const (
		acctW = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		acctF = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		org   = "a0000000-0000-4000-8000-00000000000a"
	)
	r := newRig(t)
	prof := func(id string) string { return filepath.Join(r.paths.Root, "profiles", id) }
	reg := accounts.Registry{Accounts: []accounts.Account{
		{ID: "work", ProfileDir: prof("work"), Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", AccountUUID: acctW, OrgID: org},
		{ID: "fresh", ProfileDir: prof("fresh"), Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", AccountUUID: acctF, OrgID: org},
		{ID: "key", ProfileDir: prof("key"), Enabled: true, LoggedIn: true, AuthMethod: "api_key"},
		{ID: "out", ProfileDir: prof("out"), Enabled: true, AuthMethod: "claude.ai"},
	}}
	if err := accounts.Save(r.paths.Accounts(), reg); err != nil {
		t.Fatal(err)
	}
	if err := usage.Save(r.paths.UsageDir(), "fresh", usage.Usage{SevenDay: &usage.Window{Used: 5, ResetsAt: time.Now().Add(time.Hour)}, Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	r.srv.mu.Lock()
	r.srv.claude, r.srv.claudeAt = ClaudeInfo{Found: true, Path: `C:\stand-in\claude.exe`, Version: "2.1.331"}, time.Now()
	r.srv.mu.Unlock()
	var mu sync.Mutex
	var asked []string
	release := make(chan struct{})
	r.srv.opt.AskUsage = func(exe, profileDir, dir, account string) (usage.Usage, error) {
		<-release
		mu.Lock()
		asked = append(asked, account)
		mu.Unlock()
		if exe != `C:\stand-in\claude.exe` || profileDir != prof(account) || dir != r.paths.UsageDir() {
			t.Errorf("asked with %q %q %q", exe, profileDir, dir)
		}
		u := usage.Usage{FiveHour: &usage.Window{Used: 34, ResetsAt: time.Now().Add(time.Hour)}, Updated: time.Now()}
		return u, usage.Save(dir, account, u)
	}
	got := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
	r.login()
	t.Cleanup(r.srv.waitBackground)

	code, body := r.do("POST", "/api/usage/refresh", `{}`, nil)
	if code != 200 || body == "" {
		t.Fatalf("refresh: %d %s", code, body)
	}
	read := func() map[string]bool {
		var st struct { // a new one each time: a false usage_checking is left out
			Accounts []struct {
				ID            string       `json:"id"`
				Usage         *usage.Usage `json:"usage"`
				UsageChecking bool         `json:"usage_checking"`
			} `json:"accounts"`
		}
		_, body := r.do("GET", "/api/state", "", nil)
		if err := json.Unmarshal([]byte(body), &st); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, a := range st.Accounts {
			out[a.ID] = a.UsageChecking
		}
		return out
	}
	if checking := read(); !checking["work"] || checking["fresh"] || checking["key"] || checking["out"] {
		t.Fatalf("checking while asked: %v", checking)
	}
	close(release)
	r.srv.waitBackground()
	if a := got(); !slices.Equal(a, []string{"work"}) {
		t.Fatalf("asked %v, want only work (fresh is new, key no subscription, out signed out)", a)
	}
	if checking := read(); checking["work"] {
		t.Fatalf("still checking: %v", checking)
	}
	// Shown again soon: nothing is old, nothing asked twice.
	r.do("POST", "/api/usage/refresh", `{}`, nil)
	r.srv.waitBackground()
	if a := got(); len(a) != 1 {
		t.Fatalf("asked again: %v", a)
	}

	// A reply of the user's own Claude Code ends: its Claude account's baton
	// accounts are asked for, though their numbers are new.
	usageAfterTurn = 0
	t.Cleanup(func() { usageAfterTurn = 3 * time.Second })
	home := filepath.Join(r.paths.Root, "userclaude")
	writeJSON := func(p string, v any) {
		b, _ := json.Marshal(v)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"accountUuid": acctF, "organizationUuid": org}})
	entry := filepath.Join(home, "sessions", strconv.Itoa(os.Getpid())+".json")
	session := func(status string) {
		writeJSON(entry, map[string]any{"pid": os.Getpid(), "sessionId": "own-1", "entrypoint": "claude-desktop", "status": status})
	}
	session("busy")
	r.srv.watchTurns() // the first look
	session("waiting") // asks the user something: still its turn
	r.srv.watchTurns()
	r.srv.waitBackground()
	if a := got(); len(a) != 1 {
		t.Fatalf("asked before the reply ended: %v", a)
	}
	session("busy")
	r.srv.watchTurns()
	session("idle")
	r.srv.watchTurns()
	r.srv.waitBackground()
	if a := got(); !slices.Equal(a, []string{"work", "fresh"}) {
		t.Fatalf("after the reply: asked %v", a)
	}
}
