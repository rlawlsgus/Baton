package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/envx"
	"baton/internal/ledger"
	"baton/internal/winproc"
)

type env struct {
	paths app.Paths
	cfg   config.Config
	reg   accounts.Registry
}

func load() (*env, error) {
	p, err := app.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := p.Ensure(); err != nil {
		return nil, err
	}
	// Each `claude --version` takes about a second; the versions found are
	// kept for every baton process.
	claude.UseVersionCache(filepath.Join(p.Root, "claude-versions.json"))
	cfg, err := config.Load(p.Config())
	if err != nil {
		return nil, err
	}
	reg, err := accounts.Load(p.Accounts())
	if err != nil {
		return nil, err
	}
	return &env{paths: p, cfg: cfg, reg: reg}, nil
}

// resolveClaude finds the Claude Code to use (see claude.Choose).
func (e *env) resolveClaude() (claude.Install, error) {
	inst, _, err := e.resolveClaudeAll()
	return inst, err
}

// resolveClaudeAll is resolveClaude that also returns every copy of Claude
// Code found. When the one chosen in the settings cannot be used now, it says
// in one line which one is used instead.
func (e *env) resolveClaudeAll() (claude.Install, []claude.Install, error) {
	inst, all, fb, err := e.findClaude()
	if fb != nil {
		fmt.Fprintln(os.Stderr, fallbackLine(fb, inst))
	}
	if inst.Problem == claude.ProblemKnownBad {
		fmt.Fprintf(os.Stderr, say("baton: warning: Claude Code %s has a known problem with baton (%s); update it\n",
			"baton: 주의: Claude Code %s에는 baton과 맞지 않는 알려진 문제가 있습니다 (%s). 업데이트하세요\n"), inst.Version, inst.Detail)
	}
	return inst, all, err
}

func (e *env) findClaude() (claude.Install, []claude.Install, *claude.Fallback, error) {
	base, _ := envx.Build(os.Environ(), nil)
	return claude.ResolveUsable(e.cfg.ClaudePath, e.cfg.DesktopBundleAllowed(), base)
}

// fallbackLine says that the Claude Code chosen is not available and which
// one is used for now.
func fallbackLine(fb *claude.Fallback, inst claude.Install) string {
	wanted := fb.Wanted
	if wanted == claude.DesktopChoice {
		wanted = say("the Claude desktop app's", "Claude 데스크톱 앱의 것")
	}
	why := say("it is no longer there", "더 이상 없음")
	switch fb.Why {
	case claude.ProblemTooOld:
		why = say("it is too old", "버전이 너무 낮음")
	case claude.ProblemNotRunnable:
		why = say("it does not start", "실행되지 않음")
	case claude.ProblemInstalling:
		why = say("the Claude app is still installing it", "Claude 앱이 설치하는 중")
	case claude.ProblemWrapper:
		why = say("it is a .cmd/.ps1 wrapper", ".cmd/.ps1 래퍼임")
	case claude.ProblemKnownBad:
		why = say("it has a known problem with baton", "baton과 맞지 않는 문제가 있음")
	case claude.ProblemNotSetUp:
		why = say("the Claude desktop app has not set up its Claude Code yet (open its Code tab once)",
			"Claude 데스크톱 앱이 아직 Claude Code를 준비하지 않음 (Code 탭을 한 번 여세요)")
	}
	return fmt.Sprintf(say("baton: the Claude Code you chose (%s) is not available (%s); using Claude Code %s from %s for now",
		"baton: 선택한 Claude Code(%s)를 쓸 수 없어(%s) 지금은 %[4]s의 Claude Code %[3]s 버전을 씁니다"), wanted, why, inst.Version, installSource(inst.Source))
}

// installSource says where a copy of Claude Code comes from.
func installSource(src string) string {
	switch src {
	case "desktop":
		return say("the Claude desktop app", "Claude 데스크톱 앱")
	case "native":
		return say("the native installer", "기본 설치 프로그램")
	case "npm":
		return "npm"
	case "winget":
		return "WinGet"
	case "scoop":
		return "Scoop"
	case "path":
		return "PATH"
	}
	return say("the path you set", "직접 지정한 경로")
}

// claudeForLogout is a Claude Code that can sign profiles out, however old:
// removing an account must not leave its login behind because the Claude
// Code found is too old for sessions.
func (e *env) claudeForLogout() (string, bool) {
	inst, all, _, err := e.findClaude()
	if err == nil {
		return inst.Path, true
	}
	i, ok := claude.AnyRunnable(all)
	return i.Path, ok
}

