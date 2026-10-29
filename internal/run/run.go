// Package run is the on-disk contract of one supervised session:
//
//	runs\<id>\settings.json   --settings file with baton's hooks
//	runs\<id>\events.jsonl    one line per hook call (written by `baton hook`)
//	runs\<id>\run.json        journal: who supervises, which account/session
//	runs\<id>\switch.json     a pending `baton switch` request
package run

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"baton/internal/fsx"
)

func SettingsPath(dir string) string { return filepath.Join(dir, "settings.json") }
func EventsPath(dir string) string   { return filepath.Join(dir, "events.jsonl") }
func JournalPath(dir string) string  { return filepath.Join(dir, "run.json") }
func StatusPath(dir string) string   { return filepath.Join(dir, "status.json") }
func ControlPath(dir string) string  { return filepath.Join(dir, "switch.json") }
func StopPath(dir string) string     { return filepath.Join(dir, "stop.json") }

// NewID returns a sortable run id: UTC time plus random suffix.
func NewID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// NewSessionID returns a random RFC 4122 v4 UUID for --session-id.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ---- hook events -----------------------------------------------------------

type Event struct {
	TS        time.Time       `json:"ts"`
	Event     string          `json:"event"`
	ClaudePID int             `json:"claude_pid,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// Payload holds the hook input fields baton uses.
type Payload struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	Cwd                  string `json:"cwd"`
	Source               string `json:"source"` // SessionStart: startup | resume | clear | compact
	Reason               string `json:"reason"` // SessionEnd
	Error                string `json:"error"`  // StopFailure
	LastAssistantMessage string `json:"last_assistant_message"`
}

func (e Event) Decode() Payload {
	var p Payload
	_ = json.Unmarshal(e.Payload, &p)
	return p
}

// AppendEvent adds one line to events.jsonl. O_APPEND writes of one line are
// not interleaved, so concurrent hook processes are safe.
func AppendEvent(dir string, ev Event) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(EventsPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Tail reads events appended since the last call.
type Tail struct {
	path   string
	offset int64
	buf    []byte
}

func NewTail(dir string) *Tail { return &Tail{path: EventsPath(dir)} }

func (t *Tail) Next() []Event {
	f, err := os.Open(t.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil
	}
	data, _ := io.ReadAll(f)
	t.offset += int64(len(data))
	t.buf = append(t.buf, data...)
	var out []Event
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		line := t.buf[:i]
		t.buf = t.buf[i+1:]
		var ev Event
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

// ---- journal ---------------------------------------------------------------

const (
	StateRunning   = "running"
	StateStopping  = "stopping" // a switch is due; baton waits for claude's current turn to end
	StateSwitching = "switching"
	StateWaiting   = "waiting" // claude stopped; baton waits for a reset or the switch rate limit
	StateAsking    = "asking"  // claude stopped; baton asks the user in its window
	StateEnding    = "ending"  // `baton stop`: baton waits for claude's current turn, then ends the session
	StateDone      = "done"
)

type Journal struct {
	RunID             string    `json:"run_id"`
	SupervisorPID     int       `json:"supervisor_pid"`
	SupervisorCreated time.Time `json:"supervisor_created"`
	State             string    `json:"state"`
	Account           string    `json:"account"`
	SwitchMode        string    `json:"switch_mode,omitempty"`
	ChildPID          int       `json:"child_pid,omitempty"`
	RemoteControl     bool      `json:"remote_control,omitempty"` // claude runs with --remote-control
	SessionID         string    `json:"session_id,omitempty"`
	TranscriptPath    string    `json:"transcript_path,omitempty"`
	Cwd               string    `json:"cwd"`
	Started           time.Time `json:"started"`
	Updated           time.Time `json:"updated"`
}

func SaveJournal(dir string, j Journal) error {
	j.Updated = time.Now()
	return fsx.WriteJSONAtomic(JournalPath(dir), j)
}

func LoadJournal(dir string) (Journal, error) {
	var j Journal
	err := fsx.ReadJSON(JournalPath(dir), &j)
	return j, err
}

// Active lists runs whose supervisor is still alive. alive(pid, created)
// must compare the process creation time, not just the pid.
func Active(runsDir string, alive func(pid int, created time.Time) bool) []Journal {
	dirs, _ := os.ReadDir(runsDir)
	var out []Journal
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		j, err := LoadJournal(filepath.Join(runsDir, d.Name()))
		if err != nil || j.State == StateDone {
			continue
		}
		if alive(j.SupervisorPID, j.SupervisorCreated) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Started.Before(out[k].Started) })
	return out
}

// ---- control ---------------------------------------------------------------

type SwitchRequest struct {
	Target string    `json:"target,omitempty"` // empty: let baton pick
	At     time.Time `json:"at"`
}

func RequestSwitch(dir string, req SwitchRequest) error {
	return fsx.WriteJSONAtomic(ControlPath(dir), req)
}

// TakeSwitch returns and removes a pending request.
func TakeSwitch(dir string) (*SwitchRequest, error) {
	p := ControlPath(dir)
	var req SwitchRequest
	if err := fsx.ReadJSON(p, &req); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		os.Remove(p)
		return nil, fmt.Errorf("bad switch request: %w", err)
	}
	if err := os.Remove(p); err != nil {
		return nil, err
	}
	return &req, nil
}

// RequestStop asks the run to end its session (`baton stop`, the GUI).
func RequestStop(dir string) error {
	return fsx.WriteJSONAtomic(StopPath(dir), SwitchRequest{At: time.Now()})
}

// TakeStop reports and clears a pending stop request.
func TakeStop(dir string) bool {
	if _, err := os.Stat(StopPath(dir)); err != nil {
		return false
	}
	_ = os.Remove(StopPath(dir))
	return true
}

// ReadLimited reads at most n bytes from r (hook stdin).
func ReadLimited(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(bufio.NewReader(io.LimitReader(r, n)))
}
