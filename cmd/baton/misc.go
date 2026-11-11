package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/sched"
	"baton/internal/winproc"
)

func supervisorAlive(pid int, created time.Time) bool {
	got := winproc.ProcessCreated(pid)
	if got.IsZero() {
		return false
	}
	d := got.Sub(created)
	return d > -time.Second && d < time.Second
}

func cmdSwitch(args []string) error {
	fs := newFlags("switch", "baton switch [<name>] [--run <id>]")
	runID := fs.String("run", "", "run id (see: baton status) when several sessions are running")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errors.New("usage: baton switch [<name>] [--run <id>]")
	}
	if err := refuseInsideSession("switch accounts"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	target := ""
	if len(pos) == 1 {
		target = pos[0]
		if a := e.reg.Get(target); a == nil || !a.Enabled || !a.LoggedIn {
			return fmt.Errorf("no enabled, signed-in account named %q", target)
		}
		st, err := ledger.Load(e.paths.State())
		if err != nil {
			return fmt.Errorf("%s is unreadable (%v); fix or delete it", e.paths.State(), err)
		}
		switch st.StatusOf(target).Status {
		case ledger.StatusBlocked:
			return fmt.Errorf("%q is marked blocked; once resolved, run: baton enable %s", target, target)
		case ledger.StatusNeedsLogin:
			return fmt.Errorf("%q needs to sign in again: baton login %s", target, target)
		}
	}
	active := run.Active(e.paths.RunsDir(), supervisorAlive)
	var chosen *run.Journal
	for i := range active {
		if *runID == "" || active[i].RunID == *runID {
			if chosen != nil && *runID == "" {
				return errors.New("several sessions are running; pick one with --run <id> (see: baton status)")
			}
			chosen = &active[i]
		}
	}
	if chosen == nil {
		return errors.New("no running baton session found")
	}
	rule := e.cfg.RuleFor(chosen.Cwd)
	if target != "" && !rule.Allows(target) {
		return fmt.Errorf(say("the rule for %s lets its sessions use only %s, so this one cannot move to %q (see: baton rules)",
			"%s 폴더의 규칙은 %s 계정만 허용하므로 이 세션을 %q 계정으로 옮길 수 없습니다"), rule.Folder, strings.Join(rule.Accounts, ", "), target)
	}
	// Another Claude Code opened its conversation, so it moves to a copy on
	// its account first; a named account can still be where it goes.
	if chosen.Moving && (chosen.State == run.StateSwitching || chosen.State == run.StateStopping && target == "") {
		return errors.New(say("that session is moving to a copy of its conversation right now (it was opened elsewhere too); try again in a few seconds",
			"그 세션은 지금 대화의 사본으로 옮기는 중입니다(다른 곳에서도 대화가 열렸습니다). 몇 초 뒤에 다시 시도하세요"))
	}
	switch chosen.State {
	case run.StateSwitching:
		return errors.New(say("that session is switching accounts right now; try again in a few seconds",
			"그 세션은 지금 계정을 전환하는 중입니다. 몇 초 뒤에 다시 시도하세요"))
	case run.StateEnding:
		return errors.New(say("that session is ending", "그 세션은 끝나는 중입니다"))
	case run.StateAsking:
		return errors.New(say("baton is asking a question in the session window; answer it there",
			"세션 창에서 확인을 기다리고 있습니다. 그 창에서 답하세요"))
	case run.StateStopping:
		if target == "" {
			return errors.New(say("that session is already switching; baton waits for Claude Code to finish its current turn first",
				"그 세션은 이미 전환을 기다리는 중입니다. Claude Code가 현재 응답을 끝내면 전환됩니다"))
		}
	}
	if target == "" {
		st, err := ledger.Load(e.paths.State())
		if err != nil {
			return fmt.Errorf("%s is unreadable (%v); fix or delete it", e.paths.State(), err)
		}
		if best, _, _ := sched.Pick(allowedOnly(e.reg, rule), st, time.Now(), chosen.Account); best == nil {
			return fmt.Errorf(say("no other account is available to switch %q to (see: baton list)",
				"%q 세션을 옮길 수 있는 다른 계정이 없습니다"), chosen.Account)
		}
	}
	// While waiting for a reset, naming the current account means "try it now".
	if target != "" && target == chosen.Account && chosen.State != run.StateWaiting {
		return fmt.Errorf("the session already runs on %q", target)
	}
	if err := run.RequestSwitch(e.paths.Run(chosen.RunID), run.SwitchRequest{Target: target, At: time.Now()}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Asked run %s (on %q) to switch%s.\n", chosen.RunID, chosen.Account,
		map[bool]string{true: " to " + strconv.Quote(target), false: ""}[target != ""])
	if chosen.State == run.StateStopping {
		fmt.Fprintln(os.Stderr, "It is waiting for Claude Code to finish its current turn first.")
	}
	return nil
}

