package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/claude"
	"baton/internal/convdel"
	"baton/internal/recycle"
	"baton/internal/run"
	"baton/internal/winproc"
)

// A conversation being deleted is started by no way in: baton resume (in
// English and Korean, as it is or in a copy), -r <id>, --resume <path>, -c,
// or reopening a run that was cut off with it. Once the delete lets go, they
// all find it again.
func TestStartsRefusedWhileDeleting(t *testing.T) {
	c := newCLI(t)
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	env := []string{"CLAUDE_CONFIG_DIR=" + home}
	proj := t.TempDir()
	const sid = "11111111-2222-4333-8444-555555555551"
	tr := filepath.Join(home, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	if err := os.WriteFile(tr, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"hi"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := envAt(c.home)
	runID := "20261109-101500-abc123"
	dir := e.paths.Run(runID)
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, run.Journal{RunID: runID, SupervisorPID: 0x7ffffffc, State: run.StateRunning, Account: "a",
		SessionID: sid, TranscriptPath: tr, Cwd: proj}); err != nil {
		t.Fatal(err)
	}
	resolve := func(args ...string) (claude.Args, error) {
		t.Helper()
		a, err := claude.SplitArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		return a, resolveResume(&a, e, proj)
	}
	deleting := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "conversation "+sid+" is being deleted")
	}

	release, err := run.MarkDeleting(c.home, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(env, "resume", sid); code != 1 || !strings.Contains(out, "is being deleted") || strings.Contains(out, "continuing in") {
		t.Errorf("resume: %d %s", code, out)
	}
	if out, code := c.run(append(env, "BATON_LANG=ko"), "resume", strings.ToUpper(sid), "--copy"); code != 1 || !strings.Contains(out, sid+" 대화를 삭제하는 중입니다") {
		t.Errorf("resume --copy in Korean: %d %s", code, out)
	}
	for _, copyIt := range []bool{false, true} {
		if _, code, err := planResume(e, sid, copyIt); code != 1 || !deleting(err) {
			t.Errorf("planResume copy=%v: %d %v", copyIt, code, err)
		}
	}
	for _, args := range [][]string{{"-r", sid}, {"--resume=" + strings.ToUpper(sid)}, {"--resume", tr}, {"-c"}} {
		if _, err := resolve(args...); !deleting(err) {
			t.Errorf("%q: %v", args, err)
		}
	}
	if _, err := cutOffRun(e, runID); !deleting(err) {
		t.Errorf("restore: %v", err)
	}

	release()
	if plan, code, err := planResume(e, sid, false); err != nil || code != 0 || plan.transcript != tr {
		t.Errorf("planResume after: %+v %d %v", plan, code, err)
	}
	for _, args := range [][]string{{"-r", sid}, {"--resume", tr}, {"-c"}} {
		if a, err := resolve(args...); err != nil || a.ResumeArg != tr {
			t.Errorf("%q after: %q %v", args, a.ResumeArg, err)
		}
	}
	if j, err := cutOffRun(e, runID); err != nil || j.TranscriptPath != tr {
		t.Errorf("restore after: %+v %v", j, err)
	}
}

