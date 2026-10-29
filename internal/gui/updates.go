package gui

import (
	"context"
	"net/http"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/run"
	"baton/internal/update"
)

// The window asks GitHub for a newer baton at most every updateEvery (when
// the setting is on); the Help tab can ask any time.
const updateEvery = 6 * time.Hour

type updateState struct {
	at         time.Time
	busy       bool
	rel        *update.Release
	err        string
	installing bool
}

// updateView is what the window shows about updates.
type updateView struct {
	Current    string     `json:"current"`
	Latest     string     `json:"latest,omitempty"`
	Newer      bool       `json:"newer"`
	Notes      string     `json:"notes,omitempty"`
	Page       string     `json:"page,omitempty"`
	Checked    *time.Time `json:"checked,omitempty"`
	Checking   bool       `json:"checking"`
	Installing bool       `json:"installing"`
	Error      string     `json:"error,omitempty"`
}

// updateInfo returns what is known and, when it is old and the setting is
// on, asks again in the background.
func (s *Server) updateInfo(cfg config.Config) *updateView {
	if s.opt.CheckUpdate == nil {
		return nil
	}
	s.mu.Lock()
	again := cfg.UpdateChecks() && !s.upd.busy && time.Since(s.upd.at) > updateEvery
	if again {
		s.upd.busy = true
	}
	v := s.updateViewLocked()
	s.mu.Unlock()
	if again {
		go s.checkUpdate()
	}
	return v
}

func (s *Server) updateViewLocked() *updateView {
	v := &updateView{Current: s.opt.Version, Checking: s.upd.busy, Installing: s.upd.installing, Error: s.upd.err}
	if !s.upd.at.IsZero() {
		at := s.upd.at
		v.Checked = &at
	}
	if r := s.upd.rel; r != nil {
		v.Latest, v.Notes, v.Page = r.Version, r.Notes, r.Page
		v.Newer = update.Compare(r.Version, s.opt.Version) > 0
	}
	return v
}

func (s *Server) checkUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := s.opt.CheckUpdate(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upd.at, s.upd.busy = time.Now(), false
	if err != nil {
		s.upd.err = err.Error()
		return
	}
	s.upd.rel, s.upd.err = &rel, ""
}

// apiUpdateCheck asks GitHub now.
func (s *Server) apiUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.opt.CheckUpdate == nil {
		fail(w, http.StatusNotImplemented, "no_update", "not available", "")
		return
	}
	s.mu.Lock()
	busy := s.upd.busy
	s.upd.busy = true
	s.mu.Unlock()
	if !busy {
		s.checkUpdate()
	}
	s.mu.Lock()
	v := s.updateViewLocked()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// apiUpdateInstall installs the newer release and reopens baton. Like the
// uninstall it waits until no session runs: their baton.exe is replaced.
func (s *Server) apiUpdateInstall(w http.ResponseWriter, r *http.Request) {
	if s.opt.Update == nil {
		fail(w, http.StatusNotImplemented, "no_update", "not available", "")
		return
	}
	if code, msg := s.replaceRefusal(); code != "" {
		fail(w, http.StatusConflict, code, msg, "")
		return
	}
	s.mu.Lock()
	rel := s.upd.rel
	if rel == nil || update.Compare(rel.Version, s.opt.Version) <= 0 || s.upd.installing {
		s.mu.Unlock()
		fail(w, http.StatusConflict, "no_update", "there is no newer baton to install", "")
		return
	}
	s.upd.installing = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	app, err := s.opt.Update(ctx, *rel)
	s.mu.Lock()
	s.upd.installing = false
	s.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "update", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": rel.Version})
	if s.opt.Relaunch != nil {
		go func() {
			time.Sleep(300 * time.Millisecond) // let the answer reach the page
			_ = s.opt.Relaunch(app)
		}()
	}
}

// replaceRefusal says why baton's programs cannot be replaced or removed
// now: a session runs (its supervisor and hooks use baton.exe), or a window
// this GUI opened is still open.
func (s *Server) replaceRefusal() (code, msg string) {
	busy := len(run.Active(s.opt.Paths.RunsDir(), supervisorAlive)) > 0
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	for _, a := range reg.Accounts {
		busy = busy || claude.BusyProfile(a.ProfileDir)
	}
	if busy {
		return "sessions_running", "end the running sessions first"
	}
	if s.anyJobRunning() {
		return "windows_open", "close the open baton windows first"
	}
	return "", ""
}
