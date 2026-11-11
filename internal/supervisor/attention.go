package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/notify"
	"baton/internal/run"
	"baton/internal/winproc"
	"baton/internal/winui"
)

// What a session tells the user about while they are away: Windows
// notifications, and keeping the PC awake while there is work going on.

// Kinds of notification; a newer one of a kind replaces the older one.
const (
	noteDone      = "done"      // a long response finished
	noteWaiting   = "waiting"   // Claude Code waits for an answer in the session window
	noteAsking    = "asking"    // baton asks in the session window before switching
	noteSwitched  = "switched"  // the session goes on, on another account or after a wait
	noteLimited   = "limited"   // every account is limited, or switching waits
	noteProblem   = "problem"   // an account problem, or the session ended unexpectedly
	noteElsewhere = "elsewhere" // the conversation was opened somewhere else too: the session moves to a copy
)

const (
	settleTime = 3 * time.Second  // how long Claude Code's status must last to be news
	longTurn   = 30 * time.Second // a finished response is news if it took this long
)

// claudeWatch follows what Claude Code is doing (its session registry says
// idle, busy or waiting) and picks out what is worth a notification.
type claudeWatch struct {
	status    string    // at the last look: idle, busy or waiting
	since     time.Time // when it started
	turnStart time.Time // when the current response started (zero while idle)
	turnLen   time.Duration
	told      bool // the current status was dealt with
	lastLook  time.Time
}

// step records the status seen at now and returns what is news: noteWaiting,
// noteDone or "". A status is news once it has lasted settleTime: a turn
// that ends in a usage limit goes idle a moment before baton hears why.
func (w *claudeWatch) step(status string, now time.Time) string {
	if status != "idle" && status != "waiting" {
		status = "busy"
	}
	if status != w.status {
		switch {
		case status != "idle" && w.turnStart.IsZero():
			w.turnStart = now
		case status == "idle":
			w.turnLen = 0
			if !w.turnStart.IsZero() {
				w.turnLen = now.Sub(w.turnStart)
			}
			w.turnStart = time.Time{}
		}
		w.status, w.since, w.told = status, now, false
	}
	if w.told || now.Sub(w.since) < settleTime {
		return ""
	}
	w.told = true
	switch {
	case status == "waiting":
		return noteWaiting
	case status == "idle" && w.turnLen >= longTurn:
		return noteDone
	}
	return ""
}

// lookAtClaude checks Claude Code's status (about once a second while the
// session runs), notifies, and keeps the PC awake while it works.
func (s *Supervisor) lookAtClaude() {
	now := time.Now()
	s.watch.lastLook = now
	if now.Sub(s.cfgAt) > 5*time.Second {
		s.cfgAt = now
		s.reloadConfig() // notifications or staying awake changed in the window
	}
	pid := s.claudePID
	if pid == 0 && s.child != nil {
		pid = s.child.PID
	}
	if s.started && pid != 0 {
		s.lookElsewhere(now)
		ls, ok := claude.SessionFor(s.acct.ProfileDir, pid)
		s.moveWhenIdle(ls.Status, ok, now)
		if ok {
			defer func() {
				if s.owed() != s.journal.Owed {
					s.saveJournal() // what a restore after a crash would owe
				}
			}()
			switch s.watch.step(ls.Status, now) {
			case noteDone:
				s.notify(noteDone, s.say("Claude finished its response.", "Claude가 응답을 마쳤습니다."))
			case noteWaiting:
				if ls.WaitingFor != "" {
					s.notify(noteWaiting, s.say("Claude is waiting for you in the session window: %s", "Claude가 세션 창에서 기다립니다: %s", ls.WaitingFor))
				} else {
					s.notify(noteWaiting, s.say("Claude is waiting for you in the session window.", "Claude가 세션 창에서 기다립니다."))
				}
			}
		}
	}
	s.updateAwake()
}

// lookElsewhere notices another claude that opens this session's
// conversation (the desktop app, a terminal, another baton session): both
// would write to it, so it would split and what is said on one side would be
// lost to the other. Claude Code does not notice, since each looks only at
// its own config folder. baton never keeps writing a conversation someone
// else has open: it moves this session to a copy, right away when Claude is
// idle, else as soon as its response is done (lookAtClaude starts the move).
func (s *Supervisor) lookElsewhere(now time.Time) {
	if s.session == "" || now.Sub(s.elsewhereAt) < 3*time.Second {
		return
	}
	s.elsewhereAt = now
	// The folders the start checked: every account, the conversation's own
	// folder and the user's own Claude Code.
	var other *claude.LiveSession
	others := claude.OpenElsewhere(s.holderDirs(), s.session, s.ownPIDs()...)
	for _, o := range others {
		if s.otherMoves(o) {
			if s.yieldedTo != o.PID {
				s.yieldedTo = o.PID
				s.logf("the conversation is also open in baton's session in pid %d (%s), whose claude started later: that one moves to a copy, this one stays", o.PID, o.ConfigDir())
			}
			continue
		}
		other = &o
		break
	}
	if len(others) == 0 {
		// Mode rows up to here are this session's own: see markForeign.
		if fi, err := os.Stat(s.transcr); s.transcr != "" && err == nil {
			s.cleanFile, s.cleanSize = s.transcr, fi.Size()
		}
	} else {
		// The other side may write its own mode into the conversation (a
		// terminal's claude does, at its first prompt or when it quits),
		// even one that moves instead of this session.
		s.markForeign()
	}
	if other == nil {
		if s.held != nil {
			s.logf("pid %d no longer has the conversation open; it stays here", s.held.PID)
		}
		s.setHeld(nil)
		return
	}
	if s.held != nil && s.held.PID == other.PID {
		return
	}
	s.setHeld(other)
	s.logf("the conversation is also open in pid %d (entrypoint %q, %s): this session moves to a copy once Claude is idle",
		other.PID, other.Entrypoint, other.ConfigDir())
}

