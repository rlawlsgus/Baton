package gui

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/desktop"
)

// The window's part in showing sessions in the Claude desktop app
// (internal/desktop): the dialog that picks the accounts, the badges on
// session rows, and keeping the app's lists in line while the window is open.

// deskState is what the state poll knows of the desktop feature, read again
// only when it may have changed.
type deskState struct {
	mu     sync.Mutex
	dataAt time.Time // when the desktop app's data folder was looked for
	data   string
	ixSize int64
	ixMod  time.Time
	ix     desktop.Index
	badges map[string]desktop.Badge
}

// deskIndex is the desktop app's data folder ("" without the app: the
// feature is not offered) and baton's index, read again only when it changed.
func (s *Server) deskIndex() (string, desktop.Index, map[string]desktop.Badge) {
	d := &s.deskSt
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.dataAt) > 30*time.Second {
		d.data, d.dataAt = desktop.DataDir(), time.Now()
	}
	fi, err := os.Stat(s.opt.Paths.Desktop())
	switch {
	case err != nil:
		d.ix, d.badges, d.ixSize, d.ixMod = desktop.Index{}, nil, 0, time.Time{}
	case fi.Size() != d.ixSize || !fi.ModTime().Equal(d.ixMod):
		if ix, err := desktop.LoadIndex(s.opt.Paths.Desktop()); err == nil {
			d.ix, d.badges = ix, ix.Badges()
			d.ixSize, d.ixMod = fi.Size(), fi.ModTime()
		}
	}
	return d.data, d.ix, d.badges
}

// deskNow is whether the desktop app has a data folder (the feature is
// offered), the badges by conversation id and baton's threads.
func (s *Server) deskNow() (bool, map[string]desktop.Badge, []*desktop.Thread) {
	data, ix, badges := s.deskIndex()
	return data != "", badges, ix.Threads
}

func (s *Server) desk() *desktop.Desk {
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	return desktop.New(s.opt.Paths, reg)
}

// conversationOf finds the file of conversation id: the one the newest run
// on it names, else in an account's profile, else in the user's own setup
// (the profile's name first: it is the one baton's sessions write by).
func (s *Server) conversationOf(id string) string {
	best, bestAt := "", time.Time{}
	for _, j := range s.cache.journals(s.opt.Paths.RunsDir()) {
		if strings.EqualFold(j.SessionID, id) && j.TranscriptPath != "" && j.Updated.After(bestAt) && isFile(j.TranscriptPath) {
			best, bestAt = j.TranscriptPath, j.Updated
		}
	}
	if best != "" {
		return best
	}
	return claude.FindTranscript(s.allConfigDirs(), id)
}

// apiDesktopPlaces is the dialog's content for ?session_id=: the accounts
// whose desktop app can list the session, and where it shows now.
func (s *Server) apiDesktopPlaces(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.URL.Query().Get("session_id"))
	if !sessionIDRx.MatchString(id) {
		fail(w, http.StatusBadRequest, "bad_session", "unknown session", "")
		return
	}
	t := s.conversationOf(id)
	if t == "" {
		fail(w, http.StatusConflict, "no_file", "the session has no conversation yet", "")
		return
	}
	v, err := s.desk().Places(desktop.Conversation{ID: id, Transcript: t})
	if err != nil {
		deskFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// apiDesktopApply shows the session in the desktop app for exactly the
// places named (as far as baton decides), and says what changed.
func (s *Server) apiDesktopApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string   `json:"session_id"`
		Places    []string `json:"places"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	id := strings.ToLower(req.SessionID)
	if !sessionIDRx.MatchString(id) || len(req.Places) > 64 {
		fail(w, http.StatusBadRequest, "bad_session", "unknown session", "")
		return
	}
	t := s.conversationOf(id)
	if t == "" {
		fail(w, http.StatusConflict, "no_file", "the session has no conversation yet", "")
		return
	}
	res, err := s.desk().Apply(desktop.Conversation{ID: id, Transcript: t}, req.Places)
	if err != nil {
		deskFail(w, err)
		return
	}
	go s.ReconcileDesktop()
	writeJSON(w, http.StatusOK, res)
}

func deskFail(w http.ResponseWriter, err error) {
	var de *desktop.Error
	if errors.As(err, &de) {
		status := http.StatusConflict
		if de.Code == "unknown_place" {
			status = http.StatusBadRequest
		}
		fail(w, status, de.Code, err.Error(), "")
		return
	}
	fail(w, http.StatusInternalServerError, "desktop", err.Error(), "")
}

// ReconcileDesktop brings the desktop app's lists in line with what baton
// shows there (see desktop.Reconcile). It is cheap when baton shows nothing.
// A failure, even a panic, is only logged: the window goes on.
func (s *Server) ReconcileDesktop() {
	s.deskMu.Lock() // one at a time; a second one would only find the same
	defer s.deskMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			s.logf("desktop app: %v", r)
		}
	}()
	if err := s.desk().Reconcile(); err != nil {
		s.logf("desktop app: %v", err)
	}
}

// deskWait is what DesktopLoop knows between its looks: whether a change
// waited on a running desktop app at the last one.
type deskWait struct{ open bool }

// closed reports whether the desktop app was just closed while a change
// waited for that (waiting: one waits now; running: the app runs now).
func (w *deskWait) closed(waiting bool, running func() bool) bool {
	open := waiting && running()
	was := w.open
	w.open = open
	return was && !open
}

// DesktopLoop keeps the desktop app's lists in line while the window runs:
// at once, then every 12 seconds, until stop is closed. While a change to
// the list of the account the desktop app is signed in to waits for the app
// to close (an untick, a record to point at the session's conversation now),
// it looks every second whether the app is still open, and makes the change
// as soon as it is not: the app writes that list back as it quits and reads
// it again as soon as it is opened, so a quick restart must find it done.
func (s *Server) DesktopLoop(stop <-chan struct{}) {
	s.ReconcileDesktop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	last := time.Now()
	var w deskWait
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		if s.deskClosed(&w) || time.Since(last) >= 12*time.Second {
			s.ReconcileDesktop()
			last = time.Now()
		}
	}
}

// deskClosed reports whether the desktop app was just closed while a change
// waited for that (see DesktopLoop). A failure here only skips one look.
func (s *Server) deskClosed(w *deskWait) (closed bool) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("desktop app: %v", r)
			closed = false
		}
	}()
	data, ix, _ := s.deskIndex()
	return data != "" && w.closed(ix.Waiting(data), desktop.AppRunning)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
