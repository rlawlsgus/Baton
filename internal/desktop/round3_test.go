package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/run"

	"golang.org/x/sys/windows"
)

// hold opens path (a file or a folder) as another program can, sharing
// nothing, until release is called (at the latest when the test ends).
func hold(t *testing.T, path string) (release func()) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	must(t, err)
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(release)
	return release
}

// A name baton took off a list, whose record the running app may still write
// back, is never given to another thread's record: the write-back would land
// on it. The other thread's tick gets a record of its own, and the record
// written back goes again.
func TestGoneNameNotReusedByAnotherThread(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next)) // the app is closed: A's record points at sid2 now
	old := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
	r.running = true // the app starts on A and loads the record (pointing at sid2)
	before, err := os.ReadFile(old)
	must(t, err)
	r.apply(sid2, next)                       // A unticked: hiding
	r.apply(sid2, next, keyA)                 // ticked again: the old one is gone, a new record
	r.apply(sid, file, keyA)                  // the earlier conversation, in A on its own
	must(t, os.WriteFile(old, before, 0o600)) // the app quits, writing back what it had
	r.running = false
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	if r.recordsFor(keyA, sid2) != 1 || r.recordsFor(keyA, sid) != 1 {
		t.Fatalf("A has %d records of the session's conversation and %d of the earlier one: %+v",
			r.recordsFor(keyA, sid2), r.recordsFor(keyA, sid), r.index())
	}
	ix := r.index()
	if p := ix.Thread(sid).Places[keyA]; p == nil || p.State != stateShown {
		t.Fatalf("the earlier conversation's place: %+v", p)
	}
	if row := rowOf(r.view(sid, file), keyA); !row.Checked || row.Locked {
		t.Fatalf("the earlier conversation's row: %+v", row)
	}
	if isFile(old) {
		t.Fatal("baton's record the app wrote back stayed")
	}
}

// While another thread's record still points at a conversation (waiting to
// be pointed on to the one its session is on now), a tick of that
// conversation in the same list waits too: never two records of it there.
func TestNoSecondRecordWhileAnotherWaitsToMoveOn(t *testing.T) {
	t.Run("nothing typed yet", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		next := filepath.Join(filepath.Dir(file), sid2+".jsonl") // /clear: nothing typed yet
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid, file, keyB) // the earlier conversation, ticked in B on its own
		must(t, r.d.Reconcile())
		must(t, r.d.Reconcile())
		if n := r.recordsFor(keyB, sid); n != 1 {
			t.Fatalf("B has %d records of %s written by baton", n, sid)
		}
		if row := rowOf(r.view(sid, file), keyB); !row.Checked || row.Locked {
			t.Fatalf("the tick was lost: %+v", row)
		}
		r.conversation(r.prof, sid2) // the session's first turn
		must(t, r.d.Touch(sid2, next))
		must(t, r.d.Reconcile())
		if a, b := r.recordsFor(keyB, sid), r.recordsFor(keyB, sid2); a != 1 || b != 1 {
			t.Fatalf("B has %d records of the earlier conversation and %d of the session's", a, b)
		}
	})
	t.Run("the app runs on that list", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctB})
		r.running = true // B's record waits for the app to close to be pointed on
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid, file, keyB)
		must(t, r.d.Reconcile())
		if n := r.recordsFor(keyB, sid); n != 1 {
			t.Fatalf("B has %d records of %s written by baton", n, sid)
		}
		r.running = false
		must(t, r.d.Reconcile())
		if a, b := r.recordsFor(keyB, sid), r.recordsFor(keyB, sid2); a != 1 || b != 1 {
			t.Fatalf("B has %d records of the earlier conversation and %d of the session's", a, b)
		}
	})
	t.Run("unticked while it waits", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid, file, keyB)
		r.apply(sid, file)
		r.conversation(r.prof, sid2)
		must(t, r.d.Touch(sid2, next))
		must(t, r.d.Reconcile())
		if a, b := r.recordsFor(keyB, sid), r.recordsFor(keyB, sid2); a != 0 || b != 1 {
			t.Fatalf("B has %d records of the earlier conversation and %d of the session's", a, b)
		}
	})
}

