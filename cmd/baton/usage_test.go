package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// `baton doctor` (also the window's "Check sign-ins") asks each signed-in
// subscription's Claude Code for its plan usage: no prompt, hooks and MCP
// servers off, nothing saved as a session, in the profile's own folder and
// out of its session list. `baton list` shows what it said. A signed-out
// account is not asked, and a Claude Code that does not know the request
// changes nothing.
func TestDoctorAsksForUsage(t *testing.T) {
	c := newCLI(t)
	for _, name := range []string{"personal", "work"} {
		if out, code := c.run(nil, "add", name); code != 0 {
			t.Fatalf("add %s: %d\n%s", name, code, out)
		}
	}
	reg := c.registry()
	personal, work := reg.Get("personal").ProfileDir, reg.Get("work").ProfileDir
	if err := os.WriteFile(filepath.Join(work, "fake-usage.json"),
		[]byte(`{"five_hour":{"utilization":80,"resets_at":"2099-01-01T00:00:00Z"},"seven_day":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := c.run(nil, "doctor")
	if code != 0 || !strings.Contains(out, "account personal     usage: 5h 34%, week 12% (just now)") ||
		!strings.Contains(out, "account work         usage: 5h 80% (just now)") {
		t.Fatalf("doctor: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(c.home, "usage", "personal.json")); err != nil {
		t.Fatal("usage not kept")
	}
	calls := usageCalls(t, personal)
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	call := calls[0]
	for _, want := range []string{"-p", "stream-json", "--strict-mcp-config", `{"disableAllHooks":true}`, "--no-session-persistence"} {
		if !slices.Contains(call.Args, want) {
			t.Errorf("args %q lack %q", call.Args, want)
		}
	}
	if call.Entrypoint != "baton-usage" || !strings.EqualFold(filepath.Clean(call.Cwd), filepath.Clean(personal)) {
		t.Errorf("entrypoint %q, folder %q", call.Entrypoint, call.Cwd)
	}
	if ents, _ := os.ReadDir(filepath.Join(personal, "sessions")); len(ents) != 0 {
		t.Errorf("left in the session list: %v", ents)
	}
	if out, _ := c.run(nil, "list"); !strings.Contains(out, "5h 34%, week 12%") || !strings.Contains(out, "5h 80%") {
		t.Fatalf("accounts:\n%s", out)
	}

	// Signed out: not asked. A Claude Code that does not know get_usage:
	// said so, and what was known stays.
	if err := os.Remove(filepath.Join(work, "fake-auth.json")); err != nil { // its sign-in ended
		t.Fatal(err)
	}
	out, code = c.run([]string{"FAKE_USAGE_FAIL=1"}, "doctor")
	if !strings.Contains(out, "account personal     usage not reported: claude get_usage: Unknown control request subtype: get_usage") ||
		strings.Contains(out, "account work         usage") || len(usageCalls(t, work)) != 1 {
		t.Fatalf("doctor, one signed out and one Claude Code without get_usage: %d\n%s", code, out)
	}
	if out, _ := c.run(nil, "list"); !strings.Contains(out, "5h 34%, week 12%") {
		t.Fatalf("accounts after a failed check:\n%s", out)
	}
}

type usageCall struct {
	Args       []string `json:"args"`
	Entrypoint string   `json:"entrypoint"`
	Cwd        string   `json:"cwd"`
}

// usageCalls are the get_usage runs the fake Claude Code noted in a profile.
func usageCalls(t *testing.T, profile string) []usageCall {
	t.Helper()
	f, err := os.Open(filepath.Join(profile, "fake-usage-calls.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []usageCall
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c usageCall
		if json.Unmarshal(sc.Bytes(), &c) == nil {
			out = append(out, c)
		}
	}
	return out
}
