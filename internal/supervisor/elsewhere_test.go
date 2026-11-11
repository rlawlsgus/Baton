package supervisor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/sched"
	"baton/internal/winproc"
)

const heldSID = "11111111-2222-4333-8444-555555555555"

// holderEntry writes a session registry entry in dir for a live claude (this
// test's own process) that has conversation sid open, started at started (ms;
// 0: when the process started).
func holderEntry(t *testing.T, dir, sid, entrypoint string, started int64) {
	t.Helper()
	pid := os.Getpid()
	if started == 0 {
		started = winproc.ProcessCreated(pid).UnixMilli()
	}
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"), []byte(`{"pid":`+strconv.Itoa(pid)+
		`,"sessionId":"`+sid+`","entrypoint":"`+entrypoint+`","startedAt":`+strconv.FormatInt(started, 10)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(claude.OpenElsewhere([]string{dir}, sid)) == 0 {
		t.Skip("cannot fake a running claude here")
	}
}

// ownEntry writes this session's own claude's registry entry (a pid that is
// not running: only its start time is read) and makes it the session's.
func ownEntry(t *testing.T, s *Supervisor, status string, started int64) {
	t.Helper()
	const pid = 999998
	s.claudePID, s.started = pid, true
	dir := filepath.Join(s.acct.ProfileDir, "sessions")
	_ = os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(`{"pid":`+strconv.Itoa(pid)+
		`,"sessionId":"`+s.session+`","status":"`+status+`","startedAt":`+strconv.FormatInt(started, 10)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// notesSince returns the notifications written since mark (from notes()).
func notes() string {
	b, _ := os.ReadFile(os.Getenv("BATON_NOTIFY_FILE"))
	return string(b)
}

func notesSince(mark string) string { return strings.TrimPrefix(notes(), mark) }

// heldSession is a session on account a that runs a conversation kept in the
// user's own Claude Code (brought in without a copy), in acceptEdits mode.
func heldSession(t *testing.T) (*Supervisor, string, string) {
	t.Helper()
	s := newTest(t)
	s.opt.Out = &bytes.Buffer{}
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	orig := writeTranscript(t, home, heldSID)
	s.transcr, s.session = orig, heldSID
	s.noteModeOffset()
	appendRow(t, orig, "acceptEdits") // this session's own prompt
	return s, home, orig
}

// The desktop app opens the conversation while Claude is idle: baton moves
// the session to a copy right away, on the same account. No question in
// confirm mode, nothing counted or logged as a switch, the original is
// resumed as a fork in the session's mode without a prompt, and the
// notification says what happened.
func TestOpenedElsewhereWhileIdleMovesToACopy(t *testing.T) {
	s, home, orig := heldSession(t)
	s.opt.Config.SwitchMode = config.ModeConfirm
	s.guard = sched.RateGuard{Cooldown: time.Hour, MaxPerHour: 1} // any counted switch shows
	ownEntry(t, s, "idle", time.Now().UnixMilli())
	now := time.Now()
	s.lookElsewhere(now) // nobody else yet
	if s.held != nil || s.cleanSize == 0 {
		t.Fatalf("held %+v clean %d", s.held, s.cleanSize)
	}
	// The other side opens it, and (a terminal's claude would) writes its
	// own mode into it: the copy must keep this session's mode.
	holderEntry(t, home, heldSID, "claude-desktop", 0)
	appendRow(t, orig, "bypassPermissions")
	mark := notes()
	s.lookElsewhere(now.Add(5 * time.Second))
	s.moveWhenIdle("idle", true, now.Add(5*time.Second))
	if s.pend != nil {
		t.Fatal("moved before Claude stayed idle a moment")
	}
	s.moveWhenIdle("idle", true, now.Add(9*time.Second))
	if s.pend == nil || s.pend.reason != reasonElsewhere || s.pend.holder == nil || s.pend.holder.PID != os.Getpid() {
		t.Fatalf("pend %+v", s.pend)
	}
	if added := notesSince(mark); added != "" {
		t.Fatalf("a notification before the move: %s", added)
	}
	p := s.pend
	next, ok := s.chooseNext(p)
	if !ok || next.ID != "a" || s.confirmed {
		t.Fatalf("chooseNext: %+v %v confirmed=%v", next, ok, s.confirmed)
	}
	s.prepareSwitch(p, next)
	if s.session != "" || s.transcr != "" || s.forkSource != orig || s.held != nil {
		t.Fatalf("session=%q transcr=%q fork=%q", s.session, s.transcr, s.forkSource)
	}
	args, withPrompt := s.relaunchArgs(p)
	if want := []string{"--permission-mode", "acceptEdits", "--resume", orig, "--fork-session"}; !slices.Equal(args, want) || withPrompt {
		t.Fatalf("relaunch %q prompt=%v\nwant %q", args, withPrompt, want)
	}
	added := notesSince(mark)
	if !strings.Contains(added, `"tag":"elsewhere"`) || !strings.Contains(added, "the Claude desktop app, so baton moved this session to a copy") {
		t.Fatalf("notification: %s", added)
	}
	st, _ := ledger.Load(s.opt.Paths.State())
	if len(st.Switches) != 0 {
		t.Fatalf("logged as a switch: %+v", st.Switches)
	}
	if allowed, _ := s.guard.Allow(time.Now()); !allowed {
		t.Fatal("the guard counted it")
	}
}

// Opened elsewhere while Claude answers (or waits for an answer): one
// notification says the move comes once the response is done, and the move
// starts when Claude is idle, not before. If the other side closes it first,
// nothing moves.
func TestOpenedElsewhereWhileBusyWaitsForIdle(t *testing.T) {
	s, home, _ := heldSession(t)
	ownEntry(t, s, "busy", time.Now().UnixMilli())
	holderEntry(t, home, heldSID, "cli", 0)
	mark := notes()
	now := time.Now()
	s.lookElsewhere(now)
	s.moveWhenIdle("busy", true, now)
	s.moveWhenIdle("waiting", true, now.Add(time.Second))
	s.moveWhenIdle("idle", true, now.Add(2*time.Second))
	s.moveWhenIdle("busy", true, now.Add(3*time.Second)) // a moment of idle between tools
	s.moveWhenIdle("idle", true, now.Add(4*time.Second))
	if s.pend != nil {
		t.Fatalf("moved while busy: %+v", s.pend)
	}
	added := notesSince(mark)
	if strings.Count(added, "as soon as Claude finishes its current response") != 1 || !strings.Contains(added, "Claude Code in a terminal") ||
		!strings.Contains(added, `"tag":"elsewhere"`) {
		t.Fatalf("notifications: %s", added)
	}
	s.moveWhenIdle("idle", true, now.Add(8*time.Second))
	if s.pend == nil || s.pend.reason != reasonElsewhere {
		t.Fatalf("not moved once idle: %+v", s.pend)
	}

	// The other side closed it before Claude was done: it stays here.
	s.pend = nil
	_ = os.Remove(filepath.Join(home, "sessions", strconv.Itoa(os.Getpid())+".json"))
	s.lookElsewhere(now.Add(15 * time.Second))
	s.moveWhenIdle("idle", true, now.Add(20*time.Second))
	if s.held != nil || s.pend != nil {
		t.Fatalf("held %+v pend %+v", s.held, s.pend)
	}
}

// Two baton sessions with one conversation open: only the one whose claude
// started later moves, so they never both move. A claude in an account's
// profile that no running baton session started is not one of baton's: then
// this session moves whatever the times say.
func TestTwoBatonSessionsOnlyTheLaterMoves(t *testing.T) {
	s, _, _ := heldSession(t)
	pid := os.Getpid()
	other := s.reg.Accounts[1].ProfileDir
	started := winproc.ProcessCreated(pid).UnixMilli()
	holderEntry(t, other, heldSID, "cli", started)
	look := func() bool {
		s.held, s.elsewhereAt = nil, time.Time{}
		s.lookElsewhere(time.Now())
		return s.held != nil
	}
	// No run started that claude: not a baton session.
	ownEntry(t, s, "idle", started-60_000)
	if !look() {
		t.Fatal("a claude no baton run started was taken for a baton session")
	}
	// Another running baton session's claude.
	dir := s.opt.Paths.Run("20261109-101500-abc123")
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: "20261109-101500-abc123", SupervisorPID: pid, SupervisorCreated: winproc.ProcessCreated(pid),
		State: run.StateRunning, Account: "b", ChildPID: pid, SessionID: heldSID}); err != nil {
		t.Fatal(err)
	}
	if look() {
		t.Fatal("this session moves although the other one's claude started later")
	}
	ownEntry(t, s, "idle", started+60_000) // this one started later
	if !look() || s.held.PID != pid {
		t.Fatalf("the later session stays: %+v", s.held)
	}
}

