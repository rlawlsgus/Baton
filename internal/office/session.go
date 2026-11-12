package office

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"
)

// How long things are shown.
const (
	arriveFor = 3 * time.Second // a new subagent (or the next account's Claude) comes out of the elevator
	reportFor = 3 * time.Second // a finished subagent reports to Claude, then leaves
	leaveFor  = 8 * time.Second // one who left stays in the picture this long, walking out
	talkFor   = 4 * time.Second // speech counts as talking this long
	doneFor   = 5 * time.Second // a todo list whose every item is done stays on the whiteboard this long
	failFor   = 5 * time.Second // a failed tool shows as an error this long
	// activeFor: a conversation no live Claude Code holds still counts as
	// going on while its transcript was written this recently (Claude Code
	// can run where baton cannot see its registry).
	activeFor = 2 * time.Minute
	// agentStale: a subagent whose transcript has not changed this long has
	// gone, whatever its files say.
	agentStale = 15 * time.Minute
	// oldRows: rows appended now with a timestamp older than when the office
	// began watching, by more than this, are copies of earlier rows.
	oldRows = 2 * time.Minute
)

// Bounds on what one conversation keeps.
const (
	maxEvents  = 500 // events kept for clients that poll for what is new
	seedEvents = 40  // events a client gets when it starts over
	maxAgents  = 48  // subagents followed at once
	showAgents = 12  // subagents drawn; the rest are counted
	maxOpen    = 32  // open tool uses per person
)

const (
	kindClaude = "claude"
	kindAgent  = "agent"
	kindMe     = "me"
	mainID     = "main"
	meID       = "me"
)

// Event is one thing that happened in the office.
type Event struct {
	Seq    int64     `json:"seq"`
	T      time.Time `json:"t"`
	Actor  string    `json:"actor"`
	Kind   string    `json:"kind"` // think, say, tool, ok, fail, deny, prompt, error, compact, idle, hire, report, leave, ask, shift, clear, end
	Room   string    `json:"room,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	Type   string    `json:"type,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Git    bool      `json:"git,omitempty"` // a command that runs git
}

// Say is the newest thing said in the conversation.
type Say struct {
	Actor string    `json:"actor"`
	Text  string    `json:"text"`
	T     time.Time `json:"t"`
}

// actor is one person in the office: Claude, a subagent, or the user.
type actor struct {
	id, kind, typ, label, desc string

	tools     int      // tool uses seen
	open      []string // its open tool uses, oldest first
	room      string   // where it last worked
	thinking  bool
	thinkAt   time.Time
	saidAt    time.Time
	failAt    time.Time
	failRoom  string
	failTool  string
	turnEnded bool
	endedAt   time.Time
	hiredAt   time.Time
	arriveTo  time.Time // arriving until then
	reportAt  time.Time
	leaveAt   time.Time
	lastAt    time.Time // its newest row
	file      string    // a subagent's transcript

	shown      string // the state last shown, and since when
	shownSince time.Time
}

type openTool struct {
	actor, name, room, detail string
	git                       bool
	start                     time.Time
	undo                      *todoState // the whiteboard before this todo call, should it be refused
	key                       int        // a TaskCreate's call, on the task it put on the whiteboard
}

// todoState is what the meeting room's whiteboard shows.
type todoState struct {
	todos    []Todo
	todoTool string    // the tool that wrote the list (the desktop app's plan is one turn's)
	wrapped  bool      // the plan's turn was wrapped up, which checks every step off
	doneAt   time.Time // when every item got done (zero while some are not)
	todoGen  int       // bumped on every change: a refused call undoes only its own
	hidden   bool      // put away: a plan the turn going on has not touched (kept, as a later call may go on with it)
}

// board is the list as the whiteboard has it: the plan with every step
// checked off once its turn is wrapped up (tasks among them are not steps).
func (b todoState) board() []Todo {
	if !b.wrapped {
		return b.todos
	}
	list := append([]Todo(nil), b.todos...)
	for i := range list {
		if list[i].id == 0 {
			list[i].Status = "completed"
		}
	}
	return list
}

type scheduled struct {
	at    time.Time
	actor string
}

// emit modes: events are numbered and kept (live), collected to be sorted
// and numbered at the end of a replay, or dropped (state only).
const (
	modeLive = iota
	modeReplay
	modeQuiet
)

