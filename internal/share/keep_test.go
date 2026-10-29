package share

import (
	"os"
	"path/filepath"
	"testing"

	"baton/internal/fsx"
)

// Removing an account or baton keeps the conversations: they move into the
// user's own Claude Code, which never loses a file of its own.
func TestKeepConversations(t *testing.T) {
	root := t.TempDir()
	prof, home := filepath.Join(root, "profile"), filepath.Join(root, "home")
	write := func(p, body string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			return "<missing>"
		}
		return string(b)
	}
	// In the profile: two conversations in a folder the user also has, one in
	// a new folder, a session's subagent folder, memory and a checkpoint.
	write(filepath.Join(prof, "projects", "C--p", "s1.jsonl"), "baton one")
	write(filepath.Join(prof, "projects", "C--p", "s1", "subagents", "a.jsonl"), "sub")
	write(filepath.Join(prof, "projects", "C--p", "same.jsonl"), "identical")
	write(filepath.Join(prof, "projects", "C--p", "clash.jsonl"), "from baton")
	write(filepath.Join(prof, "projects", "C--p", "memory", "MEMORY.md"), "baton memory")
	write(filepath.Join(prof, "projects", "C--q", "s2.jsonl"), "baton two")
	write(filepath.Join(prof, "file-history", "s1", "x@v1"), "checkpoint")
	// The user's own setup already has some of it.
	write(filepath.Join(home, "projects", "C--p", "mine.jsonl"), "mine")
	write(filepath.Join(home, "projects", "C--p", "same.jsonl"), "identical")
	write(filepath.Join(home, "projects", "C--p", "clash.jsonl"), "the user's")
	write(filepath.Join(home, "projects", "C--p", "memory", "MEMORY.md"), "user memory")
	// A link inside the profile (as baton makes for skills) is not followed.
	other := filepath.Join(root, "elsewhere")
	write(filepath.Join(other, "secret.txt"), "not moved")
	if err := fsx.CreateJunction(filepath.Join(prof, "projects", "link"), other); err != nil {
		t.Fatal(err)
	}

	n, err := KeepConversations(prof, home, "work")
	if err != nil || n != 4 {
		t.Fatalf("moved %d, %v", n, err)
	}
	for p, want := range map[string]string{
		filepath.Join(home, "projects", "C--p", "s1.jsonl"):                         "baton one",
		filepath.Join(home, "projects", "C--p", "s1", "subagents", "a.jsonl"):       "sub",
		filepath.Join(home, "projects", "C--p", "mine.jsonl"):                       "mine",
		filepath.Join(home, "projects", "C--p", "same.jsonl"):                       "identical",
		filepath.Join(home, "projects", "C--p", "clash.jsonl"):                      "the user's",
		filepath.Join(home, "projects", "C--p", "clash (baton work).jsonl"):         "from baton",
		filepath.Join(home, "projects", "C--p", "memory", "MEMORY.md"):              "user memory",
		filepath.Join(home, "projects", "C--p", "memory", "MEMORY (baton work).md"): "baton memory",
		filepath.Join(home, "projects", "C--q", "s2.jsonl"):                         "baton two",
		filepath.Join(home, "file-history", "s1", "x@v1"):                           "checkpoint",
		filepath.Join(other, "secret.txt"):                                          "not moved",
	} {
		if got := read(p); got != want {
			t.Errorf("%s = %q, want %q", p, got, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "projects", "link")); err == nil {
		t.Error("a link from the profile was moved")
	}
	// Moving again finds nothing more to move.
	if n, err := KeepConversations(prof, home, "work"); err != nil || n != 0 {
		t.Fatalf("second run moved %d, %v", n, err)
	}
}
