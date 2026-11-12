package office

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mainIdles are Claude's idle events, oldest first.
func mainIdles(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == "idle" && e.Actor == mainID {
			out = append(out, e)
		}
	}
	return out
}

// The log a page starts from, read back from the transcript: a subagent
// that reported and left early on keeps Claude at its desk only until it
// left, not for every later turn of the window read.
func TestAttachWaitsOnlyWhileTheSubagentWasThere(t *testing.T) {
	prompt := func(k *rowKit, sec float64, s, proj string) string {
		return k.user(sec, s, map[string]any{"cwd": proj, "promptSource": "typed", "origin": map[string]any{"kind": "human"}})
	}
	hire := func(k *rowKit, proj string) string {
		return prompt(k, 0, "look into it", proj) +
			k.asst(4, "tool_use", use("toolu_fg", "Agent", map[string]any{"description": "dig", "subagent_type": "Explore", "prompt": "x"})) +
			k.user(23, result("toolu_fg", "found it", false), map[string]any{"toolUseResult": map[string]any{"status": "completed", "agentId": "fg1"}})
	}
	attach := func(t *testing.T, rows func(k *rowKit, proj string) string) (Response, time.Time) {
		w := newWorld(t)
		now := time.Now()
		t0 := now.Add(-5 * time.Minute).Truncate(time.Second)
		k := newKit(t0)
		proj := filepath.Join(w.root, "p")
		appendTo(t, w.transcript(w.home, proj, sid1), rows(k, proj))
		w.register(w.home, sid1, proj, "idle", "", "")
		w.look(now)
		r := w.poll("", 0)
		if !r.Reset || r.View == nil || find(r.Events, "leave", "toolu_fg") == nil {
			t.Fatalf("attached: reset %v, events %+v", r.Reset, r.Events)
		}
		return r, t0
	}
	at := func(t0 time.Time, sec float64) time.Time {
		return t0.Add(time.Duration(sec * float64(time.Second))).UTC()
	}

	t.Run("later turns", func(t *testing.T) {
		// The turn ends while the agent reports (it waits at its desk), the
		// agent leaves a moment later, and two more turns go by without one.
		r, t0 := attach(t, func(k *rowKit, proj string) string {
			return hire(k, proj) + k.asst(25, "end_turn", text("It found it.")) +
				prompt(k, 123, "and now?", proj) + k.asst(128, "end_turn", text("Nothing else.")) +
				prompt(k, 243, "thanks", proj) + k.asst(248, "end_turn", text("You're welcome."))
		})
		idles := mainIdles(r.Events)
		want := []struct {
			sec  float64
			room string
		}{{25, "seat"}, {26, "lounge"}, {128, "lounge"}, {248, "lounge"}}
		if len(idles) != len(want) {
			t.Fatalf("Claude's idle events: %+v", idles)
		}
		for i, w := range want {
			if !idles[i].T.Equal(at(t0, w.sec)) || idles[i].Room != w.room {
				t.Fatalf("idle %d: %+v, want %v at +%vs (all: %+v)", i, idles[i], w.room, w.sec, idles)
			}
		}
		if m := actorOf(r.View, mainID); m == nil || m.State != "idle" || m.Room != "lounge" {
			t.Fatalf("Claude: %+v", m)
		}
	})

	t.Run("still at work when it left", func(t *testing.T) {
		// Claude goes on working after the agent reported: the agent leaving
		// does not end Claude's turn, and the turn ends with nobody to wait for.
		r, t0 := attach(t, func(k *rowKit, proj string) string {
			return hire(k, proj) + k.asst(24, "tool_use", use("toolu_r1", "Read", map[string]any{"file_path": "a.go"})) +
				k.user(27, result("toolu_r1", "x", false), nil) + k.asst(30, "end_turn", text("Done."))
		})
		idles := mainIdles(r.Events)
		if len(idles) != 1 || !idles[0].T.Equal(at(t0, 30)) || idles[0].Room != "lounge" {
			t.Fatalf("Claude's idle events: %+v", idles)
		}
	})
}

