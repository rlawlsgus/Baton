package gui

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/desklogin"
	"baton/internal/desktop"
)

// Switching the Claude desktop app's sign-in between the user's own
// accounts (internal/desklogin), from the window: the setting and its
// notice, the card on the Accounts tab, and the banner for a switch that
// was cut off. Only these handlers switch, and only on the user's click:
// nothing in the window does it on its own (desklogin's rules test holds
// every caller of Use, Blank, Recover, Settle and Launch, and of the
// switches that quit the app first, to this file and the CLI's).

// loginsState is the last look at the kept sign-ins (desklogin.View). A
// look stats every file of every kept sign-in and lists the app's
// processes, and the state poll comes every second and a half: it is kept
// a few seconds, and dropped after every change made here.
type loginsState struct {
	mu  sync.Mutex
	at  time.Time
	on  bool // what the look found: kept longer while switching is off
	v   desklogin.View
	err error
}

// How long a look is kept: while switching is on the card shows whether
// the app runs, so not long; while off only the setting and a leftover
// line read it.
const (
	loginsTTL    = 3 * time.Second
	loginsOffTTL = 10 * time.Second
)

func (s *Server) logins() *desklogin.Logins {
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	return desklogin.New(s.opt.Paths, reg)
}

// loginsView is the kept sign-ins and where the app is, as of a few seconds
// ago at most.
func (s *Server) loginsView() (desklogin.View, error) {
	c := &s.loginsSt
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := loginsOffTTL
	if c.on {
		ttl = loginsTTL
	}
	if !c.at.IsZero() && time.Since(c.at) < ttl {
		return c.v, c.err
	}
	c.v, c.err = s.logins().View()
	c.on, c.at = c.v.On, time.Now()
	return c.v, c.err
}

// loginsChanged drops the last look, and the badges named by it: the next
// poll sees what a change here did at once.
func (s *Server) loginsChanged() {
	c := &s.loginsSt
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
	s.deskStale()
}

// loginsKnown is what baton keeps of the desktop app's sign-ins, for its
// desktop lists (desktop.Desk.Known): an account the app was on that is
// none of baton's is named by it. nil while switching is off.
func (s *Server) loginsKnown() func(acct string) (org, email string, kept bool) {
	c, err := config.Load(s.opt.Paths.Config())
	if err != nil || !c.DesktopLoginsOn() {
		return nil
	}
	return desklogin.New(s.opt.Paths, accounts.Registry{}).Known
}

// loginsSummary is what the state poll says of switching the desktop app's
// sign-in: enough for the setting, the banner and the account rows. The
// card reads the whole look (desktop/logins).
type loginsSummary struct {
	On      bool `json:"on"`
	Running bool `json:"running"`
	CutOff  bool `json:"cut_off"`           // a switch was cut off: Put it back (or Leave it as it is)
	Kept    int  `json:"kept"`              // sign-ins kept
	Unnamed int  `json:"unnamed,omitempty"` // what signed-out apps left, set aside with them
	// Leftover: copies a switch that stopped early left in the app's folder
	// (desklogin.View.Leftover), which Tidy deletes.
	Leftover int    `json:"leftover,omitempty"`
	Problem  string `json:"problem,omitempty"` // why it cannot work on this PC (a dl_* code)
	// ProblemDetail says more of Problem (which layout of the app's folders it is).
	ProblemDetail string `json:"problem_detail,omitempty"`
	Active        string `json:"active,omitempty"` // what the account the app is on is called ("" signed out)
	Store         string `json:"store"`            // where they are kept, for the notice
}

// loginsNow is the summary for the state poll ("nil": nothing to say, no
// desktop app and nothing kept), and marks baton's accounts the app is
// signed in to, or keeps a sign-in of, while switching is on.
func (s *Server) loginsNow(cfg config.Config, desktopApp bool, accts []accountView) *loginsSummary {
	if !desktopApp && !cfg.DesktopLogins && !isDir(s.opt.Paths.DesktopLogins()) {
		return nil
	}
	v, err := s.loginsView()
	if err != nil {
		return nil
	}
	sum := &loginsSummary{On: v.On, Running: v.Running, CutOff: v.CutOff, Kept: v.Kept, Unnamed: len(v.Unnamed),
		Leftover: v.Leftover, Problem: v.Problem, ProblemDetail: v.ProblemDetail, Store: v.Store}
	for _, g := range v.Logins {
		if g.Active {
			sum.Active = g.Label
		}
		if !v.On {
			continue
		}
		for i := range accts {
			if !slices.Contains(g.Baton, accts[i].ID) {
				continue
			}
			accts[i].DesktopKept = g.Kept
			switch {
			case g.Active:
				accts[i].DesktopLogin = "active"
			case g.Kept:
				accts[i].DesktopLogin = "kept"
			}
		}
	}
	return sum
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// ---- the setting ---------------------------------------------------------------

// configDesktopLogins turns switching on (with its notice accepted: the
// page shows it, never ticked for the user) or off (the page asks first:
// every kept sign-in is deleted). Neither goes through config.Setter, which
// refuses both: TurnOn checks the PC first, and TurnOff deletes the store.
func (s *Server) configDesktopLogins(w http.ResponseWriter, value string, acknowledge bool) {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_value", "desktop_logins must be true or false", "")
		return
	}
	if !on {
		n, err := desklogin.TurnOff(s.opt.Paths)
		s.loginsChanged()
		if err != nil {
			s.loginsFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n})
		return
	}
	err = desklogin.TurnOn(s.opt.Paths, acknowledge)
	s.loginsChanged()
	if errors.Is(err, config.ErrNeedsDesktopAcknowledgement) {
		fail(w, http.StatusConflict, "needs_desktop_ack", err.Error(), "")
		return
	}
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- the card --------------------------------------------------------------------

