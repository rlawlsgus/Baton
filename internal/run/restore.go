package run

import (
	"os"
	"sort"
	"time"
)

// EndedShutdown: the run ended while Windows shut down, restarted or signed
// the user out (Journal.EndedBy).
const EndedShutdown = "shutdown"

// RestoreWindow is how long a cut-off session is offered for reopening.
const RestoreWindow = 7 * 24 * time.Hour

// CutOff reports whether the run ended without the user ending it: its
// supervisor is gone without having said it was done (a crash, the power, a
// restart that did not wait), or Windows shut down under it. A run that was
// reopened, or that the user chose not to reopen, no longer counts.
func (j Journal) CutOff(alive bool) bool {
	switch {
	case j.RestoredBy != "" || j.Dismissed:
		return false
	case j.State != StateDone:
		return !alive
	}
	return j.EndedBy == EndedShutdown
}

// Unfinished picks the cut-off runs worth reopening, newest first: their
// conversation and folder are still there, and the conversation has not gone
// on since (in a later run, or one that is running now). One per conversation.
func Unfinished(journals []Journal, alive func(Journal) bool, now time.Time) []Journal {
	later := map[string]time.Time{} // session id -> the newest start of a run of it
	running := map[string]bool{}
	for _, j := range journals {
		if j.SessionID == "" {
			continue
		}
		if j.Started.After(later[j.SessionID]) {
			later[j.SessionID] = j.Started
		}
		if j.State != StateDone && alive(j) {
			running[j.SessionID] = true
		}
	}
	var out []Journal
	seen := map[string]bool{}
	for _, j := range journals {
		if j.SessionID == "" || j.TranscriptPath == "" || seen[j.SessionID] || running[j.SessionID] ||
			later[j.SessionID].After(j.Started) || now.Sub(j.Updated) > RestoreWindow || !j.CutOff(alive(j)) {
			continue
		}
		if fi, err := os.Stat(j.TranscriptPath); err != nil || fi.IsDir() {
			continue
		}
		if fi, err := os.Stat(j.Cwd); err != nil || !fi.IsDir() {
			continue
		}
		seen[j.SessionID] = true
		out = append(out, j)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Updated.After(out[b].Updated) })
	return out
}
