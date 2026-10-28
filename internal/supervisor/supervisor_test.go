package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
)

func newTest(t *testing.T, userArgs ...string) *Supervisor {
	t.Helper()
	root := t.TempDir()
	paths := app.Paths{Root: root}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	ca, err := claude.SplitArgs(userArgs)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.SwitchMode = config.ModeAuto
	runDir := filepath.Join(root, "runs", "r1")
	_ = os.MkdirAll(runDir, 0o700)
	s := &Supervisor{
		opt:    Options{Paths: paths, Config: cfg, ClaudeArgs: ca, Cwd: root},
		runDir: runDir,
		tail:   run.NewTail(runDir),
		logf:   func(string, ...any) {},
		reg: accounts.Registry{Accounts: []accounts.Account{
			{ID: "a", ProfileDir: filepath.Join(root, "profiles", "a"), Enabled: true, LoggedIn: true, Priority: 20},
			{ID: "b", ProfileDir: filepath.Join(root, "profiles", "b"), Enabled: true, LoggedIn: true, Priority: 10},
		}},
	}
	s.acct = s.reg.Accounts[0]
	return s
}

func writeTranscript(t *testing.T, profile, session string) string {
	t.Helper()
	p := filepath.Join(profile, "projects", "C--proj", session+".jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(`{"sessionId":"`+session+`","cwd":"C:\\proj"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func emit(t *testing.T, s *Supervisor, event string, pid int, payload map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	if err := run.AppendEvent(s.runDir, run.Event{TS: time.Now(), Event: event, ClaudePID: pid, Payload: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestFirstArgsNewSessionKeepsDashDashLast(t *testing.T) {
	s := newTest(t, "--model", "opus", "fix it", "--", "-v is broken")
	args, err := s.firstArgs()
	if err != nil {
		t.Fatal(err)
	}
	if s.session == "" {
		t.Fatal("no session id assigned")
	}
	want := []string{"--session-id", s.session, "--model", "opus", "fix it", "--", "-v is broken"}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %q\nwant   %q", args, want)
	}
}

// claude (commander) lets a variadic option swallow the following words, so
// the first launch must keep the user's own order; regrouping options in
// front of the prompt would turn the prompt into an --add-dir value.
func TestFirstArgsKeepUserOrderWhenResuming(t *testing.T) {
	p := writeTranscript(t, t.TempDir(), "abc")
	s := newTest(t, "fix the failing test", "--add-dir", `..\lib`, "--resume", p)
	args, err := s.firstArgs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fix the failing test", "--add-dir", `..\lib`, "--resume", p}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %q\nwant   %q", args, want)
	}
	re, err := claude.SplitArgs(args)
	if err != nil || !slices.Equal(re.FirstOnly[:1], []string{"fix the failing test"}) {
		t.Fatalf("re-parsed under claude's rules the prompt is lost: %+v %v", re, err)
	}
}

func TestRelaunchGuardsDashPromptAndForks(t *testing.T) {
	s := newTest(t)
	s.session = "sid"
	p := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "sid")
	s.opt.Config.ContinuePrompt = "- keep going"
	if got, _ := s.relaunchArgs(&pending{reason: reasonLimit}); !slices.Equal(got, []string{"--resume", p, "--", "- keep going"}) {
		t.Errorf("dash prompt: %q", got)
	}
	os.Remove(p)
	s.transcr, s.session = "", "fork-id"
	s.forkSource = writeTranscript(t, t.TempDir(), "orig")
	if got, _ := s.relaunchArgs(&pending{reason: reasonManual}); !slices.Equal(got, []string{"--resume", s.forkSource, "--fork-session"}) {
		t.Errorf("fork without a file yet: %q", got)
	}
}

