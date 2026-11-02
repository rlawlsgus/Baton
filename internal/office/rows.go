package office

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// row is the part of a transcript row the office reads. Tool inputs and
// results stay raw: they are looked into only for the few fields shown.
type row struct {
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	UUID             string `json:"uuid"`
	Timestamp        string `json:"timestamp"`
	IsMeta           bool   `json:"isMeta"`
	IsAPIError       bool   `json:"isApiErrorMessage"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	ToolDenialKind   string `json:"toolDenialKind"`
	CustomTitle      string `json:"customTitle"`
	RelocatedCwd     string `json:"relocatedCwd"`
	Origin           *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		Model      string          `json:"model"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Attachment    *struct {
		CommandMode string          `json:"commandMode"`
		Prompt      json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

// block is one part of a message's content.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// blocks reads a message's content: a list of blocks, or a plain string
// (returned as text).
func blocks(raw json.RawMessage) ([]block, string) {
	raw = trimJSON(raw)
	if len(raw) == 0 {
		return nil, ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return nil, s
	}
	var bs []block
	_ = json.Unmarshal(raw, &bs)
	return bs, ""
}

func trimJSON(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

// textOf is the text of a tool result's content or a prompt: a string, or
// its text blocks.
func textOf(raw json.RawMessage) string {
	bs, s := blocks(raw)
	if s != "" {
		return s
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func rowTime(r *row) time.Time {
	if r.Timestamp == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// toolInput holds the fields of a tool's input the office shows. Fields a
// tool does not have stay empty; a field of an unexpected type is skipped.
type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Query        string `json:"query"`
	SubagentType string `json:"subagent_type"`
	Skill        string `json:"skill"`
	To           string `json:"to"`
	ScriptPath   string `json:"scriptPath"`
	Subject      string `json:"subject"`
	Status       string `json:"status"`
	TaskID       any    `json:"taskId"`
	Questions    []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
	} `json:"questions"`
	Todos []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"todos"`
	Items []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"items"`
	Advance bool  `json:"advance"`
	Done    []int `json:"done"`
	Start   int   `json:"start"`
}

func parseInput(raw json.RawMessage) *toolInput {
	in := &toolInput{}
	_ = json.Unmarshal(raw, in) // a field of another type is left out, the rest is read
	return in
}

// Rooms of the office, each for a kind of work.
const (
	roomLibrary  = "library"
	roomDev      = "dev"
	roomServer   = "server"
	roomSearch   = "search"
	roomMeeting  = "meeting"
	roomDesk     = "desk"
	roomSeat     = "seat"
	roomLounge   = "lounge"
	roomElevator = "elevator"
)

// planTool is the desktop app's plan (todo list) tool; unlike other MCP
// tools it is planning, not looking things up.
const planTool = "mcp__ccd_turn__update_plan"

// roomOf is where a tool is used: reading in the library, changing code in
// the dev room, commands in the server room, the web and MCP tools in the
// search room, planning in the meeting room, questions at the user's desk.
// Anything else, and phoning a subagent, happens at Claude's own seat.
func roomOf(tool string) string {
	switch tool {
	case "Read", "Grep", "Glob", "LS", "ToolSearch", "NotebookRead":
		return roomLibrary
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return roomDev
	case "Bash", "PowerShell", "BashOutput", "KillShell", "Monitor", "TaskStop":
		return roomServer
	case "WebFetch", "WebSearch":
		return roomSearch
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "EnterPlanMode", "Workflow", "Skill", planTool:
		return roomMeeting
	case "AskUserQuestion", "ExitPlanMode":
		return roomDesk
	}
	if strings.HasPrefix(tool, "mcp__") {
		return roomSearch
	}
	return roomSeat
}

// isAgentTool: the tool that hires a subagent ("Task" in older versions).
func isAgentTool(tool string) bool { return tool == "Agent" || tool == "Task" }

// isTodoTool: tools that change the todo list on the meeting room's whiteboard.
func isTodoTool(tool string) bool {
	switch tool {
	case "TodoWrite", "TaskCreate", "TaskUpdate", planTool:
		return true
	}
	return false
}

// gitRx finds a git command in a shell command line (also after cd ... &&).
var gitRx = regexp.MustCompile(`(^|[;&|(]\s*)git(\s|$)`)

// detailOf is the short, private-safe line that says what a tool works on:
// a file's base name, a search pattern, a command's description (or the
// start of the command), a web host. Never a file's content or a full path.
func detailOf(tool string, in *toolInput) (detail string, git bool) {
	switch tool {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookRead", "NotebookEdit":
		p := in.FilePath
		if p == "" {
			p = in.NotebookPath
		}
		return baseName(p), false
	case "LS":
		return baseName(in.Path), false
	case "Grep", "Glob":
		return short(in.Pattern, 60), false
	case "Bash", "PowerShell", "Monitor":
		git = gitRx.MatchString(strings.TrimSpace(in.Command))
		if in.Description != "" {
			return short(in.Description, 80), git
		}
		return short(in.Command, 60), git
	case "WebFetch":
		if u, err := url.Parse(in.URL); err == nil {
			return u.Hostname(), false
		}
		return "", false
	case "WebSearch", "ToolSearch":
		return short(in.Query, 60), false
	case "Agent", "Task":
		typ := in.SubagentType
		if typ == "" {
			typ = "general-purpose"
		}
		if d := short(in.Description, 60); d != "" {
			return typ + " · " + d, false
		}
		return typ, false
	case "AskUserQuestion":
		if len(in.Questions) > 0 {
			if in.Questions[0].Header != "" {
				return short(in.Questions[0].Header, 60), false
			}
			return short(in.Questions[0].Question, 80), false
		}
		return "", false
	case "Skill":
		return short(in.Skill, 60), false
	case "SendMessage":
		return short(in.To, 40), false
	case "Workflow":
		return baseName(in.ScriptPath), false
	}
	if strings.HasPrefix(tool, "mcp__") && tool != planTool {
		server, name, _ := strings.Cut(strings.TrimPrefix(tool, "mcp__"), "__")
		return short(server+"/"+name, 60), false
	}
	return "", false
}

// baseName is a path's last part (paths are private; a file name is not much).
func baseName(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `\/`)
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	return short(p, 60)
}

// short puts s on one line of at most n characters.
func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

// firstLine is the first non-empty line of s, at most n characters.
func firstLine(s string, n int) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return short(l, n)
		}
	}
	return ""
}