// Follow saves the index (the thread moved on) before it points a record at
// the new conversation: a stop in between never leaves baton's own record
// looking like one the desktop app took over.
func TestFollowSavesTheIndexBeforeARecordMovesOn(t *testing.T) {
	t.Run("the save fails", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		next := r.conversation(r.prof, sid2) // the copy, with its first turn
		var release func()
		r.d.Running = func() bool { // look() runs once the index is loaded
			if release == nil {
				release = hold(t, r.d.IndexPath)
			}
			return false
		}
		err := r.d.Follow(sid, sid2, next)
		release()
		r.d.Running = func() bool { return r.running }
		if err == nil {
			t.Fatal("setup: the index was written while held")
		}
		if m := r.record(keyB, "local_"+sid); m["cliSessionId"] != sid {
			t.Fatalf("the record moved on before the index said so: %v", m)
		}
		must(t, r.d.Reconcile())
		if p := r.index().Threads[0].Places[keyB]; p.State != stateShown {
			t.Fatalf("baton's own record is now %s (the app's)", p.State)
		}
	})
	t.Run("a stop after the save", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
		must(t, r.d.Follow(sid, sid2, next)) // saved: the thread is on sid2
		// The record was pointed on, and the stop came before the index said so.
		rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
		m := r.record(keyB, "local_"+sid)
		m["cliSessionId"], m["priorCliSessionIds"] = sid2, []string{sid}
		b, _ := json.Marshal(m)
		must(t, os.WriteFile(rec, b, 0o600))
		r.conversation(r.prof, sid2)
		must(t, r.d.Reconcile())
		ix := r.index()
		p := ix.Thread(sid2).Places[keyB]
		if p.State != stateShown || p.Cli != sid2 {
			t.Fatalf("place: %+v", p)
		}
		if row := rowOf(r.view(sid2, next), keyB); !row.Checked || row.Locked {
			t.Fatalf("row: %+v", row)
		}
		if isFile(r.second(file, sid)) {
			t.Fatal("the earlier conversation's second name stayed although nothing needs it")
		}
	})
}

// A list the user unticked while the desktop app had baton's record there
// (the app keeps it) gets no record of baton's again when the session moves
// on: no copy follows /clear into it.
func TestUntickOfATakenOverRecordStaysAfterClear(t *testing.T) {
	t.Run("unticked", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		entry := r.live(sid, "local_"+sid) // the app runs A's record
		r.apply(sid, file, keyB)           // A unticked
		if row := rowOf(r.view(sid, file), keyA); !row.Checked || !row.Locked || row.Why != "released" {
			t.Fatalf("row of the record the app keeps: %+v", row)
		}
		must(t, os.Remove(entry))
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		if n := r.recordsFor(keyA, sid2); n != 0 {
			t.Fatalf("A was unticked, yet baton wrote %d record(s) of the session there", n)
		}
		if r.recordsFor(keyA, sid) != 1 || !sameFile(r.second(file, sid), file) {
			t.Fatal("the app's record, or the second name it opens, went")
		}
		if row := rowOf(r.view(sid2, next), keyA); row.Checked || row.Locked {
			t.Fatalf("row of the unticked list: %+v", row)
		}
	})
	t.Run("hiding", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		rec := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
		before, err := os.ReadFile(rec)
		must(t, err)
		r.running = true         // signed in to A
		r.apply(sid, file, keyB) // A unticked: hiding
		must(t, os.WriteFile(rec, before, 0o600))
		entry := r.live(sid, "local_"+sid) // the app wrote it back and runs it
		must(t, r.d.Reconcile())
		must(t, os.Remove(entry))
		r.running = false
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		if n := r.recordsFor(keyA, sid2); n != 0 {
			t.Fatalf("A was unticked, yet baton wrote %d record(s) of the session there", n)
		}
		if r.recordsFor(keyA, sid) != 1 {
			t.Fatal("the record the app took over went")
		}
	})
	t.Run("ticked again after /clear", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		entry := r.live(sid, "local_"+sid)
		r.apply(sid, file, keyB)
		must(t, os.Remove(entry))
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid2, next, keyA, keyB)
		var m map[string]any
		for rec, path := range listRecords(r.placeDir(keyA)) {
			if h, ok := readHead(path); ok && h.Cli == sid2 {
				m = r.record(keyA, rec)
			}
		}
		if m == nil || m["title"] != "fix the <login> bug (baton)" {
			t.Fatalf("baton's record beside the app's: %v", m)
		}
	})
}

