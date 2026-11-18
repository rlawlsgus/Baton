package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/convdel"
	"baton/internal/recycle"
	"baton/internal/run"
	"baton/internal/winproc"
)

// Deleting a conversation from the window: the plan the dialog shows, the
// delete, what it refuses and how it answers. Everything is in temporary
// folders: baton's data, the user's own Claude Code folder, Claude Code's
// temporary folder, the desktop app's data and a stand-in Recycle Bin.

const (
	delSID   = "de1e7e00-0000-4000-8000-000000000001" // the conversation
	delOther = "de1e7e00-0000-4000-8000-000000000002" // another one in the same folder
	delAcct  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	delOrg   = "a0000000-0000-4000-8000-00000000000a"
	delRunID = "20261113-100000-dddddd"
)

type delRig struct {
	*rig
	prof  string // account a's profile
	home  string // the user's own Claude Code folder
	proj  string // the project folder
	file  string // the conversation, in a's profile
	twin  string // its second name in the user's own folder
	keep  string // the other conversation
	temp  string // what stands in for %TEMP%
	bin   string // the stand-in Recycle Bin
	data  string // the desktop app's data folder
	place string // its list for account a
}

// newDelRig is a window with account a and a conversation of it that ended
// in a baton session: a second name in the user's own folder (as the desktop
// side makes), /rewind checkpoints, a temporary folder and the run. The
// office hides it and another one.
func newDelRig(t *testing.T) *delRig {
	t.Helper()
	r := newRig(t)
	t.Setenv("CLAUDECODE", "")
	out := t.TempDir()
	d := &delRig{rig: r, prof: r.paths.Profile("a"), home: os.Getenv("CLAUDE_CONFIG_DIR"), proj: t.TempDir(),
		temp: filepath.Join(out, "Temp"), bin: filepath.Join(out, "bin"), data: filepath.Join(out, "Claude-Data", "claude_test")}
	d.place = filepath.Join(d.data, "claude-code-sessions", delAcct, delOrg)
	for _, dir := range []string{d.prof, d.home, d.temp, d.bin, d.place} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	d.write(filepath.Join(d.data, "config.json"), `{"lastKnownAccountUuid":"`+delAcct+`"}`)
	t.Setenv("BATON_RECYCLE", d.bin)
	t.Setenv("BATON_DESKTOP_DATA", d.data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	r.srv.delTemp = d.temp
	if err := accounts.Save(r.paths.Accounts(), accounts.Registry{Accounts: []accounts.Account{{ID: "a", ProfileDir: d.prof,
		Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "a@example.com", AccountUUID: delAcct, OrgID: delOrg}}}); err != nil {
		t.Fatal(err)
	}
	row := func(text string) string {
		return `{"type":"user","cwd":` + strconv.Quote(d.proj) + `,"message":{"role":"user","content":"` + text + `"}}` + "\n"
	}
	d.file = d.write(filepath.Join(d.prof, "projects", "C--proj", delSID+".jsonl"), row("fix the login bug"))
	d.keep = d.write(filepath.Join(d.prof, "projects", "C--proj", delOther+".jsonl"), row("tidy the docs"))
	d.twin = filepath.Join(d.home, "projects", "C--proj", delSID+".jsonl")
	_ = os.MkdirAll(filepath.Dir(d.twin), 0o700)
	if err := os.Link(d.file, d.twin); err != nil {
		t.Fatal(err)
	}
	d.write(filepath.Join(d.prof, "file-history", delSID, "a.txt@v1"), "before")
	d.write(filepath.Join(d.temp, "claude", "C--proj", delSID, "tasks", "out.txt"), "output")
	dir := r.paths.Run(delRunID)
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: delRunID, SupervisorPID: 999999, SupervisorCreated: time.Now().Add(-time.Hour),
		State: run.StateDone, Account: "a", Cwd: d.proj, SessionID: delSID, TranscriptPath: d.file, Started: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.OfficeView = &config.OfficeView{HiddenSessions: []string{delOther, delSID}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.login()
	// Before the settings above are put back and these folders go.
	t.Cleanup(r.srv.waitBackground)
	return d
}

func (d *delRig) write(path, data string) string {
	d.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		d.t.Fatal(err)
	}
	return path
}

func (d *delRig) plan(id, where string) (int, string) {
	d.t.Helper()
	return d.do("GET", "/api/sessions/delete-plan?session_id="+id+"&where="+where, "", nil)
}

func (d *delRig) del(id, where string) (int, string) {
	d.t.Helper()
	b, _ := json.Marshal(map[string]string{"session_id": id, "where": where})
	return d.do("POST", "/api/sessions/delete", string(b), nil)
}

// binned are the paths the stand-in bin took, in order.
func (d *delRig) binned() []string {
	d.t.Helper()
	m, err := recycle.ReadManifest(d.bin)
	if err != nil {
		d.t.Fatal(err)
	}
	var out []string
	for _, e := range m {
		out = append(out, e.From)
	}
	return out
}

func (d *delRig) recent() []recentView {
	d.t.Helper()
	_, body := d.do("GET", "/api/state", "", nil)
	var st stateView
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		d.t.Fatalf("state: %v %s", err, body)
	}
	return st.Recent
}

