package gui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/usage"
	"baton/internal/winproc"

	"golang.org/x/sys/windows"
)

type accountView struct {
	ID                  string     `json:"id"`
	Enabled             bool       `json:"enabled"`
	DisabledAsDuplicate bool       `json:"disabled_as_duplicate"`
	LoggedIn            bool       `json:"logged_in"`
	Email               string     `json:"email"`
	Plan                string     `json:"plan"`
	Org                 string     `json:"org"`
	AuthMethod          string     `json:"auth_method"`
	Priority            int        `json:"priority"`
	CheckedAt           *time.Time `json:"checked_at,omitempty"`
	Status              string     `json:"status"` // ok | limited | needs_login | blocked | signed_out
	RetryAt             *time.Time `json:"retry_at,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	SigningIn           bool       `json:"signing_in"` // a sign-in window from this GUI is open for it
	InUse               bool       `json:"in_use"`     // a claude runs on this profile now
	// Usage: the 5-hour and weekly limits as a baton session on this
	// account last saw them (nil until one has).
	Usage *usage.Usage `json:"usage,omitempty"`
}

type runView struct {
	RunID      string    `json:"run_id"`
	Account    string    `json:"account"`
	SwitchMode string    `json:"switch_mode"` // the run's own, which can be stricter than the setting
	State      string    `json:"state"`
	SessionID  string    `json:"session_id"`
	Cwd        string    `json:"cwd"`
	Started    time.Time `json:"started"`
}

type switchView struct {
	At     time.Time `json:"at"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Reason string    `json:"reason"`
}

type jobView struct {
	ID       int           `json:"id"`
	Kind     string        `json:"kind"` // add | login | run
	Target   string        `json:"target"`
	Started  time.Time     `json:"started"`
	Running  bool          `json:"running"`
	ExitCode int           `json:"exit_code"`
	Result   *SignInResult `json:"result,omitempty"`
}

type configView struct {
	SwitchMode            string `json:"switch_mode"`
	OnExhausted           string `json:"on_exhausted"`
	CooldownSeconds       int    `json:"cooldown_seconds"`
	MaxSwitchesPerHour    int    `json:"max_switches_per_hour"`
	ExhaustedRetryMinutes int    `json:"exhausted_retry_minutes"`
	ContinuePrompt        string `json:"continue_prompt"`
	ShareUserConfig       bool   `json:"share_user_config"`
	AllowDesktopBundle    bool   `json:"allow_desktop_bundle"`
	ClaudePath            string `json:"claude_path"`
	GUILanguage           string `json:"gui_language"`
	RiskAcknowledged      bool   `json:"risk_acknowledged"`
}

// recentView is an ended session that can be continued.
type recentView struct {
	SessionID   string    `json:"session_id"`
	Where       string    `json:"where"`
	Cwd         string    `json:"cwd"`
	Account     string    `json:"account"`
	Title       string    `json:"title"`
	FirstPrompt string    `json:"first_prompt"`
	Ended       time.Time `json:"ended"`
}

type stateView struct {
	Version       string        `json:"version"`
	Lang          string        `json:"lang"`
	Home          string        `json:"home"`
	Claude        ClaudeInfo    `json:"claude"`
	ClaudePending bool          `json:"claude_pending"`
	Config        configView    `json:"config"`
	Accounts      []accountView `json:"accounts"`
	Runs          []runView     `json:"runs"`
	Switches      []switchView  `json:"switches"`
	Jobs          []jobView     `json:"jobs"`
	RecentFolders []string      `json:"recent_folders"`
	Recent        []recentView  `json:"recent_sessions"`
	Elevated      bool          `json:"elevated"` // running as administrator
	Installed     bool          `json:"installed"`
	CanInstall    bool          `json:"can_install"`
	Checking      bool          `json:"checking"`
	Problems      []string      `json:"problems,omitempty"`
}

