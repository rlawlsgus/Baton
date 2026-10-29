package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Not a repository yet.
	if c, err := Read(dir, 10*time.Second); err != nil || c.Repo {
		t.Fatalf("plain folder: %+v %v", c, err)
	}
	git("init", "-q")
	write("keep.txt", "a\nb\nc\n")
	write("gone.txt", "x\n")
	write("old name.txt", "same\n")
	git("add", "-A")
	git("commit", "-q", "-m", "first")
	write("keep.txt", "a\nB\nc\nd\n") // 2 added, 1 removed
	_ = os.Remove(filepath.Join(dir, "gone.txt"))
	git("mv", "old name.txt", "새 이름.txt")
	write("sub/new.txt", "n\n")
	// Read from a subfolder: paths are the repository's.
	c, err := Read(filepath.Join(dir, "sub"), 10*time.Second)
	if err != nil || !c.Repo {
		t.Fatalf("%+v %v", c, err)
	}
	want := map[string][3]any{
		"gone.txt":    {"deleted", 0, 1},
		"keep.txt":    {"modified", 2, 1},
		"sub/new.txt": {"untracked", 1, 0},
		"새 이름.txt":    {"renamed", 0, 0},
	}
	if len(c.Files) != len(want) {
		t.Fatalf("files %+v", c.Files)
	}
	for _, f := range c.Files {
		w, ok := want[f.Path]
		if !ok || f.Status != w[0] || f.Added != w[1] || f.Removed != w[2] {
			t.Errorf("%+v, want %v", f, w)
		}
	}
	if c.Added != 3 || c.Removed != 2 {
		t.Fatalf("totals +%d -%d", c.Added, c.Removed)
	}
}
