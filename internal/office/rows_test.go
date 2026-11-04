package office

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Synthetic transcript rows, shaped like Claude Code 2.1.309's.

type rowKit struct {
	t0 time.Time
}

var uuids int // row uuids are unique across files, as Claude Code's are

func newKit(t0 time.Time) *rowKit { return &rowKit{t0: t0} }

// at is the time of the i-th second after t0, as rows write it.
func (k *rowKit) at(sec float64) string {
	return k.t0.Add(time.Duration(sec * float64(time.Second))).UTC().Format("2006-01-02T15:04:05.000Z")
}

func (k *rowKit) uuid() string {
	uuids++
	n := strconv.Itoa(uuids)
	return "00000000-0000-4000-8000-" + strings.Repeat("0", 12-len(n)) + n
}

func jsonl(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

func (k *rowKit) asst(sec float64, stop string, content ...map[string]any) string {
	var st any
	if stop != "" {
		st = stop
	}
	return jsonl(map[string]any{"parentUuid": "p", "isSidechain": false, "type": "assistant", "uuid": k.uuid(), "timestamp": k.at(sec),
		"cwd": `C:\work\proj`, "sessionId": "s", "message": map[string]any{"id": "msg", "model": "claude-opus-5-5", "role": "assistant",
			"content": content, "stop_reason": st}})
}

func (k *rowKit) user(sec float64, content any, extra map[string]any) string {
	r := map[string]any{"parentUuid": "p", "isSidechain": false, "type": "user", "uuid": k.uuid(), "timestamp": k.at(sec),
		"cwd": `C:\work\proj`, "sessionId": "s", "message": map[string]any{"role": "user", "content": content}}
	for key, v := range extra {
		r[key] = v
	}
	return jsonl(r)
}

func (k *rowKit) apiRequest(sec float64) string {
	return jsonl(map[string]any{"type": "api-request", "id": "req", "timestamp": k.at(sec), "querySource": "sdk",
		"params": map[string]any{"model": "claude-opus-5-5"}})
}

func (k *rowKit) system(sec float64, subtype string) string {
	return jsonl(map[string]any{"parentUuid": "p", "isSidechain": false, "type": "system", "subtype": subtype, "uuid": k.uuid(),
		"timestamp": k.at(sec), "level": "suggestion"})
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func thinking(s string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": s, "signature": "sig"}
}
func use(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input, "caller": map[string]any{"type": "direct"}}
}
func result(id string, content any, isErr bool) []map[string]any {
	b := map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}
	if isErr {
		b["is_error"] = true
	}
	return []map[string]any{b}
}

// feed runs rows through a session as its main transcript.
func feed(s *session, now time.Time, rows ...string) {
	for _, r := range rows {
		for _, line := range strings.Split(strings.TrimSpace(r), "\n") {
			if b := []byte(line); wanted(b) {
				s.row(b, s.mainActor(), now)
			}
		}
	}
}

// lastEvent returns the newest event (failing if there is none).
func lastEvent(t *testing.T, s *session) Event {
	t.Helper()
	if len(s.events) == 0 {
		t.Fatal("no events")
	}
	return s.events[len(s.events)-1]
}

func liveSession(now time.Time) *session {
	var seq int64
	s := newSession("11111111-2222-4333-8444-555555555555", now, &seq)
	s.info = sessionInfo{live: true, status: "busy"}
	s.wasLive = true
	return s
}

func mainView(t *testing.T, s *session, now time.Time) ActorView {
	t.Helper()
	s.refresh(now, func(string) int { return -1 }, "me")
	return s.view.Actors[0]
}

