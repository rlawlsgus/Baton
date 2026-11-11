package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"baton/internal/run"
	"baton/internal/winproc"
)

// What may still open a conversation through its second name, whoever
// made that name: every thread of baton's, every record in the desktop
// app's lists, and every Claude Code that has it open (in the user's own
// folder or in an account's profile). The name goes only once none does.

// second is conversation id's second name, for its file in a profile.
func (r *rig) second(file, id string) string {
	return filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), id+".jsonl")
}

// appRecord writes a record the desktop app made itself, for conversation
// cli, in list key.
func (r *rig) appRecord(key, rec, cli string) string {
	r.t.Helper()
	p := filepath.Join(r.placeDir(key), rec+".json")
	r.writeJSON(p, map[string]any{"sessionId": rec, "cliSessionId": cli, "cwd": r.proj, "title": "made in the app"})
	return p
}

// recordsFor counts the records of conversation cli in list key.
func (r *rig) recordsFor(key, cli string) int {
	n := 0
	for _, path := range listRecords(r.placeDir(key)) {
		if h, ok := readHead(path); ok && h.Cli == cli {
			n++
		}
	}
	return n
}

// liveIn registers a Claude Code with conversation id open in config folder
// dir (a Claude Code of baton's session, in an account's profile): this
// test's own process stands in for it.
func (r *rig) liveIn(dir, id string) string {
	r.t.Helper()
	created := winproc.ProcessCreated(os.Getpid())
	p := filepath.Join(dir, "sessions", strconv.Itoa(os.Getpid())+".json")
	r.writeJSON(p, map[string]any{"pid": os.Getpid(), "sessionId": id, "cwd": r.proj, "kind": "interactive",
		"status": "idle", "entrypoint": "cli", "startedAt": created.UnixMilli() + 500})
	return p
}

// runOn writes the journal of a running baton session on conversation id
// that writes by transcript; this test's own process stands in for its
// supervisor and its claude.
func (r *rig) runOn(id, transcript string) string {
	r.t.Helper()
	dir := filepath.Join(r.d.RunsDir, run.NewID(time.Now()))
	must(r.t, os.MkdirAll(dir, 0o700))
	must(r.t, run.SaveJournal(dir, run.Journal{RunID: filepath.Base(dir), SupervisorPID: os.Getpid(),
		SupervisorCreated: winproc.ProcessCreated(os.Getpid()), State: run.StateRunning, ChildPID: os.Getpid(),
		SessionID: id, TranscriptPath: transcript, Cwd: r.proj, Started: time.Now()}))
	return dir
}

// A record the desktop app made itself opens the conversation through its
// second name (the app imported it from there): the name stays while that
// record is there, also after baton's own records went or moved on.
func TestSecondNameKeptForTheAppsOwnRecord(t *testing.T) {
	t.Run("unticked", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		own := r.appRecord(keyB, "local_d0000000-0000-4000-8000-0000000000d1", sid)
		if row := rowOf(r.view(sid, file), keyB); !row.Checked || !row.Locked || row.Why != "own" {
			t.Fatalf("row of the app's own record: %+v", row)
		}
		r.apply(sid, file)
		must(t, r.d.Reconcile())
		if !sameFile(h, file) {
			t.Fatal("the second name went while the app's own record of the conversation opens it")
		}
		must(t, os.Remove(own))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) || len(r.index().Threads) != 0 {
			t.Fatalf("not tidied once nothing needs it: second name %v, index %+v", isFile(h), r.index().Threads)
		}
	})
	t.Run("cleared", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		h := r.second(file, sid)
		own := r.appRecord(keyA, "local_d0000000-0000-4000-8000-0000000000d0", sid)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		if r.record(keyB, "local_"+sid)["cliSessionId"] != sid2 || !sameFile(r.second(next, sid2), next) {
			t.Fatal("baton's record did not move on")
		}
		if !sameFile(h, file) {
			t.Fatal("the old conversation's second name went while the app's own record of it opens it")
		}
		must(t, os.Remove(own))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the old second name stayed once nothing needs it")
		}
	})
}

