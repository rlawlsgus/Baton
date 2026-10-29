// Package supervisor runs one interactive Claude Code session and moves it to
// another account when the current one hits its usage limit:
//
//	StopFailure hook (error=rate_limit) -> wait until claude is idle ->
//	CTRL_BREAK (claude saves and exits) -> restore the console ->
//	claude --resume "<transcript.jsonl>" "<continue prompt>" on the next profile.
//
// Resuming by absolute transcript path (not by id) is what makes this work
// across CLAUDE_CONFIG_DIRs: the session id stays the same and every account
// appends to the one transcript file.
package supervisor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/classify"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/envx"
	"baton/internal/fsx"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/sched"
	"baton/internal/share"
	"baton/internal/winproc"
	"baton/internal/winui"
)

type Options struct {
	Paths      app.Paths
	Config     config.Config
	ClaudeExe  string
	BatonExe   string
	Account    string // --account; empty lets baton pick
	ClaudeArgs claude.Args
	Cwd        string
	Out        io.Writer // notices between claude runs (stderr)
	In         io.Reader // confirm prompts (stdin)
	// Attention, when set, is set to true when the session ended without the
	// user asking (no usable account, every account limited, a failed start):
	// its window must stay open so the reason can be read.
	Attention *bool
}

type reason string

const (
	reasonLimit  reason = "usage_limit"
	reasonAuth   reason = "authentication_failed"
	reasonManual reason = "manual"
	reasonStop   reason = "stop" // `baton stop` / the GUI: end the session, no switch
)

type pending struct {
	reason reason
	target string // a specific account the user asked for
	detail string // message claude showed
	reset  time.Time
}

type Supervisor struct {
	opt       Options
	userEnded bool // the user ended the session (Ctrl+C, `baton stop`)
	abandoned bool // it ended without the user asking: see Options.Attention
	reg       accounts.Registry
	console   *winproc.ConsoleState
	runID     string
	runDir    string
	journal   run.Journal
	tail      *run.Tail
	guard     sched.RateGuard
	in        *bufio.Reader
	signals   chan os.Signal
	logf      func(format string, a ...any)

	acct       accounts.Account
	child      *winproc.Child
	launchedAt time.Time
	claudePID  int  // claude's pid as hooks report it (differs from child.PID behind a shim)
	started    bool // SessionStart seen for the current child
	anyStart   bool // SessionStart seen at all in this run, i.e. hooks work
	userQuit   bool // SessionEnd(prompt_input_exit) for the current child
	contOwed   bool // current child got the continue prompt and has not finished a turn yet
	session    string
	transcr    string
	forkSource string // transcript a --fork-session start forked from, until the fork has a file
	modeFile   string // transcript the current child resumed, and its size at launch:
	modeFrom   int64  // permission-mode rows before that offset are from an earlier run
	knownMode  string // the mode baton last knew the session in (set at relaunch, kept across /clear)
	pend       *pending
	declined   bool                           // the user said no to a confirm prompt
	broke      bool                           // the last stop interrupted a busy turn
	marks      map[string]ledger.AccountState // marks the shared ledger could not record
	lastState  *ledger.State                  // the ledger as last read, for when it cannot be read
	blocked    []string                       // accounts marked blocked during this run
}

