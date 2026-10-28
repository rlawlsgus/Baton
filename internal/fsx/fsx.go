// Package fsx has small filesystem helpers that are safe for files other
// processes (claude.exe, other baton instances) read concurrently.
package fsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WriteFileAtomic writes data to a temp file in the same directory and
// renames it over path, so readers see either the old or the new content.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	_ = os.Chmod(tmpName, perm)
	if err := replace(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// replace renames from over to. A POSIX-semantics rename works even while
// another process holds the destination open (a reader, `tail -f`, an
// indexer); plain MoveFileEx fails then, so it is only the fallback.
func replace(from, to string) error {
	if posixRename(from, to) == nil {
		return nil
	}
	// Bounded (~1 s): callers may hold a lock other baton processes wait on.
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(20*(i+1)) * time.Millisecond)
	}
	return err
}

type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

const (
	fileRenameInfoEx              = 22
	fileRenameFlagReplaceIfExists = 0x1
	fileRenameFlagPosixSemantics  = 0x2
)

func posixRename(from, to string) error {
	absTo, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	name := utf16.Encode([]rune(absTo))
	off := unsafe.Offsetof(fileRenameInfo{}.FileName)
	buf := make([]byte, int(off)+len(name)*2+2)
	if len(buf) < int(unsafe.Sizeof(fileRenameInfo{})) {
		buf = make([]byte, unsafe.Sizeof(fileRenameInfo{}))
	}
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = fileRenameFlagReplaceIfExists | fileRenameFlagPosixSemantics
	info.FileNameLength = uint32(len(name) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[off])), len(name)), name)
	return windows.SetFileInformationByHandle(h, fileRenameInfoEx, &buf[0], uint32(len(buf)))
}

func WriteJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// ReadFileShared reads a whole file opened with FILE_SHARE_DELETE, so a
// concurrent WriteFileAtomic can still replace it while it is being read
// (os.ReadFile's handle would block that replacement).
func ReadFileShared(path string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(f)
}

// StripBOM drops a leading UTF-8 byte-order mark, which Windows editors and
// PowerShell 5.1 often write and which claude itself accepts.
func StripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}

// ReadJSON decodes path into v. A missing file leaves v untouched and returns
// os.ErrNotExist so callers can apply defaults.
func ReadJSON(path string, v any) error {
	data, err := ReadFileShared(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(StripBOM(data), v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Lock takes an exclusive cross-process lock on path (created if missing) and
// returns the function that releases it.
func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(h, 0, 1, 0, ol)
		f.Close()
	}, nil
}

// IsReparsePoint reports whether fi (from os.Lstat) is a junction, symlink or
// other reparse point.
func IsReparsePoint(fi os.FileInfo) bool {
	if fi.Mode()&os.ModeSymlink != 0 || fi.Mode()&os.ModeIrregular != 0 {
		return true
	}
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}

// RemoveTreeSafe deletes path recursively without ever descending into a
// junction or symlink: links are removed themselves, their targets are left
// alone. (A naive walk follows NTFS junctions and deletes what they point at.)
func RemoveTreeSafe(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if IsReparsePoint(fi) {
		return removeLink(path, fi)
	}
	if fi.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		// Delete as much as possible: one file held open by another program
		// must not leave everything after it behind.
		var first error
		for _, e := range entries {
			if err := RemoveTreeSafe(filepath.Join(path, e.Name())); err != nil && first == nil {
				first = err
			}
		}
		if first != nil {
			return first
		}
	}
	return removeWritable(path)
}

// removeWritable removes a file or empty directory, clearing the read-only
// attribute if that is what blocks it.
func removeWritable(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if p, perr := syscall.UTF16PtrFromString(path); perr == nil {
		if attrs, aerr := syscall.GetFileAttributes(p); aerr == nil && attrs&syscall.FILE_ATTRIBUTE_READONLY != 0 {
			_ = syscall.SetFileAttributes(p, attrs&^syscall.FILE_ATTRIBUTE_READONLY)
			return os.Remove(path)
		}
	}
	return err
}

func removeLink(path string, fi os.FileInfo) error {
	// A directory junction/symlink is removed with RemoveDirectory, a file
	// symlink with DeleteFile; neither touches the target.
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok && d.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		p, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		return syscall.RemoveDirectory(p)
	}
	return os.Remove(path)
}

// IsWithin reports whether path is a strict descendant of dir (no "..").
func IsWithin(path, dir string) bool {
	ap, err1 := filepath.Abs(path)
	ad, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ad, ap)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !hasDotDot(rel)
}

func hasDotDot(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}
