package convdel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/desktop"
	"baton/internal/fsx"
	"baton/internal/recycle"
	"baton/internal/run"
	"baton/internal/winproc"
)

// Deleting a conversation: everything of it, in every config folder, goes
// to a stand-in Recycle Bin (BATON_RECYCLE) in the designed order, nothing
// else is touched, and a refusal changes nothing at all.

const (
	sid    = "1b2c3d4e-0000-4000-8000-000000000001" // the conversation
	other  = "1b2c3d4e-0000-4000-8000-000000000002" // another one: a /clear sibling
	copyID = "1b2c3d4e-0000-4000-8000-000000000003" // a copy of it
	acctA  = "a0000000-0000-4000-8000-0000000000a1"
	orgA   = "a0000000-0000-4000-8000-0000000000a2"
	keyA   = acctA + "/" + orgA
)

type world struct {
	t     *testing.T
	root  string
	paths app.Paths
	profA string // the profile of baton's account a
	profB string // of account b
	home  string // the user's own Claude Code folder
	proj  string // the project folder
	slug  string
	temp  string // what stands in for %TEMP%
	bin   string // the stand-in Recycle Bin
	data  string // the desktop app's data folder
	list  string // its list for account a
	alive map[int]bool
	desk  *desktop.Desk
}

// rigDepth is the longest path the tests here move, below the world's root
// and its backslash (a session folder's subagents in a profile: 94
// characters), with a little room to spare.
const rigDepth = 100

func newWorld(t *testing.T) *world {
	t.Helper()
	// Not t.TempDir, which adds the test's name: the rig's paths must stay
	// within what the Recycle Bin takes (MAX_PATH), which the stand-in bin
	// checks too, also for a long TEMP.
	root, err := os.MkdirTemp("", "cd")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	if n := len(utf16.Encode([]rune(longPath(root)))) + 1 + rigDepth; n >= 260 {
		t.Skipf("TEMP is too long for the stand-in Recycle Bin's MAX_PATH check (%d characters deep)", n)
	}
	w := &world{t: t, root: root, paths: app.Paths{Root: filepath.Join(root, "baton")},
		home: filepath.Join(root, "user", ".claude"), proj: filepath.Join(root, "Proj"),
		temp: filepath.Join(root, "Temp"), bin: filepath.Join(root, "bin"),
		data: filepath.Join(root, "Claude-Data", "claude_test"), alive: map[int]bool{}}
	w.profA, w.profB = w.paths.Profile("a"), w.paths.Profile("b")
	// Any folder name will do (baton never rebuilds it), and a short one
	// keeps the test's paths well within what the Recycle Bin takes.
	w.slug = "C--Proj"
	w.list = filepath.Join(w.data, "claude-code-sessions", acctA, orgA)
	for _, d := range []string{w.paths.RunsDir(), w.profA, w.profB, w.home, w.proj, w.temp, w.bin, w.list} {
		must(t, os.MkdirAll(d, 0o700))
	}
	w.writeJSON(filepath.Join(w.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctA})
	// The office hides this conversation and another one.
	w.writeJSON(w.paths.Config(), map[string]any{"office_view": map[string]any{"hidden_sessions": []string{other, sid}}})
	t.Setenv("BATON_RECYCLE", w.bin)
	t.Setenv("BATON_DESKTOP_DATA", w.data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	w.desk = desktop.New(w.paths, accounts.Registry{Accounts: []accounts.Account{
		{ID: "a", ProfileDir: w.profA, Priority: 20, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai",
			Email: "a@example.com", AccountUUID: acctA, OrgID: orgA},
		{ID: "b", ProfileDir: w.profB, Priority: 10, Enabled: true, LoggedIn: true, AuthMethod: "api_key"},
	}})
	w.desk.UserHome = w.home
	return w
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// longPath is p with 8.3 short names spelled out, as the bin measures it.
func longPath(p string) string {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}

// req asks about conversation id, picked in config folder where.
func (w *world) req(id, where string) Request {
	return Request{ID: id, Where: where, Paths: w.paths,
		Dirs: []Dir{{Where: "a", Path: w.profA}, {Where: "b", Path: w.profB}, {Where: "claude-code", Path: w.home}},
		Desk: w.desk, Bin: recycle.New(), TempRoot: w.temp,
		Alive: func(pid int, _ time.Time) bool { return w.alive[pid] }}
}

func (w *world) write(path, data string) string {
	w.t.Helper()
	must(w.t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(w.t, os.WriteFile(path, []byte(data), 0o600))
	return path
}

func (w *world) writeJSON(path string, v any) string {
	w.t.Helper()
	b, err := json.Marshal(v)
	must(w.t, err)
	return w.write(path, string(b))
}

// conv writes conversation id in config folder dir and returns its file.
func (w *world) conv(dir, id string, rows ...string) string {
	w.t.Helper()
	cwd := strconv.Quote(w.proj)
	text := `{"type":"user","cwd":` + cwd + `,"sessionId":"` + id + `","timestamp":"2026-11-01T10:00:00.000Z","message":{"role":"user","content":"fix the login bug"}}` + "\n" +
		`{"type":"assistant","cwd":` + cwd + `,"timestamp":"2026-11-01T10:05:00.000Z","message":{"role":"assistant","content":"done"}}` + "\n"
	for _, r := range rows {
		text += r + "\n"
	}
	return w.write(filepath.Join(dir, "projects", w.slug, id+".jsonl"), text)
}

// link gives file a second name in the user's own folder, as the desktop
// side does.
func (w *world) link(file string) string {
	w.t.Helper()
	h := filepath.Join(w.home, "projects", filepath.Base(filepath.Dir(file)), filepath.Base(file))
	must(w.t, os.MkdirAll(filepath.Dir(h), 0o700))
	must(w.t, os.Link(file, h))
	return h
}

func (w *world) journal(runID string, j run.Journal) string {
	w.t.Helper()
	dir := filepath.Join(w.paths.RunsDir(), runID)
	must(w.t, os.MkdirAll(dir, 0o700))
	j.RunID = runID
	must(w.t, run.SaveJournal(dir, j))
	return dir
}

// junction makes link a junction to target, or says it can't.
func junction(t *testing.T, link, target string) bool {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(link), 0o700))
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Logf("no junction (%v): %s", err, out)
		return false
	}
	return true
}

