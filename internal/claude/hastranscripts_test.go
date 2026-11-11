package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// HasTranscripts finds a project's conversation in its own folder, whatever
// the case of the path, and never reads another project's files to say
// there is none.
func TestHasTranscripts(t *testing.T) {
	root := t.TempDir()
	write := func(cfg, folder, sid, cwd string) string {
		p := filepath.Join(cfg, "projects", folder, sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(`{"type":"user","cwd":`+strconv.Quote(cwd)+`,"message":{"role":"user","content":"hi"}}`+"\n"), 0o600)
		return p
	}
	a, home, empty := filepath.Join(root, "a"), filepath.Join(root, "home"), filepath.Join(root, "empty")
	proj := filepath.Join(root, "Proj")
	other := filepath.Join(root, "Other")
	write(home, ProjectSlug(proj), "11111111-2222-4333-8444-000000000001", proj)
	otherFile := write(a, ProjectSlug(other), "11111111-2222-4333-8444-000000000002", other)

	if !HasTranscripts([]string{empty, a, home}, proj) || !HasTranscripts([]string{home}, strings.ToLower(proj)) {
		t.Fatal("the project's conversation was not found")
	}
	described.Lock()
	clear(described.m)
	described.Unlock()
	gone := filepath.Join(root, "Gone")
	if HasTranscripts([]string{empty, a, home}, gone) || HasTranscripts(nil, proj) || HasTranscripts([]string{a}, "") {
		t.Fatal("found a conversation of a project that has none")
	}
	described.Lock()
	_, read := described.m[otherFile]
	described.Unlock()
	if read {
		t.Fatal("another project's conversation was read")
	}

	// claude's folder names are lossy: a\b and a-b share one, and the cwd
	// written in the file decides.
	dashed, nested := filepath.Join(root, "x-y"), filepath.Join(root, "x", "y")
	write(a, ProjectSlug(dashed), "11111111-2222-4333-8444-000000000003", dashed)
	if HasTranscripts([]string{a}, nested) || !HasTranscripts([]string{a}, dashed) {
		t.Fatal("a folder shared by two projects")
	}

	// A name over 200 characters: cut, with a hash appended.
	long := filepath.Join(root, strings.Repeat("L", 210))
	write(a, ProjectSlug(long)[:slugMax]+"-9f8e7d", "11111111-2222-4333-8444-000000000004", long)
	if !HasTranscripts([]string{a}, long) {
		t.Fatal("long folder not found")
	}

	// A config folder that cannot be listed (here a name no system takes)
	// may hold one; one that is not there holds none.
	if !HasTranscripts([]string{"bad\x00name"}, gone) || HasTranscripts([]string{filepath.Join(root, "nowhere")}, gone) {
		t.Fatal("an unreadable config folder counted as empty, or a missing one as full")
	}
}
