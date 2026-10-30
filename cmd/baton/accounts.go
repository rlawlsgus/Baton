package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/envx"
	"baton/internal/fsx"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/share"
	"baton/internal/usage"
	"baton/internal/winui"
)

func cmdAdd(args []string) (err error) {
	fs := newFlags("add", "baton add <name> [--email <address>] [--priority N] [--private] [--no-login]")
	priority := fs.Int("priority", 0, "higher is preferred (default: below existing accounts)")
	noLogin := fs.Bool("no-login", false, "create the profile without signing in")
	so := signInFlags(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	name, err := oneName(pos, "baton add <name> [--priority N]")
	if err != nil {
		return err
	}
	defer so.writeIfMissing(name, &err)
	if err := accounts.ValidateID(name); err != nil {
		return err
	}
	if err := refuseInsideSession("add accounts"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	dir := e.paths.Profile(name)
	if _, err := os.Stat(dir); err == nil && e.reg.Get(name) == nil {
		return fmt.Errorf("profile folder %s already exists but is not registered; remove it first", dir)
	}
	inst, err := e.resolveClaude()
	if err != nil {
		return err
	}
	e.reg, err = accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
		a := accounts.Account{ID: name, ProfileDir: dir, Enabled: true, Added: time.Now(), Priority: *priority}
		if a.Priority == 0 {
			a.Priority = r.NextPriority()
		}
		if r.Get(name) != nil {
			return fmt.Errorf("account %q already exists (sign it in again with: baton login %s)", name, name)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return r.Add(a)
	})
	if err != nil {
		return err
	}
	if so.resultFile == "" { // the baton window shows the outcome itself
		fmt.Fprintf(os.Stderr, "Created account %q (profile: %s)\n", name, dir)
	}
	if e.cfg.ShareEnabled() {
		if so.resultFile == "" {
			printShare(e, name, dir)
		} else if _, err := share.Sync(share.UserHome(e.paths.ProfilesDir()), dir); err != nil {
			fmt.Fprintf(os.Stderr, say("baton: warning: could not share your Claude Code setup with %q: %v\n",
				"baton: 주의: 평소 Claude Code 설정을 %q 계정에 공유하지 못했습니다: %v\n"), name, err)
		}
	}
	if *noLogin {
		warnLedger(setStatus(e, name, ledger.StatusNeedsLogin))
		fmt.Fprintf(os.Stderr, "Sign it in later with: baton login %s\n", name)
		return nil
	}
	return signIn(e, inst.Path, name, *so)
}

func cmdLogin(args []string) (err error) {
	fs := newFlags("login", "baton login <name> [--email <address>] [--private]")
	so := signInFlags(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	name, err := oneName(pos, "baton login <name>")
	if err != nil {
		return err
	}
	defer so.writeIfMissing(name, &err)
	if err := refuseInsideSession("sign accounts in"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if e.reg.Get(name) == nil {
		return fmt.Errorf("no account named %q", name)
	}
	inst, err := e.resolveClaude()
	if err != nil {
		return err
	}
	return signIn(e, inst.Path, name, *so)
}

type signInOpts struct {
	email      string // pre-filled on Claude's sign-in page
	resultFile string // where the GUI reads the outcome (JSON)
	private    bool   // open the sign-in page in an InPrivate window
}

func signInFlags(fs *flag.FlagSet) *signInOpts {
	o := &signInOpts{}
	fs.StringVar(&o.email, "email", "", "email address to pre-fill on Claude's sign-in page")
	fs.StringVar(&o.resultFile, "result-file", "", "write the outcome as JSON to this file (used by the baton window)")
	fs.BoolVar(&o.private, "private", false, "open the sign-in page in an InPrivate window (when your browser is signed in to another Claude account)")
	return o
}

// signInPrivateEnv tells baton.exe, run by Claude Code's sign-in as its
// browser, to open the sign-in page in an InPrivate window. Its value is the
// folder for the window's throw-away browser profile.
const signInPrivateEnv = "BATON_SIGNIN_PRIVATE"

// openSignInPrivate is baton.exe as the browser of a --private sign-in: it
// opens Claude's sign-in page, and nothing else, in an InPrivate window.
func openSignInPrivate(arg, tmpRoot string) int {
	u, ok := winui.SignInURL(arg)
	if !ok {
		fmt.Fprintln(os.Stderr, say("baton: that is not a Claude sign-in address, so it was not opened",
			"baton: Claude 로그인 주소가 아니어서 열지 않았습니다"))
		return 1
	}
	if err := winui.OpenPrivate(u, tmpRoot); err != nil {
		fmt.Fprintf(os.Stderr, say("baton: could not open an InPrivate window (%v); paste the address shown here into a private window\n",
			"baton: InPrivate 창을 열지 못했습니다 (%v). 여기 나온 주소를 InPrivate(시크릿) 창에 붙여 넣으세요\n"), err)
		return 1
	}
	return 0
}

// writeIfMissing gives the baton window an outcome when the command ended
// before signIn could write one (Claude Code missing, a folder conflict).
func (o *signInOpts) writeIfMissing(name string, err *error) {
	if o.resultFile == "" {
		return
	}
	if _, serr := os.Stat(o.resultFile); serr == nil {
		return
	}
	res := signInResult{OK: *err == nil, Code: "ok", Account: name}
	if *err != nil {
		res.Code, res.Message = "error", (*err).Error()
	}
	if b, jerr := json.Marshal(res); jerr == nil && fsx.WriteFileAtomic(o.resultFile, b, 0o600) == nil {
		windowHasResult = true
	}
}

// signInResult is what the baton window shows after a sign-in window closes.
type signInResult struct {
	OK            bool   `json:"ok"`
	Code          string `json:"code"` // ok | busy | login_failed | not_signed_in | duplicate | error
	Account       string `json:"account"`
	Email         string `json:"email,omitempty"`
	Plan          string `json:"plan,omitempty"`
	DuplicateOf   string `json:"duplicate_of,omitempty"`
	StillDisabled bool   `json:"still_disabled,omitempty"`
	NotSubscribed bool   `json:"not_subscribed,omitempty"` // signed in with an API key or a cloud provider
	Message       string `json:"message,omitempty"`
}

// signIn runs Claude Code's own sign-in for the profile; baton never sees or
// stores the credentials.
func signIn(e *env, exe, name string, o signInOpts) (err error) {
	res := &signInResult{Account: name, Code: "error"}
	window := o.resultFile != "" // started by the baton window, which shows the outcome
	if window {
		defer func() {
			p := recover()
			switch {
			case p != nil:
				res.OK, res.Code, res.Message = false, "error", fmt.Sprint(p)
			case err == nil:
				res.OK, res.Code = true, "ok"
			default:
				res.Message = err.Error()
			}
			if b, jerr := json.Marshal(res); jerr == nil && fsx.WriteFileAtomic(o.resultFile, b, 0o600) == nil {
				windowHasResult = true
			}
			if p != nil {
				panic(p)
			}
		}()
	}
	// windowErr replaces the CLI's `baton login` advice when the baton window
	// shows what to do next.
	windowErr := func(cli error) error {
		if !window {
			return cli
		}
		return errors.New(say("the sign-in did not finish; the baton window shows what to do next",
			"로그인이 끝나지 않았습니다. 다음 안내는 baton 창에 나옵니다"))
	}
	cur := e.reg.Get(name)
	if cur == nil {
		return fmt.Errorf("no account named %q", name)
	}
	a := *cur
	prev := a
	// Signing in again under a running claude would swap its login mid-session.
	if claude.BusyProfile(a.ProfileDir) {
		res.Code = "busy"
		return fmt.Errorf(say("%q is in use by a running Claude Code; finish that session first",
			"%q 계정을 쓰는 Claude Code가 실행 중입니다. 그 세션을 먼저 끝내세요"), name)
	}
	email := o.email
	if email == "" && !a.DisabledAsDuplicate && (a.Enabled || !sameAsAnother(e.reg, a)) {
		// Signing in again: offer the account it was. A duplicate's stored
		// address is the other account's, which would lead right back to it;
		// a duplicate can never be enabled, so an enabled one is the original.
		email = a.Email
	}
	var extra map[string]string
	if o.private {
		// Claude Code opens its sign-in page with the BROWSER command: baton
		// itself, which opens it in an InPrivate window.
		self, err := os.Executable()
		if err != nil {
			return err
		}
		extra = map[string]string{"BROWSER": winui.ShellWord(self), signInPrivateEnv: filepath.Join(e.paths.Root, "tmp")}
		fmt.Fprintf(os.Stderr, say("Signing in %q with Claude's own login (an InPrivate window will open)...\n",
			"Claude 공식 로그인으로 %q 계정에 로그인합니다 (InPrivate 창이 열립니다)...\n"), name)
	} else {
		fmt.Fprintf(os.Stderr, say("Signing in %q with Claude's own login (a browser window will open)...\n",
			"Claude 공식 로그인으로 %q 계정에 로그인합니다 (브라우저 창이 열립니다)...\n"), name)
	}
	pe, _ := envx.ForProfile(a.ProfileDir, extra)
	cwd, _ := os.Getwd()
	loginArgs := []string{"auth", "login"}
	if email != "" {
		loginArgs = append(loginArgs, "--email", email)
	}
	code, err := claude.RunInteractive(exe, loginArgs, pe, cwd)
	if err != nil {
		return err
	}
	if code != 0 {
		// A failed sign-in can leave the old login in place, so `auth status`
		// would still look fine: change nothing (a blocked account stays blocked).
		res.Code = "login_failed"
		return windowErr(fmt.Errorf("sign-in did not finish (claude auth login exited with code %d); %q keeps its previous state. "+
			"Try again with: baton login %s", code, name, name))
	}
	if err := e.refreshAuth(exe, &a); err != nil {
		return err
	}
	res.Email, res.Plan = a.Email, a.Subscription
	res.NotSubscribed = a.AuthMethod != "" && a.AuthMethod != "claude.ai"
	var dup *accounts.Account
	reg, err := accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
		cur := r.Get(name)
		if cur == nil {
			return fmt.Errorf("account %q was removed meanwhile", name)
		}
		copyAuth(cur, a)
		if cur.Enabled && prev.LoggedIn && accounts.SameIdentity(prev, *cur) {
			return nil // an enabled account signed in again as itself
		}
		for i := range r.Accounts {
			o := r.Accounts[i]
			if o.ID != name && o.Enabled && o.LoggedIn && cur.LoggedIn && accounts.SameIdentity(o, *cur) {
				dup = &r.Accounts[i]
				// Switching to it would land on the same quota. Only an account
				// baton disables itself is re-enabled by a later correct login.
				cur.DisabledAsDuplicate = cur.Enabled || cur.DisabledAsDuplicate
				cur.Enabled = false
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.reg = reg
	if !a.LoggedIn {
		warnLedger(setStatus(e, name, ledger.StatusNeedsLogin))
		res.Code = "not_signed_in"
		return windowErr(fmt.Errorf("%q is not signed in; try again with: baton login %s", name, name))
	}
	if dup != nil {
		res.Code, res.DuplicateOf = "duplicate", dup.ID
		return windowErr(fmt.Errorf("%q signed in as %s, the same Claude account as %q, so it is disabled. "+
			"Sign out of claude.ai in your browser, or sign in in an InPrivate window: baton login %s --private",
			name, orDash(a.Email), dup.ID, name))
	}
	if x := e.reg.Get(name); x != nil && !x.Enabled {
		if x.DisabledAsDuplicate {
			// baton disabled it as a duplicate; it is a different account now.
			if _, err := accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
				if y := r.Get(name); y != nil {
					y.Enabled, y.DisabledAsDuplicate = true, false
				}
				return nil
			}); err != nil {
				return err
			}
		} else {
			res.StillDisabled = true
			if !window {
				fmt.Fprintf(os.Stderr, "%q is still disabled (you disabled it); run `baton enable %s` to use it\n", name, name)
			}
		}
	}
	warnLedger(ledger.Update(e.paths.State(), e.paths.StateLock(), func(st *ledger.State) error {
		if s := st.Account(name); s.Status == ledger.StatusNeedsLogin || s.Status == ledger.StatusBlocked {
			s.Status = s.Cleared(time.Now()) // a fresh, working login clears both
		}
		return nil
	}))
	fmt.Fprintf(os.Stderr, say("%q signed in: %s (%s)\n", "%q 로그인 완료: %s (%s)\n"), name, orDash(a.Email), orDash(a.Subscription))
	if a.AuthMethod != "" && a.AuthMethod != "claude.ai" {
		fmt.Fprintf(os.Stderr, say("baton: warning: %q uses %q, not a Claude subscription login\n",
			"baton: 주의: %q 계정은 Claude 구독이 아닌 %q 방식으로 로그인되었습니다\n"), name, a.AuthMethod)
	}
	return nil
}

// sameAsAnother reports that a is signed in as the same Claude account as
// another registered account (a duplicate, also one the user turned off).
func sameAsAnother(reg accounts.Registry, a accounts.Account) bool {
	for _, o := range reg.Accounts {
		if o.ID != a.ID && o.LoggedIn && a.LoggedIn && accounts.SameIdentity(o, a) {
			return true
		}
	}
	return false
}

func copyAuth(dst *accounts.Account, src accounts.Account) {
	dst.LoggedIn, dst.AuthMethod, dst.Email = src.LoggedIn, src.AuthMethod, src.Email
	dst.AccountUUID, dst.OrgID, dst.OrgName = src.AccountUUID, src.OrgID, src.OrgName
	dst.Subscription, dst.CheckedAt = src.Subscription, src.CheckedAt
}

func setStatus(e *env, name, status string) error {
	return ledger.Update(e.paths.State(), e.paths.StateLock(), func(st *ledger.State) error {
		st.Account(name).Status = status
		return nil
	})
}

// warnLedger reports a state.json write that failed; the command itself
// already did its main job.
func warnLedger(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "baton: warning: could not update the account ledger: %v\n", err)
	}
}

func cmdList(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: baton list")
	}
	e, err := load()
	if err != nil {
		return err
	}
	if len(e.reg.Accounts) == 0 {
		fmt.Println("No accounts yet. Add one with: baton add <name>")
		return nil
	}
	printAccounts(e.reg, e.loadState(), e.paths.UsageDir())
	return nil
}