// files is everything under the test's folder and what each file holds;
// "<dir>" for a folder, "<link>" for a junction. Locks and the deleting
// marker are left out: they come and go.
func (w *world) files() map[string]string {
	w.t.Helper()
	out := map[string]string{}
	skip := filepath.Join(w.paths.Root, "deleting")
	_ = filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case strings.EqualFold(p, skip):
			return filepath.SkipDir
		case strings.HasSuffix(p, ".lock"):
		case d.IsDir():
			out[p] = "<dir>"
		case d.Type()&fs.ModeIrregular != 0 || d.Type()&fs.ModeSymlink != 0:
			out[p] = "<link>"
		default:
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return nil
	})
	return out
}

func (w *world) unchanged(before map[string]string, what string) {
	w.t.Helper()
	after := w.files()
	if maps.Equal(before, after) {
		return
	}
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			w.t.Errorf("%s: %s changed or went", what, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			w.t.Errorf("%s: %s turned up", what, p)
		}
	}
	w.t.FailNow()
}

// binned are the paths the stand-in bin took, in order.
func (w *world) binned() []string {
	w.t.Helper()
	m, err := recycle.ReadManifest(w.bin)
	must(w.t, err)
	var out []string
	for _, e := range m {
		out = append(out, e.From)
	}
	return out
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return fmt.Sprintf("not an *Error: %v", err)
}

// refused checks that Look and Delete both refuse with want first, and
// that neither changes anything.
func (w *world) refused(r Request, want Blocker) {
	w.t.Helper()
	before := w.files()
	p, err := Look(r)
	if err != nil || p.OK || len(p.Blockers) == 0 || p.Blockers[0] != want {
		w.t.Fatalf("look: %+v, %v; want %+v first", p.Blockers, err, want)
	}
	w.unchanged(before, "a look")
	res, err := Delete(r)
	var e *Error
	if !errors.As(err, &e) || e.Code != want.Code || len(e.Blockers) == 0 || e.Blockers[0] != want || res.Moved != 0 {
		w.t.Fatalf("delete: %+v, %v; want %+v", res, err, want)
	}
	w.unchanged(before, "a refused delete")
	if len(w.binned()) != 0 {
		w.t.Fatal("something went to the bin")
	}
}

