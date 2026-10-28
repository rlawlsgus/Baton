package gui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/winproc"
)

type fakeSpawner struct {
	mu      sync.Mutex
	console [][]string
	dirs    []string
	hidden  [][]string
	out     string
	code    int
	release chan struct{} // when set, console windows stay open until closed
	result  string        // JSON a sign-in window writes to its --result-file
	// hiddenGate, when set, holds hidden commands until it is closed.
	hiddenGate chan struct{}
}

func (f *fakeSpawner) Console(dir string, args []string) (func() int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.console = append(f.console, args)
	f.dirs = append(f.dirs, dir)
	release := f.release
	result := f.result
	return func() int {
		if release != nil {
			<-release
		}
		// What `baton add/login --result-file` writes when it ends.
		for i, a := range args {
			if a == "--result-file" && result != "" {
				_ = os.WriteFile(args[i+1], []byte(result), 0o600)
			}
		}
		return 0
	}, nil
}

func (f *fakeSpawner) Hidden(args []string) (string, int, error) {
	f.mu.Lock()
	gate := f.hiddenGate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden = append(f.hidden, args)
	return f.out, f.code, nil
}

type rig struct {
	t      *testing.T
	srv    *Server
	spawn  *fakeSpawner
	client *http.Client
	paths  app.Paths
	token  string // the page's session token
	page   string // the id of the page load sending requests
	seq    int    // its request counter
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	paths := app.Paths{Root: root}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "userclaude"))
	t.Setenv("BATON_RUN", "")
	sp := &fakeSpawner{}
	srv := New(Options{Paths: paths, Spawner: sp, Version: "test", Lang: "ko",
		Claude: func() ClaudeInfo { return ClaudeInfo{Found: true, Version: "2.1.308", Source: "desktop"} }})
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: srv}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	// Switching set up, as after onboarding (sessions refuse to start before).
	if _, err := config.Update(paths.Config(), paths.ConfigLock(), func(c *config.Config) error {
		c.SwitchMode, c.RiskAcknowledged = config.ModeConfirm, config.DisclosureVersion
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, srv: srv, spawn: sp, client: &http.Client{}, paths: paths}
}

// exchange trades a window URL's one-time code for a session token, as the
// page does.
func (r *rig) exchange(windowURL string) (int, string) {
	r.t.Helper()
	u, err := url.Parse(windowURL)
	if err != nil {
		r.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"code": u.Query().Get("k")})
	resp, err := r.client.Post(r.srv.Origin()+"/api/session", "application/json", strings.NewReader(string(b)))
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Token
}

// login opens a page: the token goes with every later request.
func (r *rig) login() {
	r.t.Helper()
	code, token := r.exchange(r.srv.WindowURL())
	if code != 200 || token == "" {
		r.t.Fatalf("code exchange: %d", code)
	}
	r.token, r.page = token, "load-"+token[:8]
}

