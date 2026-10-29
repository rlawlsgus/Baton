package run

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnfinished(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	conv := func(name string) string {
		p := filepath.Join(dir, name+".jsonl")
		_ = os.WriteFile(p, []byte("{}\n"), 0o600)
		return p
	}
	at := func(h int) time.Time { return now.Add(time.Duration(h) * time.Hour) }
	js := []Journal{
		// Cut off: the supervisor died without saying it was done.
		{RunID: "crashed", SessionID: "s1", TranscriptPath: conv("s1"), Cwd: dir, State: StateRunning, Started: at(-3), Updated: at(-2)},
		// Cut off: Windows restarted under it.
		{RunID: "restart", SessionID: "s2", TranscriptPath: conv("s2"), Cwd: dir, State: StateDone, EndedBy: EndedShutdown, Started: at(-5), Updated: at(-1)},
		// Ended by the user.
		{RunID: "quit", SessionID: "s3", TranscriptPath: conv("s3"), Cwd: dir, State: StateDone, Started: at(-5), Updated: at(-1)},
		// Still running.
		{RunID: "live", SessionID: "s4", TranscriptPath: conv("s4"), Cwd: dir, State: StateRunning, Started: at(-5), Updated: at(-1)},
		// Reopened, dismissed, too old, its folder or conversation gone.
		{RunID: "reopened", SessionID: "s5", TranscriptPath: conv("s5"), Cwd: dir, State: StateRunning, RestoredBy: "x", Started: at(-5), Updated: at(-1)},
		{RunID: "dismissed", SessionID: "s6", TranscriptPath: conv("s6"), Cwd: dir, State: StateRunning, Dismissed: true, Started: at(-5), Updated: at(-1)},
		{RunID: "old", SessionID: "s7", TranscriptPath: conv("s7"), Cwd: dir, State: StateRunning, Started: at(-200), Updated: at(-199)},
		{RunID: "nofolder", SessionID: "s8", TranscriptPath: conv("s8"), Cwd: filepath.Join(dir, "gone"), State: StateRunning, Started: at(-5), Updated: at(-1)},
		{RunID: "noconv", SessionID: "s9", TranscriptPath: filepath.Join(dir, "gone.jsonl"), Cwd: dir, State: StateRunning, Started: at(-5), Updated: at(-1)},
		// Cut off, but the conversation went on in a later run (ended or running).
		{RunID: "superseded", SessionID: "s10", TranscriptPath: conv("s10"), Cwd: dir, State: StateRunning, Started: at(-5), Updated: at(-4)},
		{RunID: "later", SessionID: "s10", TranscriptPath: conv("s10"), Cwd: dir, State: StateDone, Started: at(-3), Updated: at(-2)},
		{RunID: "cut2", SessionID: "s4", TranscriptPath: conv("s4"), Cwd: dir, State: StateRunning, Started: at(-9), Updated: at(-8)},
	}
	alive := func(j Journal) bool { return j.RunID == "live" }
	got := Unfinished(js, alive, now)
	var ids []string
	for _, j := range got {
		ids = append(ids, j.RunID)
	}
	if len(ids) != 2 || ids[0] != "restart" || ids[1] != "crashed" {
		t.Fatalf("unfinished %v, want [restart crashed] (newest first)", ids)
	}
}
