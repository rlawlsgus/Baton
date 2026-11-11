package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/desktop"
)

// Where a session shows in the Claude desktop app follows it into the
// conversation it goes on in: after /clear, and into a copy it moved to by
// itself; not after an in-session /resume of another conversation. All of it
// in temporary folders (the desktop app's data folder included).
func TestDesktopChoiceFollowsTheSession(t *testing.T) {
	const (
		acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		org  = "a0000000-0000-4000-8000-00000000000a"
		old  = "11111111-2222-4333-8444-000000000001"
		next = "11111111-2222-4333-8444-000000000002"
		cp   = "11111111-2222-4333-8444-000000000003"
		pick = "11111111-2222-4333-8444-000000000004"
	)
	s := newTest(t)
	data := filepath.Join(t.TempDir(), "Claude-Data", "claude_test")
	if err := os.MkdirAll(filepath.Join(data, "claude-code-sessions", acct, org), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BATON_DESKTOP_DATA", data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("BATON_RUN", "")
	for i := range s.reg.Accounts {
		s.reg.Accounts[i].AuthMethod, s.reg.Accounts[i].AccountUUID, s.reg.Accounts[i].OrgID = "claude.ai", acct, org
	}
	s.acct = s.reg.Accounts[0]
	prof := s.acct.ProfileDir
	file := writeTranscript(t, prof, old)
	appendTurn(t, file)
	key := acct + "/" + org
	if _, err := desktop.New(s.opt.Paths, s.reg).Apply(desktop.Conversation{ID: old, Transcript: file}, []string{key}); err != nil {
		t.Fatal(err)
	}
	s.session, s.transcr = old, file
	s.launchedAt = time.Now().Add(-time.Second)
	record := filepath.Join(data, "claude-code-sessions", acct, org, "local_"+old+".json")
	cli := func() string {
		var m map[string]any
		b, _ := os.ReadFile(record)
		_ = json.Unmarshal(b, &m)
		id, _ := m["cliSessionId"].(string)
		return id
	}
	thread := func() string {
		ix, err := desktop.LoadIndex(s.opt.Paths.Desktop())
		if err != nil || len(ix.Threads) != 1 {
			t.Fatalf("index: %v %+v", err, ix)
		}
		return ix.Threads[0].Session
	}
	settle := func() {
		w := s.desk
		s.desk = nil
		w.wait(10 * time.Second)
	}

	// /clear: a new conversation; once it has its first turn the record points to it.
	nextFile := filepath.Join(filepath.Dir(file), next+".jsonl")
	emit(t, s, "SessionStart", 0, map[string]any{"session_id": next, "transcript_path": nextFile, "source": "clear"})
	s.handleEvents()
	settle()
	if thread() != next || cli() != old {
		t.Fatalf("after /clear: thread %s, record %s", thread(), cli())
	}
	writeTranscript(t, prof, next)
	appendTurn(t, nextFile)
	emit(t, s, "Stop", 0, map[string]any{"session_id": next, "transcript_path": nextFile})
	s.handleEvents()
	settle()
	if cli() != next {
		t.Fatalf("after the first turn of the new conversation the record says %s", cli())
	}

	// An in-session /resume of another conversation: the choice stays.
	pickFile := writeTranscript(t, prof, pick)
	emit(t, s, "SessionStart", 0, map[string]any{"session_id": pick, "transcript_path": pickFile, "source": "resume"})
	s.handleEvents()
	settle()
	if thread() != next {
		t.Fatalf("followed a /resume: %s", thread())
	}

	// Back on it, then into a copy the session moved to by itself.
	s.session, s.transcr = next, nextFile
	s.followFrom = next
	s.session, s.transcr = "", ""
	copyFile := filepath.Join(filepath.Dir(file), cp+".jsonl")
	emit(t, s, "SessionStart", 0, map[string]any{"session_id": cp, "transcript_path": copyFile, "source": "resume"})
	s.handleEvents()
	writeTranscript(t, prof, cp)
	appendTurn(t, copyFile)
	emit(t, s, "Stop", 0, map[string]any{"session_id": cp, "transcript_path": copyFile})
	s.handleEvents()
	settle()
	if thread() != cp || cli() != cp || s.followFrom != "" {
		t.Fatalf("into the copy: thread %s, record %s, followFrom %q", thread(), cli(), s.followFrom)
	}
	// The second names follow too: only the copy's is left.
	names, _ := filepath.Glob(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "*", "*.jsonl"))
	if len(names) != 1 || !strings.HasSuffix(names[0], cp+".jsonl") {
		t.Fatalf("second names: %v", names)
	}
}

// A copy asked for at the start (--fork-session) starts afresh; one made
// because the conversation is open elsewhere (--auto-copy-of) takes the
// desktop app's choice along.
func TestAutoCopyFollows(t *testing.T) {
	src := filepath.Join(t.TempDir(), "projects", "p", "11111111-2222-4333-8444-000000000001.jsonl")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newTest(t, "--resume", src, "--fork-session")
	if _, err := s.firstArgs(); err != nil || s.followFrom != "" {
		t.Fatalf("an asked-for copy follows: %v %q", err, s.followFrom)
	}
	s = newTest(t, "--resume", src, "--fork-session")
	s.opt.AutoCopyOf = "11111111-2222-4333-8444-000000000001"
	if _, err := s.firstArgs(); err != nil || s.followFrom != s.opt.AutoCopyOf {
		t.Fatalf("an automatic copy does not follow: %v %q", err, s.followFrom)
	}
}

func appendTurn(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.WriteString(`{"type":"user","cwd":"C:\\proj","timestamp":"2026-11-01T10:00:00.000Z","message":{"role":"user","content":"go on"}}` + "\n")
}