func (r *rig) do(method, path, body string, hdr map[string]string) (int, string) {
	r.t.Helper()
	req, _ := http.NewRequest(method, r.srv.Origin()+path, strings.NewReader(body))
	if r.token != "" {
		req.Header.Set(tokenHeader, r.token)
		req.Header.Set(pageHeader, r.page)
		r.seq++
		req.Header.Set(seqHeader, strconv.Itoa(r.seq))
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAuthOneTimeCodeAndHeaders(t *testing.T) {
	r := newRig(t)
	// No token yet: the API answers nothing.
	if code, _ := r.do("GET", "/api/state", "", nil); code != http.StatusForbidden {
		t.Fatalf("state without token: %d", code)
	}
	u := r.srv.WindowURL()
	if code, token := r.exchange(u); code != 200 || token == "" {
		t.Fatalf("exchange: %d", code)
	} else {
		r.token = token
	}
	// The code is single use.
	if code, token := r.exchange(u); code != http.StatusForbidden || token != "" {
		t.Fatalf("reused code accepted: %d", code)
	}
	if code, body := r.do("GET", "/api/state", "", nil); code != 200 || !strings.Contains(body, `"version":"test"`) {
		t.Fatalf("state: %d %s", code, body)
	}
	// No cookie is set: a browser would send it to every 127.0.0.1 port.
	resp, err := r.client.Get(r.srv.Origin() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(resp.Cookies()) != 0 {
		t.Fatalf("cookies: %v", resp.Cookies())
	}
	// Cross-site requests: no or a wrong token, another Origin, or cross-site fetch metadata.
	for name, hdr := range map[string]map[string]string{
		"no token":    {tokenHeader: ""},
		"wrong token": {tokenHeader: strings.Repeat("0", 64)},
		"origin":      {"Origin": "https://evil.example"},
		"fetch site":  {"Sec-Fetch-Site": "cross-site"},
	} {
		if code, _ := r.do("POST", "/api/config", `{"key":"switch_mode","value":"off"}`, hdr); code != http.StatusForbidden {
			t.Errorf("%s: %d", name, code)
		}
	}
	// A rebinding name that resolves to 127.0.0.1 is refused.
	req, _ := http.NewRequest("GET", r.srv.Origin()+"/api/state", nil)
	req.Host = "attacker.example:80"
	req.Header.Set(tokenHeader, r.token)
	resp, err = r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("foreign Host: %d", resp.StatusCode)
	}
	// Security headers.
	resp, _ = r.client.Get(r.srv.Origin() + "/")
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers: %v", resp.Header)
	}
}

func TestExpiredCode(t *testing.T) {
	r := newRig(t)
	u := r.srv.WindowURL()
	k := u[strings.Index(u, "k=")+2:]
	r.srv.mu.Lock()
	r.srv.codes[k] = time.Now().Add(-time.Second)
	r.srv.mu.Unlock()
	if code, token := r.exchange(u); code != http.StatusForbidden || token != "" {
		t.Fatalf("expired code accepted: %d", code)
	}
}

// age moves every page load 4 s into the past (its last bye and its last
// request alike, so their order is kept).
func (r *rig) age() {
	r.srv.mu.Lock()
	defer r.srv.mu.Unlock()
	for _, loads := range r.srv.tokens {
		for _, p := range loads {
			if !p.byeAt.IsZero() {
				p.byeAt = p.byeAt.Add(-4 * time.Second)
			}
			p.lastSeen = p.lastSeen.Add(-4 * time.Second)
		}
	}
}

func TestByeClosesUnlessThePageComesBack(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/bye", "", nil); code != http.StatusNoContent {
		t.Fatalf("bye: %d", code)
	}
	r.age()
	if !r.srv.Closed() {
		t.Fatal("not closed after bye")
	}
	// A poll sent before the bye but answered after it does not bring the
	// page back.
	r.do("POST", "/api/bye", "", nil)
	time.Sleep(30 * time.Millisecond) // Windows' clock ticks coarsely
	r.seq -= 2
	r.do("GET", "/api/state", "", map[string]string{})
	r.seq += 2
	r.age()
	if !r.srv.Closed() {
		t.Fatal("a late poll from a closed page kept the GUI open")
	}
	// A request numbered after the bye is the same load shown again (back
	// from the browser's page cache): it is open again.
	time.Sleep(30 * time.Millisecond)
	r.do("GET", "/api/state", "", nil)
	if r.srv.Closed() {
		t.Fatal("a page restored from the cache was taken for closed")
	}
	// A reload says bye, then the new load polls: that is not a close.
	r.do("POST", "/api/bye", "", nil)
	r.age()
	r.page = "reloaded"
	r.do("GET", "/api/state", "", nil)
	if r.srv.Closed() {
		t.Fatal("a reload was taken for a close")
	}
	// A duplicated tab copies the token but is a load of its own: closing
	// it leaves the GUI running for the original.
	original := r.page
	r.page = "duplicate"
	r.do("GET", "/api/state", "", nil)
	r.do("POST", "/api/bye", "", nil)
	r.age()
	if r.srv.Closed() {
		t.Fatal("closing a duplicated tab closed the GUI")
	}
	// Two tabs from two window addresses: both must say bye.
	first, firstPage := r.token, original
	r.login()
	second, secondPage := r.token, r.page
	r.do("GET", "/api/state", "", nil)
	r.token, r.page = first, firstPage
	r.do("POST", "/api/bye", "", nil)
	r.age()
	if r.srv.Closed() {
		t.Fatal("closed while another tab is open")
	}
	r.token, r.page = second, secondPage
	r.do("POST", "/api/bye", "", nil)
	r.age()
	if !r.srv.Closed() {
		t.Fatal("not closed after every tab said bye")
	}
}

