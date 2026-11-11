package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"baton/internal/winproc"

	"golang.org/x/sys/windows"
)

// LiveSession is one entry of a profile's sessions/<pid>.json registry,
// which every running claude.exe keeps up to date.
type LiveSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Status    string `json:"status"` // idle | busy | waiting
	// WaitingFor: what a waiting session waits for (a permission
	// question, input needed...), in Claude Code's words.
	WaitingFor string `json:"waitingFor"`
	Entrypoint string `json:"entrypoint"` // what runs it: claude-desktop, cli, claude-vscode...
	StartedAt  int64  `json:"startedAt"`
	// ProcStart is the process's creation time as a Windows FILETIME, in
	// decimal (newer Claude Code versions). startedAt can come long after it:
	// the desktop app keeps a claude ready and registers it when it is used.
	ProcStart string `json:"procStart"`
	// Name is the conversation's live title; StatusUpdatedAt is when its
	// status last changed (Unix ms).
	Name            string `json:"name"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	// HostSessionID: in the Claude desktop app, the session record it runs
	// for (local_<id>), which says which of the app's entries was opened.
	HostSessionID string `json:"hostSessionId"`
	File          string `json:"-"`
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
	if ft, err := strconv.ParseInt(s.ProcStart, 10, 64); err == nil && ft > 0 {
		d := ft - (int64(creation.HighDateTime)<<32 | int64(creation.LowDateTime))
		return d > -100000 && d < 100000 // within 10 ms: the very process
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
// Ids match in any case (one can come from a path typed by hand).
func Holders(profileDirs []string, sessionID string) []LiveSession {
	var out []LiveSession
	for _, dir := range profileDirs {
		for _, s := range ReadSessions(dir) {
			if ((s.SessionID != "" && strings.EqualFold(s.SessionID, sessionID)) || s.Status == "unknown") && s.Alive() {
				out = append(out, s)
			}
		}
	}
	return out
}

// OpenElsewhere returns the live claudes, other than the processes in self,
// that have the conversation open now: two writers split it. Unlike Holders
// it counts only entries that name the conversation, so it never cries wolf
// over a registry file caught mid-rewrite. Ids match in any case.
func OpenElsewhere(configDirs []string, sessionID string, self ...int) []LiveSession {
	if sessionID == "" {
		return nil
	}
	var list []LiveSession
	for sid, l := range LiveByConversation(configDirs) {
		if strings.EqualFold(sid, sessionID) {
			list = append(list, l...)
		}
	}
	return Others(list, self...)
}

// LiveByConversation reads the session registries of the config folders once
// and returns the live claudes by the conversation they have open.
func LiveByConversation(configDirs []string) map[string][]LiveSession {
	out := map[string][]LiveSession{}
	for _, dir := range configDirs {
		for _, s := range ReadSessions(dir) {
			if s.SessionID != "" && s.Alive() {
				out[s.SessionID] = append(out[s.SessionID], s)
			}
		}
	}
	return out
}

// Others is list without the processes in self and the ones they started:
// on Windows a claude that relaunches itself (to switch screen modes, say)
// waits while a child goes on with the same conversation.
func Others(list []LiveSession, self ...int) []LiveSession {
	var out []LiveSession
	var parents map[uint32]uint32
	for _, s := range list {
		if slices.Contains(self, s.PID) {
			continue
		}
		if parents == nil {
			parents = processParents()
		}
		if descendsFrom(s.PID, self, parents) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// StartedBy reports whether s is one of roots or a process one of them
// started (a shim, or a claude that relaunched itself): given the claudes
// that baton's runs started, it tells a baton session from other Claude Codes.
func StartedBy(s LiveSession, roots ...int) bool {
	return len(roots) > 0 && len(Others([]LiveSession{s}, roots...)) == 0
}

// StartedLater reports whether a's claude started after b's, by the times
// their registry entries give (the pid settles a tie, so both sides of a
// comparison agree): of two baton sessions with one conversation open, only
// the later one moves to a copy.
func StartedLater(a, b LiveSession) bool {
	if a.StartedAt != b.StartedAt {
		return a.StartedAt > b.StartedAt
	}
	return a.PID > b.PID
}

// descendsFrom reports whether pid's chain of parents reaches one of the
// processes in self within a few steps. Each parent must be older than its
// child: a dead parent's pid can be in use by an unrelated process now.
func descendsFrom(pid int, self []int, parents map[uint32]uint32) bool {
	child := uint32(pid)
	childAt := winproc.ProcessCreated(pid)
	for i := 0; i < 4 && !childAt.IsZero(); i++ {
		p := parents[child]
		if p == 0 || p == child {
			return false
		}
		pAt := winproc.ProcessCreated(int(p))
		if pAt.IsZero() || pAt.After(childAt) {
			return false
		}
		if slices.Contains(self, int(p)) {
			return true
		}
		child, childAt = p, pAt
	}
	return false
}

// processParents maps every running process to its parent.
func processParents() map[uint32]uint32 {
	out := map[uint32]uint32{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[e.ProcessID] = e.ParentProcessID
	}
	return out
}

// ConfigDir is the config folder whose session registry holds the entry.
func (s LiveSession) ConfigDir() string {
	return filepath.Dir(filepath.Dir(s.File))
}

// OpenSessions returns the conversations that a live claude in any of the
// profiles has open now, by session id, with what runs that claude (its
// entrypoint, such as claude-desktop or cli; "?" when it does not say).
func OpenSessions(profileDirs []string) map[string]string {
	out := map[string]string{}
	for _, dir := range profileDirs {
		for _, s := range ReadSessions(dir) {
			if s.SessionID != "" && s.Alive() {
				out[s.SessionID] = s.Entrypoint
				if s.Entrypoint == "" {
					out[s.SessionID] = "?"
				}
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
// character to '-', so it is lossy. A conversation baton shows in the Claude
// desktop app has a second name in the user's own setup (a hard link): the
// name in the folder listed first wins, so callers list the accounts'
// profiles before the user's own setup. When one name is gone (Claude Code's
// cleanup removes old files per config folder) the other is found.
func FindTranscript(profileDirs []string, sessionID string) string {
	var best string
	var bestFi os.FileInfo
	for _, dir := range profileDirs {
		m, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sessionID+".jsonl"))
		for _, p := range m {
			fi, err := os.Stat(p)
			if err != nil || bestFi != nil && (!fi.ModTime().After(bestFi.ModTime()) || os.SameFile(fi, bestFi)) {
				continue
			}
			best, bestFi = p, fi
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
		for _, p := range transcriptFiles(dir, cwd) {
			fi, err := os.Stat(p)
			if err != nil || !fi.ModTime().After(bestMod) {
				continue
			}
			if c := Describe(p, fi).Cwd; c != "" && strings.EqualFold(filepath.Clean(c), filepath.Clean(cwd)) {
				best, bestMod = p, fi.ModTime()
			}
		}
	}
	return best
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