// Every kind of row becomes the event the office shows, and Claude goes to
// the room of the work it does.
func TestRowsToEventsAndRooms(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	now := t0 // rows are read as they are written

	feed(s, now, k.user(0, "고쳐 주세요: 테스트가 깨져요", map[string]any{"promptSource": "typed", "origin": map[string]any{"kind": "human"}}))
	if e := lastEvent(t, s); e.Kind != "prompt" || e.Actor != "me" || e.Detail != "고쳐 주세요: 테스트가 깨져요" {
		t.Fatalf("prompt: %+v", e)
	}
	// The desktop app puts a reminder before what the user typed.
	feed(s, now, k.user(0.05, []any{text("<system-reminder>\nToday is Monday.\n</system-reminder>"), text("이것도 봐 주세요")},
		map[string]any{"promptSource": "sdk", "origin": map[string]any{"kind": "human"}}))
	if e := lastEvent(t, s); e.Kind != "prompt" || e.Detail != "이것도 봐 주세요" {
		t.Fatalf("desktop prompt: %+v", e)
	}
	// Claude Code's own rows are not the user's prompts.
	n := len(s.events)
	feed(s, now,
		k.user(0.1, "<command-name>/clear</command-name>", nil),
		k.user(0.1, "caveat", map[string]any{"isMeta": true}),
		k.user(0.1, "This session is being continued", map[string]any{"isCompactSummary": true}),
		k.user(0.1, "from a peer", map[string]any{"origin": map[string]any{"kind": "peer"}}))
	if len(s.events) != n {
		t.Fatalf("not prompts: %+v", s.events[n:])
	}

	feed(s, now, k.apiRequest(1))
	if e := lastEvent(t, s); e.Kind != "think" || e.Actor != "main" || e.Room != "seat" {
		t.Fatalf("think: %+v", e)
	}
	// A thinking block while already thinking is not news, unless it says something.
	n = len(s.events)
	feed(s, now, k.asst(2, "", thinking("")))
	if len(s.events) != n {
		t.Fatalf("empty thinking again: %+v", s.events[n:])
	}
	feed(s, now, k.asst(2.1, "", thinking("Let me look at the parser first, then the tests that use it")))
	if e := lastEvent(t, s); e.Kind != "think" || !strings.HasPrefix(e.Detail, "Let me look") {
		t.Fatalf("thinking text: %+v", e)
	}

	type step struct {
		input        map[string]any
		tool, room   string
		detail       string
		git          bool
		resultIsErr  bool
		resultText   any
		resultKind   string
		resultDetail string
	}
	steps := []step{
		{map[string]any{"file_path": `C:\work\proj\internal\parser.go`, "limit": 40}, "Read", "library", "parser.go", false, false, "     1\tpackage x", "ok", ""},
		{map[string]any{"pattern": "func Parse", "path": `C:\work\proj`, "output_mode": "content"}, "Grep", "library", "func Parse", false, false, "x.go:1", "ok", ""},
		{map[string]any{"pattern": "**/*_test.go"}, "Glob", "library", "**/*_test.go", false, false, "a_test.go", "ok", ""},
		{map[string]any{"file_path": `C:\work\proj\internal\parser.go`, "old_string": "a", "new_string": "b"}, "Edit", "dev", "parser.go", false, false, "updated", "ok", ""},
		{map[string]any{"file_path": `C:\work\proj\NOTES.md`, "content": strings.Repeat("secret ", 100)}, "Write", "dev", "NOTES.md", false, false, "created", "ok", ""},
		{map[string]any{"command": "go test ./...", "description": "Run the tests"}, "Bash", "server", "Run the tests", false, true, "Exit code 1\n--- FAIL: TestParse", "fail", "Exit code 1"},
		{map[string]any{"command": "cd C:\\work\\proj && git status --short"}, "PowerShell", "server", `cd C:\work\proj && git status --short`, true, false, "M x.go", "ok", ""},
		{map[string]any{"url": "https://pkg.go.dev/encoding/json?tab=doc", "prompt": "summarise"}, "WebFetch", "search", "pkg.go.dev", false, false, "ok", "ok", ""},
		{map[string]any{"query": "golang json streaming decoder"}, "WebSearch", "search", "golang json streaming decoder", false, false, "ok", "ok", ""},
		{map[string]any{"owner": "x", "repo": "y"}, "mcp__github__search_code", "search", "github/search_code", false, false, "[]", "ok", ""},
		{map[string]any{"skill": "simplify"}, "Skill", "meeting", "simplify", false, false, "ok", "ok", ""},
		{map[string]any{"query": "select:Read"}, "ToolSearch", "library", "select:Read", false, false, "ok", "ok", ""},
		{map[string]any{"message": "x"}, "SomethingNew", "seat", "", false, false, "ok", "ok", ""},
	}
	sec := 3.0
	for i, st := range steps {
		id := "toolu_" + strconv.Itoa(i)
		feed(s, now, k.asst(sec, "tool_use", use(id, st.tool, st.input)))
		e := lastEvent(t, s)
		if e.Kind != "tool" || e.Tool != st.tool || e.Room != st.room || e.Detail != st.detail || e.Git != st.git || e.Actor != "main" {
			t.Fatalf("%s: %+v", st.tool, e)
		}
		if v := mainView(t, s, k.t0.Add(time.Duration(sec*float64(time.Second))+time.Second)); v.State != "working" || v.Room != st.room || v.Tool != st.tool {
			t.Fatalf("%s state: %+v", st.tool, v)
		}
		feed(s, now, k.user(sec+0.5, result(id, st.resultText, st.resultIsErr), map[string]any{"toolUseResult": map[string]any{"stdout": ""}}))
		e = lastEvent(t, s)
		if e.Kind != st.resultKind || e.Tool != st.tool || e.Detail != st.resultDetail || e.Room != st.room {
			t.Fatalf("%s result: %+v", st.tool, e)
		}
		if st.resultKind == "fail" {
			at := k.t0.Add(time.Duration((sec + 1) * float64(time.Second)))
			if v := mainView(t, s, at); v.State != "error" || v.Room != "server" {
				t.Fatalf("after a failure: %+v", v)
			}
			if v := mainView(t, s, at.Add(10*time.Second)); v.State == "error" {
				t.Fatalf("error kept: %+v", v)
			}
		}
		sec++
	}
	if m := s.mainActor(); m.tools != len(steps) || len(m.open) != 0 || len(s.tools) != 0 {
		t.Fatalf("tools %d open %v", m.tools, m.open)
	}

	// Rejected and interrupted tools are softer than failures.
	feed(s, now, k.asst(sec, "tool_use", use("toolu_rej", "Bash", map[string]any{"command": "rm -rf build"})),
		k.user(sec+0.1, result("toolu_rej", "The user doesn't want to proceed with this tool use.", true), map[string]any{"toolDenialKind": "user-rejected"}))
	if e := lastEvent(t, s); e.Kind != "deny" || e.Tool != "Bash" {
		t.Fatalf("rejected: %+v", e)
	}
	feed(s, now, k.asst(sec+1, "tool_use", use("toolu_int", "Read", map[string]any{"file_path": "a.go"})),
		k.user(sec+1.1, result("toolu_int", []map[string]any{text("[Request interrupted by user for tool use]")}, true), nil))
	if e := lastEvent(t, s); e.Kind != "deny" {
		t.Fatalf("interrupted: %+v", e)
	}

	// Speech, then the end of the turn: one idle, Claude goes for a coffee.
	sec += 2
	feed(s, now, k.asst(sec, "", text("The parser now handles\nnested lists. "+strings.Repeat("All tests pass. ", 20))))
	e := lastEvent(t, s)
	if e.Kind != "say" || strings.Contains(e.Detail, "\n") || len([]rune(e.Detail)) > 160 || s.say == nil || s.say.Text != e.Detail {
		t.Fatalf("say: %+v", e)
	}
	at := k.t0.Add(time.Duration(sec*float64(time.Second)) + time.Second)
	if v := mainView(t, s, at); v.State != "talking" || v.Room != "seat" {
		t.Fatalf("talking: %+v", v)
	}
	feed(s, now, k.asst(sec+0.1, "end_turn", text("Done.")), k.system(sec+0.2, "stop_hook_summary"), k.system(sec+0.3, "turn_duration"))
	idles := 0
	for _, e := range s.events {
		if e.Kind == "idle" {
			idles++
		}
	}
	if idles != 1 {
		t.Fatalf("idle events: %d", idles)
	}
	s.info.status = "idle"
	if v := mainView(t, s, at.Add(10*time.Second)); v.State != "idle" || v.Room != "lounge" {
		t.Fatalf("after the turn: %+v", v)
	}

	// API errors and compaction.
	feed(s, now, jsonl(map[string]any{"parentUuid": "p", "type": "assistant", "uuid": k.uuid(), "timestamp": k.at(sec + 1),
		"isApiErrorMessage": true, "error": "rate_limit", "message": map[string]any{"model": "<synthetic>", "role": "assistant",
			"stop_reason": "stop_sequence", "content": []any{text("API Error: Request rejected (429) · rate limited, try again later")}}}))
	if e := lastEvent(t, s); e.Kind != "error" || !strings.HasPrefix(e.Detail, "API Error: Request rejected (429)") {
		t.Fatalf("api error: %+v", e)
	}
	feed(s, now, jsonl(map[string]any{"parentUuid": "p", "type": "assistant", "uuid": k.uuid(), "timestamp": k.at(sec + 1.5),
		"message": map[string]any{"model": "<synthetic>", "role": "assistant", "content": []any{text("No response requested.")}}}))
	if e := lastEvent(t, s); e.Kind == "say" {
		t.Fatalf("synthetic text said: %+v", e)
	}
	feed(s, now, k.system(sec+2, "compact_boundary"))
	if e := lastEvent(t, s); e.Kind != "compact" {
		t.Fatalf("compact: %+v", e)
	}

	// A row written twice (same uuid) counts once.
	row := k.asst(sec+3, "tool_use", use("toolu_dup", "Read", map[string]any{"file_path": "x.go"}))
	n = len(s.events)
	feed(s, now, row, row)
	if len(s.events) != n+1 {
		t.Fatalf("duplicate row: %+v", s.events[n:])
	}
	// The user interrupts: whatever was open closes, the turn is over.
	feed(s, now, k.user(sec+4, []map[string]any{text("[Request interrupted by user]")}, nil))
	if e := lastEvent(t, s); e.Kind != "idle" || len(s.mainActor().open) != 0 {
		t.Fatalf("interrupt: %+v open %v", e, s.mainActor().open)
	}
	// Seqs only grow.
	for i := 1; i < len(s.events); i++ {
		if s.events[i].Seq <= s.events[i-1].Seq {
			t.Fatalf("seq order at %d", i)
		}
	}
}

