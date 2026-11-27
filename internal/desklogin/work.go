package desklogin

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"baton/internal/claude"
	"baton/internal/desktop"
	"baton/internal/winproc"
)

// What a session of the desktop app's is doing that quitting the app would
// stop, from the most to the least pressing: Working.What.
const (
	WorkTurn    = "turn"    // Claude is working on a reply
	WorkWaiting = "waiting" // Claude is waiting for the user's answer, in the middle of a reply
	WorkCommand = "command" // a command runs in the background (or a Monitor)
	WorkAgent   = "agent"   // a background agent or workflow runs
	WorkWakeup  = "wakeup"  // Claude is to wake up again (a /loop, a cron job)
	WorkUnknown = "unknown" // baton cannot tell now whether it is working
	// WorkSilent: its Claude Code does not say what it is doing (an older one
	// says it only in a terminal, not for the desktop app's sessions), so
	// baton never can tell: the user quits the app instead.
	WorkSilent = "silent"
)

var workOrder = []string{WorkTurn, WorkWaiting, WorkCommand, WorkAgent, WorkWakeup, WorkUnknown, WorkSilent}

// silentAfter: a Claude Code that says what it is doing does so within a
// moment of taking up its session; one that has said nothing for longer
// does not (WorkSilent).
const silentAfter = 15 * time.Second

// Working is a session of the desktop app's that quitting the app would
// stop.
type Working struct {
	Session string `json:"session,omitempty"` // its conversation's id
	Title   string `json:"title"`             // what the app calls it (its folder's name if nothing)
	Cwd     string `json:"cwd,omitempty"`
	What    string `json:"what"`
}

// WorkingError refuses to quit the desktop app for a switch: something in
// it is working. Its code is CodeAppWorking.
type WorkingError struct{ Sessions []Working }

func (e *WorkingError) Error() string {
	return fmt.Sprintf("the desktop app is working in %d session(s); stop that first", len(e.Sessions))
}

func (e *WorkingError) Unwrap() error { return &desktop.Error{Code: CodeAppWorking} }

// Working reports the sessions of the desktop app's that a quit would stop
// now. They are the Claude Code sessions the app runs (registered in the
// user's own Claude Code folder by the app: entrypoint claude-desktop, or
// local-agent), each one alive. One is working when Claude works on a
// reply or waits for the user's answer in it (as Claude Code registers
// it), when a command it started in the background still runs (a shell
// Claude Code's own launcher started, under its process), or when its
// transcript tells of background work started since its process did and
// not ended (agents, workflows, commands; a wake-up or cron job to come).
// A registration that cannot be read, or that names a state baton does not
// know, counts as one that may be working; so does one that names none
// (WorkSilent once it has had time to). A process the app keeps ready for a
// session to come (spare) does nothing yet.
func (l *Logins) Working() []Working { return l.working(true) }

// working is Working; without background, it looks at the sessions'
// states only (no processes, no transcripts), which is quick.
func (l *Logins) working(background bool) []Working {
	if l.workingFn != nil {
		return l.workingFn()
	}
	now := l.Now()
	var out []Working
	for _, s := range claude.ReadSessions(l.UserHome) {
		if s.Status != "unknown" && !desktopEntry(s.Entrypoint) || s.Spare && s.Status != "busy" && s.Status != "waiting" || !s.Alive() {
			continue
		}
		w := Working{Session: s.SessionID, Title: strings.TrimSpace(s.Name), Cwd: s.Cwd}
		if w.Title == "" && s.Cwd != "" {
			w.Title = filepath.Base(filepath.Clean(s.Cwd))
		}
		switch s.Status {
		case "busy":
			w.What = WorkTurn
		case "waiting":
			w.What = WorkWaiting
		case "idle":
			if background {
				w.What = l.background(s, now)
			}
		case "":
			// None at all: said within a moment by a Claude Code that says it.
			// (startedAt is Claude Code's own clock: against the real one.)
			w.What = WorkUnknown
			if s.StartedAt > 0 && time.Since(time.UnixMilli(s.StartedAt)) > silentAfter {
				w.What = WorkSilent
			}
		default:
			w.What = WorkUnknown
		}
		if w.What != "" {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := slices.Index(workOrder, out[i].What), slices.Index(workOrder, out[j].What)
		if a != b {
			return a < b
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// desktopEntry: a Claude Code the desktop app runs (its Code tab, its
// Cowork), which ends when the app quits.
func desktopEntry(e string) bool { return e == "claude-desktop" || e == "local-agent" }

// background is what an idle session of the app's has going on in the
// background ("" nothing).
func (l *Logins) background(s claude.LiveSession, now time.Time) string {
	for _, c := range winproc.Children(s.PID) {
		if launchedShell(c) {
			return WorkCommand
		}
	}
	if s.SessionID == "" {
		return ""
	}
	since := winproc.ProcessCreated(s.PID)
	if since.IsZero() {
		return "" // ended meanwhile: nothing of it to stop
	}
	// The app makes a Claude Code ahead of time and gives it a session when
	// one is opened (startedAt): work of the session's from before then was
	// an earlier process's, and ended with it.
	if at := time.UnixMilli(s.StartedAt); s.StartedAt > 0 && at.After(since) {
		since = at
	}
	b, err := claude.BackgroundSince(claude.FindTranscript([]string{l.UserHome}, s.SessionID), since.Add(-2*time.Second), now)
	switch {
	case err != nil:
		return WorkUnknown
	case len(b.Commands) > 0:
		return WorkCommand
	case len(b.Agents) > 0:
		return WorkAgent
	case len(b.Wakeups) > 0:
		return WorkWakeup
	}
	return ""
}

// shellLaunchMarks are in the command line of every shell Claude Code's
// Bash and PowerShell tools start (the Bash tool sources its shell
// snapshot; the PowerShell tool hands its script over in a variable).
var shellLaunchMarks = []string{"shell-snapshots", "__claudecodescript", "claude_code_shell_launcher_script"}

// shells are the programs Claude Code runs commands in.
var shells = []string{"bash.exe", "sh.exe", "zsh.exe", "powershell.exe", "pwsh.exe", "cmd.exe"}

// launchedShell reports whether c, a process an idle Claude Code started,
// is a command of its own that still runs: a shell its tools started. Its
// other processes (an MCP server it runs, the console host) end with it
// and do no work of the user's. A shell whose command line cannot be read
// counts.
func launchedShell(c winproc.Proc) bool {
	if winproc.IsConsoleHost(c.Exe) {
		return false
	}
	line, err := winproc.CommandLine(c.PID)
	if err != nil {
		return slices.ContainsFunc(shells, func(s string) bool { return strings.EqualFold(s, c.Exe) })
	}
	low := strings.ToLower(line)
	return slices.ContainsFunc(shellLaunchMarks, func(m string) bool { return strings.Contains(low, m) })
}
