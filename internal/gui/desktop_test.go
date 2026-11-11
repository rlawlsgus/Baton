package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/accounts"
	"baton/internal/claude"
)

// The window's dialog for the Claude desktop app: its places, applying a
// choice, the badge on the session's row, and what it refuses. The desktop
// app's data folder and the user's own Claude Code folder are temporary.
func TestDesktopDialog(t *testing.T) {
	const (
		acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		org  = "a0000000-0000-4000-8000-00000000000a"
		sid  = "d35c0000-0000-4000-8000-000000000001"
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
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	prof := filepath.Join(r.paths.Root, "profiles", "work")
	reg := accounts.Registry{Accounts: []accounts.Account{
		{ID: "work", ProfileDir: prof, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "w@example.com", AccountUUID: acct, OrgID: org},
		{ID: "key", ProfileDir: filepath.Join(r.paths.Root, "profiles", "key"), Enabled: true, LoggedIn: true, AuthMethod: "api_key"},
	}}
	if err := accounts.Save(r.paths.Accounts(), reg); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	file := filepath.Join(prof, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(file), 0o700)
	if err := os.WriteFile(file, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"tidy the docs"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.login()
	key := acct + "/" + org

	code, body := r.do("GET", "/api/desktop/places?session_id="+sid, "", nil)
	var v struct {
		Title      string `json:"title"`
		AppRunning bool   `json:"app_running"`
		Places     []struct {
			Key      string   `json:"key"`
			Accounts []string `json:"accounts"`
			Active   bool     `json:"active"`
			Checked  bool     `json:"checked"`
			Disabled bool     `json:"disabled"`
			Why      string   `json:"why"`
		} `json:"places"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &v) != nil || v.Title != "tidy the docs" || !v.AppRunning || len(v.Places) != 2 ||
		v.Places[0].Key != key || !v.Places[0].Active || v.Places[0].Checked || v.Places[1].Why != "api_key" || !v.Places[1].Disabled {
		t.Fatalf("places: %d %s", code, body)
	}
	code, body = r.do("POST", "/api/desktop/apply", `{"session_id":"`+strings.ToUpper(sid)+`","places":["`+key+`"]}`, nil)
	if code != 200 || !strings.Contains(body, `"active_added":true`) || !strings.Contains(body, `"app_running":true`) {
		t.Fatalf("apply: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(place, "local_"+sid+".json")); err != nil {
		t.Fatal("no record written")
	}
	// The session is listed (it has no run any more) with its badge.
	code, body = r.do("GET", "/api/state", "", nil)
	var st struct {
		Desktop bool `json:"desktop"`
		Recent  []struct {
			SessionID string `json:"session_id"`
			Where     string `json:"where"`
			Desktop   *struct {
				Places []string `json:"places"`
			} `json:"desktop"`
		} `json:"recent_sessions"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil || !st.Desktop || len(st.Recent) != 1 || st.Recent[0].SessionID != sid ||
		st.Recent[0].Where != "work" || st.Recent[0].Desktop == nil || len(st.Recent[0].Desktop.Places) != 1 || st.Recent[0].Desktop.Places[0] != "work" {
		t.Fatalf("state: %d %s", code, body)
	}

	for name, c := range map[string]struct {
		method, path, body string
		status             int
		code               string
	}{
		"bad id":         {"GET", "/api/desktop/places?session_id=..%5Cx", "", http.StatusBadRequest, "bad_session"},
		"no file":        {"GET", "/api/desktop/places?session_id=d35c0000-0000-4000-8000-000000000009", "", http.StatusConflict, "no_file"},
		"unknown place":  {"POST", "/api/desktop/apply", `{"session_id":"` + sid + `","places":["` + acct + `/` + acct + `"]}`, http.StatusBadRequest, "unknown_place"},
		"not a place":    {"POST", "/api/desktop/apply", `{"session_id":"` + sid + `","places":["..\\..\\x"]}`, http.StatusBadRequest, "unknown_place"},
		"no file apply":  {"POST", "/api/desktop/apply", `{"session_id":"d35c0000-0000-4000-8000-000000000009","places":[]}`, http.StatusConflict, "no_file"},
		"unknown fields": {"POST", "/api/desktop/apply", `{"session_id":"` + sid + `","places":[],"path":"C:\\x"}`, http.StatusBadRequest, "bad_request"},
	} {
		code, body := r.do(c.method, c.path, c.body, nil)
		if code != c.status || !strings.Contains(body, `"code":"`+c.code+`"`) {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	// Like every API call, it needs the page's token.
	r.token = ""
	if code, _ := r.do("POST", "/api/desktop/apply", `{"session_id":"`+sid+`","places":[]}`, nil); code != http.StatusForbidden {
		t.Fatalf("without the token: %d", code)
	}
	r.login()
	code, body = r.do("POST", "/api/desktop/apply", `{"session_id":"`+sid+`","places":[]}`, nil)
	if code != 200 || !strings.Contains(body, `"active_removed":true`) {
		t.Fatalf("untick: %d %s", code, body)
	}

	// Without the desktop app the feature is not offered.
	t.Setenv("BATON_DESKTOP_DATA", "")
	r.srv.deskSt.dataAt = r.srv.deskSt.dataAt.AddDate(0, 0, -1)
	if code, body := r.do("GET", "/api/state", "", nil); code != 200 || !strings.Contains(body, `"desktop":false`) {
		t.Fatalf("state without the app: %d %s", code, body)
	}
	if code, body := r.do("GET", "/api/desktop/places?session_id="+sid, "", nil); code != http.StatusConflict || !strings.Contains(body, `"code":"no_desktop"`) {
		t.Fatalf("places without the app: %d %s", code, body)
	}
}

// While a change waits for the desktop app to close, the window makes it as
// soon as the app is gone; it asks whether the app runs only meanwhile.
func TestDeskWaitClosed(t *testing.T) {
	var w deskWait
	asked := 0
	running := true
	app := func() bool { asked++; return running }
	for i, c := range []struct {
		waiting, running, want bool
	}{
		{false, true, false},
		{true, true, false},
		{true, true, false},
		{true, false, true}, // quit while a change waited: now
		{true, false, false},
		{true, true, false}, // opened again before it was done
		{true, false, true},
		{false, false, false},
	} {
		running = c.running
		if got := w.closed(c.waiting, app); got != c.want {
			t.Fatalf("step %d: %v, want %v", i, got, c.want)
		}
	}
	if asked != 6 {
		t.Fatalf("asked whether the app runs %d times, want only while waiting (6)", asked)
	}
}
