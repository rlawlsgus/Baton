package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/sched"
	"baton/internal/share"
	"baton/internal/update"
	"baton/internal/winui"
)

// job is a console window this GUI opened (sign-in or session).
type job struct {
	id         int
	kind       string
	target     string
	started    time.Time
	done       bool
	exitCode   int
	resultFile string        // sign-ins: where baton.exe writes the outcome
	result     *SignInResult // read when the window closes
}

// SignInResult is what `baton add/login --result-file` writes.
type SignInResult struct {
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Account       string `json:"account"`
	Email         string `json:"email,omitempty"`
	Plan          string `json:"plan,omitempty"`
	DuplicateOf   string `json:"duplicate_of,omitempty"`
	StillDisabled bool   `json:"still_disabled,omitempty"`
	NotSubscribed bool   `json:"not_subscribed,omitempty"`
	Message       string `json:"message,omitempty"`
}

func (j *job) running() bool { return !j.done }

func (j *job) view() jobView {
	return jobView{ID: j.id, Kind: j.kind, Target: j.target, Started: j.started, Running: !j.done, ExitCode: j.exitCode, Result: j.result}
}

var emailRx = regexp.MustCompile(`^[^\s@-][^\s@]*@[^\s@]+\.[^\s@]+$`)

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path == "session" {
		s.apiSession(w, r)
		return
	}
	pg := s.authed(r)
	if pg == nil {
		fail(w, http.StatusForbidden, "expired", "this baton window has expired; open baton again", "")
		return
	}
	if !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "forbidden", "forbidden", "")
		return
	}
	if path == "bye" && r.Method == http.MethodPost {
		// The page is closing (or reloading: then it comes back).
		seq, _ := strconv.Atoi(r.Header.Get(seqHeader))
		s.mu.Lock()
		pg.byeAt, pg.byeSeq = time.Now(), seq
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	get := map[string]func(http.ResponseWriter, *http.Request){
		"state":         s.apiState,
		"conversations": s.apiConversations,
		"changes":       s.apiChanges,
		"folder/info":   s.apiFolderInfo,
	}
	post := map[string]func(http.ResponseWriter, *http.Request){
		"accounts/add":     s.apiAddOrLogin("add"),
		"accounts/login":   s.apiAddOrLogin("login"),
		"accounts/enable":  s.apiAccountCmd("enable"),
		"accounts/disable": s.apiAccountCmd("disable"),
		"accounts/remove":  s.apiRemove,
		"accounts/move":    s.apiMove,
		"accounts/check":   s.apiCheck,
		"signin/private":   s.apiSignInPrivate,
		"sessions/start":   s.apiStart,
		"sessions/switch":  s.apiSwitch,
		"sessions/stop":    s.apiStop,
		"sessions/focus":   s.apiFocus,
		"sessions/restore": s.apiRestore,
		"sessions/dismiss": s.apiDismiss,
		"notify/test":      s.apiNotifyTest,
		"folder/pick":      s.apiPickFolder,
		"config":           s.apiConfig,
		"rules/set":        s.apiRuleSet,
		"rules/remove":     s.apiRuleRemove,
		"claude/refresh":   s.apiClaudeRefresh,
		"install":          s.apiInstall,
		"uninstall":        s.apiUninstall,
		"update/check":     s.apiUpdateCheck,
		"update/install":   s.apiUpdateInstall,
		"doctor":           s.apiDoctor,
		"open":             s.apiOpen,
	}
	switch r.Method {
	case http.MethodGet:
		if h := get[path]; h != nil {
			h(w, r)
			return
		}
	case http.MethodPost:
		if h := post[path]; h != nil {
			h(w, r)
			return
		}
	}
	fail(w, http.StatusNotFound, "not_found", "not found", "")
}

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

// ---- accounts ------------------------------------------------------------------

type nameReq struct {
	Name string `json:"name"`
}

