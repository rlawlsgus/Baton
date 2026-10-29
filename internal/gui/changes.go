package gui

import (
	"errors"
	"net/http"
	"time"

	"baton/internal/gitinfo"
	"baton/internal/run"
)

// A running session's git changes are read in the background at most every
// changesEvery: git status can take a moment in a big repository, and the
// window polls every few seconds.
const changesEvery = 20 * time.Second

type changesEntry struct {
	at   time.Time
	busy bool
	c    gitinfo.Changes
	err  string
}

// changesView is the summary a session row shows.
type changesView struct {
	Repo    bool   `json:"repo"`
	Files   int    `json:"files"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Error   string `json:"error,omitempty"` // no_git, or git's own message
}

// changesOf returns what is known of a run's changes (nil before the first
// read) and starts a fresh read when that is old.
func (s *Server) changesOf(runID, cwd string) *changesView {
	s.mu.Lock()
	if s.changes == nil {
		s.changes = map[string]*changesEntry{}
	}
	e := s.changes[runID]
	if e == nil {
		e = &changesEntry{}
		s.changes[runID] = e
	}
	stale := !e.busy && time.Since(e.at) > changesEvery
	if stale {
		e.busy = true
	}
	var v *changesView
	if !e.at.IsZero() {
		v = &changesView{Repo: e.c.Repo, Files: len(e.c.Files), Added: e.c.Added, Removed: e.c.Removed, Error: e.err}
	}
	s.mu.Unlock()
	if stale {
		go func() { _, _ = s.readChanges(runID, cwd) }()
	}
	return v
}

func (s *Server) readChanges(runID, cwd string) (gitinfo.Changes, error) {
	c, err := gitinfo.Read(cwd, 10*time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changes == nil {
		s.changes = map[string]*changesEntry{}
	}
	e := s.changes[runID]
	if e == nil {
		e = &changesEntry{}
		s.changes[runID] = e
	}
	e.at, e.busy, e.c, e.err = time.Now(), false, c, changesError(err)
	return c, err
}

func changesError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, gitinfo.ErrNoGit):
		return "no_git"
	}
	return err.Error()
}

// forgetChanges drops what is kept for runs that are no longer running.
func (s *Server) forgetChanges(running map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.changes {
		if !running[id] {
			delete(s.changes, id)
		}
	}
}

// apiChanges lists a running session's changed files, read now.
func (s *Server) apiChanges(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("run_id")
	if !run.ValidID(id) {
		fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
		return
	}
	j, err := run.LoadJournal(s.opt.Paths.Run(id))
	if err != nil || j.State == run.StateDone || !supervisorAlive(j.SupervisorPID, j.SupervisorCreated) {
		fail(w, http.StatusNotFound, "bad_run", "that session is not running", "")
		return
	}
	c, err := s.readChanges(id, j.Cwd)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "files": []gitinfo.File{}, "error": changesError(err)})
		return
	}
	writeJSON(w, http.StatusOK, c)
}
