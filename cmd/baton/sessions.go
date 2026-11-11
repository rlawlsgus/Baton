package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"baton/internal/claude"
	"baton/internal/run"
	"baton/internal/share"
	"baton/internal/winui"
)

// conversationDirs are the config folders conversations live in: every
// account's profile, and the normal setup (~/.claude, where the desktop app
// and a plain `claude` keep theirs). names says what each one is.
func conversationDirs(e *env) (dirs []string, names map[string]string) {
	names = map[string]string{}
	for _, a := range e.reg.Accounts {
		dirs = append(dirs, a.ProfileDir)
		names[a.ProfileDir] = fmt.Sprintf(say("account %s", "%s 계정"), a.ID)
	}
	if home := share.UserHome(e.paths.ProfilesDir()); home != "" {
		dirs = append(dirs, home)
		names[home] = ""
	}
	return dirs, names
}

// sourceName says what made a conversation kept in the normal setup.
func sourceName(source string) string {
	switch source {
	case "claude-desktop":
		return say("Claude desktop app", "Claude 데스크톱 앱")
	case "cli":
		return say("terminal", "터미널")
	case "claude-vscode":
		return "VS Code"
	}
	return "Claude Code"
}

// holderName says what holds a conversation open (a live claude's entrypoint).
func holderName(entrypoint string) string {
	switch entrypoint {
	case "claude-desktop":
		return say("the Claude desktop app", "Claude 데스크톱 앱")
	case "cli":
		return say("Claude Code in a terminal", "터미널의 Claude Code")
	case "claude-vscode":
		return "VS Code"
	}
	return say("another Claude Code window", "다른 Claude Code 창")
}

// cmdSessions lists the conversations of every folder, newest first: the
// ones on baton's accounts and the ones in the normal Claude Code setup,
// the desktop app's sessions included.
func cmdSessions(args []string) error {
	fs := newFlags("sessions", "baton sessions [--max N]")
	max := fs.Int("max", 20, "how many to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: baton sessions [--max N]")
	}
	e, err := load()
	if err != nil {
		return err
	}
	dirs, names := conversationDirs(e)
	list := claude.ListAllTranscripts(dirs, *max)
	if len(list) == 0 {
		fmt.Println(say("No conversations yet.", "아직 대화가 없습니다."))
		return nil
	}
	open := claude.OpenSessions(dirs)
	inBaton := map[string]bool{}
	for sid := range open {
		inBaton[sid] = run.HeldBy(dirs, e.paths.RunsDir(), sid, supervisorAlive).Run != nil
	}
	for _, t := range list {
		title := t.Title
		if title == "" {
			title = t.FirstPrompt
		}
		where := names[t.ConfigDir]
		if where == "" {
			where = sourceName(t.Source)
		}
		mark := ""
		switch holder := open[t.SessionID]; {
		case inBaton[t.SessionID]:
			mark = say("  [running in baton]", "  [baton에서 실행 중]")
		case holder != "":
			mark = say("  [open in "+holderName(holder)+": it continues in a copy]",
				"  [열려 있음: "+holderName(holder)+". 사본으로 이어 갑니다]")
		}
		fmt.Printf("%s  %s  %s%s\n    %s  (%s)\n", t.SessionID, t.Modified.Local().Format("2006-01-02 15:04"), title, mark, t.Cwd, where)
	}
	fmt.Println()
	fmt.Println(say("Continue one in its folder:  baton resume <id>   (--copy goes on in a copy, leaving the original as it is)",
		"이어 가기 (그 대화의 폴더에서 시작):  baton resume <id>   (--copy: 원래 대화는 그대로 두고 사본으로 이어 가기)"))
	return nil
}

