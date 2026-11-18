package supervisor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
	"baton/internal/winproc"
)

const opensSID = "0be50000-0000-4000-8000-000000000001"

// Before a run waits for its first account, its journal names the
// conversation the first launch opens: in place (Opens), as a copy's source
// (ForkOf), or a cut-off session reopened (Opens). A new conversation, or a
// file that is not there, names nothing and writes no journal yet.
func TestJournalNamesWhatItOpensBeforeTheWait(t *testing.T) {
	journal := func(s *Supervisor) run.Journal {
		t.Helper()
		j, err := run.LoadJournal(s.runDir)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	orig := writeTranscript(t, t.TempDir(), opensSID)

	s := newTest(t, "--resume", orig)
	s.nameWhatItOpens()
	if j := journal(s); j.Opens != orig || j.ForkOf != "" || j.SessionID != "" {
		t.Fatalf("in place: %+v", j)
	}
	s = newTest(t, "--resume", orig, "--fork-session")
	s.nameWhatItOpens()
	if j := journal(s); j.ForkOf != orig || j.Opens != "" || j.SessionID != "" {
		t.Fatalf("a copy: %+v", j)
	}
	s = newTest(t)
	s.opt.Restore = &run.Journal{RunID: "20261101-100000-aaaaaa", TranscriptPath: orig, SessionID: opensSID}
	s.nameWhatItOpens()
	if j := journal(s); j.Opens != orig || j.SessionID != "" {
		t.Fatalf("reopened: %+v", j)
	}
	for _, s := range []*Supervisor{newTest(t), newTest(t, "--resume", filepath.Join(t.TempDir(), "none.jsonl"))} {
		s.nameWhatItOpens()
		if _, err := run.LoadJournal(s.runDir); !errors.Is(err, os.ErrNotExist) || s.opens != "" {
			t.Fatalf("named %q: %v", s.opens, err)
		}
	}
}

// Every account is limited, so a resume waits for the first one; the
// conversation is deleted meanwhile, by something else than baton (whose
// delete refuses while the run's journal names it). When the wait ends the
// session does not start (no claude on a file that is gone): it says why,
// without saying it was deleted (it may have been moved), and no "ended
// unexpectedly" follows.
func TestDeletedWhileItWaited(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("BATON_RUN", "")
	orig := writeTranscript(t, t.TempDir(), opensSID)
	s := newTest(t, "--resume", orig)
	var out bytes.Buffer
	s.opt.Out = &out
	setExhausted(t, s, config.ExhaustedWait)
	s.acct = accounts.Account{}
	s.signals = make(chan os.Signal, 4)
	_, _ = accounts.Update(s.opt.Paths.Accounts(), s.opt.Paths.AccountsLock(), func(r *accounts.Registry) error {
		r.Accounts = append([]accounts.Account{}, s.reg.Accounts...)
		return nil
	})
	_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
		for _, id := range []string{"a", "b"} {
			a := st.Account(id)
			a.Status, a.RetryAt, a.LimitedAt = ledger.StatusExhausted, time.Now().Add(2*time.Hour), time.Now()
		}
		return nil
	})
	started := false
	was := startProcess
	t.Cleanup(func() { startProcess = was })
	startProcess = func(string, []string, []string, string) (*winproc.Child, error) {
		started = true
		return nil, errors.New("no claude in tests")
	}

	done := make(chan int, 1)
	go func() { c, _ := s.loop(); done <- c }()
	var j run.Journal
	for end := time.Now().Add(5 * time.Second); j.State != run.StateWaiting && time.Now().Before(end); {
		time.Sleep(50 * time.Millisecond)
		j, _ = run.LoadJournal(s.runDir)
	}
	if j.State != run.StateWaiting || j.Opens != orig {
		t.Fatalf("waiting: %+v", j)
	}
	if err := os.Rename(orig, orig+".in-the-bin"); err != nil {
		t.Fatal(err)
	}
	if err := run.RequestSwitch(s.runDir, run.SwitchRequest{Target: "a", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 || started || !s.endNoted || !s.abandoned || s.userEnded ||
			!strings.Contains(out.String(), "no longer at "+orig+" (deleted or moved while the session waited)") {
			t.Fatalf("code %d, started %v, endNoted %v, abandoned %v, userEnded %v\n%s", code, started, s.endNoted, s.abandoned, s.userEnded, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("still waiting\n%s", out.String())
	}
}

// Removing an account while a start waits for its first account keeps the
// account's conversations in the user's own folder: the conversation moved,
// it was not deleted, and the start goes on from where it is now (a resume,
// a copy and a cut-off session reopened alike), named in the journal there.
// One found nowhere does not start, and is not said to be deleted; nor does
// a name a delete that stopped half way left (the delete's to finish).
func TestMovedWhileItWaited(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("BATON_RUN", "")
	moved := filepath.Join(home, "projects", "C--proj", opensSID+".jsonl")
	for _, how := range []string{"resume", "copy", "reopen", "found nowhere", "left by a delete"} {
		t.Run(how, func(t *testing.T) {
			_ = os.Remove(moved)
			orig := writeTranscript(t, t.TempDir(), opensSID) // in the profile of the account removed
			s := newTest(t, "--resume", orig)
			switch how {
			case "copy":
				s = newTest(t, "--resume", orig, "--fork-session")
			case "reopen":
				s = newTest(t)
				s.opt.Restore = &run.Journal{RunID: "20261101-100000-aaaaaa", TranscriptPath: orig, SessionID: opensSID}
			}
			var out bytes.Buffer
			s.opt.Out = &out
			s.nameWhatItOpens()
			if how == "found nowhere" {
				if err := os.Remove(orig); err != nil {
					t.Fatal(err)
				}
				if args, err := s.firstArgs(); !errors.Is(err, errGone) || args != nil {
					t.Fatalf("%q %v", args, err)
				}
				return
			}
			if err := os.MkdirAll(filepath.Dir(moved), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(orig, moved); err != nil {
				t.Fatal(err)
			}
			if how == "left by a delete" {
				if err := run.NoteLeft(s.opt.Paths.Root, opensSID); err != nil {
					t.Fatal(err)
				}
				if args, err := s.firstArgs(); !errors.Is(err, errGone) || args != nil {
					t.Fatalf("%q %v", args, err)
				}
				return
			}
			args, err := s.firstArgs()
			if err != nil || !slices.Contains(args, moved) || slices.Contains(args, orig) {
				t.Fatalf("%q %v\n%s", args, err, out.String())
			}
			j, _ := run.LoadJournal(s.runDir)
			if how == "copy" && (s.forkSource != moved || j.ForkOf != moved) ||
				how != "copy" && (s.transcr != moved || j.Opens != moved) {
				t.Fatalf("names %q / %q, journal %+v", s.transcr, s.forkSource, j)
			}
			if !strings.Contains(out.String(), "moved while the session waited") {
				t.Fatalf("not said:\n%s", out.String())
			}
		})
	}
}

// A delete of the conversation that holds its marker at the launch is
// waited out: one that lets go (it refused: this run's journal stopped it)
// leaves the start to go on, one that holds on is taken as deleting it.
func TestStartWaitsOutADelete(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("BATON_RUN", "")
	was := deletingWait
	t.Cleanup(func() { deletingWait = was })
	deletingWait = 3 * time.Second
	orig := writeTranscript(t, t.TempDir(), opensSID)

	s := newTest(t, "--resume", orig)
	s.opt.Out = &bytes.Buffer{}
	s.nameWhatItOpens()
	release, err := run.MarkDeleting(s.opt.Paths.Root, opensSID)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		release()
	}()
	if args, err := s.firstArgs(); err != nil || !slices.Contains(args, orig) {
		t.Fatalf("a delete that let go: %q %v", args, err)
	}

	deletingWait = 200 * time.Millisecond
	s = newTest(t, "--resume", orig)
	s.opt.Out = &bytes.Buffer{}
	s.nameWhatItOpens()
	release, err = run.MarkDeleting(s.opt.Paths.Root, opensSID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if args, err := s.firstArgs(); !errors.Is(err, errDeleted) || args != nil {
		t.Fatalf("a delete that holds on: %q %v", args, err)
	}
}

// Once the run is done its log is closed: the process may live on with its
// window open, and the run's folder can go to the Recycle Bin meanwhile. A
// line logged later does no harm.
func TestFinishLetsGoOfTheLog(t *testing.T) {
	s := newTest(t)
	s.opt.Out = &bytes.Buffer{}
	if err := s.prepareRun(); err != nil {
		t.Fatal(err)
	}
	s.logf("something to say")
	s.finish()
	s.logf("said after the end")
	if err := os.Rename(s.runDir, s.runDir+"-binned"); err != nil {
		t.Fatalf("the run's folder can't move: %v", err)
	}
	if j, err := run.LoadJournal(s.runDir + "-binned"); err != nil || j.State != run.StateDone {
		t.Fatalf("journal: %+v %v", j, err)
	}
}
