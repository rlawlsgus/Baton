package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"baton/internal/run"
)

// While a delete of a conversation runs (its marker is held), a tick never
// shows it in the desktop app and a session never follows on to it: the
// delete took baton's records off already, and a record and a second name
// written now would outlive it. Nothing is written; once the delete is
// over, the same tick goes through.
func TestNothingShownWhileItIsDeleted(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	release, err := run.MarkDeleting(filepath.Dir(r.d.IndexPath), sid)
	must(t, err)
	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA}); err == nil || err.(*Error).Code != "deleting" {
		t.Fatalf("apply: %v", err)
	}
	link := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	if ents, _ := os.ReadDir(r.placeDir(keyA)); len(ents) != 0 || isFile(link) || isFile(r.d.IndexPath) {
		t.Fatalf("written while deleting: records %d, second name %v, index %v", len(ents), isFile(link), isFile(r.d.IndexPath))
	}

	// A session that showed sid2 goes on in sid, which is being deleted.
	release()
	file2 := r.conversation(r.prof, sid2)
	r.apply(sid2, file2, keyA)
	release, err = run.MarkDeleting(filepath.Dir(r.d.IndexPath), sid)
	must(t, err)
	if err := r.d.Follow(sid2, sid, file); err == nil || err.(*Error).Code != "deleting" {
		t.Fatalf("follow: %v", err)
	}
	if ix := r.index(); len(ix.Threads) != 1 || ix.Threads[0].Session != sid2 || isFile(link) {
		t.Fatalf("followed while deleting: %+v, second name %v", ix.Threads, isFile(link))
	}
	release()

	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA}); err != nil {
		t.Fatalf("after the delete: %v", err)
	}
}