// Every row of a final reply carries end_turn, and the reply often starts
// with a thinking row: the turn ends after the last words, not before them.
func TestTurnEndsAfterTheLastWords(t *testing.T) {
	now := time.Now()
	s := liveSession(now)
	k := newKit(now.Add(-2 * time.Second))
	feed(s, now, k.user(0, "hi", nil), k.apiRequest(0.5), k.asst(1, "end_turn", thinking("")), k.asst(1.2, "end_turn", text("hello")))
	var kinds []string
	for _, e := range s.events {
		kinds = append(kinds, e.Kind)
	}
	if got := strings.Join(kinds, " "); got != "prompt think say idle" {
		t.Fatalf("events: %s", got)
	}
}

// A question for the user brings Claude to the user's desk, and so does a
// session waiting for an answer (a permission prompt), whose text is cut
// down to the tool's name.
func TestAskingAtTheDesk(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0, k.asst(1, "tool_use", use("toolu_q", "AskUserQuestion", map[string]any{"questions": []any{
		map[string]any{"question": "Which database should the cache use?", "header": "Cache DB", "multiSelect": false,
			"options": []any{map[string]any{"label": "Redis"}, map[string]any{"label": "SQLite"}}}}})))
	if e := lastEvent(t, s); e.Room != "desk" || e.Detail != "Cache DB" {
		t.Fatalf("question: %+v", e)
	}
	if v := mainView(t, s, t0.Add(2*time.Second)); v.State != "asking" || v.Room != "desk" || v.Tool != "AskUserQuestion" {
		t.Fatalf("asking: %+v", v)
	}
	feed(s, t0, k.user(5, result("toolu_q", `Your questions have been answered: "Which database"="Redis".`, false), nil))
	s.info.status = "waiting"
	s.info.waitingFor = `approve Bash: rm -rf C:\Users\me\secret`
	v := mainView(t, s, t0.Add(6*time.Second))
	if v.State != "asking" || v.Room != "desk" || v.Tool != "Bash" || strings.Contains(v.Tool+v.Detail, "secret") {
		t.Fatalf("permission prompt: %+v", v)
	}
	for in, want := range map[string]string{
		"approve Bash: rm -rf x": "Bash", "approve Edit": "Edit", "approve plan": "ExitPlanMode",
		"input needed": "input needed", "Which database should the cache use?": "", "": "",
	} {
		if got := waitTool(in); got != want {
			t.Errorf("waitTool(%q) = %q, want %q", in, got, want)
		}
	}
}

