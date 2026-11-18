package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// bgTranscript writes rows as Claude Code writes them, one JSON object a
// line, each with its timestamp.
type bgTranscript struct {
	t    *testing.T
	rows []string
}

func (b *bgTranscript) add(at time.Time, row map[string]any) {
	row["timestamp"] = at.UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(row)
	if err != nil {
		b.t.Fatal(err)
	}
	b.rows = append(b.rows, string(raw))
}

// call is an assistant row calling a tool.
func (b *bgTranscript) call(at time.Time, id, name string, input map[string]any) {
	b.add(at, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}})
}

// answer is the user row with a tool call's result.
func (b *bgTranscript) answer(at time.Time, id, text string, result map[string]any) {
	b.add(at, map[string]any{"type": "user", "message": map[string]any{"role": "user",
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": text}}}, "toolUseResult": result})
}

func notice(task, call, status, summary string) string {
	s := "<task-notification>\n<task-id>" + task + "</task-id>\n"
	if call != "" {
		s += "<tool-use-id>" + call + "</tool-use-id>\n"
	}
	if status != "" {
		s += "<status>" + status + "</status>\n"
	}
	return s + "<summary>" + summary + "</summary>\n</task-notification>"
}

// queued is a task notification as it is queued for Claude.
func (b *bgTranscript) queued(at time.Time, text string) {
	b.add(at, map[string]any{"type": "queue-operation", "operation": "enqueue", "content": text})
}

// handed is a task notification as it is handed to Claude.
func (b *bgTranscript) handed(at time.Time, text string) {
	b.add(at, map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "prompt": text,
		"commandMode": "task-notification"}})
}

func (b *bgTranscript) write() string {
	p := filepath.Join(b.t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(b.rows, "\n")+"\n"), 0o600); err != nil {
		b.t.Fatal(err)
	}
	return p
}