func printAccounts(reg accounts.Registry, st ledger.State, usageDir string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRIORITY\tENABLED\tACCOUNT\tPLAN\tSTATUS\tUSAGE")
	for _, a := range sortedByPriority(reg.Accounts) {
		s := st.StatusOf(a.ID)
		status := s.Status
		if s.Status == ledger.StatusExhausted {
			if time.Now().Before(s.RetryAt) {
				status = "limited until " + s.RetryAt.Local().Format("Jan 2 15:04")
			} else {
				status = ledger.StatusOK
			}
		}
		if !a.LoggedIn && status == ledger.StatusOK {
			status = "not signed in"
		}
		fmt.Fprintf(w, "%s\t%d\t%v\t%s\t%s\t%s\t%s\n", a.ID, a.Priority, a.Enabled, orDash(a.Email), orDash(a.Subscription), status,
			planUsageText(usageDir, a.ID))
	}
	w.Flush()
}

// planUsageText is an account's last known usage, e.g. "5h 34%, week 12% (5 min ago)".
func planUsageText(dir, account string) string {
	u, ok := usage.Load(dir, account)
	if !ok {
		return "-"
	}
	u = u.Current(time.Now())
	var parts []string
	if w := u.FiveHour; w != nil {
		parts = append(parts, fmt.Sprintf("5h %d%%", int(math.Round(w.Used))))
	}
	if w := u.SevenDay; w != nil {
		parts = append(parts, fmt.Sprintf("week %d%%", int(math.Round(w.Used))))
	}
	ago := time.Since(u.Updated).Round(time.Minute)
	when := "just now"
	switch {
	case ago >= 48*time.Hour:
		when = fmt.Sprintf("%d days ago", int(ago.Hours()/24))
	case ago >= time.Hour:
		when = fmt.Sprintf("%d h ago", int(ago.Hours()))
	case ago >= time.Minute:
		when = fmt.Sprintf("%d min ago", int(ago.Minutes()))
	}
	return strings.Join(parts, ", ") + " (" + when + ")"
}

