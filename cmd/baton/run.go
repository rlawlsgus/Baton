package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/envx"
	"baton/internal/share"
	"baton/internal/supervisor"
	"baton/internal/winproc"
)

const riskNotice = `
Read this before baton switches accounts for you:

  * When the account in use hits its usage limit, baton stops Claude Code and
    continues the same conversation on another account you have added.
  * Only add accounts that you personally own. Sharing an account with other
    people is against Anthropic's terms.
  * Anthropic's Usage Policy prohibits intentionally bypassing restrictions in
    its products without Anthropic's permission, and using multiple accounts to
    get around its safeguards. Continuing past a usage limit on another account
    may be treated as exactly that, and Anthropic can suspend accounts without
    notice. No Anthropic source says this use is allowed. Use it at your own risk.
  * Each switch makes the next account read the whole conversation again
    (prompt caches are per account), which uses part of its quota.
  * baton never reads or stores your credentials; every account is signed in
    and refreshed by Claude Code itself.
`

const modeChoice = `
How should baton switch accounts?
  [a] auto     switch without asking
  [c] confirm  ask in this window before each switch
  [o] off      never switch automatically (you can still run: baton switch)
`

func cmdRun(args []string) (int, error) {
	fs := newFlags("run", "baton run [--account <name>] [-- <claude arguments>]")
	account := fs.String("account", "", "account to start on (default: best available)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	// Claude Code arguments must follow "--", so `baton run work` (meant as
	// an account name) is not silently sent to the model as the first prompt.
	if n := fs.NArg(); n > 0 && (len(args) <= n || args[len(args)-n-1] != "--") {
		hint := ""
		if e, err := load(); err == nil && e.reg.Get(fs.Arg(0)) != nil {
			hint = fmt.Sprintf(" To start on account %q use: baton run --account %s.", fs.Arg(0), fs.Arg(0))
		}
		return 2, fmt.Errorf("put Claude Code arguments after --, e.g. baton run -- \"fix the bug\".%s", hint)
	}
	for _, t := range fs.Args() {
		if t == "--" {
			break // the rest is a prompt
		}
		if t == "--account" || strings.HasPrefix(t, "--account=") {
			return 2, errors.New("--account is a baton option; put it before --, e.g. baton run --account work -- \"fix the bug\"")
		}
	}
	cargs, err := claude.SplitArgs(fs.Args())
	if err != nil {
		return 2, err
	}
	if err := refuseInsideSession("start a supervised session"); err != nil {
		return 1, err
	}
	if !consoleUsable() {
		return reopenInNewConsole(args)
	}
	e, err := load()
	if err != nil {
		return 1, err
	}
	if err := ensureSwitchMode(e); err != nil {
		return 1, err
	}
	inst, err := e.resolveClaude()
	if err != nil {
		return 1, err
	}
	if _, removed := envx.Build(os.Environ(), nil); len(envx.Notable(removed)) > 0 {
		fmt.Fprintf(os.Stderr, "baton: not passing %s to claude (each account uses its own login)\n",
			strings.Join(envx.Notable(removed), ", "))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	resolveResume(&cargs, e, cwd)
	self, err := os.Executable()
	if err != nil {
		return 1, err
	}
	return supervisor.Run(supervisor.Options{
		Paths:      e.paths,
		Config:     e.cfg,
		ClaudeExe:  inst.Path,
		BatonExe:   self,
		Account:    *account,
		ClaudeArgs: cargs,
		Cwd:        cwd,
		Out:        os.Stderr,
		In:         stdin,
		Attention:  &runNeedsAttention,
	})
}

// runNeedsAttention: the session ended without the user asking, so its
// window waits for Enter instead of closing.
var runNeedsAttention bool

const newConsoleEnv = "BATON_NEW_CONSOLE"

// inNewConsole is set when this process is the `baton run` that
// reopenInNewConsole started in its own window.
var inNewConsole bool

// windowHasResult is set when a sign-in started by the baton window ended:
// the window shows the outcome, so its console need not wait for Enter.
var windowHasResult bool

// consoleUsable reports whether claude's TUI can run here: stdin must be a
// real console (mintty without ConPTY and IDE output panes use pipes).
func consoleUsable() bool {
	if !winproc.StdinIsConsole() {
		return false
	}
	cs, err := winproc.SaveConsole()
	if err != nil {
		return false
	}
	cs.Close()
	return true
}

// reopenInNewConsole starts `baton run ...` again in its own console window.
func reopenInNewConsole(args []string) (int, error) {
	if inNewConsole {
		return 1, errors.New("no usable console even in a new window")
	}
	self, err := os.Executable()
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(os.Stderr, "baton: this terminal is not a Windows console; opening Claude Code in a new console window...")
	cwd, _ := os.Getwd()
	env := append(os.Environ(), newConsoleEnv+"=1")
	return winproc.RunInNewConsole(self, append([]string{"run"}, args...), env, cwd)
}

// holdNewConsole keeps a window baton opened itself from vanishing with the
// last messages in it. A finished sign-in only pauses briefly.
func holdNewConsole(succeeded bool) {
	if !inNewConsole || !winproc.StdinIsConsole() {
		return
	}
	if !succeeded && windowHasResult {
		fmt.Fprint(os.Stderr, say("\nThe baton window shows what to do next. This window closes in 10 seconds.",
			"\n다음 안내는 baton 창에 나옵니다. 이 창은 10초 뒤에 닫힙니다."))
		time.Sleep(10 * time.Second)
		return
	}
	if succeeded {
		fmt.Fprint(os.Stderr, say("\nDone. This window closes in 5 seconds.", "\n완료되었습니다. 이 창은 5초 뒤에 닫힙니다."))
		time.Sleep(5 * time.Second)
		return
	}
	fmt.Fprint(os.Stderr, say("\nPress Enter to close this window.", "\nEnter를 누르면 이 창이 닫힙니다."))
	_, _ = stdin.ReadString('\n')
}

// resolveResume points -r <id>, --resume=<id> and -c at the transcript file,
// wherever it lives. claude itself only looks inside the current account's
// profile, but a conversation stays in the profile of the account that
// started it, or in the normal setup (~/.claude) if it began before baton.
func resolveResume(a *claude.Args, e *env, cwd string) {
	dirs := make([]string, 0, len(e.reg.Accounts)+1)
	for _, acc := range e.reg.Accounts {
		dirs = append(dirs, acc.ProfileDir)
	}
	home := share.UserHome(e.paths.ProfilesDir())
	if home != "" {
		dirs = append(dirs, home)
	}
	switch {
	case a.Continue:
		if p := claude.LatestTranscriptFor(dirs, cwd); p != "" {
			a.ReplaceResume(p)
		}
	case a.ResumeArg != "":
		v := a.ResumeArg
		if strings.EqualFold(filepath.Ext(v), ".jsonl") || strings.ContainsAny(v, `\/`) {
			return // already a path
		}
		if p := claude.FindTranscript(dirs, v); p != "" {
			a.ReplaceResume(p)
		} else {
			fmt.Fprintf(os.Stderr, "baton: no session %q in any account's profile or in %s; claude will look only in the account it starts on "+
				"(session names are not looked up across accounts; pass the .jsonl path instead)\n", v, orDash(home))
		}
	}
}

// ensureSwitchMode shows the risk notice (again, whenever it changed) and
// records the user's choice.
func ensureSwitchMode(e *env) error {
	if e.cfg.SwitchMode != "" && (e.cfg.SwitchMode == config.ModeOff || e.cfg.RiskAcknowledged >= config.DisclosureVersion) {
		return nil
	}
	fmt.Fprint(os.Stderr, riskNotice+modeChoice)
	for {
		ans, err := ask("Choose [a/c/o] (default c): ")
		if err != nil {
			return fmt.Errorf("%w (or set it with: baton config switch_mode auto|confirm|off)", err)
		}
		var mode string
		switch strings.ToLower(ans) {
		case "a", "auto":
			mode = config.ModeAuto
		case "", "c", "confirm":
			mode = config.ModeConfirm
		case "o", "off":
			mode = config.ModeOff
		default:
			continue
		}
		// Re-read under the lock: other settings may have changed while the
		// question was open.
		cfg, err := config.Update(e.paths.Config(), e.paths.ConfigLock(), func(c *config.Config) error {
			c.SwitchMode, c.RiskAcknowledged = mode, config.DisclosureVersion
			return nil
		})
		if err != nil {
			return err
		}
		e.cfg = cfg
		fmt.Fprintf(os.Stderr, "Switch mode: %s (change it with: baton config switch_mode <auto|confirm|off>)\n\n", mode)
		return nil
	}
}
