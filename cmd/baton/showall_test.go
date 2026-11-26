package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/claude"
)

// `baton config desktop_show_all true` lists every conversation on this PC
// in every account's desktop app at once, and false takes those entries off
// again. Every folder is a temporary one.
func TestConfigDesktopShowAll(t *testing.T) {
	c := newCLI(t)
	root := t.TempDir()
	data := filepath.Join(root, "Claude-Data", "claude_test")
	home := filepath.Join(root, "user")
	env := []string{"USERPROFILE=" + home, "LOCALAPPDATA=" + filepath.Join(root, "local"), "APPDATA=" + filepath.Join(root, "roaming"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"), "BATON_DESKTOP_DATA=" + data, "BATON_DESKTOP_RUNNING=0"}
	if out, code := c.run(env, "add", "personal"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	a := c.registry().Get("personal")
	place := filepath.Join(data, "claude-code-sessions", a.AccountUUID, a.OrgID)
	if err := os.MkdirAll(place, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(`{"lastKnownAccountUuid":"`+a.AccountUUID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(root, "Proj")
	_ = os.MkdirAll(proj, 0o700)
	write := func(dir, id string) {
		file := filepath.Join(dir, "projects", claude.ProjectSlug(proj), id+".jsonl")
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		row := `{"type":"user","cwd":` + strconv.Quote(proj) + `,"timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"conversation"}}` + "\n"
		if err := os.WriteFile(file, []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const onAccount, mine = "d35c0000-0000-4000-8000-0000000000b1", "d35c0000-0000-4000-8000-0000000000b2"
	write(a.ProfileDir, onAccount)
	write(filepath.Join(home, ".claude"), mine)
	records := func() int {
		m, _ := filepath.Glob(filepath.Join(place, "local_*.json"))
		return len(m)
	}

	out, code := c.run(env, "config", "desktop_show_all", "true")
	if code != 0 || !strings.Contains(out, "Conversations listed in the desktop app: 2 (entries: 2).") ||
		!strings.Contains(out, "New conversations are listed while baton's window is open.") || records() != 2 {
		t.Fatalf("on: %d\n%s", code, out)
	}
	if out, _ := c.run(env, "config", "desktop_show_all"); strings.TrimSpace(out) != "true" {
		t.Fatalf("get: %s", out)
	}
	if out, code := c.run(env, "config", "desktop_show_all", "true"); code != 0 || !strings.Contains(out, "listed in every account's desktop app already") {
		t.Fatalf("on again: %d\n%s", code, out)
	}
	// What the setting listed shows ticked like any other, and says so. Taken
	// off by name, it stays off: listing again leaves it out.
	if out, _ := c.run(env, "desktop", mine); !strings.Contains(out, "[x] personal") || !strings.Contains(out, "listed because every conversation is shown") {
		t.Fatalf("list:\n%s", out)
	}
	if out, code := c.run(env, "desktop", mine, "--hide", "personal"); code != 0 || records() != 1 {
		t.Fatalf("hide: %d\n%s", code, out)
	}
	if out, code := c.run(env, "config", "desktop_show_all", "true"); code != 0 || records() != 1 || !strings.Contains(out, "already") {
		t.Fatalf("on after a hide: %d\n%s", code, out)
	}
	if out, _ := c.run(env, "desktop", mine); !strings.Contains(out, "[ ] personal") || !strings.Contains(out, "you took it off here") {
		t.Fatalf("list after a hide:\n%s", out)
	}
	if out, code := c.run(append(env, "BATON_RUN=x"), "config", "desktop_show_all", "false"); code == 0 || !strings.Contains(out, "inside a baton session") || records() != 1 {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
	out, code = c.run(env, "config", "desktop_show_all", "false")
	if code != 0 || !strings.Contains(out, "Entries taken off the desktop app: 1.") || records() != 0 {
		t.Fatalf("off: %d\n%s", code, out)
	}
	if out, code := c.run(env, "config", "desktop_show_all", "false"); code != 0 || !strings.Contains(out, "Nothing was listed by this setting.") {
		t.Fatalf("off again: %d\n%s", code, out)
	}
	if out, code := c.run(env, "config", "desktop_show_all", "maybe"); code == 0 || !strings.Contains(out, "true or false") {
		t.Fatalf("a bad value: %d\n%s", code, out)
	}
}
