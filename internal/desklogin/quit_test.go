package desklogin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"baton/internal/desktop"
	"baton/internal/winproc"

	"golang.org/x/sys/windows"
)

// sleeper starts a process that stands in for a Claude Code: this test
// binary run again, sleeping until the test ends (it is killed then).
func sleeper(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSleeperHelper$")
	cmd.Env = append(os.Environ(), "BATON_SLEEPER=1")
	must(t, cmd.Start())
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd.Process.Pid
}

func TestSleeperHelper(t *testing.T) {
	if os.Getenv("BATON_SLEEPER") == "" {
		t.Skip("run by sleeper")
	}
	time.Sleep(10 * time.Minute)
}

// register writes the user's Claude Code registry entry of the live
// process pid, as Claude Code does: started when the process was.
func (r *rig) register(pid int, sid, status, entry, name string) {
	r.t.Helper()
	r.registerAt(pid, sid, status, entry, name, time.Time{}, "")
}

// registerAt is register for a process the app made ahead of time and gave
// the session at taken (zero: when it started), with more of the entry's
// fields (extra, JSON members with a leading comma).
func (r *rig) registerAt(pid int, sid, status, entry, name string, taken time.Time, extra string) {
	r.t.Helper()
	born := winproc.ProcessCreated(pid)
	if born.IsZero() {
		r.t.Fatalf("process %d is gone", pid)
	}
	ft := windows.NsecToFiletime(born.UnixNano())
	procStart := strconv.FormatInt(int64(ft.HighDateTime)<<32|int64(ft.LowDateTime), 10)
	if taken.IsZero() {
		taken = born
	}
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":%q,"startedAt":%d,"procStart":%q,"kind":"interactive","entrypoint":%q,"name":%q,"status":%q%s}`,
		pid, sid, filepath.Join(r.root, "proj", name), taken.UnixMilli(), procStart, entry, name, status, extra)
	dir := filepath.Join(r.l.UserHome, "sessions")
	must(r.t, os.MkdirAll(dir, 0o700))
	must(r.t, os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), []byte(body), 0o600))
}

// transcript writes a session's transcript in the user's Claude Code folder.
func (r *rig) transcript(sid string, rows ...string) {
	r.t.Helper()
	dir := filepath.Join(r.l.UserHome, "projects", "C--proj")
	must(r.t, os.MkdirAll(dir, 0o700))
	b := ""
	for _, row := range rows {
		b += row + "\n"
	}
	must(r.t, os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(b), 0o600))
}

func works(ws []Working) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Title+":"+w.What)
	}
	return out
}

