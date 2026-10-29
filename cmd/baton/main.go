// Command baton keeps a Claude Code session going across several of your own
// Claude accounts: when the current account hits its usage limit, baton
// restarts the same conversation on the next logged-in account.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"baton/internal/app"
	"baton/internal/config"
	"baton/internal/winproc"
	"baton/internal/winui"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "0.3.0-dev"

const usageText = `baton - continue a Claude Code session on your other accounts when one hits its usage limit

Usage:
  baton add <name> [--priority N]    log in a new account (opens Claude's sign-in)
  baton login <name>                 sign an account in again
  baton list                         show accounts and their status
  baton enable|disable <name>        include or skip an account (enable also clears "blocked")
  baton remove <name> [--force]      log out and delete an account's profile
  baton run [--account <name>] [-- <claude args>]
                                     start Claude Code under supervision
  baton sessions                     list your conversations in every folder, including
                                     the Claude desktop app's
  baton resume <id> [--copy]         continue one of them in its folder (--copy leaves
                                     the original as it is, for one open elsewhere)
  baton switch [<name>] [--run <id>] move a running session to another account
                                     (once its current turn ends)
  baton stop [--run <id>]            end a running session (it can be continued later)
  baton gui                          open the baton window
  baton status                       accounts plus running sessions
  baton sync                         share your normal Claude setup (skills, settings,
                                     CLAUDE.md...) with every account profile
  baton config [<key> [<value>]]     show or change settings
  baton doctor                       check claude.exe, the console and every login
  baton version

Data lives in %LOCALAPPDATA%\baton (override with BATON_HOME).
`

// plainUsage answers --help for the commands that have no options of their
// own (the others print theirs through newFlags).
var plainUsage = map[string]string{
	"list":    "baton list",
	"ls":      "baton list",
	"enable":  "baton enable <name>",
	"disable": "baton disable <name>",
	"status":  "baton status",
	"sync":    "baton sync",
	"config":  "baton config [<key> [<value>]]   keys: " + strings.Join(config.Keys, ", ") + "; claude_path auto detects it again",
	"doctor":  "baton doctor",
}

func main() {
	if guiBuild == "1" {
		// baton-app.exe: the window only. It has no console, so its output and
		// any crash go to a log file.
		logPath := openGUILog()
		if msg := unzipFirst(); msg != "" {
			winui.MessageBox("baton", msg)
			os.Exit(1)
		}
		if err := cmdGUI(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "baton:", err)
			winui.MessageBox("baton", err.Error()+"\n\n"+logPath)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) < 2 {
		if winui.OwnConsole() {
			// Double-clicked in Explorer: open the window instead of flashing
			// the usage text in a console that closes at once.
			if os.Getenv("BATON_RUN") == "" && startApp() {
				os.Exit(0)
			}
			fmt.Fprint(os.Stderr, usageText+"\nbaton.exe is the command-line program; open baton-app.exe for the window,\n"+
				"or run commands like `baton add <name>` in PowerShell.\n\nPress Enter to close this window.")
			_, _ = stdin.ReadString('\n')
			os.Exit(0)
		}
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if os.Getenv(newConsoleEnv) == "1" && (cmd == "run" || cmd == "add" || cmd == "login" || cmd == "uninstall") {
		// Started in a console window of its own (by reopenInNewConsole or the
		// GUI): talk to that window, not NUL. Never for `baton hook`, which
		// must keep reading its payload from stdin.
		inNewConsole = true
		os.Unsetenv(newConsoleEnv)
		_ = winproc.AdoptConsoleStdio()
		stdin = bufio.NewReader(os.Stdin)
		switch {
		case cmd == "run":
			cwd, _ := os.Getwd()
			winui.SetConsoleTitle("baton - " + filepath.Base(cwd))
		case cmd == "uninstall":
			winui.SetConsoleTitle("baton - uninstall")
		case len(args) > 0:
			winui.SetConsoleTitle("baton - sign in: " + args[0])
		}
	}
	if cmd == "uninstall" && !inNewConsole && winui.OwnConsole() {
		// Started from Windows' installed-apps list, in a console of its own:
		// keep it open to show the outcome, in the window's language.
		inNewConsole = true
		if os.Getenv("BATON_LANG") == "" {
			if p, err := app.DefaultPaths(); err == nil {
				os.Setenv("BATON_LANG", uiLang(p.Config(), winui.UserLang()))
			}
		}
	}
	if u, ok := plainUsage[cmd]; ok && len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(os.Stderr, "usage:", u)
		os.Exit(0)
	}
	var err error
	code := 0
	switch cmd {
	case "run":
		code, err = cmdRun(args)
	case "resume":
		code, err = cmdResume(args)
	case "sessions":
		err = cmdSessions(args)
	case "add":
		err = cmdAdd(args)
	case "login":
		err = cmdLogin(args)
	case "list", "ls":
		err = cmdList(args)
	case "enable":
		err = cmdSetEnabled(args, true)
	case "disable":
		err = cmdSetEnabled(args, false)
	case "remove", "rm":
		err = cmdRemove(args)
	case "switch":
		err = cmdSwitch(args)
	case "stop":
		err = cmdStop(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "uninstall-finish":
		err = cmdUninstallFinish(args)
	case "status":
		err = cmdStatus(args)
	case "sync":
		err = cmdSync(args)
	case "config":
		err = cmdConfig(args)
	case "doctor":
		err = cmdDoctor(args)
	case "gui":
		if err = refuseInsideSession("open the baton window"); err != nil {
			break
		}
		if len(args) == 0 && startApp() {
			break // baton-app.exe opens the window; this console is free again
		}
		err = cmdGUI(args)
	case "hook":
		os.Exit(cmdHook(args)) // called by claude.exe; must never block or fail loudly
	case "statusline":
		cmdStatusLine() // called by claude.exe for the line under its prompt
		os.Exit(0)
	case "version", "--version", "-v":
		fmt.Println("baton", version)
	case "help", "-h", "--help":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "baton: unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		err, code = nil, 0 // `baton <cmd> --help` printed its usage; that is not a failure
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "baton:", err)
		if code == 0 {
			code = 1
		}
	}
	// A session that ended normally says how to continue it; the window can
	// close. Anything else waits for Enter so the message can be read.
	holdNewConsole(err == nil && (cmd != "run" && cmd != "resume" || code == 0 && !runNeedsAttention))
	os.Exit(code)
}
