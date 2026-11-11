package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/run"

	"golang.org/x/sys/windows"
)

// What keeps a second name, and what a kept one costs: a name a record or a
// cut-off run keeps stays as long as that one is there, and is looked at
// again without reading every list and every run each time.

// A second name a record of the desktop app's keeps costs one read at each
// turn of the session, not a read of every list.
func TestKeptNameReadsNoOtherList(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	r.appRecord(keyA, "local_d0000000-0000-4000-8000-0000000000d0", sid)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	must(t, r.d.Reconcile())
	other := filepath.Join(r.data, sessionsDir, "0e000000-0000-4000-8000-000000000000", "0f000000-0000-4000-8000-000000000000") // read first
	for i := 0; i < 2100; i++ {
		rec := fmt.Sprintf("local_%08x-0000-4000-8000-000000000000", i)
		r.writeJSON(filepath.Join(other, rec+".json"), map[string]any{"sessionId": rec,
			"cliSessionId": fmt.Sprintf("%08x-0000-4000-8000-0000000000ff", i), "cwd": "x"})
	}
	heads.Lock()
	heads.m = nil
	heads.Unlock()
	start := time.Now()
	must(t, r.d.Touch(sid2, next)) // one turn
	took := time.Since(start)
	heads.Lock()
	n := 0
	for p := range heads.m {
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(other)) {
			n++
		}
	}
	heads.Unlock()
	if n > 0 {
		t.Fatalf("one turn read %d records of a list that has nothing of the conversation (%v)", n, took)
	}
	if !sameFile(r.second(file, sid), file) {
		t.Fatal("the second name the app's own record opens went")
	}
}

// Nor does it read every run of baton's at each look.
func TestKeptNameReadsNoRuns(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.appRecord(keyB, "local_d0000000-0000-4000-8000-0000000000d1", sid)
	r.apply(sid, file) // the thread stays for the second name, which the app's record opens
	pass := func() time.Duration {
		must(t, r.d.Reconcile())
		start := time.Now()
		for i := 0; i < 3; i++ {
			must(t, r.d.Reconcile())
		}
		return time.Since(start) / 3
	}
	base := pass()
	for i := 0; i < 800; i++ {
		dir := filepath.Join(r.d.RunsDir, fmt.Sprintf("20261101-%06d-%06x", i, i))
		must(t, os.MkdirAll(dir, 0o700))
		must(t, run.SaveJournal(dir, run.Journal{RunID: filepath.Base(dir), SupervisorPID: 4, State: run.StateDone,
			SessionID: sid2, TranscriptPath: "x", Started: time.Now()}))
	}
	if more := pass() - base; more > 100*time.Millisecond {
		t.Fatalf("+%v at each look for one kept second name", more)
	}
	if !sameFile(r.second(file, sid), file) {
		t.Fatal("the second name the app's own record opens went")
	}
}

// A record the desktop app was writing when it stopped (left as
// local_*.json.tmp, which the app makes a record at its next start) keeps
// the second name it opens.
func TestSecondNameKeptForTheAppsUnfinishedRecord(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	h := r.second(file, sid)
	rec := "local_d0000000-0000-4000-8000-0000000000d1"
	tmp := filepath.Join(r.placeDir(keyB), rec+".json.tmp")
	r.writeJSON(tmp, map[string]any{"sessionId": rec, "cliSessionId": sid, "cwd": r.proj})
	r.apply(sid, file)
	must(t, r.d.Reconcile())
	if !sameFile(h, file) {
		t.Fatal("the second name went while the app's left-over record opens it")
	}
	must(t, os.Remove(tmp))
	must(t, r.d.Reconcile())
	if isFile(h) || !isFile(file) {
		t.Fatal("the second name stayed once nothing needs it")
	}
}