// apiLogins is the card's content: every sign-in kept, the account the app
// is on, whether it runs, and a switch cut off. Also while switching is off
// (what is left from before, a switch to put back).
func (s *Server) apiLogins(w http.ResponseWriter, r *http.Request) {
	v, err := s.loginsView()
	if err != nil {
		s.loginsBroke(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// apiLoginsUse switches the desktop app to the sign-in kept for an account
// (its id), and opens the app afterwards when asked. With quit (the user
// agreed to it, the app running), the app is quit first when nothing in it
// is working, and opened again after.
func (s *Server) apiLoginsUse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Open    bool   `json:"open"`
		Quit    bool   `json:"quit"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	l := s.logins()
	var res desklogin.Switched
	var err error
	if req.Quit {
		res, err = l.UseAfterQuit(req.Account)
	} else {
		res, err = l.Use(req.Account)
	}
	s.switched(w, l, res, err, req.Open)
}

// apiLoginsAdd keeps the sign-in the app has, then empties it on this PC
// only, so the user can sign in to another account there.
func (s *Server) apiLoginsAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Open bool `json:"open"`
		Quit bool `json:"quit"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	l := s.logins()
	var res desklogin.Switched
	var err error
	if req.Quit {
		res, err = l.BlankAfterQuit()
	} else {
		res, err = l.Blank()
	}
	s.switched(w, l, res, err, req.Open)
}

// switched answers a switch (Use or Blank). baton's desktop lists follow
// the account the app is on now before it opens, not in the background as
// after a tick: the app reads them as it starts. Opening it is a
// convenience; when baton cannot, the user does (open_error says why).
//
// An app baton quit for the switch (res.Quit) opens again whatever was
// asked, also when the switch then failed or was refused: on the sign-in
// it has then. The answer says so (quit, and opened or open_error).
func (s *Server) switched(w http.ResponseWriter, l *desklogin.Logins, res desklogin.Switched, err error, open bool) {
	s.loginsChanged()
	if err != nil {
		var extra map[string]any
		if res.Quit {
			s.ReconcileDesktop()
			extra = map[string]any{"quit": true}
			s.launchAfter(l, true, extra)
		}
		s.loginsFailWith(w, err, extra)
		return
	}
	s.ReconcileDesktop()
	out := map[string]any{"ok": true, "from": res.From, "to": res.To, "kept": res.Kept, "recovered": res.Recovered}
	if res.Stashed != "" {
		out["stashed"] = res.Stashed
	}
	if res.Quit {
		out["quit"] = true
	}
	s.launchAfter(l, open || res.Quit, out)
	writeJSON(w, http.StatusOK, out)
}

// launchAfter opens the desktop app when the user asked, and says how it
// went in out.
func (s *Server) launchAfter(l *desklogin.Logins, open bool, out map[string]any) {
	if !open {
		return
	}
	if err := l.Launch(); err != nil {
		code := desklogin.Code(err)
		if code == "" {
			code = desklogin.CodeLaunchUnsupported
		}
		out["open_error"] = code
		return
	}
	out["opened"] = true
}

// apiLoginsRecover puts back a switch that was cut off (the app must be
// fully quit). It works while switching is off: the banner offers it then
// too.
func (s *Server) apiLoginsRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Open bool `json:"open"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	l := s.logins()
	undone, err := l.Recover()
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	out := map[string]any{"ok": true, "undone": undone}
	if undone {
		s.ReconcileDesktop()
		s.launchAfter(l, req.Open, out)
	}
	writeJSON(w, http.StatusOK, out)
}

// apiLoginsSettle leaves the desktop app as it is now, for a switch that
// was cut off and that Recover will not or cannot put back (the banner
// offers it then, and asks first): the app stays on the sign-in it has. It
// works while switching is off, with the app fully quit.
func (s *Server) apiLoginsSettle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Open bool `json:"open"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	l := s.logins()
	settled, err := l.Settle()
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	out := map[string]any{"ok": true, "settled": settled}
	if settled {
		s.ReconcileDesktop()
		s.launchAfter(l, req.Open, out)
	}
	writeJSON(w, http.StatusOK, out)
}

// apiLoginsTidy deletes the copies switches that stopped early left in the
// app's folder (desklogin.Tidy). The app never reads them, so this works
// while it runs, and while switching is off.
func (s *Server) apiLoginsTidy(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	n, err := s.logins().Tidy()
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n})
}

