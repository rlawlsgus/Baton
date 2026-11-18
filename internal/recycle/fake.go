package recycle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// fakeBin stands in for the Recycle Bin (BATON_RECYCLE): each item is
// renamed into a new folder of its own under dir, and a line naming where it
// came from is added to dir\manifest.jsonl. Like the real bin it only moves
// within a volume, so an item on another drive than dir is refused as one
// the bin can't take.
type fakeBin struct{ dir string }

// ManifestName is the fake bin's list of what it took, one JSON Entry per
// line, in the order the items went.
const ManifestName = "manifest.jsonl"

// Entry is one item in the fake bin: where it was and where it is now.
type Entry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ReadManifest lists what the fake bin in dir took, oldest first.
func ReadManifest(dir string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(dir, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return out, fmt.Errorf("%s: %w", ManifestName, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func (b fakeBin) facts() facts {
	home := strings.ToLower(volumeOf(b.dir))
	return facts{
		root:      func(p string) (string, error) { return volumeOf(p), nil },
		fixed:     func(string) (bool, string) { return true, "" },
		hasBin:    func(root string) bool { return strings.ToLower(root) == home },
		policyOff: func() bool { return false },
		settings:  func(string) (bool, int64, error) { return false, 0, nil },
		scan:      scanTree,
	}
}

// volumeOf is the drive part of a path with a backslash: C:\ or
// \\server\share\.
func volumeOf(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.VolumeName(p) + `\`
}

func (b fakeBin) Preflight(paths []string) error { return preflight(paths, b.facts()) }

// Holds looks in each item's folder: a file is kept there by its own name.
func (b fakeBin) Holds(path string) int {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return 0
	}
	items, _ := os.ReadDir(b.dir)
	n := 0
	for _, it := range items {
		if it.IsDir() && strings.HasPrefix(it.Name(), "item-") {
			n += holding(filepath.Join(b.dir, it.Name()), fi, func(string) bool { return true })
		}
	}
	return n
}

func (b fakeBin) Recycle(paths []string) (int, error) {
	for _, p := range paths {
		guard(p)
	}
	guard(b.dir)
	if err := b.Preflight(paths); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return 0, err
	}
	return recycleEach(paths, b.move)
}

func (b fakeBin) move(path string) error {
	hold, err := os.MkdirTemp(b.dir, "item-")
	if err != nil {
		return err
	}
	to := filepath.Join(hold, filepath.Base(path))
	if err := os.Rename(path, to); err != nil {
		os.Remove(hold)
		return renameError(err)
	}
	line, _ := json.Marshal(Entry{From: path, To: to})
	f, err := os.OpenFile(filepath.Join(b.dir, ManifestName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// renameError sorts out why a rename failed the way the real bin's are.
func renameError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errGone
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION),
		errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return ErrInUse
	case errors.Is(err, windows.ERROR_NOT_SAME_DEVICE):
		return fmt.Errorf("%w: another drive", ErrUnavailable)
	case errors.Is(err, windows.ERROR_FILENAME_EXCED_RANGE):
		return ErrTooLong
	}
	return err
}
