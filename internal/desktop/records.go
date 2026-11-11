package desktop

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"baton/internal/fsx"
)

// record is a session record as the desktop app writes one when it imports
// a conversation from Claude Code itself (its adoptCliSession), field for
// field and in its order. The app reads records field by field and drops
// what it does not know when it saves one, so nothing else is put in.
type record struct {
	SessionID                string `json:"sessionId"`
	CliSessionID             string `json:"cliSessionId"`
	Cwd                      string `json:"cwd"`
	OriginCwd                string `json:"originCwd"`
	CreatedAt                int64  `json:"createdAt"`
	LastActivityAt           int64  `json:"lastActivityAt"`
	IndexedAt                int64  `json:"indexedAt"`
	IsArchived               bool   `json:"isArchived"`
	Title                    string `json:"title"`
	TitleSource              string `json:"titleSource"`
	SessionPermissionUpdates []any  `json:"sessionPermissionUpdates"`
	AlwaysAllowedReasons     []any  `json:"alwaysAllowedReasons"`
	AdoptedFromOtherSurface  bool   `json:"adoptedFromOtherSurface"`
	FromCliTranscript        bool   `json:"fromCliTranscript"`
}

// newRecord is the content of a new record for conversation cli.
func newRecord(id, cli, cwd, title string, created, last, now int64) []byte {
	r := record{SessionID: id, CliSessionID: cli, Cwd: cwd, OriginCwd: cwd, CreatedAt: created, LastActivityAt: last,
		IndexedAt: now, Title: title, TitleSource: "auto", SessionPermissionUpdates: []any{}, AlwaysAllowedReasons: []any{},
		AdoptedFromOtherSurface: true, FromCliTranscript: true}
	return encode(r)
}

// encode writes JSON as the desktop app does: one line, "<" and "&" as they are.
func encode(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// maxRecord: the desktop app skips records over 10 MB, and so does baton.
const maxRecord = 10 << 20

// head is what baton reads of a record.
type head struct {
	SessionID   string `json:"sessionId"`
	Cli         string `json:"cliSessionId"`
	Title       string `json:"title"`
	TitleSource string `json:"titleSource"` // auto, tool or user (the user named it in the app)
	Last        any    `json:"lastActivityAt"`
	Archived    bool   `json:"isArchived"`
	// Turns is how many turns the desktop app completed in the session
	// itself (not the ones in the conversation before it got the record):
	// more than none means the user went on with it there. It is any so
	// that an unexpected type never makes the whole record unreadable.
	Turns any `json:"completedTurns"`
}

// wentOn reports whether the desktop app completed a turn of its own in the
// session: from then on the record is the app's.
func (h head) wentOn() bool {
	n, ok := h.Turns.(float64)
	return ok && n > 0
}

// heads keeps the records read, by path, size and time: a list is read
// again when the window asks, and records seldom change.
var heads struct {
	sync.Mutex
	m map[string]cachedHead
}

type cachedHead struct {
	size int64
	mod  time.Time
	h    head
	ok   bool
}

// readHead reads the record at path; ok is false when there is none (or it
// is too big, empty or not JSON, which the desktop app skips too). What it
// read is kept until the file changes; a read that failed (another program
// held the file for a moment) is not, so the next look reads it again.
func readHead(path string) (head, bool) {
	h, ok, _ := readHeadErr(path)
	return h, ok
}

// readHeadErr is readHead, and says when the record could not be read right
// now (another program holds it, or it may not be opened): it may be there
// and point anywhere. A file that is not there, and one that is no record,
// is no error.
func readHeadErr(path string) (head, bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return head{}, false, err
	}
	if fi.IsDir() {
		return head{}, false, nil
	}
	heads.Lock()
	c, hit := heads.m[path]
	heads.Unlock()
	if hit && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.h, c.ok, nil
	}
	var h head
	ok := false
	if fi.Size() > 0 && fi.Size() <= maxRecord {
		// A record is a JSON object; anything else (null, a list) is no record.
		data, err := fsx.ReadFileShared(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
			return head{}, false, err
		}
		data = bytes.TrimSpace(fsx.StripBOM(data))
		if bytes.HasPrefix(data, []byte("{")) && json.Unmarshal(data, &h) == nil {
			ok = true
		}
	}
	heads.Lock()
	if heads.m == nil || len(heads.m) > 2000 {
		heads.m = map[string]cachedHead{}
	}
	heads.m[path] = cachedHead{fi.Size(), fi.ModTime(), h, ok}
	heads.Unlock()
	return h, ok, nil
}