func TestConfigNeedsAcknowledgement(t *testing.T) {
	r := newRig(t)
	r.login()
	_, _ = config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.SwitchMode, c.RiskAcknowledged = "", 0
		return nil
	})
	code, body := r.do("POST", "/api/config", `{"key":"switch_mode","value":"auto"}`, nil)
	if code != http.StatusConflict || !strings.Contains(body, "needs_ack") {
		t.Fatalf("auto without ack: %d %s", code, body)
	}
	if code, body := r.do("POST", "/api/config", `{"key":"switch_mode","value":"auto","acknowledge":true}`, nil); code != 200 {
		t.Fatalf("auto with ack: %d %s", code, body)
	}
	c, _ := config.Load(r.paths.Config())
	if c.SwitchMode != "auto" || c.RiskAcknowledged != config.DisclosureVersion {
		t.Fatalf("config: %+v", c)
	}
	if code, _ := r.do("POST", "/api/config", `{"key":"gui_language","value":"xx"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("bad language accepted: %d", code)
	}
	if code, _ := r.do("POST", "/api/config", `{"key":"switch_mode","value":"auto","extra":1}`, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d", code)
	}
}

func TestAccountActionsRunBaton(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/accounts/add", `{"name":"Bad Name"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("bad name: %d", code)
	}
	if code, _ := r.do("POST", "/api/accounts/add", `{"name":"work"}`, nil); code != http.StatusAccepted {
		t.Fatalf("add: %d", code)
	}
	if code, _ := r.do("POST", "/api/accounts/remove", `{"name":"work","force":true}`, nil); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	r.spawn.code, r.spawn.out = 1, "baton: no account named \"x\"\n"
	code, body := r.do("POST", "/api/accounts/enable", `{"name":"x"}`, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `no account named \"x\"`) {
		t.Fatalf("failed command: %d %s", code, body)
	}
	// A finished sign-in window also refreshes every account's status (doctor).
	var hidden [][]string
	for _, h := range r.spawn.hidden {
		if h[0] != "doctor" {
			hidden = append(hidden, h)
		}
	}
	if !slices.Equal(r.spawn.console[0][:3], []string{"add", "work", "--result-file"}) || len(hidden) != 2 ||
		!slices.Equal(hidden[0], []string{"remove", "work", "--yes", "--force"}) ||
		!slices.Equal(hidden[1], []string{"enable", "x"}) {
		t.Fatalf("spawned: console=%q hidden=%q", r.spawn.console, r.spawn.hidden)
	}
}

