package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// What the desktop app did to a record of baton's (deleted it, took it
// over) is judged one way everywhere: the listing, Apply, Follow, a re-tick
// and every look note it first (settle), whichever of them comes first.

// deleteInApp does what the desktop app does when the user deletes the
// conversation of record rec (pointing at cli) in key's list: the record
// goes, deleted_<cli> holds the time (ms), and <cli>.desktop-released.json
// {reason: delete} is left next to the conversation's name in the user's
// own folder (file is a conversation file of that project).
func (r *rig) deleteInApp(key, rec, cli, file string, at int64) (tomb, marker string) {
	r.t.Helper()
	dir := r.placeDir(key)
	must(r.t, os.Remove(filepath.Join(dir, rec+".json")))
	tomb = filepath.Join(dir, "deleted_"+cli)
	must(r.t, os.WriteFile(tomb, []byte(strconv.FormatInt(at, 10)), 0o600))
	marker = filepath.Join(filepath.Dir(r.second(file, cli)), cli+".desktop-released.json")
	must(r.t, os.MkdirAll(filepath.Dir(marker), 0o700))
	must(r.t, os.WriteFile(marker, []byte(`{"v":1,"reason":"delete"}`), 0o600))
	return tomb, marker
}

// place is baton's place in key's list for the thread of conversation id,
// as desktop.json has it now.
func (r *rig) place(id, key string) *Place {
	r.t.Helper()
	ix := r.index()
	return placeOf(ix.Thread(id), key)
}

// sent is what the window's dialog and `baton desktop` send back for view v
// when the user changed nothing in it: every ticked row, also a locked one,
// as Kept.
func sent(v View) []string { return v.Ticked() }

// [0] A record of baton's it took off, which the desktop app wrote back
// after the user went on with it there (a turn of the app's own), is the
// app's: never removed, its row ticked and locked, its conversation's
// second name kept for it.
func TestGoneWrittenBackAfterATurnInTheAppIsTheApps(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		// A shows sid; the app runs on A; /clear moves the session on to sid2
		// (local_sid still points at sid); A is unticked and ticked again, so
		// local_sid is gone from baton's list while the app still has it.
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
		r.apply(sid2, next, keyA)
		// The user goes on with the entry the app still lists; the app quits,
		// writing it back: all before any look.
		must(t, os.WriteFile(old, before, 0o600))
		r.adopt(keyA, sid, 1)
		taken, err := os.ReadFile(old)
		must(t, err)
		r.running = false
		for range 3 {
			must(t, r.d.Reconcile())
		}
		if got, _ := os.ReadFile(old); string(got) != string(taken) {
			t.Fatal("baton removed or changed the record the user went on with in the app")
		}
		if !sameFile(h, file) {
			t.Fatal("the second name of the conversation the app went on with went")
		}
		if a, b := r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2); a != 1 || b != 1 {
			t.Fatalf("A has %d records of the earlier conversation and %d of the session's", a, b)
		}
		if g := r.index().Gone; len(g) != 0 {
			t.Fatalf("desktop.json still calls the app's record baton's: %+v", g)
		}
	})
	t.Run("unloaded", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
		before, err := os.ReadFile(rec)
		must(t, err)
		r.apply(sid, file) // unticked while safe: taken off at once
		must(t, os.WriteFile(rec, before, 0o600))
		r.adopt(keyB, sid, 1)
		taken, err := os.ReadFile(rec)
		must(t, err)
		if row := rowOf(r.view(sid, file), keyB); !row.Checked || !row.Locked {
			t.Fatalf("row b before any look: %+v", row)
		}
		r.apply(sid, file, keyB) // a tick puts no second record beside it
		must(t, r.d.Reconcile())
		if got, _ := os.ReadFile(rec); string(got) != string(taken) || r.recordsFor(keyB, sid) != 1 {
			t.Fatalf("the app's record was removed or changed, or another went beside it (%d records)", r.recordsFor(keyB, sid))
		}
		if row := rowOf(r.view(sid, file), keyB); !row.Checked || !row.Locked {
			t.Fatalf("row b after a look: %+v", row)
		}
	})
}