// The meeting room's whiteboard follows TodoWrite, the desktop app's plan
// tool and the Task tools.
func TestTodos(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0, k.asst(1, "tool_use", use("toolu_t1", "TodoWrite", map[string]any{"todos": []any{
		map[string]any{"content": "Write the parser", "status": "completed", "activeForm": "Writing"},
		map[string]any{"content": "Test it", "status": "in_progress", "activeForm": "Testing"},
		map[string]any{"content": "Ship", "status": "pending", "activeForm": "Shipping"}}})))
	if e := lastEvent(t, s); e.Room != "meeting" || e.Detail != "1/3 done" || len(s.todos) != 3 || s.todos[1].Status != "in_progress" {
		t.Fatalf("TodoWrite: %+v %+v", e, s.todos)
	}
	feed(s, t0, k.asst(2, "tool_use", use("toolu_p1", planTool, map[string]any{"items": []any{
		map[string]any{"content": "Look around", "status": "in_progress"},
		map[string]any{"content": "Fix", "status": "pending"},
		map[string]any{"content": "Verify", "status": "pending"}}, "scope": "first_task"})))
	if e := lastEvent(t, s); e.Room != "meeting" || e.Detail != "0/3 done" || s.todos[0].Text != "Look around" {
		t.Fatalf("plan: %+v %+v", e, s.todos)
	}
	feed(s, t0, k.asst(3, "tool_use", use("toolu_p2", planTool, map[string]any{"advance": true})))
	if s.todos[0].Status != "completed" || s.todos[1].Status != "in_progress" || lastEvent(t, s).Detail != "1/3 done" {
		t.Fatalf("advance: %+v", s.todos)
	}
	feed(s, t0, k.asst(4, "tool_use", use("toolu_p3", planTool, map[string]any{"done": []int{2}, "start": 3})))
	if s.todos[1].Status != "completed" || s.todos[2].Status != "in_progress" {
		t.Fatalf("done/start: %+v", s.todos)
	}
	s.refresh(t0.Add(5*time.Second), func(string) int { return 0 }, "me")
	if len(s.view.Todos) != 3 || s.view.Todos[2].Text != "Verify" {
		t.Fatalf("view todos %+v", s.view.Todos)
	}
	feed(s, t0,
		k.asst(5, "tool_use", use("toolu_c1", "TaskCreate", map[string]any{"subject": "Port the CLI", "description": "x"})),
		k.asst(6, "tool_use", use("toolu_c2", "TaskCreate", map[string]any{"subject": "Port the GUI", "description": "y"})),
		k.asst(7, "tool_use", use("toolu_u1", "TaskUpdate", map[string]any{"taskId": "1", "status": "deleted"})),
		k.asst(8, "tool_use", use("toolu_u2", "TaskUpdate", map[string]any{"taskId": "2", "status": "in_progress"})))
	if n := len(s.todos); n != 4 || s.todos[0].Text != "Look around" || s.todos[3].Text != "Port the GUI" || s.todos[3].Status != "in_progress" {
		t.Fatalf("tasks: %+v", s.todos)
	}
}

// feedAs runs rows through a session as one person's transcript.
func feedAs(s *session, who *actor, now time.Time, rows ...string) {
	for _, r := range rows {
		for _, line := range strings.Split(strings.TrimSpace(r), "\n") {
			if b := []byte(line); wanted(b) {
				s.row(b, who, now)
			}
		}
	}
}

func boardText(s *session, now time.Time) string {
	var out []string
	for _, td := range s.boardAt(now) {
		out = append(out, td.Text+":"+td.Status)
	}
	return strings.Join(out, " ")
}

func plan(steps ...string) map[string]any {
	var items []any
	for i, st := range steps {
		status := "pending"
		if i == 0 {
			status = "in_progress"
		}
		items = append(items, map[string]any{"content": st, "status": status})
	}
	return map[string]any{"items": items, "scope": "continuation"}
}

