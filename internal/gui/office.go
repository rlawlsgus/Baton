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

// apiOfficeLayout keeps the width of the office's side columns, and which
// are folded, for one kind of window (the tab, or the pop-out), so they look
// the same next time: the page's own storage starts empty every launch.
// {"window":"tab"|"popout","left":px,"right":px,"left_folded":b,"right_folded":b}
func (s *Server) apiOfficeLayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Window string `json:"window"`
		config.OfficePanes
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.officeOn() {
		fail(w, http.StatusNotFound, "office_off", "the office is turned off", "")
		return
	}
	if req.Window != "tab" && req.Window != "popout" {
		fail(w, http.StatusBadRequest, "bad_value", "window must be tab or popout", "")
		return
	}
	panes := req.OfficePanes.Clamp()
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		if c.OfficeView == nil {
			c.OfficeView = &config.OfficeView{}
		}
		if req.Window == "tab" {
			c.OfficeView.Tab = panes
		} else {
			c.OfficeView.Popout = panes
		}
		c.OfficeView = c.OfficeView.Normalized()
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, "config", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// officeHide is one thing hidden from the office, or shown again: a session
// (by id) or a floor (by its project folder).
type officeHide struct {
	Session string `json:"session,omitempty"`
	Floor   string `json:"floor,omitempty"`
	Hidden  bool   `json:"hidden"`
}

// apiOfficeHide hides sessions and floors from the office, or shows them
// again; follow mode skips what is hidden. One change at the top level,
// more in "changes" (all written at once: showing one session of a hidden
// floor hides the floor's others), or {"all":true,"hidden":false} to show
// everything again.
func (s *Server) apiOfficeHide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		officeHide
		All     bool         `json:"all"`
		Changes []officeHide `json:"changes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.officeOn() {
		fail(w, http.StatusNotFound, "office_off", "the office is turned off", "")
		return
	}
	changes := req.Changes
	if req.Session != "" || req.Floor != "" {
		changes = append([]officeHide{req.officeHide}, changes...)
	}
	switch {
	case req.All && (req.Hidden || len(changes) > 0):
		fail(w, http.StatusBadRequest, "bad_value", "all only shows everything again", "")
		return
	case !req.All && len(changes) == 0:
		fail(w, http.StatusBadRequest, "bad_value", "nothing to hide or show", "")
		return
	case len(changes) > 2*config.OfficeHiddenSessionsMax:
		fail(w, http.StatusBadRequest, "bad_value", "too many changes", "")
		return
	}
	for i, c := range changes {
		switch {
		case (c.Session == "") == (c.Floor == ""):
			fail(w, http.StatusBadRequest, "bad_value", "each change names a session or a floor", "")
			return
		case c.Session != "" && !office.ValidSession(c.Session):
			fail(w, http.StatusBadRequest, "bad_session", "unknown session", "")
			return
		case c.Floor != "":
			if len(c.Floor) > 1024 || office.ProjectKey(c.Floor) == "" {
				fail(w, http.StatusBadRequest, "bad_value", "not a project folder", "")
				return
			}
			changes[i].Floor = office.ProjectKey(c.Floor)
		}
	}
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		if c.OfficeView == nil {
			c.OfficeView = &config.OfficeView{}
		}
		v := c.OfficeView
		if req.All {
			v.HiddenSessions, v.HiddenFloors = nil, nil
		}
		for _, ch := range changes {
			if ch.Floor != "" {
				v.SetHidden(true, ch.Floor, ch.Hidden)
			} else {
				v.SetHidden(false, ch.Session, ch.Hidden)
			}
		}
		c.OfficeView = v.Normalized()
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, "config", err.Error(), "")
		return
	}
	if tr := s.officeTracker(); tr != nil {
		tr.Refresh()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// officeSources tells the office where conversations live and which baton
// sessions run: every account's profile (in accounts.json order, which
// gives each account its colour) and the user's own setup.
func (s *Server) officeSources() office.Sources {
	p := s.opt.Paths
	var src office.Sources
	reg, err := accounts.Load(p.Accounts())
	// A folder out of sight must not look like a project with nothing left.
	src.Partial = err != nil
	for _, a := range reg.Accounts {
		src.Accounts = append(src.Accounts, a.ID)
		if a.ProfileDir != "" {
			src.Dirs = append(src.Dirs, office.Dir{Path: a.ProfileDir, Account: a.ID})
		}
	}
	if h := share.UserHome(p.ProfilesDir()); h != "" {
		src.Dirs = append(src.Dirs, office.Dir{Path: h})
	} else {
		src.Partial = true
	}
	if cfg, _ := config.Load(p.Config()); cfg.OfficeView != nil {
		src.HiddenSessions, src.HiddenFloors = cfg.OfficeView.HiddenSessions, cfg.OfficeView.HiddenFloors
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