// claude does not restore the permission mode on resume; the relaunch passes
// the mode the transcript last recorded, else the one the user started with.
func TestRelaunchCarriesPermissionMode(t *testing.T) {
	s := newTest(t, "--permission-mode", "acceptEdits", "go")
	s.session = "sid"
	p := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "sid")
	got, _ := s.relaunchArgs(&pending{reason: reasonLimit})
	if !slices.Equal(got[:3], []string{"--permission-mode", "acceptEdits", "--resume"}) {
		t.Fatalf("from the start flag: %q", got)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"sid"}` + "\n")
	f.Close()
	got, _ = s.relaunchArgs(&pending{reason: reasonLimit})
	if !slices.Equal(got[:2], []string{"--permission-mode", "bypassPermissions"}) || !bypassRequested(got) {
		t.Fatalf("from the transcript: %q", got)
	}
}

func TestReloadConfigOnlyTightens(t *testing.T) {
	s := newTest(t)
	s.opt.Config.SwitchMode = config.ModeConfirm
	cfg := s.opt.Config
	cfg.SwitchMode = config.ModeAuto // e.g. written from inside the session
	_ = config.Save(s.opt.Paths.Config(), cfg)
	s.reloadConfig()
	if s.opt.Config.SwitchMode != config.ModeConfirm {
		t.Fatalf("confirm was loosened to %s mid-run", s.opt.Config.SwitchMode)
	}
	cfg.SwitchMode = config.ModeOff
	_ = config.Save(s.opt.Paths.Config(), cfg)
	s.reloadConfig()
	if s.opt.Config.SwitchMode != config.ModeOff {
		t.Fatalf("off was not applied: %s", s.opt.Config.SwitchMode)
	}
}

func TestMarksSurviveALedgerWriteFailure(t *testing.T) {
	s := newTest(t)
	// Make the ledger unwritable: a directory where state.json should be.
	_ = os.MkdirAll(s.opt.Paths.State(), 0o700)
	s.markAccount(ledger.StatusExhausted, time.Now().Add(time.Hour), "limit")
	if st := s.loadState().StatusOf("a"); st.Status != ledger.StatusExhausted {
		t.Fatalf("in-memory mark lost: %+v", st)
	}
}

func TestLimitDuringManualWaitKeepsContinuePrompt(t *testing.T) {
	s := newTest(t)
	p := &pending{reason: reasonManual}
	s.pend = &pending{reason: reasonLimit, detail: "limit"}
	// The merge stopChild does after the stop:
	if s.pend != nil && p.reason == reasonManual && (s.pend.reason == reasonLimit || s.pend.reason == reasonAuth) {
		p.reason, p.detail = s.pend.reason, s.pend.detail
	}
	if p.reason != reasonLimit {
		t.Fatal("limit not carried over")
	}
}

func TestFirstArgsResumeByPathTracksTranscript(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "home", ".claude")
	p := writeTranscript(t, outside, "abc")
	s := newTest(t, "--resume", p)
	if _, err := s.firstArgs(); err != nil {
		t.Fatal(err)
	}
	if s.transcr != p || s.session != "abc" {
		t.Fatalf("transcr=%q session=%q", s.transcr, s.session)
	}
	// A later manual switch must resume that file, not start an empty session.
	args, prompt := s.relaunchArgs(&pending{reason: reasonManual})
	if !slices.Equal(args, []string{"--resume", p}) || prompt {
		t.Fatalf("relaunch = %q prompt=%v", args, prompt)
	}
}

func TestRelaunchArgs(t *testing.T) {
	s := newTest(t, "--model", "opus", "first prompt", "--file", "x")
	s.session = "sid"
	p := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "sid")
	cont := s.opt.Config.ContinuePrompt
	cases := []struct {
		name  string
		setup func()
		p     pending
		want  []string
	}{
		{"limit", func() {}, pending{reason: reasonLimit}, []string{"--model", "opus", "--resume", p, cont}},
		{"auth", func() {}, pending{reason: reasonAuth}, []string{"--model", "opus", "--resume", p, cont}},
		{"declined", func() { s.declined = true }, pending{reason: reasonLimit}, []string{"--model", "opus", "--resume", p}},
		{"manual", func() {}, pending{reason: reasonManual}, []string{"--model", "opus", "--resume", p}},
		{"manual-owed", func() { s.contOwed = true }, pending{reason: reasonManual}, []string{"--model", "opus", "--resume", p, cont}},
		{"manual-broke", func() { s.broke = true }, pending{reason: reasonManual}, []string{"--model", "opus", "--resume", p, restartPrompt}},
	}
	for _, c := range cases {
		s.declined, s.contOwed, s.broke = false, false, false
		c.setup()
		got, _ := s.relaunchArgs(&c.p)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %q\nwant %q", c.name, got, c.want)
		}
	}
	// No transcript anywhere yet: fresh start under the same id, no prompt.
	os.Remove(p)
	s.transcr = ""
	if got, _ := s.relaunchArgs(&pending{reason: reasonLimit}); !slices.Equal(got, []string{"--model", "opus", "--session-id", "sid"}) {
		t.Errorf("no transcript: %q", got)
	}
}

func TestHandleEventsFiltersAndTracks(t *testing.T) {
	s := newTest(t)
	emit(t, s, "StopFailure", 111, map[string]any{"error": "rate_limit"}) // before launch: stale
	time.Sleep(5 * time.Millisecond)
	s.launchedAt = time.Now()
	real := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "s1")
	emit(t, s, "SessionStart", 222, map[string]any{"session_id": "s1", "source": "resume",
		"transcript_path": filepath.Join(s.reg.Accounts[1].ProfileDir, "projects", "x", "s1.jsonl")})
	emit(t, s, "StopFailure", 333, map[string]any{"error": "rate_limit"}) // another claude: ignored
	s.handleEvents()
	if s.pend != nil {
		t.Fatal("stale or foreign StopFailure was acted on")
	}
	if s.claudePID != 222 || !s.started || s.transcr != real {
		t.Fatalf("pid=%d started=%v transcr=%q (want the real file %q)", s.claudePID, s.started, s.transcr, real)
	}
	s.contOwed = true
	emit(t, s, "Stop", 222, map[string]any{"session_id": "s1"})
	emit(t, s, "StopFailure", 222, map[string]any{"session_id": "s1", "error": "rate_limit",
		"transcript_path": real, "last_assistant_message": "You've hit your session limit · resets 3pm (Asia/Seoul)"})
	emit(t, s, "SessionEnd", 222, map[string]any{"session_id": "s1", "reason": "prompt_input_exit"})
	s.handleEvents()
	if s.contOwed || s.pend == nil || s.pend.reason != reasonLimit || !s.userQuit {
		t.Fatalf("contOwed=%v pend=%+v userQuit=%v", s.contOwed, s.pend, s.userQuit)
	}
	st, _ := ledger.Load(s.opt.Paths.State())
	if a := st.StatusOf("a"); a.Status != ledger.StatusExhausted || !a.RetryAt.After(time.Now()) {
		t.Fatalf("ledger: %+v", a)
	}
}

func TestOverloadAndBillingDoNotSwitch(t *testing.T) {
	s := newTest(t)
	s.launchedAt = time.Now().Add(-time.Second)
	emit(t, s, "StopFailure", 1, map[string]any{"error": "server_error"})
	s.handleEvents()
	if s.pend != nil {
		t.Fatal("server_error must not switch")
	}
	emit(t, s, "StopFailure", 1, map[string]any{"error": "billing_error"})
	s.handleEvents()
	st, _ := ledger.Load(s.opt.Paths.State())
	if s.pend != nil || st.StatusOf("a").Status != ledger.StatusBlocked {
		t.Fatalf("billing: pend=%+v status=%s", s.pend, st.StatusOf("a").Status)
	}
}

func TestSwitchModeOffRecordsButDoesNotSwitch(t *testing.T) {
	s := newTest(t)
	s.opt.Config.SwitchMode = config.ModeOff
	// Turned off with `baton config` while the run was going: picked up live.
	cfg := s.opt.Config
	cfg.SwitchMode = config.ModeOff
	if err := config.Save(s.opt.Paths.Config(), cfg); err != nil {
		t.Fatal(err)
	}
	s.opt.Config.SwitchMode = config.ModeAuto
	s.launchedAt = time.Now().Add(-time.Second)
	emit(t, s, "StopFailure", 1, map[string]any{"error": "rate_limit"})
	s.handleEvents()
	st, _ := ledger.Load(s.opt.Paths.State())
	if s.pend != nil || st.StatusOf("a").Status != ledger.StatusExhausted {
		t.Fatalf("pend=%+v status=%s", s.pend, st.StatusOf("a").Status)
	}
}

func TestManualTargetValidation(t *testing.T) {
	s := newTest(t)
	if err := accounts.Save(s.opt.Paths.Accounts(), s.reg); err != nil {
		t.Fatal(err)
	}
	if !s.manualTargetUsable("b") || s.manualTargetUsable("a") || s.manualTargetUsable("nope") {
		t.Fatal("basic target checks failed")
	}
	// Disabled meanwhile in accounts.json: the running supervisor must notice.
	_, _ = accounts.Update(s.opt.Paths.Accounts(), s.opt.Paths.AccountsLock(), func(r *accounts.Registry) error {
		r.Get("b").Enabled = false
		return nil
	})
	if s.manualTargetUsable("b") || s.manualTargetUsable("") {
		t.Fatal("a disabled account was accepted as a switch target")
	}
}

func appendRow(t *testing.T, transcript, mode string) {
	t.Helper()
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(`{"type":"permission-mode","permissionMode":"` + mode + `"}` + "\n")
}

// A start flag --dangerously-skip-permissions would override the carried mode,
// putting a session the user moved out of bypass back into it.
func TestRelaunchDropsBypassFlagOnceLeft(t *testing.T) {
	s := newTest(t, "--dangerously-skip-permissions", "go")
	s.session = "sid"
	p := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "sid")
	appendRow(t, p, "plan")
	got, _ := s.relaunchArgs(&pending{reason: reasonManual})
	want := []string{"--allow-dangerously-skip-permissions", "--permission-mode", "plan", "--resume", p}
	if !slices.Equal(got, want) {
		t.Fatalf("left bypass: %q\nwant %q", got, want)
	}
	appendRow(t, p, "bypassPermissions")
	got, _ = s.relaunchArgs(&pending{reason: reasonManual})
	want = []string{"--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "--resume", p}
	if !slices.Equal(got, want) {
		t.Fatalf("still bypass: %q\nwant %q", got, want)
	}
}

// A fork that has no file yet runs in its own start mode, not the mode its
// source transcript ended in.
func TestForkRelaunchKeepsForksOwnMode(t *testing.T) {
	s := newTest(t, "--permission-mode", "plan")
	src := writeTranscript(t, t.TempDir(), "orig")
	appendRow(t, src, "bypassPermissions")
	s.forkSource, s.session = src, "fork-id"
	got, _ := s.relaunchArgs(&pending{reason: reasonManual})
	if want := []string{"--permission-mode", "plan", "--resume", src, "--fork-session"}; !slices.Equal(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
}

// After /clear the new transcript is empty: the mode the session was in is
// kept, and a former fork is not forked from its source again.
func TestClearKeepsModeAndEndsFork(t *testing.T) {
	s := newTest(t)
	s.launchedAt = time.Now().Add(-time.Second)
	s.forkSource = writeTranscript(t, t.TempDir(), "orig")
	fork := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "fork1")
	appendRow(t, fork, "acceptEdits")
	emit(t, s, "Stop", 7, map[string]any{"session_id": "fork1", "transcript_path": fork})
	cleared := filepath.Join(s.reg.Accounts[0].ProfileDir, "projects", "C--proj", "new2.jsonl")
	emit(t, s, "SessionStart", 7, map[string]any{"session_id": "new2", "source": "clear", "transcript_path": cleared})
	s.handleEvents()
	got, _ := s.relaunchArgs(&pending{reason: reasonManual})
	if want := []string{"--permission-mode", "acceptEdits", "--session-id", "new2"}; !slices.Equal(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
}

// Mode rows written before this launch describe an earlier run: claude does
// not restore that mode on resume, so they must not be carried over.
func TestResumeIgnoresModeRowsFromEarlierRuns(t *testing.T) {
	old := writeTranscript(t, filepath.Join(t.TempDir(), ".claude"), "abc")
	appendRow(t, old, "bypassPermissions")
	s := newTest(t, "--resume", old)
	if _, err := s.firstArgs(); err != nil {
		t.Fatal(err)
	}
	s.noteModeOffset() // what launch does
	if got, _ := s.relaunchArgs(&pending{reason: reasonManual}); !slices.Equal(got, []string{"--resume", old}) {
		t.Fatalf("before any prompt: %q", got)
	}
	appendRow(t, old, "default") // the first prompt of this run
	if got, _ := s.relaunchArgs(&pending{reason: reasonManual}); !slices.Equal(got, []string{"--permission-mode", "default", "--resume", old}) {
		t.Fatalf("after a prompt: %q", got)
	}
	// The next launch starts after these rows; the mode passed to it carries on.
	s.noteModeOffset()
	if got, _ := s.relaunchArgs(&pending{reason: reasonManual}); !slices.Equal(got, []string{"--permission-mode", "default", "--resume", old}) {
		t.Fatalf("after a relaunch: %q", got)
	}
}

func TestBlockedNoticeOncePerAccount(t *testing.T) {
	s := newTest(t)
	s.markAccount(ledger.StatusBlocked, time.Time{}, "on hold")
	s.markAccount(ledger.StatusBlocked, time.Time{}, "on hold")
	if len(s.blocked) != 1 {
		t.Fatalf("blocked = %q", s.blocked)
	}
}

// An unreadable state.json must not make blocked accounts look fine.
func TestLoadStateKeepsLastGoodCopy(t *testing.T) {
	s := newTest(t)
	s.markAccount(ledger.StatusBlocked, time.Time{}, "on hold")
	if s.loadState().StatusOf("a").Status != ledger.StatusBlocked {
		t.Fatal("mark not recorded")
	}
	if err := os.WriteFile(s.opt.Paths.State(), []byte("{,"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.loadState().StatusOf("a").Status; got != ledger.StatusBlocked {
		t.Fatalf("after corruption: %s", got)
	}
}

// A conversation picked after launch (-r picker, /resume) carries mode rows
// from its earlier runs; they must not be passed to the next account.
func TestPickerResumeIgnoresOldModeRows(t *testing.T) {
	s := newTest(t, "--resume")
	if _, err := s.firstArgs(); err != nil {
		t.Fatal(err)
	}
	s.noteModeOffset() // what launch does: nothing known yet
	s.launchedAt = time.Now().Add(-time.Second)
	old := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "old")
	appendRow(t, old, "bypassPermissions")
	emit(t, s, "SessionStart", 9, map[string]any{"session_id": "old", "source": "resume",
		"transcript_path": filepath.Join(s.reg.Accounts[1].ProfileDir, "projects", "x", "old.jsonl")})
	s.handleEvents()
	if got, _ := s.relaunchArgs(&pending{reason: reasonManual}); !slices.Equal(got, []string{"--resume", old}) {
		t.Fatalf("%q", got)
	}
}

// A login failure on a limited account keeps the limit's reset time.
func TestNeedsLoginMarkKeepsLimit(t *testing.T) {
	s := newTest(t)
	retry := time.Now().Add(time.Hour).Round(time.Second)
	s.markAccount(ledger.StatusExhausted, retry, "limit")
	s.markAccount(ledger.StatusNeedsLogin, time.Time{}, "401")
	st := s.loadState().StatusOf("a")
	if st.Status != ledger.StatusNeedsLogin || !st.RetryAt.Equal(retry) || st.Cleared(time.Now()) != ledger.StatusExhausted {
		t.Fatalf("%+v", st)
	}
}

// An in-session /resume keeps the mode the running claude is in (as /clear
// does); it must not fall back to the start flags, here bypass.
func TestInSessionResumeKeepsCurrentMode(t *testing.T) {
	s := newTest(t, "--dangerously-skip-permissions")
	s.session = "a1"
	s.launchedAt = time.Now().Add(-time.Second)
	a := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "a1")
	s.transcr = a
	s.noteModeOffset()
	appendRow(t, a, "bypassPermissions")
	appendRow(t, a, "default") // Shift+Tab out of bypass, then a prompt
	older := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "older")
	appendRow(t, older, "acceptEdits") // from an earlier run
	emit(t, s, "SessionStart", 5, map[string]any{"session_id": "older", "source": "resume",
		"transcript_path": filepath.Join(s.reg.Accounts[1].ProfileDir, "projects", "x", "older.jsonl")})
	s.handleEvents()
	got, _ := s.relaunchArgs(&pending{reason: reasonManual})
	want := []string{"--allow-dangerously-skip-permissions", "--permission-mode", "default", "--resume", older}
	if !slices.Equal(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
}
