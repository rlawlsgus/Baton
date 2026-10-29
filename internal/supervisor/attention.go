package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/notify"
	"baton/internal/run"
	"baton/internal/winui"
)

// What a session tells the user about while they are away: Windows
// notifications, and keeping the PC awake while there is work going on.

// Kinds of notification; a newer one of a kind replaces the older one.
const (
	noteDone     = "done"     // a long response finished
	noteWaiting  = "waiting"  // Claude Code waits for an answer in the session window
	noteAsking   = "asking"   // baton asks in the session window before switching
	noteSwitched = "switched" // the session goes on, on another account or after a wait
	noteLimited  = "limited"  // every account is limited, or switching waits
	noteProblem  = "problem"  // an account problem, or the session ended unexpectedly
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
		if ls, ok := claude.SessionFor(s.acct.ProfileDir, pid); ok {
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
	app := filepath.Join(filepath.Dir(s.opt.BatonExe), "baton-app.exe")
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