// Run supervises until the user quits claude (returning claude's exit code)
// or baton has to stop.
func Run(opt Options) (int, error) {
	reg, err := accounts.Load(opt.Paths.Accounts())
	if err != nil {
		return 1, err
	}
	if len(reg.Accounts) == 0 {
		return 1, errors.New("no accounts yet; add one with: baton add <name>")
	}
	// Without the ledger baton would treat limited and blocked accounts as fine.
	if _, err := ledger.Load(opt.Paths.State()); err != nil {
		return 1, fmt.Errorf("%s is unreadable (%v); fix or delete it (it records usage limits and blocked accounts)",
			opt.Paths.State(), err)
	}
	console, err := winproc.SaveConsole()
	if err != nil {
		return 1, fmt.Errorf("baton run must be started from a console window (Windows Terminal, PowerShell, cmd): %w", err)
	}
	defer console.Close()

	s := &Supervisor{
		opt:     opt,
		reg:     reg,
		console: console,
		guard: sched.RateGuard{
			Cooldown:   time.Duration(opt.Config.CooldownSeconds) * time.Second,
			MaxPerHour: opt.Config.MaxSwitchesPerHour,
		},
		in:      bufio.NewReader(opt.In),
		signals: make(chan os.Signal, 4),
	}
	// Ctrl+C reaches claude as a key while its TUI runs (raw console). In the
	// gaps between runs it would become a console event and kill baton, which
	// would orphan the session, so baton catches it. It stops a wait or a
	// question, so it must arrive even if baton's parent had it switched off.
	winproc.EnableCtrlC()
	// SIGTERM is the window being closed: Windows ends baton a few seconds
	// later, which leaves time to record that the session ended.
	signal.Notify(s.signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(s.signals)

	if err := s.prepareRun(); err != nil {
		return 1, err
	}
	defer s.finish()
	defer s.recoverPanic()
	code, err := s.loop()
	if opt.Attention != nil && code == 0 && err == nil && s.abandoned {
		*opt.Attention = true
	}
	return code, err
}

// recoverPanic keeps a baton bug from orphaning claude on the shared console:
// it stops claude cleanly, restores the console, then re-raises the panic.
func (s *Supervisor) recoverPanic() {
	r := recover()
	if r == nil {
		return
	}
	s.logf("panic: %v", r)
	if s.child != nil && !s.child.Exited() {
		_ = s.child.CtrlBreak()
		if !s.child.WaitExit(15 * time.Second) {
			s.child.Kill()
		}
	}
	s.console.Restore()
	s.console.ResetTerminal()
	s.printResumeHint()
	panic(r)
}

func (s *Supervisor) prepareRun() error {
	if err := s.opt.Paths.Ensure(); err != nil {
		return err
	}
	pruneRuns(s.opt.Paths.RunsDir(), 30*24*time.Hour)
	now := time.Now()
	s.runID = run.NewID(now)
	s.runDir = s.opt.Paths.Run(s.runID)
	if err := os.MkdirAll(s.runDir, 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(s.runDir, "baton.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.logf = func(format string, a ...any) {
		fmt.Fprintf(logFile, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, a...)...)
	}
	if err := claude.WriteRunSettings(run.SettingsPath(s.runDir), s.opt.BatonExe, s.runDir, nil); err != nil {
		return err
	}
	s.tail = run.NewTail(s.runDir)
	s.journal = run.Journal{
		RunID:             s.runID,
		SupervisorPID:     os.Getpid(),
		SupervisorCreated: winproc.ProcessCreated(os.Getpid()),
		State:             run.StateRunning,
		Cwd:               s.opt.Cwd,
		Started:           now,
	}
	s.logf("run %s cwd=%q claude=%q", s.runID, s.opt.Cwd, s.opt.ClaudeExe)
	return nil
}

func (s *Supervisor) finish() {
	s.journal.State = run.StateDone
	s.saveJournal()
}

func (s *Supervisor) saveJournal() {
	s.journal.Account = s.acct.ID
	s.journal.SwitchMode = s.opt.Config.SwitchMode // this run's own: it only ever gets stricter
	s.journal.SessionID = s.session
	s.journal.TranscriptPath = s.transcr
	if s.child != nil {
		s.journal.ChildPID = s.child.PID
	}
	if err := run.SaveJournal(s.runDir, s.journal); err != nil {
		s.logf("journal: %v", err)
	}
}

func (s *Supervisor) notice(format string, a ...any) {
	fmt.Fprintf(s.opt.Out, "baton: "+tr(format)+"\n", a...)
	s.logf("notice: "+format, a...)
}

func (s *Supervisor) loop() (int, error) {
	first, ok := s.initialAccount()
	if !ok {
		if s.userEnded {
			return 0, nil // stopped while waiting for a first account
		}
		return 1, nil
	}
	s.acct = *first
	s.warnBeforeStart()
	s.copyTrustForFirstLaunch()
	args, err := s.firstArgs()
	if err != nil {
		return 1, err
	}
	withPrompt := false
	for {
		if err := s.launch(args, withPrompt); err != nil {
			s.printResumeHint()
			return 1, err
		}
		if exited := s.supervise(); exited {
			return s.childExited(), nil
		}
		p := s.pend
		s.pend = nil
		if s.stopChild(p) {
			// claude ended by itself while baton waited for it to go idle:
			// the user quit, which wins over the pending switch.
			return s.childExited(), nil
		}
		if p.reason == reasonStop {
			s.notice("session ended as requested")
			s.printResumeHint()
			return 0, nil
		}
		next, ok := s.chooseNext(p)
		if !ok || s.stopRequested() {
			s.abandoned = !s.userEnded // e.g. every account limited
			s.printResumeHint()
			return 0, nil
		}
		if !s.prepareSwitch(p, next) {
			s.printResumeHint()
			return 1, nil
		}
		args, withPrompt = s.relaunchArgs(p)
		// Preparing can take seconds (a holder check): a stop that came in
		// meanwhile must not start the next account.
		if s.stopRequested() {
			s.printResumeHint()
			return 0, nil
		}
	}
}

// warnBeforeStart reports account problems while the console still belongs
// to baton. (Hook blocking is checked before every launch, in launch.)
func (s *Supervisor) warnBeforeStart() {
	if m := s.acct.AuthMethod; m != "" && m != "claude.ai" {
		s.notice("warning: account %q signs in with %q, not a Claude subscription", s.acct.ID, m)
	}
	if !s.acct.LoggedIn {
		s.notice("warning: account %q was not signed in at the last check (sign in with: baton login %s)", s.acct.ID, s.acct.ID)
	}
}

// childExited handles claude ending on its own (user quit, crash, external kill).
func (s *Supervisor) childExited() int {
	code := s.child.ExitCode()
	s.child.Close()
	s.logf("claude exited by itself, code %d, user quit %v", code, s.userQuit)
	s.console.Restore()
	if !s.userQuit {
		// A crash or kill skips claude's own terminal cleanup.
		s.console.ResetTerminal()
	}
	switch {
	case s.started:
	case code != 0 && time.Since(s.launchedAt) < 10*time.Second:
		s.notice("Claude Code exited during startup (exit code %d); see its message above", code)
	default:
		s.notice("Claude Code never reported to baton on %q, so usage limits could not be detected there "+
			"(hooks disabled by settings or policy, or the folder trust prompt was not answered)", s.acct.ID)
		s.abandoned = true // worth reading even after a clean quit
	}
	s.printResumeHint()
	return code
}

// ---- launching -------------------------------------------------------------

// firstArgs is the user's command line in their own order (claude parses
// variadic options greedily, so regrouping could swallow the prompt), with
// baton's generated --session-id in front.
func (s *Supervisor) firstArgs() ([]string, error) {
	a := s.opt.ClaudeArgs
	args := append([]string{}, a.Original...)
	if a.Resumes {
		if p := a.ResumeArg; p != "" && fileExists(p) && strings.EqualFold(filepath.Ext(p), ".jsonl") {
			if claude.HasFlag(a.FirstOnly, "--fork-session") {
				s.forkSource = p
				return args, nil
			}
			// Track the user's transcript from the start: on a resume-by-path
			// claude's SessionStart reports a path that does not exist.
			s.transcr = p
			s.session = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			if h := claude.Holders(s.holderDirs(), s.session); len(h) > 0 {
				return nil, fmt.Errorf("this session is already open in another claude window (pid %d); close it first", h[0].PID)
			}
		}
		return args, nil
	}
	for i, t := range a.FirstOnly {
		if t == "--session-id" && i+1 < len(a.FirstOnly) {
			s.session = a.FirstOnly[i+1]
		} else if v, ok := strings.CutPrefix(t, "--session-id="); ok {
			s.session = v
		}
	}
	if s.session == "" {
		s.session = run.NewSessionID()
		args = append([]string{"--session-id", s.session}, args...)
	}
	return args, nil
}

// relaunchArgs builds the next launch; it reports whether it includes the
// continue prompt.
func (s *Supervisor) relaunchArgs(p *pending) ([]string, bool) {
	args := append([]string{}, s.opt.ClaudeArgs.Persistent...)
	if t := s.currentTranscript(); t != "" {
		// claude does not restore the permission mode on a resume; without this
		// the next account would fall back to its default mode (and an
		// unattended continue could stop at the first approval prompt).
		args = s.withMode(args, s.permissionMode(t))
		args = append(args, "--resume", t)
		var cp string
		switch {
		case (p.reason == reasonLimit || p.reason == reasonAuth) && !s.declined, p.reason == reasonManual && s.contOwed:
			cp = s.opt.Config.ContinuePrompt
		case s.broke:
			cp = restartPrompt // a manual switch had to interrupt a running turn
		}
		if cp != "" {
			if strings.HasPrefix(cp, "-") {
				args = append(args, "--", cp) // never let the prompt be read as an option
			} else {
				args = append(args, cp)
			}
		}
		return args, cp != ""
	}
	if s.forkSource != "" && fileExists(s.forkSource) {
		// A fork that has no turn yet has no file: fork the source again here,
		// in the fork's own mode (the source's rows describe another session).
		args = s.withMode(args, s.permissionMode(""))
		return append(args, "--resume", s.forkSource, "--fork-session"), false
	}
	// No turn finished anywhere yet, so there is no transcript: start fresh
	// under the same session id on the new account, in the mode it started in.
	args = s.withMode(args, s.permissionMode(""))
	if s.session != "" {
		args = append(args, "--session-id", s.session)
	}
	return args, false
}

const restartPrompt = "The session was restarted while you were working. " +
	"Continue the task you were working on; do not repeat work that is already complete."

// permissionMode is the mode the session is in as far as baton can tell: the
// last permission-mode row claude wrote since this launch (it writes one per
// prompt), else the mode baton last knew (from a relaunch, or from before a
// /clear), else the --permission-mode the user started with.
func (s *Supervisor) permissionMode(transcript string) string {
	if m := s.modeSinceLaunch(transcript); m != "" {
		return m
	}
	if s.knownMode != "" {
		return s.knownMode
	}
	fo := s.opt.ClaudeArgs.FirstOnly
	for i, t := range fo {
		if t == "--permission-mode" && i+1 < len(fo) {
			return fo[i+1]
		}
		if v, ok := strings.CutPrefix(t, "--permission-mode="); ok {
			return v
		}
	}
	return ""
}

// noteModeOffset records where the transcript about to be resumed ends: mode
// rows before that point are from earlier runs.
func (s *Supervisor) noteModeOffset() {
	s.modeFile, s.modeFrom = "", 0
	if s.transcr != "" {
		if fi, err := os.Stat(s.transcr); err == nil {
			s.modeFile, s.modeFrom = s.transcr, fi.Size()
		}
	}
}

// modeSinceLaunch reads the transcript's permission-mode rows, skipping those
// written before the current child started: claude did not restore that mode.
func (s *Supervisor) modeSinceLaunch(transcript string) string {
	if transcript == "" {
		return ""
	}
	var from int64
	if strings.EqualFold(transcript, s.modeFile) {
		from = s.modeFrom
	}
	return claude.LastPermissionModeFrom(transcript, from)
}

// withMode adds the permission mode the session continues in. A
// --dangerously-skip-permissions from the start would override it, so once the
// session has left bypass mode that flag becomes
// --allow-dangerously-skip-permissions, which only keeps bypass on offer.
func (s *Supervisor) withMode(args []string, mode string) []string {
	if mode == "" {
		return args
	}
	s.knownMode = mode
	if mode != "bypassPermissions" && claude.HasFlag(args, "--dangerously-skip-permissions") {
		out := make([]string, 0, len(args)+1)
		for _, t := range args {
			if t != "--dangerously-skip-permissions" {
				out = append(out, t)
			}
		}
		if !claude.HasFlag(out, "--allow-dangerously-skip-permissions") {
			out = append(out, "--allow-dangerously-skip-permissions")
		}
		args = out
	}
	return append(args, "--permission-mode", mode)
}

// bypassRequested reports whether a launch asks for bypass-permissions mode.
func bypassRequested(args []string) bool {
	for i, t := range args {
		switch {
		case t == "--dangerously-skip-permissions" || t == "--allow-dangerously-skip-permissions":
			return true
		case t == "--permission-mode" && i+1 < len(args) && args[i+1] == "bypassPermissions",
			t == "--permission-mode=bypassPermissions":
			return true
		}
	}
	return false
}

func (s *Supervisor) currentTranscript() string {
	if s.transcr != "" && fileExists(s.transcr) {
		return s.transcr
	}
	if s.session != "" {
		if t := claude.FindTranscript(s.profileDirs(), s.session); t != "" {
			s.transcr = t
			s.forkSource = ""
			return t
		}
	}
	return ""
}

func (s *Supervisor) launch(args []string, withPrompt bool) error {
	s.reloadConfig()
	if !fileExists(s.opt.ClaudeExe) {
		// The desktop app keeps its Claude Code in a folder named after the
		// version; an app update can remove the old one between two launches.
		base, _ := envx.Build(os.Environ(), nil)
		if inst, _, err := claude.ResolveUsable(s.opt.Config.ClaudePath, s.opt.Config.DesktopBundleAllowed(), base); err == nil {
			s.notice("Claude Code moved (app update?); now using %s", inst.Path)
			s.opt.ClaudeExe = inst.Path
		}
	}
	if s.opt.Config.ShareEnabled() && !claude.BusyProfile(s.acct.ProfileDir) {
		if res, err := share.Sync(share.UserHome(s.opt.Paths.ProfilesDir()), s.acct.ProfileDir); err != nil {
			s.logf("share %s: %v", s.acct.ID, err)
		} else if len(res.Linked)+len(res.Copied) > 0 {
			s.logf("share %s: linked=%v copied=%v kept=%v", s.acct.ID, res.Linked, res.Copied, res.Kept)
		}
	}
	// Checked after sharing, for every account: a disableAllHooks copied in or
	// set on this profile would leave baton blind on it.
	if why := claude.HooksBlocked(s.acct.ProfileDir, s.opt.Cwd); why != "" {
		s.notice("warning: %s, so Claude Code will not report usage limits to baton on %q", why, s.acct.ID)
	}
	// A `baton switch` written while baton was switching is stale now.
	if req := s.takeSwitch(); req != nil {
		s.notice("ignored a `baton switch` request made while the previous switch was in progress")
	}
	// The user accepted the bypass-permissions warning once already (on some
	// account); carry that over so the new account does not stop on it.
	extra := map[string]any{}
	if bypassRequested(args) && claude.BypassAccepted(append(s.profileDirs(), share.UserHome(s.opt.Paths.ProfilesDir()))...) {
		extra["skipDangerousModePermissionPrompt"] = true
	}
	// baton's status line shows the account and its usage, and records the
	// usage for the baton window; a status line of the user's own stays.
	if !claude.UserStatusLine(s.acct.ProfileDir, s.opt.Cwd) {
		extra["statusLine"] = claude.StatusLine(s.opt.BatonExe)
	}
	if err := claude.WriteRunSettings(run.SettingsPath(s.runDir), s.opt.BatonExe, s.runDir, extra); err != nil {
		return err
	}
	env, _ := envx.ForProfile(s.acct.ProfileDir, map[string]string{"BATON_RUN": s.runID})
	s.noteModeOffset()
	winui.SetConsoleTitle(s.title(""))
	// baton's own options go first so a user "--" can never swallow them.
	full := append([]string{"--settings", run.SettingsPath(s.runDir)}, args...)
	s.logf("launch account=%s args=%q", s.acct.ID, full)
	s.launchedAt = time.Now()
	child, err := winproc.Start(s.opt.ClaudeExe, full, env, s.opt.Cwd)
	if err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	s.child = child
	s.claudePID, s.started, s.userQuit = 0, false, false
	s.declined, s.broke, s.pend = false, false, nil
	s.contOwed = withPrompt
	s.journal.State = run.StateRunning
	s.saveJournal()
	_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
		a := st.Account(s.acct.ID)
		if a.Status == ledger.StatusExhausted && !time.Now().Before(a.RetryAt) {
			a.Status = ledger.StatusOK
		}
		return nil
	})
	return nil
}