// A limit while the desktop app has the conversation open: after the short
// wait for its own claude, the session goes on in a copy on the next account
// with the continue prompt (it never ends over it), the switch is logged and
// the notification says so.
func TestSwitchWhileHeldGoesOnInACopy(t *testing.T) {
	s, home, orig := heldSession(t)
	s.lookElsewhere(time.Now()) // while nobody else had it open
	holderEntry(t, home, heldSID, "claude-desktop", 0)
	mark := notes()
	p := &pending{reason: reasonLimit}
	start := time.Now()
	s.prepareSwitch(p, &s.reg.Accounts[1])
	if time.Since(start) < 4*time.Second {
		t.Fatalf("did not wait for its own claude to go: %v", time.Since(start))
	}
	if s.acct.ID != "b" || s.session != "" || s.forkSource != orig {
		t.Fatalf("acct=%s session=%q fork=%q", s.acct.ID, s.session, s.forkSource)
	}
	args, withPrompt := s.relaunchArgs(p)
	want := []string{"--permission-mode", "acceptEdits", "--resume", orig, "--fork-session", s.opt.Config.ContinuePrompt}
	if !slices.Equal(args, want) || !withPrompt {
		t.Fatalf("relaunch %q\nwant %q", args, want)
	}
	st, _ := ledger.Load(s.opt.Paths.State())
	if len(st.Switches) != 1 || st.Switches[0].To != "b" || st.Switches[0].Session != heldSID {
		t.Fatalf("switches %+v", st.Switches)
	}
	added := notesSince(mark)
	if strings.Count(added, `"tag":"switched"`) != 1 || !strings.Contains(added, "a hit its usage limit. The conversation is also open in the Claude desktop app, so it goes on in a copy on b; the original stays there.") {
		t.Fatalf("notification: %s", added)
	}

	// Nobody else has it open: the switch goes on in place, as always.
	s2, _, orig2 := heldSession(t)
	s2.prepareSwitch(&pending{reason: reasonLimit}, &s2.reg.Accounts[1])
	if args, _ := s2.relaunchArgs(&pending{reason: reasonLimit}); slices.Contains(args, "--fork-session") || !slices.Contains(args, orig2) {
		t.Fatalf("in place: %q", args)
	}
}

