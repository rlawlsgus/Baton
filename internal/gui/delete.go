package gui

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"baton/internal/accounts"
	"baton/internal/convdel"
	"baton/internal/share"
)

// Deleting a conversation from the window (internal/convdel does it): the
// dialog first asks what a delete would do and what keeps it from going now,
// then has it done. Everything goes to the Windows Recycle Bin, never away
// for good, and the answers name no file: the paths stay in this server.

// delRequest is convdel's request for conversation id, picked in config
// folder where (an account id, or "claude-code" for the user's own setup).
func (s *Server) delRequest(id, where string) convdel.Request {
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	var dirs []convdel.Dir
	for _, a := range reg.Accounts {
		dirs = append(dirs, convdel.Dir{Where: a.ID, Path: a.ProfileDir})
	}
	if h := share.UserHome(s.opt.Paths.ProfilesDir()); h != "" {
		dirs = append(dirs, convdel.Dir{Where: "claude-code", Path: h})
	}
	return convdel.Request{ID: id, Where: where, Paths: s.opt.Paths, Dirs: dirs, Desk: s.desk(),
		Alive: supervisorAlive, Office: s.officeForget, TempRoot: s.delTemp}
}

// officeForget has the office let go of a conversation's files before they
// move: it reads the files of the one it watches every tick, and a folder
// can't go to the Recycle Bin while a file in it is open. An office nobody
// looked at has nothing open.
func (s *Server) officeForget(id string) {
	s.mu.Lock()
	tr := s.office
	s.mu.Unlock()
	if tr != nil {
		tr.Forget(id)
	}
}

// convGate keeps a desktop tick and a delete of the same conversation apart
// in this window: a tick made after the delete took the conversation off the
// desktop app would show it there again, pointing at files on their way to
// the Recycle Bin. A delete holds it; a tick only tries it.
func (s *Server) convGate(id string) *sync.RWMutex {
	id = strings.ToLower(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gates == nil {
		s.gates = map[string]*sync.RWMutex{}
	}
	g := s.gates[id]
	if g == nil {
		g = &sync.RWMutex{}
		s.gates[id] = g
	}
	return g
}

// delPlan is the plan the dialog shows: convdel.Plan, which names no file or
// folder, and whether it is a leftover (see leftover).
type delPlan struct {
	convdel.Plan
	Leftover bool `json:"leftover"`
}

// apiDeletePlan is what deleting ?session_id= (picked in ?where=) would do.
func (s *Server) apiDeletePlan(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, where := q.Get("session_id"), q.Get("where")
	if !sessionIDRx.MatchString(id) || where == "" {
		fail(w, http.StatusBadRequest, convdel.CodeBadSession, "unknown session", "")
		return
	}
	p, err := convdel.Look(s.delRequest(id, where))
	if err != nil {
		s.delFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delPlan{Plan: p, Leftover: leftover(p)})
}

// leftover tells a plan of only what an earlier delete that stopped half
// way left behind: no name of the conversation's file is among what goes.
// The page can't tell by the title: a conversation whose file is empty, or
// whose rows can't be read, has no title, folder or source either, and its
// file goes. Nor is one behind a link, or in a folder baton can't read,
// a leftover: nothing of it is counted as going, but the conversation is
// there (the plan is refused as linked). `baton sessions delete` decides
// the same way (printDeletePlan).
func leftover(p convdel.Plan) bool {
	if slices.ContainsFunc(p.Blockers, func(b convdel.Blocker) bool { return b.Code == convdel.CodeLinkedFolder }) {
		return false
	}
	return !slices.ContainsFunc(p.Paths(), func(path string) bool { return strings.EqualFold(filepath.Base(path), p.SessionID+".jsonl") })
}

// apiDelete moves a conversation to the Recycle Bin, once nothing keeps it.
// Deletes run one at a time, and never while this window removes an account:
// that moves the account's conversations to the user's own folder, so a
// delete would plan files on the move (each waits for the other, seconds).
func (s *Server) apiDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Where     string `json:"where"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	id := strings.ToLower(req.SessionID)
	if !sessionIDRx.MatchString(id) || req.Where == "" {
		fail(w, http.StatusBadRequest, convdel.CodeBadSession, "unknown session", "")
		return
	}
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	g := s.convGate(id)
	g.Lock()
	defer g.Unlock()
	res, err := convdel.Delete(s.delRequest(id, req.Where))
	// Something may have gone, even when not all of it did.
	s.deskStale()
	if err != nil {
		s.delFail(w, err)
		return
	}
	out := map[string]any{"ok": true, "moved": res.Moved, "desktop_off": res.DesktopOff}
	if res.Warning != "" {
		out["warning"] = res.Warning // reopened: a Claude Code opened it again meanwhile
	}
	writeJSON(w, http.StatusOK, out)
}

// delError is a refused or failed delete: the page says why by Code, and by
// the first blocker's detail (what has it open, which drive).
type delError struct {
	apiError
	Blocker *convdel.Blocker `json:"blocker,omitempty"`
	// DesktopOff: the delete took it off these lists of the desktop app
	// before it stopped ("" is the app's own account).
	DesktopOff []string `json:"desktop_off,omitempty"`
	Moved      int      `json:"moved,omitempty"` // partly: what went
	Left       int      `json:"left,omitempty"`  // partly: what is still there
}

// delFail answers a refused or failed delete. What the page cannot word
// (a failure that is no refusal, or the Recycle Bin's own reason, which
// names files) goes to the log, never to the page.
func (s *Server) delFail(w http.ResponseWriter, err error) {
	var e *convdel.Error
	if !errors.As(err, &e) {
		s.logf("delete a conversation: %v", err)
		fail(w, http.StatusInternalServerError, "delete", "the conversation was not deleted", "")
		return
	}
	if e.Code == convdel.CodeRecycleFailed || e.Code == convdel.CodePartly || e.Code == convdel.CodeChanged {
		s.logf("delete a conversation: %v", e)
	}
	// The message names no file: convdel's own error may (the Recycle Bin's).
	out := delError{apiError: apiError{Error: "the conversation was not deleted: " + e.Code, Code: e.Code}, DesktopOff: e.DesktopOff}
	if len(e.Blockers) > 0 {
		b := e.Blockers[0]
		out.Blocker = &b
	}
	if e.Code == convdel.CodePartly {
		out.Error = "only part of the conversation went to the Recycle Bin"
		out.Details = strconv.Itoa(e.Left)
		out.Moved, out.Left = e.Moved, e.Left
	}
	writeJSON(w, e.Status(), out)
}

// sessionOfFile is the conversation id a transcript is named by ("" for a
// path that is none).
func sessionOfFile(transcript string) string {
	id, ok := strings.CutSuffix(strings.ToLower(filepath.Base(transcript)), ".jsonl")
	if !ok || !sessionIDRx.MatchString(id) {
		return ""
	}
	return id
}

// deskStale has the next state poll read baton's desktop index again, so a
// badge of a deleted conversation goes at once.
func (s *Server) deskStale() {
	d := &s.deskSt
	d.mu.Lock()
	d.ixSize = -1
	d.mu.Unlock()
}
