package gui

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/share"
	"baton/internal/usage"
)

// Plan usage between sessions. A session's status line keeps the usage of
// the account it runs on current; for the others the window asks Claude Code
// (usage.Ask): when the Accounts tab shows, for the accounts whose numbers
// are older than usageStale, and after a Claude Code of the user's own setup
// (the desktop app's Code tab, a terminal, an IDE) finished a reply, for the
// baton accounts that are its Claude account (the limits are the account's,
// wherever it is used). "Check sign-ins" asks for every account (`baton
// doctor`). Nothing is asked otherwise: each check runs Claude Code for a few
// seconds. One check at a time, and an account at most once a usageGap.

const (
	usageStale = 5 * time.Minute
	usageGap   = time.Minute
)

// usageAfterTurn: how long after a reply ends its account is asked for (the
// app may go on with another turn at once). A test shortens it.
var usageAfterTurn = 3 * time.Second

// usageAsks is what the window asks Claude Code for, and what it saw of the
// user's own Claude Code sessions at its last look.
type usageAsks struct {
	queue   []string             // accounts to ask for, in order
	current string               // the one asked for now
	asked   map[string]time.Time // when each was last asked for
	running bool
	turns   map[string]string // the user's own sessions: status by session id (nil: not looked yet)
	due     time.Time         // a reply ended: ask for its accounts then
}

// askUsage queues accounts for a check, unless queued, being asked for, or
// asked for within usageGap, and starts the one worker when none runs.
func (s *Server) askUsage(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &s.asks
	if u.asked == nil {
		u.asked = map[string]time.Time{}
	}
	for _, id := range ids {
		if id == "" || id == u.current || slices.Contains(u.queue, id) || time.Since(u.asked[id]) < usageGap {
			continue
		}
		u.queue = append(u.queue, id)
	}
	if len(u.queue) == 0 || u.running {
		return
	}
	u.running = true
	s.bg.Add(1)
	go s.usageWorker()
}

// usageWorker asks for the queued accounts, one at a time.
func (s *Server) usageWorker() {
	defer s.bg.Done()
	for {
		s.mu.Lock()
		u := &s.asks
		if len(u.queue) == 0 {
			u.running, u.current = false, ""
			s.mu.Unlock()
			return
		}
		id := u.queue[0]
		u.queue, u.current = u.queue[1:], id
		u.asked[id] = time.Now()
		s.mu.Unlock()
		s.askOne(id)
	}
}

// askOne asks Claude Code for one account's usage, as far as it can be
// asked for now. A failure, even a panic, is only logged.
func (s *Server) askOne(id string) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("usage of %s: %v", id, r)
		}
	}()
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	a := reg.Get(id)
	if a == nil || !usageAskable(*a) {
		return
	}
	info, pending := s.claudeInfo()
	if pending || !info.Found || info.Path == "" {
		return
	}
	ask := s.opt.AskUsage
	if ask == nil {
		ask = usage.Ask
	}
	if _, err := ask(info.Path, a.ProfileDir, s.opt.Paths.UsageDir(), id); err != nil {
		s.logf("usage of %s: %v", id, err)
	}
}

// usageAskable is the CLI's rule (cmd/baton): a signed-in Claude
// subscription, no disabled duplicate, and no session on it now (its status
// line reports it then).
func usageAskable(a accounts.Account) bool {
	return a.LoggedIn && strings.EqualFold(a.AuthMethod, "claude.ai") && !a.DisabledAsDuplicate && !claude.BusyProfile(a.ProfileDir)
}

// usageChecking reports whether account id is queued or asked for now.
func (s *Server) usageChecking(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asks.current == id || slices.Contains(s.asks.queue, id)
}

// apiUsageRefresh asks for the usage of every account whose numbers are
// older than usageStale (the Accounts tab shows).
func (s *Server) apiUsageRefresh(w http.ResponseWriter, r *http.Request) {
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	var ids []string
	for _, a := range reg.Accounts {
		if !usageAskable(a) {
			continue
		}
		if u, ok := usage.Load(s.opt.Paths.UsageDir(), a.ID); ok && time.Since(u.Updated) < usageStale {
			continue
		}
		ids = append(ids, a.ID)
	}
	s.askUsage(ids...)
	writeJSON(w, http.StatusOK, map[string]int{"asked": len(ids)})
}

// watchTurns looks at the user's own Claude Code sessions (DesktopLoop, every
// second): once one finished a reply (busy, then idle or gone), it asks for
// the usage of the baton accounts that are its Claude account, a moment
// later. A session that waits for an answer is still in its turn.
func (s *Server) watchTurns() {
	defer func() {
		if r := recover(); r != nil {
			s.logf("usage: %v", r)
		}
	}()
	home := share.UserHome(s.opt.Paths.ProfilesDir())
	if home == "" {
		return
	}
	now := map[string]string{}
	for _, l := range claude.ReadSessions(home) {
		if l.SessionID != "" {
			now[l.SessionID] = l.Status
		}
	}
	s.mu.Lock()
	u := &s.asks
	for id, was := range u.turns {
		if st := now[id]; was == "busy" && st != "busy" && st != "waiting" {
			u.due = time.Now().Add(usageAfterTurn)
		}
	}
	u.turns = now
	ready := !u.due.IsZero() && !time.Now().Before(u.due)
	if ready {
		u.due = time.Time{}
	}
	s.mu.Unlock()
	if ready {
		s.askUsage(s.ownAccounts(home)...)
	}
}

// ownAccounts are the baton accounts that are the Claude account the user's
// own Claude Code (in home) is signed in to.
func (s *Server) ownAccounts(home string) []string {
	acct, org, _ := claude.OAuthIdentity(claude.UserClaudeJSONDir(home))
	if acct == "" {
		return nil
	}
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	var ids []string
	for _, a := range reg.Accounts {
		if strings.EqualFold(a.AccountUUID, acct) && (org == "" || a.OrgID == "" || strings.EqualFold(a.OrgID, org)) {
			ids = append(ids, a.ID)
		}
	}
	return ids
}