// A conversation baton made, spread over two profiles and the user's own
// folder: every name of it, what Claude Code keeps next to it and by its id,
// its temporary folder and its ended run go to the bin, in order, and
// nothing else; a junction is left with what it points at. The office lets
// go of it before anything moves, and no longer hides it afterwards.
func TestDeleteASpreadConversation(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	h := w.link(a)
	inA := filepath.Join(w.profA, "projects", w.slug)
	inB := filepath.Join(w.profB, "projects", w.slug)
	tip := w.write(filepath.Join(inA, sid+".ccr-tip.json"), `{}`)
	compact := w.write(filepath.Join(inA, sid+".jsonl.compact.tmp.123"), "half")
	session := filepath.Join(inB, sid)
	w.write(filepath.Join(session, "subagents", "agent-1.jsonl"), "{}\n")
	outputs := w.write(filepath.Join(session, "tool-results", "t1.txt"), "a large output")
	history := w.write(filepath.Join(w.profB, "file-history", sid, "v1@1"), "old")
	env := w.write(filepath.Join(w.profA, "session-env", sid, "hook-1.sh"), "set x=1")
	tasks := w.write(filepath.Join(w.profB, "tasks", sid, "1.json"), "{}")
	debug := w.write(filepath.Join(w.home, "debug", sid+".txt"), "log")
	temp := filepath.Join(w.temp, "claude", "C--Proj", sid)
	w.write(filepath.Join(temp, "scratchpad", "notes.txt"), "notes")
	ended := w.journal("r1", run.Journal{SessionID: sid, State: run.StateDone, SupervisorPID: 4001, TranscriptPath: a})
	w.write(filepath.Join(ended, "events.jsonl"), `{"last_assistant_message":"done"}`+"\n")

	// Kept: the app's marker next to it, the folder's memory, a /clear
	// sibling, a copy (it points at the outputs), plans, prompt history,
	// another conversation's task list and temporary folder, a run of
	// another conversation that mentions it, and a profile's junction.
	kept := []string{
		w.write(filepath.Join(inA, sid+".desktop-released.json"), `{"v":1}`),
		w.write(filepath.Join(inA, "memory", "MEMORY.md"), "remember"),
		w.conv(w.profA, other),
		w.conv(w.profA, copyID, string(must1(json.Marshal(map[string]any{"type": "user", "toolUseResult": outputs})))),
		w.write(filepath.Join(w.profA, "plans", "brave-otter.md"), "plan"),
		w.write(filepath.Join(w.home, "history.jsonl"), `{"sessionId":"`+sid+`"}`+"\n"),
		w.write(filepath.Join(w.home, "tasks", other, "1.json"), "{}"),
		w.write(filepath.Join(w.temp, "claude", "C--Proj", other, "x.txt"), "x"),
		w.write(filepath.Join(w.journal("r2", run.Journal{SessionID: other, State: run.StateDone}), "events.jsonl"), sid),
	}
	links := 0
	target := filepath.Join(w.root, "elsewhere")
	kept = append(kept, w.write(filepath.Join(target, "keep.txt"), "mine"))
	envLink := filepath.Join(w.home, "session-env", sid)
	if junction(t, envLink, target) {
		links++
	}
	inner := filepath.Join(w.root, "elsewhere2")
	kept = append(kept, w.write(filepath.Join(inner, "keep.txt"), "mine too"))
	junction(t, filepath.Join(session, "linked"), inner) // moves with the folder, as a link
	agents := filepath.Join(w.profA, "agents")
	kept = append(kept, w.write(filepath.Join(w.home, "agents", "helper.md"), "agent"))
	junction(t, agents, filepath.Join(w.home, "agents"))
	keptData := map[string]string{}
	for _, p := range kept {
		b, err := os.ReadFile(p)
		must(t, err)
		keptData[p] = string(b)
	}

	before := w.files()
	p, err := Look(w.req(sid, "a"))
	must(t, err)
	w.unchanged(before, "a look")
	want := []string{h, a, tip, compact, session, filepath.Dir(env), filepath.Dir(history), filepath.Dir(tasks), debug, temp, ended}
	if !p.OK || p.MadeIn != "baton" || p.Title != "fix the login bug" || p.Cwd != w.proj || !slices.Equal(p.Paths(), want) {
		t.Fatalf("plan: %+v\npaths %q\nwant  %q", p, p.Paths(), want)
	}
	if p.Items.Files != 4 || p.Items.Folders != 5 || p.Items.Runs != 1 || p.Items.Bytes == 0 {
		t.Fatalf("counts: %+v", p.Items)
	}
	wantNotes := []Note{{Code: NoteCopies, N: 1}}
	if links > 0 {
		wantNotes = append(wantNotes, Note{Code: NoteLinksKept, N: links})
	}
	if !slices.Equal(p.Notes, wantNotes) {
		t.Fatalf("notes: %+v, want %+v", p.Notes, wantNotes)
	}
	// The window gets no file or folder of the conversation's.
	js := string(must1(json.Marshal(p)))
	for _, d := range []string{w.profA, w.profB, w.home, w.temp, w.paths.RunsDir()} {
		if esc := string(must1(json.Marshal(d))); strings.Contains(js, strings.Trim(esc, `"`)) {
			t.Fatalf("the plan names %s: %s", d, js)
		}
	}
	if strings.Contains(js, "jsonl") || !strings.Contains(js, `"made_in":"baton"`) {
		t.Fatalf("plan: %s", js)
	}

	// The office lets go first; the bin gets everything in one call.
	r := w.req(sid, "a")
	var steps []string
	r.Office = func(id string) { steps = append(steps, "office "+id) }
	r.Bin = &spyBin{Bin: r.Bin, call: func(paths []string) { steps = append(steps, fmt.Sprintf("recycle %d", len(paths))) }}
	res, err := Delete(r)
	must(t, err)
	if res.Moved != len(want) || res.Warning != "" || len(res.DesktopOff) != 0 {
		t.Fatalf("result: %+v", res)
	}
	if !slices.Equal(steps, []string{"office " + sid, fmt.Sprintf("recycle %d", len(want))}) {
		t.Fatalf("steps: %q", steps)
	}
	if got := w.binned(); !slices.Equal(got, want) {
		t.Fatalf("binned %q\nwant   %q", got, want)
	}
	for _, p := range want {
		if exists(p) {
			t.Fatalf("%s stayed", p)
		}
	}
	for p, b := range keptData {
		if got, err := os.ReadFile(p); err != nil || string(got) != b {
			t.Fatalf("%s changed or went", p)
		}
	}
	for _, l := range []string{envLink, agents} {
		if fi, err := os.Lstat(l); links > 0 && (err != nil || !fsx.IsReparsePoint(fi)) {
			t.Fatalf("the junction %s went", l)
		}
	}
	if v := w.hidden(); !slices.Equal(v, []string{other}) {
		t.Fatalf("hidden in the office: %q", v)
	}
	if _, err := Look(w.req(sid, "a")); codeOf(err) != CodeGone {
		t.Fatalf("look afterwards: %v", err)
	}
}