// [1] A take-over noticed only after /clear moved the session on (the user
// went on with the record waiting to be pointed on): the record stays the
// app's, and the session gets its "(baton)" record there, as when it is
// noticed before /clear.
func TestTakeOverNoticedAfterClearGetsTheCopy(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.running = true // signed in to A: A's record waits to be pointed on
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	r.adopt(keyA, sid, 1)
	theirs, err := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json"))
	must(t, err)
	if a := rowOf(r.view(sid2, next), keyA); !a.Checked || a.Locked || !a.Copy {
		t.Fatalf("row a before any look: %+v", a)
	}
	must(t, r.d.Reconcile())
	r.running = false
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	n, title := 0, ""
	for rec, path := range listRecords(r.placeDir(keyA)) {
		if h, ok := readHead(path); ok && h.Cli == sid2 {
			n++
			title, _ = r.record(keyA, rec)["title"].(string)
		}
	}
	ix := r.index()
	if n != 1 || title != "fix the <login> bug (baton)" {
		t.Fatalf("%d record(s) of the session in A (%q); place %+v", n, title, placeOf(ix.Thread(sid2), keyA))
	}
	if a := rowOf(r.view(sid2, next), keyA); !a.Checked || a.Locked || !a.Copy {
		t.Fatalf("row a: %+v", a)
	}
	if got, _ := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json")); string(got) != string(theirs) {
		t.Fatal("the record the app went on with was changed")
	}
	if !sameFile(r.second(file, sid), file) {
		t.Fatal("the second name the app's record opens went")
	}
}

// [2] A record the app took over, deleted in the app later: the row says
// deleted (unticked, free), /clear brings no record of baton's into that
// list, and a tick lists it again, clearing what the delete left.
func TestReleasedThenDeletedInTheApp(t *testing.T) {
	for _, look := range []bool{false, true} {
		t.Run(map[bool]string{false: "no look between", true: "a look between"}[look], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyB)
			r.adopt(keyB, sid, 1)
			if look {
				must(t, r.d.Reconcile())
				if p := r.place(sid, keyB); p == nil || p.State != stateReleased {
					t.Fatalf("after a look: %+v", p)
				}
			}
			tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_200_000)
			if b := rowOf(r.view(sid, file), keyB); b.Checked || b.Locked || b.Why != "deleted" {
				t.Fatalf("row b once deleted in the app: %+v", b)
			}
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			must(t, r.d.Reconcile())
			if n := len(listRecords(r.placeDir(keyB))); n != 0 {
				t.Fatalf("/clear wrote %d record(s) into the list where the user deleted it", n)
			}
			if p := r.place(sid2, keyB); p == nil || p.State != stateDeleted {
				t.Fatalf("place b: %+v", p)
			}
			if b := rowOf(r.view(sid2, next), keyB); b.Checked || b.Locked || b.Why != "deleted" {
				t.Fatalf("row b after /clear: %+v", b)
			}
			r.apply(sid2, next, keyB)
			if r.recordsFor(keyB, sid2) != 1 || isFile(tomb) || isFile(marker) {
				t.Fatalf("ticking again: %d record(s), tombstone left %v, marker left %v", r.recordsFor(keyB, sid2), isFile(tomb), isFile(marker))
			}
		})
	}
}

// [3] Ticked again after a delete in the app, before any look: listed
// again, and what the delete left goes, as after a look.
func TestReTickOfADeletedRowBeforeALook(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_005_000)
	v := r.view(sid, file)
	if b := rowOf(v, keyB); b.Checked || b.Why != "deleted" {
		t.Fatalf("row b: %+v", b)
	}
	r.apply(sid, file, append(sent(v), keyB)...)
	if !isFile(filepath.Join(r.placeDir(keyB), "local_"+sid+".json")) || isFile(tomb) || isFile(marker) {
		t.Fatalf("re-tick: record %v, tombstone left %v, released marker left %v",
			isFile(filepath.Join(r.placeDir(keyB), "local_"+sid+".json")), isFile(tomb), isFile(marker))
	}
}

// [5] The same, later (baton desktop <id> --show b): no later look leaves
// the delete's marks, and the folder baton made is not taken for one the
// app uses.
func TestDeletedRowReTickClearsTheDeleteForGood(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_005_000)
	if b := rowOf(r.view(sid, file), keyB); b.Checked || b.Why != "deleted" {
		t.Fatalf("row b: %+v", b)
	}
	r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
	r.apply(sid, file, keyB)
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	if isFile(tomb) || isFile(marker) {
		t.Fatalf("after the re-tick and two looks: tombstone left %v, released marker left %v", isFile(tomb), isFile(marker))
	}
	if b := rowOf(r.view(sid, file), keyB); !b.Checked || b.Why != "" || b.Used {
		t.Fatalf("row b: %+v", b)
	}
}