// Two threads of baton's can need one second name: a session's earlier
// conversation (a prior id of one thread) shown on its own is another
// thread. Neither removes the name while the other needs it.
func TestSharedSecondName(t *testing.T) {
	setup := func(t *testing.T) (r *rig, file, next, h string) {
		r = newRig(t)
		file = r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.running = true // signed in to A: A's record waits to be pointed on
		next = r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid, file, keyB) // the earlier conversation, shown on its own
		if n := len(r.index().Threads); n != 2 {
			t.Fatalf("%d threads", n)
		}
		return r, file, next, r.second(file, sid)
	}
	t.Run("the session goes on", func(t *testing.T) {
		r, file, next, h := setup(t)
		r.running = false
		must(t, r.d.Touch(sid2, next))
		if r.record(keyA, "local_"+sid)["cliSessionId"] != sid2 {
			t.Fatal("A's record not pointed on")
		}
		if !sameFile(h, file) {
			t.Fatal("the second name went while B's record (the other thread's) opens it")
		}
		r.apply(sid, file)
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing needs it")
		}
	})
	t.Run("the earlier one unticked", func(t *testing.T) {
		r, file, _, h := setup(t)
		r.apply(sid, file) // B goes; A's record (the running app's list) still points at sid
		if !sameFile(h, file) {
			t.Fatal("the second name went while A's record (the other thread's) opens it")
		}
		r.running = false
		must(t, r.d.Reconcile())
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) || len(r.index().Threads) != 1 {
			t.Fatalf("not tidied once nothing needs it: second name %v, %d threads", isFile(h), len(r.index().Threads))
		}
	})
}

// A second name one thread finds made already (another thread's) is safe to
// take on: it is removed only when no thread needs it.
func TestAdoptedSecondNameStays(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	entry := r.live(sid, "local_"+sid) // the app opens baton's record: the session goes on in a copy
	copyFile := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, copyFile))
	must(t, os.Remove(entry))
	must(t, r.d.Reconcile())
	h := r.second(file, sid)
	r.apply(sid, file, keyB) // the original, shown in B as well
	if n := len(r.index().Threads); n != 2 {
		t.Fatalf("%d threads", n)
	}
	r.apply(sid, file)
	must(t, r.d.Reconcile())
	must(t, r.d.Touch(sid2, copyFile))
	if !sameFile(h, file) {
		t.Fatal("the second name the app's record (taken over from baton) opens was removed")
	}
}

// A Claude Code of baton's session in an account's profile that has the
// conversation open by its second name (resumed by that path) keeps it, as
// does a run whose journal names it while its claude is being started
// again; one that writes by the profile's own name does not.
func TestLiveClaudeKeepsSecondName(t *testing.T) {
	t.Run("registered in a profile", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		must(t, r.d.Touch(sid, h)) // baton run -- --resume <the second name>
		entry := r.liveIn(r.prof, sid)
		r.apply(sid, h)
		if !sameFile(h, file) {
			t.Fatal("the second name went while a claude in a profile has it open")
		}
		must(t, os.Remove(entry))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing has it open")
		}
	})
	t.Run("a run writes by it", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		dir := r.runOn(sid, h) // between two claudes (an account switch): none is registered
		r.apply(sid, file)
		if !sameFile(h, file) {
			t.Fatal("the second name went while a run of baton's writes by it")
		}
		must(t, os.RemoveAll(dir))
		must(t, r.d.Reconcile())
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed once nothing has it open")
		}
	})
	t.Run("baton's session writes by its own name", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		h := r.second(file, sid)
		r.liveIn(r.prof, sid)
		r.runOn(sid, file)
		r.apply(sid, file)
		if isFile(h) || !isFile(file) {
			t.Fatal("the second name stayed for baton's own session, which writes by the profile's name")
		}
	})
}

