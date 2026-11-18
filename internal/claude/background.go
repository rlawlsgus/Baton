package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Background is what a conversation's Claude Code has going on in the
// background, as its transcript tells it: work it started and that has not
// told it it ended. Each names what is still going on (a task's or a tool
// call's id; a wake-up by when it is due).
type Background struct {
	Commands []string // background commands (Bash or PowerShell run in the background)
	Agents   []string // background agents and workflows
	Wakeups  []string // a /loop's next wake-up (ScheduleWakeup), cron jobs it made
}

// Any reports whether anything is going on.
func (b Background) Any() bool { return len(b.Commands)+len(b.Agents)+len(b.Wakeups) > 0 }

// wakeGrace is how long after its time a wake-up still counts as to come:
// one that falls due while Claude works fires once it is done.
const wakeGrace = 2 * time.Minute

// cronMaxAge is how long a repeating cron job lives: it then fires once
// more, and is deleted (Claude Code's recurringMaxAgeMs).
const cronMaxAge = 7 * 24 * time.Hour

// BackgroundSince reads transcript for background work its Claude Code
// started at since or later and that has not ended by now. Work started
// before since belonged to an earlier process of the conversation, and
// ended with it.
//
// A task ends with the task notification that says how it ended
// (completed, failed, killed...), or when Claude stopped it (TaskStop); a
// Monitor's notifications of what it saw do not end it. A wake-up is to
// come until a little after it is due, unless Claude called it off. A cron
// job made for this session only is to come until Claude deletes it, or
// until it ends on its own: a one-time job once it has run, a repeating
// one after cronMaxAge. One kept on disk (durable) survives a restart, and
// does not count. A missing transcript has nothing going on.
func BackgroundSince(transcript string, since, now time.Time) (Background, error) {
	f, err := os.Open(transcript)
	if errors.Is(err, os.ErrNotExist) {
		return Background{}, nil
	}
	if err != nil {
		return Background{}, err
	}
	defer f.Close()
	s := bgScan{since: since, open: map[string]bgTask{}, ended: map[string]bool{}, crons: map[string]*bgCron{}}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && s.worthReading(line) {
			s.row(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Background{}, err
		}
	}
	return s.result(now), nil
}

// bgMarks are what a row worth reading here holds; the others (most) are
// not parsed at all.
var bgMarks = [][]byte{
	[]byte(`"backgroundTaskId"`), []byte(`async_launched`), []byte(`"isAsync":true`), []byte(`task-notification`),
	[]byte(`"task_id"`), []byte(`"shell_id"`), []byte(`"ScheduleWakeup"`), []byte(`"CronCreate"`), []byte(`"CronDelete"`),
}

type bgTask struct {
	kind   string // "command" or "agent"
	taskID string
}

// bgCron is a cron job made for the session only.
type bgCron struct {
	until  time.Time // when it ends on its own
	result string    // what the call that made it answered (it names the job's id)
}

type bgScan struct {
	since   time.Time
	open    map[string]bgTask  // work started in the background, by the tool call that started it
	ended   map[string]bool    // task and tool call ids that ended
	wakeAt  time.Time          // the last wake-up asked for, when it is due
	wakeOff bool               // the last ScheduleWakeup called the loop off
	crons   map[string]*bgCron // cron jobs, by the tool call that made them, until deleted
}

// worthReading: a row with one of bgMarks, or the answer to a call that
// made a cron job (it names the job's id).
func (s *bgScan) worthReading(line []byte) bool {
	for _, m := range bgMarks {
		if bytes.Contains(line, m) {
			return true
		}
	}
	for call := range s.crons {
		if bytes.Contains(line, []byte(call)) {
			return true
		}
	}
	return false
}