func (s *Server) apiAddOrLogin(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Email   string `json:"email"`
			Private bool   `json:"private"` // sign in in an InPrivate window
		}
		if !readJSON(w, r, &req) {
			return
		}
		if err := accounts.ValidateID(req.Name); err != nil {
			fail(w, http.StatusBadRequest, "bad_name", err.Error(), "")
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if req.Email != "" && (len(req.Email) > 254 || !emailRx.MatchString(req.Email)) {
			fail(w, http.StatusBadRequest, "bad_email", "that does not look like an email address", "")
			return
		}
		reg, _ := accounts.Load(s.opt.Paths.Accounts())
		existing := reg.Get(req.Name)
		exists := existing != nil
		kind := kind
		if kind == "add" && exists {
			if existing.LoggedIn {
				fail(w, http.StatusConflict, "exists", "an account with this name already exists", "")
				return
			}
			kind = "login" // its first sign-in was cancelled: carry on with it
		}
		if kind == "login" && !exists {
			fail(w, http.StatusNotFound, "no_account", "no account with this name", "")
			return
		}
		// One sign-in at a time: two browser sign-ins at once are easy to mix up.
		if s.anySigningIn() {
			fail(w, http.StatusConflict, "signin_open", "finish the sign-in that is already open first", "")
			return
		}
		jobs := filepath.Join(s.opt.Paths.Root, "gui-jobs")
		_ = os.MkdirAll(jobs, 0o700)
		resultFile := filepath.Join(jobs, fmt.Sprintf("%s-%d.json", req.Name, time.Now().UnixNano()))
		args := []string{kind, req.Name, "--result-file", resultFile}
		if req.Private {
			args = append(args, "--private")
		}
		if req.Email != "" {
			args = append(args, "--email", req.Email)
		}
		if err := s.consoleJob(kind, req.Name, s.opt.Paths.Root, args, resultFile); err != nil {
			fail(w, http.StatusInternalServerError, "spawn", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	}
}

// apiSignInPrivate opens the sign-in address the user copied from the sign-in
// window in an InPrivate window. The clipboard is read only on this request.
func (s *Server) apiSignInPrivate(w http.ResponseWriter, r *http.Request) {
	if s.opt.Clipboard == nil || s.opt.OpenPrivate == nil {
		fail(w, http.StatusNotImplemented, "no_private", "not available", "")
		return
	}
	u, ok := winui.SignInURL(s.opt.Clipboard())
	if !ok {
		fail(w, http.StatusBadRequest, "no_url", "copy the sign-in address from the sign-in window first", "")
		return
	}
	if err := s.opt.OpenPrivate(u); err != nil {
		fail(w, http.StatusInternalServerError, "no_edge", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiAccountCmd(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req nameReq
		if !readJSON(w, r, &req) {
			return
		}
		if err := accounts.ValidateID(req.Name); err != nil {
			fail(w, http.StatusBadRequest, "bad_name", err.Error(), "")
			return
		}
		s.hidden(w, []string{cmd, req.Name})
	}
}

func (s *Server) apiRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := accounts.ValidateID(req.Name); err != nil {
		fail(w, http.StatusBadRequest, "bad_name", err.Error(), "")
		return
	}
	// The account's conversations are moved to the user's own Claude Code.
	s.hidden(w, []string{"remove", req.Name, "--yes"})
}

// apiMove moves an account one place up or down in the switching order.
func (s *Server) apiMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Dir  string `json:"dir"` // up | down
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Dir != "up" && req.Dir != "down" {
		fail(w, http.StatusBadRequest, "bad_request", "dir must be up or down", "")
		return
	}
	_, err := accounts.Update(s.opt.Paths.Accounts(), s.opt.Paths.AccountsLock(), func(reg *accounts.Registry) error {
		order := byPriority(reg.Accounts)
		i := -1
		for k, a := range order {
			if a.ID == req.Name {
				i = k
			}
		}
		if i < 0 {
			return errors.New("no account with this name")
		}
		k := i - 1
		if req.Dir == "down" {
			k = i + 1
		}
		if k < 0 || k >= len(order) {
			return nil
		}
		order[i], order[k] = order[k], order[i]
		// Spread the priorities out again so the new order is unambiguous.
		for n, a := range order {
			reg.Get(a.ID).Priority = (len(order) - n) * 10
		}
		return nil
	})
	if err != nil {
		fail(w, http.StatusBadRequest, "move", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiCheck refreshes every account's sign-in status in the background:
// `baton doctor` asks Claude Code (`claude auth status`) for each profile.
func (s *Server) apiCheck(w http.ResponseWriter, r *http.Request) {
	s.StartCheck()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// StartCheck refreshes sign-in status. Asked for while a check runs (a
// sign-in just finished), it runs once more afterwards.
func (s *Server) StartCheck() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opt.Spawner == nil {
		return
	}
	if s.checking {
		s.recheck = true
		return
	}
	s.checking = true
	go func() {
		for {
			_, _, _ = s.opt.Spawner.Hidden([]string{"doctor"})
			s.mu.Lock()
			again := s.recheck
			s.recheck, s.checking = false, again
			s.mu.Unlock()
			if !again {
				return
			}
		}
	}()
}

// ---- sessions --------------------------------------------------------------------

// checkFolder accepts a local folder baton and Claude Code can work in.
func checkFolder(p string) (string, string) {
	// Explorer's "Copy as path" puts the path in quotes.
	p = filepath.Clean(strings.Trim(strings.TrimSpace(p), `"`))
	switch {
	case !driveRx.MatchString(p):
		return "", "bad_folder" // relative, network (\\server) or device (\\?\) paths
	case len(p) >= 248:
		return "", "long_folder"
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return "", "bad_folder"
	}
	return p, ""
}

var driveRx = regexp.MustCompile(`^[A-Za-z]:\\`)

// apiFolderInfo says whether a folder can be used and whether Claude Code
// would ask to trust it (it is trusted on no account and not in the user's
// own Claude Code).
func (s *Server) apiFolderInfo(w http.ResponseWriter, r *http.Request) {
	folder, problem := checkFolder(r.URL.Query().Get("path"))
	if problem != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "problem": problem})
		return
	}
	var allowed []string
	if cfg, err := config.Load(s.opt.Paths.Config()); err == nil {
		if r := cfg.RuleFor(folder); r != nil {
			allowed = r.Accounts
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": folder, "trusted": s.trusted(folder), "allowed": allowed})
}

func (s *Server) trusted(folder string) bool {
	for _, d := range s.profileDirs() {
		if claude.IsTrusted(d, folder) {
			return true
		}
	}
	h := share.UserHome(s.opt.Paths.ProfilesDir())
	return h != "" && claude.IsTrusted(claude.UserClaudeJSONDir(h), folder)
}

func (s *Server) apiStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder    string `json:"folder"`
		Account   string `json:"account"`
		Mode      string `json:"mode"` // new | continue | resume
		SessionID string `json:"session_id"`
		Where     string `json:"where"` // resume: an account id, or "claude-code" for the user's own setup
		Trust     bool   `json:"trust"` // the user said in the window that they trust the folder
		Copy      bool   `json:"copy"`  // continue or resume: go on in a copy, leaving the conversation as it is
		// RemoteControl: Claude Code's Remote Control, on whichever account
		// the session runs (claude.ai/code or the Claude app, signed in with it).
		RemoteControl bool `json:"remote_control"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	folder, problem := checkFolder(req.Folder)
	if problem != "" {
		fail(w, http.StatusBadRequest, problem, "choose an existing local folder", "")
		return
	}
	// `baton run` would otherwise ask in English in the console, where Enter
	// counts as agreeing to the notice the user has not accepted here.
	if cfg, err := config.Load(s.opt.Paths.Config()); err == nil &&
		(cfg.SwitchMode == "" || cfg.SwitchMode != config.ModeOff && cfg.RiskAcknowledged < config.DisclosureVersion) {
		fail(w, http.StatusConflict, "needs_mode", "choose how baton switches accounts first", "")
		return
	}
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	cfg, _ := config.Load(s.opt.Paths.Config())
	rule := cfg.RuleFor(folder)
	args := []string{"run"}
	if req.Account != "" {
		if accounts.ValidateID(req.Account) != nil || reg.Get(req.Account) == nil {
			fail(w, http.StatusBadRequest, "no_account", "no account with this name", "")
			return
		}
		if !rule.Allows(req.Account) {
			fail(w, http.StatusBadRequest, "rule_account", "this folder's rule does not allow that account", "")
			return
		}
		args = append(args, "--account", req.Account)
	} else if id := s.idleStart(onlyAllowed(reg, rule), folder); id != "" {
		args = append(args, "--account", id)
	}
	var cargs []string // Claude Code's own options, after "--"
	switch req.Mode {
	case "", "new":
	case "continue":
		// The newest conversation in the folder, checked like a chosen one.
		latest := claude.ListTranscripts(s.allConfigDirs(), folder, 1)
		if len(latest) == 0 {
			fail(w, http.StatusBadRequest, "no_conversation", "no conversation in this folder yet", "")
			return
		}
		t, code := s.findTranscript(latest[0].SessionID, s.whereOf(latest[0].ConfigDir), req.Copy)
		if code != "" {
			fail(w, http.StatusBadRequest, code, "that conversation cannot be opened", "")
			return
		}
		cargs = append(cargs, "--resume", t)
	case "resume":
		t, code := s.findTranscript(req.SessionID, req.Where, req.Copy)
		if code != "" {
			fail(w, http.StatusBadRequest, code, "that conversation cannot be opened", "")
			return
		}
		cargs = append(cargs, "--resume", t)
	default:
		fail(w, http.StatusBadRequest, "bad_request", "mode must be new, continue or resume", "")
		return
	}
	if req.Copy && (req.Mode == "continue" || req.Mode == "resume") {
		// Claude Code reads the conversation and goes on under a new session
		// id in the account's profile; the original file is never written.
		cargs = append(cargs, "--fork-session")
	}
	if req.RemoteControl {
		// Kept on every relaunch, so it comes back on after a switch.
		cargs = append(cargs, "--remote-control")
	}
	if len(cargs) > 0 {
		args = append(append(args, "--"), cargs...)
	}
	// A new account's first start would stop at Claude Code's first-run
	// screens (in English, and the folder question exits on Enter): answer
	// the setup ones, and folder trust only if the user just agreed to it.
	for _, a := range reg.Accounts {
		if a.Enabled && !claude.BusyProfile(a.ProfileDir) {
			_, _ = claude.SeedProfile(a.ProfileDir, folder, true, req.Trust)
		}
	}
	if err := s.console("run", folder, folder, args); err != nil {
		fail(w, http.StatusInternalServerError, "spawn", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// idleStart picks the account an "Automatic" session starts on when the one
// baton would pick runs another Claude Code and does not trust the folder:
// trust cannot be written into a profile in use, and Claude Code's own trust
// question exits on Enter. "" leaves the choice to baton.
func (s *Server) idleStart(reg accounts.Registry, folder string) string {
	st, err := ledger.Load(s.opt.Paths.State())
	if err != nil {
		return ""
	}
	now := time.Now()
	best, _, _ := sched.Pick(reg, st, now, "") // what `baton run` would pick
	if best == nil || !claude.BusyProfile(best.ProfileDir) || claude.IsTrusted(best.ProfileDir, folder) {
		return ""
	}
	for _, a := range byPriority(reg.Accounts) {
		if ok, _ := sched.Eligible(a, st.StatusOf(a.ID), now); ok && a.ID != best.ID && !claude.BusyProfile(a.ProfileDir) {
			return a.ID
		}
	}
	return ""
}

// onlyAllowed is the registry without the accounts rule does not allow.
func onlyAllowed(reg accounts.Registry, rule *config.FolderRule) accounts.Registry {
	if rule == nil {
		return reg
	}
	out := accounts.Registry{Schema: reg.Schema}
	for _, a := range reg.Accounts {
		if rule.Allows(a.ID) {
			out.Accounts = append(out.Accounts, a)
		}
	}
	return out
}

// apiRuleSet adds or replaces the rule for a folder.
func (s *Server) apiRuleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder   string   `json:"folder"`
		Accounts []string `json:"accounts"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	folder, problem := checkFolder(req.Folder)
	if problem != "" {
		fail(w, http.StatusBadRequest, problem, "choose an existing local folder", "")
		return
	}
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	for _, a := range req.Accounts {
		if reg.Get(a) == nil {
			fail(w, http.StatusBadRequest, "no_account", "no account with this name", "")
			return
		}
	}
	if len(req.Accounts) == 0 {
		fail(w, http.StatusBadRequest, "rule_empty", "choose at least one account", "")
		return
	}
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		return c.SetRule(folder, req.Accounts)
	}); err != nil {
		fail(w, http.StatusBadRequest, "bad_value", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiRuleRemove drops the rule for a folder.
func (s *Server) apiRuleRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	removed := false
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		removed = c.RemoveRule(req.Folder)
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, "config", err.Error(), "")
		return
	}
	if !removed {
		fail(w, http.StatusNotFound, "missing", "there is no rule for that folder", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// allConfigDirs are every folder conversations live in.
func (s *Server) allConfigDirs() []string {
	var all []string
	for _, d := range s.configDirs() {
		all = append(all, d)
	}
	return all
}

// whereOf names the config folder dir ("" if it is none of them).
func (s *Server) whereOf(dir string) string {
	for where, d := range s.configDirs() {
		if strings.EqualFold(filepath.Clean(d), filepath.Clean(dir)) {
			return where
		}
	}
	return ""
}

var sessionIDRx = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// findTranscript finds a conversation by session id in one config folder:
// a regular .jsonl file directly inside <folder>\projects\<project>, which no
// other Claude Code is writing right now (unless only a copy of it goes on).
func (s *Server) findTranscript(sessionID, where string, copy bool) (string, string) {
	if !sessionIDRx.MatchString(sessionID) {
		return "", "bad_transcript"
	}
	dir, ok := s.configDirs()[where]
	if !ok {
		return "", "bad_transcript"
	}
	t := claude.FindTranscript([]string{dir}, sessionID)
	if t == "" {
		return "", "bad_transcript"
	}
	fi, err := os.Lstat(t)
	if err != nil || !fi.Mode().IsRegular() {
		return "", "bad_transcript"
	}
	var all []string
	for _, d := range s.configDirs() {
		all = append(all, d)
	}
	if !copy && len(claude.Holders(all, sessionID)) > 0 {
		return "", "open_elsewhere"
	}
	return t, ""
}

// configDirs are the config folders conversations live in, by "where": each
// account's profile, and "claude-code" for the user's own setup.
func (s *Server) configDirs() map[string]string {
	reg, _ := accounts.Load(s.opt.Paths.Accounts())
	dirs := map[string]string{}
	for _, a := range reg.Accounts {
		dirs[a.ID] = a.ProfileDir
	}
	if h := share.UserHome(s.opt.Paths.ProfilesDir()); h != "" {
		dirs["claude-code"] = h
	}
	return dirs
}

func (s *Server) profileDirs() []string {
	var out []string
	for where, d := range s.configDirs() {
		if where != "claude-code" {
			out = append(out, d)
		}
	}
	return out
}

// conversationView is one conversation in a "continue" list. It names the
// conversation by id and place, never by file path.
type conversationView struct {
	SessionID   string `json:"session_id"`
	Where       string `json:"where"`
	Title       string `json:"title"`
	FirstPrompt string `json:"first_prompt"`
	Cwd         string `json:"cwd"`
	FolderOK    bool   `json:"folder_ok"` // the conversation can go on in its folder
	// Why it cannot: "gone" (the folder no longer exists), or checkFolder's
	// problem for a folder baton does not start in.
	FolderProblem string    `json:"folder_problem,omitempty"`
	Source        string    `json:"source"`           // what made it: claude-desktop, cli, claude-vscode...
	Open          bool      `json:"open"`             // another Claude Code (such as the desktop app) has it open now
	Holder        string    `json:"holder,omitempty"` // what has it open: claude-desktop, cli, claude-vscode...
	Modified      time.Time `json:"modified"`
}

// apiConversations lists the conversations of one folder (?folder=), or the
// newest of every folder (?all=1) for bringing in ones from Claude Code.
func (s *Server) apiConversations(w http.ResponseWriter, r *http.Request) {
	list := []conversationView{}
	dirs := s.configDirs()
	whereOf := map[string]string{}
	var all []string
	for where, d := range dirs {
		whereOf[strings.ToLower(filepath.Clean(d))] = where
		all = append(all, d)
	}
	var found []claude.Transcript
	if r.URL.Query().Get("all") == "1" {
		found = claude.ListAllTranscripts(all, 200)
	} else {
		folder, problem := checkFolder(r.URL.Query().Get("folder"))
		if problem != "" {
			writeJSON(w, http.StatusOK, list)
			return
		}
		found = claude.ListTranscripts(all, folder, 20)
	}
	open := claude.OpenSessions(all)
	for _, t := range found {
		_, problem := checkFolder(t.Cwd)
		if problem == "bad_folder" && driveRx.MatchString(filepath.Clean(t.Cwd)) {
			problem = "gone" // a local folder that is not there (any more)
		}
		list = append(list, conversationView{SessionID: t.SessionID, Where: whereOf[strings.ToLower(filepath.Clean(t.ConfigDir))],
			Title: t.Title, FirstPrompt: t.FirstPrompt, Cwd: t.Cwd, FolderOK: problem == "", FolderProblem: problem,
			Source: t.Source, Open: open[t.SessionID] != "", Holder: open[t.SessionID], Modified: t.Modified})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) apiStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID string `json:"run_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !run.ValidID(req.RunID) {
		fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
		return
	}
	s.hidden(w, []string{"stop", "--run", req.RunID})
}

// apiFocus brings a running session's window to the front.
func (s *Server) apiFocus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID string `json:"run_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !run.ValidID(req.RunID) {
		fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
		return
	}
	j, err := run.LoadJournal(s.opt.Paths.Run(req.RunID))
	if err != nil || j.ConsoleWindow == 0 || !supervisorAlive(j.SupervisorPID, j.SupervisorCreated) {
		fail(w, http.StatusNotFound, "no_window", "the session's window is not open", "")
		return
	}
	if s.opt.Focus == nil {
		fail(w, http.StatusNotImplemented, "no_focus", "not available", "")
		return
	}
	if err := s.opt.Focus(uintptr(j.ConsoleWindow)); err != nil {
		fail(w, http.StatusConflict, "focus", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// cutOff returns the cut-off runs the request names, of those on offer.
func (s *Server) cutOff(w http.ResponseWriter, r *http.Request) ([]run.Journal, bool) {
	var req struct {
		RunIDs []string `json:"run_ids"`
	}
	if !readJSON(w, r, &req) {
		return nil, false
	}
	offered := map[string]run.Journal{}
	for _, j := range s.unfinished(s.cache.journals(s.opt.Paths.RunsDir())) {
		offered[j.RunID] = j
	}
	var out []run.Journal
	for _, id := range req.RunIDs {
		j, ok := offered[id]
		if !ok {
			fail(w, http.StatusNotFound, "not_cut_off", "that session cannot be reopened", "")
			return nil, false
		}
		out = append(out, j)
	}
	return out, true
}

// apiRestore reopens cut-off sessions, each in its own console window.
func (s *Server) apiRestore(w http.ResponseWriter, r *http.Request) {
	if cfg, err := config.Load(s.opt.Paths.Config()); err == nil &&
		(cfg.SwitchMode == "" || cfg.SwitchMode != config.ModeOff && cfg.RiskAcknowledged < config.DisclosureVersion) {
		fail(w, http.StatusConflict, "needs_mode", "choose how baton switches accounts first", "")
		return
	}
	cut, ok := s.cutOff(w, r)
	if !ok {
		return
	}
	for _, j := range cut {
		if err := s.console("run", j.Cwd, j.Cwd, []string{"run", "--restore", j.RunID}); err != nil {
			fail(w, http.StatusInternalServerError, "spawn", err.Error(), "")
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

// apiDismiss stops offering cut-off sessions (they stay in Recent sessions).
func (s *Server) apiDismiss(w http.ResponseWriter, r *http.Request) {
	cut, ok := s.cutOff(w, r)
	if !ok {
		return
	}
	for _, j := range cut {
		dir := s.opt.Paths.Run(j.RunID)
		if cur, err := run.LoadJournal(dir); err == nil {
			cur.Dismissed = true
			if err := run.SaveJournal(dir, cur); err != nil {
				fail(w, http.StatusInternalServerError, "dismiss", err.Error(), "")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// apiNotifyTest shows a sample notification, so the user sees whether
// Windows lets baton's through.
func (s *Server) apiNotifyTest(w http.ResponseWriter, r *http.Request) {
	if s.opt.TestNotify == nil {
		fail(w, http.StatusNotImplemented, "no_notify", "not available", "")
		return
	}
	lang := s.opt.Lang
	if c, err := config.Load(s.opt.Paths.Config()); err == nil && c.GUILanguage != "" {
		lang = c.GUILanguage
	}
	if err := s.opt.TestNotify(lang); err != nil {
		fail(w, http.StatusInternalServerError, "notify", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID  string `json:"run_id"`
		Target string `json:"target"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !run.ValidID(req.RunID) {
		fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
		return
	}
	args := []string{"switch"}
	if req.Target != "" {
		if err := accounts.ValidateID(req.Target); err != nil {
			fail(w, http.StatusBadRequest, "bad_name", err.Error(), "")
			return
		}
		args = append(args, req.Target)
	}
	s.hidden(w, append(args, "--run", req.RunID))
}

// ---- misc --------------------------------------------------------------------------

func (s *Server) apiPickFolder(w http.ResponseWriter, r *http.Request) {
	if s.opt.PickFolder == nil {
		fail(w, http.StatusNotImplemented, "no_picker", "folder picker not available", "")
		return
	}
	p, err := s.opt.PickFolder()
	if err != nil {
		fail(w, http.StatusInternalServerError, "picker", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "cancelled": p == ""})
}

func (s *Server) apiConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key         string `json:"key"`
		Value       string `json:"value"`
		Acknowledge bool   `json:"acknowledge"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cur, err := config.Load(s.opt.Paths.Config())
	if err != nil {
		fail(w, http.StatusInternalServerError, "config", err.Error(), "")
		return
	}
	set, err := config.Setter(cur, req.Key, req.Value, req.Acknowledge)
	if errors.Is(err, config.ErrNeedsAcknowledgement) {
		fail(w, http.StatusConflict, "needs_ack", err.Error(), "")
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_value", err.Error(), "")
		return
	}
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		set(c)
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, "config", err.Error(), "")
		return
	}
	s.mu.Lock()
	if req.Key == "claude_path" || req.Key == "allow_desktop_bundle" {
		s.claudeAt = time.Time{} // look for Claude Code again
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reopen bool `json:"reopen"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.opt.Install == nil {
		fail(w, http.StatusNotImplemented, "no_install", "not available", "")
		return
	}
	app, err := s.opt.Install()
	if err != nil {
		fail(w, http.StatusInternalServerError, "install", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": app})
	if req.Reopen && s.opt.Relaunch != nil {
		go func() {
			time.Sleep(300 * time.Millisecond) // let the answer reach the page
			_ = s.opt.Relaunch(app)
		}()
	}
}

func (s *Server) apiUninstall(w http.ResponseWriter, r *http.Request) {
	if s.opt.Uninstall == nil {
		fail(w, http.StatusNotImplemented, "no_uninstall", "not available", "")
		return
	}
	// Refused here as `baton uninstall` would, before this window closes. A
	// session window that is still open (an ended session waiting for Enter)
	// holds its log in baton's data folder, which would stop the deletion.
	if code, msg := s.replaceRefusal(); code != "" {
		fail(w, http.StatusConflict, code, msg, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = s.opt.Uninstall()
	}()
}

// apiClaudeRefresh looks for Claude Code again (after the user set up the
// desktop app's Code tab, or updated it).
func (s *Server) apiClaudeRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.claudeAt = time.Time{}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) apiDoctor(w http.ResponseWriter, r *http.Request) {
	if s.opt.Spawner == nil {
		fail(w, http.StatusInternalServerError, "spawn", errNoSpawner.Error(), "")
		return
	}
	out, code, err := s.opt.Spawner.Hidden([]string{"doctor"})
	if err != nil {
		fail(w, http.StatusInternalServerError, "spawn", err.Error(), out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": out, "ok": code == 0})
}

// apiOpen shows one of baton's own folders, or a session's project folder.
func (s *Server) apiOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		What  string `json:"what"` // data | run | folder | release
		RunID string `json:"run_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var target string
	switch req.What {
	case "release":
		// The newest release's page on GitHub, in the default browser; only
		// ever this repository's.
		s.mu.Lock()
		rel := s.upd.rel
		s.mu.Unlock()
		if rel == nil || !strings.HasPrefix(rel.Page, "https://github.com/"+update.Repo+"/releases/") || s.opt.Open == nil {
			fail(w, http.StatusNotFound, "missing", "no release page", "")
			return
		}
		if err := s.opt.Open(rel.Page); err != nil {
			fail(w, http.StatusInternalServerError, "open", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	case "data":
		target = s.opt.Paths.Root
	case "run", "folder":
		if !run.ValidID(req.RunID) {
			fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
			return
		}
		dir := s.opt.Paths.Run(req.RunID)
		target = dir
		if req.What == "folder" {
			j, err := run.LoadJournal(dir)
			if err != nil || j.Cwd == "" {
				fail(w, http.StatusBadRequest, "bad_run", "unknown session", "")
				return
			}
			target = j.Cwd
		}
	default:
		fail(w, http.StatusBadRequest, "bad_request", "what must be data, run, folder or release", "")
		return
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		fail(w, http.StatusNotFound, "missing", "the folder does not exist", "")
		return
	}
	if s.opt.Open == nil {
		fail(w, http.StatusNotImplemented, "no_open", "not available", "")
		return
	}
	if err := s.opt.Open(target); err != nil {
		fail(w, http.StatusInternalServerError, "open", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- running baton.exe ---------------------------------------------------------------

func (s *Server) hidden(w http.ResponseWriter, args []string) {
	if s.opt.Spawner == nil {
		fail(w, http.StatusInternalServerError, "spawn", errNoSpawner.Error(), "")
		return
	}
	out, code, err := s.opt.Spawner.Hidden(args)
	switch {
	case err != nil:
		fail(w, http.StatusInternalServerError, "spawn", err.Error(), out)
	case code != 0:
		fail(w, http.StatusUnprocessableEntity, "failed", lastLine(out), out)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
	}
}

func (s *Server) console(kind, target, dir string, args []string) error {
	return s.consoleJob(kind, target, dir, args, "")
}

func (s *Server) consoleJob(kind, target, dir string, args []string, resultFile string) error {
	if s.opt.Spawner == nil {
		return errNoSpawner
	}
	wait, err := s.opt.Spawner.Console(dir, args)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.nextJob++
	j := &job{id: s.nextJob, kind: kind, target: target, started: time.Now(), resultFile: resultFile}
	s.jobs = append(s.jobs, j)
	// Keep every job still running (its window is open) and the newest
	// finished ones, up to 20 in all.
	for i := 0; len(s.jobs) > 20 && i < len(s.jobs); {
		if s.jobs[i].running() {
			i++
			continue
		}
		s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
	}
	s.mu.Unlock()
	go func() {
		exited := make(chan int, 1)
		go func() { exited <- wait() }()
		var res *SignInResult
		code := 0
		if resultFile == "" {
			code = <-exited
		} else {
			// The outcome counts as soon as it is written: the sign-in window
			// may stay open a few seconds longer.
			tick := time.NewTicker(500 * time.Millisecond)
		poll:
			for {
				select {
				case code = <-exited:
					res = readResult(resultFile)
					break poll
				case <-tick.C:
					if res = readResult(resultFile); res != nil {
						code = map[bool]int{true: 0, false: 1}[res.OK]
						break poll
					}
				}
			}
			tick.Stop()
			_ = os.Remove(resultFile)
		}
		s.mu.Lock()
		j.done, j.exitCode, j.result = true, code, res
		s.mu.Unlock()
		if kind == "add" || kind == "login" {
			s.StartCheck() // pick up the new sign-in at once
		}
	}()
	return nil
}

func readResult(path string) *SignInResult {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	res := &SignInResult{}
	if json.Unmarshal(b, res) != nil {
		return nil // still being written
	}
	return res
}

func (s *Server) anyJobRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.running() {
			return true
		}
	}
	return false
}

func (s *Server) anySigningIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.running() && (j.kind == "add" || j.kind == "login") {
			return true
		}
	}
	return false
}

// lastLine is the most telling line of baton.exe's output ("baton: ...").
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return strings.TrimPrefix(l, "baton: ")
		}
	}
	return "failed"
}