func must1[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// hidden are the sessions the office hides.
func (w *world) hidden() []string {
	w.t.Helper()
	var c struct {
		View struct {
			Hidden []string `json:"hidden_sessions"`
		} `json:"office_view"`
	}
	must(w.t, fsx.ReadJSON(w.paths.Config(), &c))
	return c.View.Hidden
}

// spyBin sees each Recycle call before the bin does.
type spyBin struct {
	recycle.Bin
	call func(paths []string)
}

func (b *spyBin) Recycle(paths []string) (int, error) {
	b.call(paths)
	return b.Bin.Recycle(paths)
}

// failBin moves the first at items, then fails as an item in use (once).
type failBin struct {
	recycle.Bin
	at int
}

func (b *failBin) Recycle(paths []string) (int, error) {
	if b.at < 0 || b.at >= len(paths) {
		return b.Bin.Recycle(paths)
	}
	at := b.at
	b.at = -1
	moved, err := b.Bin.Recycle(paths[:at])
	if err != nil {
		return moved, err
	}
	return moved, &recycle.Error{Path: paths[at], Drive: "C:", Moved: moved, Err: recycle.ErrInUse}
}

// noBin is a drive whose Recycle Bin is off.
type noBin struct{}

func (noBin) Preflight(paths []string) error {
	return &recycle.Error{Path: paths[0], Drive: "C:", Err: fmt.Errorf("%w: it is set to delete at once", recycle.ErrUnavailable)}
}

func (b noBin) Recycle(paths []string) (int, error) { return 0, b.Preflight(paths) }

func (noBin) Holds(string) int { return 0 }

// Each refusal, by Look and by Delete, changes nothing.
func TestRefusals(t *testing.T) {
	setup := func(t *testing.T) (*world, string) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		w.write(filepath.Join(w.profA, "file-history", sid, "v1"), "old")
		return w, a
	}
	t.Run("another delete runs", func(t *testing.T) {
		w, _ := setup(t)
		release, err := run.MarkDeleting(w.paths.Root, sid)
		must(t, err)
		defer release()
		w.refused(w.req(sid, "a"), Blocker{Code: CodeDeleting})
	})
	t.Run("a baton session waits to go on with it", func(t *testing.T) {
		w, _ := setup(t)
		w.journal("r9", run.Journal{SessionID: sid, State: run.StateWaiting, SupervisorPID: 4242})
		w.alive[4242] = true
		w.refused(w.req(sid, "a"), Blocker{Code: CodeInBaton, RunID: "r9"})
	})
	t.Run("a baton session's claude has it open", func(t *testing.T) {
		w, _ := setup(t)
		w.registry(w.profA, sid, "sdk-cli", false)
		w.journal("r7", run.Journal{SessionID: other, State: run.StateRunning, SupervisorPID: 4244, ChildPID: os.Getpid()})
		w.alive[4244] = true
		w.refused(w.req(sid, "a"), Blocker{Code: CodeInBaton, RunID: "r7"})
	})
	t.Run("a copy of it is starting", func(t *testing.T) {
		w, a := setup(t)
		w.journal("r8", run.Journal{SessionID: copyID, State: run.StateRunning, SupervisorPID: 4243, ForkOf: strings.ToUpper(a)})
		w.alive[4243] = true
		w.refused(w.req(sid, "a"), Blocker{Code: CodeCopyStarting, RunID: "r8"})
	})
	t.Run("a Claude Code has it open", func(t *testing.T) {
		w, _ := setup(t)
		w.registry(w.home, sid, "claude-vscode", false)
		w.refused(w.req(sid, "a"), Blocker{Code: CodeOpenElsewhere, Holder: "claude-vscode"})
	})
	t.Run("a registry entry can't be read", func(t *testing.T) {
		w, _ := setup(t)
		w.registry(w.home, sid, "", true)
		w.refused(w.req(sid, "a"), Blocker{Code: CodeCannotCheck})
	})
	t.Run("a name outside Claude Code's folders", func(t *testing.T) {
		w, a := setup(t)
		must(t, os.MkdirAll(filepath.Join(w.root, "elsewhere"), 0o700))
		must(t, os.Link(a, filepath.Join(w.root, "elsewhere", "saved.jsonl")))
		w.refused(w.req(sid, "a"), Blocker{Code: CodeOtherName})
	})
	t.Run("a path too long for the bin", func(t *testing.T) {
		w, _ := setup(t)
		deep := filepath.Join(w.profB, "projects", w.slug, sid, "subagents")
		for len(deep) < 300 {
			deep = filepath.Join(deep, strings.Repeat("d", 40))
		}
		w.write(filepath.Join(deep, "agent.jsonl"), "{}")
		w.refused(w.req(sid, "a"), Blocker{Code: CodePathTooLong})
	})
	t.Run("no Recycle Bin", func(t *testing.T) {
		w, _ := setup(t)
		r := w.req(sid, "a")
		r.Bin = noBin{}
		w.refused(r, Blocker{Code: CodeNoRecycleBin, Drive: "C:"})
	})
	t.Run("the desktop app's own", func(t *testing.T) {
		w, _ := setup(t)
		w.writeJSON(filepath.Join(w.list, "local_d0000000-0000-4000-8000-0000000000d0.json"),
			map[string]any{"sessionId": "local_d0000000-0000-4000-8000-0000000000d0", "cliSessionId": sid, "cwd": w.proj})
		w.refused(w.req(sid, "a"), Blocker{Code: CodeDesktopApp, Detail: "own"})
	})
	t.Run("the running desktop app shows it", func(t *testing.T) {
		w, a := setup(t)
		w.show(a)
		t.Setenv("BATON_DESKTOP_RUNNING", "1") // signed in to a's list
		must(t, w.desk.Reconcile())
		w.refused(w.req(sid, "a"), Blocker{Code: CodeDesktopBusy})
	})
	t.Run("another session's record waits to move on", func(t *testing.T) {
		w, a := setup(t)
		w.show(a)
		// The session went on in another conversation (/clear) while the
		// running app holds the list: its record still points here.
		w.conv(w.profA, other)
		ix := filepath.Join(w.paths.Root, "desktop.json")
		var raw map[string]any
		must(t, fsx.ReadJSON(ix, &raw))
		th := raw["threads"].([]any)[0].(map[string]any)
		th["session"], th["prior"], th["transcript"] = other, []string{sid}, filepath.Join(w.profA, "projects", w.slug, other+".jsonl")
		w.writeJSON(ix, raw)
		t.Setenv("BATON_DESKTOP_RUNNING", "1")
		must(t, w.desk.Reconcile())
		w.refused(w.req(sid, "a"), Blocker{Code: CodeDesktopWaiting})
	})
	t.Run("a file in use", func(t *testing.T) {
		w, a := setup(t)
		f, err := os.Open(a) // no delete sharing: the bin can't move it
		must(t, err)
		defer f.Close()
		before := w.files()
		_, err = Delete(w.req(sid, "a"))
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeInUse || e.Left != 2 {
			t.Fatalf("delete: %v", err)
		}
		w.unchanged(before, "a file in use")
	})
	t.Run("not there", func(t *testing.T) {
		w, _ := setup(t)
		before := w.files()
		if _, err := Look(w.req(other, "a")); codeOf(err) != CodeGone {
			t.Fatalf("look: %v", err)
		}
		if _, err := Delete(w.req(other, "")); codeOf(err) != CodeGone {
			t.Fatalf("delete: %v", err)
		}
		w.unchanged(before, "gone")
	})
	t.Run("a bad request", func(t *testing.T) {
		w, _ := setup(t)
		before := w.files()
		for _, r := range []Request{w.req("nope", "a"), w.req(sid+"x", "a"), w.req(sid, "z")} {
			if _, err := Look(r); codeOf(err) != CodeBadSession {
				t.Fatalf("look %q %q: %v", r.ID, r.Where, err)
			}
			if _, err := Delete(r); codeOf(err) != CodeBadSession {
				t.Fatalf("delete %q %q: %v", r.ID, r.Where, err)
			}
		}
		w.unchanged(before, "a bad request")
	})
}