// [6] A row shown as deleted in the app, left as it is while the user
// changes another: it is not taken for an untick (nothing reported removed),
// stays deleted, and a later tick clears what the delete left.
func TestDeletedRowLeftAloneByAnotherChange(t *testing.T) {
	for _, onB := range []bool{false, true} {
		t.Run(map[bool]string{false: "app closed", true: "the app runs on B"}[onB], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyB)
			tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_005_000)
			if onB {
				r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctB})
				r.running = true
			}
			v := r.view(sid, file)
			res := r.apply(sid, file, append(sent(v), keyA)...)
			if slices.Contains(res.Removed, keyB) || res.ActiveRemoved {
				t.Fatalf("B reported taken off though the user left it: %+v", res)
			}
			if p := r.place(sid, keyB); p == nil || p.State != stateDeleted {
				t.Fatalf("place b: %+v", p)
			}
			if b := rowOf(r.view(sid, file), keyB); b.Why != "deleted" {
				t.Fatalf("row b lost its note: %+v", b)
			}
			r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
			r.apply(sid, file, keyA, keyB)
			if r.record(keyB, "local_"+sid) == nil || isFile(tomb) || isFile(marker) {
				t.Fatalf("ticked again: tombstone left %v, marker left %v", isFile(tomb), isFile(marker))
			}
		})
	}
}

// [7] baton's record not written yet (the copy's, waiting for its file)
// whose name another file has: the row stays ticked and free, a change
// keeps it, and the copy is written under another name.
func TestPendingCopyRowStaysFree(t *testing.T) {
	const other = "f0000000-0000-4000-8000-00000000000f"
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.live(sid, "local_"+sid)
	next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
	must(t, r.d.Follow(sid, sid2, next)) // no file yet: the copy's record waits
	theirs := r.appRecord(keyA, "local_"+sid2, other)
	content, err := os.ReadFile(theirs)
	must(t, err)
	r.conversation(r.prof, sid2)
	v := r.view(sid2, next)
	if a := rowOf(v, keyA); !a.Checked || a.Locked || !a.Copy {
		t.Fatalf("row a: %+v", a)
	}
	r.apply(sid2, next, sent(v)...)
	must(t, r.d.Touch(sid2, next))
	if n := r.recordsFor(keyA, sid2); n != 1 {
		t.Fatalf("%d record(s) of baton's copy in A", n)
	}
	if b, _ := os.ReadFile(theirs); string(b) != string(content) {
		t.Fatalf("the other file was changed: %s", b)
	}
}

// [8] Deleted in the app while the app's Claude Code for it is still
// registered: deleted, not taken over, before a look and after it, through
// a look or a change; no earlier record is kept for it, and its second
// name goes once nothing uses it.
func TestDeletedWhileTheAppStillRunsIt(t *testing.T) {
	for _, how := range []string{"look", "change"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			h := r.second(file, sid)
			entry := r.live(sid, "local_"+sid)
			r.deleteInApp(keyA, "local_"+sid, sid, file, 1_800_000_005_000)
			v := r.view(sid, file)
			if a := rowOf(v, keyA); a.Checked || a.Locked || a.Why != "deleted" {
				t.Fatalf("row a before a look: %+v", a)
			}
			if how == "look" {
				must(t, r.d.Reconcile())
			} else {
				r.apply(sid, file, sent(v)...)
			}
			if a := rowOf(r.view(sid, file), keyA); a.Checked || a.Locked || a.Why != "deleted" {
				t.Fatalf("row a after the %s: %+v", how, a)
			}
			ix := r.index()
			th := ix.Thread(sid)
			if p := placeOf(th, keyA); p == nil || p.State != stateDeleted || len(th.Released) != 0 {
				t.Fatalf("place a %+v, released %+v", p, th.Released)
			}
			must(t, os.Remove(entry))
			must(t, r.d.Reconcile())
			if isFile(h) || !isFile(file) {
				t.Fatal("the second name stayed for a record the user deleted, once nothing used it")
			}
		})
	}
}