func TestStartSessionArgsAndTranscriptCheck(t *testing.T) {
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
	empty := t.TempDir()

	body := func(v map[string]string) string { b, _ := json.Marshal(v); return string(b) }
	cases := []struct {
		req  map[string]string
		code int
		args []string
	}{
		{map[string]string{"folder": proj}, 202, []string{"run"}},
		{map[string]string{"folder": proj, "account": "a", "mode": "continue"}, 202, []string{"run", "--account", "a", "--", "--resume", tr}},
		{map[string]string{"folder": empty, "mode": "continue"}, 400, nil},
		{map[string]string{"folder": `"` + proj + `"`}, 202, []string{"run"}},
		{map[string]string{"folder": proj, "mode": "resume", "session_id": sid, "where": "a"}, 202, []string{"run", "--", "--resume", tr}},
		{map[string]string{"folder": proj, "mode": "resume", "session_id": sid, "where": "claude-code"}, 400, nil},
		{map[string]string{"folder": proj, "mode": "resume", "session_id": "../../evil", "where": "a"}, 400, nil},
		{map[string]string{"folder": proj, "mode": "resume", "session_id": sid, "where": "nobody"}, 400, nil},
		{map[string]string{"folder": "relative"}, 400, nil},
		{map[string]string{"folder": `\\server\share\proj`}, 400, nil},
		{map[string]string{"folder": proj + `\` + strings.Repeat("x", 250)}, 400, nil},
		{map[string]string{"folder": proj, "account": "nobody"}, 400, nil},
	}
	n := 0
	for i, c := range cases {
		code, b := r.do("POST", "/api/sessions/start", body(c.req), nil)
		if code != c.code {
			t.Errorf("case %d: %d %s", i, code, b)
			continue
		}
		if c.args != nil {
			if !slices.Equal(r.spawn.console[n], c.args) || !strings.EqualFold(r.spawn.dirs[n], filepath.Clean(proj)) {
				t.Errorf("case %d: args %q dir %q", i, r.spawn.console[n], r.spawn.dirs[n])
			}
			n++
		}
	}
}

func TestOpenOnlyBatonFolders(t *testing.T) {
	r := newRig(t)
	var opened []string
	r.srv.opt.Open = func(p string) error { opened = append(opened, p); return nil }
	r.login()
	if code, _ := r.do("POST", "/api/open", `{"what":"data"}`, nil); code != 200 {
		t.Fatalf("data: %d", code)
	}
	for _, bad := range []string{`{"what":"run","run_id":"..\\..\\Windows"}`, `{"what":"folder","run_id":"x"}`, `{"what":"C:\\"}`} {
		if code, _ := r.do("POST", "/api/open", bad, nil); code == 200 {
			t.Errorf("%s opened", bad)
		}
	}
	if len(opened) != 1 || opened[0] != r.paths.Root {
		t.Fatalf("opened %q", opened)
	}
}

func TestStateShowsAccountsAndLanguage(t *testing.T) {
	r := newRig(t)
	r.login()
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		_ = reg.Add(accounts.Account{ID: "b", Enabled: true, LoggedIn: true, Priority: 10})
		return reg.Add(accounts.Account{ID: "a", Enabled: true, LoggedIn: false, Priority: 20})
	})
	_, body := r.do("GET", "/api/state", "", nil)
	var st stateView
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	if st.Lang != "ko" || len(st.Accounts) != 2 || st.Accounts[0].ID != "a" || st.Accounts[0].Status != "signed_out" || st.Accounts[1].Status != "ok" {
		t.Fatalf("state: %+v", st)
	}
	_, _ = config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error { c.GUILanguage = "en"; return nil })
	_, body = r.do("GET", "/api/state", "", nil)
	_ = json.Unmarshal([]byte(body), &st)
	if st.Lang != "en" {
		t.Fatalf("language override ignored: %s", st.Lang)
	}
}

func TestMoveReordersAccounts(t *testing.T) {
	r := newRig(t)
	r.login()
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		for _, id := range []string{"a", "b", "c"} {
			_ = reg.Add(accounts.Account{ID: id, Priority: 5})
		}
		return nil
	})
	if code, _ := r.do("POST", "/api/accounts/move", `{"name":"c","dir":"up"}`, nil); code != 200 {
		t.Fatalf("move: %d", code)
	}
	reg, _ := accounts.Load(r.paths.Accounts())
	var order []string
	for _, a := range byPriority(reg.Accounts) {
		order = append(order, a.ID)
	}
	if !slices.Equal(order, []string{"a", "c", "b"}) {
		t.Fatalf("order %v", order)
	}
}

func TestStaticFiles(t *testing.T) {
	r := newRig(t)
	r.login()
	for _, p := range []string{"/", "/app.js", "/app.css", "/icon.svg"} {
		if code, _ := r.do("GET", p, "", nil); code != 200 {
			t.Errorf("%s: %d", p, code)
		}
	}
	if code, _ := r.do("GET", "/../server.go", "", nil); code == 200 {
		t.Error("source file served")
	}
}

func TestSignInResultAndOneAtATime(t *testing.T) {
	r := newRig(t)
	r.login()
	r.spawn.release = make(chan struct{})
	r.spawn.result = `{"ok":false,"code":"duplicate","account":"work","email":"me@example.com","duplicate_of":"personal"}`
	if code, _ := r.do("POST", "/api/accounts/add", `{"name":"work","email":"-x@y.z"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("bad email accepted: %d", code)
	}
	if code, _ := r.do("POST", "/api/accounts/add", `{"name":"work","email":"me@example.com"}`, nil); code != http.StatusAccepted {
		t.Fatalf("add: %d", code)
	}
	if got := r.spawn.console[0]; !slices.Contains(got, "--email") || got[len(got)-1] != "me@example.com" {
		t.Fatalf("args %q", got)
	}
	// A second sign-in while the first window is open is refused.
	if code, body := r.do("POST", "/api/accounts/add", `{"name":"other"}`, nil); code != http.StatusConflict || !strings.Contains(body, "signin_open") {
		t.Fatalf("second sign-in: %d %s", code, body)
	}
	close(r.spawn.release)
	var st stateView
	for i := 0; i < 50; i++ {
		_, body := r.do("GET", "/api/state", "", nil)
		_ = json.Unmarshal([]byte(body), &st)
		if len(st.Jobs) == 1 && !st.Jobs[0].Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(st.Jobs) != 1 || st.Jobs[0].Result == nil || st.Jobs[0].Result.Code != "duplicate" || st.Jobs[0].Result.DuplicateOf != "personal" {
		t.Fatalf("job result: %+v", st.Jobs)
	}
}

func TestSignInPrivateOpensOnlyClaudeAddresses(t *testing.T) {
	r := newRig(t)
	var opened []string
	clip := ""
	r.srv.opt.Clipboard = func() string { return clip }
	r.srv.opt.OpenPrivate = func(u string) error { opened = append(opened, u); return nil }
	r.login()
	for _, bad := range []string{"", "hello", "https://evil.example/oauth/authorize", "http://claude.ai/oauth/authorize", "https://claude.ai/settings"} {
		clip = bad
		if code, _ := r.do("POST", "/api/signin/private", "{}", nil); code != http.StatusBadRequest {
			t.Errorf("%q accepted: %d", bad, code)
		}
	}
	// Copied from a console: split over lines, with spaces.
	clip = "  https://claude.ai/oauth/authorize?code=true&client_id=abc\r\n  &state=xyz "
	if code, _ := r.do("POST", "/api/signin/private", "{}", nil); code != 200 {
		t.Fatalf("good address: %d", code)
	}
	if len(opened) != 1 || opened[0] != "https://claude.ai/oauth/authorize?code=true&client_id=abc&state=xyz" {
		t.Fatalf("opened %q", opened)
	}
}

func TestFolderTrustAndStop(t *testing.T) {
	r := newRig(t)
	r.login()
	prof := filepath.Join(r.paths.ProfilesDir(), "a")
	_ = os.MkdirAll(prof, 0o700)
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		return reg.Add(accounts.Account{ID: "a", ProfileDir: prof, Enabled: true, LoggedIn: true})
	})
	proj := t.TempDir()
	info := func() map[string]any {
		_, body := r.do("GET", "/api/folder/info?path="+url.QueryEscape(proj), "", nil)
		var m map[string]any
		_ = json.Unmarshal([]byte(body), &m)
		return m
	}
	if m := info(); m["ok"] != true || m["trusted"] != false {
		t.Fatalf("before: %v", m)
	}
	// Starting with the user's yes seeds onboarding and the folder's trust.
	if code, _ := r.do("POST", "/api/sessions/start", `{"folder":`+strconv.Quote(proj)+`,"trust":true}`, nil); code != 202 {
		t.Fatalf("start: %d", code)
	}
	if m := info(); m["trusted"] != true {
		t.Fatalf("after: %v", m)
	}
	if code, _ := r.do("POST", "/api/sessions/stop", `{"run_id":"20261028-101500-abc123"}`, nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	if last := r.spawn.hidden[len(r.spawn.hidden)-1]; !slices.Equal(last, []string{"stop", "--run", "20261028-101500-abc123"}) {
		t.Fatalf("stop args %q", last)
	}
}

func TestConversationsNameNoPaths(t *testing.T) {
	r := newRig(t)
	r.login()
	proj := t.TempDir()
	home := os.Getenv("CLAUDE_CONFIG_DIR")
	tr := filepath.Join(home, "projects", "p", "11111111-2222-4333-8444-555555555555.jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	row := `{"type":"user","cwd":` + strconv.Quote(proj) + `,"message":{"role":"user","content":"<b>hi</b> there"}}` + "\n"
	_ = os.WriteFile(tr, []byte(row), 0o600)
	_, body := r.do("GET", "/api/conversations?folder="+url.QueryEscape(proj), "", nil)
	if strings.Contains(body, "jsonl") || strings.Contains(body, filepath.ToSlash(home)) || !strings.Contains(body, `"where":"claude-code"`) {
		t.Fatalf("list: %s", body)
	}
}

func TestStartNeedsASwitchMode(t *testing.T) {
	r := newRig(t)
	r.login()
	_, _ = config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.SwitchMode, c.RiskAcknowledged = "", 0
		return nil
	})
	proj := t.TempDir()
	code, body := r.do("POST", "/api/sessions/start", `{"folder":`+strconv.Quote(proj)+`}`, nil)
	if code != http.StatusConflict || !strings.Contains(body, "needs_mode") || len(r.spawn.console) != 0 {
		t.Fatalf("start without a mode: %d %s (spawned %q)", code, body, r.spawn.console)
	}
	// "off" needs no acknowledgement.
	_, _ = config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.SwitchMode = config.ModeOff
		return nil
	})
	if code, body := r.do("POST", "/api/sessions/start", `{"folder":`+strconv.Quote(proj)+`}`, nil); code != http.StatusAccepted {
		t.Fatalf("start with mode off: %d %s", code, body)
	}
}

