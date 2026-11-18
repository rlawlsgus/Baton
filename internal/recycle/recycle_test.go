package recycle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// fakeIn sets BATON_RECYCLE to a new folder and returns New()'s bin.
func fakeIn(t *testing.T) (Bin, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	t.Setenv("BATON_RECYCLE", dir)
	b := New()
	if _, ok := b.(fakeBin); !ok {
		t.Fatalf("New() = %T with BATON_RECYCLE set", b)
	}
	return b, dir
}

func quickRetries(t *testing.T, wait time.Duration) {
	t.Helper()
	old := retryWait
	retryWait = wait
	t.Cleanup(func() { retryWait = old })
}

func TestNewNeverRealInTests(t *testing.T) {
	t.Setenv("BATON_RECYCLE", "")
	defer func() {
		if recover() == nil {
			t.Fatal("New() gave a test the real Recycle Bin")
		}
	}()
	New()
}

func TestFakeMovesInOrder(t *testing.T) {
	b, dir := fakeIn(t)
	src := t.TempDir()
	file := filepath.Join(src, "a.jsonl")
	folder := filepath.Join(src, "a")
	gone := filepath.Join(src, "gone.json")
	keep := filepath.Join(src, "keep.jsonl")
	write(t, file, "one")
	write(t, filepath.Join(folder, "subagents", "agent-1.jsonl"), "two")
	write(t, keep, "kept")

	if err := b.Preflight([]string{file, folder, gone}); err != nil {
		t.Fatal(err)
	}
	moved, err := b.Recycle([]string{folder, gone, file})
	if err != nil || moved != 2 {
		t.Fatalf("Recycle = %d, %v; want 2, nil", moved, err)
	}
	if exists(file) || exists(folder) || !exists(keep) {
		t.Fatal("the items are not gone, or something else went with them")
	}
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[0].From != folder || m[1].From != file {
		t.Fatalf("manifest = %+v; want the folder, then the file", m)
	}
	if data, err := os.ReadFile(filepath.Join(m[0].To, "subagents", "agent-1.jsonl")); err != nil || string(data) != "two" {
		t.Fatalf("the folder in the bin: %q, %v", data, err)
	}
	if data, err := os.ReadFile(m[1].To); err != nil || string(data) != "one" {
		t.Fatalf("the file in the bin: %q, %v", data, err)
	}

	// Another delete into the same bin adds to the manifest; a name taken
	// already in the bin is no trouble.
	write(t, file, "again")
	if moved, err := b.Recycle([]string{file}); err != nil || moved != 1 {
		t.Fatalf("second Recycle = %d, %v", moved, err)
	}
	if m, _ := ReadManifest(dir); len(m) != 3 || m[2].From != file || m[2].To == m[1].To {
		t.Fatalf("manifest after the second = %+v", m)
	}
}

func TestReadManifestEmpty(t *testing.T) {
	m, err := ReadManifest(t.TempDir())
	if err != nil || m != nil {
		t.Fatalf("ReadManifest of an empty bin = %v, %v", m, err)
	}
}

// An item another program holds stops the rest: what went before stays in
// the bin, what comes after stays in place, and a retry finishes the job.
func TestFakePartial(t *testing.T) {
	quickRetries(t, time.Millisecond)
	b, dir := fakeIn(t)
	src := t.TempDir()
	a, held, c := filepath.Join(src, "a"), filepath.Join(src, "held"), filepath.Join(src, "c")
	write(t, filepath.Join(a, "x"), "a")
	write(t, held, "b")
	write(t, c, "c")
	f, err := os.Open(held) // without FILE_SHARE_DELETE, like claude.Describe
	if err != nil {
		t.Fatal(err)
	}
	moved, err := b.Recycle([]string{a, held, c})
	f.Close()
	if moved != 1 {
		t.Fatalf("moved %d; want 1", moved)
	}
	var e *Error
	if !errors.As(err, &e) || e.Path != held || e.Moved != 1 {
		t.Fatalf("err = %#v; want an *Error for %s after 1", err, held)
	}
	if !errors.Is(err, ErrInUse) || !errors.Is(err, ErrPartial) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v; want in use and partial", err)
	}
	if exists(a) || !exists(held) || !exists(c) {
		t.Fatal("the bin did not stop at the item in use")
	}
	if m, _ := ReadManifest(dir); len(m) != 1 || m[0].From != a {
		t.Fatalf("manifest = %+v", m)
	}

	moved, err = b.Recycle([]string{a, held, c})
	if err != nil || moved != 2 || exists(held) || exists(c) {
		t.Fatalf("retry = %d, %v", moved, err)
	}
}