// session is one conversation the office shows: what discovery knows of
// it, and the people in it as its transcripts tell.
type session struct {
	id      string
	created time.Time
	polled  time.Time
	seq     *int64 // the tracker's event counter
	base    int64  // the counter when the session was attached
	lastSeq int64
	events  []Event
	trimmed bool // older events were dropped
	mode    int
	quiet   time.Time // live events from rows older than this are dropped (a subagent found late)
	replay  []Event
	changed bool

	main       *tailer  // the conversation's own transcript (nil until it exists)
	agentDirs  []string // its subagents folders, in every config folder
	dirsAt     time.Time
	globAt     time.Time // the last search of every project folder for subagents
	scanAt     time.Time
	scanned    bool
	burstUntil time.Time // look for subagent files every tick until then
	files      map[string]*agentFile

	actors   map[string]*actor // Claude and subagents, by id
	order    []string          // subagents in the order they came
	me       actor
	tools    map[string]*openTool // open tool uses of everyone, by tool use id
	agentIDs map[string]string    // a subagent's own id -> its actor
	say      *Say
	title    string // the newest name in the transcript
	cwd      string // where the desktop app moved it
	seen     dedupe
	sched    []scheduled
	deskWait bool // Claude's turn ended with it waiting at its desk for its subagents
	lastRow  time.Time
	now      time.Time // when the rows being read were read

	todoState     // the meeting room's whiteboard
	todoKeys  int // TaskCreate calls seen, to tell their tasks apart

	info    sessionInfo
	wasLive bool
	endAt   time.Time // its Claude Code went away (zero while it runs)
	asking  bool
	askAt   time.Time
	runAcct string
	runSt   string

	view   *View
	digest uint64
}

func newSession(id string, now time.Time, seq *int64) *session {
	// A number of its own: a client of an earlier attach of this conversation
	// (the tracker slept, or dropped it) holds an older one, so it is answered
	// with a reset instead of being sent the seeded log again as news.
	*seq++
	s := &session{id: id, created: now, polled: now, seq: seq, base: *seq, lastSeq: *seq,
		files: map[string]*agentFile{}, actors: map[string]*actor{}, tools: map[string]*openTool{}, agentIDs: map[string]string{}}
	s.actors[mainID] = &actor{id: mainID, kind: kindClaude, label: "Claude", room: roomSeat, turnEnded: true}
	s.me = actor{id: meID, kind: kindMe, room: roomDesk}
	return s
}

func (s *session) mainActor() *actor { return s.actors[mainID] }

func (s *session) emit(e Event) {
	e.T = utc(e.T)
	switch s.mode {
	case modeQuiet:
		return
	case modeReplay:
		s.replay = append(s.replay, e)
		return
	}
	if !s.quiet.IsZero() && e.T.Before(s.quiet) {
		return
	}
	*s.seq++
	e.Seq = *s.seq
	s.lastSeq = e.Seq
	s.events = append(s.events, e)
	if len(s.events) > maxEvents+maxEvents/5 {
		s.events = append([]Event(nil), s.events[len(s.events)-maxEvents:]...)
		s.trimmed = true
	}
	s.changed = true
}

// seed ends a replay: the newest events, in time order, become the log a
// client starts from.
func (s *session) seed() {
	sort.SliceStable(s.replay, func(i, j int) bool { return s.replay[i].T.Before(s.replay[j].T) })
	evs := s.replay
	if len(evs) > seedEvents {
		evs = evs[len(evs)-seedEvents:]
	}
	s.replay = nil
	s.mode = modeLive
	for _, e := range evs {
		*s.seq++
		e.Seq = *s.seq
		s.events = append(s.events, e)
	}
	s.lastSeq = *s.seq
	if len(s.events) > 0 {
		s.base = s.events[0].Seq - 1
	}
}

// knows says whether since is where a client of this session's log is: the
// log as attached, or one of its events still kept.
func (s *session) knows(since int64) bool {
	if since <= 0 {
		return false
	}
	if since == s.lastSeq || since == s.base && !s.trimmed {
		return true
	}
	i := sort.Search(len(s.events), func(i int) bool { return s.events[i].Seq >= since })
	return i < len(s.events) && s.events[i].Seq == since
}

// after returns the events a client that has since still needs.
func (s *session) after(since int64) []Event {
	i := sort.Search(len(s.events), func(i int) bool { return s.events[i].Seq > since })
	return append([]Event{}, s.events[i:]...)
}

func (s *session) last(n int) []Event {
	return append([]Event{}, s.events[max(0, len(s.events)-n):]...)
}

// ---- rows -----------------------------------------------------------------------

// row applies one transcript row; who is the person whose transcript it is.
func (s *session) row(line []byte, who *actor, now time.Time) {
	var r row
	if err := json.Unmarshal(line, &r); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return // not JSON; a field of an unexpected type only leaves that field out
		}
	}
	if r.UUID != "" && !s.seen.add(r.UUID) {
		return // written again (Claude Code has been seen appending earlier rows a second time)
	}
	s.now = now
	t := rowTime(&r)
	if t.IsZero() {
		t = now
	}
	if s.mode == modeLive && r.UUID != "" && t.Before(s.created.Add(-oldRows)) && s.quiet.IsZero() {
		return
	}
	if who.kind == kindClaude {
		// The conversation's rows come in time order: subagents due to leave
		// by this row's time leave first. Otherwise a replay (which reaches
		// advance only once every row is read), or a tick that came late,
		// keeps them around for every later turn, and Claude waits at its
		// desk for nobody.
		s.leaveDue(t)
	}
	if t.After(s.lastRow) {
		s.lastRow = t
	}
	if t.After(who.lastAt) {
		who.lastAt = t
	}
	main := who.kind == kindClaude
	switch r.Type {
	case "api-request":
		if main {
			s.think(who, t, "")
		}
	case "custom-title":
		if main {
			s.title = short(r.CustomTitle, 120)
		}
	case "relocated":
		if main && r.RelocatedCwd != "" {
			s.cwd = r.RelocatedCwd
		}
	case "system":
		switch r.Subtype {
		case "compact_boundary":
			who.thinking = false
			s.emit(Event{T: t, Actor: who.id, Kind: "compact", Room: s.restRoom(who)})
		case "stop_hook_summary", "turn_duration":
			s.lastStepDone(who, t)
			s.turnEnd(who, t)
		}
	case "assistant":
		s.assistant(&r, who, t)
	case "user":
		s.user(&r, who, t)
	case "attachment":
		if r.Attachment != nil && r.Attachment.CommandMode == "task-notification" {
			if n, ok := parseNotice(textOf(r.Attachment.Prompt)); ok {
				s.notified(n, t)
			}
		}
	}
}