// A record of a copy that is not written yet is never taken for one the
// desktop app took over: the index never names a file of the app's.
func TestPendingCopyNeverReleasedAtFollow(t *testing.T) {
	const sid3 = "5e550000-0000-4000-8000-000000000003"
	const other = "f0000000-0000-4000-8000-00000000000f"
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.live(sid, "local_"+sid)
	next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
	must(t, r.d.Follow(sid, sid2, next)) // no file yet: the copy's record waits
	theirs := r.appRecord(keyA, "local_"+sid2, other)
	must(t, r.d.Follow(sid2, sid3, ""))
	ix := r.index()
	th := ix.Thread(sid3)
	for _, p := range th.Released {
		if strings.EqualFold(p.Record, "local_"+sid2) {
			t.Fatalf("the index names a record baton never wrote: %+v", p)
		}
	}
	if p := th.Places[keyA]; p == nil || p.State != stateShown || strings.EqualFold(p.Record, "local_"+sid2) {
		t.Fatalf("the copy's place: %+v", p)
	}
	for _, p := range th.Released {
		if p.Written == 0 {
			t.Fatalf("a record baton never wrote is kept as one the app took over: %+v", p)
		}
	}
	if m := r.record(keyA, "local_"+sid2); m["cliSessionId"] != other || !isFile(theirs) {
		t.Fatalf("the app's record was changed: %v", m)
	}
}

// A record the desktop app left half written (local_*.json.tmp, which it
// makes a record at its next start) locks the row of its conversation as
// one of the app's own: a tick never puts baton's record beside it.
func TestAppsUnfinishedRecordLocksTheRow(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	rec := "local_d0000000-0000-4000-8000-0000000000d1"
	tmp := filepath.Join(r.placeDir(keyB), rec+".json.tmp")
	r.writeJSON(tmp, map[string]any{"sessionId": rec, "cliSessionId": sid, "cwd": r.proj})
	row := rowOf(r.view(sid, file), keyB)
	r.apply(sid, file, keyB)
	must(t, os.Rename(tmp, filepath.Join(r.placeDir(keyB), rec+".json"))) // the app's next start
	if n := r.recordsFor(keyB, sid); n != 1 || !row.Locked || row.Why != "own" {
		t.Fatalf("row %+v; B has %d records of the conversation", row, n)
	}
}

// A record, or a list folder, that cannot be read right now (another
// program holds it) may point at the conversation: its second name stays,
// and is asked about again at the next look. One that is gone, or is no
// record (not JSON), keeps nothing.
func TestHeldRecordKeepsTheSecondName(t *testing.T) {
	const appRec = "local_d0000000-0000-4000-8000-0000000000d1"
	t.Run("untick", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		own := r.appRecord(keyB, appRec, sid)
		release := hold(t, own)
		r.apply(sid, file) // A unticked
		release()
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record (unreadable for a moment) opens it")
		}
		must(t, r.d.Reconcile())
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record opens it")
		}
		must(t, os.Remove(own))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing needs it")
		}
	})
	t.Run("reconcile", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		own := r.appRecord(keyB, appRec, sid)
		r.apply(sid, file) // A unticked: the name stays for the app's record
		must(t, r.d.Reconcile())
		if !sameFile(h, file) {
			t.Fatal("setup")
		}
		// The app writes its record again (a turn in the app, its quit).
		r.writeJSON(own, map[string]any{"sessionId": appRec, "cliSessionId": sid,
			"cwd": r.proj, "title": "made in the app", "lastActivityAt": 1_900_000_000_000})
		release := hold(t, own)
		must(t, r.d.Reconcile())
		release()
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record (unreadable for a moment) opens it")
		}
	})
	t.Run("list folder", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		own := r.appRecord(keyB, appRec, sid)
		release := hold(t, r.placeDir(keyB))
		if _, err := os.ReadDir(r.placeDir(keyB)); err == nil {
			t.Fatal("setup: the folder can be read")
		}
		r.apply(sid, file) // A unticked
		must(t, r.d.Reconcile())
		release()
		if !sameFile(h, file) {
			t.Fatal("the second name went while a list that cannot be read now may need it")
		}
		must(t, r.d.Reconcile())
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record opens it")
		}
		must(t, os.Remove(own))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing needs it")
		}
	})
	t.Run("not a record", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		broken := filepath.Join(r.placeDir(keyB), appRec+".json")
		must(t, os.MkdirAll(filepath.Dir(broken), 0o700))
		must(t, os.WriteFile(broken, []byte(`{"cliSessionId":"`+sid+`"`), 0o600))
		r.apply(sid, file)
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("a file that is no record kept the second name")
		}
	})
	// What it costs: a name kept by something that cannot be read now is
	// asked about by that one thing at each look, never by a read of every
	// list.
	t.Run("cost", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		own := r.appRecord(keyB, appRec, sid)
		release := hold(t, own)
		r.apply(sid, file) // A unticked: kept for the record that cannot be read now
		other := filepath.Join(r.data, sessionsDir, "0e000000-0000-4000-8000-000000000000", "0f000000-0000-4000-8000-000000000000")
		for i := 0; i < 2100; i++ {
			rec := fmt.Sprintf("local_%08x-0000-4000-8000-000000000000", i)
			r.writeJSON(filepath.Join(other, rec+".json"), map[string]any{"sessionId": rec,
				"cliSessionId": fmt.Sprintf("%08x-0000-4000-8000-0000000000ff", i), "cwd": "x"})
		}
		heads.Lock()
		heads.m = nil
		heads.Unlock()
		for i := 0; i < 3; i++ {
			must(t, r.d.Reconcile())
		}
		release()
		heads.Lock()
		n := 0
		for p := range heads.m {
			if strings.HasPrefix(strings.ToLower(p), strings.ToLower(other)) {
				n++
			}
		}
		heads.Unlock()
		if n > 0 {
			t.Fatalf("each look read %d records of a list that has nothing of the conversation", n)
		}
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record (unreadable for a moment) opens it")
		}
	})
}

