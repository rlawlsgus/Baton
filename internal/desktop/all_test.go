package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/claude"
	"baton/internal/run"
)

const (
	sid3 = "5e550000-0000-4000-8000-000000000003"
	sid4 = "5e550000-0000-4000-8000-000000000004"
	sid5 = "5e550000-0000-4000-8000-000000000005"
	own2 = "local_0aa00000-0000-4000-8000-000000000002" // the desktop app's own record of sid2
)

// conversationIn writes conversation id that ran in folder cwd, in config
// folder dir, and returns its file.
func (r *rig) conversationIn(dir, id, cwd string) string {
	r.t.Helper()
	p := filepath.Join(dir, "projects", claude.ProjectSlug(cwd), id+".jsonl")
	must(r.t, os.MkdirAll(filepath.Dir(p), 0o700))
	q := strconv.Quote(cwd)
	rows := `{"type":"user","cwd":` + q + `,"timestamp":"2026-11-02T10:00:00.000Z","message":{"role":"user","content":"conversation ` + id + `"}}` + "\n" +
		`{"type":"assistant","cwd":` + q + `,"timestamp":"2026-11-02T10:05:00.000Z","message":{"role":"assistant","content":"ok"}}` + "\n"
	must(r.t, os.WriteFile(p, []byte(rows), 0o600))
	return p
}

// allRig is a rig with the conversations ShowAll is asked about:
//
//	sid  in account a's profile: listed everywhere, with a second name
//	sid2 the desktop app's own in list A (which also names sid5 as one it
//	     went on from): listed in the other lists only
//	sid3 deleted in the app's list X: listed in A and B only
//	sid4 its folder is gone: listed nowhere
//	sid5 an earlier conversation of the app's session sid2: listed nowhere
//
// List A is the account the app is signed in to, B is baton's account b
// (the app has no list for it yet), X an account the app was on before.
func allRig(t *testing.T) (*rig, []Conversation) {
	r := newRig(t)
	must(t, os.MkdirAll(r.placeDir(keyX), 0o700))
	r.writeJSON(filepath.Join(r.placeDir(keyA), own2+".json"), map[string]any{"sessionId": own2, "cliSessionId": sid2,
		"priorCliSessionIds": []any{sid5, 7}, "cwd": r.proj, "title": "the app's own"})
	must(t, os.WriteFile(filepath.Join(r.placeDir(keyX), "deleted_"+sid3), []byte("1700000000000"), 0o600))
	convs := []Conversation{
		{ID: sid, Transcript: r.conversation(r.prof, sid)},
		{ID: sid2, Transcript: r.conversationIn(r.home, sid2, r.proj)},
		{ID: sid3, Transcript: r.conversationIn(r.home, sid3, r.proj)},
		{ID: sid4, Transcript: r.conversationIn(r.home, sid4, filepath.Join(r.root, "gone"))},
		{ID: sid5, Transcript: r.conversationIn(r.home, sid5, r.proj)},
	}
	return r, convs
}

// listed are the conversations list key's records point at, sorted.
func (r *rig) listed(key string) []string {
	r.t.Helper()
	var out []string
	for _, p := range listRecords(r.placeDir(key)) {
		if h, ok := readHead(p); ok {
			out = append(out, h.Cli)
		}
	}
	slices.Sort(out)
	return out
}

