package office

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rescan reads the conversation list again (as every 20 s), then looks around.
func (w *world) rescan(now time.Time) {
	w.t.Helper()
	w.tr.mu.Lock()
	w.tr.scanAt = time.Time{}
	w.tr.mu.Unlock()
	w.tr.discover(now) // starts the read
	for i := 0; i < 300; i++ {
		w.tr.mu.Lock()
		busy := w.tr.scanning
		w.tr.mu.Unlock()
		if !busy {
			w.tr.rescan.Store(false)
			w.tr.discover(now)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.t.Fatal("the conversation list was never read again")
}

// conversation writes a finished conversation of cwd, last written at mod.
func (w *world) conversation(dir, cwd, id, prompt string, mod time.Time) string {
	w.t.Helper()
	k := newKit(mod.Add(-5 * time.Second))
	p := w.transcript(dir, cwd, id)
	appendTo(w.t, p, k.user(0, prompt, map[string]any{"cwd": cwd})+k.asst(1, "end_turn", text("ok")))
	if err := os.Chtimes(p, mod, mod); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func floorOf(b Building, path string) *Floor {
	for i := range b.Floors {
		if projectKey(b.Floors[i].Path) == projectKey(path) {
			return &b.Floors[i]
		}
	}
	return nil
}

func numbers(b Building) string {
	var out []string
	for _, f := range b.Floors {
		out = append(out, fmt.Sprintf("%dF=%s", f.Floor, f.Name))
	}
	return strings.Join(out, " ")
}

// Follow mode never picks what the user hid, a held poll hears of it at
// once, a session asked for by id is shown all the same, and a hidden floor
// hides what is new on it too.
func TestFollowSkipsHidden(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	pa, pb := filepath.Join(w.root, "alpha"), filepath.Join(w.root, "beta")
	k := newKit(now.Add(-time.Minute))
	busy := w.transcript(w.prof, pa, sid1)
	appendTo(t, busy, k.user(0, "busy one", map[string]any{"cwd": pa})+k.apiRequest(1))
	_ = os.Chtimes(busy, now.Add(-time.Minute), now.Add(-time.Minute))
	w.register(w.prof, sid1, pa, "busy", "", "")
	w.conversation(w.home, pb, sid2, "idle one", now.Add(-5*time.Second))
	w.register(w.home, sid2, pb, "idle", "", "")
	w.look(now)
	r := w.poll("", 0)
	if r.View == nil || r.View.ID != sid1 {
		t.Fatalf("before hiding: %+v", r.View)
	}

	// The busy one is hidden while a poll is held: it hears at once, and
	// the office moves on to the other.
	got := make(chan Response, 1)
	go func() { got <- w.tr.Poll(context.Background(), Query{Since: r.Seq, Wait: true}) }()
	time.Sleep(200 * time.Millisecond)
	w.hiddenS = []string{sid1}
	start := time.Now()
	w.tr.discover(now.Add(time.Second))
	w.step(now.Add(time.Second))
	select {
	case x := <-got:
		if x.View == nil || x.View.ID != sid2 || time.Since(start) > 2*time.Second {
			t.Fatalf("held poll after hiding: %+v after %v", x.View, time.Since(start))
		}
		r = x
	case <-time.After(5 * time.Second):
		t.Fatal("the held poll did not hear of the hiding")
	}
	fa := floorOf(r.Building, pa)
	if fa == nil || fa.Hidden || fa.Live || fa.Busy || fa.Count != 1 || fa.Shown != 0 || len(fa.Sessions) != 1 || !fa.Sessions[0].Hidden {
		t.Fatalf("alpha with its one session hidden: %+v", fa)
	}
	if fb := floorOf(r.Building, pb); fb == nil || fb.Hidden || !fb.Live || fb.Shown != 1 || fb.Sessions[0].Hidden {
		t.Fatalf("beta: %+v", fb)
	}
	// Asked for by id, it is shown all the same.
	if p := w.poll(sid1, 0); p.View == nil || p.View.ID != sid1 {
		t.Fatalf("pinned hidden: %+v", p.View)
	}

	// Everything hidden: nothing is followed, but the floors are still
	// listed, flagged.
	w.hiddenF = []string{strings.ToUpper(pb)}
	w.tr.discover(now.Add(2 * time.Second))
	r = w.poll("", 0)
	if r.View != nil || len(r.Building.Floors) != 2 {
		t.Fatalf("all hidden: view %+v, %s", r.View, numbers(r.Building))
	}
	if fb := floorOf(r.Building, pb); fb == nil || !fb.Hidden || fb.Live || fb.Shown != 1 {
		t.Fatalf("beta hidden: %+v", fb)
	}

	// A new conversation on the hidden floor is hidden with it.
	w.conversation(w.home, pb, sid0, "new on beta", now.Add(time.Second))
	w.rescan(now.Add(3 * time.Second))
	r = w.poll("", 0)
	if fb := floorOf(r.Building, pb); r.View != nil || fb == nil || fb.Count != 2 {
		t.Fatalf("new on a hidden floor: view %+v, beta %+v", r.View, fb)
	}

	// Shown again: the busy one is followed again.
	w.hiddenS, w.hiddenF = nil, nil
	w.tr.discover(now.Add(4 * time.Second))
	if r = w.poll("", 0); r.View == nil || r.View.ID != sid1 {
		t.Fatalf("shown again: %+v", r.View)
	}
}

// A floor counts all its conversations (it lists the newest 40), and only
// what is not hidden lights it up; the flags wake held polls.
func TestBuildingFlagsAndCounts(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	proj := filepath.Join(w.root, "big")
	id := func(i int) string { return fmt.Sprintf("%08x-2222-4333-8444-555555555555", i) }
	for i := 0; i < 45; i++ {
		w.conversation(w.home, proj, id(i), fmt.Sprintf("question %d", i), now.Add(time.Duration(i-60)*time.Minute))
	}
	w.register(w.home, id(44), proj, "busy", "", "")
	w.look(now)
	r := w.poll("", 0)
	f := floorOf(r.Building, proj)
	if f == nil || f.Count != 45 || len(f.Sessions) != floorSessions || f.Shown != 45 || !f.Live || !f.Busy || f.Hidden {
		t.Fatalf("floor %+v", f)
	}
	if b, _ := json.Marshal(f); !strings.Contains(string(b), `"count":45`) || strings.Contains(string(b), `"hidden"`) {
		t.Fatalf("json %s", b)
	}

	w.tr.mu.Lock()
	d0 := w.tr.bDigest
	w.tr.dirty = false
	w.tr.mu.Unlock()
	w.hiddenS = []string{id(44)}
	w.tr.discover(now.Add(time.Second))
	r = w.poll(id(44), 0)
	f = floorOf(r.Building, proj)
	if f == nil || f.Count != 45 || f.Shown != 44 || f.Live || f.Busy || !f.Sessions[0].Hidden || f.Sessions[1].Hidden {
		t.Fatalf("the live one hidden: %+v", f)
	}
	w.tr.mu.Lock()
	d1, dirty := w.tr.bDigest, w.tr.dirty
	w.tr.mu.Unlock()
	if d1 == d0 || !dirty {
		t.Fatal("hiding a session did not change the building's digest")
	}
	w.hiddenF = []string{proj}
	w.tr.discover(now.Add(2 * time.Second))
	w.tr.mu.Lock()
	d2 := w.tr.bDigest
	w.tr.mu.Unlock()
	if f = floorOf(w.poll("", 0).Building, proj); f == nil || !f.Hidden || d2 == d1 {
		t.Fatalf("floor hidden: %+v", f)
	}
}

// A floor whose project has no conversations left anywhere goes to the next
// new project; one watched, one still on disk (only too old for the list),
// or one in a config folder out of sight keeps its number.
func TestVacantFloorGoesToTheNextProject(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	dir := func(n string) string { return filepath.Join(w.root, n) }
	ids := map[string]string{"alpha": "a0000000-2222-4333-8444-555555555555", "beta": "b0000000-2222-4333-8444-555555555555",
		"gamma": "c0000000-2222-4333-8444-555555555555", "delta": "d0000000-2222-4333-8444-555555555555",
		"eps": "e0000000-2222-4333-8444-555555555555", "zeta": "f0000000-2222-4333-8444-555555555555",
		"eta": "f1000000-2222-4333-8444-555555555555", "theta": "f2000000-2222-4333-8444-555555555555"}
	paths := map[string]string{}
	add := func(n string, mod time.Time) { paths[n] = w.conversation(w.home, dir(n), ids[n], n+" question", mod) }
	add("alpha", now.Add(-3*time.Hour))
	add("beta", now.Add(-2*time.Hour))
	add("gamma", now.Add(-time.Hour))
	w.look(now)
	r := w.poll("", 0)
	if got := numbers(r.Building); got != "3F=gamma 2F=beta 1F=alpha" {
		t.Fatalf("first numbering: %s", got)
	}

	// beta's only conversation is deleted and a new project comes: it moves
	// into 2F; nothing else moves.
	_ = os.Remove(paths["beta"])
	add("delta", now.Add(-time.Minute))
	w.rescan(now.Add(time.Second))
	if got := numbers(w.poll("", 0).Building); got != "3F=gamma 2F=delta 1F=alpha" {
		t.Fatalf("after beta went: %s", got)
	}

	// gamma is watched (pinned) when its conversation is deleted: it keeps
	// 3F while it is, so the next project goes on top.
	if p := w.poll(ids["gamma"], 0); p.View == nil || p.View.Floor != 3 {
		t.Fatalf("watching gamma: %+v", p.View)
	}
	_ = os.Remove(paths["gamma"])
	add("eps", now.Add(-50*time.Second))
	w.rescan(now.Add(2 * time.Second))
	if got := numbers(w.poll(ids["gamma"], 0).Building); got != "4F=eps 2F=delta 1F=alpha" || w.tr.floors.of(projectKey(dir("gamma"))) != 3 {
		t.Fatalf("while gamma is watched: %s", got)
	}

	// A config folder out of sight: no number is given away.
	w.tr.mu.Lock()
	clear(w.tr.sessions) // nobody watches gamma any more
	w.tr.mu.Unlock()
	w.partial = true
	add("zeta", now.Add(-40*time.Second))
	w.rescan(now.Add(3 * time.Second))
	if got := numbers(w.poll("", 0).Building); got != "5F=zeta 4F=eps 2F=delta 1F=alpha" {
		t.Fatalf("partial: %s", got)
	}

	// alpha only aged out of the list of newest conversations (it is still
	// on disk): it keeps 1F. gamma is vacant now, so eta gets 3F.
	w.partial = false
	w.tr.mu.Lock()
	keep := w.tr.recent[:0]
	for _, c := range w.tr.recent {
		if c.SessionID != ids["alpha"] {
			keep = append(keep, c)
		}
	}
	w.tr.recent = keep
	w.tr.mu.Unlock()
	w.register(w.prof, ids["eta"], dir("eta"), "busy", "", "")
	w.tr.discover(now.Add(4 * time.Second))
	b := w.poll("", 0).Building
	if got := numbers(b); got != "5F=zeta 4F=eps 3F=eta 2F=delta" || w.tr.floors.of(projectKey(dir("alpha"))) != 1 {
		t.Fatalf("aged out: %s (alpha %d)", got, w.tr.floors.of(projectKey(dir("alpha"))))
	}
	// And with no number vacant, the next one goes on top.
	add("theta", now.Add(-30*time.Second))
	w.rescan(now.Add(5 * time.Second))
	if f := floorOf(w.poll("", 0).Building, dir("theta")); f == nil || f.Floor != 6 {
		t.Fatalf("theta: %+v", f)
	}
}

// A number whose project left the building and has no conversations left
// goes to the next new project; nothing in the building ever moves.
func TestFloorsReuseAVacantNumber(t *testing.T) {
	f := loadFloors(filepath.Join(t.TempDir(), "office.json"))
	f.m = map[string]int{"pa": 1, "pb": 2, "pc": 3, "pe": 5}
	now := time.Now()
	asked := map[string]int{}
	vacant := func(k string) bool { asked[k]++; return k == "pb" || k == "pc" }
	// pa is in the building, so never asked about; pb and pc are gone and
	// vacant, pe is gone but not vacant.
	if !f.number(map[string]time.Time{"pa": now, "pd": now.Add(-time.Hour), "pf": now}, vacant) {
		t.Fatal("nothing numbered")
	}
	if f.of("pa") != 1 || f.of("pd") != 2 || f.of("pf") != 3 || f.of("pb") != 0 || f.of("pc") != 0 || f.of("pe") != 5 {
		t.Fatalf("reused %v", f.m)
	}
	if asked["pa"] != 0 || asked["pb"] != 1 || asked["pc"] != 1 || asked["pe"] != 0 {
		t.Fatalf("asked %v", asked)
	}
	// No vacant number left: on top, each candidate asked about once.
	if !f.number(map[string]time.Time{"pg": now}, vacant) || f.of("pg") != 6 || asked["pe"] != 1 {
		t.Fatalf("on top %v, asked %v", f.m, asked)
	}
	// Without vacant, always on top, as before.
	g := loadFloors(filepath.Join(t.TempDir(), "office.json"))
	g.m = map[string]int{"pa": 1, "pb": 2}
	if !g.number(map[string]time.Time{"pa": now, "pz": now}, nil) || g.of("pz") != 3 || g.of("pb") != 2 {
		t.Fatalf("nil vacant %v", g.m)
	}
}

// A client still showing a conversation hears at once that none is shown
// any more, though that happened between two of its polls (everything was
// hidden); a client showing none is held until there is news.
func TestPollHearsTheViewGoneAtOnce(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	pa := filepath.Join(w.root, "alpha")
	w.conversation(w.home, pa, sid1, "the only one", now.Add(-5*time.Second))
	w.look(now)
	r := w.poll("", 0)
	if r.View == nil || r.View.ID != sid1 || r.Seq == 0 {
		t.Fatalf("before hiding: %+v seq %d", r.View, r.Seq)
	}
	w.hiddenF = []string{pa}
	w.tr.discover(now.Add(time.Second))

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	x := w.tr.Poll(ctx, Query{Since: r.Seq, Wait: true})
	if x.View != nil || x.Seq != 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("held poll of a view gone: %+v seq %d after %v", x.View, x.Seq, time.Since(start))
	}

	got := make(chan Response, 1)
	go func() { got <- w.tr.Poll(ctx, Query{Since: x.Seq, Wait: true}) }()
	time.Sleep(300 * time.Millisecond)
	select {
	case y := <-got:
		t.Fatalf("answered without news: %+v", y.View)
	default:
	}
	w.hiddenF = nil
	w.tr.discover(now.Add(2 * time.Second))
	w.step(now.Add(2 * time.Second))
	select {
	case y := <-got:
		if y.View == nil || y.View.ID != sid1 {
			t.Fatalf("shown again: %+v", y.View)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("the held poll did not hear of the showing")
	}
}