// cmdStop ends a supervised session: Claude Code is stopped the same careful
// way as for a switch (the conversation is saved), and baton prints how to
// continue it later.
func cmdStop(args []string) error {
	fs := newFlags("stop", "baton stop [--run <id>]")
	runID := fs.String("run", "", "run id (see: baton status) when several sessions are running")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New("usage: baton stop [--run <id>]")
	}
	if err := refuseInsideSession("stop sessions"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	var chosen *run.Journal
	active := run.Active(e.paths.RunsDir(), supervisorAlive)
	for i := range active {
		if *runID == "" || active[i].RunID == *runID {
			if chosen != nil && *runID == "" {
				return errors.New("several sessions are running; pick one with --run <id> (see: baton status)")
			}
			chosen = &active[i]
		}
	}
	if chosen == nil {
		return errors.New("no running baton session found")
	}
	if err := run.RequestStop(e.paths.Run(chosen.RunID)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Asked run %s (on %q) to end its session.\n", chosen.RunID, chosen.Account)
	if chosen.State == run.StateAsking {
		fmt.Fprintln(os.Stderr, "baton is asking a question in the session window: it ends once that is answered (or press Ctrl+C there).")
	}
	return nil
}

func cmdStatus(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: baton status")
	}
	e, err := load()
	if err != nil {
		return err
	}
	st := e.loadState()
	if len(e.reg.Accounts) > 0 {
		printAccounts(e.reg, st, e.paths.UsageDir())
	} else {
		fmt.Println("No accounts yet. Add one with: baton add <name>")
	}
	active := run.Active(e.paths.RunsDir(), supervisorAlive)
	fmt.Println()
	if len(active) == 0 {
		fmt.Println("No running sessions.")
	}
	for _, j := range active {
		state := j.State
		if j.Moving && (state == run.StateStopping || state == run.StateSwitching) {
			state = "moving" // to a copy on its own account, not to another account
		}
		fmt.Printf("run %s  account=%s  state=%s  session=%s\n  cwd: %s\n", j.RunID, j.Account, state, orDash(j.SessionID), j.Cwd)
	}
	alive := func(j run.Journal) bool { return supervisorAlive(j.SupervisorPID, j.SupervisorCreated) }
	if cut := run.Unfinished(run.All(e.paths.RunsDir()), alive, time.Now()); len(cut) > 0 {
		fmt.Println("\nCut off (Windows restarted, or the session stopped unexpectedly); reopen one with: baton run --restore <run>")
		for _, j := range cut {
			busy := ""
			if j.Owed {
				busy = "  (was working)"
			}
			fmt.Printf("run %s  account=%s  ended %s%s\n  cwd: %s\n", j.RunID, j.Account, j.Updated.Local().Format("Jan 2 15:04"), busy, j.Cwd)
		}
	}
	if n := len(st.Switches); n > 0 {
		fmt.Println("\nRecent switches:")
		for _, sw := range st.Switches[max(0, n-5):] {
			fmt.Printf("  %s  %s -> %s  (%s)\n", sw.At.Local().Format("Jan 2 15:04"), sw.From, sw.To, sw.Reason)
		}
	}
	return nil
}

func cmdConfig(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		for _, k := range config.Keys {
			v, _ := e.cfg.Get(k)
			fmt.Printf("%s = %s\n", k, v)
		}
		return nil
	case 1:
		v, ok := e.cfg.Get(args[0])
		if !ok {
			return fmt.Errorf("unknown key %q (keys: %s)", args[0], strings.Join(config.Keys, ", "))
		}
		fmt.Println(v)
		return nil
	}
	if err := refuseInsideSession("change baton settings"); err != nil {
		return err
	}
	key, val := args[0], strings.Join(args[1:], " ")
	// Questions are asked first; the change itself is applied to a fresh copy
	// under the lock, so it cannot undo another command's change.
	set, err := config.Setter(e.cfg, key, val, false)
	if errors.Is(err, config.ErrNeedsAcknowledgement) {
		fmt.Fprint(os.Stderr, riskNotice)
		ok, err := yes("Have you read this, and do you want baton to switch accounts? [y/N] ")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("switch mode not changed")
		}
		set, err = config.Setter(e.cfg, key, val, true)
	}
	if err != nil {
		return err
	}
	_, err = config.Update(e.paths.Config(), e.paths.ConfigLock(), func(c *config.Config) error {
		set(c)
		return nil
	})
	return err
}