// setHeld records which other claude has the conversation open (nil: none).
// Each change starts the moment of idle the move waits for over (a claude
// that comes back hours later gets that moment again, so a usage limit can
// still be heard first). The notice that the move waits for a response is
// given once while the conversation stays open elsewhere, even when what
// has it open changes meanwhile.
func (s *Supervisor) setHeld(o *claude.LiveSession) {
	s.held, s.heldIdle, s.heldMisses = o, time.Time{}, 0
	if o == nil {
		s.heldTold = false
	}
}

// moveWhenIdle starts the move to a copy that lookElsewhere decided on, once
// Claude is idle: status is what its registry says at now (ok false: unread).
// While a response runs (or a question waits) the user is told once that the
// move comes after it.
func (s *Supervisor) moveWhenIdle(status string, ok bool, now time.Time) {
	if s.held == nil || s.pend != nil {
		return
	}
	if !ok {
		// Its registry file can be mid-rewrite; one that stays unreadable
		// cannot say it is busy either.
		if s.heldMisses++; s.heldMisses < 10 {
			return
		}
		status = "idle"
	}
	s.heldMisses = 0
	if status != "idle" {
		s.heldIdle = time.Time{}
		if !s.heldTold {
			s.heldTold = true
			s.logf("claude is %s: the move to a copy waits for it to finish", status)
			s.notify(noteElsewhere, s.say(
				"This conversation was also opened in %s. baton moves this session to a copy as soon as Claude finishes its current response.",
				"이 대화가 %s에서도 열렸습니다. Claude가 지금 응답을 마치면 baton이 이 세션을 사본으로 옮깁니다.", s.placeOf(*s.held)))
		}
		return
	}
	// Idle for a moment first: a turn that ends in a usage limit goes idle a
	// moment before baton hears why, and the switch for that limit then takes
	// the session to a copy on the next account instead.
	if s.heldIdle.IsZero() {
		s.heldIdle = now
	}
	if now.Sub(s.heldIdle) < settleTime {
		return
	}
	s.logf("claude is idle: moving the session to a copy now")
	s.pend = &pending{reason: reasonElsewhere, holder: s.held}
}

// otherMoves reports whether o is the claude of another baton session that
// started after this session's claude: that session moves to a copy, so
// this one stays where it is (two baton sessions must never both move). A
// claude in an account's profile that no live run started is not baton's,
// and anything baton cannot tell for sure means this session moves.
func (s *Supervisor) otherMoves(o claude.LiveSession) bool {
	if !s.inProfile(o) {
		return false
	}
	own, ok := claude.SessionFor(s.acct.ProfileDir, s.ownPID())
	if !ok || own.StartedAt == 0 || !claude.StartedLater(o, own) {
		return false
	}
	for _, j := range run.Active(s.opt.Paths.RunsDir(), supervisorAlive) {
		if j.RunID != s.runID && j.ChildPID > 0 && claude.StartedBy(o, j.ChildPID) {
			return true
		}
	}
	return false
}

// inProfile reports whether o runs in one of the accounts' profiles.
func (s *Supervisor) inProfile(o claude.LiveSession) bool {
	dir := filepath.Clean(o.ConfigDir())
	for _, a := range s.reg.Accounts {
		if strings.EqualFold(filepath.Clean(a.ProfileDir), dir) {
			return true
		}
	}
	return false
}

// ownPID is this session's claude as its hooks report it (they can differ
// from the started process behind a shim); ownPIDs has both.
func (s *Supervisor) ownPID() int {
	if s.claudePID == 0 && s.child != nil {
		return s.child.PID
	}
	return s.claudePID
}

func (s *Supervisor) ownPIDs() []int {
	self := []int{s.claudePID}
	if s.child != nil {
		self = append(self, s.child.PID)
	}
	return self
}

func supervisorAlive(pid int, created time.Time) bool {
	got := winproc.ProcessCreated(pid)
	if got.IsZero() {
		return false
	}
	d := got.Sub(created)
	return d > -time.Second && d < time.Second
}