// The desktop app checks its plan off when the turn is wrapped up, and a
// list whose every item is done leaves the whiteboard a moment later.
func TestWrapUpChecksOffThePlan(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.asst(1, "tool_use", use("toolu_p1", planTool, plan("Look", "Fix", "Verify"))),
		k.user(1.1, result("toolu_p1", "ok", false), nil),
		k.asst(2, "tool_use", use("toolu_p2", planTool, map[string]any{"done": []int{1, 2}, "start": 3})),
		k.user(2.1, result("toolu_p2", "ok", false), nil),
		k.asst(3, "tool_use", use("toolu_w1", wrapTool, map[string]any{"recap": "All done."})))
	if e := lastEvent(t, s); e.Room != roomSeat || e.Detail != "3/3 done" {
		t.Fatalf("wrap_up event %+v", e)
	}
	if got := boardText(s, t0.Add(4*time.Second)); got != "Look:completed Fix:completed Verify:completed" {
		t.Fatalf("wrapped board %q", got)
	}
	if s.todos[2].Status != "in_progress" {
		t.Fatalf("the plan itself changed: %+v", s.todos)
	}
	feed(s, t0, k.user(3.1, result("toolu_w1", "ok", false), nil))
	if got := boardText(s, t0.Add(3*time.Second+doneFor)); got != "" {
		t.Fatalf("finished board still shown: %q", got)
	}
	s.refresh(t0.Add(9*time.Second), func(string) int { return 0 }, "me")
	if len(s.view.Todos) != 0 {
		t.Fatalf("view todos %+v", s.view.Todos)
	}

	// A plan call after a wrap_up in the same turn counts from the plan as it
	// was (the app checks off only while the wrap_up is the newest).
	feed(s, t0, k.asst(10, "tool_use", use("toolu_p3", planTool, map[string]any{"start": 2})))
	if got := boardText(s, t0.Add(11*time.Second)); got != "Look:completed Fix:in_progress Verify:in_progress" {
		t.Fatalf("after a later plan call %q", got)
	}

	// No plan: a wrap_up changes nothing and says nothing of todos.
	s2 := liveSession(t0)
	feed(s2, t0, k.asst(1, "tool_use", use("toolu_w2", wrapTool, map[string]any{"recap": "x"})))
	if e := lastEvent(t, s2); e.Detail != "" || len(s2.todos) != 0 {
		t.Fatalf("wrap_up without a plan %+v %+v", e, s2.todos)
	}
}

// A finished TodoWrite list stays a moment, then the board is wiped.
func TestFinishedListLeavesTheBoard(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0, k.asst(1, "tool_use", use("toolu_t1", "TodoWrite", map[string]any{"todos": []any{
		map[string]any{"content": "A", "status": "completed", "activeForm": "a"},
		map[string]any{"content": "B", "status": "completed", "activeForm": "b"}}})))
	if got := boardText(s, t0.Add(1*time.Second+doneFor-time.Millisecond)); got != "A:completed B:completed" {
		t.Fatalf("finished list not shown for a moment: %q", got)
	}
	if got := boardText(s, t0.Add(1*time.Second+doneFor)); got != "" {
		t.Fatalf("finished list still shown: %q", got)
	}
	// Reopened: shown again.
	feed(s, t0, k.asst(20, "tool_use", use("toolu_t2", "TodoWrite", map[string]any{"todos": []any{
		map[string]any{"content": "A", "status": "completed", "activeForm": "a"},
		map[string]any{"content": "C", "status": "in_progress", "activeForm": "c"}}})))
	if got := boardText(s, t0.Add(60*time.Second)); got != "A:completed C:in_progress" {
		t.Fatalf("open list %q", got)
	}
}

// A new turn starts with a clean whiteboard: the desktop app's plan is put
// away (and comes back if a later plan call goes on with it); a TodoWrite
// list, which Claude Code keeps, stays.
func TestNewTurnPutsThePlanAway(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.user(0, "Fix the parser.", nil),
		k.asst(1, "tool_use", use("toolu_p1", planTool, plan("Look", "Fix"))),
		k.user(1.1, result("toolu_p1", "ok", false), nil),
		k.user(1.5, "Also the tests.", nil)) // sent while Claude works
	if len(s.todos) != 2 {
		t.Fatalf("a message mid-turn wiped the plan: %+v", s.todos)
	}
	feed(s, t0,
		k.asst(2, "end_turn", text("Here is where it stands.")),
		k.system(2.1, "stop_hook_summary"),
		k.user(9, "Thanks, go on.", nil))
	if got := boardText(s, t0.Add(10*time.Second)); got != "" || len(s.todos) != 2 {
		t.Fatalf("the plan is still up (%q), or was thrown away: %+v", got, s.todos)
	}
	feed(s, t0, k.asst(11, "tool_use", use("toolu_p2", planTool, map[string]any{"done": []int{1}, "start": 2})))
	if got := boardText(s, t0.Add(12*time.Second)); got != "Look:completed Fix:in_progress" {
		t.Fatalf("the plan did not come back: %q", got)
	}

	s2 := liveSession(t0)
	feed(s2, t0,
		k.asst(1, "tool_use", use("toolu_t1", "TodoWrite", map[string]any{"todos": []any{
			map[string]any{"content": "A", "status": "in_progress", "activeForm": "a"}}})),
		k.user(1.1, result("toolu_t1", "ok", false), nil),
		k.asst(2, "end_turn", text("Paused.")),
		k.system(2.1, "stop_hook_summary"),
		k.user(9, "Go on.", nil))
	if len(s2.todos) != 1 {
		t.Fatalf("a TodoWrite list was wiped by a new turn: %+v", s2.todos)
	}
}