// loadState reads the account ledger. If it cannot be read, that is said
// out loud: an empty ledger would show limited and blocked accounts as ok.
func (e *env) loadState() ledger.State {
	st, err := ledger.Load(e.paths.State())
	if err != nil {
		fmt.Fprintf(os.Stderr, "baton: warning: %s is unreadable (%v); usage limits and blocked marks are not shown. Fix or delete it.\n",
			e.paths.State(), err)
	}
	return st
}

// refreshAuth stores the non-secret login status of one account.
func (e *env) refreshAuth(exe string, a *accounts.Account) error {
	pe, _ := envx.ForProfile(a.ProfileDir, nil)
	st, err := claude.ReadAuthStatus(exe, pe)
	if err != nil {
		return err
	}
	applyAuth(a, st, a.ProfileDir)
	return nil
}

// applyAuth records what `claude auth status` said about the account whose
// profile is profileDir. The status has no account id, which the Claude
// desktop app names its per-account folders after: it comes from the
// profile's own .claude.json, when that is the sign-in the status describes
// (same organization and email). A signed-out profile keeps the ids it had:
// the desktop app signs in on its own, so its list for that account stays.
func applyAuth(a *accounts.Account, st claude.AuthStatus, profileDir string) {
	keepUUID, keepOrg := a.AccountUUID, a.OrgID
	a.LoggedIn = st.LoggedIn
	a.AuthMethod = st.AuthMethod
	if st.APIProvider != "" && st.APIProvider != "firstParty" {
		a.AuthMethod = st.APIProvider // a cloud provider overrides the login
	}
	a.Email = st.Email
	a.AccountUUID = st.AccountUUID
	a.OrgID = st.OrgID
	a.OrgName = st.OrgName
	a.Subscription = st.Subscription
	a.CheckedAt = time.Now()
	switch {
	case !st.LoggedIn:
		a.AccountUUID, a.OrgID = keepUUID, keepOrg
	case a.AuthMethod == "claude.ai" && a.AccountUUID == "":
		id, org, email := claude.OAuthIdentity(profileDir)
		if id != "" && org != "" && strings.EqualFold(org, st.OrgID) && email != "" && strings.EqualFold(email, st.Email) {
			a.AccountUUID = id
		}
	}
}

// say picks the Korean text when the baton window asked for Korean
// (BATON_LANG=ko) in a window it opened.
func say(en, ko string) string {
	if os.Getenv("BATON_LANG") == "ko" {
		return ko
	}
	return en
}

// refuseInsideSession blocks account-changing commands run from inside a
// supervised claude (its tools inherit BATON_RUN): the model, or a prompt
// injection, must not be able to move the session or change accounts.
func refuseInsideSession(what string) error {
	if os.Getenv("BATON_RUN") != "" {
		return fmt.Errorf("cannot %s from inside a baton session; run this in another terminal", what)
	}
	return nil
}

// newFlags is a FlagSet whose --help prints baton's own usage line and
// double-dash options.
func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s\n", usage)
		fs.VisitAll(func(f *flag.Flag) {
			arg := " <value>"
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				arg = ""
			}
			fmt.Fprintf(os.Stderr, "  --%s%s\n        %s\n", f.Name, arg, f.Usage)
		})
	}
	return fs
}

// parseInterspersed parses flags that may appear before or after positional
// arguments (Go's flag package stops at the first positional one).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func oneName(pos []string, usage string) (string, error) {
	if len(pos) != 1 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	return pos[0], nil
}

var stdin = bufio.NewReader(os.Stdin)

func ask(question string) (string, error) {
	if !winproc.StdinIsConsole() {
		return "", errors.New("cannot ask for confirmation here because stdin is not a console; " +
			"run this in Windows Terminal, PowerShell or cmd")
	}
	fmt.Fprint(os.Stderr, question)
	line, err := stdin.ReadString('\n')
	switch {
	case errors.Is(err, io.EOF) && strings.TrimSpace(line) == "":
		// Ctrl+Z or Ctrl+C at the prompt: no answer, not the default one.
		fmt.Fprintln(os.Stderr)
		return "", errors.New("no answer given")
	case err != nil && !errors.Is(err, io.EOF):
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// yes asks a y/N question; it fails (rather than answering "no") when there
// is no console to ask on.
func yes(question string) (bool, error) {
	ans, err := ask(question)
	if err != nil {
		return false, err
	}
	ans = strings.ToLower(ans)
	return ans == "y" || ans == "yes", nil
}