func cmdDoctor(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: baton doctor")
	}
	e, err := load()
	if err != nil {
		return err
	}
	ok := true
	fmt.Printf("data folder:  %s\n", e.paths.Root)
	inst, all, fb, err := e.findClaude()
	var after []string // what the message says after its list of copies
	if err != nil {
		msg := err.Error()
		if len(all) > 0 {
			// Each copy, with its fix, is listed below; what else the message
			// says (the desktop app's Code tab, claude_path) comes after it.
			lines := strings.Split(msg, "\n")
			msg = lines[0]
			for _, l := range lines[1:] {
				if !strings.HasPrefix(l, "  ") {
					after = append(after, l)
				}
			}
		}
		fmt.Printf("claude:       PROBLEM: %s\n", msg)
		ok = false
		inst = claude.Install{}
	} else {
		fmt.Printf("claude:       %s (%s, found via %s)\n", inst.Version, inst.Path, inst.Source)
	}
	// Every Claude Code on this PC; * is the one sessions use.
	for _, i := range all {
		mark, note, v := " ", "", i.Version
		if inst.Path != "" && strings.EqualFold(i.Path, inst.Path) {
			mark, note = "*", "(in use) "
		}
		if i.Problem != "" {
			note += i.Fix()
		}
		if v == "" {
			v = "?"
		}
		line := fmt.Sprintf("              %s %-9s %-8s %s  %s", mark, v, i.Source, i.Path, note)
		fmt.Println(strings.TrimRight(line, " "))
	}
	for _, l := range after {
		fmt.Println("              " + l)
	}
	if fb != nil {
		fmt.Println("              " + strings.TrimPrefix(fallbackLine(fb, inst), "baton: "))
	}
	fmt.Printf("              newest version tested with this baton: %s\n", claude.TestedWith)
	switch {
	case consoleUsable():
		fmt.Println("console:      ok")
	case winproc.StdinIsConsole():
		fmt.Println("console:      PROBLEM: not a console window; run baton from Windows Terminal, PowerShell or cmd")
		ok = false
	default:
		fmt.Println("console:      stdin is not a console here; `baton run` will open a new console window")
	}
	// Claude Code reads a picture from the clipboard (Alt+V) with Windows
	// PowerShell, found on PATH; it skips a match in the working folder.
	if p, err := exec.LookPath("powershell"); err == nil && p != "" {
		fmt.Println("image paste:  ok (Alt+V in the session window)")
	} else {
		fmt.Println(`image paste:  Windows PowerShell is not on PATH, so Alt+V cannot paste pictures in Claude Code; put C:\Windows\System32\WindowsPowerShell\v1.0 back on PATH`)
	}
	fmt.Printf("switch mode:  %s\n", orDash(e.cfg.SwitchMode))
	if inst.Path != "" {
		cwd, _ := os.Getwd()
		fresh := map[string]accounts.Account{}
		asked := map[string]time.Time{} // when each account's status was read
		var ids []string
		for _, a := range e.reg.Accounts {
			asked[a.ID] = time.Now()
			if err := e.refreshAuth(inst.Path, &a); err != nil {
				fmt.Printf("account %-12s PROBLEM: %v\n", a.ID, err)
				ok = false
				continue
			}
			fresh[a.ID] = a
			ids = append(ids, a.ID)
			state := "signed in"
			if !a.LoggedIn {
				state, ok = "NOT signed in (baton login "+a.ID+")", false
			}
			fmt.Printf("account %-12s %s  %s  %s\n", a.ID, state, orDash(a.Email), orDash(a.Subscription))
			if a.LoggedIn && a.AuthMethod != "" && a.AuthMethod != "claude.ai" {
				fmt.Printf("             PROBLEM: signs in with %q, not a Claude subscription\n", a.AuthMethod)
				ok = false
			}
			if why := claude.HooksBlocked(a.ProfileDir, cwd); why != "" {
				fmt.Printf("             PROBLEM: %s; usage limits would go unnoticed in this folder\n", why)
				ok = false
			}
		}
		for i, x := range ids {
			for _, y := range ids[i+1:] {
				// A disabled duplicate is never switched to, so only enabled pairs matter.
				if fresh[x].Enabled && fresh[y].Enabled && fresh[x].LoggedIn && fresh[y].LoggedIn && accounts.SameIdentity(fresh[x], fresh[y]) {
					fmt.Printf("PROBLEM: %q and %q are the same Claude account (%s); disable one: baton disable %s\n", x, y, fresh[x].Email, y)
					ok = false
				}
			}
		}
		_, _ = accounts.Update(e.paths.Accounts(), e.paths.AccountsLock(), func(r *accounts.Registry) error {
			for i := range r.Accounts {
				a, found := fresh[r.Accounts[i].ID]
				// A sign-in that finished after this check read the account is
				// newer than what the check saw.
				if found && !r.Accounts[i].CheckedAt.After(asked[a.ID]) {
					copyAuth(&r.Accounts[i], a)
				}
			}
			return nil
		})
	}
	if !ok {
		return errors.New("some checks failed")
	}
	return nil
}

// cmdHook records one hook call for the supervisor. It is run by claude.exe
// (exec form, no shell) and must stay fast, quiet and exit 0: a failing
// UserPromptSubmit-style hook could block the user's prompt.
func cmdHook(args []string) int {
	if len(args) < 3 || args[1] != "--run-dir" {
		return 0
	}
	event, dir := args[0], args[2]
	data, _ := run.ReadLimited(os.Stdin, 4<<20)
	payload := json.RawMessage(data)
	if !json.Valid(data) {
		payload, _ = json.Marshal(string(data))
	}
	pid, _ := strconv.Atoi(os.Getenv("CLAUDE_PID"))
	_ = run.AppendEvent(dir, run.Event{TS: time.Now(), Event: event, ClaudePID: pid, Payload: payload})
	return 0
}