type bgRow struct {
	Type          string          `json:"type"`
	Timestamp     time.Time       `json:"timestamp"`
	Content       json.RawMessage `json:"content"` // a queue operation's prompt
	Message       *bgMessage      `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Attachment    *struct {
		CommandMode string          `json:"commandMode"`
		Prompt      json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

type bgMessage struct {
	Content json.RawMessage `json:"content"`
}

type bgBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Content   json.RawMessage `json:"content"`
}

type bgResult struct {
	BackgroundTaskID string `json:"backgroundTaskId"`
	Status           string `json:"status"`
	TaskID           string `json:"taskId"`
	AgentID          string `json:"agentId"`
	IsAsync          bool   `json:"isAsync"`
	StoppedTask      string `json:"task_id"`
	StoppedShell     string `json:"shell_id"`
}

func (s *bgScan) row(line []byte) {
	var r bgRow
	if json.Unmarshal(line, &r) != nil {
		return
	}
	fresh := !r.Timestamp.IsZero() && !r.Timestamp.Before(s.since)
	// Task notifications, wherever they are written: queued, handed to
	// Claude, or as a user row (older versions).
	if r.Type == "queue-operation" {
		s.notified(textOf(r.Content))
	}
	if r.Attachment != nil && r.Attachment.CommandMode == "task-notification" {
		s.notified(textOf(r.Attachment.Prompt))
	}
	var blocks []bgBlock
	if r.Message != nil && len(r.Message.Content) > 0 && r.Message.Content[0] == '[' {
		_ = json.Unmarshal(r.Message.Content, &blocks)
	}
	var res bgResult
	if len(r.ToolUseResult) > 0 && r.ToolUseResult[0] == '{' {
		_ = json.Unmarshal(r.ToolUseResult, &res)
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			s.notified(b.Text)
		case "tool_use":
			if fresh {
				s.called(b, r.Timestamp)
			}
		case "tool_result":
			s.answered(b.ToolUseID, res, string(r.ToolUseResult)+" "+textOf(b.Content), fresh)
		}
	}
}

// called notes a call that asks for work to come: a wake-up, a cron job;
// or one that calls a cron job off.
func (s *bgScan) called(b bgBlock, at time.Time) {
	switch b.Name {
	case "ScheduleWakeup":
		var in struct {
			Delay float64 `json:"delaySeconds"`
			Stop  bool    `json:"stop"`
		}
		_ = json.Unmarshal(b.Input, &in)
		if in.Stop {
			s.wakeOff, s.wakeAt = true, time.Time{}
			return
		}
		d := min(max(in.Delay, 60), 3600)
		s.wakeOff, s.wakeAt = false, at.Add(time.Duration(d*float64(time.Second)))
	case "CronCreate":
		var in struct {
			Cron      string `json:"cron"`
			Recurring *bool  `json:"recurring"`
			Durable   bool   `json:"durable"`
		}
		_ = json.Unmarshal(b.Input, &in)
		if in.Durable || b.ID == "" {
			return
		}
		until := at.Add(cronMaxAge + wakeGrace)
		if in.Recurring != nil && !*in.Recurring {
			if due, ok := oneTimeDue(in.Cron, at); ok {
				until = due.Add(wakeGrace)
			}
		}
		s.crons[b.ID] = &bgCron{until: until}
	case "CronDelete":
		var in struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(b.Input, &in)
		if id := strings.TrimSpace(in.ID); id != "" {
			for call, c := range s.crons {
				if strings.Contains(c.result, id) {
					delete(s.crons, call)
				}
			}
		}
	}
}

// answered notes what a tool call's answer says: work started in the
// background, work stopped, or (raw) the id of a cron job it made.
func (s *bgScan) answered(call string, res bgResult, raw string, fresh bool) {
	if c := s.crons[call]; c != nil {
		c.result = raw
	}
	if id := strings.TrimSpace(res.StoppedTask + res.StoppedShell); id != "" {
		s.ended[id] = true
	}
	if !fresh || call == "" {
		return
	}
	switch {
	case res.BackgroundTaskID != "":
		s.open[call] = bgTask{kind: "command", taskID: res.BackgroundTaskID}
	case res.Status == "async_launched" || res.IsAsync:
		id := res.TaskID
		if id == "" {
			id = res.AgentID
		}
		s.open[call] = bgTask{kind: "agent", taskID: id}
	}
}

var (
	bgTaskIDRx    = regexp.MustCompile(`<task-id>\s*([^<\s]+)\s*</task-id>`)
	bgToolUseIDRx = regexp.MustCompile(`<tool-use-id>\s*([^<\s]+)\s*</tool-use-id>`)
	bgStatusRx    = regexp.MustCompile(`<status>\s*([^<\s]*)\s*</status>`)
)

// notified notes a task notification that says how a task ended. One
// without a status (a Monitor's event) ends nothing.
func (s *bgScan) notified(text string) {
	if !strings.Contains(text, "<task-notification>") {
		return
	}
	st := bgStatusRx.FindStringSubmatch(text)
	if st == nil || st[1] == "" || strings.EqualFold(st[1], "running") {
		return
	}
	if m := bgTaskIDRx.FindStringSubmatch(text); m != nil {
		s.ended[m[1]] = true
	}
	if m := bgToolUseIDRx.FindStringSubmatch(text); m != nil {
		s.ended[m[1]] = true
	}
}

func (s *bgScan) result(now time.Time) Background {
	var b Background
	for call, t := range s.open {
		if s.ended[call] || t.taskID != "" && s.ended[t.taskID] {
			continue
		}
		id := t.taskID
		if id == "" {
			id = call
		}
		if t.kind == "command" {
			b.Commands = append(b.Commands, id)
		} else {
			b.Agents = append(b.Agents, id)
		}
	}
	if !s.wakeOff && !s.wakeAt.IsZero() && now.Before(s.wakeAt.Add(wakeGrace)) {
		b.Wakeups = append(b.Wakeups, s.wakeAt.UTC().Format(time.RFC3339))
	}
	for call, c := range s.crons {
		if now.Before(c.until) {
			b.Wakeups = append(b.Wakeups, "cron "+call)
		}
	}
	return b
}

// oneTimeDue is when a one-time cron job made at made fires: its minute,
// hour, day and month are fixed (local time); the first such moment from
// then on.
func oneTimeDue(cron string, made time.Time) (time.Time, bool) {
	f := strings.Fields(cron)
	if len(f) != 5 {
		return time.Time{}, false
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			return time.Time{}, false
		}
		n[i] = v
	}
	minute, hour, day, month := n[0], n[1], n[2], n[3]
	if minute < 0 || minute > 59 || hour < 0 || hour > 23 || day < 1 || day > 31 || month < 1 || month > 12 {
		return time.Time{}, false
	}
	local := made.Local()
	for y := local.Year(); y <= local.Year()+1; y++ {
		t := time.Date(y, time.Month(month), day, hour, minute, 0, 0, time.Local)
		if t.Day() == day && !t.Before(local.Add(-time.Minute)) {
			return t, true
		}
	}
	return time.Time{}, false
}

// textOf is a JSON string's text, or the text of a JSON array's blocks.
func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []bgBlock
	if raw[0] == '[' && json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, x := range blocks {
			b.WriteString(x.Text)
		}
		return b.String()
	}
	return ""
}