// A start that would open a conversation in place: in a copy when another
// Claude Code has it open (it says so), as it is when nobody does, and not at
// all when a baton session runs it.
func TestStartGoesOnInACopyOrLeavesItToBaton(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	orig := writeTranscript(t, home, heldSID)
	start := func() (*Supervisor, []string, error, string) {
		s := newTest(t, "--model", "opus", "--resume", orig, "--", "go on")
		var out bytes.Buffer
		s.opt.Out = &out
		args, err := s.firstArgs()
		return s, args, err, out.String()
	}
	s, args, err, _ := start()
	if err != nil || !slices.Equal(args, []string{"--model", "opus", "--resume", orig, "--", "go on"}) || s.session != heldSID {
		t.Fatalf("nobody has it open: %q %v session=%q", args, err, s.session)
	}

	holderEntry(t, home, heldSID, "claude-desktop", 0)
	s, args, err, out := start()
	if err != nil || !slices.Equal(args, []string{"--model", "opus", "--fork-session", "--resume", orig, "--", "go on"}) {
		t.Fatalf("held: %q %v", args, err)
	}
	if s.session != "" || s.transcr != "" || s.forkSource != orig || !strings.Contains(out, "It is open in the Claude desktop app, so it continues in a copy") {
		t.Fatalf("session=%q transcr=%q fork=%q\n%s", s.session, s.transcr, s.forkSource, out)
	}

	// Held by a baton session: nothing starts, and the window would come
	// forward (it has none here).
	pid := os.Getpid()
	id := "20261109-101500-def456"
	s3 := newTest(t, "--resume", orig)
	s3.opt.Out = &bytes.Buffer{}
	dir := s3.opt.Paths.Run(id)
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: id, SupervisorPID: pid, SupervisorCreated: winproc.ProcessCreated(pid),
		State: run.StateRunning, Account: "a", ChildPID: pid, SessionID: heldSID}); err != nil {
		t.Fatal(err)
	}
	if args, err := s3.firstArgs(); !errors.Is(err, errInBaton) || args != nil {
		t.Fatalf("held by baton: %q %v", args, err)
	}
	if out := s3.opt.Out.(*bytes.Buffer).String(); !strings.Contains(out, "already running in baton") {
		t.Fatalf("said: %s", out)
	}

	// Reopening a cut-off session whose conversation is open elsewhere now.
	s4 := newTest(t)
	s4.opt.Out = &bytes.Buffer{}
	args, err = s4.restoreArgs(&run.Journal{RunID: "20261109-090000-aaaaaa", TranscriptPath: orig, SessionID: heldSID, Owed: true})
	if err != nil || !slices.Equal(args, []string{"--resume", orig, "--fork-session", restartPrompt}) || s4.session != "" || s4.forkSource != orig {
		t.Fatalf("restore: %q %v session=%q", args, err, s4.session)
	}
}