// A dialog opened before the app took a record over shows its row free; an
// untick there releases it, whether or not a look noticed the take-over
// first: the app keeps its record, and no copy follows /clear into that list.
func TestUntickOfARowTakenOverSinceTheDialogOpened(t *testing.T) {
	for _, look := range []bool{false, true} {
		t.Run(map[bool]string{false: "no look between", true: "a look between"}[look], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA, keyB)
			v := r.view(sid, file)
			if a := rowOf(v, keyA); !a.Checked || a.Locked {
				t.Fatalf("row a when the dialog opened: %+v", a)
			}
			r.adopt(keyA, sid, 1)
			theirs, err := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json"))
			must(t, err)
			if look {
				must(t, r.d.Reconcile())
			}
			r.apply(sid, file, slices.DeleteFunc(sent(v), func(k string) bool { return k == Kept(keyA) })...)
			if p := r.place(sid, keyA); p != nil {
				t.Fatalf("the untick was not carried out: %+v", p)
			}
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			must(t, r.d.Reconcile())
			if n := r.recordsFor(keyA, sid2); n != 0 {
				t.Fatalf("A was unticked, yet baton wrote %d record(s) of the session there", n)
			}
			if got, _ := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json")); string(got) != string(theirs) {
				t.Fatal("the record the app went on with was removed or changed")
			}
			if !sameFile(r.second(file, sid), file) {
				t.Fatal("the second name the app's record opens went")
			}
		})
	}
}

// [4] A row the app took over since the tick (the user went on with it
// there, or the app runs it) is shown ticked and locked before any look; a
// change to another row keeps that tick: the list stays the app's, and
// baton's "(baton)" record follows /clear into it.
func TestTakenOverRowKeepsItsTick(t *testing.T) {
	for _, open := range []bool{false, true} {
		t.Run(map[bool]string{false: "went on", true: "open in the app"}[open], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			entry := ""
			if open {
				entry = r.live(sid, "local_"+sid)
			} else {
				r.adopt(keyA, sid, 1)
			}
			v := r.view(sid, file)
			if a := rowOf(v, keyA); !a.Checked || !a.Locked || a.Why != "released" {
				t.Fatalf("row a: %+v", a)
			}
			r.apply(sid, file, append(sent(v), keyB)...) // B ticked, A left as it was
			ix := r.index()
			if p := placeOf(ix.Thread(sid), keyA); p == nil || p.State != stateReleased {
				t.Fatalf("A's tick was lost: place %+v, released %+v", p, ix.Thread(sid).Released)
			}
			if b := ix.Badges()[sid]; !slices.Equal(b.Places, []string{"a, a2", "b"}) {
				t.Fatalf("badge: %+v", b)
			}
			if entry != "" {
				must(t, os.Remove(entry))
			}
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			must(t, r.d.Reconcile())
			if n := r.recordsFor(keyA, sid2); n != 1 {
				t.Fatalf("%d record(s) of the session in A after /clear", n)
			}
			if a := rowOf(r.view(sid2, next), keyA); !a.Checked || a.Locked || !a.Copy {
				t.Fatalf("row a after /clear: %+v", a)
			}
		})
	}
}

// moveOn rewrites baton's record rec in key's list as the desktop app does
// when the user goes on with it in a conversation of the app's own there
// (cleared it in the app): it points at x, the one before is kept in
// priorCliSessionIds, and the app's own turns are noted.
func (r *rig) moveOn(key, rec, x, before string) {
	r.t.Helper()
	must(r.t, rewrite(filepath.Join(r.placeDir(key), rec+".json"), func(m map[string]any) {
		m["cliSessionId"] = x
		m["priorCliSessionIds"] = []any{before}
		m["completedTurns"] = 2
	}))
}