// A turn that ends normally with only the plan's last step open checks it
// off; an interrupted one does not.
func TestLastStepDoneAtTurnEnd(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	for _, interrupted := range []bool{false, true} {
		s := liveSession(t0)
		feed(s, t0,
			k.asst(1, "tool_use", use("toolu_p1", planTool, plan("Look", "Fix"))),
			k.user(1.1, result("toolu_p1", "ok", false), nil),
			k.asst(2, "tool_use", use("toolu_p2", planTool, map[string]any{"advance": true})),
			k.user(2.1, result("toolu_p2", "ok", false), nil))
		if interrupted {
			feed(s, t0, k.user(3, "[Request interrupted by user]", nil))
		} else {
			feed(s, t0, k.asst(3, "end_turn", text("Fixed.")))
		}
		want := "Look:completed Fix:completed"
		if interrupted {
			want = "Look:completed Fix:in_progress"
		}
		if got := boardText(s, t0.Add(4*time.Second)); got != want {
			t.Errorf("interrupted=%v: board %q, want %q", interrupted, got, want)
		}
	}
}

// A subagent's plan and TodoWrite list are its own (the app refuses
// subagents the plan); the Task list is the whole conversation's.
func TestSubagentTodosStayOffTheBoard(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.asst(1, "tool_use", use("toolu_p1", planTool, plan("Main A", "Main B"))),
		k.user(1.1, result("toolu_p1", "ok", false), nil))
	a := s.hire("toolu_ag", "Explore", "look", t0.Add(2*time.Second))
	feedAs(s, a, t0,
		k.asst(3, "tool_use", use("toolu_ap", planTool, plan("Agent X"))),
		k.user(3.1, result("toolu_ap", "Not shown: you are a subagent", true), map[string]any{"toolDenialKind": "permission-rule"}),
		k.asst(4, "tool_use", use("toolu_at", "TodoWrite", map[string]any{"todos": []any{
			map[string]any{"content": "Agent Y", "status": "in_progress", "activeForm": "y"}}})))
	if got := boardText(s, t0.Add(5*time.Second)); got != "Main A:in_progress Main B:pending" {
		t.Fatalf("a subagent's list reached the board: %q", got)
	}
	feed(s, t0, k.asst(5, "tool_use", use("toolu_p2", planTool, map[string]any{"advance": true})))
	if got := boardText(s, t0.Add(6*time.Second)); got != "Main A:completed Main B:in_progress" {
		t.Fatalf("Claude's advance %q", got)
	}
	feedAs(s, a, t0, k.asst(6, "tool_use", use("toolu_ac", "TaskCreate", map[string]any{"subject": "Shared task"})))
	if n := len(s.todos); n != 3 || s.todos[2].Text != "Shared task" {
		t.Fatalf("a subagent's task is the conversation's: %+v", s.todos)
	}
}

// A refused or failed todo call leaves the whiteboard as it was.
func TestRefusedTodoCallIsUndone(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.asst(1, "tool_use", use("toolu_p1", planTool, plan("A", "B"))),
		k.user(1.1, result("toolu_p1", "ok", false), nil),
		k.asst(2, "tool_use", use("toolu_p2", planTool, map[string]any{"items": []any{map[string]any{"content": "X", "status": "pending"}}})),
		k.user(2.1, result("toolu_p2", "items also needs a scope", true), nil))
	if got := boardText(s, t0.Add(3*time.Second)); got != "A:in_progress B:pending" {
		t.Fatalf("a refused plan stayed: %q", got)
	}
	// A refusal that comes after a newer change does not undo that one.
	feed(s, t0,
		k.asst(3, "tool_use", use("toolu_p3", planTool, map[string]any{"done": []int{9}})),
		k.asst(3.5, "tool_use", use("toolu_p4", planTool, map[string]any{"advance": true})),
		k.user(3.6, result("toolu_p4", "ok", false), nil),
		k.user(3.7, result("toolu_p3", "step 9 is out of range", true), nil))
	if got := boardText(s, t0.Add(4*time.Second)); got != "A:completed B:in_progress" {
		t.Fatalf("an older refusal undid a newer change: %q", got)
	}
}

// Plan steps move the way the desktop app moves them: advance finishes the
// step at hand and starts the next one waiting, from the top if need be;
// steps with no words are dropped and twelve are kept.
func TestPlanMovesLikeTheApp(t *testing.T) {
	list := []Todo{{Text: "A", Status: "pending"}, {Text: "B", Status: "in_progress"}, {Text: "C", Status: "completed"}}
	got := applyTodos(list, planTool, &toolInput{Advance: true})
	if got[0].Status != "in_progress" || got[1].Status != "completed" || got[2].Status != "completed" {
		t.Fatalf("advance wraps round: %+v", got)
	}
	if list[1].Status != "in_progress" {
		t.Fatal("applyTodos changed the list it was given")
	}
	two := []Todo{{Text: "A", Status: "in_progress"}, {Text: "B", Status: "in_progress"}}
	if got := applyTodos(two, planTool, &toolInput{Advance: true}); got[0].Status != "completed" || got[1].Status != "in_progress" {
		t.Fatalf("advance finishes one step: %+v", got)
	}
	in := &toolInput{}
	for i := 0; i < 15; i++ {
		c := strconv.Itoa(i)
		if i == 3 {
			c = " "
		}
		in.Items = append(in.Items, struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		}{c, "pending"})
	}
	if got := applyTodos(nil, planTool, in); len(got) != maxPlanSteps || got[3].Text != "4" {
		t.Fatalf("items: %d %+v", len(got), got)
	}
}

