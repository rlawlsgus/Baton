// Package recycle moves files and folders to the Windows Recycle Bin, and
// only there: an item the bin would not take is refused before anything is
// moved, never deleted for good.
package recycle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// Bin is a Recycle Bin. Preflight says whether it takes every one of paths,
// changing nothing. Recycle runs the same check, then moves paths one at a
// time in the order given and stops at the first that does not go; moved
// counts the items now in the bin. A path that is gone already is skipped.
//
// Holds counts the names of the file at path that are items in the bin.
// Recycling a file renames it into the bin on the same volume, so a file
// one of whose names went there still counts that name among its hard
// links (fsx.LinkCount). 0 when the bin can't be read.
type Bin interface {
	Preflight(paths []string) error
	Recycle(paths []string) (moved int, err error)
	Holds(path string) int
}

// holding counts the files right in dir whose names pass match and that are
// the file fi under another name.
func holding(dir string, fi os.FileInfo, match func(name string) bool) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.Type().IsRegular() || !match(e.Name()) {
			continue
		}
		if o, err := os.Lstat(filepath.Join(dir, e.Name())); err == nil && os.SameFile(o, fi) {
			n++
		}
	}
	return n
}

// New returns the Windows Recycle Bin. BATON_RECYCLE=<dir> names a folder
// that stands in for it instead (tests and the end-to-end test): items are
// moved into it and listed in its manifest. A Go test binary never gets the
// real one.
func New() Bin {
	if d := os.Getenv("BATON_RECYCLE"); d != "" {
		return fakeBin{dir: filepath.Clean(d)}
	}
	if testing.Testing() {
		panic("recycle: a test asked for the real Recycle Bin; set BATON_RECYCLE to a temporary folder")
	}
	return &shellBin{facts: osFacts()}
}

var (
	// ErrUnavailable: Windows can't put the item in the Recycle Bin of its
	// drive (a network drive, the bin turned off, more than the bin holds).
	ErrUnavailable = errors.New("the Recycle Bin can't take it")
	// ErrTooLong: the path is MAX_PATH or longer, which the shell can't move.
	ErrTooLong = errors.New("the path is too long for the Recycle Bin")
	// ErrInUse: another program kept the item open through every try.
	ErrInUse = errors.New("another program is using it")
	// ErrPartial matches an *Error that came after some items went.
	ErrPartial = errors.New("only part went to the Recycle Bin")

	// errGone: the item vanished between the check and the move.
	errGone = errors.New("gone already")
)

// Error says which item did not go to the Recycle Bin, and why.
type Error struct {
	Path  string // the item
	Drive string // its drive, such as "C:"
	Moved int    // items that went to the bin before this one
	Err   error  // wraps ErrUnavailable, ErrTooLong or ErrInUse, or is what Windows said
}