// [9] A tick a client sends back from the view its dialog showed, which the
// user left as it was (View.Ticked), leaves the row as it is by the time it
// is applied: a record the user deleted in the app while the dialog was open
// is not written again, also one the row showed locked, and what the delete
// left stays. A tick the user gives the row afterwards lists it again.
func TestStaleTickNeverUndoesADelete(t *testing.T) {
	for _, how := range []string{"released", "released, the app runs on A", "free"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			if how != "free" {
				r.adopt(keyA, sid, 1)
				must(t, r.d.Reconcile())
			}
			r.running = how == "released, the app runs on A"
			v := r.view(sid, file)
			if a := rowOf(v, keyA); !a.Checked || a.Locked != (how != "free") {
				t.Fatalf("row a when the dialog opened: %+v", a)
			}
			tomb, marker := r.deleteInApp(keyA, "local_"+sid, sid, file, 1_800_000_005_000)
			r.apply(sid, file, append(v.Ticked(), keyB)...)
			if n := r.recordsFor(keyA, sid); n != 0 || !isFile(tomb) || !isFile(marker) {
				t.Fatalf("Apply wrote %d record(s) of the conversation into A; the app's delete marks left: tombstone %v, marker %v",
					n, isFile(tomb), isFile(marker))
			}
			if a := rowOf(r.view(sid, file), keyA); a.Checked || a.Locked || a.Why != "deleted" {
				t.Fatalf("row a after: %+v", a)
			}
			if r.record(keyB, "local_"+sid) == nil {
				t.Fatal("the tick of B was lost")
			}
			r.running = false
			r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
			r.apply(sid, file, keyA, keyB) // ticked by the user: listed again
			if r.recordsFor(keyA, sid) != 1 || isFile(tomb) || isFile(marker) {
				t.Fatalf("ticked again: %d record(s), tombstone left %v, marker left %v", r.recordsFor(keyA, sid), isFile(tomb), isFile(marker))
			}
		})
	}
}

// [10] The same for a row the app took over before any look, shown ticked
// and locked ("released"): the user cannot change its box, so it is never
// taken for a tick.
func TestLockedTickSentBackNeverRewritesADelete(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	r.adopt(keyB, sid, 1)
	v := r.view(sid, file)
	if b := rowOf(v, keyB); !b.Checked || !b.Locked || b.Why != "released" {
		t.Fatalf("row b when the dialog opened: %+v", b)
	}
	tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_200_000)
	r.apply(sid, file, append(v.Ticked(), keyA)...)
	if isFile(filepath.Join(r.placeDir(keyB), "local_"+sid+".json")) || !isFile(tomb) || !isFile(marker) {
		t.Fatalf("deleted record written again %v, tombstone left %v, marker left %v; place %+v",
			isFile(filepath.Join(r.placeDir(keyB), "local_"+sid+".json")), isFile(tomb), isFile(marker), r.place(sid, keyB))
	}
	if p := r.place(sid, keyB); p == nil || p.State != stateDeleted {
		t.Fatalf("place b: %+v", p)
	}
	if r.record(keyA, "local_"+sid) == nil {
		t.Fatal("the tick of A was lost")
	}
}

// [11] A row of the desktop app's own record of the conversation (locked,
// "own"), sent back while the user deletes that record in the app: baton
// puts no record of its own there.
func TestOwnRowSentBackNeverWritesBatons(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	own := r.appRecord(keyB, "local_d0000000-0000-4000-8000-0000000000d0", sid)
	v := r.view(sid, file)
	if b := rowOf(v, keyB); !b.Checked || !b.Locked || b.Why != "own" {
		t.Fatalf("row b when the dialog opened: %+v", b)
	}
	must(t, os.Remove(own))
	must(t, os.WriteFile(filepath.Join(r.placeDir(keyB), "deleted_"+sid), []byte("1800000200000"), 0o600))
	r.apply(sid, file, append(v.Ticked(), keyA)...)
	if n := r.recordsFor(keyB, sid); n != 0 {
		t.Fatalf("baton wrote %d record(s) of the conversation into B; place %+v", n, r.place(sid, keyB))
	}
	if r.record(keyA, "local_"+sid) == nil {
		t.Fatal("the tick of A was lost")
	}
}