func TestBackgroundSince(t *testing.T) {
	since := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return since.Add(time.Duration(min) * time.Minute) }
	b := &bgTranscript{t: t}
	ran := func(min int, call, task string) {
		b.call(at(min), call, "Bash", map[string]any{"command": "npm run dev", "run_in_background": true})
		b.answer(at(min), call, "Command running in background with ID: "+task, map[string]any{"backgroundTaskId": task, "stdout": ""})
	}
	// Before since: an earlier process's, ended with it.
	ran(-30, "toolu_old", "bold")
	// A dev server, still running; a build, done (queued, then handed);
	// one stopped by Claude.
	ran(1, "toolu_dev", "bdev")
	ran(2, "toolu_build", "bbuild")
	b.queued(at(3), notice("bbuild", "toolu_build", "completed", "build done"))
	b.handed(at(3), notice("bbuild", "toolu_build", "completed", "build done"))
	ran(4, "toolu_watch", "bwatch")
	b.call(at(5), "toolu_stop", "TaskStop", map[string]any{"task_id": "bwatch"})
	b.answer(at(5), "toolu_stop", "Successfully stopped task: bwatch", map[string]any{"task_id": "bwatch", "task_type": "local_bash"})
	// A Monitor's event says nothing of how it ended.
	ran(6, "toolu_mon", "bmon")
	b.queued(at(7), notice("bmon", "", "", "Monitor event: x"))
	// A background agent, done; a workflow, still running; one that failed.
	b.call(at(8), "toolu_agent", "Agent", map[string]any{"prompt": "x", "run_in_background": true})
	b.answer(at(8), "toolu_agent", "Async agent launched successfully.", map[string]any{"isAsync": true, "status": "async_launched", "agentId": "a1"})
	b.queued(at(9), notice("a1", "toolu_agent", "completed", "agent done"))
	b.call(at(10), "toolu_wf", "Workflow", map[string]any{"script": "x"})
	b.answer(at(10), "toolu_wf", "Workflow started", map[string]any{"status": "async_launched", "taskId": "wf1", "runId": "r"})
	b.call(at(11), "toolu_wf2", "Workflow", map[string]any{"script": "x"})
	b.answer(at(11), "toolu_wf2", "Workflow started", map[string]any{"status": "async_launched", "taskId": "wf2"})
	b.queued(at(12), notice("wf2", "toolu_wf2", "failed", "workflow failed"))
	// The last wake-up asked for, and cron jobs: one recurring (still to
	// come), one kept on disk (survives a restart), one deleted, one
	// one-time job long past.
	b.call(at(13), "toolu_w1", "ScheduleWakeup", map[string]any{"delaySeconds": 1200, "prompt": "/loop x"})
	b.call(at(14), "toolu_c1", "CronCreate", map[string]any{"cron": "7 * * * *", "prompt": "x"})
	b.answer(at(14), "toolu_c1", "Scheduled job c1x (next run 10:07)", map[string]any{"id": "c1x"})
	b.call(at(15), "toolu_c2", "CronCreate", map[string]any{"cron": "7 * * * *", "prompt": "x", "durable": true})
	b.answer(at(15), "toolu_c2", "Scheduled job c2x", map[string]any{"id": "c2x"})
	b.call(at(16), "toolu_c3", "CronCreate", map[string]any{"cron": "9 * * * *", "prompt": "x"})
	b.answer(at(16), "toolu_c3", "Scheduled job c3x", map[string]any{"id": "c3x"})
	b.call(at(17), "toolu_c3d", "CronDelete", map[string]any{"id": "c3x"})
	past := at(18).Local()
	b.call(at(18), "toolu_c4", "CronCreate", map[string]any{"cron": cronAt(past), "prompt": "x", "recurring": false})
	b.answer(at(18), "toolu_c4", "Scheduled job c4x", map[string]any{"id": "c4x"})
	p := b.write()

	got, err := BackgroundSince(p, since, at(20))
	if err != nil {
		t.Fatal(err)
	}
	if !sameSet(got.Commands, []string{"bdev", "bmon"}) || !slices.Equal(got.Agents, []string{"wf1"}) {
		t.Fatalf("commands %v, agents %v", got.Commands, got.Agents)
	}
	if !sameSet(got.Wakeups, []string{at(33).Format(time.RFC3339), "cron toolu_c1"}) {
		t.Fatalf("wake-ups %v", got.Wakeups)
	}
	// Long after: the wake-up is past (it fired), the one-time job ran; the
	// repeating job lives a week.
	if got, _ := BackgroundSince(p, since, at(60*24)); !slices.Equal(got.Wakeups, []string{"cron toolu_c1"}) {
		t.Fatalf("a day later: %v", got.Wakeups)
	}
	if got, _ := BackgroundSince(p, since, since.Add(8*24*time.Hour)); len(got.Wakeups) != 0 {
		t.Fatalf("eight days later: %v", got.Wakeups)
	}
	// The loop called off.
	b.call(at(21), "toolu_w2", "ScheduleWakeup", map[string]any{"stop": true})
	if got, _ := BackgroundSince(b.write(), since, at(22)); slices.Contains(got.Wakeups, at(33).Format(time.RFC3339)) {
		t.Fatalf("called off: %v", got.Wakeups)
	}
	// None of it is from this process: nothing.
	if got, _ := BackgroundSince(p, at(30), at(31)); got.Any() {
		t.Fatalf("since later: %+v", got)
	}
	if got, err := BackgroundSince(filepath.Join(t.TempDir(), "none.jsonl"), since, at(1)); err != nil || got.Any() {
		t.Fatalf("no transcript: %+v %v", got, err)
	}
}

// cronAt is a one-time cron expression for t (local time).
func cronAt(t time.Time) string {
	return strings.Join([]string{itoa(t.Minute()), itoa(t.Hour()), itoa(t.Day()), itoa(int(t.Month())), "*"}, " ")
}

func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(x string) bool { return !slices.Contains(b, x) })
}
