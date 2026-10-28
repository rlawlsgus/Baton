package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// LiveSession is one entry of a profile's sessions/<pid>.json registry,
// which every running claude.exe keeps up to date.
type LiveSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Status    string `json:"status"` // idle | busy
	StartedAt int64  `json:"startedAt"`
	File      string `json:"-"`
}

// ReadSessions lists a profile's registry entries. A file that cannot be read
// even after a few retries (mid-rewrite, sharing violation) is still reported,
// with its pid from the file name and status "unknown", so callers checking
// whether a profile is busy fail safe instead of seeing "nobody here".
func ReadSessions(profileDir string) []LiveSession {
	files, _ := filepath.Glob(filepath.Join(profileDir, "sessions", "*.json"))
	var out []LiveSession
	for _, f := range files {
		var s LiveSession
		ok := false
		for try := 0; try < 5 && !ok; try++ {
			// Read and parse together: a file caught mid-rewrite reads fine
			// but does not parse.
			if data, err := os.ReadFile(f); err == nil && json.Unmarshal(data, &s) == nil {
				ok = true
			} else {
				time.Sleep(40 * time.Millisecond)
			}
		}
		if !ok {
			pid, perr := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".json"))
			if perr != nil {
				continue
			}
			s = LiveSession{PID: pid, Status: "unknown"}
		}
		if s.PID == 0 {
			continue
		}
		s.File = f
		out = append(out, s)
	}
	return out
}

// SessionFor returns the registry entry of one claude pid, if present.
func SessionFor(profileDir string, pid int) (LiveSession, bool) {
	data, err := os.ReadFile(filepath.Join(profileDir, "sessions", itoa(pid)+".json"))
	if err != nil {
		return LiveSession{}, false
	}
	var s LiveSession
	if json.Unmarshal(data, &s) != nil {
		return LiveSession{}, false
	}
	return s, true
}

// Alive checks the registry entry against the real process: the pid must be
// running and must have started when the entry says (a killed claude leaves a
// stale file whose pid can later be reused by an unrelated process).
func (s LiveSession) Alive() bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(s.PID))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 /* STILL_ACTIVE */ {
		return false
	}
	var creation, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) != nil {
		return true
	}
	created := time.Unix(0, creation.Nanoseconds())
	if s.StartedAt == 0 {
		// Unreadable entry: the process can only be its writer if it already
		// existed when the file was last written (a pid reused later cannot be).
		if fi, err := os.Stat(s.File); err == nil {
			return !created.After(fi.ModTime().Add(2 * time.Second))
		}
		return true
	}
	started := time.UnixMilli(s.StartedAt)
	d := started.Sub(created)
	return d > -5*time.Second && d < 120*time.Second
}

// Holders returns live processes in any of the profiles that have sessionID
// open. A live process whose registry entry could not be read counts too:
// better to wait or refuse than to start a second writer on one transcript.
func Holders(profileDirs []string, sessionID string) []LiveSession {
	var out []LiveSession
	for _, dir := range profileDirs {
		for _, s := range ReadSessions(dir) {
			if (s.SessionID == sessionID || s.Status == "unknown") && s.Alive() {
				out = append(out, s)
			}
		}
	}
	return out
}

// BusyProfile reports whether any live claude process uses the profile.
func BusyProfile(profileDir string) bool {
	for _, s := range ReadSessions(profileDir) {
		if s.Alive() {
			return true
		}
	}
	return false
}

// FindTranscript looks for <sessionID>.jsonl in the profiles' projects dirs.
// Never rebuild the path from the cwd: claude's slug maps every non-ASCII
// character to '-', so it is lossy.
func FindTranscript(profileDirs []string, sessionID string) string {
	var best string
	var bestMod time.Time
	for _, dir := range profileDirs {
		m, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sessionID+".jsonl"))
		for _, p := range m {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(bestMod) {
				best, bestMod = p, fi.ModTime()
			}
		}
	}
	return best
}

// LatestTranscriptFor returns the most recently written transcript, in any
// of the profiles, of a conversation that ran in cwd — what `claude -c` would
// pick if all accounts shared one profile.
func LatestTranscriptFor(profileDirs []string, cwd string) string {
	var best string
	var bestMod time.Time
	for _, dir := range profileDirs {
		files, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "*.jsonl"))
		for _, p := range files {
			fi, err := os.Stat(p)
			if err != nil || !fi.ModTime().After(bestMod) {
				continue
			}
			if c := transcriptCwd(p); c != "" && strings.EqualFold(filepath.Clean(c), filepath.Clean(cwd)) {
				best, bestMod = p, fi.ModTime()
			}
		}
	}
	return best
}

// transcriptCwd reads the cwd recorded in the first rows of a transcript.
func transcriptCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for i := 0; i < 50 && sc.Scan(); i++ {
		var row struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.Cwd != "" {
			return row.Cwd
		}
	}
	return ""
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