// [12] A record the app moved on to a conversation of its own (the user
// cleared it there), deleted in the app since: the app's delete names that
// conversation (deleted_<x>), not baton's. It is deleted all the same, with
// or without a look between the move and the delete: the row says so, /clear
// brings no record of baton's into that list, nothing of it is kept, and its
// conversation's second name goes once nothing uses it.
func TestDeletedAfterTheAppMovedItOn(t *testing.T) {
	const x = "f0000000-0000-4000-8000-00000000000f"
	for _, look := range []bool{true, false} {
		t.Run(map[bool]string{false: "no look between", true: "a look between"}[look], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyB)
			h := r.second(file, sid)
			r.moveOn(keyB, "local_"+sid, x, sid)
			if look {
				must(t, r.d.Reconcile())
				if p := r.place(sid, keyB); p == nil || p.State != stateReleased {
					t.Fatalf("after a look: %+v", p)
				}
			}
			r.deleteInApp(keyB, "local_"+sid, x, file, 1_800_000_005_000)
			must(t, r.d.Reconcile())
			if n := len(listRecords(r.placeDir(keyB))); n != 0 {
				t.Fatalf("a look wrote %d record(s) into B; place %+v", n, r.place(sid, keyB))
			}
			if b := rowOf(r.view(sid, file), keyB); b.Checked || b.Locked || b.Why != "deleted" {
				t.Fatalf("row b once deleted in the app: %+v", b)
			}
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			must(t, r.d.Reconcile())
			must(t, r.d.Reconcile())
			if n := len(listRecords(r.placeDir(keyB))); n != 0 {
				t.Fatalf("/clear wrote %d record(s) into B", n)
			}
			ix := r.index()
			if th := ix.Thread(sid2); th != nil && len(th.Released) != 0 {
				t.Fatalf("an earlier record is kept for what the user deleted: %+v", th.Released)
			}
			if b := rowOf(r.view(sid2, next), keyB); b.Checked || b.Locked || b.Why != "deleted" {
				t.Fatalf("row b after /clear: %+v", b)
			}
			if isFile(h) || !isFile(file) {
				t.Fatal("the second name stayed for a record the user deleted, once nothing used it")
			}
		})
	}
}

// [13] More of the same: a tick lists it again and clears what the delete
// left; a record the app took over first and moved on later, after /clear,
// is let go of once deleted (baton's own copy there stays); and a record
// merely missing, where the app deleted a conversation of another project
// since, is still written again.
func TestDeletedAfterTheAppMovedItOnMore(t *testing.T) {
	const x = "f0000000-0000-4000-8000-00000000000f"
	t.Run("ticked again", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		r.moveOn(keyB, "local_"+sid, x, sid)
		must(t, r.d.Reconcile())
		tomb, marker := r.deleteInApp(keyB, "local_"+sid, x, file, 1_800_000_005_000)
		must(t, r.d.Reconcile())
		r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
		r.apply(sid, file, keyB)
		if r.recordsFor(keyB, sid) != 1 || isFile(tomb) || isFile(marker) {
			t.Fatalf("ticked again: %d record(s), tombstone left %v, marker left %v", r.recordsFor(keyB, sid), isFile(tomb), isFile(marker))
		}
		must(t, r.d.Reconcile())
		if p := r.place(sid, keyB); p == nil || p.State != stateShown || len(p.Moved) != 0 {
			t.Fatalf("after a look: %+v", p)
		}
	})
	for _, look := range []bool{true, false} {
		t.Run(map[bool]string{false: "taken over, moved on after /clear, no look between", true: "taken over, moved on after /clear"}[look], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyB)
			h := r.second(file, sid)
			r.adopt(keyB, sid, 1)
			must(t, r.d.Reconcile())
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			must(t, r.d.Reconcile())
			if ix := r.index(); len(ix.Thread(sid2).Released) != 1 || r.recordsFor(keyB, sid2) != 1 {
				t.Fatalf("setup: released %+v, %d copy record(s)", ix.Thread(sid2).Released, r.recordsFor(keyB, sid2))
			}
			r.moveOn(keyB, "local_"+sid, x, sid)
			if look {
				must(t, r.d.Reconcile())
			}
			r.deleteInApp(keyB, "local_"+sid, x, file, 1_800_000_005_000)
			must(t, r.d.Reconcile())
			ix := r.index()
			if th := ix.Thread(sid2); len(th.Released) != 0 {
				t.Fatalf("the record the user deleted is still kept: %+v", th.Released)
			}
			if b := rowOf(r.view(sid2, next), keyB); !b.Checked || b.Locked || !b.Copy || r.recordsFor(keyB, sid2) != 1 {
				t.Fatalf("baton's copy: row %+v, %d record(s)", b, r.recordsFor(keyB, sid2))
			}
			if isFile(h) {
				t.Fatal("the earlier conversation's second name stayed once nothing used it")
			}
		})
	}
	t.Run("missing, another project's conversation deleted there", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		must(t, os.Remove(filepath.Join(r.placeDir(keyB), "local_"+sid+".json")))
		must(t, os.WriteFile(filepath.Join(r.placeDir(keyB), "deleted_"+x), []byte("1800000005000"), 0o600))
		must(t, r.d.Reconcile())
		if r.record(keyB, "local_"+sid) == nil {
			t.Fatalf("a record merely missing was not written again: %+v", r.place(sid, keyB))
		}
	})
}