func cmdSetEnabled(args []string, enabled bool) error {
	verb := map[bool]string{true: "enable", false: "disable"}[enabled]
	name, err := oneName(args, "baton "+verb+" <name>")
	if err != nil {
		return err
	}
	if enabled {
		if err := refuseInsideSession("enable accounts"); err != nil {
			return err
		}
	}
	e, err := load()
	if err != nil {
		return err
	}
	_, err = accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
		a := r.Get(name)
		if a == nil {
			return fmt.Errorf("no account named %q", name)
		}
		if enabled {
			for _, o := range r.Accounts {
				if o.ID != name && o.Enabled && o.LoggedIn && a.LoggedIn && accounts.SameIdentity(o, *a) {
					return fmt.Errorf(say("%q is signed in as the same Claude account as %q (%s); sign it in as a different account first: baton login %s",
						"%q 계정은 %q 계정과 같은 Claude 계정(%s)으로 로그인되어 있습니다. 먼저 다른 계정으로 다시 로그인하세요 (%s)"),
						name, o.ID, orDash(a.Email), name)
				}
			}
		}
		a.Enabled, a.DisabledAsDuplicate = enabled, false
		return nil
	})
	if err != nil || !enabled {
		return err
	}
	// Enabling is also how the user says a "blocked" problem is resolved.
	cleared := false
	if err := ledger.Update(e.paths.State(), e.paths.StateLock(), func(st *ledger.State) error {
		if s := st.Account(name); s.Status == ledger.StatusBlocked {
			s.Status, cleared = s.Cleared(time.Now()), true
		}
		return nil
	}); err != nil {
		return fmt.Errorf("%q is enabled, but its blocked mark (if any) could not be cleared: %v", name, err)
	}
	if cleared {
		fmt.Fprintf(os.Stderr, "Cleared the blocked mark on %q\n", name)
	}
	return nil
}