// The mode the copy goes on in is the one this session's own claude last
// reported: its Stop hook says the mode at the end of each turn, so a change
// made while the move waited for the response counts (either way), and the
// mode row the other side wrote when it opened the conversation never does.
func TestMoveKeepsTheModeTheSessionReported(t *testing.T) {
	for _, c := range []struct {
		name, holder, before, atStop, want string
	}{
		{"to plan while the response ran", "cli", "acceptEdits", "plan", "plan"},
		{"out of bypass while the response ran", "cli", "bypassPermissions", "default", "default"},
		{"no turn ended since: the mode before the other side came", "cli", "acceptEdits", "", "acceptEdits"},
		{"the desktop app", "claude-desktop", "acceptEdits", "plan", "plan"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, home, orig := heldSession(t)
			appendRow(t, orig, c.before)
			ownEntry(t, s, "busy", time.Now().UnixMilli())
			now := time.Now()
			s.lookElsewhere(now) // nobody else yet
			holderEntry(t, home, heldSID, c.holder, 0)
			appendRow(t, orig, "bypassPermissions") // what a terminal's claude writes, even one that only looks
			s.lookElsewhere(now.Add(5 * time.Second))
			if s.held == nil {
				t.Fatal("not held")
			}
			s.moveWhenIdle("busy", true, now.Add(5*time.Second))
			if c.atStop != "" {
				// The user changed the mode in baton's window while Claude answered.
				emit(t, s, "Stop", s.claudePID, map[string]any{"session_id": heldSID, "transcript_path": orig, "permission_mode": c.atStop})
				s.handleEvents()
			}
			s.moveWhenIdle("idle", true, now.Add(6*time.Second))
			s.moveWhenIdle("idle", true, now.Add(10*time.Second))
			if s.pend == nil {
				t.Fatal("did not move")
			}
			s.prepareSwitch(s.pend, &s.acct)
			args, _ := s.relaunchArgs(&pending{reason: reasonElsewhere})
			if want := []string{"--permission-mode", c.want, "--resume", orig, "--fork-session"}; !slices.Equal(args, want) {
				t.Fatalf("relaunch %q\nwant %q", args, want)
			}
		})
	}
}

// A session cut off while it ran in acceptEdits; since then a terminal's
// claude opened the conversation and wrote bypassPermissions into it.
// Reopening it goes on in a copy in the mode the run saved as its own, never
// the terminal's. A run saved before baton kept the mode has only the
// transcript to go by.
func TestRestoreKeepsTheRunsOwnMode(t *testing.T) {
	s, home, orig := heldSession(t)
	s.saveJournal()
	j, err := run.LoadJournal(s.runDir)
	if err != nil || j.PermissionMode != "acceptEdits" {
		t.Fatalf("saved mode %q (%v)", j.PermissionMode, err)
	}
	holderEntry(t, home, heldSID, "cli", 0)
	appendRow(t, orig, "bypassPermissions")
	r := &run.Journal{RunID: "20261109-090000-aaaaaa", TranscriptPath: orig, SessionID: heldSID, PermissionMode: j.PermissionMode}
	s2 := newTest(t)
	s2.opt.Out = &bytes.Buffer{}
	args, err := s2.restoreArgs(r)
	if want := []string{"--permission-mode", "acceptEdits", "--resume", orig, "--fork-session"}; err != nil || !slices.Equal(args, want) {
		t.Fatalf("restore %q %v\nwant %q", args, err, want)
	}
	r.PermissionMode = ""
	s3 := newTest(t)
	s3.opt.Out = &bytes.Buffer{}
	if args, _ := s3.restoreArgs(r); !slices.Contains(args, "bypassPermissions") {
		t.Fatalf("an old run: %q", args)
	}

	// While another Claude Code has it open, what the session saves is its own
	// mode, not a row the other side wrote.
	s4, home4, orig4 := heldSession(t)
	ownEntry(t, s4, "idle", time.Now().UnixMilli())
	s4.lookElsewhere(time.Now())
	holderEntry(t, home4, heldSID, "cli", 0)
	appendRow(t, orig4, "bypassPermissions")
	s4.lookElsewhere(time.Now().Add(5 * time.Second))
	s4.saveJournal()
	if j, _ := run.LoadJournal(s4.runDir); j.PermissionMode != "acceptEdits" {
		t.Fatalf("saved while held: %q", j.PermissionMode)
	}
}

