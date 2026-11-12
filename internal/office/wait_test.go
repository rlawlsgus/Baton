package office

import (
	"testing"
	"time"
)

// While a subagent works in the background, Claude, its turn over, waits
// for it at its own desk, where the subagent comes to report, and goes to
// the lounge only once the last one has reported and left.
func TestClaudeWaitsAtItsDeskForItsSubagents(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	at := func(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }

	feed(s, t0, k.user(0, "look into it", map[string]any{"promptSource": "typed", "origin": map[string]any{"kind": "human"}}),
		k.asst(1, "tool_use", use("toolu_bg", "Agent", map[string]any{"description": "dig", "prompt": "x", "run_in_background": true})),
		k.user(1.5, result("toolu_bg", "Async agent launched successfully.", false),
			map[string]any{"toolUseResult": map[string]any{"isAsync": true, "status": "async_launched", "agentId": "bg1"}}),
		k.asst(2, "end_turn", text("It is digging; I will tell you what it finds.")))
	if e := lastEvent(t, s); e.Kind != "idle" || e.Room != "seat" {
		t.Fatalf("turn over with a subagent at work: %+v", e)
	}
	s.info.status = "idle"
	for _, sec := range []float64{10, 60} {
		if v := mainView(t, s, at(sec)); v.State != "idle" || v.Room != "seat" {
			t.Fatalf("Claude at %vs, its subagent at work: %+v", sec, v)
		}
	}

	// The subagent is done: it reports at Claude's desk, with Claude there.
	feed(s, at(70), k.user(70, "<task-notification>\n<task-id>bg1</task-id>\n<tool-use-id>toolu_bg</tool-use-id>\n<status>completed</status>\n</task-notification>",
		map[string]any{"origin": map[string]any{"kind": "task-notification"}}))
	s.refresh(at(71), func(string) int { return -1 }, "me")
	if a := actorOf(s.view, "toolu_bg"); a == nil || a.State != "reporting" || a.Room != "seat" {
		t.Fatalf("subagent: %+v", a)
	}
	if v := mainView(t, s, at(71)); v.State != "idle" || v.Room != "seat" {
		t.Fatalf("Claude while it reports: %+v", v)
	}

	// It leaves: now Claude goes for its coffee.
	n := len(s.events)
	s.advance(at(80))
	var idle *Event
	for i := n; i < len(s.events); i++ {
		if e := s.events[i]; e.Kind == "idle" && e.Actor == mainID {
			idle = &s.events[i]
		}
	}
	if idle == nil || idle.Room != "lounge" || find(s.events[n:], "leave", "toolu_bg") == nil {
		t.Fatalf("after the subagent left: %+v", s.events[n:])
	}
	if v := mainView(t, s, at(80)); v.State != "idle" || v.Room != "lounge" {
		t.Fatalf("Claude once its subagent left: %+v", v)
	}
}

// With no subagent around, or one Claude still waits on mid-turn, nothing
// changes: the lounge after the turn, the desk while the turn goes on.
func TestNoSubagentNoWait(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0, k.user(0, "hi", nil), k.asst(1, "end_turn", text("hello")))
	if e := lastEvent(t, s); e.Kind != "idle" || e.Room != "lounge" {
		t.Fatalf("turn over, nobody else here: %+v", e)
	}
	s.info.status = "idle"
	if v := mainView(t, s, t0.Add(10*time.Second)); v.Room != "lounge" {
		t.Fatalf("Claude: %+v", v)
	}
	// A subagent that leaves while Claude is still at work does not send it
	// to the lounge.
	s.info.status = "busy"
	feed(s, t0.Add(11*time.Second), k.user(11, "more", map[string]any{"promptSource": "typed", "origin": map[string]any{"kind": "human"}}),
		k.asst(12, "tool_use", use("toolu_fg", "Agent", map[string]any{"description": "plan", "subagent_type": "Plan", "prompt": "x"})),
		k.user(13, result("toolu_fg", "Here is the plan", false), map[string]any{"toolUseResult": map[string]any{"status": "completed", "agentId": "zz"}}))
	n := len(s.events)
	s.advance(t0.Add(30 * time.Second))
	if find(s.events[n:], "leave", "toolu_fg") == nil {
		t.Fatalf("the foreground agent did not leave: %+v", s.events[n:])
	}
	if e := find(s.events[n:], "idle", mainID); e != nil {
		t.Fatalf("Claude sent off mid-turn: %+v", e)
	}
}

// baton goes on with another account: it stopped the Claude Code the
// subagents ran in, so they go home with it, and the next account's Claude
// does not sit at its desk waiting for them. The shift says it already: no
// "resting" line follows it.
func TestShiftSendsTheSubagentsHome(t *testing.T) {
	for _, states := range [][]string{{"switching", "waiting", "running"}, {"running"}} {
		t.Run(states[0], func(t *testing.T) {
			t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
			k := newKit(t0)
			s := liveSession(t0)
			tr := newTracker(Options{Root: t.TempDir()})
			defer tr.Close()
			info := sessionInfo{id: s.id, live: true, status: "busy", run: &Run{Account: "work", State: "running"}}
			tr.info = map[string]*sessionInfo{s.id: &info}
			tr.update(s, t0)
			feed(s, t0, k.user(0, "look into it", map[string]any{"promptSource": "typed", "origin": map[string]any{"kind": "human"}}),
				k.asst(1, "tool_use", use("toolu_bg", "Agent", map[string]any{"description": "dig", "prompt": "x", "run_in_background": true})),
				k.user(1.5, result("toolu_bg", "Async agent launched successfully.", false),
					map[string]any{"toolUseResult": map[string]any{"isAsync": true, "status": "async_launched", "agentId": "bg1"}}),
				k.asst(2, "end_turn", text("It is digging.")))
			if v := mainView(t, s, t0.Add(10*time.Second)); v.Room != "seat" {
				t.Fatalf("setup: Claude %+v", v)
			}
			n := len(s.events)
			at := t0.Add(20 * time.Second)
			for _, st := range states {
				next := info
				next.run = &Run{Account: "other", State: st}
				if st == "switching" {
					next.run.Account = "work"
				}
				tr.info[s.id] = &next
				tr.update(s, at)
				at = at.Add(5 * time.Second)
			}
			if find(s.events[n:], "leave", "toolu_bg") == nil {
				t.Fatalf("the subagent stayed after the shift: %+v", s.events[n:])
			}
			if e := find(s.events[n:], "idle", mainID); e != nil {
				t.Fatalf("a resting line after the shift: %+v", e)
			}
			s.info.status = "idle"
			s.mainActor().turnEnded = true
			if v := mainView(t, s, at.Add(time.Minute)); v.Room != "lounge" {
				t.Fatalf("the next account's Claude: %+v", v)
			}
		})
	}
}
