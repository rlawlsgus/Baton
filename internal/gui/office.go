package gui

import (
	"net/http"
	"os"
	"strconv"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/office"
	"baton/internal/run"
	"baton/internal/share"
)

// The office: every Claude Code conversation on this PC shown live as
// people working in an office (internal/office does the watching). Just
// for fun, and only while turned on in the settings.

func (s *Server) officeOn() bool {
	cfg, _ := config.Load(s.opt.Paths.Config())
	return cfg.Office
}

// officeTracker is made the first time the office is looked at, and sleeps
// while nobody looks. nil once the server is closing.
func (s *Server) officeTracker() *office.Tracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.office == nil {
		s.office = office.New(office.Options{Root: s.opt.Paths.Root, Sources: s.officeSources, Me: userName()})
	}
	return s.office
}

// apiOffice answers the office page's poll:
// GET /api/office?session=<id or empty to follow>&since=<seq>&wait=<0|1>.
// With wait=1 it is held (up to 15 s) until there is news.
func (s *Server) apiOffice(w http.ResponseWriter, r *http.Request) {
	if !s.officeOn() {
		fail(w, http.StatusNotFound, "office_off", "the office is turned off", "")
		return
	}
	q := r.URL.Query()
	id := q.Get("session")
	if id != "" && !office.ValidSession(id) {
		fail(w, http.StatusBadRequest, "bad_session", "unknown session", "")
		return
	}
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	tr := s.officeTracker()
	if tr == nil {
		fail(w, http.StatusServiceUnavailable, "closing", "baton is closing", "")
		return
	}
	writeJSON(w, http.StatusOK, tr.Poll(r.Context(), office.Query{Session: id, Since: since, Wait: q.Get("wait") == "1"}))
}

// apiOfficePopout opens the office alone in a window of its own, with a
// fresh one-time code (the page reads #office before it drops the code).
func (s *Server) apiOfficePopout(w http.ResponseWriter, r *http.Request) {
	if !s.officeOn() {
		fail(w, http.StatusNotFound, "office_off", "the office is turned off", "")
		return
	}
	if s.opt.OpenApp == nil {
		fail(w, http.StatusNotImplemented, "no_popout", "not available", "")
		return
	}
	if err := s.opt.OpenApp(s.WindowURL() + "#office"); err != nil {
		fail(w, http.StatusInternalServerError, "popout", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// officeSources tells the office where conversations live and which baton
// sessions run: every account's profile (in accounts.json order, which
// gives each account its colour) and the user's own setup.
func (s *Server) officeSources() office.Sources {
	p := s.opt.Paths
	var src office.Sources
	reg, _ := accounts.Load(p.Accounts())
	for _, a := range reg.Accounts {
		src.Accounts = append(src.Accounts, a.ID)
		if a.ProfileDir != "" {
			src.Dirs = append(src.Dirs, office.Dir{Path: a.ProfileDir, Account: a.ID})
		}
	}
	if h := share.UserHome(p.ProfilesDir()); h != "" {
		src.Dirs = append(src.Dirs, office.Dir{Path: h})
	}
	for _, j := range s.cache.journals(p.RunsDir()) {
		if j.State == run.StateDone || !supervisorAlive(j.SupervisorPID, j.SupervisorCreated) {
			continue
		}
		src.Runs = append(src.Runs, office.Run{ID: j.RunID, Account: j.Account, State: j.State, SessionID: j.SessionID,
			TranscriptPath: j.TranscriptPath, Cwd: j.Cwd, Window: j.ConsoleWindow != 0, Started: j.Started})
	}
	return src
}

// userName is the name on the user's desk in the office.
func userName() string {
	if n := os.Getenv("USERNAME"); n != "" {
		return n
	}
	return "me"
}