func (e *Error) Error() string {
	s := fmt.Sprintf("recycle %s: %v", e.Path, e.Err)
	if e.Moved > 0 {
		s += fmt.Sprintf(" (%d moved before it)", e.Moved)
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrPartial) true once anything has moved.
func (e *Error) Is(target error) bool { return target == ErrPartial && e.Moved > 0 }

// maxPath is MAX_PATH with its terminating NUL: the shell takes no longer
// path, and no \\?\ one.
const maxPath = 260

// An item another program holds is tried this many times, this far apart:
// the office and the window poll conversation files and let go quickly.
var (
	tries     = 3
	retryWait = 250 * time.Millisecond
)

// facts are what Preflight needs to know about the volume an item is on. The
// real bin asks Windows, the fake its own folder, and tests say what they
// like.
type facts struct {
	root      func(path string) (string, error)                        // where its volume is mounted, ending in a backslash
	fixed     func(root string) (bool, string)                         // a local fixed disk; else what it is
	hasBin    func(root string) bool                                   // the Recycle Bin answers for it
	policyOff func() bool                                              // a policy turned the Recycle Bin off
	settings  func(root string) (nuke bool, capacity int64, err error) // the bin's own settings; capacity in bytes, 0 if unknown
	scan      func(path string) (size int64, longest int)              // what the item holds, and its longest path inside
}

// preflight refuses paths unless the bin takes every one of them. All items
// on one volume together must fit in its bin: the bin would make room for a
// later item by deleting an earlier one for good. A folder with a path of
// MAX_PATH or longer inside is refused too: the shell would delete it for
// good rather than recycle it (seen on Windows 11).
func preflight(paths []string, f facts) error {
	type volume struct {
		err      error
		capacity int64
		total    int64
	}
	vols := map[string]*volume{}
	off := false
	if f.policyOff != nil {
		off = f.policyOff()
	}
	for _, p := range paths {
		if err := checkPath(p); err != nil {
			return &Error{Path: p, Err: err}
		}
		root, err := f.root(p)
		if err != nil {
			return &Error{Path: p, Err: fmt.Errorf("%w: its drive is unknown (%v)", ErrUnavailable, err)}
		}
		key := strings.ToLower(root)
		v := vols[key]
		if v == nil {
			v = &volume{}
			vols[key] = v
			switch fixed, what := f.fixed(root); {
			case off:
				v.err = fmt.Errorf("%w: a policy turned the Recycle Bin off", ErrUnavailable)
			case !fixed:
				v.err = fmt.Errorf("%w: %s", ErrUnavailable, what)
			case !f.hasBin(root):
				v.err = fmt.Errorf("%w: the drive has no Recycle Bin", ErrUnavailable)
			default:
				nuke, capacity, err := f.settings(root)
				switch {
				case err != nil:
					v.err = fmt.Errorf("%w: its settings can't be read (%v)", ErrUnavailable, err)
				case nuke:
					v.err = fmt.Errorf("%w: it is set to delete at once", ErrUnavailable)
				}
				v.capacity = capacity
			}
		}
		if v.err != nil {
			return &Error{Path: p, Drive: drive(root), Err: v.err}
		}
		size, longest := f.scan(p)
		if longest >= maxPath {
			return &Error{Path: p, Drive: drive(root), Err: fmt.Errorf("%w: a path inside has %d characters", ErrTooLong, longest)}
		}
		if v.capacity > 0 {
			v.total += size
			if v.total > v.capacity {
				return &Error{Path: p, Drive: drive(root), Err: fmt.Errorf("%w: more than its Recycle Bin holds", ErrUnavailable)}
			}
		}
	}
	return nil
}

// checkPath takes a plain absolute path shorter than MAX_PATH, both as given
// and spelled out in full (a short 8.3 name can hide a longer one).
func checkPath(p string) error {
	if !filepath.IsAbs(p) || strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return fmt.Errorf("not a plain absolute path")
	}
	if utf16Len(p) >= maxPath || utf16Len(longPath(p)) >= maxPath {
		return ErrTooLong
	}
	return nil
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// drive names a volume root for a message: "C:", or the folder it is
// mounted on.
func drive(root string) string {
	if d := strings.TrimRight(root, `\`); d != "" {
		return d
	}
	return root
}

// recycleEach moves paths one at a time with move, in order, and stops at
// the first that does not go. An item in use is tried again a few times.
func recycleEach(paths []string, move func(path string) error) (int, error) {
	moved := 0
	for _, p := range paths {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var err error
		for try := 1; ; try++ {
			if err = move(p); !errors.Is(err, ErrInUse) || try >= tries {
				break
			}
			time.Sleep(retryWait)
		}
		switch {
		case err == nil:
			moved++
		case errors.Is(err, errGone):
		default:
			e := &Error{Path: p, Err: err}
			if errors.As(err, &e) {
				e.Path = p
			}
			e.Moved = moved
			return moved, e
		}
	}
	return moved, nil
}

// scanTree is how many bytes a file, or everything in a folder, holds, and
// the length of the longest path in it, spelled out in full. Links inside
// are not followed: the bin moves them, not what they point at.
func scanTree(path string) (size int64, longest int) {
	filepath.WalkDir(longPath(path), func(p string, d fs.DirEntry, err error) error {
		longest = max(longest, utf16Len(p))
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
			}
		}
		return nil
	})
	return size, longest
}

// Inside a Go test binary a bin, real or fake, takes only what lies in the
// temporary folder, and never the user's own data even then. A test that
// got a path wrong must not move the user's conversations anywhere.
func guard(path string) {
	if !testing.Testing() {
		return
	}
	long := longPath(path)
	for _, own := range realData {
		if within(long, longPath(own)) {
			panic(fmt.Sprintf("recycle: a test tried to recycle %s, in the user's own data", path))
		}
	}
	if !within(long, longPath(os.TempDir())) {
		panic(fmt.Sprintf("recycle: a test tried to recycle %s, outside the temporary folder", path))
	}
}

// realData are the user's own folders, read before any test changes the
// environment.
var realData = func() []string {
	if !testing.Testing() {
		return nil
	}
	var out []string
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		out = append(out, filepath.Join(d, "Claude-Data"), filepath.Join(d, "AnthropicClaude"), filepath.Join(d, "baton"))
	}
	if d := os.Getenv("APPDATA"); d != "" {
		out = append(out, filepath.Join(d, "Claude"))
	}
	if d := os.Getenv("USERPROFILE"); d != "" {
		out = append(out, filepath.Join(d, ".claude"), filepath.Join(d, ".claude.json"))
	}
	return out
}()

// longPath spells path out in full: 8.3 short names (C:\Users\JOHNDO~1) in
// the part that exists are expanded, so two spellings of one folder compare
// and a length check sees the real one.
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rest := ""
	for d := abs; ; {
		if l, ok := longName(d); ok {
			return filepath.Join(l, rest)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs
		}
		rest = filepath.Join(filepath.Base(d), rest)
		d = parent
	}
}

func longName(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

// within reports whether path is dir or inside it, ignoring case.
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if strings.EqualFold(path, dir) {
		return true
	}
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir) &&
		(path[len(dir)] == filepath.Separator || strings.HasSuffix(dir, string(filepath.Separator)))
}
