package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// Every change this package makes on disk goes through the functions below:
// records in the Claude desktop app's folders, second names in the user's
// own Claude Code folder, and baton's index. Inside a Go test binary, a
// change anywhere but the temporary folder panics. The user's real data must
// never be reached from a test, whatever else a test gets wrong.

func guard(op, path string) {
	if !testing.Testing() {
		return
	}
	long := longPath(path)
	for _, own := range realData {
		if under(long, longPath(own)) {
			panic(fmt.Sprintf("desktop: a test tried to %s %s, in the user's own data", op, path))
		}
	}
	if under(long, longPath(os.TempDir())) {
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
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
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

// linkCount is how many names the file at path has (0 when that cannot be
// read). A second name is removed only from a file that has another one.
func linkCount(path string) uint32 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var fi windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &fi) != nil {
		return 0
	}
	return fi.NumberOfLinks
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