// baton sessions delete says what goes and what stays, refuses inside a
// session and while baton runs the conversation, changes nothing on a dry
// run or without a console to ask on, and then moves the conversation and
// what is kept by its id to the Recycle Bin (here a folder standing in for
// it), leaving the rest of the project as it is.
func TestSessionsDelete(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	c := newCLI(t)
	root := t.TempDir()
	user := filepath.Join(root, "user")
	home := filepath.Join(user, ".claude")
	tmp := filepath.Join(root, "tmp")
	bin := filepath.Join(root, "bin")
	env := []string{"USERPROFILE=" + user, "CLAUDE_CONFIG_DIR=" + home, "LOCALAPPDATA=" + filepath.Join(root, "local"),
		"APPDATA=" + filepath.Join(root, "roaming"), "TEMP=" + tmp, "TMP=" + tmp, "BATON_RECYCLE=" + bin}
	if out, code := c.run(env, "add", "a"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	prof := c.registry().Get("a").ProfileDir
	proj := filepath.Join(root, "Proj")
	const sid = "de1e7e00-0000-4000-8000-000000000001"
	const other = "de1e7e00-0000-4000-8000-000000000002"
	slug := claude.ProjectSlug(proj)
	write := func(p, content string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	row := func(prompt string) string {
		return `{"type":"user","cwd":` + strconv.Quote(proj) + `,"timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"` + prompt + `"}}` + "\n"
	}
	_ = os.MkdirAll(proj, 0o700)
	file := write(filepath.Join(prof, "projects", slug, sid+".jsonl"), row("tidy the docs"))
	goes := []string{file,
		write(filepath.Join(prof, "projects", slug, sid, "subagents", "agent-1.jsonl"), row("look around")),
		write(filepath.Join(prof, "file-history", sid, "a.txt@v1"), "before"),
		write(filepath.Join(tmp, "claude", "h", sid, "out.txt"), "scratch"),
	}
	stays := map[string]string{
		filepath.Join(prof, "projects", slug, other+".jsonl"):        row("another one"),
		filepath.Join(prof, "projects", slug, "memory", "MEMORY.md"): "remember",
		filepath.Join(proj, "README.md"):                             "the project",
	}
	for p, s := range stays {
		write(p, s)
	}
	unchanged := func(what string) {
		t.Helper()
		for _, p := range goes {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("%s: %s is gone", what, p)
			}
		}
		if _, err := os.Stat(bin); !os.IsNotExist(err) {
			t.Fatalf("%s: the Recycle Bin was used", what)
		}
	}

	// Asked from inside a session: refused before anything is looked at.
	if out, code := c.run(append(env, "BATON_RUN=r1"), "sessions", "delete", sid, "--yes"); code == 0 || !strings.Contains(out, "inside a baton session") {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
	unchanged("inside a session")

	// While baton runs it: refused, with the session to end.
	e := envAt(c.home)
	runID := "20261113-101500-de1e7e"
	dir := e.paths.Run(runID)
	j := run.Journal{RunID: runID, SupervisorPID: os.Getpid(), SupervisorCreated: winproc.ProcessCreated(os.Getpid()),
		State: run.StateRunning, Account: "a", SessionID: sid, TranscriptPath: file, Cwd: proj}
	_ = os.MkdirAll(dir, 0o700)
	if err := run.SaveJournal(dir, j); err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(env, "sessions", "delete", sid, "--yes"); code == 0 || !strings.Contains(out, "running in baton (session "+runID+")") {
		t.Fatalf("running in baton: %d\n%s", code, out)
	}
	unchanged("running in baton")
	// It ended: its run's records go with it.
	j.SupervisorPID, j.State = 0x7ffffffc, run.StateDone
	if err := run.SaveJournal(dir, j); err != nil {
		t.Fatal(err)
	}

	out, code := c.run(env, "sessions", "delete", strings.ToUpper(sid), "--dry-run")
	if code != 0 || !strings.Contains(out, "tidy the docs") || !strings.Contains(out, "in "+proj) ||
		!strings.Contains(out, "To the Recycle Bin: ") || !strings.Contains(out, "the records of 1 baton session(s)") ||
		!strings.Contains(out, "your other conversations stay as they are") || !strings.Contains(out, "Nothing was changed (--dry-run).") {
		t.Fatalf("dry run: %d\n%s", code, out)
	}
	unchanged("dry run")
	if out, code := c.run(append(env, "BATON_LANG=ko"), "sessions", "delete", sid, "--dry-run"); code != 0 ||
		!strings.Contains(out, "휴지통으로 옮깁니다") || !strings.Contains(out, "아무것도 바꾸지 않았습니다 (--dry-run).") {
		t.Fatalf("dry run in Korean: %d\n%s", code, out)
	}
	// No console to ask on, and no --yes: it says why and does nothing.
	if out, code := c.run(env, "sessions", "delete", sid); code == 0 || !strings.Contains(out, "not a console") || !strings.Contains(out, "--yes") {
		t.Fatalf("no console: %d\n%s", code, out)
	}
	unchanged("no console")

	out, code = c.run(env, "sessions", "delete", sid, "--yes")
	if code != 0 || !strings.Contains(out, "Moved the conversation to the Recycle Bin") {
		t.Fatalf("delete: %d\n%s", code, out)
	}
	for _, p := range append(goes, dir) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", p, err)
		}
	}
	for p, s := range stays {
		if b, err := os.ReadFile(p); err != nil || string(b) != s {
			t.Errorf("%s changed: %q %v", p, b, err)
		}
	}
	list, err := recycle.ReadManifest(bin)
	if err != nil || len(list) == 0 || !strings.EqualFold(list[0].From, file) {
		t.Fatalf("the Recycle Bin: %+v %v", list, err)
	}
	if b, err := os.ReadFile(list[0].To); err != nil || string(b) != row("tidy the docs") {
		t.Fatalf("the conversation in the bin: %q %v", b, err)
	}

	if out, code := c.run(env, "sessions", "delete", sid, "--yes"); code == 0 || !strings.Contains(out, "no conversation "+sid) {
		t.Fatalf("again: %d\n%s", code, out)
	}
	if out, code := c.run(env, "sessions"); code != 0 || strings.Contains(out, sid) || !strings.Contains(out, other) ||
		!strings.Contains(out, "baton sessions delete <id>") {
		t.Fatalf("sessions after: %d\n%s", code, out)
	}
	if out, code := c.run(env, "sessions", "delete", "not-an-id"); code == 0 || !strings.Contains(out, "1b2c3d4e-") {
		t.Fatalf("a bad id: %d\n%s", code, out)
	}
}