// restRoom is where someone is when not using a tool: Claude at its own
// seat, a subagent where it last worked.
func (s *session) restRoom(a *actor) string {
	if a.kind == kindAgent && a.room != "" && a.room != roomElevator {
		return a.room
	}
	return roomSeat
}

// shownFrom is when something written at t is shown from: rows reach the disk a
// moment after their timestamp, so a live one counts from when it was read,
// or a short bubble would be over before it is seen.
func (s *session) shownFrom(t time.Time) time.Time {
	if s.mode == modeLive && s.now.After(t) {
		return s.now
	}
	return t
}

func (s *session) think(who *actor, t time.Time, text string) {
	text = short(text, 100)
	if who.thinking && text == "" {
		return
	}
	if !who.thinking {
		who.thinkAt = t
	}
	who.thinking, who.turnEnded = true, false
	s.emit(Event{T: t, Actor: who.id, Kind: "think", Room: s.restRoom(who), Detail: text})
}

func (s *session) assistant(r *row, who *actor, t time.Time) {
	bs, str := blocks(r.Message.Content)
	if r.IsAPIError {
		text := str
		for _, b := range bs {
			if text == "" && b.Type == "text" {
				text = b.Text
			}
		}
		who.thinking = false
		who.failAt, who.failRoom, who.failTool = s.shownFrom(t), s.restRoom(who), ""
		s.emit(Event{T: t, Actor: who.id, Kind: "error", Room: s.restRoom(who), Detail: firstLine(text, 120)})
		return
	}
	// Every row of a message carries the message's stop_reason, and a final
	// reply often starts with a thinking row: only a row with what Claude says
	// or does ends the turn, or it would end before the last words.
	final := false
	for _, b := range bs {
		switch b.Type {
		case "thinking", "redacted_thinking":
			s.think(who, t, b.Thinking)
		case "text":
			final = true
			text := short(b.Text, 160)
			if text == "" || r.Message.Model == "<synthetic>" {
				continue
			}
			who.thinking, who.turnEnded = false, false
			who.saidAt = s.shownFrom(t)
			s.say = &Say{Actor: who.id, Text: text, T: utc(t)}
			s.emit(Event{T: t, Actor: who.id, Kind: "say", Room: s.restRoom(who), Detail: text})
		case "tool_use":
			final = true
			s.toolUse(who, b, t)
		}
	}
	if r.Message.StopReason == "end_turn" && final && len(who.open) == 0 {
		s.lastStepDone(who, t)
		s.turnEnd(who, t)
	}
}

// turnEnd: Claude's turn is over (it goes for a coffee); a subagent's is
// its whole job, so it reports back.
func (s *session) turnEnd(who *actor, t time.Time) {
	who.thinking = false
	if who.kind == kindAgent {
		if len(who.open) == 0 {
			s.finish(who, t)
		}
		return
	}
	if who.turnEnded {
		return
	}
	who.turnEnded, who.endedAt = true, t
	room := s.idleRoom()
	s.deskWait = room == roomSeat
	s.emit(Event{T: t, Actor: who.id, Kind: "idle", Room: room})
}

// agentsAround: a subagent is in the office and not on its way out yet
// (coming in, at work, or reporting back).
func (s *session) agentsAround() bool {
	for _, id := range s.order {
		if a := s.actors[id]; a != nil && a.kind == kindAgent && a.leaveAt.IsZero() {
			return true
		}
	}
	return false
}

// idleRoom is where Claude waits once its turn is over: at its desk while
// a subagent still works, since that is where the subagent comes to
// report, and in the lounge once they are all gone.
func (s *session) idleRoom() string {
	if s.agentsAround() {
		return roomSeat
	}
	return roomLounge
}

// setTodos changes the whiteboard and returns how it was, to undo the change
// should its call be refused.
func (s *session) setTodos(next todoState, t time.Time) *todoState {
	was := s.todoState
	next.doneAt = was.doneAt
	s.todoState = next
	s.doneSince(t)
	s.todoGen = was.todoGen + 1
	was.todoGen = s.todoGen // undone only while this change is the newest
	return &was
}