// Tasks carry Claude Code's numbers, from TaskCreate's result (or a
// TaskList), so a list read from the middle of a transcript still matches.
func TestTaskNumbersFromResults(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.asst(1, "tool_use", use("toolu_c5", "TaskCreate", map[string]any{"subject": "Fifth"})),
		k.user(1.1, result("toolu_c5", "Task #5 created successfully: Fifth", false), map[string]any{
			"toolUseResult": map[string]any{"task": map[string]any{"id": "5", "subject": "Fifth"}}}),
		k.asst(2, "tool_use", use("toolu_c6", "TaskCreate", map[string]any{"subject": "Sixth"})),
		k.user(2.1, result("toolu_c6", "Task #6 created successfully: Sixth", false), nil),
		k.asst(3, "tool_use", use("toolu_u1", "TaskUpdate", map[string]any{"taskId": "1", "status": "completed"})),
		k.asst(4, "tool_use", use("toolu_u5", "TaskUpdate", map[string]any{"taskId": "5", "status": "completed"})))
	if got := boardText(s, t0.Add(5*time.Second)); got != "Fifth:completed Sixth:pending" || s.todos[1].id != 6 {
		t.Fatalf("tasks %q %+v", got, s.todos)
	}
	// A TaskCreate the hook refused is dropped.
	feed(s, t0,
		k.asst(5, "tool_use", use("toolu_cx", "TaskCreate", map[string]any{"subject": "Blocked"})),
		k.user(5.1, result("toolu_cx", "blocked by a hook", true), nil))
	if len(s.todos) != 2 {
		t.Fatalf("a refused task stayed: %+v", s.todos)
	}
	// Everything done: wiped a moment later; the next task starts a new list,
	// numbered on.
	feed(s, t0, k.asst(6, "tool_use", use("toolu_u6", "TaskUpdate", map[string]any{"taskId": "#6", "status": "completed"})))
	if got := boardText(s, t0.Add(6*time.Second+doneFor)); got != "" {
		t.Fatalf("finished tasks still shown %q", got)
	}
	feed(s, t0, k.asst(20, "tool_use", use("toolu_c7", "TaskCreate", map[string]any{"subject": "Seventh"})))
	if len(s.todos) != 1 || s.todos[0].id != 7 || boardText(s, t0.Add(21*time.Second)) != "Seventh:pending" {
		t.Fatalf("new list %+v", s.todos)
	}
	// A TaskList result is the whole list.
	feed(s, t0,
		k.asst(21, "tool_use", use("toolu_l1", "TaskList", map[string]any{})),
		k.user(21.1, result("toolu_l1", "#2 Two, #7 Seventh", false), map[string]any{
			"toolUseResult": map[string]any{"tasks": []any{
				map[string]any{"id": "2", "subject": "Two", "status": "in_progress", "blockedBy": []any{}},
				map[string]any{"id": "7", "subject": "Seventh", "status": "pending", "blockedBy": []any{}}}}}),
		k.asst(22, "tool_use", use("toolu_u2", "TaskUpdate", map[string]any{"taskId": "2", "status": "completed"})))
	if got := boardText(s, t0.Add(23*time.Second)); got != "Two:completed Seventh:pending" {
		t.Fatalf("after TaskList %q", got)
	}
}

// Tasks made together are told apart by their calls, not by the numbers
// given here: renumbering, a refusal and an undo each touch their own task.
func TestTasksMadeTogether(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	created := func(id string, n int) string {
		return k.user(1.5, result(id, "Task #"+strconv.Itoa(n)+" created successfully", false), nil)
	}
	creates := func(base float64, ids ...string) (rows []string) {
		for i, id := range ids {
			rows = append(rows, k.asst(base+float64(i)*0.1, "tool_use", use(id, "TaskCreate", map[string]any{"subject": strings.TrimPrefix(id, "toolu_")})))
		}
		return rows
	}
	// Read from the middle: Claude Code's numbers start at #2.
	s := liveSession(t0)
	feed(s, t0, creates(1, "toolu_A", "toolu_B", "toolu_C")...)
	feed(s, t0, created("toolu_A", 2), created("toolu_B", 3), created("toolu_C", 4),
		k.asst(2, "tool_use", use("toolu_u", "TaskUpdate", map[string]any{"taskId": "2", "status": "in_progress"})))
	if got := boardText(s, t0.Add(3*time.Second)); got != "A:in_progress B:pending C:pending" || s.todos[2].id != 4 {
		t.Fatalf("renumbered %q %+v", got, s.todos)
	}
	// All refused: none stays.
	s = liveSession(t0)
	feed(s, t0, creates(1, "toolu_A", "toolu_B", "toolu_C")...)
	for _, id := range []string{"toolu_A", "toolu_B", "toolu_C"} {
		feed(s, t0, k.user(1.5, result(id, "[Request interrupted by user for tool use]", true), nil))
	}
	if len(s.todos) != 0 {
		t.Fatalf("refused tasks stayed: %+v", s.todos)
	}
	// A renumber survives the undo of a later call that failed.
	s = liveSession(t0)
	feed(s, t0, creates(1, "toolu_A")...)
	feed(s, t0,
		k.asst(1.2, "tool_use", use("toolu_u9", "TaskUpdate", map[string]any{"taskId": "9", "status": "completed"})),
		created("toolu_A", 7),
		k.user(1.6, result("toolu_u9", "Task not found", true), nil),
		k.asst(2, "tool_use", use("toolu_u7", "TaskUpdate", map[string]any{"taskId": "7", "status": "in_progress"})))
	if got := boardText(s, t0.Add(3*time.Second)); got != "A:in_progress" {
		t.Fatalf("after the undo %q %+v", got, s.todos)
	}
}