// What a quit of the desktop app would stop: the sessions the app runs
// (claude-desktop, local-agent) where Claude works or waits for the user's
// answer, or where something runs in the background (a command its own
// shell launcher started, background work its transcript tells of since
// its process started). Others' sessions, ended ones and idle ones are not.
func TestWorking(t *testing.T) {
	r := newRig(t)
	if got := r.l.Working(); len(got) != 0 {
		t.Fatalf("nothing registered: %v", works(got))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	agent := func(at string) []string {
		return []string{
			`{"type":"assistant","timestamp":"` + at + `","message":{"content":[{"type":"tool_use","id":"toolu_a","name":"Agent","input":{"run_in_background":true}}]}}`,
			`{"type":"user","timestamp":"` + at + `","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"Async agent launched"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"ag1"}}`,
		}
	}
	r.register(sleeper(t), "s-turn", "busy", "claude-desktop", "turn")
	r.register(sleeper(t), "s-wait", "waiting", "local-agent", "wait")
	r.register(sleeper(t), "s-cli", "busy", "cli", "terminal")         // not the app's
	r.register(sleeper(t), "s-idle", "idle", "claude-desktop", "idle") // nothing going on
	pid := sleeper(t)
	r.register(pid, "s-agent", "idle", "claude-desktop", "agent")
	// Stamped once its process runs: started under load, a process can start
	// seconds after now, and work stamped before it was its predecessor's.
	r.transcript("s-agent", agent(time.Now().UTC().Format(time.RFC3339Nano))...)
	pidOld := sleeper(t)
	r.register(pidOld, "s-old", "idle", "claude-desktop", "old")
	r.transcript("s-old", agent(old)...) // its process's predecessor's
	// An ended process's entry, left behind.
	gone := exec.Command(os.Args[0], "-test.run=^$")
	must(t, gone.Run())
	must(t, os.WriteFile(filepath.Join(r.l.UserHome, "sessions", strconv.Itoa(gone.ProcessState.Pid())+".json"),
		[]byte(`{"pid":`+strconv.Itoa(gone.ProcessState.Pid())+`,"sessionId":"s-gone","entrypoint":"claude-desktop","status":"busy","startedAt":1}`), 0o600))
	want := []string{"turn:" + WorkTurn, "wait:" + WorkWaiting, "agent:" + WorkAgent}
	if got := works(r.l.Working()); !slices.Equal(got, want) {
		t.Fatalf("working %v, want %v", got, want)
	}

	// A command run in the background: a shell under its Claude Code that
	// Claude Code's launcher started. This test process stands in for that
	// Claude Code.
	r.register(os.Getpid(), "s-cmd", "idle", "claude-desktop", "cmd")
	if got := works(r.l.Working()); !slices.Equal(got, want) {
		t.Fatalf("no command yet: %v", got)
	}
	other := exec.Command("cmd.exe", "/d", "/c", "ping -n 120 127.0.0.1 >nul") // a shell of its own (an MCP server's, say)
	must(t, other.Start())
	t.Cleanup(func() { other.Process.Kill(); other.Wait() })
	if got := works(r.l.Working()); !slices.Equal(got, want) {
		t.Fatalf("a shell not Claude Code's: %v", got)
	}
	shell := exec.Command("cmd.exe", "/d", "/c", "echo shell-snapshots >nul & ping -n 120 127.0.0.1 >nul")
	must(t, shell.Start())
	t.Cleanup(func() { shell.Process.Kill(); shell.Wait() })
	want = []string{"turn:" + WorkTurn, "wait:" + WorkWaiting, "cmd:" + WorkCommand, "agent:" + WorkAgent}
	if got := works(r.l.Working()); !slices.Equal(got, want) {
		t.Fatalf("with a command: %v, want %v", got, want)
	}

	// A process the app keeps ready does nothing yet; one the app made
	// ahead of time counts the session's work from when it was taken up
	// (the agent before then was an earlier process's).
	r.registerAt(sleeper(t), "", "idle", "claude-desktop", "spare", time.Time{}, `,"spare":true`)
	premade := sleeper(t)
	r.registerAt(premade, "s-premade", "idle", "claude-desktop", "premade", time.Now().Add(time.Minute), "")
	r.transcript("s-premade", agent(now)...)
	if got := works(r.l.Working()); !slices.Equal(got, want) {
		t.Fatalf("spare and pre-made: %v, want %v", got, want)
	}
	if got := works(r.l.working(false)); !slices.Equal(got, []string{"turn:" + WorkTurn, "wait:" + WorkWaiting}) {
		t.Fatalf("states only: %v", got)
	}

	// An entry that cannot be read may be any session's, and a state baton
	// does not know (none, or a new one) may be work: baton cannot tell.
	p := sleeper(t)
	must(t, os.WriteFile(filepath.Join(r.l.UserHome, "sessions", strconv.Itoa(p)+".json"), []byte(`{"pid":`), 0o600))
	if got := r.l.Working(); len(got) != len(want)+1 || got[len(got)-1].What != WorkUnknown {
		t.Fatalf("unreadable: %v", works(got))
	}
	r.register(sleeper(t), "s-none", "", "claude-desktop", "none")
	r.register(sleeper(t), "s-new", "compacting", "claude-desktop", "new")
	got := works(r.l.Working())
	if !slices.Contains(got, "none:"+WorkUnknown) || !slices.Contains(got, "new:"+WorkUnknown) {
		t.Fatalf("unknown states: %v", got)
	}
	// None for longer than a Claude Code that says it takes: its Claude Code
	// does not say it (an older one, for the desktop app's sessions).
	r.registerAt(sleeper(t), "s-quiet", "", "claude-desktop", "quiet", time.Now().Add(-time.Minute), "")
	if got := works(r.l.Working()); !slices.Contains(got, "quiet:"+WorkSilent) || !slices.Contains(got, "none:"+WorkUnknown) {
		t.Fatalf("a Claude Code that does not say: %v", got)
	}
}

// running makes the desktop app run, as the stand-in file says
// (desktop.QuitApp deletes it); it returns the file.
func (r *rig) running() string {
	r.t.Helper()
	f := filepath.Join(r.root, "app-running")
	must(r.t, os.WriteFile(f, nil, 0o600))
	r.t.Setenv("BATON_DESKTOP_RUNNING", "file:"+f)
	return f
}

// A switch for a running app quits it first, when nothing in it is working
// and the switch would go ahead; it then switches as with the app quit,
// and says it quit it (the caller opens it again). Anything else quits
// nothing and changes nothing.
func TestSwitchAfterQuit(t *testing.T) {
	r := newRig(t)
	r.keptAll() // on A, A and B kept
	f := r.running()
	before, store := tree(t, r.data), tree(t, r.l.Store)
	unchanged := func(what string) {
		t.Helper()
		sameTree(t, what+": the app's folder", before, tree(t, r.data))
		sameTree(t, what+": the store", store, tree(t, r.l.Store))
		if !isFile(f) {
			t.Fatalf("%s: the app was quit", what)
		}
	}
	res, err := r.l.Use(acctB)
	wantCode(t, "use while running", err, CodeAppRunning)
	unchanged("use while running")

	// Refused before anything is quit: already on it, nothing kept, damaged.
	res, err = r.l.UseAfterQuit(acctA)
	wantCode(t, "already", err, CodeAlready)
	if res.Quit {
		t.Fatal("quit for a switch refused")
	}
	unchanged("already")
	wantCode(t, "not kept", useErr(r.l.UseAfterQuit(acctC)), CodeNotKept)
	unchanged("not kept")

	// Something in the app is working: it says what, and quits nothing.
	r.l.workingFn = func() []Working { return []Working{{Title: "fix the bug", What: WorkTurn}} }
	res, err = r.l.UseAfterQuit(acctB)
	var we *WorkingError
	if !errors.As(err, &we) || Code(err) != CodeAppWorking || len(we.Sessions) != 1 || we.Sessions[0].Title != "fix the bug" || res.Quit {
		t.Fatalf("working: %v %+v", err, res)
	}
	unchanged("working")
	r.l.workingFn = nil

	// The app would not quit, or not in time.
	for _, c := range []struct {
		err    error
		detail string
	}{{desktop.ErrQuitRefused, "refused"}, {desktop.ErrQuitTimeout, ""}} {
		r.l.QuitApp = func() error { return c.err }
		res, err = r.l.UseAfterQuit(acctB)
		var de *desktop.Error
		if !errors.As(err, &de) || de.Code != CodeQuitFailed || de.Detail != c.detail || res.Quit {
			t.Fatalf("%v: %v %+v", c.err, err, res)
		}
		unchanged("not quit")
	}
	r.l.QuitApp = func() error { return desktop.QuitApp(time.Second) }

	// Quit, then switched.
	res, err = r.l.UseAfterQuit(acctB)
	if err != nil || !res.Quit || !res.Kept || res.From != acctA || res.To != acctB {
		t.Fatalf("switch: %v %+v", err, res)
	}
	if isFile(f) || desktop.Active(r.data) != acctB {
		t.Fatal("not quit, or not switched")
	}
	r.noStage()

	// Quit, and the switch then fails: it is undone, and says it quit the
	// app all the same (the caller opens it again, on B).
	f = r.running()
	before = tree(t, r.data)
	r.stopAt("config", errTest)
	res, err = r.l.UseAfterQuit(acctA)
	wantCode(t, "failed after the quit", err, CodeSwitchFailed)
	if !res.Quit || isFile(f) {
		t.Fatalf("failed after the quit: %+v", res)
	}
	sameTree(t, "undone", before, tree(t, r.data))
	r.l.failAt = nil

	// Not running: as Use; nothing quit.
	if res, err := r.l.UseAfterQuit(acctA); err != nil || res.Quit || desktop.Active(r.data) != acctA {
		t.Fatalf("not running: %v %+v", err, res)
	}

	// A switch cut off: it is put back with the app quit by the user, and
	// the app is never quit for a switch then (nothing could open it again).
	r.cutBeforeFlip()
	f = r.running()
	res, err = r.l.UseAfterQuit(acctB)
	wantCode(t, "cut off", err, CodeSwitchUnrestored)
	if res.Quit || !isFile(f) {
		t.Fatalf("quit over a cut-off switch: %+v", res)
	}
	must(t, os.Remove(f))
	if undone, err := r.l.Recover(); err != nil || !undone {
		t.Fatalf("recover: %v %v", undone, err)
	}

	// Blank the same way.
	f = r.running()
	if res, err := r.l.BlankAfterQuit(); err != nil || !res.Quit || !res.Kept || isFile(f) || desktop.Active(r.data) != "" {
		t.Fatalf("blank: %v %+v", err, res)
	}
}