// A run that was cut off while it wrote by the second name (resumed by that
// path) is offered for reopening by that path: the name stays while it is.
func TestSecondNameKeptForACutOffRun(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	h := r.second(file, sid)
	must(t, r.d.Touch(sid, h)) // baton run -- --resume <the second name>
	dir := filepath.Join(r.d.RunsDir, run.NewID(time.Now()))
	must(t, os.MkdirAll(dir, 0o700))
	j := run.Journal{RunID: filepath.Base(dir), SupervisorPID: 0x7ffffff0, SupervisorCreated: time.Now().Add(-time.Hour),
		State: run.StateRunning, ChildPID: 0x7ffffff4, SessionID: sid, TranscriptPath: h, Cwd: r.proj, Started: time.Now().Add(-time.Hour)}
	must(t, run.SaveJournal(dir, j))
	offered := func() int {
		return len(run.Unfinished(run.All(r.d.RunsDir), func(run.Journal) bool { return false }, time.Now()))
	}
	if offered() != 1 {
		t.Fatal("setup: the cut-off run is not offered")
	}
	r.apply(sid, file)
	must(t, r.d.Reconcile())
	if !sameFile(h, file) || offered() != 1 {
		t.Fatalf("the second name went (%v) while the cut-off run is offered by it; offered %d", isFile(h), offered())
	}
	j.Dismissed = true // the user chose not to reopen it
	must(t, run.SaveJournal(dir, j))
	must(t, r.d.Reconcile())
	if isFile(h) || !isFile(file) {
		t.Fatal("the second name stayed once nothing needs it")
	}
}

// Ticked again while the running app may still write it back, a place
// takes its old record back only when that record is still its own: never
// one another thread of baton's (the session's earlier conversation, shown
// on its own) wrote under that name since.
func TestHidingPlaceNeverTakesAnothersRecord(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	r.running = true // signed in to A
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	r.apply(sid2, next, keyB)       // A unticked: hiding
	r.apply(sid, file, keyA)        // the earlier conversation, in A on its own
	r.apply(sid2, next, keyA, keyB) // A ticked again for the session
	r.running = false
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	if a, b := r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2); a != 1 || b != 1 {
		t.Fatalf("A has %d records of the earlier conversation and %d of the session's: %+v", a, b, r.index().Threads)
	}
	for _, th := range r.index().Threads {
		if p := th.Places[keyA]; p == nil || p.State != stateShown {
			t.Fatalf("thread %s: place A %+v", th.Session, p)
		}
	}
}

// A record of baton's the app wrote back after baton took it off is taken
// back only by a thread whose conversation it points at: one pointing at
// another conversation of that session's goes again, and the tick gets a
// record of its own.
func TestGoneRecordOfAnotherConversation(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	must(t, r.d.Reconcile())
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	before, err := os.ReadFile(rec) // points at sid2 now
	must(t, err)
	r.apply(sid2, next) // B unticked
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctB})
	r.running = true
	must(t, os.WriteFile(rec, before, 0o600)) // the app had B open after all: written back
	r.apply(sid, file, keyB)                  // the earlier conversation, ticked in B
	must(t, r.d.Reconcile())
	r.running = false
	must(t, r.d.Reconcile())
	ix := r.index()
	p := ix.Thread(sid).Places[keyB]
	if p.State != stateShown || r.recordsFor(keyB, sid) != 1 {
		t.Fatalf("place %+v, %d records of the conversation", p, r.recordsFor(keyB, sid))
	}
	if isFile(rec) || r.recordsFor(keyB, sid2) != 0 {
		t.Fatal("baton's record written back stayed")
	}
}

// A record that could not be read for a moment (another program held it) is
// read again: it is not taken for no record until it changes.
func TestRecordReadAgainAfterAFailedRead(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	h := r.second(file, sid)
	own := r.appRecord(keyB, "local_d0000000-0000-4000-8000-0000000000d1", sid)
	lk := lockFile(t, own)
	_ = r.view(sid, file) // the dialog reads the lists meanwhile
	windows.CloseHandle(lk)
	row := rowOf(r.view(sid, file), keyB)
	r.apply(sid, file) // A unticked
	if !row.Locked || row.Why != "own" || !sameFile(h, file) {
		t.Fatalf("row B %+v, second name kept %v", row, sameFile(h, file))
	}
}