// recordFile is the record id's file in folder dir.
func recordFile(dir, id string) string { return filepath.Join(dir, id+".json") }

// listRecords lists the records in a place's folder: name (the id) -> path.
func listRecords(dir string) map[string]string {
	out := map[string]string{}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "local_") || !strings.HasSuffix(n, ".json") {
			continue
		}
		out[strings.TrimSuffix(n, ".json")] = filepath.Join(dir, n)
	}
	return out
}

// rewrite changes keys of the record at path and keeps every other key as
// it is, numbers byte for byte: the desktop app may have added fields of
// its own since baton wrote it.
func rewrite(path string, change func(m map[string]any)) error {
	data, err := fsx.ReadFileShared(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(fsx.StripBOM(data)))
	dec.UseNumber()
	m := map[string]any{}
	if err := dec.Decode(&m); err != nil {
		return err
	}
	if m == nil {
		return errors.New(path + " is not a record")
	}
	change(m)
	out := encode(m)
	if out == nil {
		return errors.New("cannot write " + path)
	}
	return writeRecord(path, out, true)
}

// number reads a JSON number of a record (0 when it is not one).
func number(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	}
	return 0
}

// timesSeen keeps what times read, by path, size and time: every look at
// the lists asks again for each conversation shown.
var timesSeen struct {
	sync.Mutex
	m map[string]cachedTimes
}

type cachedTimes struct {
	size          int64
	mod           time.Time
	created, last int64
}

// times reads when a conversation began (its first row's time) and when it
// last moved (its last row's, else the file's time), in milliseconds.
func times(path string) (created, last int64) {
	fi, err := os.Stat(path)
	if err != nil {
		now := time.Now().UnixMilli()
		return now, now
	}
	timesSeen.Lock()
	c, hit := timesSeen.m[path]
	timesSeen.Unlock()
	if hit && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.created, c.last
	}
	created, last = readTimes(path, fi)
	timesSeen.Lock()
	if timesSeen.m == nil || len(timesSeen.m) > 500 {
		timesSeen.m = map[string]cachedTimes{}
	}
	timesSeen.m[path] = cachedTimes{fi.Size(), fi.ModTime(), created, last}
	timesSeen.Unlock()
	return created, last
}

func readTimes(path string, fi os.FileInfo) (created, last int64) {
	last = fi.ModTime().UnixMilli()
	created = last
	f, err := os.Open(path)
	if err != nil {
		return created, last
	}
	defer f.Close()
	br := bufio.NewReaderSize(io.LimitReader(f, 4<<20), 64<<10)
	for i := 0; i < 200; i++ {
		line, err := br.ReadSlice('\n')
		if ts, ok := stamp(line); ok {
			created = ts
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) { // a long row: skip the rest of it
				_, err = br.ReadSlice('\n')
			}
		}
		if err != nil {
			break
		}
	}
	// The newest row with a time, from the last 256 KB.
	off := max(0, fi.Size()-256<<10)
	buf := make([]byte, fi.Size()-off)
	if n, _ := f.ReadAt(buf, off); n > 0 {
		lines := bytes.Split(buf[:n], []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			if ts, ok := stamp(lines[i]); ok {
				last = ts
				break
			}
		}
	}
	if created > last {
		created = last
	}
	return created, last
}

// stamp is a transcript row's "timestamp", in milliseconds.
func stamp(line []byte) (int64, bool) {
	i := bytes.Index(line, []byte(`"timestamp":"`))
	if i < 0 {
		return 0, false
	}
	rest := line[i+len(`"timestamp":"`):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339Nano, string(rest[:j]))
	if err != nil {
		return 0, false
	}
	return t.UnixMilli(), true
}

// tombstone is when the user deleted a record in the desktop app (ms; 0:
// never). The app leaves a deleted_<id> file holding that time, for the
// record and for its conversations.
func tombstone(dir string, names ...string) int64 {
	var at int64
	for _, n := range tombstoneNames(names...) {
		p := filepath.Join(dir, n)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSpace(string(fsx.StripBOM(data))), 10, 64)
		if err != nil {
			ms = modTime(p).UnixMilli()
		}
		at = max(at, ms)
	}
	return at
}

func tombstoneNames(names ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		for _, s := range []string{n, strings.TrimPrefix(n, "local_")} {
			if s != "" && !seen[strings.ToLower(s)] {
				seen[strings.ToLower(s)] = true
				out = append(out, "deleted_"+s)
			}
		}
	}
	return out
}