// The other side closes the conversation and opens it again hours later:
// the move waits for Claude to be idle a moment again (a usage limit can be
// heard first), not at once on an idle seen before the first hold ended.
func TestHoldThatComesBackWaitsForIdleAgain(t *testing.T) {
	s, home, _ := heldSession(t)
	ownEntry(t, s, "idle", time.Now().UnixMilli())
	now := time.Now()
	holderEntry(t, home, heldSID, "claude-desktop", 0)
	s.lookElsewhere(now)
	s.moveWhenIdle("idle", true, now) // idle from here
	_ = os.Remove(filepath.Join(home, "sessions", strconv.Itoa(os.Getpid())+".json"))
	s.lookElsewhere(now.Add(time.Second * 4))
	if s.held != nil {
		t.Fatal("still held")
	}
	later := now.Add(2 * time.Hour)
	holderEntry(t, home, heldSID, "claude-desktop", 0)
	s.lookElsewhere(later)
	s.moveWhenIdle("idle", true, later)
	if s.pend != nil {
		t.Fatal("moved without waiting for a moment of idle")
	}
	s.moveWhenIdle("idle", true, later.Add(settleTime))
	if s.pend == nil || s.pend.reason != reasonElsewhere {
		t.Fatalf("pend %+v", s.pend)
	}
}

// What has the conversation open changes while Claude answers (one terminal
// closes as another opens it): still one notice that the move waits for the
// answer, and the move still comes once Claude is idle.
func TestAnotherHolderMeanwhileIsNotNewsAgain(t *testing.T) {
	s, home, _ := heldSession(t)
	ownEntry(t, s, "busy", time.Now().UnixMilli())
	child := exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >nul")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	for i := 0; i < 50 && winproc.ProcessCreated(child.Process.Pid).IsZero(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	mark := notes()
	now := time.Now()
	holderEntry(t, home, heldSID, "cli", 0)
	s.lookElsewhere(now)
	s.moveWhenIdle("busy", true, now)
	// The first one goes as the second comes, between two looks.
	other := child.Process.Pid
	_ = os.Remove(filepath.Join(home, "sessions", strconv.Itoa(os.Getpid())+".json"))
	if err := os.WriteFile(filepath.Join(home, "sessions", strconv.Itoa(other)+".json"), []byte(`{"pid":`+strconv.Itoa(other)+
		`,"sessionId":"`+heldSID+`","entrypoint":"claude-vscode","startedAt":`+strconv.FormatInt(winproc.ProcessCreated(other).UnixMilli(), 10)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.lookElsewhere(now.Add(4 * time.Second))
	if s.held == nil || s.held.PID != other {
		t.Skipf("cannot fake the second claude here: %+v", s.held)
	}
	s.moveWhenIdle("busy", true, now.Add(4*time.Second))
	if added := notesSince(mark); strings.Count(added, `"tag":"elsewhere"`) != 1 {
		t.Fatalf("notifications: %s", added)
	}
	s.moveWhenIdle("idle", true, now.Add(5*time.Second))
	s.moveWhenIdle("idle", true, now.Add(9*time.Second))
	if s.pend == nil || s.pend.holder == nil || s.pend.holder.PID != other {
		t.Fatalf("pend %+v", s.pend)
	}
}

// A path typed by hand in upper case is the same conversation: a start finds
// it open elsewhere all the same.
func TestStartChecksAPathInAnyCase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	orig := writeTranscript(t, home, heldSID)
	upper := filepath.Join(filepath.Dir(orig), strings.ToUpper(heldSID)+".JSONL")
	holderEntry(t, home, heldSID, "claude-desktop", 0)
	s := newTest(t, "--resume", upper)
	s.opt.Out = &bytes.Buffer{}
	args, err := s.firstArgs()
	if err != nil || !slices.Equal(args, []string{"--fork-session", "--resume", upper}) || s.forkSource != upper {
		t.Fatalf("%q %v fork=%q", args, err, s.forkSource)
	}
	if h := claude.Holders([]string{home}, strings.ToUpper(heldSID)); len(h) == 0 {
		t.Fatal("Holders: an id in upper case")
	}
}

// --fork-session goes in front of the resume option, never after a "--".
func TestAsCopy(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{[]string{"--resume", "x.jsonl"}, []string{"--fork-session", "--resume", "x.jsonl"}},
		{[]string{"--add-dir", "a", "b", "--resume", "x.jsonl", "--", "-go"}, []string{"--add-dir", "a", "b", "--fork-session", "--resume", "x.jsonl", "--", "-go"}},
		{[]string{"fix it", "--", "-go"}, []string{"fix it", "--fork-session", "--", "-go"}},
	} {
		if got := claude.AsCopy(c.in); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q", c.in, got)
		}
	}
}
