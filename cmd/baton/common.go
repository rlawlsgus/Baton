package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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

// resolveClaude finds claude.exe and checks its version.
func (e *env) resolveClaude() (claude.Install, error) {
	inst, err := claude.Resolve(e.cfg.ClaudePath, e.cfg.DesktopBundleAllowed())
	if err != nil {
		return inst, err
	}
	base, _ := envx.Build(os.Environ(), nil)
	v, err := claude.Version(inst.Path, base)
	if err != nil {
		return inst, err
	}
	inst.Version = v
	warn, err := claude.CheckVersion(v)
	if err != nil {
		return inst, err
	}
	if warn != "" {
		fmt.Fprintln(os.Stderr, say("baton: warning: "+warn,
			fmt.Sprintf("baton: 참고: Claude Code %s는 이 baton으로 시험한 최신 버전(%s)보다 새 버전입니다", v, claude.TestedMax)))
	}
	return inst, nil
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
	return nil
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