// A run's journal is read again only once it changed: a second name a run
// keeps is asked about at every look, among a month of runs that are done.
func TestRunsReadAgainOnlyOnceChanged(t *testing.T) {
	r := newRig(t)
	dir := filepath.Join(r.d.RunsDir, run.NewID(time.Now()))
	must(t, os.MkdirAll(dir, 0o700))
	j := run.Journal{RunID: filepath.Base(dir), State: run.StateDone, SessionID: sid, TranscriptPath: "x", Started: time.Now()}
	must(t, run.SaveJournal(dir, j))
	if js, _ := r.d.journals(); len(js) != 1 || js[0].SessionID != sid {
		t.Fatalf("journals: %+v", js)
	}
	lk := lockFile(t, run.JournalPath(dir)) // cannot be read now
	if js, _ := r.d.journals(); len(js) != 1 || js[0].SessionID != sid {
		t.Fatalf("an unchanged journal was read again: %+v", js)
	}
	windows.CloseHandle(lk)
	j.SessionID = sid2
	must(t, run.SaveJournal(dir, j))
	if js, _ := r.d.journals(); len(js) != 1 || js[0].SessionID != sid2 {
		t.Fatalf("a changed journal was not read again: %+v", js)
	}
}

// Ticked again while the running app may still write the old record back
// (it was not yet): the new record is baton's, and the old one, if the app
// writes it back under another name, goes again.
func TestHidingTickedAgain(t *testing.T) {
	t.Run("same name", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.running = true // signed in to A
		r.apply(sid, file)
		r.apply(sid, file, keyA)
		ix := r.index()
		if p := ix.Thread(sid).Places[keyA]; p.State != stateShown || len(ix.Gone) != 0 || r.recordsFor(keyA, sid) != 1 || ix.Waiting(r.data) {
			t.Fatalf("place %+v, gone %+v, %d records", p, ix.Gone, r.recordsFor(keyA, sid))
		}
	})
	t.Run("after /clear", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.running = true // signed in to A: A's record waits to be pointed on
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		old := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
		before, err := os.ReadFile(old)
		must(t, err)
		r.apply(sid2, next)
		r.apply(sid2, next, keyA)
		must(t, os.WriteFile(old, before, 0o600)) // the app quits, writing back what it had
		r.running = false
		must(t, r.d.Reconcile())
		if isFile(old) || r.recordsFor(keyA, sid) != 0 || r.recordsFor(keyA, sid2) != 1 {
			t.Fatalf("A has %d records of the earlier conversation and %d of the session's", r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2))
		}
	})
}

// An index that names one record for two threads (an older baton gave the
// earlier conversation's record the name of the session's hiding one): the
// hiding place lets go of the name, and never takes the other's record off.
func TestHidingLeavesAnothersRecord(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	r.running = true // signed in to A
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	r.apply(sid2, next, keyB) // A unticked: hiding
	r.apply(sid, file, keyA)  // the earlier conversation, in A on its own
	ix := r.index()
	p := ix.Thread(sid).Places[keyA]
	shared := recordFile(p.Dir, "local_"+sid)
	must(t, os.Rename(recordFile(p.Dir, p.Record), shared))
	p.Record = "local_" + sid
	must(t, writeIndex(r.d.IndexPath, ix))
	before, err := os.ReadFile(shared)
	must(t, err)
	r.running = false
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	after, _ := os.ReadFile(shared)
	ix = r.index()
	if string(after) != string(before) || ix.Thread(sid2).Places[keyA] != nil || ix.Thread(sid).Places[keyA].State != stateShown {
		t.Fatalf("the other thread's record was taken off or written again: %s; %+v", after, ix.Threads)
	}
}