// waitTool reduces Claude Code's "waiting for" text to something safe to
// show: "approve Bash: rm -rf x" carries a command or a path, so only the
// tool's name is kept; its own fixed phrases are kept as they are; anything
// else (an AskUserQuestion's question) becomes "".
func waitTool(w string) string {
	w = strings.TrimSpace(w)
	if rest, ok := strings.CutPrefix(w, "approve "); ok {
		if rest == "plan" {
			return "ExitPlanMode"
		}
		name, _, _ := strings.Cut(rest, ":")
		name = strings.TrimSpace(name)
		if f := strings.Fields(name); len(f) > 0 {
			return short(f[0], 40)
		}
		return ""
	}
	switch w {
	case "input needed", "dialog open", "sandbox request", "worker request", "goal proposal":
		return w
	}
	return ""
}

// Todo is one item on the meeting room's whiteboard.
type Todo struct {
	Text   string `json:"text"`
	Status string `json:"status"` // pending | in_progress | completed
	id     int    // a task's number: TaskCreate numbers them from 1 (TodoWrite's items have none)
}

const maxTodos = 50

// todoStatus keeps the three states the whiteboard knows.
func todoStatus(s string) string {
	switch s {
	case "in_progress", "completed":
		return s
	}
	return "pending"
}

// applyTodos changes the list the way a todo tool call does and returns it.
// TodoWrite and the plan tool's items replace it; the plan tool's advance,
// done and start move its steps; TaskCreate adds a task and TaskUpdate
// changes one (tasks are numbered from 1 in the order they were made).
func applyTodos(list []Todo, tool string, in *toolInput) []Todo {
	switch tool {
	case "TodoWrite":
		list = nil
		for _, t := range in.Todos {
			list = append(list, Todo{Text: short(t.Content, 120), Status: todoStatus(t.Status)})
		}
	case planTool:
		if len(in.Items) > 0 {
			list = nil
			for _, t := range in.Items {
				list = append(list, Todo{Text: short(t.Content, 120), Status: todoStatus(t.Status)})
			}
			break
		}
		list = append([]Todo(nil), list...)
		if in.Advance {
			next := -1
			for i := range list {
				if list[i].Status == "in_progress" {
					list[i].Status = "completed"
					next = i + 1
				}
			}
			if next < 0 {
				next = 0
			}
			for i := next; i < len(list); i++ {
				if list[i].Status == "pending" {
					list[i].Status = "in_progress"
					break
				}
			}
		}
		for _, n := range in.Done {
			if n >= 1 && n <= len(list) {
				list[n-1].Status = "completed"
			}
		}
		if in.Start >= 1 && in.Start <= len(list) {
			list[in.Start-1].Status = "in_progress"
		}
	case "TaskCreate":
		top := 0
		for _, t := range list {
			top = max(top, t.id)
		}
		list = append(append([]Todo(nil), list...), Todo{Text: short(in.Subject, 120), Status: "pending", id: top + 1})
	case "TaskUpdate":
		list = append([]Todo(nil), list...)
		n := 0
		switch v := in.TaskID.(type) {
		case string:
			n, _ = strconv.Atoi(strings.TrimPrefix(v, "#"))
		case float64:
			n = int(v)
		}
		for i := range list {
			if list[i].id != n || n == 0 {
				continue
			}
			if in.Status == "deleted" {
				list = append(list[:i], list[i+1:]...)
				break
			}
			if in.Status != "" {
				list[i].Status = todoStatus(in.Status)
			}
			if in.Subject != "" {
				list[i].Text = short(in.Subject, 120)
			}
			break
		}
	}
	if len(list) > maxTodos {
		list = list[:maxTodos]
	}
	return list
}

