package supervisor

import (
	"testing"
	"time"

	"baton/internal/claude"
	"baton/internal/run"
)

// The journal names what a copy starts from while the copy has no file of
// its own (a relaunch then forks it again, so a delete of it must wait), and
// stops naming it as soon as the copy's first turn wrote its file.
func TestJournalNamesTheForkSourceUntilTheCopyHasAFile(t *testing.T) {
	journal := func(s *Supervisor) run.Journal {
		t.Helper()
		j, err := run.LoadJournal(s.runDir)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}

	// A copy asked for at the start.
	src := writeTranscript(t, t.TempDir(), "orig")
	s := newTest(t, "--resume", src, "--fork-session")
	if _, err := s.firstArgs(); err != nil {
		t.Fatal(err)
	}
	s.saveJournal()
	if j := journal(s); j.ForkOf != src || j.SessionID != "" {
		t.Fatalf("before the copy's first turn: %+v", j)
	}
	// Its first turn ends: the journal is written at once, without the source.
	s.launchedAt = time.Now().Add(-time.Second)
	fork := writeTranscript(t, s.reg.Accounts[0].ProfileDir, "fork1")
	emit(t, s, "Stop", 7, map[string]any{"session_id": "fork1", "transcript_path": fork})
	s.handleEvents()
	if j := journal(s); j.ForkOf != "" || j.SessionID != "fork1" || j.TranscriptPath != fork {
		t.Fatalf("after the copy's first turn: %+v", j)
	}

	// A move to a copy because the conversation is open elsewhere.
	s2, _, orig := heldSession(t)
	if !s2.goOnInCopy(claude.LiveSession{PID: 1, Entrypoint: "claude-desktop"}) {
		t.Fatal("no copy")
	}
	s2.saveJournal()
	if j := journal(s2); j.ForkOf != orig || j.SessionID != "" || j.TranscriptPath != "" {
		t.Fatalf("moving to a copy: %+v", j)
	}
	// Whatever else clears the fork's source before the launch, the copy
	// still starts from orig.
	s2.forkSource = ""
	s2.saveJournal()
	if j := journal(s2); j.ForkOf != orig {
		t.Fatalf("copy still to start: %+v", j)
	}
	// A plain session names none.
	s3 := newTest(t)
	s3.saveJournal()
	if j := journal(s3); j.ForkOf != "" {
		t.Fatalf("plain session: %+v", j)
	}
}