// apiLoginsDrop deletes one kept sign-in for good, or what a signed-out app
// left (account is then its unnamed-<when> name). The page asks first.
func (s *Server) apiLoginsDrop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.logins().Drop(req.Account)
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiLoginsDropAll deletes every kept sign-in, also while switching is off
// (what is left from before) and while the app runs. The page asks first.
func (s *Server) apiLoginsDropAll(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	n, err := s.logins().DropAll()
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n})
}

// apiLoginsRename names a kept sign-in ("" takes the name off).
func (s *Server) apiLoginsRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Label   string `json:"label"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := s.logins().Rename(req.Account, req.Label)
	s.loginsChanged()
	if err != nil {
		s.loginsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// uninstallRefused answers (true) when `baton uninstall` would refuse for a
// switch of the desktop app's sign-in that was cut off: it settles that
// switch, which it does not while the app runs or from inside a Claude Code
// (CLAUDECODE, which the console it runs in gets from this window). Said
// here, before this window closes, as the console would say it. The look
// waits for the lock (CutOffNow), as the uninstall's own does: another
// look holding it a moment must not read as no switch cut off.
func (s *Server) uninstallRefused(w http.ResponseWriter) bool {
	l := s.logins()
	cut, err := l.CutOffNow()
	switch {
	case err != nil:
		s.loginsFail(w, err)
	case !cut:
		return false
	case l.Running != nil && l.Running():
		fail(w, http.StatusConflict, "cut_off_running", "a switch of the desktop app's sign-in was cut off and the app is running", "")
	case os.Getenv("CLAUDECODE") != "":
		fail(w, http.StatusConflict, "cut_off_inside_claude", "a switch of the desktop app's sign-in was cut off; it is settled only outside Claude Code", "")
	default:
		return false
	}
	return true
}

// loginsFail answers a refused change: turned off is 404 (like the office
// turned off), any other refusal 409, with desklogin's detail (key and item
// names only, never a value of the app's settings) for the page to word.
func (s *Server) loginsFail(w http.ResponseWriter, err error) { s.loginsFailWith(w, err, nil) }

// loginsFailWith is loginsFail, its answer with extra's fields as well; a
// refusal because the app is working lists its sessions (working).
func (s *Server) loginsFailWith(w http.ResponseWriter, err error, extra map[string]any) {
	var de *desktop.Error
	var we *desklogin.WorkingError
	switch {
	case !errors.As(err, &de) || !strings.HasPrefix(de.Code, "dl_"):
		if len(extra) > 0 {
			s.logf("desktop app sign-ins: %v", err)
			out := map[string]any{"error": err.Error(), "code": "desktop_logins"}
			for k, v := range extra {
				out[k] = v
			}
			writeJSON(w, http.StatusInternalServerError, out)
			return
		}
		s.loginsBroke(w, err)
		return
	case errors.As(err, &we):
		extra = withField(extra, "working", we.Sessions)
	}
	status, detail := http.StatusConflict, de.Detail
	if de.Code == desklogin.CodeOff {
		status, detail = http.StatusNotFound, ""
	}
	if len(extra) == 0 {
		fail(w, status, de.Code, err.Error(), detail)
		return
	}
	out := map[string]any{"error": err.Error(), "code": de.Code}
	if detail != "" {
		out["details"] = detail
	}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, status, out)
}

func withField(m map[string]any, k string, v any) map[string]any {
	if m == nil {
		m = map[string]any{}
	}
	m[k] = v
	return m
}

// loginsBroke answers a failure that is no refusal (baton's settings file
// unreadable, say): the page says only that it could not be done now, so
// what went wrong goes to the log.
func (s *Server) loginsBroke(w http.ResponseWriter, err error) {
	s.logf("desktop app sign-ins: %v", err)
	fail(w, http.StatusInternalServerError, "desktop_logins", err.Error(), "")
}
