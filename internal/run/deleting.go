package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"baton/internal/fsx"
	"baton/internal/winproc"
)

// A conversation being deleted has a marker, <root>\deleting\<id>, made with
// O_EXCL: two deletes of it never run at once, and baton starts nothing on it
// while its files go to the Recycle Bin (a start then would --resume a file
// that is half gone, or write a new one under the id being deleted).

// ErrDeleting: the conversation is being deleted.
var ErrDeleting = errors.New("the conversation is being deleted")

// DeletingError is MarkDeleting's answer when another delete holds the
// marker; errors.Is(err, ErrDeleting) holds for it.
type DeletingError struct {
	PID   int       // the process deleting it (0: the marker is being written)
	Since time.Time // when that delete began
}

func (e *DeletingError) Error() string {
	if e.PID <= 0 {
		return ErrDeleting.Error()
	}
	return fmt.Sprintf("%v (by process %d since %s)", ErrDeleting, e.PID, e.Since.Local().Format(time.TimeOnly))
}

func (e *DeletingError) Unwrap() error { return ErrDeleting }

// deletingStale: a delete takes seconds; a marker this old is from one that
// hung, and a later delete or start may go ahead.
const deletingStale = 2 * time.Minute

type deletingMark struct {
	PID     int       `json:"pid"`
	Created time.Time `json:"created"` // the process's creation time: a pid used again is another process
	At      time.Time `json:"at"`
}

var uuidRx = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// deletingPath is the marker of conversation id ("" if id is no session id,
// which is then never in a path). Claude Code names the files in lower case.
func deletingPath(root, id string) string {
	if !uuidRx.MatchString(id) {
		return ""
	}
	return filepath.Join(root, "deleting", strings.ToLower(id))
}

// MarkDeleting takes the marker of conversation id for a delete; release
// gives it back (it removes the marker only while it is still this one). A
// marker left by a delete whose process is gone, or older than two minutes,
// is taken over. One held by a live delete, this process's own included,
// gives a *DeletingError.
func MarkDeleting(root, id string) (release func(), err error) {
	p := deletingPath(root, id)
	if p == "" {
		return nil, fmt.Errorf("%q is not a session id", id)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	me := deletingMark{PID: os.Getpid(), Created: winproc.ProcessCreated(os.Getpid()), At: time.Now()}
	data, err := json.Marshal(me)
	if err != nil {
		return nil, err
	}
	for try := 0; ; try++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.Write(data)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(p)
				return nil, werr
			}
			var once sync.Once
			return func() { once.Do(func() { releaseMark(p, me) }) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		m, held := readMark(p)
		if held {
			return nil, &DeletingError{PID: m.PID, Since: m.At}
		}
		if try > 0 {
			return nil, fmt.Errorf("%w: its marker %s could not be taken over", ErrDeleting, p)
		}
		// Left by a delete that died or hung.
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
}

// Deleting reports whether a live delete holds conversation id's marker.
func Deleting(root, id string) bool {
	p := deletingPath(root, id)
	if p == "" {
		return false
	}
	_, held := readMark(p)
	return held
}

// readMark reads a marker and says whether it still holds: its process
// runs and it is not stale. A marker that cannot be read yet (its delete is
// writing it) holds while it is new.
func readMark(p string) (deletingMark, bool) {
	var m deletingMark
	data, err := fsx.ReadFileShared(p)
	if errors.Is(err, fs.ErrNotExist) {
		return m, false
	}
	if err != nil || json.Unmarshal(data, &m) != nil || m.PID <= 0 {
		fi, err := os.Stat(p)
		if err != nil {
			return deletingMark{}, false
		}
		return deletingMark{At: fi.ModTime()}, fresh(fi.ModTime())
	}
	return m, fresh(m.At) && processAlive(m.PID, m.Created)
}

// fresh: at is less than deletingStale ago. A time far ahead (the clock was
// set back) is stale too, or a marker could hold for as long as its
// process runs.
func fresh(at time.Time) bool {
	d := time.Since(at)
	return d > -deletingStale && d < deletingStale
}

// processAlive: pid runs and is the process created at created (to the
// second), as the run journals' supervisors are checked.
func processAlive(pid int, created time.Time) bool {
	got := winproc.ProcessCreated(pid)
	if got.IsZero() {
		return false
	}
	if created.IsZero() {
		return true // its creation time could not be read when it wrote the marker
	}
	d := got.Sub(created)
	return d > -time.Second && d < time.Second
}

// releaseMark removes the marker if it is still me: a delete that hung past
// deletingStale may have lost it to a later one.
func releaseMark(p string, me deletingMark) {
	data, err := fsx.ReadFileShared(p)
	if err != nil {
		return
	}
	var m deletingMark
	if json.Unmarshal(data, &m) == nil && m.PID == me.PID && m.At.Equal(me.At) {
		_ = os.Remove(p)
	}
}

// AnyDeleting reports whether a live delete holds the marker of any
// conversation: what moves a profile's conversations elsewhere (an
// account's removal) waits for it.
func AnyDeleting(root string) bool {
	ents, _ := os.ReadDir(filepath.Join(root, "deleting"))
	for _, e := range ents {
		if uuidRx.MatchString(e.Name()) && Deleting(root, e.Name()) {
			return true
		}
	}
	return false
}

// A delete that stopped half way leaves a note, <root>\deleting\<id>.left,
// until a later delete of the conversation finishes: what is left then may
// be only its temporary folders, which Claude Code keeps by id for any
// config folder (also one baton never looks in), and the note says they are
// a delete's to finish.
func leftPath(root, id string) string {
	if p := deletingPath(root, id); p != "" {
		return p + ".left"
	}
	return ""
}

// NoteLeft notes that a delete of conversation id stopped half way.
func NoteLeft(root, id string) error {
	p := leftPath(root, id)
	if p == "" {
		return fmt.Errorf("%q is not a session id", id)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, nil, 0o600)
}

// Left reports whether a delete of conversation id stopped half way and no
// delete finished it since.
func Left(root, id string) bool {
	p := leftPath(root, id)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// ClearLeft drops the note once a delete of conversation id finished.
func ClearLeft(root, id string) {
	if p := leftPath(root, id); p != "" {
		_ = os.Remove(p)
	}
}
