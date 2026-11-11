package supervisor

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"baton/internal/desktop"
)

// The Claude desktop app shows a baton session for the accounts the user
// picked in the window (internal/desktop). When the session goes on in
// another conversation, the app's records follow it, and after each turn
// they are brought up to date. That work is done beside the session, one
// piece after another, and never holds the session up: a failure is only
// logged.

type deskWork struct {
	mu     sync.Mutex
	jobs   chan func()
	done   chan struct{}
	closed bool
}

// deskDo queues fn on the desktop app's records.
func (s *Supervisor) deskDo(what string, fn func(d *desktop.Desk) error) {
	if s.desk == nil {
		s.desk = &deskWork{jobs: make(chan func(), 64), done: make(chan struct{})}
		go s.desk.run()
	}
	paths, reg, logf := s.opt.Paths, s.reg, s.logf
	job := func() {
		defer func() {
			if r := recover(); r != nil {
				logf("desktop app: %s: %v", what, r)
			}
		}()
		if err := fn(desktop.New(paths, reg)); err != nil {
			logf("desktop app: %s: %v", what, err)
		}
	}
	w := s.desk
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.jobs <- job:
	default:
		logf("desktop app: %s skipped (too much queued)", what)
	}
}

func (w *deskWork) run() {
	defer close(w.done)
	for job := range w.jobs {
		job()
	}
}

// wait lets the queued work finish, for at most d, and takes no more.
func (w *deskWork) wait(d time.Duration) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.jobs)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
	case <-time.After(d):
	}
}

// deskFollow moves the session's place in the desktop app from conversation
// from on to to (transcript: its file, when known).
func (s *Supervisor) deskFollow(from, to, transcript string) {
	if from == "" || to == "" || strings.EqualFold(from, to) {
		return
	}
	s.deskDo(fmt.Sprintf("follow %s -> %s", from, to), func(d *desktop.Desk) error { return d.Follow(from, to, transcript) })
}

// followCopy moves the session's place in the desktop app on to the copy it
// went on in by itself, once the copy's id is known.
func (s *Supervisor) followCopy() {
	if s.followFrom == "" || s.session == "" || strings.EqualFold(s.session, s.followFrom) {
		return
	}
	s.deskFollow(s.followFrom, s.session, s.transcriptOf(s.session))
	s.followFrom = ""
}

// deskTouch brings the desktop app's records of the session up to date
// after a turn.
func (s *Supervisor) deskTouch() {
	id, t := s.session, s.transcr
	if id == "" {
		return
	}
	s.deskDo("update", func(d *desktop.Desk) error { return d.Touch(id, t) })
}

// transcriptOf is the session's transcript when it is conversation id's.
func (s *Supervisor) transcriptOf(id string) string {
	if s.transcr != "" && strings.EqualFold(strings.TrimSuffix(filepath.Base(s.transcr), ".jsonl"), id) {
		return s.transcr
	}
	return ""
}