// A run's journal that cannot be read right now may say the run writes by
// the second name: the name stays, and is asked about again at the next
// look. A journal that is no journal (not JSON) keeps nothing.
func TestHeldJournalKeepsTheSecondName(t *testing.T) {
	fresh := func() { // a new process has read no journal yet
		journalsSeen.Lock()
		journalsSeen.m, journalsSeen.dir = nil, ""
		journalsSeen.Unlock()
	}
	t.Run("first sight", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		dir := r.runOn(sid, h) // between two claudes: none is registered
		fresh()
		release := hold(t, run.JournalPath(dir))
		r.apply(sid, file) // A unticked
		release()
		if !sameFile(h, file) {
			t.Fatal("the second name went while a run of baton's writes by it (its journal unreadable for a moment)")
		}
		must(t, r.d.Reconcile())
		if !sameFile(h, file) {
			t.Fatal("the second name went while a run of baton's writes by it")
		}
		j, err := run.LoadJournal(dir)
		must(t, err)
		j.State = run.StateDone
		must(t, run.SaveJournal(dir, j))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing needs it")
		}
	})
	t.Run("changed", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		dir := r.runOn(sid, file)
		if js, _ := r.d.journals(); len(js) != 1 {
			t.Fatalf("journals: %+v", js)
		}
		j, err := run.LoadJournal(dir)
		must(t, err)
		j.TranscriptPath = h // the run goes on by the second name
		must(t, run.SaveJournal(dir, j))
		release := hold(t, run.JournalPath(dir))
		r.apply(sid, file) // A unticked
		release()
		if !sameFile(h, file) {
			t.Fatal("the second name went while a run of baton's writes by it (read as it was before)")
		}
	})
	t.Run("not a journal", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		dir := r.runOn(sid, h)
		must(t, os.WriteFile(run.JournalPath(dir), []byte(`{"session_id":`), 0o600))
		fresh()
		r.apply(sid, file)
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("a journal that cannot be parsed kept the second name")
		}
	})
}