// The plan says what the dialog shows, and names no file or folder of
// baton's, of Claude Code's or the temporary one: only the project folder.
func TestDeletePlanNamesNoPaths(t *testing.T) {
	d := newDelRig(t)
	code, body := d.plan(delSID, "a")
	var p convdel.Plan
	if code != 200 || json.Unmarshal([]byte(body), &p) != nil {
		t.Fatalf("plan: %d %s", code, body)
	}
	if p.SessionID != delSID || p.Title != "fix the login bug" || p.MadeIn != "baton" || !p.OK || len(p.Blockers) != 0 ||
		p.Items.Files != 1 || p.Items.Folders != 2 || p.Items.Runs != 1 || p.Cwd != d.proj {
		t.Fatalf("plan: %s", body)
	}
	low := strings.ToLower(body)
	for _, dir := range []string{d.paths.Root, d.prof, d.home, d.temp, d.bin, d.data} {
		for _, s := range []string{strings.ReplaceAll(dir, `\`, `\\`), filepath.ToSlash(dir)} {
			if strings.Contains(low, strings.ToLower(s)) {
				t.Errorf("the plan names %s: %s", dir, body)
			}
		}
	}
	for _, s := range []string{"jsonl", "file-history", delRunID} {
		if strings.Contains(low, s) {
			t.Errorf("the plan names %q: %s", s, body)
		}
	}
	// Looking changes nothing.
	if len(d.binned()) != 0 || !isFile(d.file) || !isFile(d.twin) {
		t.Fatal("the plan moved something")
	}
}

// The plan says whether only what an earlier delete left is there
// (leftover), which the dialog can't tell by the title: a conversation
// whose file is empty, or whose rows can't be read, has no title, folder or
// source either, and it is the conversation's own file. So has one behind a
// junction, which baton does not read through: it is there all the same.
func TestDeletePlanSaysLeftover(t *testing.T) {
	d := newDelRig(t)
	look := func(what, id, where string, leftover bool) convdel.Plan {
		t.Helper()
		code, body := d.plan(id, where)
		var p convdel.Plan
		if code != 200 || json.Unmarshal([]byte(body), &p) != nil || !strings.Contains(body, `"leftover":`+strconv.FormatBool(leftover)) {
			t.Fatalf("%s: %d %s", what, code, body)
		}
		return p
	}

	// Empty, as a crash before its first row leaves it.
	if err := os.Truncate(d.file, 0); err != nil {
		t.Fatal(err)
	}
	if p := look("empty", delSID, "a", false); p.Title != "" || p.Cwd != "" || p.Source != "" || !p.OK || p.Items.Files != 1 {
		t.Fatalf("empty: %+v", p)
	}
	d.write(d.file, "not a row\n")
	if p := look("rows that can't be read", delSID, "a", false); p.Title != "" || p.Cwd != "" || !p.OK || p.Items.Files != 1 {
		t.Fatalf("rows that can't be read: %+v", p)
	}

	// Empty, in a project folder behind a junction: refused as linked.
	const linked = "de1e7e00-0000-4000-8000-000000000003"
	moved := filepath.Join(t.TempDir(), "C--moved")
	d.write(filepath.Join(moved, linked+".jsonl"), "")
	link := filepath.Join(d.home, "projects", "C--moved")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, moved).CombinedOutput(); err != nil {
		t.Logf("no junction (%v): %s", err, out)
	} else {
		t.Cleanup(func() { _ = os.Remove(link) })
		if p := look("behind a junction", linked, "claude-code", false); p.OK || len(p.Blockers) == 0 || p.Blockers[0].Code != convdel.CodeLinkedFolder || p.Title != "" {
			t.Fatalf("behind a junction: %+v", p)
		}
	}

	// No name of its file left: only what a delete that stopped half way
	// did not move.
	for _, f := range []string{d.file, d.twin} {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	if p := look("leftover", delSID, "a", true); !p.OK || p.Items.Files != 0 || p.Items.Folders != 2 || p.Items.Runs != 1 {
		t.Fatalf("leftover: %+v", p)
	}
}

// What the plan and the delete refuse, and with which status and code; a
// refused delete changes nothing.
func TestDeleteRefusals(t *testing.T) {
	d := newDelRig(t)
	for name, c := range map[string]struct {
		get, post string // a plan's query, a delete's body ("" skips it)
		status    int
		code      string
	}{
		"bad id":         {"session_id=..%5Cx&where=a", `{"session_id":"..\\x","where":"a"}`, http.StatusBadRequest, "bad_session"},
		"unknown where":  {"session_id=" + delSID + "&where=zz", `{"session_id":"` + delSID + `","where":"zz"}`, http.StatusBadRequest, "bad_session"},
		"no where":       {"session_id=" + delSID, `{"session_id":"` + delSID + `"}`, http.StatusBadRequest, "bad_session"},
		"gone":           {"session_id=de1e7e00-0000-4000-8000-000000000009&where=a", `{"session_id":"de1e7e00-0000-4000-8000-000000000009","where":"a"}`, http.StatusNotFound, "gone"},
		"unknown fields": {"", `{"session_id":"` + delSID + `","where":"a","path":"C:\\x"}`, http.StatusBadRequest, "bad_request"},
	} {
		if c.get != "" {
			if code, body := d.do("GET", "/api/sessions/delete-plan?"+c.get, "", nil); code != c.status || !strings.Contains(body, `"code":"`+c.code+`"`) {
				t.Errorf("%s, plan: %d %s", name, code, body)
			}
		}
		if code, body := d.do("POST", "/api/sessions/delete", c.post, nil); code != c.status || !strings.Contains(body, `"code":"`+c.code+`"`) {
			t.Errorf("%s, delete: %d %s", name, code, body)
		}
	}

	// A baton session runs it: the plan says so, and the delete is refused
	// with the run, so the page can bring its window forward.
	running := "20261113-110000-eeeeee"
	dir := d.paths.Run(running)
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: running, SupervisorPID: os.Getpid(), SupervisorCreated: winproc.ProcessCreated(os.Getpid()),
		State: run.StateWaiting, Account: "a", Cwd: d.proj, SessionID: delSID, TranscriptPath: d.file, Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	code, body := d.plan(delSID, "a")
	var p convdel.Plan
	if code != 200 || json.Unmarshal([]byte(body), &p) != nil || p.OK || len(p.Blockers) == 0 ||
		p.Blockers[0] != (convdel.Blocker{Code: "in_baton", RunID: running}) {
		t.Fatalf("plan while running: %d %s", code, body)
	}
	code, body = d.del(delSID, "a")
	var e struct {
		Code    string          `json:"code"`
		Blocker convdel.Blocker `json:"blocker"`
	}
	if code != http.StatusConflict || json.Unmarshal([]byte(body), &e) != nil || e.Code != "in_baton" || e.Blocker.RunID != running {
		t.Fatalf("delete while running: %d %s", code, body)
	}
	if len(d.binned()) != 0 || !isFile(d.file) || !isFile(d.twin) || !isFile(filepath.Join(d.prof, "file-history", delSID, "a.txt@v1")) {
		t.Fatal("a refused delete moved something")
	}
	var cfg config.Config
	if cfg, _ = config.Load(d.paths.Config()); !slices.Contains(cfg.OfficeView.HiddenSessions, delSID) {
		t.Fatal("a refused delete showed it in the office again")
	}
}

// A delete moves every name of the conversation and what is kept with it to
// the Recycle Bin, its ended run too: its row leaves Recent sessions and the
// office no longer hides it. The other conversation stays as it is.
func TestDeleteFromTheWindow(t *testing.T) {
	d := newDelRig(t)
	if rs := d.recent(); len(rs) != 1 || rs[0].SessionID != delSID || rs[0].Where != "a" {
		t.Fatalf("recent before: %+v", rs)
	}
	code, body := d.del(delSID, "a")
	var res struct {
		OK      bool   `json:"ok"`
		Moved   int    `json:"moved"`
		Warning string `json:"warning"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &res) != nil || !res.OK || res.Moved != 5 || res.Warning != "" {
		t.Fatalf("delete: %d %s", code, body)
	}
	want := []string{d.twin, d.file, filepath.Join(d.prof, "file-history", delSID), filepath.Join(d.temp, "claude", "C--proj", delSID), d.paths.Run(delRunID)}
	if got := d.binned(); !slices.EqualFunc(got, want, strings.EqualFold) {
		t.Fatalf("binned %q, want %q", got, want)
	}
	if isFile(d.file) || isFile(d.twin) || !isFile(d.keep) {
		t.Fatal("not as it should be on disk")
	}
	if rs := d.recent(); len(rs) != 0 {
		t.Fatalf("recent after: %+v", rs)
	}
	cfg, err := config.Load(d.paths.Config())
	if err != nil || cfg.OfficeView == nil || !slices.Equal(cfg.OfficeView.HiddenSessions, []string{delOther}) {
		t.Fatalf("office hides %+v (%v)", cfg.OfficeView, err)
	}
	if code, body := d.plan(delSID, "a"); code != http.StatusNotFound || !strings.Contains(body, `"code":"gone"`) {
		t.Fatalf("plan afterwards: %d %s", code, body)
	}
	if code, body := d.del(delSID, "a"); code != http.StatusNotFound || !strings.Contains(body, `"code":"gone"`) {
		t.Fatalf("again: %d %s", code, body)
	}
}

// A delete waits for an account removal in this window to finish (it moves
// the account's conversations), and plans only after it.
func TestDeleteWaitsForARemoval(t *testing.T) {
	d := newDelRig(t)
	gate := make(chan struct{})
	d.spawn.mu.Lock()
	d.spawn.hiddenGate = gate
	d.spawn.mu.Unlock()
	removed := make(chan int, 1)
	go func() {
		code, _ := d.do("POST", "/api/accounts/remove", `{"name":"a"}`, nil)
		removed <- code
	}()
	// The removal is under way once it waits in baton.exe.
	for i := 0; ; i++ {
		if !d.srv.moveMu.TryLock() {
			break
		}
		d.srv.moveMu.Unlock()
		if i > 200 {
			t.Fatal("the removal never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deleted := make(chan int, 1)
	go func() {
		code, _ := d.del(delSID, "a")
		deleted <- code
	}()
	select {
	case code := <-deleted:
		t.Fatalf("deleted during a removal: %d", code)
	case <-time.After(300 * time.Millisecond):
	}
	if len(d.binned()) != 0 {
		t.Fatal("moved something during a removal")
	}
	close(gate)
	if code := <-removed; code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if code := <-deleted; code != 200 {
		t.Fatalf("delete after the removal: %d", code)
	}
}

// A tick's pass over the desktop app's lists runs after the answer went,
// writing into baton's folder: a test's cleanup waits for it, or the folder
// can't be removed (and the pass would go on with the next test's settings).
func TestTickPassIsWaitedFor(t *testing.T) {
	d := newDelRig(t)
	d.srv.deskMu.Lock() // the pass waits here
	code, body := d.do("POST", "/api/desktop/apply", `{"session_id":"`+delSID+`","places":["`+delAcct+"/"+delOrg+`"]}`, nil)
	if code != 200 {
		d.srv.deskMu.Unlock()
		t.Fatalf("tick: %d %s", code, body)
	}
	waited := make(chan struct{})
	go func() {
		d.srv.waitBackground()
		close(waited)
	}()
	select {
	case <-waited:
		d.srv.deskMu.Unlock()
		t.Fatal("not waited for")
	case <-time.After(300 * time.Millisecond):
	}
	d.srv.deskMu.Unlock()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("the pass never ended")
	}
}

// While a conversation is being deleted, here or by the CLI, it is not
// ticked in the desktop app: the delete takes it off there, and its files
// are going. Nor is its cut-off run reopened.
func TestNothingStartsOnADelete(t *testing.T) {
	d := newDelRig(t)
	key := delAcct + "/" + delOrg
	apply := func() (int, string) {
		return d.do("POST", "/api/desktop/apply", `{"session_id":"`+delSID+`","places":["`+key+`"]}`, nil)
	}
	record := filepath.Join(d.place, "local_"+delSID+".json")

	release, err := run.MarkDeleting(d.paths.Root, delSID)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := apply(); code != http.StatusConflict || !strings.Contains(body, `"code":"deleting"`) || isFile(record) {
		t.Fatalf("tick while the CLI deletes: %d %s", code, body)
	}
	release()
	g := d.srv.convGate(delSID)
	g.Lock()
	if code, body := apply(); code != http.StatusConflict || !strings.Contains(body, `"code":"deleting"`) || isFile(record) {
		g.Unlock()
		t.Fatalf("tick while this window deletes: %d %s", code, body)
	}
	g.Unlock()
	if code, body := apply(); code != 200 || !isFile(record) {
		t.Fatalf("tick afterwards: %d %s", code, body)
	}

	// The run, cut off by a restart instead.
	if err := run.SaveJournal(d.paths.Run(delRunID), run.Journal{RunID: delRunID, SupervisorPID: 999999, SupervisorCreated: time.Now().Add(-time.Hour),
		State: run.StateDone, EndedBy: run.EndedShutdown, Account: "a", Cwd: d.proj, SessionID: delSID, TranscriptPath: d.file,
		Started: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	restore := func() (int, string) {
		return d.do("POST", "/api/sessions/restore", `{"run_ids":["`+delRunID+`"]}`, nil)
	}
	if release, err = run.MarkDeleting(d.paths.Root, delSID); err != nil {
		t.Fatal(err)
	}
	if code, body := restore(); code != http.StatusConflict || !strings.Contains(body, `"code":"deleting"`) || len(d.spawn.console) != 0 {
		t.Fatalf("reopen while deleting: %d %s %q", code, body, d.spawn.console)
	}
	release()
	if code, body := restore(); code != http.StatusAccepted || len(d.spawn.console) != 1 {
		t.Fatalf("reopen afterwards: %d %s", code, body)
	}
}

// A failure the page cannot word, and the Recycle Bin's own reason (it names
// files), go to baton's log and never to the page, which words the code.
func TestDeleteFailureIsLogged(t *testing.T) {
	var logged []string
	s := &Server{opt: Options{Log: func(format string, a ...any) { logged = append(logged, fmt.Sprintf(format, a...)) }}}
	const secret = `C:\Users\someone\.claude\projects\x\` + delSID + ".jsonl"
	for _, c := range []struct {
		err    error
		code   string
		status int
		logs   bool
	}{
		{&convdel.Error{Code: convdel.CodeRecycleFailed, Blockers: []convdel.Blocker{{Code: convdel.CodeRecycleFailed}},
			Err: errors.New("IFileOperation 0x80270000 on " + secret)}, convdel.CodeRecycleFailed, http.StatusInternalServerError, true},
		{&convdel.Error{Code: convdel.CodeChanged, Moved: 1, Err: errors.New("a name of " + secret + " is still there")}, convdel.CodeChanged, http.StatusConflict, true},
		{&convdel.Error{Code: convdel.CodeInUse, Blockers: []convdel.Blocker{{Code: convdel.CodeInUse}}}, convdel.CodeInUse, http.StatusConflict, false},
		{errors.New("open " + secret + ": access denied"), "delete", http.StatusInternalServerError, true},
	} {
		logged = nil
		w := httptest.NewRecorder()
		s.delFail(w, c.err)
		body := w.Body.String()
		if w.Code != c.status || !strings.Contains(body, `"code":"`+c.code+`"`) || strings.Contains(body, "someone") {
			t.Errorf("%s: %d %s", c.code, w.Code, body)
		}
		if got := slices.ContainsFunc(logged, func(l string) bool { return strings.Contains(l, "someone") }); got != c.logs {
			t.Errorf("%s: logged %v: %q", c.code, got, logged)
		}
	}
}
