package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// One delete in the desktop app leaves one deleted_<id>. Records of baton's
// in one list that the app moved on to conversations of its own and that the
// user then deleted there, before baton looked, are each deleted: each takes
// one of those deletes, whichever of them is looked at first (by a look, or
// by Apply for one session), and none is written again.
func TestMovedOnAndDeletedRecordsEachTakeOneDelete(t *testing.T) {
	const x = "f0000000-0000-4000-8000-00000000000f"
	const y = "f0000000-0000-4000-8000-0000000000ff"
	for _, how := range []string{"no look", "a look after the first move", "the second session's dialog first"} {
		t.Run("two sessions, "+how, func(t *testing.T) {
			r := newRig(t)
			f1 := r.conversation(r.prof, sid)
			f2 := r.conversation(r.prof, sid2)
			r.apply(sid, f1, keyB)
			r.apply(sid2, f2, keyB)
			r.moveOn(keyB, "local_"+sid, x, sid)
			if how == "a look after the first move" {
				must(t, r.d.Reconcile())
			}
			r.moveOn(keyB, "local_"+sid2, y, sid2)
			r.deleteInApp(keyB, "local_"+sid, x, f1, 1_800_000_005_000)
			r.deleteInApp(keyB, "local_"+sid2, y, f2, 1_800_000_006_000)
			for _, s := range []struct{ id, file string }{{sid, f1}, {sid2, f2}} {
				if b := rowOf(r.view(s.id, s.file), keyB); b.Checked || b.Why != "deleted" {
					t.Fatalf("row b of %s before a look: %+v", s.id, b)
				}
			}
			if how == "the second session's dialog first" {
				r.apply(sid2, f2, sent(r.view(sid2, f2))...)
			}
			must(t, r.d.Reconcile())
			must(t, r.d.Reconcile())
			ix := r.index()
			if n := len(listRecords(r.placeDir(keyB))); n != 0 {
				t.Fatalf("baton wrote %d record(s) into B that the user deleted in the app; place 1 %+v, place 2 %+v",
					n, placeOf(ix.Thread(sid), keyB), placeOf(ix.Thread(sid2), keyB))
			}
			for _, s := range []struct{ id, file string }{{sid, f1}, {sid2, f2}} {
				p := placeOf(ix.Thread(s.id), keyB)
				if p == nil || p.State != stateDeleted || len(p.Moved) != 1 {
					t.Fatalf("place b of %s: %+v", s.id, p)
				}
				if b := rowOf(r.view(s.id, s.file), keyB); b.Checked || b.Why != "deleted" {
					t.Fatalf("row b of %s after a look: %+v", s.id, b)
				}
			}
		})
	}
	t.Run("an earlier record and its copy", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		r.adopt(keyB, sid, 1)
		must(t, r.d.Reconcile())
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		if ix := r.index(); len(ix.Thread(sid2).Released) != 1 || r.recordsFor(keyB, sid2) != 1 {
			t.Fatalf("setup: %+v", ix.Thread(sid2))
		}
		r.moveOn(keyB, "local_"+sid, x, sid)
		r.moveOn(keyB, "local_"+sid2, y, sid2)
		r.deleteInApp(keyB, "local_"+sid, x, file, 1_800_000_005_000)
		r.deleteInApp(keyB, "local_"+sid2, y, next, 1_800_000_006_000)
		// Settling one index twice says the same: what Follow does (it
		// settles twice) and a look does (once) agree.
		ix := r.index()
		th := ix.Thread(sid2)
		pr := r.d.look(&ix)
		r.d.settle(&ix, th, pr)
		once := th.Places[keyB].State
		r.d.settle(&ix, th, pr)
		if twice := th.Places[keyB].State; once != stateDeleted || twice != once {
			t.Fatalf("settle once: copy place %q; settle again on the same index: %q", once, twice)
		}
		must(t, r.d.Reconcile())
		must(t, r.d.Reconcile())
		if n := len(listRecords(r.placeDir(keyB))); n != 0 {
			b, _ := os.ReadFile(filepath.Join(r.placeDir(keyB), "local_"+sid2+".json"))
			t.Fatalf("a look wrote %d record(s) into B though the user deleted both there: %s", n, b)
		}
		if ix := r.index(); len(ix.Thread(sid2).Released) != 0 || placeOf(ix.Thread(sid2), keyB).State != stateDeleted {
			t.Fatalf("thread: %+v", ix.Thread(sid2))
		}
	})
}