// A background subagent whose own transcript says it is done (Claude has not
// been told yet) is read after Claude's turn ended waiting for it: the log a
// page starts from ends with Claude in the lounge, as the snapshot has it.
func TestAttachLateFinishEndsTheWait(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	t0 := now.Add(-5 * time.Minute).Truncate(time.Second)
	k := newKit(t0)
	proj := filepath.Join(w.root, "p")
	p := w.transcript(w.home, proj, sid1)
	appendTo(t, p, k.user(0, "look into it", map[string]any{"cwd": proj, "promptSource": "typed", "origin": map[string]any{"kind": "human"}})+
		k.asst(4, "tool_use", use("toolu_bg", "Agent", map[string]any{"description": "dig", "prompt": "x", "run_in_background": true}))+
		k.user(5, result("toolu_bg", "Async agent launched successfully.", false),
			map[string]any{"toolUseResult": map[string]any{"isAsync": true, "status": "async_launched", "agentId": "bg1"}})+
		k.asst(25, "end_turn", text("It is digging.")))
	sub := filepath.Join(filepath.Dir(p), sid1, "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-bg1.meta.json"), []byte(`{"agentType":"general-purpose","description":"dig","toolUseId":"toolu_bg"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(sub, "agent-bg1.jsonl"), k.user(4.5, "dig", map[string]any{"isSidechain": true, "agentId": "bg1"})+
		k.asst(20, "end_turn", text("found it")))
	w.register(w.home, sid1, proj, "idle", "", "")
	w.look(now)
	r := w.poll("", 0)
	if find(r.Events, "report", "toolu_bg") == nil || find(r.Events, "leave", "toolu_bg") == nil {
		t.Fatalf("the subagent's finish was not read: %+v", r.Events)
	}
	idles := mainIdles(r.Events)
	end := t0.Add(25 * time.Second).UTC()
	if len(idles) != 2 || idles[0].Room != "seat" || idles[1].Room != "lounge" || !idles[0].T.Equal(end) || !idles[1].T.Equal(end) {
		t.Fatalf("Claude's idle events: %+v", idles)
	}
	if m := actorOf(r.View, mainID); m == nil || m.State != "idle" || m.Room != "lounge" {
		t.Fatalf("Claude: %+v", m)
	}
}

// Live, a turn end read in the same tick as the moment its subagent's leave
// fell due (the tick came late) sees the subagent gone.
func TestLiveTurnEndAfterALeaveFellDue(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	at := func(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }
	feed(s, at(23), k.user(0, "look into it", map[string]any{"promptSource": "typed", "origin": map[string]any{"kind": "human"}}),
		k.asst(4, "tool_use", use("toolu_fg", "Agent", map[string]any{"description": "dig", "subagent_type": "Explore", "prompt": "x"})),
		k.user(23, result("toolu_fg", "found it", false), map[string]any{"toolUseResult": map[string]any{"status": "completed", "agentId": "fg1"}}))
	// Its leave is due at +26; the next rows are read at +31, before advance.
	n := len(s.events)
	feed(s, at(31), k.asst(30, "end_turn", text("Done.")))
	evs := s.events[n:]
	leave, idle := find(evs, "leave", "toolu_fg"), find(evs, "idle", mainID)
	if leave == nil || idle == nil || idle.Room != "lounge" || leave.Seq > idle.Seq {
		t.Fatalf("events: %+v", evs)
	}
	if idles := mainIdles(evs); len(idles) != 1 {
		t.Fatalf("Claude's idle events: %+v", idles)
	}
	s.advance(at(31))
	if idles := mainIdles(s.events[n:]); len(idles) != 1 {
		t.Fatalf("Claude's idle events after advance: %+v", idles)
	}
}
