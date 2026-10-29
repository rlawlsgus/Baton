package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/winproc"
)

// The desktop app renames a session and moves it to another folder as it
// goes (and moves the file into the new folder's project folder): the newest
// name and folder are at the end of a file that can be far bigger than what is
// read of it, and the model's own `cd` into a subfolder does not count.
func TestDescribeReadsTheNewestNameAndFolder(t *testing.T) {
	dir := t.TempDir()
	first, moved := filepath.Join(dir, "first"), filepath.Join(dir, "moved")
	const sid = "11111111-2222-4333-8444-555555555555"
	p := filepath.Join(dir, "projects", ProjectSlug(moved), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	q := strconv.Quote
	var b strings.Builder
	b.WriteString(`{"type":"queue-operation","sessionId":"s"}` + "\n")
	b.WriteString(`{"type":"user","entrypoint":"claude-desktop","cwd":` + q(first) + `,"message":{"role":"user","content":"첫 질문"}}` + "\n")
	b.WriteString(`{"type":"custom-title","customTitle":"old name","sessionId":"s"}` + "\n")
	for i := 0; i < 60; i++ { // 240 KB
		b.WriteString(`{"type":"assistant","cwd":` + q(first) + `,"message":{"role":"assistant","content":"` + strings.Repeat("x", 4000) + `"}}` + "\n")
	}
	b.WriteString(`{"type":"relocated","relocatedCwd":` + q(moved) + `,"sessionId":"s"}` + "\n")
	b.WriteString(`{"type":"user","cwd":` + q(moved) + `,"message":{"role":"user","content":"later"}}` + "\n")
	b.WriteString(`{"type":"custom-title","customTitle":"  새 이름\n 두 줄 ","sessionId":"s"}` + "\n")
	// The model ran `cd web`: the newest rows are in a subfolder.
	b.WriteString(`{"type":"assistant","cwd":` + q(filepath.Join(moved, "web")) + `,"message":{"role":"assistant","content":"built"}}` + "\n")
	// A request in flight: a big row without a cwd fills the last 128 KB.
	b.WriteString(`{"type":"api-request-shape","body":"` + strings.Repeat("z", 170<<10) + `"}` + "\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	got := Describe(p, fi)
	if got.Title != "새 이름 두 줄" || got.Cwd != moved || got.Source != "claude-desktop" || got.FirstPrompt != "첫 질문" || got.SessionID != sid {
		t.Fatalf("got %+v", got)
	}
	// A rename is seen once the file changes.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"custom-title","customTitle":"renamed","sessionId":"s"}` + "\n")
	_ = f.Close()
	fi, _ = os.Stat(p)
	if got := Describe(p, fi); got.Title != "renamed" {
		t.Fatalf("after a rename: %q", got.Title)
	}
	// It is listed under the folder it moved to, not where it began or the subfolder.
	for folder, want := range map[string]int{first: 0, moved: 1, filepath.Join(moved, "web"): 0} {
		if n := len(ListTranscripts([]string{dir}, folder, 0)); n != want {
			t.Fatalf("listed under %s: %d, want %d", folder, n, want)
		}
	}
	if got := LatestTranscriptFor([]string{dir}, moved); got != p {
		t.Fatalf("latest for the new folder: %q", got)
	}
}

// A conversation that ended while the model's shell was in a subfolder is
// still the newest of its project folder.
func TestNewestConversationAfterACdIntoASubfolder(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	q := strconv.Quote
	write := func(sid, body string, age time.Duration) string {
		p := filepath.Join(dir, "projects", ProjectSlug(proj), sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(body), 0o600)
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
		return p
	}
	ask := func(cwd, text string) string {
		return `{"type":"user","cwd":` + q(cwd) + `,"message":{"role":"user","content":` + q(text) + `}}` + "\n"
	}
	write("11111111-2222-4333-8444-000000000001", ask(proj, "older"), time.Hour)
	newer := write("11111111-2222-4333-8444-000000000002", ask(proj, "newer")+ask(filepath.Join(proj, "web"), "npm run build"), 0)
	if got := LatestTranscriptFor([]string{dir}, proj); got != newer {
		t.Fatalf("latest = %q, want the newer conversation", got)
	}
	if l := ListTranscripts([]string{dir}, proj, 0); len(l) != 2 || l[0].Path != newer || l[0].Cwd != proj {
		t.Fatalf("list %+v", l)
	}
}

// A first prompt with a pasted screenshot is one long row; the conversation
// is still listed, by its text. Old-style subagent files are not.
func TestLongFirstRowAndSubagentFiles(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	folder := filepath.Join(dir, "projects", ProjectSlug(proj))
	_ = os.MkdirAll(folder, 0o700)
	img := strings.Repeat("A", 600<<10)
	row := `{"type":"user","entrypoint":"cli","cwd":` + strconv.Quote(proj) + `,"message":{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","data":"` + img + `"}},{"type":"text","text":"what is wrong here?"}]}}` + "\n"
	_ = os.WriteFile(filepath.Join(folder, "11111111-2222-4333-8444-000000000003.jsonl"), []byte(row), 0o600)
	_ = os.WriteFile(filepath.Join(folder, "agent-1a2b3c4d.jsonl"),
		[]byte(`{"type":"user","isSidechain":true,"cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"Warmup"}}`+"\n"), 0o600)
	got := ListAllTranscripts([]string{dir}, 0)
	if len(got) != 1 || got[0].FirstPrompt != "what is wrong here?" || got[0].Cwd != proj || got[0].Source != "cli" {
		t.Fatalf("got %+v", got)
	}
	if l := ListTranscripts([]string{dir}, proj, 0); len(l) != 1 {
		t.Fatalf("folder list %+v", l)
	}
}

func TestListAllTranscripts(t *testing.T) {
	root := t.TempDir()
	home, acct := filepath.Join(root, "home"), filepath.Join(root, "acct")
	proj := filepath.Join(root, "p")
	write := func(cfg, sid, body string, age time.Duration) string {
		p := filepath.Join(cfg, "projects", ProjectSlug(proj), sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(body), 0o600)
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
		return p
	}
	ask := func(prompt string) string {
		return `{"type":"user","cwd":` + strconv.Quote(proj) + `,"message":{"role":"user","content":` + strconv.Quote(prompt) + `}}` + "\n"
	}
	newest := write(acct, "s3", ask("newest"), 0)
	// Nothing asked yet: not a conversation to continue.
	write(home, "s2", `{"type":"user","isMeta":true,"cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"meta"}}`+"\n", time.Hour)
	named := write(home, "s1", `{"type":"custom-title","customTitle":"named only"}`+"\n"+`{"type":"system","cwd":`+strconv.Quote(proj)+`}`+"\n", 2*time.Hour)
	older := write(home, "s0", ask("older"), 3*time.Hour)
	_ = os.MkdirAll(filepath.Join(home, "projects", ProjectSlug(proj), "s0", "subagents"), 0o700)

	got := ListAllTranscripts([]string{acct, home}, 0)
	if len(got) != 3 || got[0].Path != newest || got[1].Path != named || got[2].Path != older {
		t.Fatalf("got %+v", got)
	}
	if got[0].ConfigDir != acct || got[1].ConfigDir != home || got[1].Title != "named only" || got[2].FirstPrompt != "older" {
		t.Fatalf("details %+v", got)
	}
	if n := len(ListAllTranscripts([]string{acct, home}, 2)); n != 2 {
		t.Fatalf("max ignored: %d", n)
	}
}

func TestOpenSessions(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	pid := os.Getpid()
	entry := func(pid int, sid string, started int64) {
		_ = os.WriteFile(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"),
			[]byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+sid+`","startedAt":`+strconv.FormatInt(started, 10)+`}`), 0o600)
	}
	entry(pid, "live", winproc.ProcessCreated(pid).UnixMilli())
	entry(999999, "gone", time.Now().UnixMilli()) // no such process
	got := OpenSessions([]string{dir})
	if got["live"] == "" && BusyProfile(dir) {
		t.Fatalf("live session not reported: %v", got)
	}
	if got["gone"] != "" || len(got) > 1 {
		t.Fatalf("a dead claude's session reported: %v", got)
	}
}

// Claude Code's folder names: one '-' per UTF-16 unit, so two for an emoji,
// and a name over 200 characters cut there with a hash appended.
func TestProjectSlugAndLongFolders(t *testing.T) {
	if got := ProjectSlug(`C:\a🚀b 가`); got != "C--a--b--" { // per rune it would be C--a-b--
		t.Fatalf("slug %q", got)
	}
	dir := t.TempDir()
	long := filepath.Join(dir, strings.Repeat("x", 210))
	slug := ProjectSlug(long)
	p := filepath.Join(dir, "projects", slug[:200]+"-1a2b3c", "11111111-2222-4333-8444-000000000009.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	q := strconv.Quote
	body := `{"type":"user","entrypoint":"claude-desktop","cwd":` + q(dir) + `,"message":{"role":"user","content":"start"}}` + "\n" +
		`{"type":"relocated","relocatedCwd":` + q(long) + `}` + "\n" +
		`{"type":"assistant","cwd":` + q(long) + `,"message":{"role":"assistant","content":"ok"}}` + "\n"
	_ = os.WriteFile(p, []byte(body), 0o600)
	if l := ListTranscripts([]string{dir}, long, 0); len(l) != 1 || l[0].Cwd != long {
		t.Fatalf("long folder: %+v", l)
	}
}

// A desktop session's newest name can be far from the end after a long turn;
// it is still found.
func TestDesktopNameFarFromTheEnd(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	p := filepath.Join(dir, "projects", ProjectSlug(proj), "11111111-2222-4333-8444-000000000010.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	q := strconv.Quote
	var b strings.Builder
	b.WriteString(`{"type":"user","entrypoint":"claude-desktop","cwd":` + q(proj) + `,"message":{"role":"user","content":"hi"}}` + "\n")
	b.WriteString(`{"type":"custom-title","customTitle":"first name"}` + "\n")
	b.WriteString(`{"type":"custom-title","customTitle":"current name"}` + "\n")
	for i := 0; i < 400; i++ { // 1.6 MB of one long turn
		b.WriteString(`{"type":"assistant","cwd":` + q(proj) + `,"message":{"role":"assistant","content":"` + strings.Repeat("w", 4000) + `"}}` + "\n")
	}
	_ = os.WriteFile(p, []byte(b.String()), 0o600)
	fi, _ := os.Stat(p)
	if got := Describe(p, fi); got.Title != "current name" {
		t.Fatalf("title %q", got.Title)
	}
}