// doneSince keeps doneAt: when the whiteboard's every item got done, or zero.
func (s *session) doneSince(t time.Time) {
	switch {
	case !allDone(s.board()):
		s.doneAt = time.Time{}
	case s.doneAt.IsZero():
		s.doneAt = s.shownFrom(t)
	}
}

// taskChanged changes (or, on false, drops) the task a TaskCreate call put on
// the whiteboard, also in the undo copies of calls still open, so an undo
// keeps the change.
func (s *session) taskChanged(key int, change func(*Todo) bool, t time.Time) {
	s.todos = withTask(s.todos, key, change)
	for _, o := range s.tools {
		if o.undo != nil {
			o.undo.todos = withTask(o.undo.todos, key, change)
		}
	}
	s.doneSince(t)
}

// numbered and unnumbered split a list into tasks and the rest (a plan's
// steps, TodoWrite's items).
func numbered(list []Todo) (out []Todo) {
	for _, td := range list {
		if td.id != 0 {
			out = append(out, td)
		}
	}
	return out
}

func unnumbered(list []Todo) (out []Todo) {
	for _, td := range list {
		if td.id == 0 {
			out = append(out, td)
		}
	}
	return out
}

// undoTodos puts the whiteboard back as it was before a refused todo call.
func (s *session) undoTodos(was *todoState) {
	if was.todoGen != s.todoGen {
		return // a newer change came since
	}
	st := *was
	st.todoGen = s.todoGen + 1
	s.todoState = st
}

// lastStepDone: a turn that ends normally with only the plan's last step
// left open checks it off, as the desktop app does.
func (s *session) lastStepDone(who *actor, t time.Time) {
	n := len(s.todos)
	if who.kind != kindClaude || s.todoTool != planTool || s.wrapped || n == 0 || s.todos[n-1].Status != "in_progress" {
		return
	}
	if n > 1 && !allDone(s.todos[:n-1]) || len(numbered(s.todos)) > 0 {
		return // steps still open, or tasks among the steps (theirs are not the plan's)
	}
	s.setTodos(todoState{todos: s.todos, todoTool: planTool, wrapped: true, hidden: s.hidden}, t)
}

// boardAt is what the whiteboard shows at now: nothing once every item has
// been done a moment (the list itself is kept, for the Task numbers).
func (s *session) boardAt(now time.Time) []Todo {
	if !s.doneAt.IsZero() && now.Sub(s.doneAt) >= doneFor {
		return []Todo{}
	}
	b := s.board()
	if s.hidden {
		b = numbered(b) // the plan's steps are put away; tasks stay up
	}
	return append([]Todo{}, b...)
}

func (s *session) toolUse(who *actor, b block, t time.Time) {
	if b.ID == "" || s.tools[b.ID] != nil {
		return
	}
	in := parseInput(b.Input)
	room := roomOf(b.Name)
	detail, git := detailOf(b.Name, in)
	var undo *todoState
	key := 0
	switch {
	case b.Name == wrapTool && who.kind == kindClaude:
		// The desktop app checks its plan off when the turn is wrapped up.
		if s.todoTool == planTool && len(s.todos) > 0 {
			undo = s.setTodos(todoState{todos: s.todos, todoTool: planTool, wrapped: true, hidden: s.hidden}, t)
			if !s.hidden {
				detail = doneCount(s.board())
			}
		}
	case b.Name == "TaskCreate" || b.Name == "TaskUpdate":
		// The Task list is the whole conversation's: anyone's calls change it.
		// On the desktop app's plan it adds its tasks; the plan's steps (no
		// numbers) keep it a plan, a wrapped-up one counting as done.
		next := todoState{todoTool: b.Name}
		base := s.todos
		if s.todoTool == planTool {
			base = s.board()
			if s.hidden {
				base = numbered(base) // a plan put away is not brought back by tasks
			}
		}
		next.todos = applyTodos(base, b.Name, in)
		if s.todoTool == planTool && len(numbered(next.todos)) < len(next.todos) {
			next.todoTool = planTool
		}
		if b.Name == "TaskCreate" && len(next.todos) > 0 {
			s.todoKeys++
			key = s.todoKeys
			next.todos[len(next.todos)-1].key = key // a fresh copy: applyTodos made it
		}
		undo = s.setTodos(next, t)
		detail = doneCount(s.todos)
	case isTodoTool(b.Name) && who.kind == kindClaude:
		// TodoWrite lists and the plan are each agent's own (the app refuses
		// subagents the plan).
		undo = s.setTodos(todoState{todos: applyTodos(s.todos, b.Name, in), todoTool: b.Name}, t)
		detail = doneCount(s.todos)
	}
	who.thinking, who.turnEnded = false, false
	who.tools++
	who.room = room
	s.tools[b.ID] = &openTool{actor: who.id, name: b.Name, room: room, detail: detail, git: git, start: t, undo: undo, key: key}
	who.open = append(who.open, b.ID)
	if len(who.open) > maxOpen {
		delete(s.tools, who.open[0])
		who.open = who.open[1:]
	}
	s.emit(Event{T: t, Actor: who.id, Kind: "tool", Room: room, Tool: b.Name, Detail: detail, Git: git})
	if isAgentTool(b.Name) {
		typ := in.SubagentType
		if typ == "" {
			typ = "general-purpose"
		}
		s.hire(b.ID, typ, short(in.Description, 60), t)
	}
	if isAgentTool(b.Name) || b.Name == "Workflow" {
		// Its transcript appears in a moment: look for it every tick.
		if u := t.Add(30 * time.Second); u.After(s.burstUntil) {
			s.burstUntil = u
		}
	}
}