func TestShowAll(t *testing.T) {
	r, convs := allRig(t)
	res, err := r.d.ShowAll(convs, 0, nil)
	must(t, err)
	if res.Added != 7 || res.Conversations != 3 || res.More || !res.ActiveAdded {
		t.Fatalf("result: %+v", res)
	}
	for key, want := range map[string][]string{keyA: {sid, sid2, sid3}, keyB: {sid, sid2, sid3}, keyX: {sid, sid2}} {
		if got := r.listed(key); !slices.Equal(got, want) {
			t.Fatalf("list %s: %v, want %v", key[:8], got, want)
		}
	}
	// The app's own record stays as it was; baton's are its usual records,
	// marked as the setting's.
	if m := r.record(keyA, own2); m["title"] != "the app's own" {
		t.Fatalf("the app's own record: %v", m)
	}
	if m := r.record(keyB, "local_"+sid); m["cliSessionId"] != sid || m["cwd"] != r.proj || m["title"] != "fix the <login> bug" {
		t.Fatalf("baton's record: %v", m)
	}
	ix := r.index()
	th := ix.Thread(sid)
	if th == nil || len(th.Places) != 3 || !th.Places[keyB].Auto || th.Places[keyB].Label != "b" || th.Places[keyX].Label != "" {
		t.Fatalf("thread: %+v", th)
	}
	if !ix.made(keyB) || ix.made(keyA) || ix.made(keyX) {
		t.Fatalf("made: %v", ix.Made)
	}
	// The conversation in the profile has its second name where the app finds it.
	if !sameFile(r.d.linkPath(convs[0].Transcript), convs[0].Transcript) {
		t.Fatal("no second name")
	}
	// The dialog shows the setting's ticks as such.
	if row := rowOf(r.view(sid, convs[0].Transcript), keyB); !row.Checked || !row.Auto || row.Off {
		t.Fatalf("row: %+v", row)
	}

	// Again: nothing new. A look leaves it all as it is.
	res, err = r.d.ShowAll(convs, 0, nil)
	must(t, err)
	if res.Added != 0 {
		t.Fatalf("again: %+v", res)
	}
	must(t, r.d.Reconcile())
	if got := r.listed(keyB); !slices.Equal(got, []string{sid, sid2, sid3}) {
		t.Fatalf("after a look: %v", got)
	}

	// Unticked in baton: gone from that list, and the setting leaves it off;
	// ticked again: back, as the user's own tick.
	r.d.AllShown = true
	v := r.view(sid, convs[0].Transcript)
	r.apply(sid, convs[0].Transcript, slices.DeleteFunc(v.Ticked(), func(k string) bool { return k == Kept(keyB) })...)
	if got := r.listed(keyB); slices.Contains(got, sid) {
		t.Fatalf("unticked: %v", got)
	}
	res, err = r.d.ShowAll(convs, 0, nil)
	must(t, err)
	if res.Added != 0 || slices.Contains(r.listed(keyB), sid) || !r.thread(sid).off(keyB) {
		t.Fatalf("listed again after an untick: %+v %v", res, r.listed(keyB))
	}
	if row := rowOf(r.view(sid, convs[0].Transcript), keyB); row.Checked || !row.Off {
		t.Fatalf("unticked row: %+v", row)
	}
	r.apply(sid, convs[0].Transcript, append(r.view(sid, convs[0].Transcript).Ticked(), keyB)...)
	th = r.thread(sid)
	if th.off(keyB) || th.Places[keyB] == nil || th.Places[keyB].Auto || !slices.Contains(r.listed(keyB), sid) {
		t.Fatalf("ticked again: %+v", th)
	}

	// Deleted in the desktop app: it stays deleted.
	rec := r.thread(sid3).Places[keyA].Record
	must(t, os.Remove(filepath.Join(r.placeDir(keyA), rec+".json")))
	must(t, os.WriteFile(filepath.Join(r.placeDir(keyA), "deleted_"+rec), []byte("1900000000000"), 0o600))
	must(t, r.d.Reconcile())
	res, err = r.d.ShowAll(convs, 0, nil)
	must(t, err)
	if res.Added != 0 || slices.Contains(r.listed(keyA), sid3) || r.thread(sid3).Places[keyA].State != stateDeleted {
		t.Fatalf("deleted in the app: %+v %v", res, r.listed(keyA))
	}

	// Turned off: the setting's records go, the user's own tick stays. The
	// running app's list keeps its record until the app is quit.
	r.running = true
	hres, err := r.d.HideAll()
	must(t, err)
	if hres.Removed != 5 || !hres.ActiveRemoved || !hres.AppRunning {
		t.Fatalf("hide: %+v", hres)
	}
	if got := r.listed(keyB); !slices.Equal(got, []string{sid}) {
		t.Fatalf("B after hide: %v", got)
	}
	if got := r.listed(keyX); len(got) != 0 {
		t.Fatalf("X after hide: %v", got)
	}
	ix = r.index()
	if p := ix.Thread(sid).Places[keyA]; p == nil || p.State != stateHiding {
		t.Fatalf("A while the app runs: %+v", p)
	}
	r.running = false
	must(t, r.d.Reconcile())
	ix = r.index()
	if p := ix.Thread(sid).Places[keyA]; p != nil {
		t.Fatalf("A once the app is quit: %+v", p)
	}
	if got := r.listed(keyA); !slices.Equal(got, []string{sid2}) {
		t.Fatalf("A after hide: %v (only the app's own record is left)", got)
	}

	// Asked whether it is still on once the index is locked: off, nothing.
	res, err = r.d.ShowAll(convs, 0, func() bool { return false })
	must(t, err)
	if res.Added != 0 || len(r.listed(keyX)) != 0 {
		t.Fatalf("turned off meanwhile: %+v", res)
	}
}