// supervise watches hook events and switch requests until claude exits by
// itself (true) or a switch is pending (false).
func (s *Supervisor) supervise() bool {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.child.Done():
			s.handleEvents()
			return true
		case sig := <-s.signals:
			// Ctrl+C while claude starts or exits; never let it kill baton.
			if sig == syscall.SIGTERM {
				s.logf("the session window is closing")
				s.finish() // claude is closing with it; baton has seconds left
			}
		case <-tick.C:
			s.handleEvents()
			if run.TakeStop(s.runDir) {
				s.logf("stop requested")
				s.pend = &pending{reason: reasonStop}
				return false
			}
			if req := s.takeSwitch(); req != nil {
				switch {
				case s.pend != nil:
					if req.Target != "" {
						s.pend.target = req.Target // the user's choice beats the automatic pick
					}
				case s.manualTargetUsable(req.Target):
					s.pend = &pending{reason: reasonManual, target: req.Target}
				}
			}
			if s.pend != nil {
				return false
			}
		}
	}
}

func (s *Supervisor) takeSwitch() *run.SwitchRequest {
	req, err := run.TakeSwitch(s.runDir)
	if err != nil {
		s.logf("switch request: %v", err)
		return nil
	}
	if req != nil {
		s.logf("switch requested target=%q", req.Target)
	}
	return req
}