func cmdRemove(args []string) error {
	fs := newFlags("remove", "baton remove <name> [--yes]")
	_ = fs.Bool("force", false, "no longer needed: conversations are always kept")
	assumeYes := fs.Bool("yes", false, "do not ask for confirmation")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	name, err := oneName(pos, "baton remove <name>")
	if err != nil {
		return err
	}
	if err := refuseInsideSession("remove accounts"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	a := e.reg.Get(name)
	if a == nil {
		return fmt.Errorf("no account named %q", name)
	}
	dir := a.ProfileDir
	// Only ever delete a folder baton created: a hand-edited profile_dir could
	// point at the user's real ~/.claude.
	if !fsx.IsWithin(dir, e.paths.ProfilesDir()) {
		return fmt.Errorf("%q's profile %s is outside %s; baton will not delete it. Remove it from accounts.json by hand",
			name, dir, e.paths.ProfilesDir())
	}
	if fi, err := os.Lstat(dir); err == nil && fsx.IsReparsePoint(fi) {
		return fmt.Errorf("%s is a link to another folder; delete the real folder by hand", dir)
	}
	if err := profileInUse(e, name, dir); err != nil {
		return err
	}
	home := share.UserHome(e.paths.ProfilesDir())
	if claude.ProfileHasTranscripts(dir) {
		fmt.Fprintf(os.Stderr, say("Conversations that started on %q are moved to your own Claude Code (%s) first.\n",
			"%q 계정에서 시작한 대화는 먼저 평소 Claude Code 폴더(%s)로 옮겨 보존합니다.\n"), name, orDash(home))
	}
	if !*assumeYes {
		ok, err := yes(fmt.Sprintf("Log out %q and delete %s? [y/N] ", name, dir))
		if err != nil {
			return fmt.Errorf("%w (or confirm with --yes)", err)
		}
		if !ok {
			return errors.New("cancelled")
		}
	}
	// Disable first, so a deletion that fails halfway never leaves a usable
	// half-profile in rotation; then re-check nothing started meanwhile.
	wasEnabled := a.Enabled
	setEnabled := func(v bool) error {
		_, err := accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
			if x := r.Get(name); x != nil {
				x.Enabled = v
			}
			return nil
		})
		return err
	}
	if err := setEnabled(false); err != nil {
		return err
	}
	if err := profileInUse(e, name, dir); err != nil {
		_ = setEnabled(wasEnabled) // nothing was removed: leave the account as it was
		return err
	}
	// The conversations go to the user's own Claude Code before the profile
	// is deleted; one that cannot be moved stops the removal.
	if n, err := share.KeepConversations(dir, home, name); err != nil {
		_ = setEnabled(wasEnabled)
		return fmt.Errorf(say("could not move the conversations of %q to your own Claude Code (%v); nothing was deleted",
			"%q 계정의 대화를 평소 Claude Code 폴더로 옮기지 못했습니다 (%v). 아무것도 지우지 않았습니다"), name, err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, say("baton: moved %d conversation(s) to %s\n", "baton: 대화 %d개를 %s 폴더로 옮겼습니다\n"), n, filepath.Join(home, "projects"))
	}
	if inst, err := e.resolveClaude(); err == nil {
		pe, _ := envx.ForProfile(dir, nil)
		if _, err := claude.RunInteractive(inst.Path, []string{"auth", "logout"}, pe, dir); err != nil {
			fmt.Fprintln(os.Stderr, "baton: warning: logout failed:", err)
		}
	}
	if err := fsx.RemoveTreeSafe(dir); err != nil {
		return fmt.Errorf("could not delete %s completely (%v); %q stays disabled, delete the folder by hand and run baton remove again", dir, err, name)
	}
	if _, err := accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
		r.Remove(name)
		return nil
	}); err != nil {
		return err
	}
	_ = ledger.Update(e.paths.State(), e.paths.StateLock(), func(st *ledger.State) error {
		delete(st.Accounts, name)
		return nil
	})
	usage.Remove(e.paths.UsageDir(), name)
	fmt.Fprintf(os.Stderr, "Removed %q\n", name)
	return nil
}