// A first fill goes a batch at a time, newest conversation first, and never
// leaves a conversation half done.
func TestShowAllBatches(t *testing.T) {
	r, convs := allRig(t)
	res, err := r.d.ShowAll(convs, 2, nil)
	must(t, err)
	if res.Added != 3 || !res.More || res.Conversations != 1 {
		t.Fatalf("first batch: %+v", res)
	}
	for _, key := range []string{keyA, keyB, keyX} {
		if got := r.listed(key); !slices.Contains(got, sid) || slices.Contains(got, sid3) {
			t.Fatalf("%s after the first batch: %v", key[:8], got)
		}
	}
	for i := 0; res.More; i++ {
		if i > 5 {
			t.Fatal("never done")
		}
		res, err = r.d.ShowAll(convs, 2, nil)
		must(t, err)
	}
	for key, want := range map[string][]string{keyA: {sid, sid2, sid3}, keyB: {sid, sid2, sid3}, keyX: {sid, sid2}} {
		if got := r.listed(key); !slices.Equal(got, want) {
			t.Fatalf("list %s: %v, want %v", key[:8], got, want)
		}
	}
}

// A record name that is someone else's is never written over: baton's takes
// another, and a conversation being deleted is left alone.
func TestShowAllNames(t *testing.T) {
	r, convs := allRig(t)
	other := filepath.Join(r.placeDir(keyX), "local_"+sid+".json")
	r.writeJSON(other, map[string]any{"sessionId": "local_" + sid, "cliSessionId": sid4})
	release, err := run.MarkDeleting(filepath.Join(r.root, "baton"), sid3)
	must(t, err)
	defer release()
	res, err := r.d.ShowAll(convs, 0, nil)
	must(t, err)
	if res.Added != 5 || slices.Contains(r.listed(keyA), sid3) {
		t.Fatalf("result: %+v", res)
	}
	if m := r.record(keyX, "local_"+sid); m["cliSessionId"] != sid4 {
		t.Fatalf("someone else's record: %v", m)
	}
	p := r.thread(sid).Places[keyX]
	if p == nil || !strings.HasPrefix(p.Record, "local_") || p.Record == "local_"+sid {
		t.Fatalf("baton's record there: %+v", p)
	}
}

// thread is the index's thread on conversation id now.
func (r *rig) thread(id string) *Thread {
	r.t.Helper()
	ix := r.index()
	return ix.Thread(id)
}

// A dialog opened before the setting listed the conversation never takes
// what the setting listed since for an untick; a tick given in it makes a
// row the setting listed the user's own, which turning the setting off
// leaves.
func TestShowAllStaleDialog(t *testing.T) {
	r, convs := allRig(t)
	clock := int64(1_800_000_000_000)
	r.d.Now = func() time.Time { clock += 1000; return time.UnixMilli(clock) }
	r.d.AllShown = true
	file := convs[0].Transcript
	v := r.view(sid, file)
	if rowOf(v, keyB).Checked || v.Seen == 0 || !slices.Contains(v.Ticked(), Seen(v.Seen)) {
		t.Fatalf("view before: %+v", v)
	}
	_, err := r.d.ShowAll(convs, 0, nil)
	must(t, err)
	r.apply(sid, file, v.Ticked()...)
	th := r.thread(sid)
	if th == nil || len(th.Places) != 3 || len(th.Off) != 0 || !slices.Contains(r.listed(keyB), sid) {
		t.Fatalf("applied as the old dialog showed it: %+v", th)
	}
	r.apply(sid, file, append(v.Ticked(), keyB)...)
	if th = r.thread(sid); th.Places[keyB].Auto || !th.Places[keyA].Auto {
		t.Fatalf("ticked in the old dialog: %+v %+v", th.Places[keyB], th.Places[keyA])
	}
	_, err = r.d.HideAll()
	must(t, err)
	if !slices.Contains(r.listed(keyB), sid) || slices.Contains(r.listed(keyA), sid) || slices.Contains(r.listed(keyX), sid) {
		t.Fatalf("after hide: A %v B %v X %v", r.listed(keyA), r.listed(keyB), r.listed(keyX))
	}
	// The same from the command line: --show on a row the setting listed.
	v = r.view(sid2, convs[1].Transcript)
	r.apply(sid2, convs[1].Transcript, v.Ticked()...) // nothing changes
	if p := r.thread(sid2); p != nil && len(p.Off) != 0 {
		t.Fatalf("a look and apply as it was: %+v", p)
	}
}

