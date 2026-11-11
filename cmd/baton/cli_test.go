package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/winproc"
)

// These tests run the real baton.exe against test/fakeclaude, so account
// management (which calls `claude auth ...`) is exercised without a real
// account or a browser.

var batonExe, fakeClaude, notesFile string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "baton-cli-test")
	if err != nil {
		panic(err)
	}
	// Notifications from the sessions these tests run go to a file, not the
	// screen, and nothing is registered with Windows for them.
	notesFile = filepath.Join(dir, "notifications.jsonl")
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	batonExe = filepath.Join(dir, "baton.exe")
	fakeClaude = filepath.Join(dir, "claude.exe")
	for _, b := range [][2]string{{batonExe, "."}, {fakeClaude, "../../test/fakeclaude"}} {
		if out, err := exec.Command(gobin, "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			panic(string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type cli struct {
	t    *testing.T
	home string
}

func newCLI(t *testing.T) *cli {
	home := t.TempDir()
	cfg := map[string]any{"claude_path": fakeClaude, "share_user_config": false}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(home, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return &cli{t: t, home: home}
}

func (c *cli) run(extraEnv []string, args ...string) (string, int) {
	c.t.Helper()
	cmd := exec.Command(batonExe, args...)
	// The Claude desktop app's data folder is never the real one in a test:
	// by default there is none (a test can name one in extraEnv).
	env := []string{"BATON_HOME=" + c.home, "BATON_NOTIFY_FILE=" + notesFile,
		"BATON_DESKTOP_DATA=" + filepath.Join(c.home, "no-desktop-app"), "BATON_DESKTOP_RUNNING=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "BATON_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		c.t.Fatal(err)
	}
	return string(out), code
}

func (c *cli) registry() *accounts.Registry {
	r, err := accounts.Load(filepath.Join(c.home, "accounts.json"))
	if err != nil {
		c.t.Fatal(err)
	}
	return &r
}

func TestAddSignsInAndDetectsDuplicates(t *testing.T) {
	c := newCLI(t)
	out, code := c.run(nil, "add", "personal")
	if code != 0 || !strings.Contains(out, `"personal" signed in: personal@example.com`) {
		t.Fatalf("add personal: code %d\n%s", code, out)
	}
	// The browser was still signed in as personal when "work" was added.
	out, code = c.run([]string{"FAKE_EMAIL=personal@example.com"}, "add", "work")
	if code == 0 || !strings.Contains(out, "same Claude account") {
		t.Fatalf("duplicate not reported: code %d\n%s", code, out)
	}
	r := c.registry()
	if p, w := r.Get("personal"), r.Get("work"); !p.Enabled || !p.LoggedIn || w.Enabled {
		t.Fatalf("personal must stay enabled, work disabled: %+v / %+v", p, w)
	}
	// Signing personal in again (same identity) must not disable it.
	if out, code := c.run(nil, "login", "personal"); code != 0 || !c.registry().Get("personal").Enabled {
		t.Fatalf("re-login disabled personal: code %d\n%s", code, out)
	}
	// Enabling the duplicate as it is, or signing it in as the same account
	// again, must not put two copies of one account into rotation.
	if out, code := c.run(nil, "enable", "work"); code == 0 || c.registry().Get("work").Enabled {
		t.Fatalf("enable of a duplicate accepted: %d\n%s", code, out)
	}
	if out, code := c.run([]string{"FAKE_EMAIL=personal@example.com"}, "login", "work"); code == 0 || c.registry().Get("work").Enabled {
		t.Fatalf("re-login of a duplicate as the same account accepted: %d\n%s", code, out)
	}
	// Fixing "work" with the right account re-enables it.
	if out, code := c.run([]string{"FAKE_EMAIL=work@example.com"}, "login", "work"); code != 0 || !c.registry().Get("work").Enabled {
		t.Fatalf("login work as itself: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "list"); code != 0 || !strings.Contains(out, "work@example.com") {
		t.Fatalf("list: %d\n%s", code, out)
	}
}

func TestAddNoLoginMarksNeedsLogin(t *testing.T) {
	c := newCLI(t)
	if out, code := c.run(nil, "add", "x", "--no-login"); code != 0 {
		t.Fatalf("add --no-login: %d\n%s", code, out)
	}
	st, _ := ledger.Load(filepath.Join(c.home, "state.json"))
	if st.StatusOf("x").Status != ledger.StatusNeedsLogin {
		t.Fatalf("status = %s", st.StatusOf("x").Status)
	}
	if out, code := c.run(nil, "login", "x"); code != 0 {
		t.Fatalf("login: %d\n%s", code, out)
	}
	st, _ = ledger.Load(filepath.Join(c.home, "state.json"))
	if st.StatusOf("x").Status != ledger.StatusOK {
		t.Fatalf("login did not clear needs_login: %s", st.StatusOf("x").Status)
	}
}

func TestPrivateSignInHandsThePageToBaton(t *testing.T) {
	c := newCLI(t)
	// Claude Code opens its sign-in page with BROWSER: with --private that is
	// baton.exe, which opens only Claude sign-in pages (so nothing opens here).
	refused := "not a Claude sign-in address"
	browse := []string{"FAKE_BROWSER_URL=https://example.com/oauth/authorize", "BROWSER="}
	out, code := c.run(browse, "add", "personal", "--private")
	if code != 0 || !strings.Contains(out, refused) || !strings.Contains(out, "InPrivate window will open") {
		t.Fatalf("add --private: code %d\n%s", code, out)
	}
	if out, code := c.run(browse, "login", "personal"); code != 0 || strings.Contains(out, refused) {
		t.Fatalf("login without --private used baton as the browser: code %d\n%s", code, out)
	}
	// Run as a browser outside a sign-in, baton is just baton.
	if out, code := c.run(nil, "https://claude.ai/oauth/authorize"); code == 0 || strings.Contains(out, refused) {
		t.Fatalf("an address without the sign-in's variable: code %d\n%s", code, out)
	}
}

func TestBlockedIsClearedByEnable(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	_ = ledger.Update(filepath.Join(c.home, "state.json"), filepath.Join(c.home, "state.lock"), func(s *ledger.State) error {
		s.Account("a").Status = ledger.StatusBlocked
		return nil
	})
	if out, code := c.run(nil, "enable", "a"); code != 0 || !strings.Contains(out, "Cleared the blocked mark") {
		t.Fatalf("enable: %d\n%s", code, out)
	}
}

func TestRefusedInsideSession(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	inside := []string{"BATON_RUN=r1"}
	for _, args := range [][]string{{"switch", "a"}, {"config", "switch_mode", "auto"}, {"add", "b"},
		{"remove", "a"}, {"login", "a"}, {"sync"}, {"run"}, {"stop"}, {"uninstall", "--yes"}, {"gui", "--browser"},
		{"rules", "set", c.home, "a"}, {"rules", "remove", c.home}} {
		if out, code := c.run(inside, args...); code == 0 || !strings.Contains(out, "inside a baton session") {
			t.Errorf("%v from inside a session: code %d\n%s", args, code, out)
		}
	}
}

func TestSwitchModeNeedsAcknowledgement(t *testing.T) {
	c := newCLI(t)
	// Not a console: the notice cannot be acknowledged, so auto is refused...
	if out, code := c.run(nil, "config", "switch_mode", "auto"); code == 0 || !strings.Contains(out, "Usage Policy") {
		t.Fatalf("auto without acknowledgement: %d\n%s", code, out)
	}
	// ...but turning switching off needs no acknowledgement.
	if out, code := c.run(nil, "config", "switch_mode", "off"); code != 0 {
		t.Fatalf("off: %d\n%s", code, out)
	}
}

func TestRemoveRefusals(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	// A profile_dir outside baton's folder is never deleted.
	outside := t.TempDir()
	_, _ = accounts.Update(filepath.Join(c.home, "accounts.json"), filepath.Join(c.home, "accounts.lock"), func(r *accounts.Registry) error {
		r.Get("a").ProfileDir = outside
		return nil
	})
	if out, code := c.run(nil, "remove", "a", "--force"); code == 0 || !strings.Contains(out, "will not delete") {
		t.Fatalf("remove outside: %d\n%s", code, out)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside folder was touched")
	}
}

func TestRemoveWithoutConsoleSaysWhyAndYesWorks(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	if out, code := c.run(nil, "remove", "a"); code == 0 || !strings.Contains(out, "not a console") {
		t.Fatalf("remove without a console must explain: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "remove", "a", "--yes"); code != 0 || c.registry().Get("a") != nil {
		t.Fatalf("remove --yes: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(c.home, "profiles", "a")); !os.IsNotExist(err) {
		t.Fatalf("profile folder left behind: %v", err)
	}
}

func TestSwitchWithoutRunAndBadTargets(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	if out, code := c.run(nil, "switch"); code == 0 || !strings.Contains(out, "no running baton session") {
		t.Fatalf("switch: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "switch", "nope"); code == 0 {
		t.Fatalf("switch to unknown accepted: %s", out)
	}
}

// A hook must read its payload from stdin even when baton was reopened in a
// new console window (BATON_NEW_CONSOLE must not reach it, and must not make
// it read the console).
func TestHookReadsPayloadInNewConsoleMode(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(batonExe, "hook", "StopFailure", "--run-dir", dir)
	cmd.Env = append(os.Environ(), "BATON_NEW_CONSOLE=1", "CLAUDE_PID=42")
	cmd.Stdin = strings.NewReader(`{"session_id":"s","error":"rate_limit"}`)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("hook hung")
	}
	evs := run.NewTail(dir).Next()
	if len(evs) != 1 || evs[0].Decode().Error != "rate_limit" || evs[0].ClaudePID != 42 {
		t.Fatalf("events = %+v", evs)
	}
}

func TestRound4CLI(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "work")
	// `baton run work` must not send "work" to the model as a prompt.
	if out, code := c.run(nil, "run", "work"); code != 2 || !strings.Contains(out, "--account work") {
		t.Errorf("run <name>: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "add", "--help"); code != 0 || strings.Contains(out, "help requested") {
		t.Errorf("add --help: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "config", "switch_mode", "automatic"); code == 0 || strings.Contains(out, "Usage Policy") {
		t.Errorf("an invalid mode must be rejected before the notice: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "config", "claude_path", `C:\nope\claude.exe`); code == 0 || !strings.Contains(out, "not a file") {
		t.Errorf("a missing claude_path was accepted: %d %s", code, out)
	}
	if out, code := c.run(nil, "list"); code != 0 {
		t.Errorf("list: %d %s", code, out)
	}
	if out, code := c.run(nil, "config", "claude_path", "auto"); code != 0 {
		t.Errorf("claude_path auto: %d %s", code, out)
	}
	if out, _ := c.run(nil, "config", "claude_path"); strings.TrimSpace(out) != "" {
		t.Errorf("claude_path not cleared: %q", out)
	}
	if out, code := c.run(nil, "add", "nul"); code == 0 || !strings.Contains(out, "reserved") {
		t.Errorf("add nul: %d\n%s", code, out)
	}
	c.run(nil, "config", "claude_path", fakeClaude)
	// A user-disabled account stays disabled after a later login.
	c.run(nil, "disable", "work")
	if out, code := c.run(nil, "login", "work"); code != 0 || c.registry().Get("work").Enabled {
		t.Errorf("login re-enabled a user-disabled account: %d\n%s", code, out)
	}
}

func TestVersion(t *testing.T) {
	c := newCLI(t)
	if out, code := c.run(nil, "version"); code != 0 || !strings.HasPrefix(out, "baton ") {
		t.Fatalf("version: %d %q", code, out)
	}
}

func (c *cli) state() ledger.State {
	c.t.Helper()
	st, err := ledger.Load(filepath.Join(c.home, "state.json"))
	if err != nil {
		c.t.Fatal(err)
	}
	return st
}

func (c *cli) setState(id string, s ledger.AccountState) {
	c.t.Helper()
	if err := ledger.Update(filepath.Join(c.home, "state.json"), filepath.Join(c.home, "state.lock"), func(st *ledger.State) error {
		*st.Account(id) = s
		return nil
	}); err != nil {
		c.t.Fatal(err)
	}
}

func TestRound5CLI(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "personal")
	c.run(nil, "add", "work")

	// A failed sign-in keeps the old login, so it must not clear a blocked mark.
	c.setState("personal", ledger.AccountState{Status: ledger.StatusBlocked})
	if out, code := c.run([]string{"FAKE_LOGIN_FAIL=1"}, "login", "personal"); code == 0 || !strings.Contains(out, "did not finish") ||
		strings.Contains(out, "signed in:") {
		t.Errorf("failed login reported as success: %d\n%s", code, out)
	}
	if s := c.state().StatusOf("personal").Status; s != ledger.StatusBlocked {
		t.Errorf("failed login cleared the blocked mark: %s", s)
	}

	// Clearing needs_login keeps a usage limit that has not reset yet.
	retry := time.Now().Add(20 * time.Hour)
	c.setState("personal", ledger.AccountState{Status: ledger.StatusNeedsLogin, RetryAt: retry})
	if out, code := c.run(nil, "login", "personal"); code != 0 {
		t.Fatalf("login: %d\n%s", code, out)
	}
	if s := c.state().StatusOf("personal"); s.Status != ledger.StatusExhausted || !s.RetryAt.Equal(retry) {
		t.Errorf("limit lost on login: %+v", s)
	}

	// A user-disabled account signed in as a duplicate, then correctly, stays
	// disabled: only accounts baton disabled itself come back.
	c.run(nil, "disable", "work")
	if out, code := c.run([]string{"FAKE_EMAIL=personal@example.com"}, "login", "work"); code == 0 {
		t.Errorf("duplicate login accepted: %s", out)
	}
	if out, code := c.run(nil, "login", "work"); code != 0 || c.registry().Get("work").Enabled || !strings.Contains(out, "still disabled") {
		t.Errorf("user-disabled account re-enabled: %d enabled=%v\n%s", code, c.registry().Get("work").Enabled, out)
	}

	// claude_path is stored as a full path.
	cwd, _ := os.Getwd()
	rel, err := filepath.Rel(cwd, fakeClaude)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(nil, "config", "claude_path", rel); code != 0 {
		t.Errorf("relative claude_path: %d %s", code, out)
	}
	if out, _ := c.run(nil, "config", "claude_path"); !filepath.IsAbs(strings.TrimSpace(out)) {
		t.Errorf("claude_path stored relative: %q", out)
	}
	c.run(nil, "config", "claude_path", fakeClaude)

	// --account after -- is baton's option in the wrong place, not claude's.
	if out, code := c.run(nil, "run", "--", "--account", "work"); code != 2 || !strings.Contains(out, "baton option") {
		t.Errorf("run -- --account: %d\n%s", code, out)
	}
	if out, _ := c.run(nil, "add", "--help"); !strings.Contains(out, "--priority") || !strings.Contains(out, "usage: baton add") {
		t.Errorf("add --help: %s", out)
	}

	// An unreadable ledger is reported, not shown as "everything is ok".
	if err := os.WriteFile(filepath.Join(c.home, "state.json"), []byte("{,"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := c.run(nil, "list"); !strings.Contains(out, "unreadable") {
		t.Errorf("list with a broken state.json: %s", out)
	}
	if out, code := c.run(nil, "switch", "personal"); code == 0 || !strings.Contains(out, "unreadable") {
		t.Errorf("switch with a broken state.json: %d %s", code, out)
	}
}

// A conversation from before baton lives in the normal ~/.claude; -r <id>
// must find it there too.
func TestResolveResumeLooksInUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	p := filepath.Join(home, "projects", "C--proj", "abc.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(`{"sessionId":"abc","cwd":"C:\proj"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &env{paths: app.Paths{Root: t.TempDir()}}
	a, err := claude.SplitArgs([]string{"-r", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	resolveResume(&a, e, `C:\proj`)
	if a.ResumeArg != p {
		t.Fatalf("resume arg = %q, want %q", a.ResumeArg, p)
	}
}

func TestRound6CLI(t *testing.T) {
	c := newCLI(t)
	if out, code := c.run(nil, "enable", "--help"); code != 0 || !strings.Contains(out, "usage: baton enable <name>") {
		t.Errorf("enable --help: %d %s", code, out)
	}
	c.run(nil, "config", "claude_path", "")
	if out, _ := c.run(nil, "config", "claude_path"); strings.TrimSpace(out) != "" {
		t.Errorf(`claude_path "" did not reset: %q`, out)
	}
	c.run(nil, "config", "claude_path", fakeClaude)
	c.run(nil, "add", "personal")
	c.run([]string{"FAKE_EMAIL=personal@example.com"}, "add", "work") // disabled as a duplicate
	if out, _ := c.run(nil, "doctor"); strings.Contains(out, "same Claude account") {
		t.Errorf("doctor reports a duplicate that is already disabled:\n%s", out)
	}
}

func TestSignInResultFile(t *testing.T) {
	c := newCLI(t)
	res := filepath.Join(t.TempDir(), "r.json")
	read := func() map[string]any {
		var m map[string]any
		b, err := os.ReadFile(res)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(b, &m)
		return m
	}
	if out, code := c.run(nil, "add", "personal", "--result-file", res, "--email", "personal@example.com"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	if m := read(); m["ok"] != true || m["code"] != "ok" || m["email"] != "personal@example.com" || m["plan"] != "max" {
		t.Fatalf("result %v", m)
	}
	if _, code := c.run([]string{"FAKE_LOGIN_FAIL=1"}, "login", "personal", "--result-file", res); code == 0 {
		t.Fatal("failed login exited 0")
	}
	if m := read(); m["ok"] != false || m["code"] != "login_failed" {
		t.Fatalf("result %v", m)
	}
	c.run(nil, "add", "work")
	if _, code := c.run([]string{"FAKE_EMAIL=personal@example.com"}, "login", "work", "--result-file", res); code == 0 {
		t.Fatal("duplicate login exited 0")
	}
	if m := read(); m["code"] != "duplicate" || m["duplicate_of"] != "personal" {
		t.Fatalf("result %v", m)
	}
}

func TestStopAndReleaseFlags(t *testing.T) {
	c := newCLI(t)
	if out, code := c.run(nil, "stop"); code == 0 || !strings.Contains(out, "no running baton session") {
		t.Errorf("stop: %d\n%s", code, out)
	}
	// Release builds have no test switches for the window.
	if out, code := c.run(nil, "gui", "--no-window"); code == 0 || !strings.Contains(out, "not defined") {
		t.Errorf("gui --no-window: %d\n%s", code, out)
	}
}

// baton sessions lists the conversations of every folder, the desktop app's
// included; baton resume refuses what it cannot continue, goes on in a copy
// of one open elsewhere, and brings forward the baton session that runs one.
func TestSessionsAndResume(t *testing.T) {
	c := newCLI(t)
	home := t.TempDir()
	env := []string{"CLAUDE_CONFIG_DIR=" + home}
	proj := t.TempDir()
	const open, lost = "11111111-2222-4333-8444-555555555551", "11111111-2222-4333-8444-555555555552"
	write := func(sid, cwd, title string) {
		p := filepath.Join(home, "projects", "p", sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		row := `{"type":"user","entrypoint":"claude-desktop","cwd":` + strconv.Quote(cwd) + `,"message":{"role":"user","content":"hi"}}` + "\n" +
			`{"type":"custom-title","customTitle":` + strconv.Quote(title) + `}` + "\n"
		if err := os.WriteFile(p, []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(open, proj, "desk work")
	write(lost, filepath.Join(t.TempDir(), "deleted"), "old work")
	pid := os.Getpid()
	_ = os.MkdirAll(filepath.Join(home, "sessions"), 0o700)
	_ = os.WriteFile(filepath.Join(home, "sessions", strconv.Itoa(pid)+".json"),
		[]byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+open+`","startedAt":`+strconv.FormatInt(winproc.ProcessCreated(pid).UnixMilli(), 10)+`}`), 0o600)

	out, code := c.run(env, "sessions")
	if code != 0 || !strings.Contains(out, open) || !strings.Contains(out, "desk work") || !strings.Contains(out, proj) ||
		!strings.Contains(out, "Claude desktop app") || !strings.Contains(out, lost) || !strings.Contains(out, "it continues in a copy]") {
		t.Fatalf("sessions: %d %s", code, out)
	}
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"resume", "../x"}, 2, "session id"},
		{[]string{"resume"}, 2, "usage"},
		{[]string{"resume", "11111111-2222-4333-8444-000000000000"}, 1, "no conversation"},
		{[]string{"resume", lost, "--copy"}, 1, "is gone"},
		{[]string{"resume", lost}, 1, "is gone"},
	} {
		if out, code := c.run(env, tc.args...); code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("%q: %d %s", tc.args, code, out)
		}
	}
	// Open elsewhere: it goes on in a copy (decided here, so no session
	// window opens), in its own folder; ids are not case-sensitive.
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	e := envAt(c.home)
	for _, id := range []string{open, strings.ToUpper(open)} {
		plan, code, err := planResume(e, id, false)
		if err != nil || code != 0 || !plan.copied || plan.inBaton != nil || !strings.EqualFold(plan.cwd, proj) ||
			filepath.Base(plan.transcript) != open+".jsonl" {
			t.Errorf("%s: %+v %d %v", id, plan, code, err)
		}
	}
	// A baton session runs it: its window comes forward (it has none here)
	// and nothing starts.
	runID := "20261109-101500-abc123"
	dir := e.paths.Run(runID)
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: runID, SupervisorPID: pid, SupervisorCreated: winproc.ProcessCreated(pid),
		State: run.StateRunning, Account: "a", ChildPID: pid, SessionID: open}); err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(env, "resume", open); code != 0 || !strings.Contains(out, "already running in baton") || strings.Contains(out, "continuing in") {
		t.Errorf("resume of one baton runs: %d %s", code, out)
	}
	if out, _ := c.run(env, "sessions"); !strings.Contains(out, "[running in baton]") {
		t.Errorf("sessions: %s", out)
	}
	// Inside a session it refuses before saying where it would continue.
	if out, code := c.run(append(env, "BATON_RUN=x"), "resume", open, "--copy"); code != 1 || !strings.Contains(out, "inside a baton session") ||
		strings.Contains(out, "continuing in") {
		t.Errorf("resume inside a session: %d %s", code, out)
	}
}

// A session moving to a copy of its conversation (it was opened elsewhere
// too) is not switching accounts: `baton switch` and `baton status` say what
// it is doing.
func TestSwitchWhileMovingToACopy(t *testing.T) {
	c := newCLI(t)
	pid := os.Getpid()
	runID := "20261109-101500-abc123"
	dir := envAt(c.home).paths.Run(runID)
	_ = os.MkdirAll(dir, 0o700)
	for _, state := range []string{run.StateStopping, run.StateSwitching} {
		if err := run.SaveJournal(dir, run.Journal{RunID: runID, SupervisorPID: pid, SupervisorCreated: winproc.ProcessCreated(pid),
			State: state, Moving: true, Account: "a", Cwd: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		if out, code := c.run(nil, "switch"); code == 0 || !strings.Contains(out, "moving to a copy") || strings.Contains(out, "switching") {
			t.Errorf("%s: %d %s", state, code, out)
		}
		if out, _ := c.run(nil, "status"); !strings.Contains(out, "state=moving") {
			t.Errorf("status while %s: %s", state, out)
		}
	}
}

// envAt is the env of a baton data folder with no accounts.
func envAt(root string) *env { return &env{paths: app.Paths{Root: root}} }

// fakeInstalls is a PC of its own (home, AppData, PATH) holding copies of
// test/fakeclaude as Claude Code CLIs and as the desktop app's copies.
type fakeInstalls struct {
	t    *testing.T
	root string
	exe  []byte
	env  []string
}

func newFakeInstalls(t *testing.T) *fakeInstalls {
	t.Helper()
	exe, err := os.ReadFile(fakeClaude)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return &fakeInstalls{t: t, root: root, exe: exe, env: []string{"USERPROFILE=" + root, "APPDATA=" + filepath.Join(root, "roaming"),
		"LOCALAPPDATA=" + filepath.Join(root, "local"), "PATH=" + filepath.Join(root, "empty"), "SCOOP="}}
}

// setenv puts this PC's folders in the test's own environment too, for
// claudeInfo called here.
func (f *fakeInstalls) setenv() {
	for _, kv := range f.env {
		k, v, _ := strings.Cut(kv, "=")
		f.t.Setenv(k, v)
	}
}

func (f *fakeInstalls) place(p string) string {
	f.t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, f.exe, 0o700); err != nil {
		f.t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p, old, old)
	return p
}

// cli places a Claude Code CLI that says it is version v.
func (f *fakeInstalls) cli(p, v string) string {
	f.place(p)
	_ = os.WriteFile(p+".ver", []byte(v), 0o600)
	return p
}

func (f *fakeInstalls) native(v string) string {
	return f.cli(filepath.Join(f.root, ".local", "bin", "claude.exe"), v)
}

// desktop places the desktop app's copy of version v, finished as the app
// marks it.
func (f *fakeInstalls) desktop(v string) string {
	p := f.place(filepath.Join(f.root, "local", "Claude-Data", "claude_x", "claude-code", v, "abc123", "claude.exe"))
	_ = os.WriteFile(filepath.Join(filepath.Dir(p), ".payload"), []byte(`{"sha256":"abc123","size":`+strconv.Itoa(len(f.exe))+`}`), 0o600)
	_ = os.WriteFile(filepath.Join(filepath.Dir(p), ".verified"), []byte("abc123"), 0o600)
	return p
}

// A Claude Code CLI installed long ago is passed over for the desktop app's
// newer copy; doctor lists every copy with what to do about it, and the
// window gets the same list.
func TestOldCLIPassedOverForTheDesktopCopy(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "config", "claude_path", "auto")
	f := newFakeInstalls(t)
	cli := f.native("2.1.200")
	bundle := f.desktop("2.1.317")
	older := f.desktop("2.1.309")
	out, code := c.run(f.env, "doctor")
	for _, want := range []string{
		"claude:       2.1.317 (" + bundle + ", found via desktop)",
		"* 2.1.317   desktop  " + bundle + "  (in use)",
		"  2.1.309   desktop  " + older + "\n",
		"  2.1.200   native   " + cli + "  too old (needs " + claude.MinVersion + "+): run \"" + cli + "\" update",
		"newest version tested with this baton: " + claude.TestedWith,
	} {
		if !strings.Contains(strings.ReplaceAll(out, "\r\n", "\n"), want) {
			t.Errorf("doctor lacks %q", want)
		}
	}
	if code != 0 || t.Failed() {
		t.Fatalf("doctor: %d\n%s", code, out)
	}
	f.setenv()
	info := claudeInfo(filepath.Join(c.home, "config.json"))
	if !info.Found || info.Source != "desktop" || !strings.EqualFold(info.Path, bundle) || len(info.Installs) != 3 ||
		info.Auto != info.Path || info.Min != claude.MinVersion || info.Fallback != nil {
		t.Fatalf("window: %+v", info)
	}
	for _, i := range info.Installs {
		if i.InUse != strings.EqualFold(i.Path, bundle) || (i.Path == cli) != (i.Problem == claude.ProblemTooOld) {
			t.Errorf("install %+v", i)
		}
	}
	// The desktop copy turned off: only the old CLI is left.
	c.run(nil, "config", "allow_desktop_bundle", "false")
	info = claudeInfo(filepath.Join(c.home, "config.json"))
	if info.Found || !info.TooOld || !info.BundleOff || len(info.Installs) != 3 || info.Auto != "" {
		t.Fatalf("window without the desktop copy: %+v", info)
	}
	out, code = c.run(f.env, "doctor")
	if code == 0 || !strings.Contains(out, "PROBLEM: the only Claude Code baton can use is the Claude desktop app's own") {
		t.Fatalf("doctor without the desktop copy: %d\n%s", code, out)
	}
}

// A chosen Claude Code that is gone (an app update removed it) does not stop
// baton: another copy is used, with one line saying so, until it is back.
func TestChosenClaudeGoneFallsBack(t *testing.T) {
	c := newCLI(t)
	f := newFakeInstalls(t)
	pinned := f.cli(filepath.Join(f.root, "tools", "claude.exe"), "2.1.300")
	f.desktop("2.1.317")
	if out, code := c.run(f.env, "config", "claude_path", pinned); code != 0 {
		t.Fatalf("config: %d %s", code, out)
	}
	if out, code := c.run(f.env, "add", "personal"); code != 0 || strings.Contains(out, "not available") {
		t.Fatalf("add with the chosen copy: %d\n%s", code, out)
	}
	_ = os.Remove(pinned)
	out, code := c.run(f.env, "add", "work")
	want := "baton: the Claude Code you chose (" + pinned + ") is not available (it is no longer there); " +
		"using Claude Code 2.1.317 from the Claude desktop app for now"
	if code != 0 || strings.Count(out, want) != 1 || !strings.Contains(out, `"work" signed in`) {
		t.Fatalf("add without the chosen copy: %d\n%s", code, out)
	}
	out, _ = c.run(append(f.env, "BATON_LANG=ko"), "doctor")
	if !strings.Contains(out, "선택한 Claude Code("+pinned+")를 쓸 수 없어(더 이상 없음) 지금은 Claude 데스크톱 앱의 Claude Code 2.1.317 버전을 씁니다") {
		t.Fatalf("doctor in Korean:\n%s", out)
	}
	// Back, and newer than any version tested: used, with nothing to say.
	f.cli(pinned, "9.9.9")
	out, code = c.run(f.env, "add", "third")
	if code != 0 || strings.Contains(out, "not available") || strings.Contains(out, "tested") || strings.Contains(out, "warning") {
		t.Fatalf("back again: %d\n%s", code, out)
	}
	// claude_path "desktop" while the desktop app has no copy that works.
	_ = os.RemoveAll(filepath.Join(f.root, "local", "Claude-Data"))
	c.run(f.env, "config", "claude_path", "desktop")
	f.native("2.1.301")
	out, code = c.run(f.env, "login", "work")
	if code != 0 || !strings.Contains(out, "you chose (the Claude desktop app's) is not available "+
		"(the Claude desktop app has not set up its Claude Code yet (open its Code tab once)); using Claude Code 2.1.301 from the native installer") {
		t.Fatalf("desktop chosen, none there: %d\n%s", code, out)
	}
	// A path to a .cmd is refused when it is set.
	cmd := filepath.Join(f.root, "claude.cmd")
	_ = os.WriteFile(cmd, []byte("@echo off"), 0o600)
	if out, code := c.run(f.env, "config", "claude_path", cmd); code == 0 || !strings.Contains(out, "claude.exe") {
		t.Fatalf("a .cmd claude_path: %d %s", code, out)
	}
}

// The window's Update button runs a too-old CLI's own `claude update`, and
// only for such a copy.
func TestClaudeUpdateRunsTheCLIsOwnUpdate(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "config", "claude_path", "auto")
	f := newFakeInstalls(t)
	cli := f.native("2.1.200")
	_ = os.WriteFile(cli+".update", []byte("2.1.320"), 0o600)
	bundle := f.desktop("2.1.317")
	current := f.cli(filepath.Join(f.root, "local", "Microsoft", "WinGet", "Links", "claude.exe"), "2.1.300")
	for _, p := range []string{bundle, current, filepath.Join(f.root, "elsewhere", "claude.exe")} {
		if out, code := c.run(f.env, "claude-update", p); code == 0 || !strings.Contains(out, "not a Claude Code CLI that baton found too old") {
			t.Errorf("%s: %d %s", p, code, out)
		}
	}
	if out, code := c.run(append(f.env, "BATON_RUN=x"), "claude-update", cli); code == 0 || !strings.Contains(out, "inside a baton session") {
		t.Errorf("inside a session: %d %s", code, out)
	}
	out, code := c.run(f.env, "claude-update", cli)
	if code != 0 || !strings.Contains(out, "Successfully updated to version 2.1.320") || !strings.Contains(out, "Claude Code is now 2.1.320.") {
		t.Fatalf("update: %d\n%s", code, out)
	}
	// Updated in place (the same file): its version is not taken from before.
	if out, _ := c.run(f.env, "doctor"); !strings.Contains(out, "claude:       2.1.320 ("+cli) {
		t.Fatalf("after the update:\n%s", out)
	}

	// `claude update` that ends well and changes nothing (a copy a package
	// manager updates): the console window waits, saying what does update it.
	pkg := f.cli(filepath.Join(f.root, "local", "Microsoft", "WinGet", "Packages", "Anthropic.ClaudeCode_x", "claude.exe"), "2.1.210")
	out, code = c.run(f.env, "claude-update", pkg)
	if code == 0 || !strings.Contains(out, "Claude Code is up to date") || strings.Contains(out, "is now") ||
		!strings.Contains(out, "Claude Code is still 2.1.210, too old for baton (needs "+claude.MinVersion+" or newer). Update it with: winget upgrade Anthropic.ClaudeCode") {
		t.Fatalf("update that changed nothing: %d\n%s", code, out)
	}
	if out, _ := c.run(f.env, "doctor"); !strings.Contains(out, pkg+"  too old (needs "+claude.MinVersion+"+): run winget upgrade Anthropic.ClaudeCode") {
		t.Fatalf("doctor names WinGet's update:\n%s", out)
	}
	// The native installer's copy: installing it again.
	_ = os.Remove(cli + ".update")
	f.native("2.1.200") // a new file, so its version is asked again
	out, code = c.run(append(f.env, "BATON_LANG=ko"), "claude-update", cli)
	if code == 0 || !strings.Contains(out, `Claude Code가 아직 2.1.200 버전이라 baton에 쓰기에는 낮습니다 (`+claude.MinVersion+` 이상 필요). 다음으로 업데이트하세요: powershell -c "irm https://claude.ai/install.ps1 | iex"`) {
		t.Fatalf("native update that changed nothing: %d\n%s", code, out)
	}
}

// A Claude Code CLI too old for baton, and the Claude desktop app whose Code
// tab was never opened: baton says that opening it is enough.
func TestOldCLIPointsToTheDesktopApp(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "config", "claude_path", "auto")
	f := newFakeInstalls(t)
	cli := f.native("2.1.200")
	_ = os.MkdirAll(filepath.Join(f.root, "local", "Claude-Data", "claude_x"), 0o700)
	const hint = "or open the Claude desktop app's Code tab once: baton then uses the Claude Code that comes with it"
	out, code := c.run(f.env, "add", "x")
	if code == 0 || !strings.Contains(out, `run "`+cli+`" update`) || !strings.Contains(out, hint) {
		t.Fatalf("add: %d\n%s", code, out)
	}
	// doctor: the copies, then what else there is to do.
	out, _ = c.run(f.env, "doctor")
	out = strings.ReplaceAll(out, "\r\n", "\n")
	want := "  2.1.200   native   " + cli + "  too old (needs " + claude.MinVersion + "+): run \"" + cli + "\" update\n              " + hint + "\n"
	if !strings.Contains(out, want) {
		t.Fatalf("doctor:\n%s", out)
	}
	f.setenv()
	info := claudeInfo(filepath.Join(c.home, "config.json"))
	if info.Found || !info.DesktopApp || len(info.Installs) != 1 || info.Installs[0].Problem != claude.ProblemTooOld {
		t.Fatalf("window: %+v", info)
	}
}

// The window is told which row claude_path is: a Scoop shim's real
// claude.exe, and the desktop app's copies for a version of the app's that it
// removed; Automatic names what it would use, not a path set by hand.
func TestClaudeInfoSelectedRow(t *testing.T) {
	c := newCLI(t)
	f := newFakeInstalls(t)
	f.setenv()
	bundle := f.desktop("2.1.317")
	real := f.cli(filepath.Join(f.root, "scoop", "apps", "claude-code", "current", "claude.exe"), "2.1.320")
	shims := filepath.Join(f.root, "scoop", "shims")
	shim := f.place(filepath.Join(shims, "claude.exe"))
	_ = os.WriteFile(filepath.Join(shims, "claude.shim"), []byte("path = \""+real+"\"\n"), 0o600)
	t.Setenv("PATH", shims)
	cfg := filepath.Join(c.home, "config.json")
	set := func(v string) {
		t.Helper()
		if out, code := c.run(f.env, "config", "claude_path", v); code != 0 {
			t.Fatalf("config %s: %d %s", v, code, out)
		}
	}
	set(shim)
	info := claudeInfo(cfg)
	if !info.Found || info.Fallback != nil || !strings.EqualFold(info.Path, real) || !strings.EqualFold(info.Selected, real) || len(info.Installs) != 2 {
		t.Fatalf("shim: %+v", info)
	}
	// A copy somewhere else, newer than all: used when chosen, not by Automatic.
	other := f.cli(filepath.Join(f.root, "tools", "claude.exe"), "2.1.330")
	set(other)
	info = claudeInfo(cfg)
	if !strings.EqualFold(info.Path, other) || !strings.EqualFold(info.Selected, other) || !strings.EqualFold(info.Auto, real) {
		t.Fatalf("elsewhere: %+v", info)
	}
	// A desktop version chosen, then removed by the app.
	older := f.desktop("2.1.309")
	set(older)
	if info = claudeInfo(cfg); !strings.EqualFold(info.Selected, older) || !strings.EqualFold(info.Path, older) {
		t.Fatalf("desktop version: %+v", info)
	}
	_ = os.RemoveAll(filepath.Dir(filepath.Dir(older)))
	info = claudeInfo(cfg)
	if info.Selected != claude.DesktopChoice || !strings.EqualFold(info.Path, bundle) || info.Fallback != nil || len(info.Installs) != 2 {
		t.Fatalf("removed desktop version: %+v", info)
	}
	set("auto")
	if info = claudeInfo(cfg); info.Selected != "" || !strings.EqualFold(info.Auto, real) {
		t.Fatalf("auto: %+v", info)
	}
}

// The status line of a baton session shows the account and its usage and
// records the usage; baton list then shows it.
func TestStatusLineRecordsUsage(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "personal")
	prof := filepath.Join(c.home, "profiles", "personal")
	reset := time.Now().Add(2 * time.Hour).Unix()
	payload := `{"model":{"id":"x"},"rate_limits":{"five_hour":{"used_percentage":34.4,"resets_at":` + strconv.FormatInt(reset, 10) +
		`},"seven_day":{"used_percentage":12,"resets_at":` + strconv.FormatInt(reset+86400, 10) + `}}}`
	status := func(env []string, stdin string) string {
		cmd := exec.Command(batonExe, "statusline")
		cmd.Env = append(os.Environ(), append([]string{"BATON_HOME=" + c.home}, env...)...)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("statusline: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := status([]string{"CLAUDE_CONFIG_DIR=" + prof}, payload); got != "baton · personal · 5h 34% · week 12%" {
		t.Fatalf("status line %q", got)
	}
	if got := status([]string{"CLAUDE_CONFIG_DIR=" + prof, "BATON_LANG=ko"}, `{}`); got != "baton · personal · 5시간 34% · 주간 12%" {
		t.Fatalf("status line before this claude saw usage %q", got)
	}
	if got := status([]string{"CLAUDE_CONFIG_DIR=" + t.TempDir()}, "not json"); got != "baton" {
		t.Fatalf("status line outside an account %q", got)
	}
	if out, _ := c.run(nil, "list"); !strings.Contains(out, "USAGE") || !strings.Contains(out, "5h 34%, week 12% (just now)") {
		t.Fatalf("list:\n%s", out)
	}
	if out, code := c.run(nil, "config", "claude_path", "desktop"); code != 0 {
		t.Fatalf("claude_path desktop: %d %s", code, out)
	}
	if out, _ := c.run(nil, "config", "claude_path"); strings.TrimSpace(out) != "desktop" {
		t.Fatalf("claude_path = %q", out)
	}
	c.run(nil, "config", "claude_path", fakeClaude) // the removal signs out through claude
	c.run(nil, "remove", "personal", "--yes")
	if _, err := os.Stat(filepath.Join(c.home, "usage", "personal.json")); !os.IsNotExist(err) {
		t.Fatalf("usage kept after remove: %v", err)
	}
}

// In a session the status line also records the model and how full the
// context is, in the run's folder.
func TestStatusLineRecordsRunStatus(t *testing.T) {
	c := newCLI(t)
	id := "20261029-101500-abc123"
	dir := filepath.Join(c.home, "runs", id)
	_ = os.MkdirAll(dir, 0o700)
	cmd := exec.Command(batonExe, "statusline")
	cmd.Env = append(os.Environ(), "BATON_HOME="+c.home, "BATON_RUN="+id)
	cmd.Stdin = strings.NewReader(`{"model":{"id":"m","display_name":"Opus 5.5"},"context_window":{"used_percentage":12.5,"context_window_size":200000}}`)
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "baton" {
		t.Fatalf("statusline: %q %v", out, err)
	}
	st, ok := run.LoadStatus(dir)
	if !ok || st.Model != "Opus 5.5" || st.Context == nil || *st.Context != 12.5 {
		t.Fatalf("status %+v %v", st, ok)
	}
}

// Removing an account, and uninstalling baton, keep the conversations that
// started on it: they move to the user's own Claude Code.
func TestRemoveAndUninstallKeepConversations(t *testing.T) {
	c := newCLI(t)
	home := t.TempDir()
	// The uninstall removes the Start menu entry and an installed baton:
	// those of a temporary user profile, never the real one's.
	env := []string{"CLAUDE_CONFIG_DIR=" + home, "LOCALAPPDATA=" + t.TempDir(), "APPDATA=" + t.TempDir()}
	c.run(env, "add", "a")
	c.run(env, "add", "b")
	write := func(account, sid string) string {
		p := filepath.Join(c.home, "profiles", account, "projects", "C--proj", sid+".jsonl")
		_ = os.MkdirAll(filepath.Join(filepath.Dir(p), sid, "tool-results"), 0o700)
		_ = os.WriteFile(filepath.Join(filepath.Dir(p), sid, "tool-results", "x.txt"), []byte("out"), 0o600)
		if err := os.WriteFile(p, []byte(`{"type":"user","cwd":"C:\proj"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(home, "projects", "C--proj", sid+".jsonl")
	}
	keptA := write("a", "11111111-2222-4333-8444-00000000000a")
	keptB := write("b", "11111111-2222-4333-8444-00000000000b")
	out, code := c.run(env, "remove", "a", "--yes")
	if code != 0 || !strings.Contains(out, "moved 1 conversation") {
		t.Fatalf("remove: %d\n%s", code, out)
	}
	if _, err := os.Stat(keptA); err != nil {
		t.Fatalf("a's conversation was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(keptA), "11111111-2222-4333-8444-00000000000a", "tool-results", "x.txt")); err != nil {
		t.Fatalf("its tool results were not kept: %v", err)
	}
	// baton's data folder is deleted only when it is named baton.
	data := filepath.Join(t.TempDir(), "baton")
	if err := os.Rename(c.home, data); err != nil {
		t.Fatal(err)
	}
	c.home = data
	_, _ = accounts.Update(filepath.Join(data, "accounts.json"), filepath.Join(data, "accounts.lock"), func(r *accounts.Registry) error {
		r.Get("b").ProfileDir = filepath.Join(data, "profiles", "b")
		return nil
	})
	out, code = c.run(env, "uninstall", "--yes")
	if code != 0 || !strings.Contains(out, "moved 1 conversation") {
		t.Fatalf("uninstall: %d\n%s", code, out)
	}
	if _, err := os.Stat(keptB); err != nil {
		t.Fatalf("b's conversation was not kept: %v", err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("baton's data folder is still there: %v", err)
	}
}

// baton update --check asks GitHub (here: a fake) and says what is out.
func TestUpdateCheck(t *testing.T) {
	c := newCLI(t)
	tag := "v9.9.9"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "baton/") {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		v := strings.TrimPrefix(tag, "v")
		switch r.URL.Path {
		case "/rlawlsgus/Baton/releases.atom":
			_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><updated>2026-10-29T04:33:47Z</updated>` +
				`<link rel="alternate" href="https://github.com/rlawlsgus/Baton/releases/tag/` + tag + `"/>` +
				`<content type="html">&lt;p&gt;big news&lt;/p&gt;</content></entry></feed>`))
		case "/rlawlsgus/Baton/releases/download/" + tag + "/checksums.txt":
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  baton_" + v + "_windows_" + runtime.GOARCH + ".zip\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	env := []string{"BATON_UPDATE_SITE=" + srv.URL}
	if out, code := c.run(env, "update", "--check"); code != 0 || !strings.Contains(out, "baton 9.9.9 is out") || !strings.Contains(out, "big news") {
		t.Fatalf("newer: %d\n%s", code, out)
	}
	tag = "v0.0.1"
	if out, code := c.run(env, "update", "--check"); code != 0 || !strings.Contains(out, "is the newest version") {
		t.Fatalf("older: %d\n%s", code, out)
	}
	if out, code := c.run(append(env, "BATON_RUN=x"), "update"); code == 0 || !strings.Contains(out, "inside a baton session") {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
}

// baton rules sets, lists and removes folder rules, and refuses unknown
// accounts and folders that do not exist.
func TestRules(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "add", "a")
	c.run(nil, "add", "b")
	proj := t.TempDir()
	if out, code := c.run(nil, "rules"); code != 0 || !strings.Contains(out, "No folder rules") {
		t.Fatalf("empty list: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "rules", "set", proj, "a,b"); code != 0 || !strings.Contains(out, "use only a, b") {
		t.Fatalf("set: %d\n%s", code, out)
	}
	if out, code := c.run(nil, "rules"); code != 0 || !strings.Contains(out, proj+"  a, b") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	for _, bad := range [][]string{{"rules", "set", proj, "nope"}, {"rules", "set", filepath.Join(proj, "missing"), "a"},
		{"rules", "remove", filepath.Join(proj, "other")}, {"rules", "set", proj}} {
		if out, code := c.run(nil, bad...); code == 0 {
			t.Errorf("%v accepted:\n%s", bad, out)
		}
	}
	if out, code := c.run(nil, "rules", "remove", proj); code != 0 || !strings.Contains(out, "Removed the rule") {
		t.Fatalf("remove: %d\n%s", code, out)
	}
	if out, _ := c.run(nil, "rules"); !strings.Contains(out, "No folder rules") {
		t.Fatalf("still listed:\n%s", out)
	}
}