// registry registers a Claude Code with conversation id open in config
// folder dir: this test's own process stands in for it. unreadable: an
// entry caught mid-write that stays so.
func (w *world) registry(dir, id, entrypoint string, unreadable bool) {
	w.t.Helper()
	p := filepath.Join(dir, "sessions", strconv.Itoa(os.Getpid())+".json")
	if unreadable {
		w.write(p, `{"pid":`)
		return
	}
	created := winproc.ProcessCreated(os.Getpid())
	w.writeJSON(p, map[string]any{"pid": os.Getpid(), "sessionId": id, "cwd": w.proj, "kind": "interactive",
		"status": "idle", "entrypoint": entrypoint, "startedAt": created.UnixMilli() + 500})
}

// show shows conversation file in the desktop app, on account a's list.
func (w *world) show(file string) {
	w.t.Helper()
	res, err := w.desk.Apply(desktop.Conversation{ID: sid, Transcript: file}, []string{keyA})
	must(w.t, err)
	if !slices.Equal(res.Added, []string{keyA}) {
		w.t.Fatalf("show: %+v", res)
	}
}

// records are the records in account a's list.
func (w *world) records() []string {
	w.t.Helper()
	ents, err := os.ReadDir(w.list)
	must(w.t, err)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// A conversation baton shows in the desktop app: the delete takes baton's
// record off the app's list first (remembering it, should the app write it
// back), then the file goes; the app's own record, tombstone and marker of
// other conversations stay.
func TestDeleteTakesItOffTheDesktopApp(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.show(a)
	h := filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
	if !exists(h) || len(w.records()) != 1 {
		t.Fatalf("setup: second name %v, records %q", exists(h), w.records())
	}
	ours := w.records()[0]
	apps := []string{
		w.writeJSON(filepath.Join(w.list, "local_d0000000-0000-4000-8000-0000000000d0.json"),
			map[string]any{"sessionId": "local_d0000000-0000-4000-8000-0000000000d0", "cliSessionId": other, "cwd": w.proj}),
		w.write(filepath.Join(w.list, "deleted_"+other), "1800000005000"),
		w.write(filepath.Join(w.home, "projects", w.slug, other+".desktop-released.json"), `{"v":1}`),
	}
	appData := map[string]string{}
	for _, p := range apps {
		appData[p] = string(must1(os.ReadFile(p)))
	}

	p, err := Look(w.req(sid, "a"))
	must(t, err)
	if !p.OK || !slices.Equal(p.DesktopOff, []string{"a"}) || !slices.Equal(p.Paths(), []string{h, a}) {
		t.Fatalf("plan: %+v %q", p, p.Paths())
	}
	res, err := Delete(w.req(sid, "a"))
	must(t, err)
	if !slices.Equal(res.DesktopOff, []string{"a"}) || res.Warning != "" {
		t.Fatalf("result: %+v", res)
	}
	if exists(filepath.Join(w.list, ours)) || exists(a) || exists(h) {
		t.Fatalf("baton's record (%v) or a name of the file (%v, %v) stayed; binned %q", exists(filepath.Join(w.list, ours)), exists(a), exists(h), w.binned())
	}
	if got := w.binned(); !slices.Contains(got, a) {
		t.Fatalf("binned: %q", got)
	}
	for p, b := range appData {
		if got, err := os.ReadFile(p); err != nil || string(got) != b {
			t.Fatalf("the app's %s changed or went", p)
		}
	}
	ix, err := desktop.LoadIndex(filepath.Join(w.paths.Root, "desktop.json"))
	must(t, err)
	knows := slices.ContainsFunc(ix.Gone, func(g desktop.Gone) bool { return slices.Contains(g.Cli, sid) })
	if len(ix.Threads) != 0 || !knows {
		t.Fatalf("index: %+v", ix)
	}
}

// A delete that stopped half way leaves the rest, off the desktop app; doing
// it again finishes it: what is left is found again, also once every name
// of the file went.
func TestPartlyThenAgain(t *testing.T) {
	for _, at := range []int{1, 3} {
		t.Run(fmt.Sprint("stopped at item ", at), func(t *testing.T) {
			w := newWorld(t)
			a := w.conv(w.profA, sid)
			w.show(a)
			h := filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
			session := filepath.Dir(w.write(filepath.Join(w.profB, "projects", w.slug, sid, "subagents", "agent-1.jsonl"), "{}"))
			history := filepath.Dir(w.write(filepath.Join(w.profB, "file-history", sid, "v1"), "old"))
			temp := filepath.Dir(w.write(filepath.Join(w.temp, "claude", "C--Proj", sid, "notes.txt"), "notes"))
			all := []string{h, a, filepath.Dir(session), history, temp}
			p, err := Look(w.req(sid, "a"))
			must(t, err)
			if !slices.Equal(p.Paths(), all) {
				t.Fatalf("paths %q\nwant  %q", p.Paths(), all)
			}

			r := w.req(sid, "a")
			r.Bin = &failBin{Bin: r.Bin, at: at}
			_, err = Delete(r)
			var e *Error
			// The second name went with the desktop record, before the bin.
			moved := at - 1
			if !errors.As(err, &e) || e.Code != CodePartly && moved > 0 || e.Left != len(all)-at || !slices.Equal(e.DesktopOff, []string{"a"}) {
				t.Fatalf("delete: %v (%+v)", err, e)
			}
			if moved == 0 && e.Code != CodeInUse {
				t.Fatalf("nothing moved, yet %s", e.Code)
			}
			if len(w.records()) != 0 {
				t.Fatalf("records: %q", w.records())
			}
			if v := w.hidden(); !slices.Contains(v, sid) {
				t.Fatalf("unhidden before it went: %q", v)
			}

			p, err = Look(w.req(sid, "a"))
			must(t, err)
			if !p.OK || p.MadeIn != "baton" || !slices.Equal(p.Paths(), all[at:]) {
				t.Fatalf("again: %+v %q", p, p.Paths())
			}
			if _, err := Delete(w.req(sid, "a")); err != nil {
				t.Fatal(err)
			}
			for _, p := range all {
				if exists(p) {
					t.Fatalf("%s stayed", p)
				}
			}
			if _, err := Look(w.req(sid, "a")); codeOf(err) != CodeGone {
				t.Fatalf("look afterwards: %v", err)
			}
			if v := w.hidden(); !slices.Equal(v, []string{other}) {
				t.Fatalf("hidden: %q", v)
			}
		})
	}
}

// A second name in the user's own folder that outlived the profile's name
// (Claude Code's cleanup, or an earlier delete) is a conversation of its
// own: made outside baton as far as anyone can tell, and deleted as one,
// also while the desktop app shows it.
func TestALoneSecondName(t *testing.T) {
	for _, shown := range []bool{false, true} {
		t.Run(fmt.Sprint("shown ", shown), func(t *testing.T) {
			w := newWorld(t)
			a := w.conv(w.profA, sid)
			var h string
			if shown {
				w.show(a)
				h = filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
			} else {
				h = w.link(a)
			}
			must(t, os.Remove(a))
			p, err := Look(w.req(sid, "a"))
			must(t, err)
			if !p.OK || p.MadeIn != "outside" || !slices.Equal(p.Paths(), []string{h}) {
				t.Fatalf("plan: %+v %q", p, p.Paths())
			}
			if _, err := Delete(w.req(sid, "claude-code")); err != nil {
				t.Fatal(err)
			}
			if got := w.binned(); !slices.Equal(got, []string{h}) {
				t.Fatalf("binned: %q", got)
			}
			if len(w.records()) != 0 {
				t.Fatalf("records: %q", w.records())
			}
		})
	}
}

// A conversation that went on apart in several places has several files by
// its name (a second name of baton's became a file of its own, and each
// side added turns; another profile has one too). Every one of them goes,
// by each of its names, the user's own folder first; the plan lists them,
// the one picked first, then the newest. Shown in the desktop app, baton's
// record goes off its list too.
func TestDeleteEveryFileOfIt(t *testing.T) {
	for _, shown := range []bool{false, true} {
		t.Run(fmt.Sprint("shown ", shown), func(t *testing.T) {
			w := newWorld(t)
			a := w.conv(w.profA, sid)
			h := filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
			if shown {
				w.show(a)
			} else {
				w.link(a)
			}
			// Something writes the second name anew: a file of its own now,
			// which the desktop app goes on in.
			must(t, os.Remove(h))
			w.write(h, string(must1(os.ReadFile(a)))+`{"type":"user","message":{"role":"user","content":"went on in the app"}}`+"\n")
			b := w.conv(w.profB, sid, `{"type":"user","message":{"role":"user","content":"another start"}}`)
			old := time.Now().Add(-time.Hour)
			must(t, os.Chtimes(b, old, old))
			// What is kept by its id is shared by all of them.
			tasks := w.write(filepath.Join(w.home, "tasks", sid, "1.json"), "{}")
			w.conv(w.profA, other) // a conversation of one file
			if shown {
				// The desktop side notes the split at its next tidying, which a
				// look does first.
				must1(Look(w.req(sid, "a")))
			}

			before := w.files()
			p, err := Look(w.req(sid, "a"))
			must(t, err)
			w.unchanged(before, "a look")
			if !p.OK || p.MadeIn != "baton" || p.Title != "fix the login bug" {
				t.Fatalf("plan: %+v", p)
			}
			if want := []string{h, a, b, filepath.Dir(tasks)}; !slices.Equal(p.Paths(), want) {
				t.Fatalf("paths %q\nwant  %q", p.Paths(), want)
			}
			size := func(f string) int64 { return must1(os.Stat(f)).Size() }
			var where [][]string
			for _, f := range p.Files {
				where = append(where, f.Where)
			}
			if len(p.Files) != 3 || !slices.EqualFunc(where, [][]string{{"a"}, {"claude-code"}, {"b"}}, slices.Equal) ||
				p.Files[0].Bytes != size(a) || p.Files[1].Bytes != size(h) || p.Files[2].Bytes != size(b) ||
				!p.Files[2].Modified.Equal(must1(os.Stat(b)).ModTime()) {
				t.Fatalf("files: %+v", p.Files)
			}
			if p.Items.Files != 3 || p.Items.Folders != 1 || p.Items.Bytes != size(a)+size(h)+size(b)+2 {
				t.Fatalf("counts: %+v", p.Items)
			}
			if shown && !slices.Equal(p.DesktopOff, []string{"a"}) || !shown && len(p.DesktopOff) != 0 {
				t.Fatalf("off the desktop app: %q", p.DesktopOff)
			}
			// One file only: no list of files.
			if p, err := Look(w.req(other, "a")); err != nil || !p.OK || len(p.Files) != 0 || p.Items.Files != 1 {
				t.Fatalf("a conversation of one file: %+v %v", p, err)
			}

			res, err := Delete(w.req(sid, "a"))
			must(t, err)
			if res.Moved != 4 || res.Warning != "" {
				t.Fatalf("result: %+v", res)
			}
			if got := w.binned(); !slices.Equal(got, []string{h, a, b, filepath.Dir(tasks)}) {
				t.Fatalf("binned %q", got)
			}
			if shown && len(w.records()) != 0 {
				t.Fatalf("records: %q", w.records())
			}
			if _, err := Look(w.req(sid, "")); codeOf(err) != CodeGone {
				t.Fatalf("look afterwards: %v", err)
			}
		})
	}
}

// A baton session that starts on it once the delete has looked (after it
// took it off the desktop app) stops the delete before anything moves; the
// error says it is off the app.
func TestStartedMeanwhile(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.show(a)
	r := w.req(sid, "a")
	r.Office = func(string) {
		w.journal("r5", run.Journal{SessionID: sid, State: run.StateRunning, SupervisorPID: 4245})
		w.alive[4245] = true
	}
	_, err := Delete(r)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInBaton || e.Blockers[0].RunID != "r5" || !slices.Equal(e.DesktopOff, []string{"a"}) {
		t.Fatalf("delete: %v (%+v)", err, e)
	}
	if !exists(a) || len(w.binned()) != 0 || len(w.records()) != 0 {
		t.Fatalf("file %v, binned %q, records %q", exists(a), w.binned(), w.records())
	}
}

// Conversations that went on from this one and point at its saved tool
// outputs are counted, in every config folder and once each; when there is
// too much to read, the plan says it did not read it all.
func TestCopiesNote(t *testing.T) {
	w := newWorld(t)
	w.conv(w.profA, sid)
	outputs := w.write(filepath.Join(w.profA, "projects", w.slug, sid, "tool-results", "t1.txt"), "a large output")
	row := string(must1(json.Marshal(map[string]any{"type": "user", "toolUseResult": outputs})))
	c := w.conv(w.profA, copyID, row)
	w.link(c) // shown in the desktop app: one copy, two names
	w.conv(w.profB, other, `{"type":"user","message":{"content":"`+filepath.ToSlash(outputs)+`"}}`)
	w.conv(w.profA, "1b2c3d4e-0000-4000-8000-000000000004") // one that does not
	p, err := Look(w.req(sid, "a"))
	must(t, err)
	if !slices.Equal(p.Notes, []Note{{Code: NoteCopies, N: 2}}) {
		t.Fatalf("notes: %+v", p.Notes)
	}
	was := copiesRead
	defer func() { copiesRead = was }()
	copiesRead = 10
	p, err = Look(w.req(sid, "a"))
	must(t, err)
	if !slices.Equal(p.Notes, []Note{{Code: NoteNotFullyChecked}}) || !p.OK {
		t.Fatalf("notes: %+v", p.Notes)
	}
}

// A Claude Code that opened the conversation while it went writes a new
// file: the delete says so.
func TestReopenedMeanwhile(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	r := w.req(sid, "a")
	r.Bin = &afterBin{Bin: r.Bin, after: func() { w.conv(w.profA, sid) }}
	res, err := Delete(r)
	must(t, err)
	if res.Warning != WarnReopened || res.Moved != 1 || !exists(a) {
		t.Fatalf("result: %+v", res)
	}
}

// afterBin runs after once the bin is done.
type afterBin struct {
	recycle.Bin
	after func()
}

func (b *afterBin) Recycle(paths []string) (int, error) {
	n, err := b.Bin.Recycle(paths)
	b.after()
	return n, err
}