// manualTargetUsable checks a `baton switch` request against the current
// registry before claude is stopped for it.
func (s *Supervisor) manualTargetUsable(target string) bool {
	s.reloadRegistry()
	if target == "" {
		best, _, _ := sched.Pick(s.reg, s.loadState(), time.Now(), s.acct.ID)
		if best == nil {
			s.logf("switch request ignored: no other usable account")
		}
		return best != nil
	}
	if a := s.reg.Get(target); a == nil || target == s.acct.ID || !s.usable(a) {
		// (Retrying the current account is only offered while waiting; see waitUntil.)
		s.logf("switch request ignored: %q is unknown, current, disabled, signed out or blocked", target)
		return false
	}
	return true
}

// usable: enabled, signed in and not blocked (a limited account still counts:
// the user may know better).
func (s *Supervisor) usable(a *accounts.Account) bool {
	st := s.loadState().StatusOf(a.ID).Status
	return a.Enabled && a.LoggedIn && st != ledger.StatusBlocked && st != ledger.StatusNeedsLogin
}

func (s *Supervisor) handleEvents() {
	for _, ev := range s.tail.Next() {
		if ev.TS.Before(s.launchedAt) {
			continue // from a previous claude
		}
		if ev.ClaudePID != 0 {
			if s.claudePID == 0 {
				s.claudePID = ev.ClaudePID
			} else if ev.ClaudePID != s.claudePID {
				continue
			}
		}
		p := ev.Decode()
		s.logf("event %s session=%s source=%s reason=%s error=%s", ev.Event, p.SessionID, p.Source, p.Reason, p.Error)
		switch ev.Event {
		case "SessionStart":
			s.onSessionStart(p)
		case "Stop":
			s.contOwed = false
			s.adoptTranscript(p)
		case "StopFailure":
			s.contOwed = false
			s.onStopFailure(p)
		case "SessionEnd":
			s.adoptTranscript(p)
			if p.Reason == "prompt_input_exit" || p.Reason == "logout" {
				s.userQuit = true
			}
		}
	}
}

// adoptTranscript takes a reported transcript path when it really exists
// (only SessionStart on a resume-by-path reports a wrong one).
func (s *Supervisor) adoptTranscript(p run.Payload) {
	if p.SessionID != "" {
		s.session = p.SessionID
	}
	if p.TranscriptPath != "" && fileExists(p.TranscriptPath) {
		s.transcr = p.TranscriptPath
		s.forkSource = "" // a fork that has its own file is resumed like any session
	}
}

func (s *Supervisor) onSessionStart(p run.Payload) {
	s.started, s.anyStart = true, true
	changed := p.SessionID != "" && p.SessionID != s.session
	if p.SessionID != "" {
		s.session = p.SessionID
	}
	switch {
	case p.Source != "resume" && p.TranscriptPath != "":
		// startup / clear / compact: the path is in the current profile
		// (the file itself appears after the first turn).
		if p.Source == "clear" {
			// /clear starts an empty transcript; the session is no longer the
			// fork it may have been.
			s.leaveTranscript(p.TranscriptPath)
			s.forkSource = ""
		}
		s.transcr = p.TranscriptPath
	case changed || s.transcr == "" || !fileExists(s.transcr):
		// On resume-by-path claude reports a path in the *new* profile that
		// never exists; look for the real file instead.
		if t := claude.FindTranscript(s.profileDirs(), s.session); t != "" {
			s.leaveTranscript(t) // an in-session /resume
			s.transcr = t
			if !strings.EqualFold(t, s.modeFile) {
				// Picked after launch (-r picker, /resume): the mode rows already
				// in it are from earlier runs.
				s.noteModeOffset()
			}
		}
	}
	s.saveJournal()
}