// Another session's earlier record the app took over, moved on and deleted
// there along with one of the first session's: it takes its own delete, so
// it is let go of, and so is its conversation's second name once nothing
// uses it.
func TestMovedOnAndDeletedReleasedOfAnotherSessionGoes(t *testing.T) {
	const x = "f0000000-0000-4000-8000-00000000000f"
	const y = "f0000000-0000-4000-8000-0000000000ff"
	const sid3 = "5e550000-0000-4000-8000-000000000003"
	const sid4 = "5e550000-0000-4000-8000-000000000004"
	r := newRig(t)
	f1 := r.conversation(r.prof, sid)
	r.apply(sid, f1, keyB)
	f3 := r.conversation(r.prof, sid3)
	r.apply(sid3, f3, keyB)
	h3 := r.second(f3, sid3)
	r.adopt(keyB, sid3, 1)
	must(t, r.d.Reconcile())
	f4 := r.conversation(r.prof, sid4)
	must(t, r.d.Follow(sid3, sid4, f4))
	must(t, r.d.Reconcile())
	if ix := r.index(); len(ix.Thread(sid4).Released) != 1 || r.recordsFor(keyB, sid4) != 1 {
		t.Fatalf("setup: %+v", ix.Thread(sid4))
	}
	r.moveOn(keyB, "local_"+sid, x, sid)
	r.moveOn(keyB, "local_"+sid3, y, sid3)
	r.deleteInApp(keyB, "local_"+sid, x, f1, 1_800_000_005_000)
	r.deleteInApp(keyB, "local_"+sid3, y, f3, 1_800_000_006_000)
	for range 3 {
		must(t, r.d.Reconcile())
	}
	ix := r.index()
	if n := len(ix.Thread(sid4).Released); n != 0 {
		t.Errorf("session 2 keeps %d earlier record(s) the user deleted in the app: %+v; session 1's place %+v",
			n, ix.Thread(sid4).Released, placeOf(ix.Thread(sid), keyB))
	}
	if p := placeOf(ix.Thread(sid), keyB); p == nil || p.State != stateDeleted {
		t.Errorf("session 1's place: %+v", p)
	}
	if isFile(h3) {
		t.Error("the second name of session 2's earlier conversation stays though nothing uses it")
	}
	if r.recordsFor(keyB, sid4) != 1 {
		t.Error("session 2's own record went")
	}
}

// A record whose move baton saw (Moved [x]) and that the user then deleted
// (deleted_x) is deleted with its own delete: a conversation of the app's own
// deleted in that list meanwhile is not taken for one it was moved on to, and
// a tick of the list again leaves what that other delete left.
func TestMovedOnSeenThenDeletedTakesNoOtherDelete(t *testing.T) {
	const x = "f0000000-0000-4000-8000-00000000000f"
	const y = "e0000000-0000-4000-8000-0000000000ee"
	const yrec = "local_d0000000-0000-4000-8000-0000000000d0"
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	r.moveOn(keyB, "local_"+sid, x, sid)
	must(t, r.d.Reconcile())
	if p := r.place(sid, keyB); p == nil || p.State != stateReleased || len(p.Moved) != 1 {
		t.Fatalf("setup: %+v", p)
	}
	r.appRecord(keyB, yrec, y)
	ytomb, ymarker := r.deleteInApp(keyB, yrec, y, file, 1_800_000_004_000)
	r.deleteInApp(keyB, "local_"+sid, x, file, 1_800_000_005_000)
	must(t, r.d.Reconcile())
	p := r.place(sid, keyB)
	if p == nil || p.State != stateDeleted {
		t.Fatalf("not deleted: %+v", p)
	}
	if !slices.Equal(p.Moved, []string{x}) {
		t.Errorf("Y, the app's own conversation, was noted as one baton's record moved on to: %v", p.Moved)
	}
	r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
	r.apply(sid, file, keyB)
	if !isFile(ytomb) || !isFile(ymarker) {
		t.Errorf("the re-tick cleared what the user's delete of the app's own conversation Y left: tombstone %v, marker %v",
			isFile(ytomb), isFile(ymarker))
	}
	if r.recordsFor(keyB, sid) != 1 {
		t.Error("the re-tick did not list it again")
	}
}
