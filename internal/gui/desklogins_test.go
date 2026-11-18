package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// Switching the Claude desktop app's sign-in from the window. Every folder
// is temporary: a fake desktop app data folder shaped like the real one
// (BATON_DESKTOP_DATA), and baton's own (the rig's). desklogin's write
// guard panics on anything else, and Launch does nothing in a test.

const (
	dlA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	dlB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// fakeApp is the desktop app's data folder.
type fakeApp struct {
	t    *testing.T
	data string
}

// newLoginsRig is the window with baton's accounts work (Claude account A)
// and home (B), and a fake desktop app signed in to A.
func newLoginsRig(t *testing.T) (*rig, *fakeApp) {
	t.Helper()
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("as administrator, baton never switches the desktop app's sign-in (dl_elevated)")
	}
	r := newRig(t)
	app := &fakeApp{t: t, data: filepath.Join(t.TempDir(), "Claude-Data", "claude_test")}
	t.Setenv("BATON_DESKTOP_DATA", app.data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	// The harness runs inside Claude Code, where nothing switches; and a store
	// in OneDrive is refused.
	for _, k := range []string{"CLAUDECODE", "OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		t.Setenv(k, "")
	}
	reg := accounts.Registry{Accounts: []accounts.Account{
		{ID: "work", Priority: 20, ProfileDir: filepath.Join(r.paths.Root, "profiles", "work"), Enabled: true, LoggedIn: true,
			AuthMethod: "claude.ai", Email: "a@example.com", AccountUUID: dlA},
		{ID: "home", Priority: 10, ProfileDir: filepath.Join(r.paths.Root, "profiles", "home"), Enabled: true, LoggedIn: true,
			AuthMethod: "claude.ai", Email: "b@example.com", AccountUUID: dlB},
	}}
	if err := accounts.Save(r.paths.Accounts(), reg); err != nil {
		t.Fatal(err)
	}
	app.write(filepath.Join("Network", "TransportSecurity"), "hsts") // never moves
	app.signIn(dlA, "A")
	r.login()
	return r, app
}

func (a *fakeApp) write(rel, content string) {
	a.t.Helper()
	p := filepath.Join(a.data, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		a.t.Fatal(err)
	}
}

func (a *fakeApp) read(rel string) string {
	a.t.Helper()
	b, err := os.ReadFile(filepath.Join(a.data, rel))
	if err != nil {
		a.t.Fatal(err)
	}
	return string(b)
}

// signIn makes the app look as it does when signed in to acct and quit:
// its login set, and the six values in config.json among its settings.
func (a *fakeApp) signIn(acct, tag string) {
	a.t.Helper()
	for _, it := range []string{"Local Storage", "Session Storage", "IndexedDB", "WebStorage", filepath.Join("Network", "Cookies"), filepath.Join("Network", "Cookies-journal")} {
		if err := os.RemoveAll(filepath.Join(a.data, it)); err != nil {
			a.t.Fatal(err)
		}
	}
	a.write(filepath.Join("Local Storage", "leveldb", "000003.log"), "ls-"+tag)
	a.write(filepath.Join("Session Storage", "000003.log"), "ss-"+tag)
	a.write(filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log"), "idb-"+tag)
	a.write(filepath.Join("WebStorage", "QuotaManager"), "qm-"+tag)
	a.write(filepath.Join("Network", "Cookies"), "cookies-"+tag)
	a.write(filepath.Join("Network", "Cookies-journal"), "")
	a.write("config.json", `{
  "locale": "ko-KR",
  "oauth:tokenCache": "sealed-`+tag+`",
  "oauth:tokenCacheV2": "sealed2-`+tag+`",
  "lastKnownAccountUuid": "`+acct+`",
  "bootSideSessions": {},
  "bootSidebarPlaceholder": "`+tag+`",
  "sshTransportFlagLastAnswer": false,
  "windowSizeWasSignedIn": true
}`)
}

type loginsStateView struct {
	Config struct {
		DesktopLogins    bool `json:"desktop_logins"`
		DesktopLoginsAck bool `json:"desktop_logins_ack"`
	} `json:"config"`
	Accounts []struct {
		ID           string `json:"id"`
		DesktopLogin string `json:"desktop_login"`
		DesktopKept  bool   `json:"desktop_kept"`
	} `json:"accounts"`
	DesktopLogins *loginsSummary `json:"desktop_logins"`
}

// loginsState is the state poll, as the next one sees it: the window's
// look at the kept sign-ins is dropped first (it is kept a few seconds).
func (r *rig) loginsState() loginsStateView {
	r.t.Helper()
	r.srv.loginsChanged()
	return r.loginsCached()
}

// loginsCached is the state poll as it comes: a change made here dropped
// the look already.
func (r *rig) loginsCached() loginsStateView {
	r.t.Helper()
	code, body := r.do("GET", "/api/state", "", nil)
	var st loginsStateView
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil {
		r.t.Fatalf("state: %d %s", code, body)
	}
	return st
}

func (st loginsStateView) login(id string) string {
	for _, a := range st.Accounts {
		if a.ID == id {
			return a.DesktopLogin
		}
	}
	return "?"
}

// want posts body to path and checks the answer's status and code.
func (r *rig) want(path, body string, status int, code string) string {
	r.t.Helper()
	got, resp := r.do("POST", path, body, nil)
	if got != status || code != "" && !strings.Contains(resp, `"code":"`+code+`"`) {
		r.t.Fatalf("%s %s: %d %s, want %d %s", path, body, got, resp, status, code)
	}
	return resp
}

func TestDesktopLoginsFromTheWindow(t *testing.T) {
	r, app := newLoginsRig(t)

	// Off: the state says so, and nothing switches.
	st := r.loginsState()
	if st.DesktopLogins == nil || st.DesktopLogins.On || st.Config.DesktopLogins || st.login("work") != "" ||
		st.DesktopLogins.Store != r.paths.DesktopLogins() || st.DesktopLogins.Problem != "" {
		t.Fatalf("off: %+v", st)
	}
	r.want("/api/desktop/logins/use", `{"account":"`+dlA+`"}`, http.StatusNotFound, "dl_off")
	r.want("/api/desktop/logins/add", `{"open":true}`, http.StatusNotFound, "dl_off")
	r.want("/api/desktop/logins/rename", `{"account":"`+dlA+`","label":"x"}`, http.StatusNotFound, "dl_off")

	// On needs its notice accepted, and the plain setter never turns it on.
	r.want("/api/config", `{"key":"desktop_logins","value":"true"}`, http.StatusConflict, "needs_desktop_ack")
	r.want("/api/config", `{"key":"desktop_logins","value":"maybe","acknowledge":true}`, http.StatusBadRequest, "bad_value")
	if c, _ := config.Load(r.paths.Config()); c.DesktopLogins || c.DesktopLoginsAcknowledged != 0 {
		t.Fatalf("turned on without the notice: %+v", c)
	}
	r.want("/api/config", `{"key":"desktop_logins","value":"true","acknowledge":true}`, http.StatusOK, "")
	st = r.loginsState()
	if !st.Config.DesktopLogins || !st.Config.DesktopLoginsAck || !st.DesktopLogins.On || st.DesktopLogins.Active != "work" ||
		st.DesktopLogins.Kept != 0 || st.login("work") != "active" || st.login("home") != "" {
		t.Fatalf("on: %+v", st)
	}
	// The card: the account the app is on, not kept yet.
	code, body := r.do("GET", "/api/desktop/logins", "", nil)
	var v struct {
		On     bool `json:"on"`
		Logins []struct {
			Account string   `json:"account"`
			Label   string   `json:"label"`
			Baton   []string `json:"baton"`
			Active  bool     `json:"active"`
			Kept    bool     `json:"kept"`
		} `json:"logins"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &v) != nil || !v.On || len(v.Logins) != 1 || v.Logins[0].Account != dlA ||
		!v.Logins[0].Active || v.Logins[0].Kept || len(v.Logins[0].Baton) != 1 || v.Logins[0].Baton[0] != "work" {
		t.Fatalf("logins: %d %s", code, body)
	}
	if strings.Contains(body, "sealed") {
		t.Fatalf("the card's answer holds a value of the app's settings: %s", body)
	}

	// Only what the page sends is read.
	r.want("/api/desktop/logins/use", `{"account":"`+dlA+`","path":"C:\\x"}`, http.StatusBadRequest, "bad_request")
	r.want("/api/desktop/logins/add", `{"open":false,"acct":"x"}`, http.StatusBadRequest, "bad_request")

	// Add an account: A is kept, and the app's sign-in on this PC emptied.
	// The next poll shows it at once.
	r.loginsCached()
	resp := r.want("/api/desktop/logins/add", `{"open":false}`, http.StatusOK, "")
	if !strings.Contains(resp, `"from":"`+dlA+`"`) || !strings.Contains(resp, `"kept":true`) || strings.Contains(resp, `"opened"`) {
		t.Fatalf("add: %s", resp)
	}
	if cfg := app.read("config.json"); strings.Contains(cfg, "lastKnownAccountUuid") || strings.Contains(cfg, "sealed") ||
		!strings.Contains(cfg, `"locale": "ko-KR"`) {
		t.Fatalf("the app's sign-in is not empty: %s", cfg)
	}
	if _, err := os.Stat(filepath.Join(app.data, "Network", "Cookies")); !os.IsNotExist(err) {
		t.Fatalf("cookies left: %v", err)
	}
	if st = r.loginsCached(); st.DesktopLogins.Active != "" || st.DesktopLogins.Kept != 1 || st.login("work") != "kept" {
		t.Fatalf("after add: %+v", st)
	}

	// The user signs in to B in the app and quits it; then switches back to A
	// here (opening the app does nothing in a test).
	app.signIn(dlB, "B")
	resp = r.want("/api/desktop/logins/use", `{"account":"`+dlA+`","open":true}`, http.StatusOK, "")
	if !strings.Contains(resp, `"to":"`+dlA+`"`) || !strings.Contains(resp, `"from":"`+dlB+`"`) || !strings.Contains(resp, `"opened":true`) {
		t.Fatalf("use: %s", resp)
	}
	if cfg := app.read("config.json"); !strings.Contains(cfg, `"sealed2-A"`) || !strings.Contains(cfg, `"lastKnownAccountUuid": "`+dlA+`"`) {
		t.Fatalf("not on A: %s", cfg)
	}
	if app.read(filepath.Join("Network", "Cookies")) != "cookies-A" || app.read(filepath.Join("Network", "TransportSecurity")) != "hsts" {
		t.Fatal("the login set is not A's, or something else moved")
	}
	st = r.loginsState()
	if st.DesktopLogins.Active != "work" || st.DesktopLogins.Kept != 2 || st.login("work") != "active" || st.login("home") != "kept" {
		t.Fatalf("after use: %+v", st)
	}
	for _, a := range st.Accounts {
		if !a.DesktopKept {
			t.Fatalf("%s: not marked kept: %+v", a.ID, st)
		}
	}
	r.want("/api/desktop/logins/use", `{"account":"`+dlA+`"}`, http.StatusConflict, "dl_already")

	// While the app runs, nothing switches.
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	if st = r.loginsState(); !st.DesktopLogins.Running {
		t.Fatalf("not running: %+v", st)
	}
	r.want("/api/desktop/logins/use", `{"account":"`+dlB+`"}`, http.StatusConflict, "dl_app_running")
	r.want("/api/desktop/logins/add", `{"open":false}`, http.StatusConflict, "dl_app_running")
	if !strings.Contains(app.read("config.json"), `"sealed2-A"`) {
		t.Fatal("switched while the app runs")
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	// Nor from inside a Claude Code.
	t.Setenv("CLAUDECODE", "1")
	r.want("/api/desktop/logins/use", `{"account":"`+dlB+`"}`, http.StatusConflict, "dl_inside_claude")
	t.Setenv("CLAUDECODE", "")

	// A name of its own; a bad one is refused with why.
	r.want("/api/desktop/logins/rename", `{"account":"`+dlB+`","label":"Home PC"}`, http.StatusOK, "")
	resp = r.want("/api/desktop/logins/rename", `{"account":"`+dlB+`","label":"`+strings.Repeat("x", 41)+`"}`, http.StatusConflict, "dl_bad_label")
	if !strings.Contains(resp, `"details":"too_long"`) {
		t.Fatalf("bad label: %s", resp)
	}
	r.want("/api/desktop/logins/rename", `{"account":"`+dlB+`","label":"work"}`, http.StatusConflict, "dl_bad_label")
	r.srv.loginsChanged()
	if _, body := r.do("GET", "/api/desktop/logins", "", nil); !strings.Contains(body, `"label":"Home PC"`) {
		t.Fatalf("not renamed: %s", body)
	}

	// Forget B: it can no longer be switched to.
	r.want("/api/desktop/logins/drop", `{"account":"`+dlB+`"}`, http.StatusOK, "")
	r.want("/api/desktop/logins/use", `{"account":"`+dlB+`"}`, http.StatusConflict, "dl_not_kept")
	r.want("/api/desktop/logins/drop", `{"account":"`+dlB+`"}`, http.StatusConflict, "dl_not_kept")
	if st = r.loginsState(); st.DesktopLogins.Kept != 1 || st.login("home") != "" {
		t.Fatalf("after forget: %+v", st)
	}

	// Off (the page asks first): every kept sign-in is deleted, and turning
	// it on again shows the notice again. The app stays on A.
	resp = r.want("/api/config", `{"key":"desktop_logins","value":"false"}`, http.StatusOK, "")
	if !strings.Contains(resp, `"deleted":1`) {
		t.Fatalf("off: %s", resp)
	}
	if _, err := os.Stat(r.paths.DesktopLogins()); !os.IsNotExist(err) {
		t.Fatalf("the store is still there: %v", err)
	}
	if st = r.loginsState(); st.Config.DesktopLogins || st.Config.DesktopLoginsAck || st.DesktopLogins.On || st.DesktopLogins.Kept != 0 ||
		st.login("work") != "" {
		t.Fatalf("after off: %+v", st)
	}
	if !strings.Contains(app.read("config.json"), `"sealed2-A"`) {
		t.Fatal("turning off changed the app")
	}
	r.want("/api/desktop/logins/use", `{"account":"`+dlA+`"}`, http.StatusNotFound, "dl_off")
	r.want("/api/config", `{"key":"desktop_logins","value":"true"}`, http.StatusConflict, "needs_desktop_ack")
}

// What is left from before can be deleted while switching is off, and a
// switch that was cut off is put back then too: only while the app is quit.
func TestDesktopLoginsLeftoversAndCutOff(t *testing.T) {
	r, app := newLoginsRig(t)
	r.want("/api/config", `{"key":"desktop_logins","value":"true","acknowledge":true}`, http.StatusOK, "")
	r.want("/api/desktop/logins/add", `{"open":false}`, http.StatusOK, "")
	// Off by hand (an older baton, say): the kept sign-in is left behind.
	if _, err := config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.DesktopLogins = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if st := r.loginsState(); st.DesktopLogins.On || st.DesktopLogins.Kept != 1 || st.login("work") != "" {
		t.Fatalf("leftovers: %+v", st)
	}
	resp := r.want("/api/desktop/logins/drop-all", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"deleted":1`) {
		t.Fatalf("drop-all: %s", resp)
	}
	if _, err := os.Stat(r.paths.DesktopLogins()); !os.IsNotExist(err) {
		t.Fatalf("the store is still there: %v", err)
	}
	r.want("/api/desktop/logins/drop-all", `{"all":true}`, http.StatusBadRequest, "bad_request")

	// A switch cut off half way: its stage, with its journal, in the app's folder.
	app.signIn(dlA, "A")
	stage := filepath.Join(app.data, ".baton-switch-cut")
	app.write(filepath.Join(".baton-switch-cut", "old", "config.json"), app.read("config.json"))
	app.write(filepath.Join(".baton-switch-cut", "journal.json"),
		`{"schema":1,"from":"`+dlA+`","to":"`+dlB+`","phase":"moving","cfg_before":"x","items":[]}`)
	if st := r.loginsState(); !st.DesktopLogins.CutOff {
		t.Fatalf("not cut off: %+v", st)
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	r.want("/api/desktop/logins/recover", `{}`, http.StatusConflict, "dl_app_running")
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("the stage went while the app runs: %v", err)
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	resp = r.want("/api/desktop/logins/recover", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"undone":true`) {
		t.Fatalf("recover: %s", resp)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the stage is still there: %v", err)
	}
	if st := r.loginsState(); st.DesktopLogins.CutOff || st.DesktopLogins.Active != "work" {
		t.Fatalf("after recover: %+v", st)
	}
	resp = r.want("/api/desktop/logins/recover", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"undone":false`) {
		t.Fatalf("nothing to put back: %s", resp)
	}
}

// Uninstalling settles a switch that was cut off, which `baton uninstall`
// refuses while the app runs or from inside a Claude Code: the window says
// so before it closes, and starts no uninstall. Its look waits for the lock:
// another baton's look holding it a moment does not hide the switch.
func TestUninstallSaysACutOffSwitchFirst(t *testing.T) {
	r, app := newLoginsRig(t)
	called := make(chan struct{}, 1)
	r.srv.opt.Uninstall = func() error { called <- struct{}{}; return nil }
	stage := filepath.Join(app.data, ".baton-switch-cut")
	app.write(filepath.Join(".baton-switch-cut", "old", "config.json"), app.read("config.json"))
	app.write(filepath.Join(".baton-switch-cut", "journal.json"),
		`{"schema":1,"from":"`+dlA+`","to":"`+dlB+`","phase":"moving","cfg_before":"x","items":[]}`)

	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	held, err := fsx.LockWait(r.paths.DesktopLoginsLock(), 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(300 * time.Millisecond); held() }()
	r.want("/api/uninstall", `{}`, http.StatusConflict, "cut_off_running")
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	t.Setenv("CLAUDECODE", "1")
	r.want("/api/uninstall", `{}`, http.StatusConflict, "cut_off_inside_claude")
	select {
	case <-called:
		t.Fatal("uninstall started although it would be refused")
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(stage, "journal.json")); err != nil {
		t.Fatalf("the cut-off switch changed: %v", err)
	}

	// Outside Claude Code with the app quit, the uninstall settles it itself.
	t.Setenv("CLAUDECODE", "")
	r.want("/api/uninstall", `{}`, http.StatusOK, "")
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("uninstall not started")
	}
}

// Without the desktop app, and with nothing kept, the state says nothing of
// it (the setting is not offered).
func TestDesktopLoginsWithoutTheApp(t *testing.T) {
	r := newRig(t)
	t.Setenv("BATON_DESKTOP_DATA", "") // a test never looks for the real one
	t.Setenv("CLAUDECODE", "")
	r.login()
	code, body := r.do("GET", "/api/state", "", nil)
	if code != 200 || strings.Contains(body, `"desktop_logins":{`) {
		t.Fatalf("state: %d %s", code, body)
	}
}

// A failure that is no refusal (baton's settings file unreadable here): the
// page words it by its code alone, so what went wrong goes to the log.
func TestDesktopLoginsFailureIsLogged(t *testing.T) {
	r := newRig(t)
	t.Setenv("BATON_DESKTOP_DATA", filepath.Join(t.TempDir(), "Claude-Data", "claude_test"))
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	var mu sync.Mutex
	var logged []string
	r.srv.opt.Log = func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(format, a...))
	}
	r.login()
	if err := os.WriteFile(r.paths.Config(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := r.do("GET", "/api/desktop/logins", "", nil)
	if code != http.StatusInternalServerError || !strings.Contains(body, `"code":"desktop_logins"`) {
		t.Fatalf("logins: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.ContainsFunc(logged, func(s string) bool { return strings.HasPrefix(s, "desktop app sign-ins: ") && len(s) > 30 }) {
		t.Fatalf("not logged: %q", logged)
	}
}

// A switch whose config.json flip landed, after which the app was used on
// the sign-in it put in: with switching off, Put it back would throw that
// sign-in away, so it is refused (dl_used_since) and changes nothing; Leave
// it as it is keeps the app on it and ends the switch. Copies a switch left
// are counted for the window and deleted on request, also while off.
func TestDesktopLoginsLeaveAsItIsAndCopies(t *testing.T) {
	r, app := newLoginsRig(t)
	// The app was on A; the switch to B moved B's in and wrote config.json, and
	// the app has run on B since (config.json is neither as read nor as written).
	before := app.read("config.json")
	app.write(filepath.Join(".baton-switch-cut", "old", "config.json"), before)
	app.signIn(dlB, "B-used")
	app.write(filepath.Join(".baton-switch-cut", "journal.json"),
		`{"schema":1,"from":"`+dlA+`","to":"`+dlB+`","phase":"config","cfg_before":"x","cfg_after":"y","items":[]}`)
	// And a stage of a switch that stopped before it moved anything: a copy.
	app.write(filepath.Join(".baton-switch-copy", "old", "config.json"), before)
	st := r.loginsState()
	if !st.DesktopLogins.CutOff || st.DesktopLogins.Leftover != 1 || st.DesktopLogins.On {
		t.Fatalf("cut off: %+v", st.DesktopLogins)
	}
	r.want("/api/desktop/logins/recover", `{}`, http.StatusConflict, "dl_used_since")
	if app.read("config.json") == before || !strings.Contains(app.read("config.json"), dlB) {
		t.Fatal("Put it back changed the app")
	}
	if _, err := os.Stat(filepath.Join(app.data, ".baton-switch-cut", "journal.json")); err != nil {
		t.Fatalf("the refused switch's stage went: %v", err)
	}

	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	r.want("/api/desktop/logins/settle", `{}`, http.StatusConflict, "dl_app_running")
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	t.Setenv("CLAUDECODE", "1")
	r.want("/api/desktop/logins/settle", `{}`, http.StatusConflict, "dl_inside_claude")
	t.Setenv("CLAUDECODE", "")
	resp := r.want("/api/desktop/logins/settle", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"settled":true`) {
		t.Fatalf("settle: %s", resp)
	}
	if _, err := os.Stat(filepath.Join(app.data, ".baton-switch-cut")); !os.IsNotExist(err) {
		t.Fatalf("the stage is still there: %v", err)
	}
	if !strings.Contains(app.read("config.json"), `"sealed-B-used"`) || app.read(filepath.Join("Network", "Cookies")) != "cookies-B-used" {
		t.Fatal("the app was not left on the sign-in it used")
	}
	st = r.loginsState()
	if st.DesktopLogins.CutOff || st.DesktopLogins.Active != "home" {
		t.Fatalf("after settle: %+v", st.DesktopLogins)
	}
	resp = r.want("/api/desktop/logins/settle", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"settled":false`) {
		t.Fatalf("nothing to settle: %s", resp)
	}

	// The copy: Settle tidied it on the way; one made again goes on request,
	// while the app runs too (it never reads it).
	app.write(filepath.Join(".baton-switch-copy", "old", "config.json"), before)
	if st := r.loginsState(); st.DesktopLogins.Leftover != 1 {
		t.Fatalf("the copy is not counted: %+v", st.DesktopLogins)
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	resp = r.want("/api/desktop/logins/tidy", `{}`, http.StatusOK, "")
	if !strings.Contains(resp, `"deleted":1`) {
		t.Fatalf("tidy: %s", resp)
	}
	if _, err := os.Stat(filepath.Join(app.data, ".baton-switch-copy")); !os.IsNotExist(err) {
		t.Fatalf("the copy is still there: %v", err)
	}
	if st := r.loginsState(); st.DesktopLogins.Leftover != 0 {
		t.Fatalf("after tidy: %+v", st.DesktopLogins)
	}
}
