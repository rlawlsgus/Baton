package fsx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWriteFileAtomicReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "f.json")
	if err := WriteFileAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "*.tmp")); len(m) > 0 {
		t.Fatalf("temp files left: %v", m)
	}
}

// A save must succeed while another baton process is reading the file.
func TestWriteFileAtomicWhileSharedReaderOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := WriteFileAtomic(p, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ptr, _ := windows.UTF16PtrFromString(p)
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if err := posixRename(writeTemp(t, filepath.Dir(p), `{"v":2}`), p); err != nil {
		t.Fatalf("replace with a share-delete reader open: %v", err)
	}
	b, err := ReadFileShared(p)
	if err != nil || string(b) != `{"v":2}` {
		t.Fatalf("got %q %v", b, err)
	}
	if _, err := ReadFileShared(filepath.Join(filepath.Dir(p), "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing file should be ErrNotExist, got %v", err)
	}
}

func writeTemp(t *testing.T, dir, s string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
	return f.Name()
}

func TestLinkCount(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := LinkCount(a); n != 1 {
		t.Fatalf("one name: %d", n)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	if n := LinkCount(b); n != 2 {
		t.Fatalf("two names: %d", n)
	}
	if n := LinkCount(filepath.Join(dir, "none")); n != 0 {
		t.Fatalf("no file: %d", n)
	}
	// A folder has one name too (it is opened as a folder, not refused).
	if n := LinkCount(dir); n != 1 {
		t.Fatalf("folder: %d", n)
	}
}

// Two names of one file have one id, which stays when a name moves; another
// file has another.
func TestFileID(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	for _, p := range []string{a, c} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	ia, ok1 := FileID(a)
	ib, ok2 := FileID(b)
	ic, ok3 := FileID(c)
	if !ok1 || !ok2 || !ok3 || ia != ib || ia == ic {
		t.Fatalf("a %v %v, b %v %v, c %v %v", ia, ok1, ib, ok2, ic, ok3)
	}
	moved := filepath.Join(dir, "moved")
	if err := os.Rename(a, moved); err != nil {
		t.Fatal(err)
	}
	if im, ok := FileID(moved); !ok || im != ia {
		t.Fatalf("moved: %v %v", im, ok)
	}
	if _, ok := FileID(a); ok {
		t.Fatal("no file, yet an id")
	}
}

func TestIsWithin(t *testing.T) {
	root := `C:\data\baton\profiles`
	for p, want := range map[string]bool{
		`C:\data\baton\profiles\a`:    true,
		`C:\data\baton\profiles\a\b`:  true,
		`C:\data\baton\profiles`:      false,
		`C:\data\baton\profiles\..\x`: false,
		`C:\Users\me\.claude`:         false,
		`C:\data\baton\profilesX\a`:   false,
	} {
		if got := IsWithin(p, root); got != want {
			t.Errorf("IsWithin(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRemoveTreeSafeClearsReadOnly(t *testing.T) {
	d := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(filepath.Join(d, "ro"), 0o700)
	f := filepath.Join(d, "ro", "f")
	_ = os.WriteFile(f, []byte("x"), 0o400) // read-only file
	if err := RemoveTreeSafe(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatalf("still exists: %v", err)
	}
}

// A profile may contain a junction into a shared store; deleting the profile
// must remove the junction only, never the store's contents.
func TestRemoveTreeSafeDoesNotFollowJunctions(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "store")
	keep := filepath.Join(store, "projects", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(keep), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(keep, []byte("conversation"), 0o600)
	profile := filepath.Join(root, "profile")
	_ = os.MkdirAll(filepath.Join(profile, "sub"), 0o700)
	_ = os.WriteFile(filepath.Join(profile, "sub", "x"), []byte("x"), 0o600)
	link := filepath.Join(profile, "projects")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(store, "projects")).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v %s", err, out)
	}
	if err := RemoveTreeSafe(profile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile still exists: %v", err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "conversation" {
		t.Fatalf("store content was touched: %v %q", err, b)
	}
}