func (s *Server) state() stateView {
	p := s.opt.Paths
	v := stateView{Version: s.opt.Version, Home: p.Root, Accounts: []accountView{}, Runs: []runView{},
		Switches: []switchView{}, Jobs: []jobView{}, RecentFolders: []string{}, Recent: []recentView{},
		Elevated: windows.GetCurrentProcessToken().IsElevated()}

	cfg, err := config.Load(p.Config())
	if err != nil {
		v.Problems = append(v.Problems, err.Error())
	}
	v.Config = configView{
		SwitchMode: cfg.SwitchMode, OnExhausted: cfg.OnExhausted, CooldownSeconds: cfg.CooldownSeconds,
		MaxSwitchesPerHour: cfg.MaxSwitchesPerHour, ExhaustedRetryMinutes: cfg.ExhaustedRetryMinutes,
		ContinuePrompt: cfg.ContinuePrompt, ShareUserConfig: cfg.ShareEnabled(), AllowDesktopBundle: cfg.DesktopBundleAllowed(),
		ClaudePath: cfg.ClaudePath, GUILanguage: cfg.GUILanguage,
		RiskAcknowledged: cfg.RiskAcknowledged >= config.DisclosureVersion,
	}
	v.Lang = cfg.GUILanguage
	if v.Lang == "" {
		v.Lang = s.opt.Lang
	}

	reg, err := accounts.Load(p.Accounts())
	if err != nil {
		v.Problems = append(v.Problems, err.Error())
	}
	st, err := ledger.Load(p.State())
	if err != nil {
		v.Problems = append(v.Problems, p.State()+" is unreadable ("+err.Error()+"); usage limits and blocked marks are not shown")
	}

	s.mu.Lock()
	signingIn := map[string]bool{}
	for _, j := range s.jobs {
		v.Jobs = append(v.Jobs, j.view())
		if j.running() && (j.kind == "add" || j.kind == "login") {
			signingIn[j.target] = true
		}
	}
	v.Checking = s.checking
	s.mu.Unlock()

	now := time.Now()
	for _, a := range byPriority(reg.Accounts) {
		ls := st.StatusOf(a.ID)
		av := accountView{ID: a.ID, Enabled: a.Enabled, DisabledAsDuplicate: a.DisabledAsDuplicate, LoggedIn: a.LoggedIn,
			Email: a.Email, Plan: a.Subscription, Org: a.OrgName, AuthMethod: a.AuthMethod, Priority: a.Priority,
			Detail: ls.Detail, SigningIn: signingIn[a.ID], InUse: claude.BusyProfile(a.ProfileDir)}
		if u, ok := usage.Load(p.UsageDir(), a.ID); ok {
			cur := u.Current(now)
			av.Usage = &cur
		}
		if !a.CheckedAt.IsZero() {
			t := a.CheckedAt
			av.CheckedAt = &t
		}
		switch {
		case ls.Status == ledger.StatusBlocked:
			av.Status = "blocked"
		case ls.Status == ledger.StatusNeedsLogin:
			av.Status = "needs_login"
		case !a.LoggedIn:
			av.Status = "signed_out"
		case ls.Status == ledger.StatusExhausted && now.Before(ls.RetryAt):
			av.Status = "limited"
		default:
			av.Status = "ok"
		}
		if now.Before(ls.RetryAt) {
			t := ls.RetryAt
			av.RetryAt = &t
		}
		v.Accounts = append(v.Accounts, av)
	}

	// The window polls often: journals are read once per poll (and parsed
	// again only when they change), and each supervisor is checked once.
	journals := s.cache.journals(p.RunsDir())
	alive := map[string]bool{}
	for _, j := range journals {
		if j.State != run.StateDone && supervisorAlive(j.SupervisorPID, j.SupervisorCreated) {
			alive[j.RunID] = true
			v.Runs = append(v.Runs, runView{RunID: j.RunID, Account: j.Account, SwitchMode: j.SwitchMode, State: j.State,
				SessionID: j.SessionID, Cwd: j.Cwd, Started: j.Started})
		}
	}
	sort.Slice(v.Runs, func(i, k int) bool { return v.Runs[i].Started.Before(v.Runs[k].Started) })
	for i := len(st.Switches) - 1; i >= 0 && len(v.Switches) < 10; i-- {
		sw := st.Switches[i]
		v.Switches = append(v.Switches, switchView{At: sw.At, From: sw.From, To: sw.To, Reason: sw.Reason})
	}
	v.RecentFolders = recentFolders(journals, 8)
	v.Recent = s.recentSessions(journals, alive, v.Runs, 6)
	v.Claude, v.ClaudePending = s.claudeInfo()
	if s.opt.Installed != nil {
		v.Installed, v.CanInstall = s.opt.Installed()
	}
	return v
}