// closeTool ends an open tool use, wherever it is in its owner's list.
func (s *session) closeTool(id string) *openTool {
	ot := s.tools[id]
	if ot == nil {
		return nil
	}
	delete(s.tools, id)
	if a := s.actors[ot.actor]; a != nil {
		for i, o := range a.open {
			if o == id {
				a.open = append(a.open[:i:i], a.open[i+1:]...)
				break
			}
		}
	}
	return ot
}

func (s *session) closeAll(a *actor) {
	for _, id := range a.open {
		delete(s.tools, id)
	}
	a.open = nil
}

func deniedBy(kind string) bool {
	switch kind {
	case "user-rejected", "interrupted", "cancelled":
		return true
	}
	return false
}

func (s *session) user(r *row, who *actor, t time.Time) {
	bs, str := blocks(r.Message.Content)
	results := false
	for _, b := range bs {
		if b.Type == "tool_result" {
			results = true
			s.toolResult(who, r, b, t)
		}
	}
	if results {
		return
	}
	text := str
	for _, b := range bs {
		if text == "" && b.Type == "text" {
			text = b.Text
		}
	}
	if n, ok := parseNotice(text); ok {
		s.notified(n, t)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(text), "<") {
		// The desktop app puts its reminders before what the user typed.
		for _, b := range bs {
			if b.Type == "text" && !strings.HasPrefix(strings.TrimSpace(b.Text), "<") {
				text = b.Text
				break
			}
		}
	}
	// A subagent's prompt is Claude's, not the user's; meta rows and the
	// summary after a compaction are Claude Code's own.
	if who.kind != kindClaude || r.IsMeta || r.IsCompactSummary {
		return
	}
	if r.Origin != nil && r.Origin.Kind != "" && r.Origin.Kind != "human" {
		return
	}
	line := short(text, 200)
	if line == "" || strings.HasPrefix(line, "<") {
		return // a slash command or Claude Code's own text
	}
	if strings.HasPrefix(line, "[Request interrupted") {
		s.closeAll(who)
		s.turnEnd(who, t)
		return
	}
	if who.turnEnded && s.todoTool == planTool && len(s.todos) > 0 && !s.hidden {
		// A new turn starts with a clean whiteboard, as in the desktop app (a
		// message sent while Claude works keeps it). The plan is put away, not
		// thrown away: a later plan call may go on with it.
		next := s.todoState
		next.hidden = true
		s.setTodos(next, t)
	}
	s.me.saidAt = s.shownFrom(t)
	who.turnEnded = false
	s.emit(Event{T: t, Actor: meID, Kind: "prompt", Room: roomDesk, Detail: short(text, 80)})
}

func (s *session) toolResult(who *actor, r *row, b block, t time.Time) {
	text := textOf(b.Content)
	kind := "ok"
	switch {
	case deniedBy(r.ToolDenialKind) || strings.HasPrefix(strings.TrimSpace(text), "[Request interrupted"):
		kind = "deny"
	case b.IsError:
		kind = "fail"
	}
	ot := s.closeTool(b.ToolUseID)
	owner := who
	if ot != nil && s.actors[ot.actor] != nil {
		owner = s.actors[ot.actor]
	}
	if ot == nil && kind == "ok" {
		return // its tool use came before what was read
	}
	ev := Event{T: t, Actor: owner.id, Kind: kind, Room: s.restRoom(owner)}
	if ot != nil {
		ev.Room, ev.Tool = ot.room, ot.name
	}
	if kind != "ok" {
		ev.Detail = firstLine(text, 120)
	}
	if kind == "fail" {
		owner.failAt, owner.failRoom, owner.failTool = s.shownFrom(t), ev.Room, ev.Tool
	}
	s.emit(ev)
	if ot == nil {
		return
	}
	switch {
	case ot.undo != nil && kind != "ok" && ot.undo.todoGen == s.todoGen:
		s.undoTodos(ot.undo) // refused or failed: the whiteboard is as it was
	case ot.key > 0 && kind != "ok":
		// A refused TaskCreate that is no longer the newest change takes back
		// just its task.
		s.taskChanged(ot.key, func(*Todo) bool { return false }, t)
	case ot.key > 0:
		// Claude Code numbers the tasks: a list read from the middle of a
		// transcript, or after deletions, does not count from #1.
		if n := createdTask(r.ToolUseResult, text); n > 0 {
			s.taskChanged(ot.key, func(td *Todo) bool { td.id = n; return true }, t)
		}
	case ot.name == "TaskList" && kind == "ok":
		if tasks, ok := taskList(r.ToolUseResult); ok {
			// The whole Task list, as Claude Code has it; a plan's steps stay.
			next := todoState{todos: tasks, todoTool: ot.name}
			if steps := unnumbered(s.board()); s.todoTool == planTool && !s.hidden && len(steps) > 0 {
				next.todos = trimTodos(append(steps, tasks...))
				next.todoTool = planTool
			}
			s.setTodos(next, t)
		}
	}
	switch {
	case isAgentTool(ot.name):
		agentID, launched := launchedOnly(r.ToolUseResult, text)
		a := s.actors[b.ToolUseID]
		if a == nil {
			return
		}
		if agentID != "" {
			s.agentIDs[agentID] = a.id
		}
		// A background agent's result only says it started; one that ran in
		// the foreground (or could not start) is done.
		if !launched || kind != "ok" {
			s.finish(a, t)
		}
	case (ot.name == "SubagentHandback" || ot.name == "StructuredOutput") && kind == "ok":
		s.finish(owner, t)
	}
}

