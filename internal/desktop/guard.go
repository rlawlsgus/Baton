package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// Every change this package makes on disk goes through the functions below:
// records in the Claude desktop app's folders, second names in the user's
// own Claude Code folder, and baton's index. Inside a Go test binary, a
// change anywhere but the temporary folder panics. The user's real data must
// never be reached from a test, whatever else a test gets wrong.

// Guard is guard, for internal/desklogin, which changes the desktop app's
// data folder too: every change it makes goes through it.
func Guard(op, path string) { guard(op, path) }

func guard(op, path string) {
	if !testing.Testing() {
		return
	}
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		temp = os.TempDir()
	}
	if inTemp(path, temp) {
		return
	}
	long := longPath(path)
	for _, own := range realLong {
		if under(long, own) {
			panic(fmt.Sprintf("desktop: a test tried to %s %s, in the user's own data", op, path))
		}
	}
	if under(long, tempLong(temp)) {
		return
	}
	panic(fmt.Sprintf("desktop: a test tried to %s %s, outside the temporary folder", op, path))
}

// realData are the user's own folders this feature reaches when it runs for
// real. A test never changes anything there, even if the temporary folder
// were set to one of them; they are read before any test changes the
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

// realLong are realData spelled out in full, once: spelling a path out asks
// the file system about each of its folders (about a millisecond each time),
// and these do not change while the tests run. A folder that is not there
// yet is spelled out as far as it exists, which is how it reads once made.
var realLong = func() []string {
	var out []string
	for _, d := range realData {
		out = append(out, longPath(d))
	}
	return out
}()

// tempLongs holds the temporary folder spelled out in full, by its absolute
// name in the test environment (a test may name another one).
var tempLongs sync.Map

func tempLong(dir string) string {
	if l, ok := tempLongs.Load(dir); ok {
		return l.(string)
	}
	l := longPath(dir)
	tempLongs.Store(dir, l)
	return l
}

// inTemp answers at once for a path written under the temporary folder as
// the test environment names it, which is where nearly every change a test
// makes is: spelling such a path out only spells out the temporary folder
// in front of the rest, so it is in the temporary folder in full too, and,
// while that folder and the user's real data are apart (neither is in the
// other), in none of the real data. Anything else is spelled out in full.
// Letters are matched in ASCII case only: NTFS tells apart letters that
// Unicode folds together (ſ and s), so a look-alike of the temporary
// folder's name is never taken for it. The answers are kept by name for the
// whole run: a short name (JOHNDO~1) that comes to mean another folder
// while the tests run is not looked for.
func inTemp(path, temp string) bool {
	abs, err := filepath.Abs(path)
	if err != nil || !underASCII(abs, temp) {
		return false
	}
	return tempApart(temp)
}

// underASCII is under with letters matched in ASCII case only.
func underASCII(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if len(path) < len(dir) || !asciiFold(path[:len(dir)], dir) {
		return false
	}
	return len(path) == len(dir) || path[len(dir)] == filepath.Separator || strings.HasSuffix(dir, string(filepath.Separator))
}

func asciiFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// tempAparts holds, by the temporary folder's name, whether it and every
// real data folder are apart, spelled out in full.
var tempAparts sync.Map

func tempApart(dir string) bool {
	if a, ok := tempAparts.Load(dir); ok {
		return a.(bool)
	}
	l, apart := tempLong(dir), true
	for _, own := range realLong {
		if under(l, own) || under(own, l) {
			apart = false
		}
	}
	tempAparts.Store(dir, apart)
	return apart
}

// under reports whether path is dir or inside it, ignoring case.
func under(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if strings.EqualFold(path, dir) {
		return true
	}
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir) &&
		(path[len(dir)] == filepath.Separator || strings.HasSuffix(dir, string(filepath.Separator)))
}

// longPath spells path out in full: 8.3 short names (C:\Users\JOHNDO~1) in
// the part that exists are expanded, so two spellings of one folder compare.
//
// Spelling a path out asks the file system about each of its folders (about
// a millisecond each time), and baton compares the paths of every record and
// conversation at every look. So the folder a path is in is spelled out once
// and kept (longDir), and the name in it is taken as it is: a short name has
// a ~ in it (a name without one that fits 8.3 is its own short name, but for
// case). A name with a ~ is asked about each time.
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	dir, base := filepath.Split(abs)
	if base == "" || dir == "" || strings.Contains(base, "~") {
		return longPathAsked(abs) // a root, or a name that may be a short one
	}
	return filepath.Join(longDir(filepath.Clean(dir)), base)
}

// longDirs keeps folders spelled out in full (longDir), by their name as
// asked, in lower case. A folder's long name does not change while baton
// runs; one that is not there is never kept.
var longDirs struct {
	sync.Mutex
	m map[string]string
}

// longDir is folder dir spelled out in full, from longDirs when it was
// spelled out before.
func longDir(dir string) string {
	key := strings.ToLower(dir)
	longDirs.Lock()
	l, ok := longDirs.m[key]
	longDirs.Unlock()
	if ok {
		return l
	}
	l, there := longName(dir)
	if !there {
		return longPathAsked(dir) // not there (yet): as far as it exists, and not kept
	}
	longDirs.Lock()
	if longDirs.m == nil || len(longDirs.m) > 4096 {
		longDirs.m = map[string]string{}
	}
	longDirs.m[key] = l
	longDirs.Unlock()
	return l
}

// longPathAsked spells abs out in full by asking the file system, as far as
// it exists.
func longPathAsked(abs string) string {
	rest := ""
	for d := abs; ; {
		if l, ok := longName(d); ok {
			if rest == "" {
				return l
			}
			return filepath.Join(l, rest)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs
		}
		if rest == "" {
			rest = filepath.Base(d)
		} else {
			rest = filepath.Join(filepath.Base(d), rest)
		}
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

func mkdirAll(dir string) error {
	guard("create", dir)
	return os.MkdirAll(dir, 0o700)
}

// removeFile removes one file; one that is gone already is fine.
func removeFile(path string) error {
	guard("remove", path)
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// link gives the file at oldname a second name.
func link(oldname, newname string) error {
	guard("link", newname)
	return os.Link(oldname, newname)
}

func writeIndex(path string, v any) error {
	guard("write", path)
	return fsx.WriteJSONAtomic(path, v)
}

// writeRecord writes data to path through a temporary file in the same
// folder, then renames it into place. The temporary name is never one the
// desktop app takes for its own: at its next start the app promotes a left
// over local_*.json.tmp to a record, and half a file from baton must never
// become one. With replace false an existing file is left alone, and the
// error says os.ErrExist.
func writeRecord(path string, data []byte, replace bool) error {
	guard("write", path)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".baton-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		if replace {
			werr = fsx.Replace(name, path)
		} else {
			werr = moveNew(name, path)
		}
	}
	if werr != nil {
		os.Remove(name)
	}
	return werr
}

// moveNew renames from to to only if to does not exist: a record that
// turned up meanwhile is the desktop app's, never replaced.
func moveNew(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, 0); err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrExist}
		}
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