func TestAddAgainAfterACancelledSignIn(t *testing.T) {
	r := newRig(t)
	r.login()
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		if err := reg.Add(accounts.Account{ID: "personal", ProfileDir: filepath.Join(r.paths.ProfilesDir(), "personal"), Enabled: true}); err != nil {
			return err
		}
		return reg.Add(accounts.Account{ID: "work", ProfileDir: filepath.Join(r.paths.ProfilesDir(), "work"), Enabled: true, LoggedIn: true})
	})
	// Never signed in: adding it again carries on with its sign-in.
	if code, body := r.do("POST", "/api/accounts/add", `{"name":"personal"}`, nil); code != http.StatusAccepted {
		t.Fatalf("add again: %d %s", code, body)
	}
	if got := r.spawn.console[0]; got[0] != "login" || got[1] != "personal" {
		t.Fatalf("args %q", got)
	}
	waitJobsDone(r)
	// A signed-in account of that name is a real conflict.
	if code, body := r.do("POST", "/api/accounts/add", `{"name":"work"}`, nil); code != http.StatusConflict || !strings.Contains(body, `"exists"`) {
		t.Fatalf("add over a signed-in account: %d %s", code, body)
	}
}

func waitJobsDone(r *rig) {
	r.t.Helper()
	for i := 0; i < 100; i++ {
		if !r.srv.anySigningIn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatal("sign-in job did not finish")
}

// A session whose supervisor runs: the window refuses to uninstall before it
// closes itself, as `baton uninstall` would after.
func TestUninstallRefusedWhileASessionRuns(t *testing.T) {
	r := newRig(t)
	called := false
	r.srv.opt.Uninstall = func() error { called = true; return nil }
	r.login()
	dir := r.paths.Run("20261028-101500-abc123")
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: "20261028-101500-abc123", SupervisorPID: os.Getpid(),
		SupervisorCreated: winproc.ProcessCreated(os.Getpid()), State: run.StateRunning, Account: "a", Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != http.StatusConflict || !strings.Contains(body, "sessions_running") {
		t.Fatalf("uninstall: %d %s", code, body)
	}
	time.Sleep(400 * time.Millisecond)
	if called {
		t.Fatal("the window closed for an uninstall that cannot run")
	}
}

// A session whose window was closed never wrote "done", yet it ended: it
// shows up under Recent sessions.
func TestRecentSessionsIncludeClosedWindows(t *testing.T) {
	r := newRig(t)
	r.login()
	prof := filepath.Join(r.paths.ProfilesDir(), "a")
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		return reg.Add(accounts.Account{ID: "a", ProfileDir: prof, Enabled: true, LoggedIn: true})
	})
	proj := t.TempDir()
	const sid = "11111111-2222-4333-8444-555555555555"
	tr := filepath.Join(prof, "projects", "x", sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	_ = os.WriteFile(tr, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"hello"}}`+"\n"), 0o600)
	dir := r.paths.Run("20261028-101500-abc123")
	_ = os.MkdirAll(dir, 0o700)
	_ = run.SaveJournal(dir, run.Journal{RunID: "20261028-101500-abc123", SupervisorPID: 999999, SupervisorCreated: time.Now().Add(-time.Hour),
		State: run.StateRunning, Account: "a", SessionID: sid, TranscriptPath: tr, Cwd: proj})
	_, body := r.do("GET", "/api/state", "", nil)
	var st stateView
	_ = json.Unmarshal([]byte(body), &st)
	if len(st.Runs) != 0 || len(st.Recent) != 1 || st.Recent[0].SessionID != sid || st.Recent[0].Where != "a" {
		t.Fatalf("runs %+v recent %+v", st.Runs, st.Recent)
	}
}

// A check asked for while one runs (a sign-in just finished) runs again
// afterwards, so the sign-in is not overwritten by the older check's view.
func TestCheckRunsAgainWhenAskedDuringACheck(t *testing.T) {
	r := newRig(t)
	gate := make(chan struct{})
	r.spawn.hiddenGate = gate
	r.srv.StartCheck()
	time.Sleep(50 * time.Millisecond)
	r.srv.StartCheck()
	r.srv.StartCheck()
	close(gate)
	for i := 0; i < 100; i++ {
		r.srv.mu.Lock()
		busy := r.srv.checking
		r.srv.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.spawn.mu.Lock()
	defer r.spawn.mu.Unlock()
	if len(r.spawn.hidden) != 2 {
		t.Fatalf("doctor runs: %q", r.spawn.hidden)
	}
}

// The uninstall waits for every window this GUI opened, a session window
// that is waiting for Enter included.
func TestUninstallRefusedWhileAWindowIsOpen(t *testing.T) {
	r := newRig(t)
	called := false
	r.srv.opt.Uninstall = func() error { called = true; return nil }
	r.login()
	r.spawn.release = make(chan struct{})
	proj := t.TempDir()
	if code, body := r.do("POST", "/api/sessions/start", `{"folder":`+strconv.Quote(proj)+`}`, nil); code != http.StatusAccepted {
		t.Fatalf("start: %d %s", code, body)
	}
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != http.StatusConflict || !strings.Contains(body, "windows_open") {
		t.Fatalf("uninstall with a window open: %d %s", code, body)
	}
	// Twenty later windows that close at once do not push the open one out
	// of the list.
	release := r.spawn.release
	r.spawn.mu.Lock()
	r.spawn.release = nil
	r.spawn.mu.Unlock()
	for i := 0; i < 20; i++ {
		_ = r.srv.consoleJob("run", proj, proj, []string{"run"}, "")
	}
	time.Sleep(200 * time.Millisecond)
	r.srv.mu.Lock()
	first := r.srv.jobs[0]
	r.srv.mu.Unlock()
	if first.id != 1 || !first.running() {
		t.Fatalf("the open window was dropped from the list (first job %d running=%v)", first.id, first.running())
	}
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != http.StatusConflict || !strings.Contains(body, "windows_open") {
		t.Fatalf("uninstall with the window still open: %d %s", code, body)
	}
	r.spawn.release = release
	close(r.spawn.release)
	time.Sleep(200 * time.Millisecond)
	if code, body := r.do("POST", "/api/uninstall", "{}", nil); code != 200 {
		t.Fatalf("uninstall after the windows closed: %d %s", code, body)
	}
	time.Sleep(400 * time.Millisecond)
	if !called {
		t.Fatal("uninstall did not start")
	}
}

// With equal priorities baton picks by when an account was last limited:
// the window's choice for a busy account must follow baton's own.
func TestIdleStartFollowsBatonsPick(t *testing.T) {
	r := newRig(t)
	busyDir := filepath.Join(r.paths.ProfilesDir(), "work")
	idleDir := filepath.Join(r.paths.ProfilesDir(), "personal")
	_ = os.MkdirAll(filepath.Join(busyDir, "sessions"), 0o700)
	_ = os.MkdirAll(idleDir, 0o700)
	// A live claude on "work": a session file holding this test's own pid.
	pid := os.Getpid()
	_ = os.WriteFile(filepath.Join(busyDir, "sessions", strconv.Itoa(pid)+".json"),
		[]byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"x","cwd":"C:\x","startedAt":`+strconv.FormatInt(winproc.ProcessCreated(pid).UnixMilli(), 10)+`}`), 0o600)
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		if err := reg.Add(accounts.Account{ID: "personal", ProfileDir: idleDir, Enabled: true, LoggedIn: true, Priority: 50}); err != nil {
			return err
		}
		return reg.Add(accounts.Account{ID: "work", ProfileDir: busyDir, Enabled: true, LoggedIn: true, Priority: 50})
	})
	if !claude.BusyProfile(busyDir) {
		t.Skip("cannot fake a running claude here")
	}
	// "personal" was limited yesterday; "work" never: baton picks "work".
	_ = ledger.Update(r.paths.State(), r.paths.StateLock(), func(st *ledger.State) error {
		a := st.Account("personal")
		a.Status, a.LimitedAt, a.RetryAt = ledger.StatusExhausted, time.Now().Add(-24*time.Hour), time.Now().Add(-23*time.Hour)
		return nil
	})
	reg, _ := accounts.Load(r.paths.Accounts())
	if got := r.srv.idleStart(reg, t.TempDir()); got != "personal" {
		t.Fatalf("idleStart = %q, want personal (baton would pick the busy work)", got)
	}
}