// A conversation whose file baton reaches only through a link (here the
// projects folder moved to another drive behind a junction) is refused as
// linked, and the plan says nothing of a leftover of an earlier delete, nor
// counts anything as going: nothing goes, and the conversation is there.
func TestDeletePlanOfALinkedFolder(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	c := newCLI(t)
	root := t.TempDir()
	user := filepath.Join(root, "user")
	home := filepath.Join(user, ".claude")
	tmp := filepath.Join(root, "tmp")
	bin := filepath.Join(root, "bin")
	env := []string{"USERPROFILE=" + user, "CLAUDE_CONFIG_DIR=" + home, "LOCALAPPDATA=" + filepath.Join(root, "local"),
		"APPDATA=" + filepath.Join(root, "roaming"), "TEMP=" + tmp, "TMP=" + tmp, "BATON_RECYCLE=" + bin}
	proj := filepath.Join(root, "Proj")
	const sid = "de1e7e00-0000-4000-8000-0000000000a1"
	moved := filepath.Join(root, "D", "projects")
	file := filepath.Join(moved, claude.ProjectSlug(proj), sid+".jsonl")
	for _, d := range []string{proj, filepath.Dir(file), home, tmp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	row := `{"type":"user","cwd":` + strconv.Quote(proj) + `,"timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"tidy the docs"}}` + "\n"
	if err := os.WriteFile(file, []byte(row), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(home, "projects"), moved).CombinedOutput(); err != nil {
		t.Skipf("no junction (%v): %s", err, out)
	}
	for _, lang := range []string{"en", "ko"} {
		out, code := c.run(append(env, "BATON_LANG="+lang), "sessions", "delete", sid, "--dry-run")
		if code == 0 || !strings.Contains(out, sid) ||
			!strings.Contains(out, say2(lang, "reached through a link or junction", "링크(정션)로 연결된 폴더")) ||
			strings.Contains(out, say2(lang, "earlier delete left behind", "앞서 삭제하다 남은 것")) ||
			strings.Contains(out, say2(lang, "To the Recycle Bin:", "휴지통으로:")) {
			t.Fatalf("%s: %d\n%s", lang, code, out)
		}
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != row {
		t.Fatalf("the conversation changed: %q %v", b, err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("the Recycle Bin was used: %v", err)
	}
}

// A conversation behind a link baton could not read (a locked or offline
// volume, a folder it may not list) has no title, folder or source in its
// plan, and MadeIn stays "outside" with no name of the file seen: the plan
// then claims nothing of where it was made, as the window's dialog does.
// One baton could name says where it was made, unless a profile holds it.
func TestDeletePlanSaysWhereMadeOnlyWhenKnown(t *testing.T) {
	const sid = "de1e7e00-0000-4000-8000-0000000000a2"
	linked := []convdel.Blocker{{Code: convdel.CodeLinkedFolder}}
	for _, lang := range []string{"en", "ko"} {
		t.Setenv("BATON_LANG", lang)
		outside := say2(lang, "made outside baton", "baton 밖(")
		out := planText(t, convdel.Plan{SessionID: sid, MadeIn: "outside", Blockers: linked})
		if !strings.Contains(out, sid+"  "+say2(lang, "(no title)", "(제목 없음)")) || strings.Contains(out, outside) ||
			strings.Contains(out, say2(lang, "earlier delete left behind", "앞서 삭제하다 남은 것")) {
			t.Errorf("%s, nameless: %s", lang, out)
		}
		out = planText(t, convdel.Plan{SessionID: sid, Title: "tidy the docs", Source: "cli", MadeIn: "outside", Blockers: linked})
		if !strings.Contains(out, "tidy the docs") || !strings.Contains(out, say2(lang,
			"It was made outside baton (terminal); it goes from there too.", "baton 밖(터미널)에서 만든 대화입니다.")) {
			t.Errorf("%s, named: %s", lang, out)
		}
		out = planText(t, convdel.Plan{SessionID: sid, Title: "tidy the docs", Source: "cli", MadeIn: "baton", Blockers: linked})
		if strings.Contains(out, outside) {
			t.Errorf("%s, in a profile: %s", lang, out)
		}
	}
}

// The size of what goes is in the plan's language, as Explorer writes it:
// under 1 KB the unit is a word ("73 bytes", "73바이트"); KB, MB and GB
// are written the same in both.
func TestDeletePlanSizeInItsLanguage(t *testing.T) {
	const sid = "de1e7e00-0000-4000-8000-0000000000a3"
	for _, c := range []struct {
		lang  string
		bytes int64
		want  string
	}{
		{"en", 73, "To the Recycle Bin: 1 file(s), 0 folder(s), 73 bytes."},
		{"ko", 73, "휴지통으로: 파일 1개, 폴더 0개, 73바이트."},
		{"en", 1536, "To the Recycle Bin: 1 file(s), 0 folder(s), 1.5 KB."},
		{"ko", 1536, "휴지통으로: 파일 1개, 폴더 0개, 1.5 KB."},
	} {
		t.Setenv("BATON_LANG", c.lang)
		out := planText(t, convdel.Plan{SessionID: sid, Title: "tidy the docs", MadeIn: "baton", Items: convdel.Counts{Files: 1, Bytes: c.bytes}})
		if !strings.Contains(out, c.want) {
			t.Errorf("%s, %d bytes: no %q in\n%s", c.lang, c.bytes, c.want, out)
		}
	}
}

// planText is what printDeletePlan prints for p.
func planText(t *testing.T, p convdel.Plan) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		read <- string(b)
	}()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	printDeletePlan(p)
	os.Stdout = stdout
	w.Close()
	out := <-read
	r.Close()
	return out
}

// say2 is the English or the Korean text, by lang.
func say2(lang, en, ko string) string {
	if lang == "ko" {
		return ko
	}
	return en
}

// Every code a delete can be refused or stopped with is worded, in English
// and in Korean, never shown as the bare code: each constant convdel names a
// code with is read from its source, so a code added there needs a text here.
func TestEveryDeleteRefusalWorded(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "internal", "convdel", "convdel.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, sp := range g.Specs {
			v := sp.(*ast.ValueSpec)
			for i, n := range v.Names {
				if !strings.HasPrefix(n.Name, "Code") || i >= len(v.Values) {
					continue
				}
				if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					codes = append(codes, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}
	if len(codes) < 15 {
		t.Fatalf("read only %d codes: %v", len(codes), codes)
	}
	const id = "1b2c3d4e-0000-4000-8000-000000000000"
	for _, lang := range []string{"en", "ko"} {
		t.Setenv("BATON_LANG", lang)
		for _, c := range codes {
			if c == "partly" {
				continue // deleteErr words it, with what moved and what is left
			}
			msg := blockerErr(id, convdel.Blocker{Code: c, RunID: "20260101-000000-abcd"}).Error()
			if strings.Contains(msg, "("+c+")") {
				t.Errorf("%s: %s is not worded: %s", lang, c, msg)
			}
		}
		e := deleteErr(id, &convdel.Error{Code: "partly", Moved: 2, Left: 1})
		if strings.Contains(e.Error(), "(partly)") {
			t.Errorf("%s: partly is not worded: %v", lang, e)
		}
	}
}

// Removing an account, or uninstalling, moves conversations to the user's
// own Claude Code: neither runs while a delete moves one to the Recycle Bin
// (in the window or in another terminal), and both run once it is done.
func TestRemoveAndUninstallWaitForADelete(t *testing.T) {
	c := newCLI(t)
	home := t.TempDir()
	// The uninstall removes the Start menu entry and an installed baton:
	// those of a temporary user profile, never the real one's.
	env := []string{"CLAUDE_CONFIG_DIR=" + home, "LOCALAPPDATA=" + t.TempDir(), "APPDATA=" + t.TempDir()}
	c.run(env, "add", "a")
	conv := filepath.Join(c.home, "profiles", "a", "projects", "C--proj", "11111111-2222-4333-8444-00000000000a.jsonl")
	_ = os.MkdirAll(filepath.Dir(conv), 0o700)
	if err := os.WriteFile(conv, []byte(`{"type":"user","cwd":"C:\proj"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := run.MarkDeleting(c.home, "11111111-2222-4333-8444-00000000000f")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"remove", "a", "--yes"}, {"uninstall", "--yes"}} {
		out, code := c.run(env, args...)
		if code == 0 || !strings.Contains(out, "a conversation is being deleted right now") {
			t.Fatalf("%v while deleting: %d\n%s", args, code, out)
		}
		if c.registry().Get("a") == nil {
			t.Fatalf("%v removed the account", args)
		}
		if _, err := os.Stat(conv); err != nil {
			t.Fatalf("%v moved the conversation: %v", args, err)
		}
	}
	if out, _ := c.run(append(env, "BATON_LANG=ko"), "remove", "a", "--yes"); !strings.Contains(out, "지금 대화를 삭제하는 중입니다") {
		t.Fatalf("in Korean:\n%s", out)
	}
	release()
	if out, code := c.run(env, "remove", "a", "--yes"); code != 0 || c.registry().Get("a") != nil {
		t.Fatalf("remove once the delete is done: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, "projects", "C--proj", "11111111-2222-4333-8444-00000000000a.jsonl")); err != nil {
		t.Fatalf("the conversation was not kept: %v", err)
	}
}