// notified: a background agent finished and Claude got told.
func (s *session) notified(n notice, t time.Time) {
	if a := s.actors[n.toolUseID]; a != nil {
		s.finish(a, t)
		return
	}
	if a := s.actors[s.agentIDs[n.taskID]]; a != nil {
		s.finish(a, t)
	}
}

// hire brings a new subagent into the office through the elevator.
func (s *session) hire(id, typ, desc string, t time.Time) *actor {
	if a := s.actors[id]; a != nil {
		return a
	}
	if len(s.order) >= maxAgents {
		s.drop(s.order[0])
	}
	a := &actor{id: id, kind: kindAgent, typ: typ, label: typ, desc: desc, room: roomElevator,
		hiredAt: t, arriveTo: s.shownFrom(t).Add(arriveFor), lastAt: s.shownFrom(t)}
	s.actors[id] = a
	s.order = append(s.order, id)
	s.emit(Event{T: t, Actor: id, Kind: "hire", Room: roomElevator, Type: typ, Detail: desc})
	return a
}

// finish: a subagent is done. It reports to Claude, and leaves a moment later.
func (s *session) finish(a *actor, t time.Time) {
	if a.kind != kindAgent || !a.reportAt.IsZero() || !a.leaveAt.IsZero() {
		return
	}
	s.closeAll(a)
	a.thinking = false
	a.reportAt = t
	s.emit(Event{T: t, Actor: a.id, Kind: "report", Room: roomSeat, Type: a.typ})
	s.sched = append(s.sched, scheduled{at: s.shownFrom(t).Add(reportFor), actor: a.id})
	if f := s.files[a.file]; f != nil {
		f.done = true
	}
}