// placeOf names where another claude runs, for a notification.
func (s *Supervisor) placeOf(o claude.LiveSession) string {
	return s.placeIn(o, s.noteKo)
}

// placeIn names where another claude runs, in Korean or English: a
// notification and a notice in the session window can differ in language.
func (s *Supervisor) placeIn(o claude.LiveSession, ko bool) string {
	say := func(en, k string, a ...any) string {
		if ko {
			return fmt.Sprintf(k, a...)
		}
		return fmt.Sprintf(en, a...)
	}
	dir := filepath.Clean(o.ConfigDir())
	for _, a := range s.reg.Accounts {
		if strings.EqualFold(filepath.Clean(a.ProfileDir), dir) {
			return say("another session on %s", "%s 계정의 다른 세션", a.ID)
		}
	}
	switch o.Entrypoint {
	case "claude-desktop":
		return say("the Claude desktop app", "Claude 데스크톱 앱")
	case "cli":
		return say("Claude Code in a terminal", "터미널의 Claude Code")
	case "claude-vscode":
		return "VS Code"
	}
	return say("another Claude Code window", "다른 Claude Code 창")
}

// wantAwake: the PC stays awake while Claude Code works and while baton
// moves the session or waits to continue it, not while anyone waits for the
// user (they are away; nothing happens until they answer).
func wantAwake(on bool, state, claudeStatus string) bool {
	switch {
	case !on:
		return false
	case state == run.StateRunning:
		return claudeStatus == "busy"
	}
	return state == run.StateStopping || state == run.StateSwitching || state == run.StateWaiting || state == run.StateEnding
}

func (s *Supervisor) updateAwake() {
	want := wantAwake(s.opt.Config.KeepAwakeOn(), s.journal.State, s.watch.status)
	if want == s.awake.On() {
		return
	}
	if err := s.awake.Set(want); err != nil {
		s.logf("keep awake %v: %v", want, err)
		return
	}
	s.logf("keep awake: %v", want)
}

// notify shows a Windows notification about this session, as the
// notifications setting allows. The frequent kinds stay quiet while the
// user looks at the session window anyway.
func (s *Supervisor) notify(kind, body string) {
	switch lvl := s.opt.Config.NotifyLevel(); {
	case lvl == config.NotifyOff, lvl == config.NotifyImportant && kind == noteDone:
		return
	}
	if (kind == noteDone || kind == noteWaiting) && winui.InFront(s.window) {
		s.logf("notify %s: skipped, the session window is in front", kind)
		return
	}
	s.registerToasts()
	n := notify.Note{Title: "baton · " + filepath.Base(s.opt.Cwd), Body: body, Tag: kind, Group: s.runID}
	if s.clickable {
		n.Launch = notify.Scheme + ":" + s.runID
	}
	s.logf("notify %s: %s", kind, body)
	if err := notify.Show(n); err != nil {
		s.logf("notify: %v", err)
	}
}

// registerToasts registers baton with Windows for its notifications, once
// per run: its name and icon, and (when baton-app.exe is next to baton.exe)
// the link a click opens, which brings this session's window forward.
func (s *Supervisor) registerToasts() {
	if s.registered {
		return
	}
	s.registered = true
	// The installed copy first: it stays where it is, while an unzipped
	// folder a session may have been started from can be deleted later.
	app := filepath.Join(winui.InstallDir(), "baton-app.exe")
	if winui.InstallDir() == "" || !fileExists(app) {
		app = filepath.Join(filepath.Dir(s.opt.BatonExe), "baton-app.exe")
	}
	if !fileExists(app) || s.window == 0 {
		app = ""
	}
	if err := notify.Prepare(s.opt.Paths.Root, app); err != nil {
		s.logf("notification registration: %v", err)
		return
	}
	s.clickable = app != ""
}

// say picks the notification text in the user's language.
func (s *Supervisor) say(en, ko string, a ...any) string {
	if s.noteKo {
		return fmt.Sprintf(ko, a...)
	}
	return fmt.Sprintf(en, a...)
}

// notesInKorean: notifications follow the language the window started the
// session in, else the window's language setting, else Windows'.
func notesInKorean(cfg config.Config) bool {
	switch os.Getenv("BATON_LANG") {
	case "ko":
		return true
	case "en":
		return false
	}
	if cfg.GUILanguage != "" {
		return cfg.GUILanguage == "ko"
	}
	return winui.UserLang() == "ko"
}

// why says what made the session move, for a notification.
func (s *Supervisor) why(p *pending, account string) string {
	switch p.reason {
	case reasonLimit:
		return s.say("%s hit its usage limit.", "%s 계정이 한도에 걸렸습니다.", account)
	case reasonAuth:
		return s.say("%s could not sign in.", "%s 계정의 로그인이 만료되었습니다.", account)
	}
	return s.say("A switch from %s was asked for.", "%s 계정에서 전환을 요청했습니다.", account)
}