// doneCount says how far a todo list is: "3/5 done".
func doneCount(list []Todo) string {
	n := 0
	for _, t := range list {
		if t.Status == "completed" {
			n++
		}
	}
	return strconv.Itoa(n) + "/" + strconv.Itoa(len(list)) + " done"
}

// agentResult is what the Agent tool's result says about the subagent.
type agentResult struct {
	Status  string `json:"status"`
	AgentID string `json:"agentId"`
	IsAsync bool   `json:"isAsync"`
}

// launchedOnly says whether an Agent result only reports that a background
// agent was started (it reports later, by a task notification), rather than
// what the agent found.
func launchedOnly(toolUseResult json.RawMessage, text string) (agentID string, launched bool) {
	var ar agentResult
	if raw := trimJSON(toolUseResult); len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &ar) == nil {
		return ar.AgentID, ar.IsAsync || ar.Status == "async_launched"
	}
	l := strings.ToLower(text)
	return "", strings.Contains(l, "async") && strings.Contains(l, "launched")
}

// notice is what a task notification (a background agent finished) names.
type notice struct {
	taskID, toolUseID string
}

var (
	taskIDRx    = regexp.MustCompile(`<task-id>\s*([^<\s]+)\s*</task-id>`)
	toolUseIDRx = regexp.MustCompile(`<tool-use-id>\s*([^<\s]+)\s*</tool-use-id>`)
)

func parseNotice(s string) (notice, bool) {
	if !strings.Contains(s, "<task-notification>") {
		return notice{}, false
	}
	var n notice
	if m := taskIDRx.FindStringSubmatch(s); m != nil {
		n.taskID = m[1]
	}
	if m := toolUseIDRx.FindStringSubmatch(s); m != nil {
		n.toolUseID = m[1]
	}
	return n, n.taskID != "" || n.toolUseID != ""
}

// projectKey is a project folder's key: one floor per folder, whatever its case.
func projectKey(cwd string) string {
	if cwd == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(cwd))
}

// folderName is the name a floor shows: the project folder's own name.
func folderName(cwd string) string {
	c := filepath.Clean(cwd)
	if b := filepath.Base(c); b != "" && b != "." && b != `\` && b != "/" {
		return b
	}
	return c
}