// profileInUse refuses while claude runs on the profile, or while any running
// session (on any account) still writes a transcript stored in it.
func profileInUse(e *env, name, dir string) error {
	if claude.BusyProfile(dir) {
		return fmt.Errorf(say("%q is in use by a running claude; quit it first",
			"%q 계정을 쓰는 Claude Code가 실행 중입니다. 먼저 끝내세요"), name)
	}
	for _, j := range run.Active(e.paths.RunsDir(), supervisorAlive) {
		if j.TranscriptPath != "" && fsx.IsWithin(j.TranscriptPath, dir) {
			return fmt.Errorf(say("baton run %s (now on %q) is continuing a conversation stored in %q's profile; finish it first",
				"실행 중인 세션 %s(현재 %q 계정)가 %q 계정에 저장된 대화를 이어 가는 중입니다. 먼저 그 세션을 끝내세요"),
				j.RunID, j.Account, name)
		}
	}
	for _, acc := range e.reg.Accounts {
		for _, ls := range claude.ReadSessions(acc.ProfileDir) {
			if ls.Alive() && claude.FindTranscript([]string{dir}, ls.SessionID) != "" {
				return fmt.Errorf(say("claude (pid %d, account %q) is writing a conversation stored in %q's profile; quit it first",
					"Claude Code(pid %d, %q 계정)가 %q 계정에 저장된 대화를 쓰고 있습니다. 먼저 끝내세요"),
					ls.PID, acc.ID, name)
			}
		}
	}
	return nil
}