var sessionIDRx = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// cmdResume continues a conversation from anywhere: `baton run` in the
// folder it last ran in, resuming its file wherever it is kept.
func cmdResume(args []string) (int, error) {
	const usage = "baton resume <id> [--account <name>] [--copy]"
	fs := newFlags("resume", usage)
	account := fs.String("account", "", "account to start on (default: best available)")
	copyIt := fs.Bool("copy", false, "go on in a copy and leave the conversation as it is (one open elsewhere goes on in a copy anyway)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	id, err := oneName(pos, usage)
	if err != nil {
		return 2, err
	}
	if !sessionIDRx.MatchString(id) {
		return 2, errors.New(say("a session id looks like 1b2c3d4e-...; see: baton sessions", "세션 id는 1b2c3d4e-... 형식입니다. baton sessions로 확인하세요"))
	}
	if err := refuseInsideSession("start a supervised session"); err != nil {
		return 1, err
	}
	e, err := load()
	if err != nil {
		return 1, err
	}
	plan, code, err := planResume(e, id, *copyIt)
	if err != nil || code != 0 {
		return code, err
	}
	if plan.inBaton != nil {
		// Two writers would split it: its own window comes forward instead.
		focusRun(*plan.inBaton)
		return 0, nil
	}
	if plan.copied {
		fmt.Fprintf(os.Stderr, "baton: "+say("It is open in %s, so it continues in a copy: everything said so far, going on separately from the original.\n",
			"%s에서 열려 있어 사본으로 이어 갑니다. 지금까지의 내용이 모두 들어 있고, 원래 대화와는 따로 진행됩니다.\n"), holderName(plan.holder))
	}
	if err := os.Chdir(plan.cwd); err != nil {
		return 1, err
	}
	fmt.Fprintf(os.Stderr, say("baton: continuing in %s\n", "baton: %s 폴더에서 이어 갑니다\n"), filepath.Clean(plan.cwd))
	run := []string{}
	if *account != "" {
		run = append(run, "--account", *account)
	}
	if plan.copied && !*copyIt {
		run = append(run, "--auto-copy-of", strings.ToLower(id))
	}
	run = append(run, "--", "--resume", plan.transcript)
	if *copyIt || plan.copied {
		run = append(run, "--fork-session")
	}
	return cmdRun(run)
}

// resumePlan is how `baton resume` goes on with a conversation: its file
// and folder, and whether it goes on in a copy because another Claude Code
// has it open (holder: what that is), or not at all because a baton session
// runs it (inBaton: that session's run).
type resumePlan struct {
	transcript, cwd string
	copied          bool
	holder          string
	inBaton         *run.Journal
}

// planResume finds conversation id and decides how it goes on; code is the
// exit code for a problem (2: a bad id). copyIt: the user asked for a copy,
// which never writes the original, so nobody else's hold matters then.
func planResume(e *env, id string, copyIt bool) (resumePlan, int, error) {
	if !sessionIDRx.MatchString(id) {
		return resumePlan{}, 2, errors.New(say("a session id looks like 1b2c3d4e-...; see: baton sessions", "세션 id는 1b2c3d4e-... 형식입니다. baton sessions로 확인하세요"))
	}
	id = strings.ToLower(id) // Claude Code names the files in lower case
	dirs, _ := conversationDirs(e)
	p := claude.FindTranscript(dirs, id)
	if p == "" {
		return resumePlan{}, 1, fmt.Errorf(say("no conversation %s (see: baton sessions)", "%s 대화가 없습니다 (baton sessions로 확인)"), id)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return resumePlan{}, 1, err
	}
	cwd := claude.Describe(p, fi).Cwd
	if st, err := os.Stat(cwd); cwd == "" || err != nil || !st.IsDir() {
		return resumePlan{}, 1, fmt.Errorf(say("the folder this conversation ran in (%s) is gone", "이 대화를 하던 폴더(%s)가 없습니다"), orDash(cwd))
	}
	plan := resumePlan{transcript: p, cwd: cwd}
	if copyIt {
		return plan, 0, nil
	}
	h := run.HeldBy(dirs, e.paths.RunsDir(), id, supervisorAlive)
	switch {
	case h.Run != nil:
		plan.inBaton = h.Run
	case len(h.Holders) > 0:
		plan.copied, plan.holder = true, h.Entrypoint()
	}
	return plan, 0, nil
}

// focusRun brings the window of a running baton session to the front, and
// says so (or where to find it, when Windows has no window to show).
func focusRun(j run.Journal) {
	if j.ConsoleWindow != 0 && winui.FocusShown(uintptr(j.ConsoleWindow)) == nil {
		fmt.Fprintln(os.Stderr, "baton: "+say("This conversation is already running in baton; its window is in front now.",
			"이 대화는 이미 baton에서 실행 중입니다. 그 창을 앞으로 가져왔습니다."))
		return
	}
	fmt.Fprintf(os.Stderr, "baton: "+say("This conversation is already running in baton (session %s; see: baton status).\n",
		"이 대화는 이미 baton에서 실행 중입니다 (세션 %s, baton status로 확인).\n"), j.RunID)
}