// leaveTranscript remembers the mode of the conversation the session is
// leaving for next (/clear, /resume): claude keeps its mode, and next has no
// row for it yet.
func (s *Supervisor) leaveTranscript(next string) {
	if s.transcr != "" && !strings.EqualFold(s.transcr, next) {
		if m := s.modeSinceLaunch(s.transcr); m != "" {
			s.knownMode = m
		}
	}
}

func (s *Supervisor) onStopFailure(p run.Payload) {
	s.adoptTranscript(p)
	s.reloadConfig() // e.g. switch_mode turned off since the run started
	kind := classify.Error(p.Error, p.LastAssistantMessage)
	s.logf("stop failure classified as %s", kind)
	now := time.Now()
	switch kind {
	case classify.UsageLimit:
		reset, ok := classify.ResetTime(p.LastAssistantMessage, now)
		if !ok {
			reset = now.Add(time.Duration(s.opt.Config.ExhaustedRetryMinutes) * time.Minute)
		}
		s.markAccount(ledger.StatusExhausted, reset, p.LastAssistantMessage)
		if s.opt.Config.SwitchMode != config.ModeOff && s.pend == nil {
			s.pend = &pending{reason: reasonLimit, detail: p.LastAssistantMessage, reset: reset}
		}
	case classify.NeedsLogin:
		s.markAccount(ledger.StatusNeedsLogin, time.Time{}, p.LastAssistantMessage)
		if s.opt.Config.SwitchMode != config.ModeOff && s.pend == nil {
			s.pend = &pending{reason: reasonAuth, detail: p.LastAssistantMessage}
		}
	case classify.Blocked:
		// Never move to another account here; the user must look at it.
		s.markAccount(ledger.StatusBlocked, time.Time{}, p.LastAssistantMessage)
	}
	s.saveJournal()
}

// markAccount records an account's state in the shared ledger, and also in
// memory: if another program holds state.json so the write fails, this run
// still knows not to switch back to the limited account.
func (s *Supervisor) markAccount(status string, retry time.Time, detail string) {
	as := ledger.AccountState{Status: status, LimitedAt: time.Now(), RetryAt: retry, Detail: truncate(detail, 300)}
	if s.marks == nil {
		s.marks = map[string]ledger.AccountState{}
	}
	err := ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
		cur := st.Account(s.acct.ID)
		if retry.IsZero() && time.Now().Before(cur.RetryAt) {
			// A login or block problem on a limited account: keep the limit, so
			// fixing the problem later does not make the account look free.
			as.RetryAt = cur.RetryAt
		}
		*cur = as
		return nil
	})
	if err != nil {
		// Kept in memory only when the shared write failed; otherwise the
		// ledger is the truth, so a later `baton login`/`enable` wins.
		s.marks[s.acct.ID] = as
		s.logf("ledger: %v", err)
	} else {
		delete(s.marks, s.acct.ID)
	}
	if status == ledger.StatusBlocked && !slices.Contains(s.blocked, s.acct.ID) {
		s.blocked = append(s.blocked, s.acct.ID)
	}
}

// ---- stopping --------------------------------------------------------------