// cmdSync shares the user's normal Claude Code setup into every idle profile.
func cmdSync(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: baton sync")
	}
	if err := refuseInsideSession("share settings between profiles"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Sharing %s with every account profile\n", orDash(share.UserHome(e.paths.ProfilesDir())))
	for _, a := range e.reg.Accounts {
		if claude.BusyProfile(a.ProfileDir) {
			fmt.Fprintf(os.Stderr, "  %-12s skipped (in use)\n", a.ID)
			continue
		}
		printShare(e, a.ID, a.ProfileDir)
	}
	return nil
}

func printShare(e *env, id, dir string) {
	res, err := share.Sync(share.UserHome(e.paths.ProfilesDir()), dir)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "  %-12s could not share your Claude setup: %v\n", id, err)
	case len(res.Linked)+len(res.Copied)+len(res.Kept) == 0:
		fmt.Fprintf(os.Stderr, "  %-12s up to date\n", id)
	default:
		fmt.Fprintf(os.Stderr, "  %-12s linked %v, copied %v, kept own %v\n", id, res.Linked, res.Copied, res.Kept)
	}
}

func sortedByPriority(in []accounts.Account) []accounts.Account {
	out := append([]accounts.Account{}, in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].Priority > out[j-1].Priority ||
			(out[j].Priority == out[j-1].Priority && out[j].ID < out[j-1].ID)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