// Bringing in a conversation: the list covers every folder and every place
// conversations are kept, says which are open elsewhere or whose folder is
// gone, and an open one can only continue in a copy.
func TestImportListsEveryFolderAndCopiesOpenOnes(t *testing.T) {
	r := newRig(t)
	r.login()
	prof := filepath.Join(r.paths.ProfilesDir(), "a")
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		return reg.Add(accounts.Account{ID: "a", ProfileDir: prof, Enabled: true, LoggedIn: true})
	})
	home := os.Getenv("CLAUDE_CONFIG_DIR")
	desk, other := t.TempDir(), t.TempDir()
	gone := filepath.Join(t.TempDir(), "deleted")
	const open, onAcct, lost = "11111111-2222-4333-8444-555555555551", "11111111-2222-4333-8444-555555555552", "11111111-2222-4333-8444-555555555553"
	write := func(cfg, sid, cwd, extra string, age time.Duration) string {
		p := filepath.Join(cfg, "projects", "p", sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(`{"type":"user","entrypoint":"claude-desktop","cwd":`+strconv.Quote(cwd)+`,"message":{"role":"user","content":"hi"}}`+"\n"+extra), 0o600)
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
		return p
	}
	tr := write(home, open, desk, `{"type":"custom-title","customTitle":"desk work"}`+"\n", 0)
	write(prof, onAcct, other, "", time.Hour)
	write(home, lost, gone, "", 2*time.Hour)
	// The desktop app has the first one open: a live claude holds it.
	pid := os.Getpid()
	_ = os.MkdirAll(filepath.Join(home, "sessions"), 0o700)
	_ = os.WriteFile(filepath.Join(home, "sessions", strconv.Itoa(pid)+".json"),
		[]byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+open+`","startedAt":`+strconv.FormatInt(winproc.ProcessCreated(pid).UnixMilli(), 10)+`}`), 0o600)
	held := claude.BusyProfile(home)

	_, body := r.do("GET", "/api/conversations?all=1", "", nil)
	var list []conversationView
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) != 3 || strings.Contains(body, "jsonl") {
		t.Fatalf("list: %v %s", err, body)
	}
	c := list[0]
	if c.SessionID != open || c.Title != "desk work" || c.Source != "claude-desktop" || c.Where != "claude-code" ||
		!strings.EqualFold(c.Cwd, desk) || !c.FolderOK || c.Open != held {
		t.Fatalf("first: %+v", c)
	}
	if list[1].SessionID != onAcct || list[1].Where != "a" || list[1].Open || list[2].SessionID != lost || list[2].FolderOK ||
		list[2].FolderProblem != "gone" || list[0].FolderProblem != "" {
		t.Fatalf("others: %+v", list[1:])
	}

	start := func(v map[string]any) (int, string) {
		b, _ := json.Marshal(v)
		return r.do("POST", "/api/sessions/start", string(b), nil)
	}
	if held {
		if code, body := start(map[string]any{"folder": desk, "mode": "resume", "session_id": open, "where": "claude-code"}); code != 400 || !strings.Contains(body, "open_elsewhere") {
			t.Fatalf("an open conversation continued as it is: %d %s", code, body)
		}
	}
	n := len(r.spawn.console)
	for _, mode := range []string{"resume", "continue"} {
		if code, body := start(map[string]any{"folder": desk, "mode": mode, "session_id": open, "where": "claude-code", "copy": true}); code != 202 {
			t.Fatalf("%s in a copy: %d %s", mode, code, body)
		}
		if got := r.spawn.console[n]; !slices.Equal(got, []string{"run", "--", "--resume", tr, "--fork-session"}) || !strings.EqualFold(r.spawn.dirs[n], desk) {
			t.Fatalf("%s in a copy: %q in %s", mode, got, r.spawn.dirs[n])
		}
		n++
	}
	if code, _ := start(map[string]any{"folder": desk, "copy": true}); code != 202 || !slices.Equal(r.spawn.console[n], []string{"run"}) {
		t.Fatalf("a new conversation is never a copy: %q", r.spawn.console[n])
	}
}