// leave sends a subagent out through the elevator.
func (s *session) leave(a *actor, t time.Time) {
	if a.kind != kindAgent || !a.leaveAt.IsZero() {
		return
	}
	s.closeAll(a)
	a.thinking = false
	a.leaveAt = t
	s.emit(Event{T: t, Actor: a.id, Kind: "leave", Room: roomElevator, Type: a.typ})
	if f := s.files[a.file]; f != nil {
		f.done = true
	}
	// The last one heard back: Claude, its turn over, waited for it at its
	// desk and now goes for its coffee. Only then: one already in the lounge
	// is not sent there again. A leave applied late (a subagent whose own
	// transcript, read after Claude's when attaching, says it finished) is
	// dated no earlier than the wait, so the log ends in the lounge too.
	if m := s.mainActor(); m != nil && m.turnEnded && s.deskWait && !s.agentsAround() {
		s.deskWait = false
		s.emit(Event{T: later(t, m.endedAt), Actor: m.id, Kind: "idle", Room: roomLounge})
	}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// drop forgets a subagent.
func (s *session) drop(id string) {
	a := s.actors[id]
	if a == nil || a.kind != kindAgent {
		return
	}
	s.closeAll(a)
	if f := s.files[a.file]; f != nil {
		f.done = true
	}
	delete(s.actors, id)
	for i, o := range s.order {
		if o == id {
			s.order = append(s.order[:i:i], s.order[i+1:]...)
			break
		}
	}
	for k, v := range s.agentIDs {
		if v == id {
			delete(s.agentIDs, k)
		}
	}
}

// advance moves time on: reported subagents leave, and those who left are
// gone from the picture after a while.
func (s *session) advance(now time.Time) {
	s.leaveDue(now)
	for _, id := range append([]string(nil), s.order...) {
		a := s.actors[id]
		switch {
		case !a.leaveAt.IsZero() && now.Sub(a.leaveAt) > leaveFor:
			s.drop(id)
		case a.leaveAt.IsZero() && a.reportAt.IsZero() && now.Sub(a.lastAt) > agentStale:
			s.leave(a, now)
		}
	}
}

// leaveDue sends off the reported subagents whose time to leave has come
// by t. Nobody is forgotten here: a replay calls it on every row, and a
// subagent dropped then would be hired again when its transcript is found.
func (s *session) leaveDue(t time.Time) {
	if len(s.sched) == 0 {
		return
	}
	sort.SliceStable(s.sched, func(i, j int) bool { return s.sched[i].at.Before(s.sched[j].at) })
	for len(s.sched) > 0 && !s.sched[0].at.After(t) {
		sc := s.sched[0]
		s.sched = s.sched[1:]
		if a := s.actors[sc.actor]; a != nil {
			s.leave(a, sc.at)
		}
	}
}

// ended: the conversation's Claude Code is gone. Its subagents go home and
// nothing it had open goes on.
func (s *session) ended(now time.Time) {
	m := s.mainActor()
	s.closeAll(m)
	m.thinking = false
	s.sendHome(now)
}

// sendHome: the Claude Code its subagents ran in is gone (the session
// ended, or baton stopped it to go on with another account), and they with
// it. Claude no longer waits for them: no "resting" line comes after the
// end or the shift, which say it already.
func (s *session) sendHome(now time.Time) {
	s.deskWait = false
	for _, id := range s.order {
		if a := s.actors[id]; a != nil && a.leaveAt.IsZero() {
			s.leave(a, now)
		}
	}
}

// active says whether the conversation goes on: a live Claude Code holds
// it, or its transcript was just written.
func (s *session) active(now time.Time) bool {
	return s.info.live || now.Sub(s.lastRow) < activeFor
}

// ---- the snapshot ----------------------------------------------------------------

// View is the conversation the office shows, as it is now.
type View struct {
	ID           string      `json:"id"`
	Floor        int         `json:"floor"`
	Title        string      `json:"title"`
	Path         string      `json:"path"`
	Source       string      `json:"source"` // baton | cli | desktop
	Account      string      `json:"account"`
	AccountIndex int         `json:"account_index"` // the account's place in accounts.json, from 0; -1: no account
	RunID        string      `json:"run_id,omitempty"`
	RunState     string      `json:"run_state,omitempty"`
	Window       bool        `json:"window"`
	Live         bool        `json:"live"`
	Status       string      `json:"status"`
	Started      *time.Time  `json:"started,omitempty"`
	Last         *time.Time  `json:"last,omitempty"`
	Actors       []ActorView `json:"actors"`
	MoreAgents   int         `json:"more_agents"`
	Todos        []Todo      `json:"todos"`
	Say          *Say        `json:"say"`
}

// ActorView is one person as shown.
type ActorView struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"` // claude | agent | me
	Type         string     `json:"type"`
	Label        string     `json:"label"`
	Desc         string     `json:"desc,omitempty"`
	Account      string     `json:"account,omitempty"`
	AccountIndex *int       `json:"account_index,omitempty"` // Claude's only
	Room         string     `json:"room"`
	State        string     `json:"state"`
	Tool         string     `json:"tool,omitempty"`
	Detail       string     `json:"detail,omitempty"`
	Git          bool       `json:"git,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
	Tools        int        `json:"tools"`
}

// topTool is the newest tool someone has open.
func (s *session) topTool(a *actor) *openTool {
	for i := len(a.open) - 1; i >= 0; i-- {
		if ot := s.tools[a.open[i]]; ot != nil {
			return ot
		}
	}
	return nil
}

// handover: baton is moving the conversation to another account, so no
// Claude Code runs it for a moment.
func (s *session) handover() bool {
	r := s.info.run
	return r != nil && (r.State == "switching" || r.State == "waiting")
}

// stateOf is where someone is and what they do, now.
func (s *session) stateOf(a *actor, now time.Time) (v ActorView, since time.Time) {
	v = ActorView{ID: a.id, Kind: a.kind, Type: a.typ, Label: a.label, Desc: a.desc, Tools: a.tools}
	set := func(state, room string, t time.Time) {
		v.State, v.Room = state, room
		since = t
	}
	switch {
	case !a.leaveAt.IsZero():
		set("leaving", roomElevator, a.leaveAt)
		return
	case now.Before(a.arriveTo):
		set("arriving", roomElevator, a.hiredAt)
		return
	case !a.reportAt.IsZero():
		set("reporting", roomSeat, a.reportAt)
		return
	}
	if a.kind == kindClaude {
		switch {
		case s.handover():
			set("leaving", roomElevator, time.Time{})
			return
		case !s.active(now) || !s.info.live && !s.endAt.IsZero():
			set("idle", roomLounge, a.endedAt)
			return
		case s.info.live && s.info.status == "waiting":
			set("asking", roomDesk, s.askAt)
			v.Tool = waitTool(s.info.waitingFor)
			if ot := s.topTool(a); ot != nil {
				v.Tool, v.Detail = ot.name, ot.detail
			}
			return
		case s.info.live && s.info.status == "idle" && now.Sub(s.lastRow) > 5*time.Second &&
			now.Sub(a.saidAt) >= talkFor && now.Sub(a.failAt) >= failFor:
			// Claude Code says its turn is over: a tool use whose result was
			// never read (or a cloud never cleared) does not keep Claude busy.
			set("idle", s.idleRoom(), a.endedAt)
			return
		}
	}
	if ot := s.topTool(a); ot != nil {
		v.Tool, v.Detail, v.Git = ot.name, ot.detail, ot.git
		if ot.room == roomDesk {
			set("asking", roomDesk, ot.start)
		} else {
			set("working", ot.room, ot.start)
		}
		return
	}
	switch {
	case now.Sub(a.failAt) < failFor:
		set("error", a.failRoom, a.failAt)
		v.Tool = a.failTool
	case a.thinking:
		set("thinking", s.restRoom(a), a.thinkAt)
	case now.Sub(a.saidAt) < talkFor:
		set("talking", s.restRoom(a), a.saidAt)
	case a.kind == kindClaude && s.info.status == "busy" && !a.turnEnded:
		// Claude Code works, and its rows are written when a block is done.
		set("thinking", roomSeat, time.Time{})
	case a.kind == kindClaude && (a.turnEnded || s.info.status == "idle"):
		set("idle", s.idleRoom(), a.endedAt)
	default:
		set("idle", s.restRoom(a), time.Time{})
	}
	return
}

// refresh builds the snapshot clients get and reports whether it changed
// in a way worth telling a waiting client.
func (s *session) refresh(now time.Time, accountIndex func(string) int, me string) bool {
	info := s.info
	v := &View{ID: s.id, Floor: info.floor, Path: info.cwd, Source: info.source(), Account: info.account,
		Live: info.live, Status: info.status, Todos: s.boardAt(now), Say: s.say, Actors: []ActorView{}}
	v.AccountIndex = accountIndex(v.Account)
	if v.Path == "" {
		v.Path = s.cwd
	}
	switch {
	case info.regName != "":
		v.Title = info.regName
	case s.title != "":
		v.Title = s.title
	case info.title != "":
		v.Title = info.title
	default:
		v.Title = info.firstPrompt
	}
	if r := info.run; r != nil {
		v.RunID, v.RunState, v.Window = r.ID, r.State, r.Window
	}
	if !info.started.IsZero() {
		t := utc(info.started)
		v.Started = &t
	}
	last := info.last
	if s.lastRow.After(last) {
		last = s.lastRow
	}
	if !last.IsZero() {
		t := utc(last)
		v.Last = &t
	}
	show := func(a *actor) ActorView {
		av, since := s.stateOf(a, now)
		if av.State != a.shown {
			a.shown, a.shownSince = av.State, now
		}
		if since.IsZero() {
			since = a.shownSince
		}
		t := utc(since)
		av.Since = &t
		return av
	}
	m := show(s.mainActor())
	m.Account, m.AccountIndex = v.Account, &v.AccountIndex
	if r := info.run; r != nil && r.Account != "" {
		i := accountIndex(r.Account)
		m.Account, m.AccountIndex = r.Account, &i
	}
	v.Actors = append(v.Actors, m)
	n := 0
	for _, id := range s.order {
		if n == showAgents {
			v.MoreAgents++
			continue
		}
		v.Actors = append(v.Actors, show(s.actors[id]))
		n++
	}
	you := ActorView{ID: meID, Kind: kindMe, Label: me, Room: roomDesk, State: "idle"}
	if now.Sub(s.me.saidAt) < talkFor {
		you.State = "talking"
	}
	v.Actors = append(v.Actors, you)
	s.view = v

	h := fnv.New64a()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w(v.ID, strconv.Itoa(v.Floor), v.Title, v.Path, v.Source, v.Account, v.RunState, strconv.FormatBool(v.Window),
		strconv.FormatBool(v.Live), v.Status, strconv.Itoa(v.MoreAgents))
	for _, a := range v.Actors {
		w(a.ID, a.State, a.Room, a.Tool, a.Detail, strconv.FormatBool(a.Git))
	}
	for _, t := range v.Todos {
		w(t.Text, t.Status)
	}
	if v.Say != nil {
		w(v.Say.Actor, v.Say.Text)
	}
	d := h.Sum64()
	changed := d != s.digest || s.changed
	s.digest, s.changed = d, false
	return changed
}

// ---- small things -----------------------------------------------------------------

// utc is how times go out: UTC, to the millisecond.
func utc(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Millisecond)
}

// dedupe remembers the uuids of recent rows, in two generations so the
// memory stays bounded.
type dedupe struct {
	cur, old map[string]struct{}
}

const dedupeGen = 10000

// add reports whether id is new.
func (d *dedupe) add(id string) bool {
	if _, ok := d.cur[id]; ok {
		return false
	}
	if _, ok := d.old[id]; ok {
		return false
	}
	if d.cur == nil || len(d.cur) >= dedupeGen {
		d.old, d.cur = d.cur, make(map[string]struct{}, 256)
	}
	d.cur[id] = struct{}{}
	return true
}

// fileSize is a file's size, read through a handle (a directory listing can
// lag behind a file another process keeps appending to).
func fileSize(path string) (int64, error) {
	f, err := openShared(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