// A record write that fails leaves the index on disk naming no record that
// was never written: one the desktop app makes under that name meanwhile is
// the app's, never refreshed or removed by baton.
func TestFailedWriteLeavesIndex(t *testing.T) {
	// trip runs f once, the next time a record's time is taken: inside a
	// write, after baton looked whether the name is free.
	trip := func(r *rig, f func()) {
		r.d.Now = func() time.Time {
			if f != nil {
				g := f
				f = nil
				g()
			}
			return time.UnixMilli(1_800_000_000_000)
		}
	}
	t.Run("ticked", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		var own string
		trip(r, func() { own = r.appRecord(keyA, "local_"+sid, sid) })
		if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA, keyB}); err == nil {
			t.Fatal("no error")
		}
		if p := r.index().Threads[0].Places[keyA]; p != nil {
			t.Fatalf("the index on disk names a record baton never wrote: %+v", p)
		}
		if row := rowOf(r.view(sid, file), keyA); !row.Checked || !row.Locked || row.Why != "own" {
			t.Fatalf("row of the app's record: %+v", row)
		}
		before, _ := os.ReadFile(own)
		must(t, r.d.Reconcile())
		r.apply(sid, file, keyB)
		if after, _ := os.ReadFile(own); string(after) != string(before) {
			t.Fatalf("the app's record was changed or removed: %s", after)
		}
	})
	t.Run("waiting", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.live(sid, "local_"+sid) // the app opens baton's record: the session goes on in a copy
		next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
		must(t, r.d.Follow(sid, sid2, next)) // no file yet: the copy's record waits
		r.conversation(r.prof, sid2)
		theirs := filepath.Join(r.placeDir(keyA), "local_"+sid2+".json")
		// A first try fails with nothing left behind (the name is held for a
		// moment), so the copy's second name is in the index already...
		trip(r, func() { must(t, os.MkdirAll(theirs, 0o700)) })
		must(t, r.d.Touch(sid2, next))
		must(t, os.Remove(theirs))
		if p := r.index().Threads[0].Places[keyA]; p == nil || p.Written != 0 || len(r.index().Threads[0].Links) != 2 {
			t.Fatalf("after the first try: %+v", r.index().Threads[0])
		}
		// ...and at the second the app makes a record of that name meanwhile.
		content := `{"sessionId":"local_` + sid2 + `","cliSessionId":"f0000000-0000-4000-8000-00000000000f","cwd":"x"}`
		trip(r, func() { must(t, os.WriteFile(theirs, []byte(content), 0o600)) })
		must(t, r.d.Touch(sid2, next))
		if p := r.index().Threads[0].Places[keyA]; p == nil || p.Written != 0 {
			t.Fatalf("the index on disk says the copy's record was written: %+v", p)
		}
		must(t, r.d.Reconcile())
		if b, _ := os.ReadFile(theirs); string(b) != content {
			t.Fatalf("the app's file was changed: %s", b)
		}
		if n := r.recordsFor(keyA, sid2); n != 1 {
			t.Fatalf("%d records of the copy", n)
		}
	})
}

// A record of baton's the desktop app took over is the app's for every
// thread of baton's: the row of the conversation it shows is ticked and
// locked, and ticking it never puts a second record of the conversation
// beside it.
func TestTakenOverRecordIsTheApps(t *testing.T) {
	t.Run("released", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		r.live(sid, "local_"+sid) // the app opens A's record: baton's session goes on in a copy
		copyFile := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, copyFile))
		if row := rowOf(r.view(sid, file), keyA); !row.Checked || !row.Locked || row.Why != "released" {
			t.Fatalf("row of the record the app took over: %+v", row)
		}
		r.apply(sid, file, keyA)
		if n := r.recordsFor(keyA, sid); n != 1 || len(r.index().Threads) != 1 {
			t.Fatalf("%d records of the conversation in A, %d threads", n, len(r.index().Threads))
		}
	})
	t.Run("the app runs it", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.running = true
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next)) // A's record waits to be pointed on
		r.live(sid, "local_"+sid)            // and the app opens it meanwhile
		if row := rowOf(r.view(sid, file), keyA); !row.Checked || !row.Locked || row.Why != "released" {
			t.Fatalf("row of the record the app runs: %+v", row)
		}
		r.apply(sid, file, keyA)
		if n := r.recordsFor(keyA, sid); n != 1 {
			t.Fatalf("%d records of the conversation in A", n)
		}
	})
	t.Run("the app went on with it", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		const other = "f0000000-0000-4000-8000-00000000000f"
		m := r.record(keyB, "local_"+sid)
		m["cliSessionId"] = other // cleared in the app
		b, _ := json.Marshal(m)
		must(t, os.WriteFile(filepath.Join(r.placeDir(keyB), "local_"+sid+".json"), b, 0o600))
		theirs := r.conversation(r.home, other)
		if row := rowOf(r.view(other, theirs), keyB); !row.Checked || !row.Locked || row.Why != "released" {
			t.Fatalf("row of the record the app went on with: %+v", row)
		}
		r.apply(other, theirs, keyB)
		if n := r.recordsFor(keyB, other); n != 1 {
			t.Fatalf("%d records of the app's conversation in B", n)
		}
	})
}