func TestFakeInUseFirstIsNotPartial(t *testing.T) {
	quickRetries(t, time.Millisecond)
	b, _ := fakeIn(t)
	src := t.TempDir()
	held, other := filepath.Join(src, "held"), filepath.Join(src, "other")
	write(t, held, "b")
	write(t, other, "c")
	f, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	moved, err := b.Recycle([]string{held, other})
	if moved != 0 || !errors.Is(err, ErrInUse) || errors.Is(err, ErrPartial) || !exists(other) {
		t.Fatalf("Recycle = %d, %v; want 0 and in use, not partial", moved, err)
	}
}

// A folder with a file open below it cannot be moved either.
func TestFakeFolderInUse(t *testing.T) {
	quickRetries(t, time.Millisecond)
	b, _ := fakeIn(t)
	folder := filepath.Join(t.TempDir(), "id")
	inner := filepath.Join(folder, "subagents", "agent-1.jsonl")
	write(t, inner, "x")
	f, err := os.Open(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if moved, err := b.Recycle([]string{folder}); moved != 0 || !errors.Is(err, ErrInUse) {
		t.Fatalf("Recycle = %d, %v; want in use", moved, err)
	}
}

// An item let go of between tries goes on a later try.
func TestFakeRetriesInUse(t *testing.T) {
	quickRetries(t, 200*time.Millisecond)
	b, _ := fakeIn(t)
	held := filepath.Join(t.TempDir(), "held")
	write(t, held, "b")
	f, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(50*time.Millisecond, func() { f.Close() })
	if moved, err := b.Recycle([]string{held}); err != nil || moved != 1 {
		t.Fatalf("Recycle = %d, %v; want it moved once let go", moved, err)
	}
}

// Nothing moves when any item is refused, wherever it is in the list.
func TestFakeRefusesBeforeMoving(t *testing.T) {
	b, dir := fakeIn(t)
	src := t.TempDir()
	first := filepath.Join(src, "first")
	write(t, first, "a")
	long := filepath.Join(src, strings.Repeat("x", maxPath))
	moved, err := b.Recycle([]string{first, long})
	if moved != 0 || !errors.Is(err, ErrTooLong) || !exists(first) {
		t.Fatalf("Recycle = %d, %v; want nothing moved and too long", moved, err)
	}
	other := `Z:\conversation.jsonl`
	if strings.EqualFold(volumeOf(dir), `Z:\`) {
		other = `Y:\conversation.jsonl`
	}
	err = b.Preflight([]string{first, other})
	var e *Error
	if !errors.As(err, &e) || !errors.Is(err, ErrUnavailable) || e.Drive != other[:2] || e.Path != other {
		t.Fatalf("an item on another drive: %v", err)
	}
	if m, _ := ReadManifest(dir); m != nil {
		t.Fatalf("manifest = %+v; want nothing", m)
	}
}

// A folder with a path of MAX_PATH or longer inside is refused whole: the
// shell would delete it for good instead of recycling it.
func TestFakeRefusesDeepFolder(t *testing.T) {
	b, dir := fakeIn(t)
	folder := filepath.Join(t.TempDir(), "id")
	write(t, filepath.Join(folder, "short.txt"), "x")
	if err := b.Preflight([]string{folder}); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(folder, "tool-results", strings.Repeat("d", maxPath-utf16Len(longPath(folder)))+".txt")
	write(t, deep, "x")
	moved, err := b.Recycle([]string{folder})
	var e *Error
	if moved != 0 || !errors.Is(err, ErrTooLong) || !errors.As(err, &e) || e.Path != folder || !exists(deep) {
		t.Fatalf("Recycle = %d, %v; want refused as too long, nothing moved", moved, err)
	}
	if m, _ := ReadManifest(dir); m != nil {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestScanTree(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "b.txt"), "12345")
	write(t, filepath.Join(dir, "c.txt"), "678")
	size, longest := scanTree(dir)
	if want := utf16Len(filepath.Join(longPath(dir), "a", "b.txt")); size != 8 || longest != want {
		t.Fatalf("scanTree = %d, %d; want 8, %d", size, longest, want)
	}
	if size, _ := scanTree(filepath.Join(dir, "c.txt")); size != 3 {
		t.Fatalf("scanTree of a file = %d", size)
	}
	if size, _ := scanTree(filepath.Join(dir, "missing")); size != 0 {
		t.Fatalf("scanTree of nothing = %d", size)
	}
}

func TestGuard(t *testing.T) {
	b, _ := fakeIn(t)
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), ".claude", "projects", "baton-recycle-guard-test-not-there.jsonl"),
		filepath.Join(home, "baton-recycle-guard-test-not-there"),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a test could recycle %s", p)
				}
			}()
			b.Recycle([]string{p})
		}()
	}
}

// facts for preflight tests: a C: drive with a bin of 100 bytes, a D: drive
// with an unlimited one; the rest is set per case.
type fakeFacts struct {
	remote, removable, noBin, policy, nuke, unreadable bool
	asked                                              map[string]int
}

func (ff *fakeFacts) facts() facts {
	ff.asked = map[string]int{}
	return facts{
		root: func(p string) (string, error) {
			if strings.HasPrefix(p, `Q:`) {
				return "", errors.New("no such volume")
			}
			return p[:3], nil
		},
		fixed: func(root string) (bool, string) {
			ff.asked[root]++
			switch {
			case ff.remote:
				return false, "a network drive"
			case ff.removable:
				return false, "a removable drive"
			}
			return true, ""
		},
		hasBin:    func(string) bool { return !ff.noBin },
		policyOff: func() bool { return ff.policy },
		settings: func(root string) (bool, int64, error) {
			if ff.unreadable {
				return false, 0, errors.New("access denied")
			}
			if root == `C:\` {
				return ff.nuke, 100, nil
			}
			return ff.nuke, 0, nil
		},
		scan: func(p string) (int64, int) {
			switch filepath.Base(p) {
			case "big":
				return 60, len(p)
			case "huge":
				return 1 << 40, len(p)
			case "deep":
				return 1, maxPath
			}
			return 1, len(p)
		},
	}
}

func TestPreflight(t *testing.T) {
	a, b := `C:\Users\u\.claude\projects\p\a.jsonl`, `D:\conversations\b.jsonl`
	cases := []struct {
		name  string
		ff    fakeFacts
		paths []string
		want  error  // nil, ErrUnavailable or ErrTooLong
		drive string // on an ErrUnavailable
		path  string // the item refused
	}{
		{name: "fine", paths: []string{a, b}},
		{name: "network drive", ff: fakeFacts{remote: true}, paths: []string{a}, want: ErrUnavailable, drive: "C:", path: a},
		{name: "removable drive", ff: fakeFacts{removable: true}, paths: []string{b}, want: ErrUnavailable, drive: "D:", path: b},
		{name: "no bin", ff: fakeFacts{noBin: true}, paths: []string{a}, want: ErrUnavailable, drive: "C:", path: a},
		{name: "policy", ff: fakeFacts{policy: true}, paths: []string{b}, want: ErrUnavailable, drive: "D:", path: b},
		{name: "nuke on delete", ff: fakeFacts{nuke: true}, paths: []string{a}, want: ErrUnavailable, drive: "C:", path: a},
		{name: "settings unreadable", ff: fakeFacts{unreadable: true}, paths: []string{a}, want: ErrUnavailable, drive: "C:", path: a},
		{name: "unknown volume", paths: []string{a, `Q:\x`}, want: ErrUnavailable, path: `Q:\x`},
		{name: "fits", paths: []string{`C:\x\big`, `C:\x\y`, `D:\huge`}},
		{name: "more than the bin together", paths: []string{`C:\x\big`, `D:\huge`, `C:\y\big`}, want: ErrUnavailable, drive: "C:", path: `C:\y\big`},
		{name: "too big alone", paths: []string{`C:\x\huge`}, want: ErrUnavailable, drive: "C:", path: `C:\x\huge`},
		{name: "too long", paths: []string{a, `C:\` + strings.Repeat("x", maxPath-3)}, want: ErrTooLong, path: `C:\` + strings.Repeat("x", maxPath-3)},
		{name: "just short enough", paths: []string{`C:\` + strings.Repeat("x", maxPath-4)}},
		{name: "too long inside", paths: []string{a, `D:\x\deep`}, want: ErrTooLong, drive: "D:", path: `D:\x\deep`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := preflight(c.paths, c.ff.facts())
			if c.want == nil {
				if err != nil {
					t.Fatalf("preflight = %v; want nil", err)
				}
				return
			}
			var e *Error
			if !errors.Is(err, c.want) || !errors.As(err, &e) {
				t.Fatalf("preflight = %v; want %v", err, c.want)
			}
			if e.Drive != c.drive || e.Path != c.path || e.Moved != 0 || errors.Is(err, ErrPartial) {
				t.Fatalf("preflight = %#v; want drive %q, path %q", e, c.drive, c.path)
			}
		})
	}
}

func TestPreflightAsksOncePerVolume(t *testing.T) {
	var ff fakeFacts
	f := ff.facts()
	if err := preflight([]string{`C:\a`, `c:\b`, `D:\c`, `C:\d`}, f); err != nil {
		t.Fatal(err)
	}
	if ff.asked[`C:\`]+ff.asked[`c:\`] != 1 || ff.asked[`D:\`] != 1 {
		t.Fatalf("asked %v; want once per volume", ff.asked)
	}
}

func TestCheckPath(t *testing.T) {
	for _, p := range []string{`relative\path`, `\\?\C:\x`, `\\.\C:\x`, `\no-drive`} {
		if err := checkPath(p); err == nil || errors.Is(err, ErrTooLong) {
			t.Errorf("checkPath(%q) = %v; want refused as not plain", p, err)
		}
	}
	// The temporary folder is often spelled with a short 8.3 name; the
	// length that counts is the full one.
	tmp := os.TempDir()
	full := longPath(tmp)
	if utf16Len(full) <= utf16Len(tmp) {
		t.Skipf("the temporary folder %s has no shorter spelling", tmp)
	}
	p := tmp + `\` + strings.Repeat("y", maxPath-1-utf16Len(tmp)-1)
	if utf16Len(p) != maxPath-1 {
		t.Fatalf("len %d", utf16Len(p))
	}
	if err := checkPath(p); !errors.Is(err, ErrTooLong) {
		t.Fatalf("checkPath of %d characters, %d spelled out = %v; want too long", utf16Len(p), utf16Len(longPath(p)), err)
	}
}

func TestErrorText(t *testing.T) {
	e := &Error{Path: `C:\x`, Drive: "C:", Moved: 2, Err: ErrInUse}
	if !errors.Is(e, ErrPartial) || !errors.Is(e, ErrInUse) || !strings.Contains(e.Error(), "2 moved") {
		t.Fatalf("%v", e)
	}
	e.Moved = 0
	if errors.Is(e, ErrPartial) || strings.Contains(e.Error(), "moved") {
		t.Fatalf("%v", e)
	}
}

func TestShellErrors(t *testing.T) {
	b := &shellBin{facts: osFacts()}
	missing := filepath.Join(t.TempDir(), "not-there")
	for _, c := range []struct {
		hr   uint32
		want error
	}{
		{0x80070020, ErrInUse},   // ERROR_SHARING_VIOLATION
		{0x80070021, ErrInUse},   // ERROR_LOCK_VIOLATION
		{0x80070005, ErrInUse},   // ERROR_ACCESS_DENIED
		{0x800700CE, ErrTooLong}, // ERROR_FILENAME_EXCED_RANGE
		{0x80070002, errGone},    // ERROR_FILE_NOT_FOUND, and it is gone
		{0x80270027, ErrInUse},   // COPYENGINE_E_SHARING_VIOLATION_SRC
		{0x80270021, ErrInUse},   // COPYENGINE_E_ACCESS_DENIED_SRC
		{0x8027001D, ErrTooLong}, // COPYENGINE_E_PATH_TOO_DEEP_SRC
		{0x80270037, ErrUnavailable},
		{0x80270023, errGone}, // COPYENGINE_E_PATH_NOT_FOUND_SRC, and it is gone
	} {
		if err := b.failed(missing, c.hr); !errors.Is(err, c.want) {
			t.Errorf("failed(%#x) = %v; want %v", c.hr, err, c.want)
		}
	}
	if err := b.failed(missing, 0x80004005); err == nil || errors.Is(err, ErrInUse) || errors.Is(err, errGone) {
		t.Errorf("E_FAIL = %v", err)
	}
	// Not found, yet still there: not to be skipped.
	there := filepath.Join(t.TempDir(), "there")
	write(t, there, "x")
	if err := b.failed(there, 0x80070002); err == nil || errors.Is(err, errGone) {
		t.Errorf("not found but there = %v", err)
	}
}

func TestVolumeRoot(t *testing.T) {
	dir := t.TempDir()
	root, err := volumeRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(root, `\`) || !within(longPath(dir), root) {
		t.Fatalf("volumeRoot(%s) = %s", dir, root)
	}
}

// A name the fake bin took is one it holds; the file's other name, and a
// file that was never in it, are not.
func TestFakeHolds(t *testing.T) {
	b, _ := fakeIn(t)
	src := t.TempDir()
	a, h := filepath.Join(src, "a.jsonl"), filepath.Join(src, "home", "a.jsonl")
	write(t, a, "x")
	if err := os.MkdirAll(filepath.Dir(h), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, h); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(src, "b.jsonl")
	write(t, other, "x")
	if n := b.Holds(a); n != 0 {
		t.Fatalf("before: %d", n)
	}
	if _, err := b.Recycle([]string{h, other}); err != nil {
		t.Fatal(err)
	}
	if n := b.Holds(a); n != 1 {
		t.Fatalf("after its second name went: %d", n)
	}
	if n := b.Holds(filepath.Join(src, "none.jsonl")); n != 0 {
		t.Fatalf("no file: %d", n)
	}
}

// The real bin is read where Windows keeps the user's items on the
// volume, <volume>\$Recycle.Bin\<sid>\$R..., by name and file only. A
// stand-in volume root here: the real bin is never looked at.
func TestShellHolds(t *testing.T) {
	root := t.TempDir()
	const sid = "S-1-5-21-1-2-3-1001"
	b := &shellBin{facts: facts{root: func(string) (string, error) { return root + `\`, nil }},
		sid: func() (string, error) { return sid, nil }}
	a := filepath.Join(root, "Users", "u", ".claude", "projects", "C--Proj", "a.jsonl")
	write(t, a, "x")
	if n := b.Holds(a); n != 0 {
		t.Fatalf("no bin yet: %d", n)
	}
	mine := filepath.Join(root, "$Recycle.Bin", sid)
	for _, name := range []string{"$RX1Y2Z3.jsonl", "$IX1Y2Z3.jsonl", "$RQQQQQQ.txt"} {
		if err := os.MkdirAll(mine, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(a, filepath.Join(mine, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(mine, "$RAAAAAA.jsonl"), "another file")
	// Another user's part of the bin is not this user's.
	theirs := filepath.Join(root, "$Recycle.Bin", "S-1-5-21-1-2-3-1002")
	if err := os.MkdirAll(theirs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, filepath.Join(theirs, "$RB1B2B3.jsonl")); err != nil {
		t.Fatal(err)
	}
	if n := b.Holds(a); n != 1 {
		t.Fatalf("holds %d, want the one $R item with its extension", n)
	}
}