// claudeInfo returns the cached answer and refreshes it in the background
// when it is older than a minute (finding claude runs `claude --version`).
func (s *Server) claudeInfo() (ClaudeInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.claudeAt.IsZero()
	if time.Since(s.claudeAt) > time.Minute && s.opt.Claude != nil && !s.claudeBusy {
		s.claudeBusy = true
		go func() {
			info := s.opt.Claude()
			s.mu.Lock()
			s.claude, s.claudeAt, s.claudeBusy = info, time.Now(), false
			s.mu.Unlock()
		}()
	}
	return s.claude, pending
}

func supervisorAlive(pid int, created time.Time) bool {
	got := winproc.ProcessCreated(pid)
	if got.IsZero() {
		return false
	}
	d := got.Sub(created)
	return d > -time.Second && d < time.Second
}

func byPriority(in []accounts.Account) []accounts.Account {
	out := append([]accounts.Account{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// readCache keeps what the state poll read last time, by file size and
// time, so unchanged journals are not parsed again (claude.Describe does
// the same for transcripts).
type readCache struct {
	mu   sync.Mutex
	runs map[string]cachedJournal
}

type cachedJournal struct {
	size int64
	mod  time.Time
	j    run.Journal
}

func (c *readCache) journals(runsDir string) []run.Journal {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := map[string]cachedJournal{}
	var out []run.Journal
	dirs, _ := os.ReadDir(runsDir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(runsDir, d.Name())
		fi, err := os.Stat(run.JournalPath(dir))
		if err != nil {
			continue
		}
		e, ok := c.runs[dir]
		if !ok || e.size != fi.Size() || !e.mod.Equal(fi.ModTime()) {
			j, err := run.LoadJournal(dir)
			if err != nil {
				continue
			}
			e = cachedJournal{fi.Size(), fi.ModTime(), j}
		}
		next[dir] = e
		out = append(out, e.j)
	}
	c.runs = next
	return out
}

// recentSessions are conversations of ended runs whose transcript still
// exists, newest first, without the ones running now. A run whose
// supervisor is gone ended too (its window was closed), whatever its
// journal last said.
func (s *Server) recentSessions(journals []run.Journal, alive map[string]bool, running []runView, max int) []recentView {
	busy := map[string]bool{}
	for _, r := range running {
		busy[r.SessionID] = true
	}
	// Newest conversation first, by when its transcript was last written: a
	// run that ended without saving its journal (power loss) has an old
	// journal time.
	type candidate struct {
		j  run.Journal
		fi os.FileInfo
	}
	var all []candidate
	for _, j := range journals {
		if j.TranscriptPath == "" || busy[j.SessionID] || (j.State != run.StateDone && alive[j.RunID]) {
			continue
		}
		if fi, err := os.Stat(j.TranscriptPath); err == nil && !fi.IsDir() {
			all = append(all, candidate{j, fi})
		}
	}
	sort.Slice(all, func(i, k int) bool { return all[i].fi.ModTime().After(all[k].fi.ModTime()) })
	where := s.configDirs()
	out := []recentView{}
	seen := map[string]bool{}
	for _, c := range all {
		j, fi := c.j, c.fi
		if seen[j.SessionID] {
			continue
		}
		seen[j.SessionID] = true
		w := ""
		for name, d := range where {
			if rel, err := filepath.Rel(filepath.Join(d, "projects"), j.TranscriptPath); err == nil && !strings.HasPrefix(rel, "..") {
				w = name
			}
		}
		if w == "" {
			continue
		}
		t := claude.Describe(j.TranscriptPath, fi)
		out = append(out, recentView{SessionID: j.SessionID, Where: w, Cwd: j.Cwd, Account: j.Account,
			Title: t.Title, FirstPrompt: t.FirstPrompt, Ended: fi.ModTime()})
		if len(out) == max {
			break
		}
	}
	return out
}

// recentFolders are the project folders of earlier runs, newest first.
func recentFolders(journals []run.Journal, max int) []string {
	type entry struct {
		cwd     string
		started time.Time
	}
	var all []entry
	for _, j := range journals {
		if j.Cwd != "" {
			all = append(all, entry{j.Cwd, j.Started})
		}
	}
	sort.Slice(all, func(i, k int) bool { return all[i].started.After(all[k].started) })
	out := []string{}
	seen := map[string]bool{}
	for _, e := range all {
		key := strings.ToLower(filepath.Clean(e.cwd))
		if seen[key] {
			continue
		}
		seen[key] = true
		if fi, err := os.Stat(e.cwd); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, e.cwd)
		if len(out) == max {
			break
		}
	}
	return out
}
