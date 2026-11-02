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
