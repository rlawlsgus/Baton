package gui

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"baton/internal/claude"
)

const (
	ccInUse   = `C:\Users\x\AppData\Local\Claude-Data\claude_x\claude-code\2.1.317\abc\claude.exe`
	ccOldCLI  = `C:\Users\x\.local\bin\claude.exe`
	ccOldDesk = `C:\Users\x\AppData\Local\Claude-Data\claude_x\claude-code\2.1.200\abc\claude.exe`
)

// withClaude has the rig's window find these copies of Claude Code; calls
// counts the times it looked.
func withClaude(r *rig, calls *atomic.Int32, gate chan struct{}) {
	r.srv.mu.Lock()
	defer r.srv.mu.Unlock()
	r.srv.opt.Claude = func() ClaudeInfo {
		calls.Add(1)
		if gate != nil {
			<-gate
		}
		return ClaudeInfo{Found: true, Version: "2.1.317", Source: "desktop", Path: ccInUse, Auto: ccInUse, Min: claude.MinVersion,
			Installs: []ClaudeInstall{
				{Path: ccOldCLI, Version: "2.1.200", Source: "native", Problem: claude.ProblemTooOld},
				{Path: ccInUse, Version: "2.1.317", Source: "desktop", InUse: true},
				{Path: ccOldDesk, Version: "2.1.200", Source: "desktop", Problem: claude.ProblemTooOld},
			}}
	}
}

// claudeState polls the state until the window has looked for Claude Code.
func claudeState(r *rig) map[string]any {
	r.t.Helper()
	for i := 0; i < 200; i++ {
		code, body := r.do("GET", "/api/state", "", nil)
		var st map[string]any
		if code != 200 || json.Unmarshal([]byte(body), &st) != nil {
			r.t.Fatalf("state: %d %s", code, body)
		}
		if st["claude_pending"] == false {
			return st["claude"].(map[string]any)
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatal("Claude Code was never looked for")
	return nil
}

// The window gets every copy of Claude Code found, to choose from; nothing
// about versions newer than tested.
func TestClaudeInfoListsEveryCopy(t *testing.T) {
	r := newRig(t)
	var calls atomic.Int32
	withClaude(r, &calls, nil)
	r.login()
	c := claudeState(r)
	ins, _ := c["installs"].([]any)
	if len(ins) != 3 || c["min"] != claude.MinVersion || c["auto"] != ccInUse || c["version"] != "2.1.317" {
		t.Fatalf("claude: %v", c)
	}
	first := ins[0].(map[string]any)
	if first["path"] != ccOldCLI || first["version"] != "2.1.200" || first["source"] != "native" || first["problem"] != "too_old" || first["in_use"] != false {
		t.Fatalf("install: %v", first)
	}
	if ins[1].(map[string]any)["in_use"] != true {
		t.Fatalf("in use: %v", ins[1])
	}
	for _, gone := range []string{"warning", "old_cli"} {
		if _, ok := c[gone]; ok {
			t.Errorf("%s is still sent", gone)
		}
	}
}

// The Update button runs `baton claude-update` in a console window only for
// a CLI copy the window found too old, one at a time, and Claude Code is
// looked for again once it ends.
func TestClaudeUpdateOnlyForAnOldCLI(t *testing.T) {
	r := newRig(t)
	var calls atomic.Int32
	withClaude(r, &calls, nil)
	r.login()
	claudeState(r)
	post := func(p string) (int, string) {
		b, _ := json.Marshal(map[string]string{"path": p})
		return r.do("POST", "/api/claude/update", string(b), nil)
	}
	for _, p := range []string{ccInUse, ccOldDesk, strings.ToUpper(ccOldCLI), `C:\Windows\System32\cmd.exe`, ""} {
		if code, body := post(p); code != http.StatusBadRequest || !strings.Contains(body, "not_updatable") {
			t.Errorf("%q: %d %s", p, code, body)
		}
	}
	if code, body := r.do("POST", "/api/claude/update", `{"path":"x","args":["--evil"]}`, nil); code != http.StatusBadRequest {
		t.Errorf("unknown field: %d %s", code, body)
	}
	r.spawn.mu.Lock()
	if len(r.spawn.console) != 0 {
		t.Fatalf("a refused update ran: %q", r.spawn.console)
	}
	release := make(chan struct{})
	r.spawn.release = release
	r.spawn.mu.Unlock()
	if code, body := post(ccOldCLI); code != 200 {
		t.Fatalf("update: %d %s", code, body)
	}
	r.spawn.mu.Lock()
	got := slices.Clone(r.spawn.console)
	r.spawn.mu.Unlock()
	if len(got) != 1 || !slices.Equal(got[0], []string{"claude-update", ccOldCLI}) {
		t.Fatalf("console: %q", got)
	}
	if code, body := post(ccOldCLI); code != http.StatusConflict || !strings.Contains(body, "claude_updating") {
		t.Fatalf("second update while one runs: %d %s", code, body)
	}
	before := calls.Load()
	close(release)
	for i := 0; i < 200 && calls.Load() == before; i++ {
		r.do("GET", "/api/state", "", nil)
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == before {
		t.Fatal("Claude Code was not looked for again after the update")
	}
}

// An answer worked out before another copy was chosen is not shown as the
// answer for the new choice.
func TestClaudeInfoStaleAnswerDropped(t *testing.T) {
	r := newRig(t)
	var calls atomic.Int32
	gate := make(chan struct{})
	withClaude(r, &calls, gate)
	r.login()
	r.do("GET", "/api/state", "", nil) // starts looking (held at the gate)
	for i := 0; i < 200 && calls.Load() == 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if code, body := r.do("POST", "/api/config", `{"key":"claude_path","value":"desktop"}`, nil); code != 200 {
		t.Fatalf("config: %d %s", code, body)
	}
	gate <- struct{}{} // the old answer arrives
	time.Sleep(50 * time.Millisecond)
	var st map[string]any
	_, body := r.do("GET", "/api/state", "", nil)
	_ = json.Unmarshal([]byte(body), &st)
	if st["claude_pending"] != true {
		t.Fatalf("the stale answer was kept: %v", st["claude"])
	}
	close(gate)
	claudeState(r)
	if calls.Load() < 2 {
		t.Fatalf("looked %d times", calls.Load())
	}
}
