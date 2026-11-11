package desktop

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A tick the user gives a list lists the session there again and clears
// what a delete in the desktop app left there for its conversation (the
// app's deleted_<id>, and its <id>.desktop-released.json), as the app's own
// import of a conversation does: also when baton no longer keeps that place
// as deleted (an earlier record the app took over, let go of once deleted;
// a place unticked in baton and deleted in the running app meanwhile).
func TestReTickClearsWhatADeleteLeft(t *testing.T) {
	t.Run("an earlier record the app took over", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		r.adopt(keyB, sid, 1)
		must(t, r.d.Reconcile())
		r.apply(sid, file) // unticked: the app keeps it (t.Released)
		if ix := r.index(); ix.Thread(sid) == nil || len(ix.Thread(sid).Released) != 1 {
			t.Fatalf("setup: %+v", ix.Thread(sid))
		}
		tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_005_000)
		must(t, r.d.Reconcile())
		r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
		r.apply(sid, file, keyB) // ticked again by the user
		must(t, r.d.Reconcile())
		if r.recordsFor(keyB, sid) != 1 || isFile(tomb) || isFile(marker) {
			t.Fatalf("ticked again: %d record(s), tombstone left %v, released marker left %v", r.recordsFor(keyB, sid), isFile(tomb), isFile(marker))
		}
	})
	for _, quit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unticked, deleted in the running app, ticked again while it runs",
			true: "unticked, deleted in the running app, ticked again after it quit"}[quit], func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			r.running = true
			r.apply(sid, file) // hiding
			if p := r.place(sid, keyA); p == nil || p.State != stateHiding {
				t.Fatalf("setup: %+v", p)
			}
			tomb := filepath.Join(r.placeDir(keyA), "deleted_"+sid)
			must(t, os.WriteFile(tomb, []byte(strconv.FormatInt(1_800_000_005_000, 10)), 0o600))
			second := r.second(file, sid)
			_ = os.Remove(second)
			marker := filepath.Join(filepath.Dir(second), sid+".desktop-released.json")
			must(t, os.WriteFile(marker, []byte(`{"v":1,"reason":"delete"}`), 0o600))
			must(t, r.d.Reconcile())
			if quit {
				r.running = false
				must(t, r.d.Reconcile())
			}
			r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
			r.apply(sid, file, keyA)
			r.running = false
			must(t, r.d.Reconcile())
			if r.recordsFor(keyA, sid) != 1 || isFile(tomb) || isFile(marker) || !sameFile(second, file) {
				t.Fatalf("ticked again: %d record(s), tombstone left %v, released marker left %v, second name %v",
					r.recordsFor(keyA, sid), isFile(tomb), isFile(marker), sameFile(second, file))
			}
		})
	}
}

// What a delete left that a tick clears can be the only sign of another
// thread's delete: the session went on from the conversation (/clear while
// the running app had the list, so baton's record still pointed at the
// earlier conversation), the user deleted that record in the app, and,
// before any look, ticked the earlier conversation on its own (`baton
// desktop <id> --show`, which never looks). That other thread's record
// stays deleted and is never written again for the session; the earlier
// conversation is listed on its own.
func TestTickOnItsOwnKeepsAnotherThreadsDelete(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.running = true // signed in to A: baton does not change A on its own
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	if p := r.place(sid2, keyA); p == nil || p.State != stateShown || p.Cli != sid || r.recordsFor(keyA, sid) != 1 {
		t.Fatalf("setup: %+v", p)
	}
	tomb, _ := r.deleteInApp(keyA, "local_"+sid, sid, file, 1_800_000_005_000)
	must(t, os.Remove(r.second(file, sid))) // the app removes its name for the conversation
	r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_010_000) }
	v := r.view(sid, file)
	if v.Thread || rowOf(v, keyA).Checked {
		t.Fatalf("view of the earlier conversation: %+v", v)
	}
	r.apply(sid, file, append(sent(v), keyA)...)
	r.running = false
	must(t, r.d.Reconcile())
	must(t, r.d.Reconcile())
	if n := r.recordsFor(keyA, sid2); n != 0 {
		t.Errorf("the user deleted the session's record in A and never ticked A for it again, yet A lists %d record(s) of the session (%s); place %+v",
			n, sid2, r.place(sid2, keyA))
	}
	if p := r.place(sid2, keyA); p == nil || p.State != stateDeleted {
		t.Errorf("the session's place in A: %+v", p)
	}
	if b := rowOf(r.view(sid2, next), keyA); b.Checked || b.Why != "deleted" {
		t.Errorf("the session's row a: %+v", b)
	}
	if r.recordsFor(keyA, sid) != 1 || isFile(tomb) {
		t.Errorf("the earlier conversation, ticked on its own: %d record(s) in A, tombstone left %v", r.recordsFor(keyA, sid), isFile(tomb))
	}
}
