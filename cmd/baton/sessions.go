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
	"baton/internal/share"
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
		if open[t.SessionID] {
			mark = say("  [open in another window]", "  [다른 창에서 열려 있음]")
		}
		fmt.Printf("%s  %s  %s%s\n    %s  (%s)\n", t.SessionID, t.Modified.Local().Format("2006-01-02 15:04"), title, mark, t.Cwd, where)
	}
	fmt.Println()
	fmt.Println(say("Continue one in its folder:  baton resume <id>   (add --copy for one open in another window)",
		"이어 가기 (그 대화의 폴더에서 시작):  baton resume <id>   (다른 창에서 열려 있으면 --copy)"))
	return nil
}

var sessionIDRx = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// cmdResume continues a conversation from anywhere: `baton run` in the
// folder it last ran in, resuming its file wherever it is kept.
func cmdResume(args []string) (int, error) {
	const usage = "baton resume <id> [--account <name>] [--copy]"
	fs := newFlags("resume", usage)
	account := fs.String("account", "", "account to start on (default: best available)")
	copyIt := fs.Bool("copy", false, "go on in a copy and leave the conversation as it is (for one open in another window)")
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
	id = strings.ToLower(id) // Claude Code names the files in lower case
	if err := refuseInsideSession("start a supervised session"); err != nil {
		return 1, err
	}
	e, err := load()
	if err != nil {
		return 1, err
	}
	dirs, _ := conversationDirs(e)
	p := claude.FindTranscript(dirs, id)
	if p == "" {
		return 1, fmt.Errorf(say("no conversation %s (see: baton sessions)", "%s 대화가 없습니다 (baton sessions로 확인)"), id)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 1, err
	}
	cwd := claude.Describe(p, fi).Cwd
	if st, err := os.Stat(cwd); cwd == "" || err != nil || !st.IsDir() {
		return 1, fmt.Errorf(say("the folder this conversation ran in (%s) is gone", "이 대화를 하던 폴더(%s)가 없습니다"), orDash(cwd))
	}
	if !*copyIt && len(claude.Holders(dirs, id)) > 0 {
		return 1, errors.New(say("this conversation is open in another window (such as the Claude desktop app); close it there, or continue in a copy: baton resume "+id+" --copy",
			"이 대화는 다른 창(Claude 데스크톱 앱 등)에서 열려 있습니다. 그쪽에서 닫거나, 사본으로 이어 가세요: baton resume "+id+" --copy"))
	}
	if err := os.Chdir(cwd); err != nil {
		return 1, err
	}
	fmt.Fprintf(os.Stderr, say("baton: continuing in %s\n", "baton: %s 폴더에서 이어 갑니다\n"), filepath.Clean(cwd))
	run := []string{}
	if *account != "" {
		run = append(run, "--account", *account)
	}
	run = append(run, "--", "--resume", p)
	if *copyIt {
		run = append(run, "--fork-session")
	}
	return cmdRun(run)
}
