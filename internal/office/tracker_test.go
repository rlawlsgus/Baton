package office

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/claude"
	"baton/internal/winproc"
)

// world is a PC with Claude Code's folders in a temp dir: the user's own
// ~/.claude and one account's profile.
type world struct {
	t    *testing.T
	root string
	home string // the user's own config folder
	prof string // the "work" account's profile
	tr   *Tracker
	runs []Run
}

func newWorld(t *testing.T) *world {
	root := t.TempDir()
	w := &world{t: t, root: root, home: filepath.Join(root, "home", ".claude"), prof: filepath.Join(root, "profiles", "work")}
	w.tr = newTracker(Options{Root: root, Me: "jh", Sources: func() Sources {
		return Sources{Dirs: []Dir{{Path: w.prof, Account: "work"}, {Path: w.home}}, Accounts: []string{"other", "work"}, Runs: w.runs}
	}})
	t.Cleanup(w.tr.Close)
	return w
}

// transcript is where Claude Code keeps a conversation of cwd.
func (w *world) transcript(dir, cwd, id string) string {
	p := filepath.Join(dir, "projects", claude.ProjectSlug(cwd), id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// register writes the registry entry of a live claude (this test process).
func (w *world) register(dir, id, cwd, status, waitingFor, name string) string {
	pid := os.Getpid()
	ms := winproc.ProcessCreated(pid).UnixMilli()
	p := filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	b, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id, "cwd": cwd, "startedAt": ms, "kind": "interactive",
		"entrypoint": "cli", "name": name, "status": status, "waitingFor": waitingFor, "statusUpdatedAt": time.Now().UnixMilli()})
	if err := os.WriteFile(p, b, 0o600); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// look runs discovery until the conversation list has been read.
func (w *world) look(now time.Time) {
	w.t.Helper()
	w.tr.discover(now)
	for i := 0; i < 200; i++ {
		w.tr.mu.Lock()
		done := w.tr.scanned && !w.tr.scanning
		w.tr.mu.Unlock()
		if done {
			w.tr.rescan.Store(false)
			w.tr.discover(now)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.t.Fatal("the conversation list was never read")
}

// step reads what was written (without looking around).
func (w *world) step(now time.Time) {
	w.tr.discoveredAt = now
	w.tr.tick(now)
}

func (w *world) poll(session string, since int64) Response {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.tr.Poll(ctx, Query{Session: session, Since: since})
}

func find(evs []Event, kind, actor string) *Event {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == kind && (actor == "" || evs[i].Actor == actor) {
			return &evs[i]
		}
	}
	return nil
}

func actorOf(v *View, id string) *ActorView {
	if v == nil {
		return nil
	}
	for i := range v.Actors {
		if v.Actors[i].ID == id {
			return &v.Actors[i]
		}
	}
	return nil
}

const (
	sid1 = "11111111-2222-4333-8444-555555555555"
	sid2 = "22222222-2222-4333-8444-555555555555"
	sid0 = "00000000-2222-4333-8444-555555555555"
)

// The whole office over one conversation: the building and its floors,
// follow mode, subagents coming and going (and a workflow's), a question
// for the user, /clear, and the end of the session.
func TestTrackerFollowsAConversation(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	k := newKit(now.Add(-30 * time.Second))
	projA, projB := filepath.Join(w.root, "work", "nccx"), filepath.Join(w.root, "work", "old-proj")
	_ = os.MkdirAll(projA, 0o700)
	// An older project, quiet for a day.
	p0 := w.transcript(w.prof, projB, sid0)
	ko := newKit(now.Add(-24 * time.Hour))
	_ = os.WriteFile(p0, []byte(ko.user(0, "old work", map[string]any{"cwd": projB})+ko.asst(1, "end_turn", text("ok"))), 0o600)
	_ = os.Chtimes(p0, now.Add(-24*time.Hour), now.Add(-24*time.Hour))
	// The conversation going on now.
	p1 := w.transcript(w.home, projA, sid1)
	appendTo(t, p1, k.user(0, "테스트 고쳐 줘", map[string]any{"cwd": projA})+k.apiRequest(1)+
		k.asst(2, "tool_use", use("toolu_r1", "Read", map[string]any{"file_path": filepath.Join(projA, "README.md")}))+
		k.user(2.5, result("toolu_r1", "readme", false), nil)+
		k.asst(3, "", text("README를 봤습니다.")))
	reg := w.register(w.home, sid1, projA, "busy", "", "NCCX 티켓 이관")
	w.look(now)

	r := w.poll("", 0)
	v := r.View
	if v == nil || v.ID != sid1 || !r.Reset || !v.Live || v.Status != "busy" || v.Title != "NCCX 티켓 이관" || v.Source != "cli" || v.Path != projA {
		t.Fatalf("view %+v reset %v", v, r.Reset)
	}
	if find(r.Events, "prompt", "me") == nil || find(r.Events, "tool", "main") == nil || find(r.Events, "say", "main") == nil || r.Seq != r.Events[len(r.Events)-1].Seq {
		t.Fatalf("seed events %+v", r.Events)
	}
	if r.Me != "jh" || actorOf(v, "me").Label != "jh" || actorOf(v, "main").State != "thinking" { // busy, and its last words were a while ago
		t.Fatalf("people %+v", v.Actors)
	}
	// Floors: the older project is 1F, and the numbers are kept.
	if r.Building.Total != 2 || r.Building.Floors[0].Name != "nccx" || r.Building.Floors[0].Floor != 2 || !r.Building.Floors[0].Live ||
		r.Building.Floors[1].Floor != 1 || r.Building.Floors[1].Sessions[0].Account != "work" || r.Building.Floors[1].Sessions[0].Source != "baton" || v.Floor != 2 {
		t.Fatalf("building %+v", r.Building)
	}
	if data, err := os.ReadFile(filepath.Join(w.root, "office.json")); err != nil || !strings.Contains(string(data), `"floors"`) {
		t.Fatalf("office.json: %s %v", data, err)
	}
	// Nothing new: no events, no reset.
	since := r.Seq
	if r = w.poll("", since); r.Reset || len(r.Events) != 0 || r.View.ID != sid1 {
		t.Fatalf("again: reset %v events %+v", r.Reset, r.Events)
	}

	// Claude hires an Explore agent; its transcript appears and it works.
	appendTo(t, p1, k.asst(10, "tool_use", use("toolu_ag1", "Agent", map[string]any{"subagent_type": "Explore", "description": "find tests", "prompt": "..."})))
	w.step(now)
	r = w.poll("", since)
	if e := find(r.Events, "hire", "toolu_ag1"); e == nil || e.Type != "Explore" || e.Room != "elevator" || e.Detail != "find tests" {
		t.Fatalf("hire %+v", r.Events)
	}
	if a := actorOf(r.View, "toolu_ag1"); a == nil || a.State != "arriving" || a.Kind != "agent" || a.Label != "Explore" {
		t.Fatalf("new agent %+v", a)
	}
	if m := actorOf(r.View, "main"); m.State != "working" || m.Room != "seat" || m.Tool != "Agent" {
		t.Fatalf("Claude phoning: %+v", m)
	}
	sub := filepath.Join(filepath.Dir(p1), sid1, "subagents")
	_ = os.MkdirAll(sub, 0o700)
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.meta.json"), []byte(`{"agentType":"Explore","description":"find tests","toolUseId":"toolu_ag1","spawnDepth":1}`), 0o600)
	ka := newKit(now.Add(-20 * time.Second))
	agent := filepath.Join(sub, "agent-a1.jsonl")
	appendTo(t, agent, ka.user(0, "find the tests", map[string]any{"isSidechain": true, "agentId": "a1"})+
		ka.asst(1, "tool_use", use("toolu_g1", "Grep", map[string]any{"pattern": "TestParse"})))
	since = r.Seq
	w.step(now.Add(4 * time.Second))
	r = w.poll("", since)
	if e := find(r.Events, "tool", "toolu_ag1"); e == nil || e.Tool != "Grep" || e.Room != "library" {
		t.Fatalf("agent's tool: %+v", r.Events)
	}
	if find(r.Events, "prompt", "") != nil {
		t.Fatalf("a subagent's prompt shown as the user's: %+v", r.Events)
	}
	if a := actorOf(r.View, "toolu_ag1"); a.State != "working" || a.Room != "library" || a.Tools != 1 {
		t.Fatalf("agent at work: %+v", a)
	}
	// It hands its report back, and leaves a moment later.
	appendTo(t, agent, ka.asst(5, "tool_use", use("toolu_h1", "SubagentHandback", map[string]any{"message": "found 3"}))+
		ka.user(5.5, result("toolu_h1", `{"success":true}`, false), nil))
	since = r.Seq
	w.step(now.Add(5 * time.Second))
	r = w.poll("", since)
	if find(r.Events, "report", "toolu_ag1") == nil || actorOf(r.View, "toolu_ag1").State != "reporting" || actorOf(r.View, "toolu_ag1").Room != "seat" {
		t.Fatalf("report: %+v", r.Events)
	}
	w.step(now.Add(9 * time.Second))
	r = w.poll("", r.Seq)
	if find(r.Events, "leave", "toolu_ag1") == nil || actorOf(r.View, "toolu_ag1").State != "leaving" {
		t.Fatalf("leave: %+v", r.Events)
	}
	w.step(now.Add(20 * time.Second))
	if r = w.poll("", r.Seq); actorOf(r.View, "toolu_ag1") != nil {
		t.Fatalf("still here: %+v", r.View.Actors)
	}

	// A foreground agent whose transcript is never seen: its result means it is done.
	appendTo(t, p1, k.asst(12, "tool_use", use("toolu_ag2", "Agent", map[string]any{"description": "plan", "subagent_type": "Plan", "prompt": "x"}))+
		k.user(13, result("toolu_ag2", "Here is the plan", false), map[string]any{"toolUseResult": map[string]any{"status": "completed", "agentId": "zz"}}))
	since = r.Seq
	w.step(now.Add(21 * time.Second))
	r = w.poll("", since)
	if find(r.Events, "hire", "toolu_ag2") == nil || find(r.Events, "report", "toolu_ag2") == nil {
		t.Fatalf("foreground agent: %+v", r.Events)
	}
	// A background one only started: it stays until its notification.
	appendTo(t, p1, k.asst(14, "tool_use", use("toolu_ag3", "Agent", map[string]any{"description": "bg", "prompt": "x"}))+
		k.user(14.5, result("toolu_ag3", "Async agent launched successfully.", false), map[string]any{"toolUseResult": map[string]any{"isAsync": true, "status": "async_launched", "agentId": "bg3"}}))
	w.step(now.Add(22 * time.Second))
	r = w.poll("", r.Seq)
	if find(r.Events, "report", "toolu_ag3") != nil || actorOf(r.View, "toolu_ag3") == nil {
		t.Fatalf("background agent left at once: %+v", r.Events)
	}
	appendTo(t, p1, k.user(30, "<task-notification>\n<task-id>bg3</task-id>\n<tool-use-id>toolu_ag3</tool-use-id>\n<status>completed</status>\n</task-notification>",
		map[string]any{"origin": map[string]any{"kind": "task-notification"}}))
	since = r.Seq
	w.step(now.Add(23 * time.Second))
	if r = w.poll("", since); find(r.Events, "report", "toolu_ag3") == nil || find(r.Events, "prompt", "") != nil {
		t.Fatalf("notification: %+v", r.Events)
	}

	// A workflow's agents come without an Agent call of their own.
	appendTo(t, p1, k.asst(31, "tool_use", use("toolu_wf", "Workflow", map[string]any{"scriptPath": `C:\x\review.js`})))
	wf := filepath.Join(sub, "workflows", "wf_1")
	_ = os.MkdirAll(wf, 0o700)
	_ = os.WriteFile(filepath.Join(wf, "agent-b2.meta.json"), []byte(`{"agentType":"workflow-subagent","description":"review:api","workflowPhase":"Review"}`), 0o600)
	kb := newKit(now.Add(-5 * time.Second))
	appendTo(t, filepath.Join(wf, "agent-b2.jsonl"), kb.user(0, "review", map[string]any{"isSidechain": true})+
		kb.asst(1, "tool_use", use("toolu_b1", "Read", map[string]any{"file_path": "api.go"})))
	since = r.Seq
	w.step(now.Add(24 * time.Second))
	r = w.poll("", since)
	if e := find(r.Events, "hire", "b2"); e == nil || e.Type != "workflow-subagent" || find(r.Events, "tool", "b2") == nil {
		t.Fatalf("workflow agent: %+v", r.Events)
	}
	if m := actorOf(r.View, "main"); m.Room != "meeting" || m.Tool != "Workflow" || m.Detail != "review.js" {
		t.Fatalf("Claude runs a workflow: %+v", m)
	}
	appendTo(t, filepath.Join(wf, "agent-b2.jsonl"), kb.user(2, result("toolu_b1", "x", false), nil)+
		kb.asst(3, "tool_use", use("toolu_so", "StructuredOutput", map[string]any{"ok": true}))+kb.user(4, result("toolu_so", "ok", false), nil))
	since = r.Seq
	w.step(now.Add(25 * time.Second))
	if r = w.poll("", since); find(r.Events, "report", "b2") == nil {
		t.Fatalf("workflow agent done: %+v", r.Events)
	}

	// Claude Code waits for a permission: Claude comes to the user's desk.
	w.register(w.home, sid1, projA, "waiting", `approve Bash: rm -rf C:\secret`, "NCCX 티켓 이관")
	since = r.Seq
	w.tr.discover(now.Add(26 * time.Second))
	w.step(now.Add(26 * time.Second))
	r = w.poll("", since)
	if e := find(r.Events, "ask", "main"); e == nil || e.Detail != "Bash" || e.Room != "desk" {
		t.Fatalf("ask: %+v", r.Events)
	}
	if m := actorOf(r.View, "main"); m.State != "asking" || m.Room != "desk" || strings.Contains(m.Detail+m.Tool, "secret") {
		t.Fatalf("asking: %+v", m)
	}

	// /clear: the same claude now holds a new conversation. A client that
	// pinned the old one is moved to it, and starts over.
	pinnedSince := r.Seq
	p2 := w.transcript(w.home, projA, sid2)
	k2 := newKit(now.Add(27 * time.Second))
	appendTo(t, p2, k2.user(0, "<command-name>/clear</command-name>", nil)+k2.user(1, "새 작업", map[string]any{"cwd": projA}))
	w.register(w.home, sid2, projA, "busy", "", "")
	w.tr.discover(now.Add(28 * time.Second))
	w.step(now.Add(28 * time.Second))
	r = w.poll(sid1, pinnedSince)
	if r.View == nil || r.View.ID != sid2 || !r.Reset || find(r.Events, "clear", "main") == nil {
		t.Fatalf("after /clear: view %+v reset %v events %+v", r.View, r.Reset, r.Events)
	}
	if f := w.poll("", 0); f.View.ID != sid2 {
		t.Fatalf("follow mode after /clear: %s", f.View.ID)
	}
	// The new conversation's rows are news as they come.
	appendTo(t, p2, k2.apiRequest(2))
	since = r.Seq
	w.step(now.Add(29 * time.Second))
	if r = w.poll(sid1, since); r.Reset || find(r.Events, "think", "main") == nil {
		t.Fatalf("new conversation's rows: reset %v %+v", r.Reset, r.Events)
	}

	// Claude Code quits: the session ended.
	_ = os.Remove(reg)
	since = r.Seq
	w.tr.discover(now.Add(30 * time.Second))
	w.step(now.Add(30 * time.Second))
	r = w.poll(sid2, since)
	if find(r.Events, "end", "main") == nil || r.View.Live || actorOf(r.View, "main").Room != "lounge" {
		t.Fatalf("end: %+v %+v", r.Events, r.View)
	}

	// A bad since (from another conversation, or too old) starts over.
	if r = w.poll(sid2, 3); !r.Reset || len(r.Events) == 0 || len(r.Events) > seedEvents {
		t.Fatalf("unknown since: reset %v, %d events", r.Reset, len(r.Events))
	}
	// An id nobody knows shows nothing.
	if r = w.poll("99999999-9999-4999-8999-999999999999", 0); r.View != nil {
		t.Fatalf("unknown session: %+v", r.View)
	}
}

// Follow mode shows the busy conversation, not the newest idle one; a baton
// session wears its account, and an account switch is a shift change.
func TestFollowPicksTheBusyOneAndShifts(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	k := newKit(now.Add(-10 * time.Second))
	proj := filepath.Join(w.root, "p")
	pIdle := w.transcript(w.home, proj, sid2)
	appendTo(t, pIdle, k.user(5, "idle one", map[string]any{"cwd": proj})+k.asst(6, "end_turn", text("done")))
	w.register(w.home, sid2, proj, "idle", "", "")
	pBusy := w.transcript(w.prof, proj, sid1)
	appendTo(t, pBusy, k.user(0, "busy one", map[string]any{"cwd": proj})+k.apiRequest(1))
	_ = os.Chtimes(pBusy, now.Add(-time.Minute), now.Add(-time.Minute))
	w.register(w.prof, sid1, proj, "busy", "", "")
	w.runs = []Run{{ID: "20261030-010101-abcdef", Account: "work", State: "running", SessionID: sid1, TranscriptPath: pBusy, Cwd: proj, Window: true, Started: now}}
	w.look(now)
	r := w.poll("", 0)
	v := r.View
	if v == nil || v.ID != sid1 || v.Source != "baton" || v.Account != "work" || v.AccountIndex != 1 || v.RunID != "20261030-010101-abcdef" ||
		v.RunState != "running" || !v.Window || *actorOf(v, "main").AccountIndex != 1 || actorOf(v, "main").State != "thinking" {
		t.Fatalf("view %+v main %+v", v, actorOf(v, "main"))
	}
	if len(r.Building.Floors) != 1 || len(r.Building.Floors[0].Sessions) != 2 || !r.Building.Floors[0].Busy {
		t.Fatalf("building %+v", r.Building)
	}
	// baton moves it to another account.
	w.runs[0].State = "switching"
	since := r.Seq
	w.tr.discover(now.Add(time.Second))
	w.step(now.Add(time.Second))
	r = w.poll("", since)
	if e := find(r.Events, "shift", "main"); e == nil || e.Detail != "work →" || actorOf(r.View, "main").State != "leaving" {
		t.Fatalf("switching: %+v", r.Events)
	}
	w.runs[0].State, w.runs[0].Account = "running", "other"
	since = r.Seq
	w.tr.discover(now.Add(2 * time.Second))
	w.step(now.Add(2 * time.Second))
	r = w.poll("", since)
	if e := find(r.Events, "shift", "main"); e == nil || e.Detail != "work → other" {
		t.Fatalf("shift: %+v", r.Events)
	}
	if m := actorOf(r.View, "main"); m.State != "arriving" || m.Room != "elevator" || m.Account != "other" || *m.AccountIndex != 0 {
		t.Fatalf("next account's Claude: %+v", m)
	}
}

// A held poll answers when there is news, and promptly when its request
// ends or the tracker closes.
func TestLongPoll(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	k := newKit(now.Add(-5 * time.Second))
	proj := filepath.Join(w.root, "p")
	p := w.transcript(w.home, proj, sid1)
	appendTo(t, p, k.user(0, "hello", map[string]any{"cwd": proj}))
	w.register(w.home, sid1, proj, "busy", "", "")
	w.look(now)
	r := w.poll("", 0)

	got := make(chan Response, 1)
	go func() { got <- w.tr.Poll(context.Background(), Query{Since: r.Seq, Wait: true}) }()
	time.Sleep(300 * time.Millisecond)
	select {
	case x := <-got:
		t.Fatalf("answered without news: %+v", x.Events)
	default:
	}
	appendTo(t, p, k.apiRequest(4))
	w.step(time.Now())
	select {
	case x := <-got:
		if find(x.Events, "think", "main") == nil {
			t.Fatalf("news: %+v", x.Events)
		}
		r = x
	case <-time.After(3 * time.Second):
		t.Fatal("the held poll did not answer")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { got <- w.tr.Poll(ctx, Query{Since: r.Seq, Wait: true}) }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case <-got:
		if d := time.Since(start); d > time.Second {
			t.Fatalf("took %v after the request ended", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled poll was held")
	}

	go func() { got <- w.tr.Poll(context.Background(), Query{Since: r.Seq, Wait: true}) }()
	time.Sleep(100 * time.Millisecond)
	w.tr.Close()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("a poll was held after Close")
	}
	if !ValidSession(sid1) || ValidSession("..\\x") || ValidSession("abc") || ValidSession(strings.Repeat("a", 65)) {
		t.Fatal("ValidSession")
	}
}

// After the tracker slept (nobody looked), a page that comes back with its
// old place in the log gets a reset, not the re-read log again as news.
func TestComingBackAfterASleep(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	k := newKit(now.Add(-30 * time.Second))
	proj := filepath.Join(w.root, "p")
	p := w.transcript(w.home, proj, sid1)
	appendTo(t, p, k.user(0, "hello", map[string]any{"cwd": proj})+k.apiRequest(1)+
		k.asst(2, "tool_use", use("toolu_r1", "Read", map[string]any{"file_path": "a.go"}))+
		k.user(2.5, result("toolu_r1", "x", false), nil)+k.asst(3, "end_turn", text("done")))
	reg := w.register(w.home, sid1, proj, "idle", "", "")
	w.look(now)
	r := w.poll("", 0)
	if !r.Reset || r.View == nil || len(r.Events) == 0 {
		t.Fatalf("first poll: reset %v, %d events", r.Reset, len(r.Events))
	}

	// Nobody looks for a while; meanwhile Claude Code quits.
	w.tr.mu.Lock()
	w.tr.sleeping = true
	w.tr.rest()
	w.tr.mu.Unlock()
	_ = os.Remove(reg)
	got := make(chan Response, 1)
	go func() { got <- w.poll("", r.Seq) }()
	select {
	case x := <-got:
		t.Fatalf("answered before a fresh look around (live %v)", x.View != nil && x.View.Live)
	case <-time.After(200 * time.Millisecond):
	}
	w.tr.discover(time.Now())
	r2 := <-got
	if !r2.Reset || r2.View == nil || r2.View.Live {
		t.Fatalf("after the sleep: reset %v, live %v", r2.Reset, r2.View != nil && r2.View.Live)
	}
	w.step(time.Now())
	if r3 := w.poll("", r2.Seq); find(r3.Events, "end", "main") != nil {
		t.Fatalf("an 'end' told as news after the sleep: %+v", r3.Events)
	}
}

// An old conversation asked for by id (out of the list of recent ones)
// keeps its name, folder and floor while it is watched.
func TestPinnedOldConversationKeepsItsFacts(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	proj := filepath.Join(w.root, "old")
	_ = os.MkdirAll(proj, 0o700)
	k := newKit(now.Add(-48 * time.Hour))
	p := w.transcript(w.home, proj, sid0)
	appendTo(t, p, k.user(0, "old question", map[string]any{"cwd": proj})+k.asst(1, "end_turn", text("old answer")))
	_ = os.Chtimes(p, now.Add(-48*time.Hour), now.Add(-48*time.Hour))
	k1 := newKit(now.Add(-5 * time.Second))
	proj1 := filepath.Join(w.root, "cur")
	appendTo(t, w.transcript(w.home, proj1, sid1), k1.user(0, "now", map[string]any{"cwd": proj1})+k1.apiRequest(1))
	w.register(w.home, sid1, proj1, "busy", "", "")
	w.look(now)
	w.tr.mu.Lock()
	keep := w.tr.recent[:0]
	for _, c := range w.tr.recent {
		if c.SessionID != sid0 {
			keep = append(keep, c)
		}
	}
	w.tr.recent = keep // as if 200 newer conversations pushed it out
	w.tr.mu.Unlock()
	w.tr.discover(now)
	r := w.poll(sid0, 0)
	if r.View == nil || r.View.Title != "old question" || r.View.Floor == 0 {
		t.Fatalf("attached: %+v", r.View)
	}
	w.tr.discover(now.Add(time.Second))
	w.step(now.Add(time.Second))
	r = w.poll(sid0, r.Seq)
	if r.View == nil || r.View.Title != "old question" || r.View.Path != proj || r.View.Floor == 0 {
		t.Fatalf("after a look around: %+v", r.View)
	}
}

// A conversation that has no events yet (its first turn not written) is
// held like any other, not answered with a reset over and over.
func TestLongPollBeforeAnyEvent(t *testing.T) {
	w := newWorld(t)
	proj := filepath.Join(w.root, "p")
	w.register(w.home, sid1, proj, "idle", "", "")
	w.look(time.Now())
	r := w.poll("", 0)
	if r.View == nil || r.View.ID != sid1 {
		t.Fatalf("view: %+v", r.View)
	}
	got := make(chan Response, 1)
	go func() { got <- w.tr.Poll(context.Background(), Query{Since: r.Seq, Wait: true}) }()
	select {
	case x := <-got:
		t.Fatalf("answered at once (seq %d, since %d, reset %v)", x.Seq, r.Seq, x.Reset)
	case <-time.After(500 * time.Millisecond):
	}
}
