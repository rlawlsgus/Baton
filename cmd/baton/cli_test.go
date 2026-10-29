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

var batonExe, fakeClaude string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "baton-cli-test")
	if err != nil {
		panic(err)
	}
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
	env := []string{"BATON_HOME=" + c.home}
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
		{"remove", "a"}, {"login", "a"}, {"sync"}, {"run"}, {"stop"}, {"uninstall", "--yes"}, {"gui", "--browser"}} {
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
	if out, code := c.run(nil, "config", "claude_path", "cli_test.go"); code != 0 {
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
// included; baton resume refuses what it cannot continue as asked.
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
		!strings.Contains(out, "Claude desktop app") || !strings.Contains(out, lost) {
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
		{[]string{"resume", open}, 1, "--copy"},
		{[]string{"resume", strings.ToUpper(open)}, 1, "--copy"}, // ids are not case-sensitive
	} {
		if out, code := c.run(env, tc.args...); code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("%q: %d %s", tc.args, code, out)
		}
	}
	// Inside a session it refuses before saying where it would continue.
	if out, code := c.run(append(env, "BATON_RUN=x"), "resume", open, "--copy"); code != 1 || !strings.Contains(out, "inside a baton session") ||
		strings.Contains(out, "continuing in") {
		t.Errorf("resume inside a session: %d %s", code, out)
	}
}

// A Claude Code CLI installed long ago is passed over for the desktop app's
// copy, and doctor and the window say so.
func TestOldCLIPassedOverForTheDesktopCopy(t *testing.T) {
	c := newCLI(t)
	c.run(nil, "config", "claude_path", "auto")
	exe, err := os.ReadFile(fakeClaude)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	place := func(p string) string {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, exe, 0o700); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		_ = os.Chtimes(p, old, old)
		return p
	}
	cli := place(filepath.Join(root, ".local", "bin", "claude.exe"))
	bundle := place(filepath.Join(root, "local", "Claude-Data", "x", "claude-code", "2.1.308", "abc", "claude.exe"))
	env := []string{"USERPROFILE=" + root, "APPDATA=" + filepath.Join(root, "roaming"), "LOCALAPPDATA=" + filepath.Join(root, "local"),
		"PATH=" + filepath.Join(root, "empty"), "FAKE_OLD_CLI=1"}
	out, _ := c.run(env, "doctor")
	if !strings.Contains(out, "found via desktop") || !strings.Contains(out, "not used: "+cli+" (native, 2.0.9, too old)") {
		t.Fatalf("doctor:\n%s", out)
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	info := claudeInfo(filepath.Join(c.home, "config.json"))
	if !info.Found || info.Source != "desktop" || !strings.EqualFold(info.Path, bundle) || info.OldCLI != "2.0.9" {
		t.Fatalf("window: %+v", info)
	}
	// The desktop copy turned off: the old CLI is reported as too old.
	c.run(nil, "config", "allow_desktop_bundle", "false")
	info = claudeInfo(filepath.Join(c.home, "config.json"))
	if info.Found || !info.TooOld || info.Source != "native" || !info.BundleOff {
		t.Fatalf("window without the desktop copy: %+v", info)
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
	env := []string{"CLAUDE_CONFIG_DIR=" + home}
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
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": tag, "body": "big news", "html_url": "https://example.com/" + tag,
			"assets": []map[string]string{
				{"name": "baton_" + v + "_windows_" + runtime.GOARCH + ".zip", "browser_download_url": "https://example.com/z"},
				{"name": "checksums.txt", "browser_download_url": "https://example.com/s"}}}})
	}))
	defer srv.Close()
	env := []string{"BATON_UPDATE_API=" + srv.URL}
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
