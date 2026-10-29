package supervisor

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"baton/internal/app"
	"baton/internal/config"
	"baton/internal/run"
)

func TestClaudeWatch(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	type look struct {
		sec    int
		status string
		want   string
	}
	cases := map[string][]look{
		"a long response is news once it has settled": {
			{0, "idle", ""}, {1, "busy", ""}, {40, "busy", ""}, {41, "idle", ""}, {43, "idle", ""}, {44, "idle", noteDone}, {50, "idle", ""},
		},
		"a short response is not": {
			{0, "busy", ""}, {10, "idle", ""}, {20, "idle", ""},
		},
		"a question is news, once": {
			{0, "busy", ""}, {5, "waiting", ""}, {8, "waiting", noteWaiting}, {30, "waiting", ""},
		},
		"a question answered at once is not": {
			{0, "busy", ""}, {5, "waiting", ""}, {6, "busy", ""}, {20, "busy", ""},
		},
		"time spent on a question counts toward the response": {
			{0, "busy", ""}, {5, "waiting", ""}, {9, "waiting", noteWaiting}, {25, "busy", ""}, {31, "idle", ""}, {35, "idle", noteDone},
		},
		"a response that ends in a usage limit goes on before it settles": {
			{0, "busy", ""}, {40, "idle", ""}, {41, "idle", ""},
		},
		"other statuses count as working": {
			{0, "shell", ""}, {35, "idle", ""}, {38, "idle", noteDone},
		},
		"a session first seen working is timed from then": {
			{100, "busy", ""}, {120, "idle", ""}, {125, "idle", ""},
		},
	}
	for name, looks := range cases {
		var w claudeWatch
		for _, l := range looks {
			if got := w.step(l.status, at(l.sec)); got != l.want {
				t.Errorf("%s: at %ds %s -> %q, want %q", name, l.sec, l.status, got, l.want)
			}
		}
	}
}

func TestWantAwake(t *testing.T) {
	cases := []struct {
		on     bool
		state  string
		claude string
		want   bool
	}{
		{true, run.StateRunning, "busy", true},
		{true, run.StateRunning, "idle", false},
		{true, run.StateRunning, "waiting", false}, // waits for the user, who is away
		{true, run.StateRunning, "", false},
		{true, run.StateStopping, "idle", true},
		{true, run.StateSwitching, "", true},
		{true, run.StateWaiting, "", true}, // waiting for a limit to reset
		{true, run.StateAsking, "", false}, // waiting for the user's answer
		{true, run.StateEnding, "busy", true},
		{true, run.StateDone, "busy", false},
		{false, run.StateRunning, "busy", false},
		{false, run.StateWaiting, "", false},
	}
	for _, c := range cases {
		if got := wantAwake(c.on, c.state, c.claude); got != c.want {
			t.Errorf("wantAwake(%v, %s, %q) = %v, want %v", c.on, c.state, c.claude, got, c.want)
		}
	}
}

// The notifications setting decides what is shown: everything, all but a
// finished response, or nothing.
func TestNotifyFollowsTheSetting(t *testing.T) {
	file := os.Getenv("BATON_NOTIFY_FILE")
	_ = os.Remove(file)
	s := &Supervisor{runID: "20261029-010203-abcdef", logf: func(string, ...any) {}}
	s.opt.Paths = app.Paths{Root: t.TempDir()}
	s.opt.Cwd = `C:\work\proj`
	for _, lvl := range []string{"", config.NotifyImportant, config.NotifyOff} {
		s.opt.Config.Notifications = lvl
		s.notify(noteDone, "done "+lvl)
		s.notify(noteSwitched, "switched "+lvl)
	}
	data, _ := os.ReadFile(file)
	var bodies []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var n map[string]string
		if json.Unmarshal([]byte(line), &n) != nil || n["title"] != "baton · proj" || n["group"] != s.runID || n["launch"] != "" {
			t.Fatalf("note %s", line)
		}
		bodies = append(bodies, n["body"])
	}
	if want := []string{"done ", "switched ", "switched important"}; !slices.Equal(bodies, want) {
		t.Fatalf("shown %q, want %q", bodies, want)
	}
}
