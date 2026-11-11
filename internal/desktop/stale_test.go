package desktop

import (
	"path/filepath"
	"slices"
	"testing"
)

// The window's dialog keeps the session it was opened for while it is open.
// When the session goes on in another conversation meanwhile (/clear, or a
// copy because the app opened its record), what the user picks there is the
// session's: an untick takes the session off that list, also the "(baton)"
// copy that followed it there, and nothing else changes.
func TestStaleDialogAfterFollowKeepsTheUntick(t *testing.T) {
	for _, how := range []string{"plain /clear", "opened in the app, baton goes on in a copy"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA, keyB)
			v := r.view(sid, file) // the dialog opens: A and B ticked, free
			if a := rowOf(v, keyA); !a.Checked || a.Locked {
				t.Fatalf("row a when the dialog opened: %+v", a)
			}
			taken := how != "plain /clear"
			if taken {
				r.live(sid, "local_"+sid)
			}
			next := r.conversation(r.prof, sid2)
			must(t, r.d.Follow(sid, sid2, next))
			res := r.apply(sid, file, slices.DeleteFunc(sent(v), func(k string) bool { return k == Kept(keyA) })...)
			must(t, r.d.Reconcile())
			must(t, r.d.Reconcile())
			if n := r.recordsFor(keyA, sid2); n != 0 || !slices.Equal(res.Removed, []string{keyA}) {
				t.Fatalf("the user unticked A, yet the session (now %s) has %d record(s) of baton's in A; Apply said %+v; place %+v",
					sid2, n, res, r.place(sid2, keyA))
			}
			if n := r.recordsFor(keyB, sid2); n != 1 {
				t.Fatalf("B, left ticked, has %d record(s) of the session", n)
			}
			if n := len(r.index().Threads); n != 1 {
				t.Fatalf("%d threads", n)
			}
			if taken && r.record(keyA, "local_"+sid) == nil {
				t.Fatal("the record the app took over was removed")
			}
		})
	}
}

// The same dialog, a list ticked there: the session is listed there, by the
// conversation it is on now, and no thread is made for the earlier one. An
// earlier conversation asked about on its own (its view has no thread) is
// still listed on its own.
func TestStaleDialogAfterFollowTicksTheSession(t *testing.T) {
	t.Run("the dialog opened before /clear", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		v := r.view(sid, file)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		res := r.apply(sid, file, append(sent(v), keyB)...)
		must(t, r.d.Reconcile())
		ix := r.index()
		if r.recordsFor(keyB, sid2) != 1 || r.recordsFor(keyB, sid) != 0 || len(ix.Threads) != 1 {
			t.Fatalf("the user ticked B for the session, but B lists %d record(s) of the session (%s) and %d of the earlier conversation (%s); threads: %d; Apply said %+v",
				r.recordsFor(keyB, sid2), sid2, r.recordsFor(keyB, sid), sid, len(ix.Threads), res)
		}
		if r.recordsFor(keyA, sid2) != 1 {
			t.Fatal("A, left ticked, lost the session")
		}
	})
	t.Run("the earlier conversation on its own", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		v := r.view(sid, file)
		if v.Thread || rowOf(v, keyB).Checked {
			t.Fatalf("view of the earlier conversation: %+v", v)
		}
		r.apply(sid, file, append(sent(v), keyB)...)
		if r.recordsFor(keyB, sid) != 1 || len(r.index().Threads) != 2 {
			t.Fatalf("B lists %d record(s) of the earlier conversation; %d threads", r.recordsFor(keyB, sid), len(r.index().Threads))
		}
	})
	t.Run("the earlier conversation's own thread, gone while its dialog is open", func(t *testing.T) {
		// Its dialog is not the session's that went on from it: what the
		// user picks there stays the earlier conversation's.
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		must(t, r.d.Reconcile())
		r.apply(sid, file, keyB) // the earlier conversation, in B on its own
		v := r.view(sid, file)
		if v.Thread || !rowOf(v, keyB).Checked {
			t.Fatalf("view of the earlier conversation's own thread: %+v", v)
		}
		r.apply(sid, file) // unticked elsewhere meanwhile: its thread goes
		if n := len(r.index().Threads); n != 1 {
			t.Fatalf("setup: %d threads", n)
		}
		r.apply(sid, file, append(sent(v), keyA)...)
		if r.recordsFor(keyA, sid) != 1 || r.recordsFor(keyA, sid2) != 1 || r.recordsFor(keyB, sid2) != 0 {
			t.Fatalf("A lists %d record(s) of the earlier conversation and %d of the session's; B %d of the session's",
				r.recordsFor(keyA, sid), r.recordsFor(keyA, sid2), r.recordsFor(keyB, sid2))
		}
	})
}

// An Apply that fails part way keeps what it did (the index says what is on
// disk), and the dialog stays open. A row it took off is no longer ticked as
// it was when the dialog opened, so a box the user ticks again afterwards is
// sent as a tick (its key, which app.js does after an error), and lists the
// session there again.
func TestReTickAfterAFailedApply(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	v := r.view(sid, file) // A and B ticked
	release := hold(t, filepath.Join(r.placeDir(keyB), "local_"+sid+".json"))
	unticked := slices.DeleteFunc(sent(v), func(k string) bool { return k == Kept(keyA) || k == Kept(keyB) })
	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, unticked); err == nil {
		t.Fatal("setup: the first Apply should fail")
	}
	release()
	if a := rowOf(r.view(sid, file), keyA); a.Checked {
		t.Fatalf("setup: the failed Apply left A: %+v", a)
	}
	res := r.apply(sid, file, append(unticked, keyA)...)
	if r.record(keyA, "local_"+sid) == nil || r.record(keyB, "local_"+sid) != nil {
		t.Fatalf("after the failed Apply, A ticked again and B left unticked: %+v; A %v, B %v",
			res, r.record(keyA, "local_"+sid) != nil, r.record(keyB, "local_"+sid) != nil)
	}
}