// Tasks put on the desktop app's plan keep it a plan: wrap_up checks off its
// steps (not the tasks), a new turn puts the steps away, and a TaskList
// result replaces the tasks only.
func TestTasksOnThePlan(t *testing.T) {
	t0 := time.Now().Add(-time.Minute).Truncate(time.Second)
	k := newKit(t0)
	s := liveSession(t0)
	feed(s, t0,
		k.asst(1, "tool_use", use("toolu_p1", planTool, plan("Look", "Fix"))),
		k.user(1.1, result("toolu_p1", "ok", false), nil),
		k.asst(2, "tool_use", use("toolu_l1", "TaskList", map[string]any{})),
		k.user(2.1, result("toolu_l1", "No tasks", false), map[string]any{"toolUseResult": map[string]any{"tasks": []any{}}}))
	if got := boardText(s, t0.Add(3*time.Second)); got != "Look:in_progress Fix:pending" {
		t.Fatalf("an empty TaskList wiped the plan: %q", got)
	}
	a := s.hire("toolu_ag", "general-purpose", "help", t0.Add(3*time.Second))
	feedAs(s, a, t0, k.asst(4, "tool_use", use("toolu_t", "TaskCreate", map[string]any{"subject": "Shared"})))
	feed(s, t0,
		k.asst(5, "tool_use", use("toolu_w", wrapTool, map[string]any{"recap": "x"})),
		k.user(5.1, result("toolu_w", "ok", false), nil))
	if got := boardText(s, t0.Add(6*time.Second)); got != "Look:completed Fix:completed Shared:pending" {
		t.Fatalf("wrap_up with a task on the plan: %q", got)
	}
	feed(s, t0,
		k.asst(7, "end_turn", text("Done.")),
		k.system(7.1, "stop_hook_summary"),
		k.user(20, "Next thing.", nil))
	if got := boardText(s, t0.Add(21*time.Second)); got != "Shared:pending" {
		t.Fatalf("new turn: %q", got)
	}
}

// A TaskList result over the cap keeps the open and newest tasks, in order.
func TestLongTaskList(t *testing.T) {
	var tasks []any
	for i := 60; i >= 1; i-- {
		st := "pending"
		if i <= 10 {
			st = "completed"
		}
		tasks = append(tasks, map[string]any{"id": strconv.Itoa(i), "subject": "T" + strconv.Itoa(i), "status": st})
	}
	raw, _ := json.Marshal(map[string]any{"tasks": tasks})
	list, ok := taskList(raw)
	if !ok || len(list) != maxTodos || list[0].id != 1+(60-maxTodos) || list[len(list)-1].id != 60 {
		t.Fatalf("ok=%v %d items, first %+v last %+v", ok, len(list), list[0], list[len(list)-1])
	}
}

// Over the cap, finished items go first, so new tasks are never cut off.
func TestTodosKeepTheNewest(t *testing.T) {
	var list []Todo
	for i := 1; i <= maxTodos; i++ {
		st := "pending"
		if i == 4 {
			st = "completed"
		}
		list = append(list, Todo{Text: strconv.Itoa(i), Status: st, id: i})
	}
	got := applyTodos(list, "TaskCreate", &toolInput{Subject: "new"})
	if len(got) != maxTodos || got[len(got)-1].Text != "new" || got[len(got)-1].id != maxTodos+1 || got[3].Text != "5" {
		t.Fatalf("%d items, last %+v, fourth %+v", len(got), got[len(got)-1], got[3])
	}
}

// Tool details are short and never carry a file's content or a path.
func TestDetailsArePrivate(t *testing.T) {
	long := strings.Repeat("x", 500)
	for _, c := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Read", map[string]any{"file_path": `C:\Users\me\secret\plans\2027.md`}, "2027.md"},
		{"Write", map[string]any{"file_path": "/home/me/notes.txt", "content": long}, "notes.txt"},
		{"Bash", map[string]any{"command": "curl -H 'Authorization: Bearer " + long + "'"}, "curl -H 'Authorization: Bearer " + strings.Repeat("x", 28) + "…"},
		{"Agent", map[string]any{"subagent_type": "Explore", "description": "find tests", "prompt": long}, "Explore · find tests"},
		{"Agent", map[string]any{"description": "look", "prompt": long, "run_in_background": "true"}, "general-purpose · look"},
		{"Workflow", map[string]any{"script": long, "scriptPath": `C:\x\flow.js`}, "flow.js"},
	} {
		got, _ := detailOf(c.tool, parseInput(mustJSON(c.input)))
		if got != c.want || len([]rune(got)) > 160 {
			t.Errorf("%s: %q, want %q", c.tool, got, c.want)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
