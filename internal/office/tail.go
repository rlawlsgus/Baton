package office

import (
	"bytes"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// Transcript rows can be big (a pasted screenshot is stored inline, a tool
// result can hold a whole file): a row longer than rowMax is skipped rather
// than kept in memory.
const (
	rowMax     = 8 << 20
	readChunk  = 256 << 10
	readBudget = 16 << 20 // the most read from one file in one go; the rest waits for the next tick
	// attachWindow is how much of the end of a transcript is read to see
	// where a conversation is when the office starts showing it.
	attachWindow = 2 << 20
	// agentWindow is the same for a subagent's own transcript.
	agentWindow = 512 << 10
)

// tailer reads the rows appended to a file since its last call. Claude Code
// only appends to a transcript and writes whole rows, but a row still being
// written is kept until its newline arrives.
type tailer struct {
	path    string
	pos     int64  // the next byte to read
	partial []byte // the start of a row whose newline has not come yet
	skip    bool   // inside a row too long to keep (or cut by where reading began): drop it up to its newline
}

// newTailer reads path from about from on. Reading that does not begin at
// the start of the file drops the row it lands in, which is only part of one.
func newTailer(path string, from int64) *tailer {
	t := &tailer{path: path}
	if from > 0 {
		// From the byte before: if that is a newline, only it is dropped.
		t.pos, t.skip = from-1, true
	}
	return t
}

// tailFrom is where reading the last window bytes of a file of size begins.
func tailFrom(size, window int64) int64 {
	return max(0, size-window)
}

// next calls fn with every complete row appended since the last call, except
// the kinds the office never looks at. The row passed to fn is only valid
// during the call. shrunk reports that the file got shorter (it was
// rewritten): reading then starts again at its last attachWindow bytes.
func (t *tailer) next(fn func(line []byte)) (shrunk bool, err error) {
	// The size comes from the open file: a directory listing (and so a
	// plain stat) can lag behind a file another process keeps appending to.
	f, err := openShared(t.path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	size := fi.Size()
	if size < t.pos { // pos counts the bytes of a partial row too
		*t = *newTailer(t.path, tailFrom(size, attachWindow))
		shrunk = true
	}
	if size == t.pos {
		return shrunk, nil
	}
	if _, err := f.Seek(t.pos, io.SeekStart); err != nil {
		return shrunk, err
	}
	buf := make([]byte, min(int64(readChunk), size-t.pos))
	for read := 0; t.pos < size && read < readBudget; {
		n, err := f.Read(buf)
		if n > 0 {
			t.feed(buf[:n], fn)
			t.pos += int64(n)
			read += n
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return shrunk, err
		}
	}
	return shrunk, nil
}

// feed splits data into rows, carrying an unfinished one over to the next call.
func (t *tailer) feed(data []byte, fn func([]byte)) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			if !t.skip {
				if len(t.partial)+len(data) > rowMax {
					t.skip, t.partial = true, nil
				} else {
					t.partial = append(t.partial, data...)
				}
			}
			return
		}
		seg := data[:i]
		data = data[i+1:]
		if t.skip {
			t.skip = false
			continue
		}
		line := seg
		if len(t.partial) > 0 {
			if len(t.partial)+len(seg) > rowMax {
				t.partial = nil
				continue
			}
			line = append(t.partial, seg...)
		}
		if line = bytes.TrimSpace(line); len(line) > 0 && wanted(line) {
			fn(line)
		}
		if cap(t.partial) > 1<<20 {
			t.partial = nil // do not keep a big row's buffer around
		} else {
			t.partial = t.partial[:0]
		}
	}
}

// Rows the office never looks at, by how they begin: copies of what was sent
// to the API (up to a megabyte each), file backups, desktop-app bookkeeping.
var skipped = [][]byte{
	[]byte(`{"type":"api-request-blob"`),
	[]byte(`{"type":"api-request-shape"`),
	[]byte(`{"type":"file-history-`),
	[]byte(`{"type":"artifact-`),
	[]byte(`{"type":"frame-link"`),
	[]byte(`{"type":"cost-state"`),
}

// wanted says whether a row is worth parsing. Attachments (reminders, file
// snapshots, tool listings; up to 250 KB) only matter when they bring the
// notice that a background agent finished.
func wanted(line []byte) bool {
	for _, p := range skipped {
		if bytes.HasPrefix(line, p) {
			return false
		}
	}
	if bytes.Contains(line, []byte(`"type":"attachment"`)) && !bytes.Contains(line, []byte("task-notification")) {
		return false
	}
	return true
}

// openShared opens a file for reading the way Claude Code expects others to:
// sharing read, write and delete, so a reader never stops it from appending,
// renaming or deleting the file (os.Open does not share delete).
func openShared(path string) (*os.File, error) {
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
	return os.NewFile(uintptr(h), path), nil
}

// readSmall reads a small file (a subagent's .meta.json) the same way.
func readSmall(path string, max int64) ([]byte, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}