// A conversation a session of baton's went on from (/clear, or a copy) is
// the session's earlier part: the setting does not list it on its own, also
// once the user took the session off the lists (the choice went on with it).
func TestShowAllEarlierOfASession(t *testing.T) {
	r, convs := allRig(t)
	r.d.AllShown = true
	const sid6 = "5e550000-0000-4000-8000-000000000006"
	file := convs[0].Transcript
	r.apply(sid, file, keyA)
	r.apply(sid, file) // taken off again: kept off A
	next := r.conversation(r.prof, sid6)
	must(t, r.d.Follow(sid, sid6, next))
	if th := r.thread(sid6); th == nil || !th.off(keyA) || !slices.Contains(th.Prior, sid) {
		t.Fatalf("thread after follow: %+v", th)
	}
	_, err := r.d.ShowAll(append(convs, Conversation{ID: sid6, Transcript: next}), 0, nil)
	must(t, err)
	for _, key := range []string{keyA, keyB, keyX} {
		if slices.Contains(r.listed(key), sid) {
			t.Fatalf("the earlier conversation listed in %s", key[:8])
		}
	}
	if slices.Contains(r.listed(keyA), sid6) || !slices.Contains(r.listed(keyB), sid6) || !slices.Contains(r.listed(keyX), sid6) {
		t.Fatalf("the session: A %v B %v X %v", r.listed(keyA), r.listed(keyB), r.listed(keyX))
	}
}

// What the setting listed and the desktop app took over stays the app's;
// the "(baton)" record that follows the session beside it is the setting's
// too, and goes when it is turned off.
func TestHideAllTakesTheSettingsCopy(t *testing.T) {
	r, convs := allRig(t)
	const sid6 = "5e550000-0000-4000-8000-000000000006"
	_, err := r.d.ShowAll(convs, 0, nil)
	must(t, err)
	rec := r.thread(sid).Places[keyA].Record
	m := r.record(keyA, rec)
	m["completedTurns"] = 1 // the user went on with it in the app
	r.writeJSON(filepath.Join(r.placeDir(keyA), rec+".json"), m)
	next := r.conversation(r.prof, sid6)
	must(t, r.d.Follow(sid, sid6, next))
	th := r.thread(sid6)
	if p := th.Places[keyA]; p == nil || !p.Copy || !p.Auto {
		t.Fatalf("the copy beside the app's record: %+v", p)
	}
	must(t, r.d.Reconcile())
	if !slices.Contains(r.listed(keyA), sid6) {
		t.Fatalf("no copy written: %v", r.listed(keyA))
	}
	_, err = r.d.HideAll()
	must(t, err)
	if got := r.listed(keyA); slices.Contains(got, sid6) || !slices.Contains(got, sid) {
		t.Fatalf("A after hide: %v (the app's record of %s stays, baton's copy goes)", got, sid[:8])
	}
}

// A record another program holds is left for later; the rest go.
func TestHideAllGoesOnPastAHeldRecord(t *testing.T) {
	r, convs := allRig(t)
	_, err := r.d.ShowAll(convs, 0, nil)
	must(t, err)
	rec := filepath.Join(r.placeDir(keyB), r.thread(sid).Places[keyB].Record+".json")
	must(t, os.Remove(rec))
	must(t, os.MkdirAll(filepath.Join(rec, "held"), 0o700)) // cannot be removed as a file
	res, err := r.d.HideAll()
	must(t, err)
	ix := r.index()
	if res.Left != 1 || res.Removed != 6 || !ix.HasAuto() {
		t.Fatalf("hide: %+v", res)
	}
	must(t, os.RemoveAll(rec))
	res, err = r.d.HideAll()
	must(t, err)
	ix = r.index()
	if res.Left != 0 || ix.HasAuto() {
		t.Fatalf("hide again: %+v", res)
	}
}
