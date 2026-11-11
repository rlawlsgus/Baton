package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// adopt rewrites baton's record of conversation id in key's list the way the
// desktop app does once it has the session: its own keys added, the
// conversation kept. turns is its completedTurns.
func (r *rig) adopt(key, id string, turns any) {
	r.t.Helper()
	path := filepath.Join(r.placeDir(key), "local_"+id+".json")
	b, err := os.ReadFile(path)
	must(r.t, err)
	m := map[string]any{}
	must(r.t, json.Unmarshal(b, &m))
	m["completedTurns"] = turns
	m["lastFocusedAt"] = 1_800_000_100_000
	m["model"] = "claude-opus-5-5"
	m["remoteMcpServersConfig"] = map[string]any{"servers": []any{}}
	r.writeJSON(path, m)
}

// Once the user went on with it in the desktop app (the app noted a turn of
// its own in the record), the record is the app's for good, also after the
// app's Claude Code is gone: the row is locked, an untick leaves the record,
// and the second name stays for it.
func TestWentOnInTheAppIsTheApps(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	h := r.d.linkPath(file)
	r.adopt(keyA, sid, 1)

	if a := rowOf(r.view(sid, file), keyA); !a.Checked || !a.Locked || a.Why != "released" {
		t.Fatalf("row a once the app went on with it: %+v", a)
	}
	must(t, r.d.Reconcile())
	ix := r.index()
	if p := placeOf(ix.Thread(sid), keyA); p == nil || p.State != stateReleased {
		t.Fatalf("after a look: %+v", p)
	}
	r.apply(sid, file) // unticked
	if r.record(keyA, "local_"+sid) == nil {
		t.Fatal("the untick removed the record the app went on with")
	}
	must(t, r.d.Reconcile())
	if !isFile(h) || !sameFile(h, file) {
		t.Fatal("the second name the app's record opens went")
	}
}

// While the desktop app's Claude Code has the session open, the row says so
// at once, before any look has marked the place released.
func TestOpenInTheAppLocksTheRowAtOnce(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.live(sid, "local_"+sid)
	if a := rowOf(r.view(sid, file), keyA); !a.Checked || !a.Locked || a.Why != "released" {
		t.Fatalf("row a while the app runs it: %+v", a)
	}
	ix := r.index()
	if p := placeOf(ix.Thread(sid), keyA); p == nil || p.State != stateShown {
		t.Fatalf("listing changed the index: %+v", p)
	}
}

// Deleted in the desktop app (the record gone, the app's deleted_<id> newer
// than baton's write): the row says so at once, before any look, and a
// look then marks the place deleted.
func TestDeletedInTheAppShowsAtOnce(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	dir := r.placeDir(keyA)
	must(t, os.Remove(filepath.Join(dir, "local_"+sid+".json")))
	must(t, os.WriteFile(filepath.Join(dir, "deleted_"+sid), []byte("1800000200000"), 0o600))
	if a := rowOf(r.view(sid, file), keyA); a.Checked || a.Locked || a.Why != "deleted" {
		t.Fatalf("row a once deleted in the app: %+v", a)
	}
	ix := r.index()
	if p := placeOf(ix.Thread(sid), keyA); p == nil || p.State != stateShown {
		t.Fatalf("listing changed the index: %+v", p)
	}
	must(t, r.d.Reconcile())
	ix = r.index()
	if p := placeOf(ix.Thread(sid), keyA); p == nil || p.State != stateDeleted {
		t.Fatalf("after a look: %+v", p)
	}
	if r.record(keyA, "local_"+sid) != nil {
		t.Fatal("baton wrote the record the user deleted in the app again")
	}
}

// A record that is merely missing (no deletion by the app) is not called
// deleted: baton writes it again once it is safe.
func TestMissingRecordIsNotDeleted(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	must(t, os.Remove(filepath.Join(r.placeDir(keyA), "local_"+sid+".json")))
	if a := rowOf(r.view(sid, file), keyA); !a.Checked || a.Why != "" {
		t.Fatalf("row a with its record missing: %+v", a)
	}
}

// A record the app only looked at (no turn of its own, or a count it writes
// in another form) stays baton's: the row is free and an untick takes it off.
func TestNoTurnOfTheAppsStaysBatons(t *testing.T) {
	for name, turns := range map[string]any{"none yet": 0, "not a number": "1", "missing": nil} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			r.adopt(keyA, sid, turns)
			if a := rowOf(r.view(sid, file), keyA); !a.Checked || a.Locked || a.Why != "" {
				t.Fatalf("row a: %+v", a)
			}
			r.apply(sid, file)
			if r.record(keyA, "local_"+sid) != nil {
				t.Fatal("the untick left baton's record")
			}
		})
	}
}
