package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func inBuilding(b Building, id string) bool {
	for _, f := range b.Floors {
		for _, s := range f.Sessions {
			if s.ID == id {
				return true
			}
		}
	}
	return false
}

// A conversation about to be deleted is let go of: it is no longer watched
// (its folder, subagents and all, can be moved away), the office looks
// around again at once, it leaves the building although the list read
// before its files went still has it, and asking for it shows nothing. One
// whose delete did not happen comes back later. Unknown ids are fine.
func TestForgetLetsGoOfAConversation(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	pa, pb := filepath.Join(w.root, "alpha"), filepath.Join(w.root, "beta")
	gone := w.conversation(w.prof, pa, sid1, "to delete", now.Add(-time.Minute))
	w.conversation(w.home, pb, sid2, "kept", now.Add(-2*time.Minute))
	agents := filepath.Join(filepath.Dir(gone), sid1, "subagents")
	_ = os.MkdirAll(agents, 0o700)
	_ = os.WriteFile(filepath.Join(agents, "agent-a1.meta.json"), []byte(`{"agentType":"Explore","description":"look","toolUseId":"toolu_a1"}`), 0o600)
	k := newKit(now.Add(-70 * time.Second))
	appendTo(t, filepath.Join(agents, "agent-a1.jsonl"), k.user(0, "look", map[string]any{"isSidechain": true, "agentId": "a1"}))
	w.look(now)
	r := w.poll(sid1, 0)
	if r.View == nil || r.View.ID != sid1 || !inBuilding(r.Building, sid1) || !inBuilding(r.Building, sid2) {
		t.Fatalf("before: view %+v, %s", r.View, numbers(r.Building))
	}
	w.tr.mu.Lock()
	watched := w.tr.sessions[sid1] != nil
	w.tr.mu.Unlock()
	if !watched {
		t.Fatal("not watched before")
	}

	select { // a poll woke the loop; only Forget's own kick counts below
	case <-w.tr.wake:
	default:
	}
	w.tr.Forget(sid1)
	w.tr.mu.Lock()
	_, watched = w.tr.sessions[sid1]
	_, known := w.tr.info[sid1]
	scanAt, follow := w.tr.scanAt, w.tr.follow
	w.tr.mu.Unlock()
	if watched || known || !scanAt.IsZero() || follow == sid1 || !w.tr.rescan.Load() {
		t.Fatalf("after Forget: watched %v known %v scanAt %v follow %q rescan %v", watched, known, scanAt, follow, w.tr.rescan.Load())
	}
	select {
	case <-w.tr.wake:
	default:
		t.Fatal("Forget did not wake the tracker")
	}

	// Its files are still there, and the list is read again before they go:
	// it stays out all the same, and asking for it shows nothing.
	w.rescan(now.Add(time.Second))
	w.step(now.Add(time.Second))
	if r = w.poll(sid1, 0); r.View != nil || inBuilding(r.Building, sid1) || !inBuilding(r.Building, sid2) {
		t.Fatalf("forgotten: view %+v, building has it %v", r.View, inBuilding(r.Building, sid1))
	}
	w.tr.mu.Lock()
	_, watched = w.tr.sessions[sid1]
	w.tr.mu.Unlock()
	if watched {
		t.Fatal("watched again")
	}
	// Moved away as the Recycle Bin does: nothing of it is open.
	if err := os.Rename(filepath.Join(filepath.Dir(gone), sid1), filepath.Join(w.root, "bin-folder")); err != nil {
		t.Fatalf("its folder could not be moved: %v", err)
	}
	if err := os.Rename(gone, filepath.Join(w.root, "bin-transcript")); err != nil {
		t.Fatalf("its transcript could not be moved: %v", err)
	}

	// A delete that did not go ahead: out for a while, then back. The one
	// that did stays out for good.
	w.tr.Forget(sid2)
	w.rescan(time.Now())
	if r = w.poll(sid2, 0); r.View != nil || inBuilding(r.Building, sid2) {
		t.Fatalf("kept, just forgotten: view %+v", r.View)
	}
	w.tr.mu.Lock()
	for id := range w.tr.forgot {
		w.tr.forgot[id] = time.Now().Add(-forgetFor)
	}
	w.tr.mu.Unlock()
	w.rescan(time.Now())
	if r = w.poll(sid2, 0); r.View == nil || r.View.ID != sid2 || !inBuilding(r.Building, sid2) {
		t.Fatalf("kept, after forgetFor: view %+v, %s", r.View, numbers(r.Building))
	}
	if r = w.poll(sid1, 0); r.View != nil || inBuilding(r.Building, sid1) {
		t.Fatalf("deleted, after forgetFor: view %+v", r.View)
	}
	w.tr.mu.Lock()
	left := len(w.tr.forgot)
	w.tr.mu.Unlock()
	if left != 0 {
		t.Fatalf("old entries not swept: %d", left)
	}

	w.tr.Forget("00000000-0000-4000-8000-000000000000")
	w.tr.Forget("")
}