// stopChild ends claude gracefully: it waits until claude is fully started (a
// break during startup leaves the terminal in the alternate screen) and idle,
// then sends CTRL_BREAK; a kill is the last resort. It returns true when
// claude exited on its own first.
func (s *Supervisor) stopChild(p *pending) bool {
	s.journal.State = run.StateStopping
	if p.reason == reasonStop {
		s.journal.State = run.StateEnding
	}
	s.saveJournal()
	s.reloadConfig() // e.g. switched to confirm mode since this claude started
	c := s.child
	wait := 10 * time.Second
	switch {
	case p.reason == reasonManual && s.opt.Config.SwitchMode == config.ModeConfirm:
		wait = 30 * time.Minute // the user will be asked anyway; never cut a turn short for it
	case p.reason == reasonManual:
		wait = 60 * time.Second // let the current turn finish
	}
	start := time.Now()
	deadline := start.Add(wait)
	busy, misses := false, 0
	for time.Now().Before(deadline) && !c.Exited() {
		if !s.started && time.Since(start) > 60*time.Second {
			break // claude never reported (hooks blocked, dialog open): no turn to wait for
		}
		s.handleEvents()
		if req := s.takeSwitch(); req != nil && req.Target != "" {
			p.target = req.Target // a later `baton switch <name>` changes where to go
		}
		if p.reason != reasonStop && run.TakeStop(s.runDir) {
			// `baton stop` during a switch: end instead, after at most the
			// wait a stop gets.
			s.logf("stop requested while stopping for %s", p.reason)
			p.reason = reasonStop
			s.journal.State = run.StateEnding
			s.saveJournal()
			if d := time.Now().Add(10 * time.Second); d.Before(deadline) {
				deadline = d
			}
		}
		if s.started {
			pid := s.claudePID
			if pid == 0 {
				pid = c.PID
			}
			ls, ok := claude.SessionFor(s.acct.ProfileDir, pid)
			if !ok {
				// The registry file can be mid-rewrite; only a lasting miss counts.
				if misses++; misses >= 10 {
					busy = false
					break
				}
			} else {
				misses = 0
				if busy = ls.Status != "idle"; !busy { // busy, waiting (permission prompt) or shell
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if c.Exited() {
		s.handleEvents()
		return true
	}
	// The target is settled from here on: later requests are told the session
	// is switching right now instead of being accepted and then dropped.
	s.journal.State = run.StateSwitching
	if p.reason == reasonStop {
		s.journal.State = run.StateEnding
	}
	s.saveJournal()
	if req := s.takeSwitch(); req != nil && req.Target != "" {
		p.target = req.Target
	}
	s.broke = busy
	early := !s.started
	if err := c.CtrlBreak(); err != nil {
		s.logf("ctrl-break: %v", err)
	}
	killed := false
	if !c.WaitExit(20 * time.Second) {
		s.logf("claude did not exit after CTRL_BREAK; killing it")
		c.Kill()
		c.WaitExit(5 * time.Second)
		killed = true
	}
	s.handleEvents()
	c.Close()
	s.logf("claude stopped code=%d killed=%v early=%v interrupted=%v", c.ExitCode(), killed, early, s.broke)
	s.console.Restore()
	if killed || early {
		s.console.ResetTerminal()
	}
	s.console.FlushInput()
	// A limit (or login failure) that arrived while a manual switch waited for
	// the turn to finish still owes the new account the continue prompt.
	if s.pend != nil && p.reason == reasonManual && (s.pend.reason == reasonLimit || s.pend.reason == reasonAuth) {
		p.reason, p.detail, p.reset = s.pend.reason, s.pend.detail, s.pend.reset
	}
	s.pend = nil
	if p.reason != reasonStop && run.TakeStop(s.runDir) {
		s.logf("stop requested while switching")
		p.reason = reasonStop
	}
	return false
}

// stopRequested takes a `baton stop` that arrived after claude was stopped
// for a switch: the session ends instead of moving on.
func (s *Supervisor) stopRequested() bool {
	if !run.TakeStop(s.runDir) {
		return false
	}
	s.logf("stop requested")
	s.notice("session ended as requested")
	s.userEnded = true
	return true
}

// ---- choosing the next account ---------------------------------------------

func (s *Supervisor) initialAccount() (*accounts.Account, bool) {
	if s.opt.Account != "" {
		a := s.reg.Get(s.opt.Account)
		if a == nil {
			s.notice("no account named %q (see: baton list)", s.opt.Account)
			return nil, false
		}
		st := s.loadState().StatusOf(a.ID)
		switch {
		case st.Status == ledger.StatusNeedsLogin:
			s.notice("account %q needs to sign in again: baton login %s", a.ID, a.ID)
			return nil, false
		case st.Status == ledger.StatusBlocked:
			s.notice("account %q was blocked (%s); once that is resolved, run: baton enable %s", a.ID, orUnknown(st.Detail), a.ID)
			return nil, false
		case st.Status == ledger.StatusExhausted && time.Now().Before(st.RetryAt):
			s.notice("account %q is limited until %s; starting on it anyway as requested", a.ID, clock(st.RetryAt))
		}
		return a, true
	}
	st := s.loadState()
	for _, a := range s.reg.Accounts {
		if a.Enabled && st.StatusOf(a.ID).Status == ledger.StatusBlocked {
			s.notice("skipping %q: marked blocked (%s); once resolved, run: baton enable %s", a.ID, orUnknown(st.StatusOf(a.ID).Detail), a.ID)
		}
	}
	a, _, ok := s.pickWithWait("", "")
	return a, ok
}

// chooseNext decides where the session continues after claude was stopped.
func (s *Supervisor) chooseNext(p *pending) (*accounts.Account, bool) {
	userAsked := false // a `baton switch` ended a rate-limit wait: the user decided
	for {
		s.reloadRegistry()
		s.reloadConfig()
		next, named, ok := s.pickFor(p)
		if !ok {
			return nil, false
		}
		s.journal.State = run.StateSwitching
		s.saveJournal()
		s.reloadConfig() // the pick may have waited for hours
		if p.reason != reasonManual && !named && s.opt.Config.SwitchMode == config.ModeOff {
			s.declined = true
			s.notice("automatic switching is off; staying on %q", s.acct.ID)
			return &s.acct, true
		}
		// Every automatic relaunch counts, onto the same account too, so an
		// account that looks free but is limited again cannot cause a restart
		// storm. After waiting, pick again: things may have changed meanwhile.
		if !named && !userAsked && p.reason != reasonManual {
			if allowed, at := s.guard.Allow(time.Now()); !allowed {
				s.notice("switching too often (at most %d per hour, %ds apart); continuing at %s (or `baton switch <name>`)",
					s.opt.Config.MaxSwitchesPerHour, s.opt.Config.CooldownSeconds, clock(at))
				s.journal.State = run.StateWaiting
				s.saveJournal()
				res := s.waitUntil(at)
				if !res.ok {
					return nil, false
				}
				p.target, userAsked = res.target, res.requested
				continue
			}
		}
		// In confirm mode nothing moves the session without a yes in this
		// window, including `baton switch` requests.
		if s.opt.Config.SwitchMode == config.ModeConfirm && next.ID != s.acct.ID {
			if s.stopRequested() {
				return nil, false
			}
			s.journal.State = run.StateAsking
			s.saveJournal()
			q := fmt.Sprintf(tr("%s. Continue this session on %q? [Y/n] "), describe(p, s.acct.ID), next.ID)
			yes, stop := s.confirm(q)
			if stop {
				fmt.Fprintln(s.opt.Out)
				s.notice("stopped")
				s.userEnded = true
				return nil, false
			}
			if s.stopRequested() {
				return nil, false // asked to end while the question was open
			}
			if !yes {
				s.declined = true
				s.notice("staying on %q", s.acct.ID)
				return &s.acct, true
			}
		}
		return next, true
	}
}

// pickFor makes the account choice for a pending switch; named reports that
// the user picked the account themselves.
func (s *Supervisor) pickFor(p *pending) (acct *accounts.Account, named bool, ok bool) {
	if p.target != "" {
		if a := s.reg.Get(p.target); a != nil && s.usable(a) {
			return a, true, true
		}
		s.notice("cannot switch to %q (unknown, disabled, signed out or blocked)", p.target)
		p.target = ""
		if p.reason == reasonManual {
			return &s.acct, true, true
		}
	}
	if p.reason == reasonManual {
		best, _, _ := sched.Pick(s.reg, s.loadState(), time.Now(), s.acct.ID)
		if best == nil {
			s.notice("no other account is available; staying on %q", s.acct.ID)
			return &s.acct, true, true
		}
		return best, false, true
	}
	return s.pickWithWait(s.acct.ID, describe(p, s.acct.ID))
}

// pickWithWait picks an eligible account, waiting for the soonest reset when
// on_exhausted is "wait". why is printed before waiting. target reports that
// the user named the account with `baton switch` during the wait.
func (s *Supervisor) pickWithWait(exclude, why string) (acct *accounts.Account, target bool, ok bool) {
	for {
		s.reloadRegistry()
		st := s.loadState()
		now := time.Now()
		best, soonest, at := sched.Pick(s.reg, st, now, exclude)
		if best == nil && exclude != "" {
			// The current account may be usable again, or the one that frees first.
			if cur := s.reg.Get(exclude); cur != nil {
				if yes, at2 := sched.Eligible(*cur, st.StatusOf(cur.ID), now); yes {
					best = cur
				} else if !at2.IsZero() && (soonest == nil || at2.Before(at)) {
					soonest, at = cur, at2
				}
			}
		}
		if best != nil {
			return best, false, true
		}
		if why != "" {
			s.notice("%s", why)
			why = ""
		}
		if soonest == nil {
			s.notice("no usable account: every account is disabled, signed out or blocked (see: baton list)")
			return nil, false, false
		}
		if s.opt.Config.OnExhausted == config.ExhaustedStop {
			s.notice("every account is limited; %q frees up first, at %s", soonest.ID, clock(at))
			return nil, false, false
		}
		s.notice("every account is limited; waiting for %q at %s (Ctrl+C to stop, or `baton switch <name>`)", soonest.ID, clock(at))
		s.journal.State = run.StateWaiting
		s.saveJournal()
		res := s.waitUntil(at.Add(30 * time.Second))
		if !res.ok {
			return nil, false, false
		}
		if res.target != "" {
			if a := s.reg.Get(res.target); a != nil && s.usable(a) {
				return a, true, true
			}
			s.notice("cannot switch to %q (unknown, disabled, signed out or blocked); still waiting", res.target)
		}
	}
}

// prepareSwitch readies the target profile and checks nobody else is writing
// the session. It returns false if the session cannot continue safely.
func (s *Supervisor) prepareSwitch(p *pending, next *accounts.Account) bool {
	from := s.acct.ID
	if next.ID != from {
		s.prepareProfile(next)
	}
	if s.session != "" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			holders := claude.Holders(s.holderDirs(), s.session)
			if len(holders) == 0 {
				break
			}
			if time.Now().After(deadline) {
				s.notice("this session is open in another claude window (pid %d); close it and run the command below", holders[0].PID)
				return false
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	now := time.Now()
	if p.reason != reasonManual && !s.declined {
		s.guard.Record(now) // a declined switch moved nothing, so it does not count
	}
	if next.ID != from {
		_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
			st.LogSwitch(ledger.Switch{At: now, From: from, To: next.ID, Session: s.session, Reason: string(p.reason)})
			return nil
		})
		s.notice("%s -> continuing on %q", describe(p, from), next.ID)
	}
	s.acct = *next
	return true
}

// prepareProfile lets an unattended relaunch start without first-run
// dialogs: onboarding is marked done, and folder trust is carried over only
// if the user already trusted this folder on one of their accounts.
func (s *Supervisor) prepareProfile(next *accounts.Account) {
	trustedSomewhere := s.trustedByUser()
	for _, a := range s.reg.Accounts {
		if claude.IsTrusted(a.ProfileDir, s.opt.Cwd) {
			trustedSomewhere = true
			break
		}
	}
	if claude.BusyProfile(next.ProfileDir) {
		if !claude.IsTrusted(next.ProfileDir, s.opt.Cwd) {
			s.notice("%q is in use elsewhere, so its folder trust could not be set; if claude asks, choose \"Yes, I trust this folder\"", next.ID)
		}
		return
	}
	res, err := claude.SeedProfile(next.ProfileDir, s.opt.Cwd, true, trustedSomewhere)
	switch {
	case err != nil:
		s.logf("seed %s: %v", next.ID, err)
	case res.Trust || res.Onboarding:
		s.logf("seeded %s onboarding=%v trust=%v", next.ID, res.Onboarding, res.Trust)
	}
	if !trustedSomewhere && !claude.IsTrusted(next.ProfileDir, s.opt.Cwd) {
		s.notice("claude may ask whether you trust this folder on %q; choose \"Yes, I trust this folder\" to continue", next.ID)
	}
}

// copyTrustForFirstLaunch carries folder trust the user already gave on one of
// their accounts over to the account the session starts on. Without it the
// trust dialog appears before any hook can run. Onboarding screens are left
// alone here: the user is present for the first launch.
func (s *Supervisor) copyTrustForFirstLaunch() {
	if claude.IsTrusted(s.acct.ProfileDir, s.opt.Cwd) {
		return
	}
	if claude.BusyProfile(s.acct.ProfileDir) {
		// Another claude owns the profile's .claude.json now.
		s.notice("%q is in use elsewhere, so its folder trust could not be set; if claude asks, choose \"Yes, I trust this folder\"", s.acct.ID)
		return
	}
	from := ""
	if s.trustedByUser() {
		from = "your own Claude Code setup"
	}
	for _, a := range s.reg.Accounts {
		if from == "" && a.ID != s.acct.ID && claude.IsTrusted(a.ProfileDir, s.opt.Cwd) {
			from = a.ID
		}
	}
	if from == "" {
		return
	}
	if _, err := claude.SeedProfile(s.acct.ProfileDir, s.opt.Cwd, false, true); err != nil {
		s.logf("copy trust to %s: %v", s.acct.ID, err)
	} else {
		s.logf("copied folder trust from %s to %s", from, s.acct.ID)
	}
}

// trustedByUser reports that the user already trusted this folder in their
// own Claude Code (the desktop app's Code tab uses the same setup).
func (s *Supervisor) trustedByUser() bool {
	h := share.UserHome(s.opt.Paths.ProfilesDir())
	return h != "" && claude.IsTrusted(claude.UserClaudeJSONDir(h), s.opt.Cwd)
}

// confirm asks a [Y/n] question in baton's window. stop reports Ctrl+C, which
// stops baton as it does during a wait.
func (s *Supervisor) confirm(question string) (yes, stop bool) {
	if !winproc.StdinIsConsole() {
		return true, false
	}
	// Only a deliberate answer counts: drop keys typed earlier (an Enter
	// pressed during an hours-long wait must not answer this).
	s.console.FlushInput()
	if n := s.in.Buffered(); n > 0 {
		_, _ = s.in.Discard(n)
	}
	// The window may be in the background: ring and mark the title.
	winui.SetConsoleTitle(s.title(tr("[answer needed] ")))
	fmt.Fprint(s.opt.Out, "\a")
	fmt.Fprint(s.opt.Out, "baton: "+question)
	line, err := s.in.ReadString('\n')
	winui.SetConsoleTitle(s.title(""))
	if err != nil {
		// Ctrl+C ends the console read with nothing; its signal follows.
		select {
		case <-s.signals:
			return false, true
		case <-time.After(500 * time.Millisecond):
			return false, false
		}
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "" || ans == "y" || ans == "yes", false
}

type waitResult struct {
	ok        bool   // false: the user stopped baton (Ctrl+C)
	requested bool   // a `baton switch` request ended the wait
	target    string // the account it named ("" if untargeted)
}

// waitUntil sleeps until t. Ctrl+C aborts; a `baton switch` request ends the
// wait early (with its target, if it named one).
func (s *Supervisor) waitUntil(t time.Time) waitResult {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-timer.C:
			return waitResult{ok: true}
		case <-s.signals:
			s.notice("stopped while waiting")
			s.userEnded = true
			return waitResult{}
		case <-poll.C:
			if run.TakeStop(s.runDir) {
				s.notice("session ended as requested")
				s.userEnded = true
				return waitResult{}
			}
			if req := s.takeSwitch(); req != nil {
				return waitResult{ok: true, requested: true, target: req.Target}
			}
		}
	}
}

func (s *Supervisor) printResumeHint() {
	for _, id := range s.blocked {
		s.notice("%q was marked blocked (on hold, billing problem or revoked login); "+
			"baton will not use it until you fix that and run: baton enable %s", id, id)
	}
	s.blocked = nil
	if t := s.currentTranscript(); t != "" {
		fmt.Fprintf(s.opt.Out, "baton: "+tr("continue later with:  baton run -- --resume \"%s\"")+"\n", t)
	}
}

// ---- helpers ---------------------------------------------------------------

// title names the session window: folder and account.
func (s *Supervisor) title(prefix string) string {
	return prefix + "baton · " + filepath.Base(s.opt.Cwd) + " · " + s.acct.ID
}

// strictness orders switch modes; a running session only ever gets stricter.
var strictness = map[string]int{config.ModeAuto: 0, config.ModeConfirm: 1, config.ModeOff: 2}

// reloadConfig picks up `baton config` changes made while this run is going.
// The switch mode can only become stricter mid-run (auto -> confirm -> off):
// otherwise anything running inside the session could quietly turn the
// user's confirm mode back to auto. Loosening takes effect on the next run.
func (s *Supervisor) reloadConfig() {
	cfg, err := config.Load(s.opt.Paths.Config())
	if err != nil {
		s.logf("config: %v", err)
		return
	}
	if cur := s.opt.Config.SwitchMode; cfg.SwitchMode == "" || strictness[cfg.SwitchMode] < strictness[cur] {
		cfg.SwitchMode, cfg.RiskAcknowledged = cur, s.opt.Config.RiskAcknowledged
	}
	s.opt.Config = cfg
	s.guard.Cooldown = time.Duration(cfg.CooldownSeconds) * time.Second
	s.guard.MaxPerHour = cfg.MaxSwitchesPerHour
}

// holderDirs are the config folders whose session registries can show a
// claude writing this session: every account, the folder that holds the
// transcript (it may be the user's normal ~/.claude) and the normal setup.
func (s *Supervisor) holderDirs() []string {
	dirs := s.profileDirs()
	if s.transcr != "" {
		if proj := filepath.Dir(filepath.Dir(s.transcr)); strings.EqualFold(filepath.Base(proj), "projects") {
			dirs = append(dirs, filepath.Dir(proj))
		}
	}
	if h := share.UserHome(s.opt.Paths.ProfilesDir()); h != "" {
		dirs = append(dirs, h)
	}
	return dirs
}

func (s *Supervisor) reloadRegistry() {
	if reg, err := accounts.Load(s.opt.Paths.Accounts()); err == nil {
		s.reg = reg // pick up enable/disable/login/add done meanwhile
		if a := reg.Get(s.acct.ID); a != nil {
			s.acct = *a
		}
	}
}

// loadState is the shared ledger with this run's own, newer marks on top. If
// state.json cannot be read, the last good copy stands in: an empty ledger
// would make limited and blocked accounts look fine.
func (s *Supervisor) loadState() ledger.State {
	st, err := ledger.Load(s.opt.Paths.State())
	if err != nil {
		s.logf("ledger: %v", err)
		if s.lastState != nil {
			st = s.lastState.Clone()
		}
	} else {
		c := st.Clone()
		s.lastState = &c
	}
	for id, m := range s.marks {
		if cur := st.StatusOf(id); cur.LimitedAt.Before(m.LimitedAt) {
			*st.Account(id) = m
		}
	}
	return st
}

func (s *Supervisor) profileDirs() []string {
	dirs := make([]string, 0, len(s.reg.Accounts))
	for _, a := range s.reg.Accounts {
		dirs = append(dirs, a.ProfileDir)
	}
	return dirs
}

// pruneRuns deletes run folders older than maxAge whose supervisor is gone.
func pruneRuns(runsDir string, maxAge time.Duration) {
	entries, _ := os.ReadDir(runsDir)
	for _, e := range entries {
		dir := filepath.Join(runsDir, e.Name())
		fi, err := e.Info()
		if err != nil || !e.IsDir() || time.Since(fi.ModTime()) < maxAge {
			continue
		}
		if j, err := run.LoadJournal(dir); err == nil && j.State != run.StateDone {
			if created := winproc.ProcessCreated(j.SupervisorPID); !created.IsZero() &&
				created.Sub(j.SupervisorCreated).Abs() < time.Second {
				continue // still supervising
			}
		}
		_ = fsx.RemoveTreeSafe(dir)
	}
}

func describe(p *pending, account string) string {
	switch p.reason {
	case reasonLimit:
		if !p.reset.IsZero() {
			return fmt.Sprintf(tr("usage limit on %q (resets %s)"), account, clock(p.reset))
		}
		return fmt.Sprintf(tr("usage limit on %q"), account)
	case reasonAuth:
		return fmt.Sprintf(tr("login failed on %q (fix with: baton login %s)"), account, account)
	default:
		return fmt.Sprintf(tr("switch requested from %q"), account)
	}
}

func clock(t time.Time) string {
	t = t.Local()
	if time.Until(t) > 20*time.Hour {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04")
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func orUnknown(s string) string {
	if s == "" {
		return "no details"
	}
	return truncate(s, 120)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