// [14] baton's record took another name while the running app still listed
// the old one (the list was unticked and ticked again before the app let go
// of it), and the user went on with that old one in the app, which points at
// the same conversation: that is the app's record of the conversation there.
// The row is ticked and locked, the app's record stays as it is, baton's own
// goes once that is safe (never two records of one conversation in one
// list), and after /clear the session gets its "(baton)" record there.
func TestTwinOfARecordTheAppWentOnWith(t *testing.T) {
	const sid3 = "5e550000-0000-4000-8000-000000000003"
	for _, quit := range []bool{true, false} {
		t.Run(map[bool]string{false: "the app still runs", true: "the app quit"}[quit], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next)) // the app is closed: local_sid points at sid2 now
			old := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
			if h, _ := readHead(old); h.Cli != sid2 {
				t.Fatalf("setup: %+v", h)
			}
			before, err := os.ReadFile(old)
			must(t, err)
			r.running = true // the app starts on A, and lists local_sid
			r.apply(sid2, next)
			r.apply(sid2, next, keyA)
			must(t, os.WriteFile(old, before, 0o600)) // the user goes on with it in the app
			r.adopt(keyA, sid, 1)
			theirs, err := os.ReadFile(old)
			must(t, err)
			if quit {
				r.running = false
			}
			must(t, r.d.Reconcile())
			must(t, r.d.Reconcile())
			if a := rowOf(r.view(sid2, next), keyA); !a.Checked || !a.Locked || a.Why != "released" {
				t.Fatalf("row a: %+v", a)
			}
			if !quit {
				r.running = false
				must(t, r.d.Reconcile())
			}
			if n := r.recordsFor(keyA, sid2); n != 1 {
				t.Fatalf("%d records of the session's conversation in A; place %+v", n, r.place(sid2, keyA))
			}
			if got, _ := os.ReadFile(old); string(got) != string(theirs) {
				t.Fatal("baton removed or changed the record the user went on with in the app")
			}
			if !sameFile(r.second(next, sid2), next) {
				t.Fatal("the second name the app's record opens went")
			}
			ix := r.index()
			if ix.gone(r.placeDir(keyA), "local_"+sid) != nil {
				t.Fatalf("desktop.json still calls the app's record baton's: %+v", ix.Gone)
			}
			third := r.conversation(r.prof, sid3)
			must(t, r.d.Follow(sid2, sid3, third))
			must(t, r.d.Reconcile())
			if n := r.recordsFor(keyA, sid3); n != 1 {
				t.Fatalf("%d record(s) of the session in A after /clear", n)
			}
			if a := rowOf(r.view(sid3, third), keyA); !a.Checked || a.Locked || !a.Copy {
				t.Fatalf("row a after /clear: %+v", a)
			}
			if got, _ := os.ReadFile(old); string(got) != string(theirs) {
				t.Fatal("the record the app went on with was changed after /clear")
			}
		})
	}
	t.Run("noticed at /clear", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		old := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
		before, err := os.ReadFile(old)
		must(t, err)
		r.running = true
		r.apply(sid2, next)
		r.apply(sid2, next, keyA)
		must(t, os.WriteFile(old, before, 0o600))
		r.adopt(keyA, sid, 1)
		theirs, err := os.ReadFile(old)
		must(t, err)
		r.running = false
		third := r.conversation(r.prof, sid3)
		must(t, r.d.Follow(sid2, sid3, third)) // before any look
		must(t, r.d.Reconcile())
		must(t, r.d.Reconcile())
		if a, b := r.recordsFor(keyA, sid2), r.recordsFor(keyA, sid3); a != 1 || b != 1 {
			t.Fatalf("A has %d record(s) of the earlier conversation and %d of the session's", a, b)
		}
		if a := rowOf(r.view(sid3, third), keyA); !a.Checked || a.Locked || !a.Copy {
			t.Fatalf("row a after /clear: %+v", a)
		}
		if got, _ := os.ReadFile(old); string(got) != string(theirs) {
			t.Fatal("baton removed or changed the record the user went on with in the app")
		}
		if !sameFile(r.second(next, sid2), next) {
			t.Fatal("the second name the app's record opens went")
		}
	})
}