// A list ticked again while its untick waited for the running app takes
// baton's old record off for good, but the app still has that record in
// memory, pointing at an earlier conversation of the session: the app opens
// that one through its second name, may take the record over, and writes it
// back as it quits. The name stays until the app let go of the list, and
// goes then once nothing else uses it.
func TestReTickKeepsTheNameTheRunningAppHolds(t *testing.T) {
	type rerun struct {
		r            *rig
		file, h, old string
		before       []byte
		next         string
	}
	// A shows sid; the app starts on A and loads local_sid (pointing at sid);
	// /clear moves the session on to sid2 (A is not safe: local_sid still
	// points at sid); A is unticked (hiding) and ticked again.
	setup := func(t *testing.T) rerun {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		r.running = true
		old := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
		before, err := os.ReadFile(old)
		must(t, err)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid2, next)
		if !sameFile(h, file) {
			t.Fatal("setup: the second name went while hiding")
		}
		r.apply(sid2, next, keyA)
		return rerun{r, file, h, old, before, next}
	}
	t.Run("while the app runs", func(t *testing.T) {
		s := setup(t)
		if !sameFile(s.h, s.file) {
			t.Fatal("the second name of sid went while the running app still has baton's record of it (local_sid -> sid) in memory")
		}
		if !s.r.index().Waiting(s.r.data) {
			t.Fatal("nothing waits for the app to close, though it may write baton's old record back as it quits")
		}
		must(t, s.r.d.Reconcile())
		if !sameFile(s.h, s.file) {
			t.Fatal("the second name of sid went at the next look while the app runs on A")
		}
	})
	t.Run("the app takes it over", func(t *testing.T) {
		s := setup(t)
		r := s.r
		live := r.live(sid, "local_"+sid)             // the user opens the entry the app still lists
		must(t, os.WriteFile(s.old, s.before, 0o600)) // the app writes it back
		must(t, r.d.Reconcile())
		r.running = false
		must(t, r.d.Reconcile())
		must(t, os.Remove(live)) // its Claude Code is closed: the record alone keeps the name
		must(t, r.d.Reconcile())
		if !isFile(s.old) {
			t.Fatal("the record the app took over went")
		}
		if !sameFile(s.h, s.file) {
			t.Fatal("the app keeps its entry for sid, but sid's second name is gone for good")
		}
		if a, b := r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2); a != 1 || b != 1 {
			t.Fatalf("A has %d records of the earlier conversation and %d of the session's", a, b)
		}
	})
	t.Run("the app runs past goneFor", func(t *testing.T) {
		s := setup(t)
		r := s.r
		r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_000_000).Add(goneFor + time.Hour) }
		must(t, r.d.Reconcile())
		must(t, r.d.Reconcile())
		if !sameFile(s.h, s.file) {
			t.Fatal("sid's second name went after goneFor while the app still runs on A")
		}
		must(t, os.WriteFile(s.old, s.before, 0o600)) // the app quits, writing back what it had
		r.running = false
		for range 3 {
			must(t, r.d.Reconcile())
		}
		if isFile(s.old) || isFile(s.h) || len(r.index().Gone) != 0 {
			t.Fatalf("after the app quit: record %v, name %v, gone %+v", isFile(s.old), isFile(s.h), r.index().Gone)
		}
	})
	for _, back := range []bool{false, true} {
		name := "the app quits"
		if back {
			name += ", writing it back"
		}
		t.Run(name, func(t *testing.T) {
			s := setup(t)
			r := s.r
			if back {
				must(t, os.WriteFile(s.old, s.before, 0o600))
			}
			r.running = false
			for range 3 {
				must(t, r.d.Reconcile())
			}
			if isFile(s.old) {
				t.Fatal("baton's old record stayed after the app quit")
			}
			if isFile(s.h) || !isFile(s.file) {
				t.Fatal("sid's second name stayed once the app let go of the list and nothing uses it")
			}
			if a, b := r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2); a != 0 || b != 1 {
				t.Fatalf("A has %d records of the earlier conversation and %d of the session's", a, b)
			}
			if ix := r.index(); ix.Waiting(r.data) {
				t.Fatalf("still waits for the app to close: %+v", ix.Gone)
			}
		})
	}
}

// Another thread's record of baton's that cannot be read right now (another
// program holds it, in a process that has read none yet) may point at the
// conversation: a tick of that conversation in the same list waits, as for
// one that does point at it, and is written once that one moved on.
func TestUnreadableRecordMayPointAtTheConversation(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	next := filepath.Join(filepath.Dir(file), sid2+".jsonl") // /clear: nothing typed yet
	must(t, r.d.Follow(sid, sid2, next))
	waiting := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	heads.Lock()
	heads.m = nil // a fresh process (baton desktop ... --show)
	heads.Unlock()
	release := hold(t, waiting) // held for a moment while the user ticks
	r.apply(sid, file, keyB)    // the earlier conversation, ticked in B on its own
	must(t, r.d.Reconcile())
	release()
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	if n := r.recordsFor(keyB, sid); n != 1 {
		t.Fatalf("B has %d records of %s written by baton", n, sid)
	}
	ix := r.index()
	if p := ix.Thread(sid).Places[keyB]; p == nil || p.State != stateShown || p.Written != 0 {
		t.Fatalf("the tick was lost: %+v", p)
	}
	r.conversation(r.prof, sid2) // the session's first turn
	must(t, r.d.Touch(sid2, next))
	must(t, r.d.Reconcile())
	if a, b := r.recordsFor(keyB, sid), r.recordsFor(keyB, sid2); a != 1 || b != 1 {
		t.Fatalf("B has %d records of the earlier conversation and %d of the session's", a, b)
	}
}
