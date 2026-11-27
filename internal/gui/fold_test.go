package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// The list of every conversation folds a session's conversations under its
// newest one: earlier parts (as the desktop app's records and baton's
// threads say, also through one no longer on disk), copies and the
// conversations they copied, another file of one conversation, and where
// the session went on apart; never two that only share a name.
func TestFoldParts(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 11, 27, 12, m, 0, 0, time.UTC) }
	c := func(id, where string, m int, from string) conversationView {
		return conversationView{SessionID: id, Where: where, Title: "the same name", Modified: at(m), from: from}
	}
	list := []conversationView{ // newest first, as listed
		c("c2", "claude-code", 60, "o1"), // a copy, newer than what it copied
		c("e5", "claude-code", 50, ""),   // the session's newest part
		c("x1", "claude-code", 45, ""),   // only the same name
		c("E3", "claude-code", 40, ""),   // an earlier part (ids in any case)
		c("k1", "a", 35, "e3"),           // a copy of an earlier part
		c("e5", "a", 30, ""),             // another file of the newest
		c("e1", "claude-code", 20, ""),   // earlier, through e2, which is gone
		c("o1", "claude-code", 15, ""),   // what c2 copied
		c("b1", "claude-code", 10, ""),   // where the session went on apart from e3
	}
	later := map[string][]string{"e1": {"e2"}, "e2": {"e3"}, "e3": {"e5", "b1"}}
	got := foldParts(list, later)
	type part struct{ id, where, part string }
	view := func(rows []conversationView) []part {
		var out []part
		for _, r := range rows {
			out = append(out, part{r.SessionID, r.Where, r.Part})
		}
		return out
	}
	if rows := view(got); !slices.Equal(rows, []part{{"c2", "claude-code", ""}, {"e5", "claude-code", ""}, {"x1", "claude-code", ""}}) {
		t.Fatalf("rows: %v", rows)
	}
	if f := view(got[0].Earlier); !slices.Equal(f, []part{{"o1", "claude-code", partOriginal}}) {
		t.Fatalf("under the copy: %v", f)
	}
	if f := view(got[1].Earlier); !slices.Equal(f, []part{{"E3", "claude-code", partEarlier}, {"k1", "a", partCopy},
		{"e5", "a", partFile}, {"e1", "claude-code", partEarlier}, {"b1", "claude-code", partBranch}}) {
		t.Fatalf("under the newest part: %v", f)
	}
	if len(got[2].Earlier) != 0 {
		t.Fatalf("a name joined two: %v", view(got[2].Earlier))
	}
	// Nothing joins: the list as it was.
	if rows := foldParts(list[2:3], nil); len(rows) != 1 || len(rows[0].Earlier) != 0 {
		t.Fatalf("alone: %v", view(rows))
	}
}

// ?fold=1 folds them in the window's list of every conversation: a copy
// under the newer conversation it copied, and an earlier part of a desktop
// app session (its record in an account's list names it) under the one the
// session is on now. Without it the list is as it was.
func TestConversationsFolded(t *testing.T) {
	r := newRig(t)
	r.login()
	home := os.Getenv("CLAUDE_CONFIG_DIR")
	data := filepath.Join(t.TempDir(), "Claude-Data", "claude_test")
	t.Setenv("BATON_DESKTOP_DATA", data)
	proj := t.TempDir()
	const (
		earlier = "11111111-2222-4333-8444-555555555501"
		now     = "11111111-2222-4333-8444-555555555502"
		copied  = "11111111-2222-4333-8444-555555555503"
		other   = "11111111-2222-4333-8444-555555555504"
		acct    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		org     = "a0000000-0000-4000-8000-00000000000a"
	)
	write := func(id, by string, age time.Duration) {
		p := filepath.Join(home, "projects", "p", id+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(`{"type":"custom-title","customTitle":"same name"}`+"\n"+
			`{"type":"user","sessionId":"`+by+`","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"hi"}}`+"\n"), 0o600)
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
	}
	write(now, now, time.Minute)
	write(copied, now, 2*time.Minute) // a copy of now: its first row is now's
	write(earlier, earlier, time.Hour)
	write(other, other, 2*time.Hour) // only the same name
	list := filepath.Join(data, "claude-code-sessions", acct, org)
	_ = os.MkdirAll(list, 0o700)
	_ = os.WriteFile(filepath.Join(list, "local_x.json"), []byte(`{"sessionId":"local_x","cliSessionId":"`+now+`","priorCliSessionIds":["`+earlier+`"]}`), 0o600)

	get := func(q string) []conversationView {
		t.Helper()
		_, body := r.do("GET", "/api/conversations?"+q, "", nil)
		var out []conversationView
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%s: %v %s", q, err, body)
		}
		return out
	}
	if flat := get("all=1&max=1000"); len(flat) != 4 || slices.ContainsFunc(flat, func(c conversationView) bool { return len(c.Earlier) > 0 }) {
		t.Fatalf("not folded: %+v", flat)
	}
	folded := get("all=1&fold=1&max=1000")
	if len(folded) != 2 || folded[0].SessionID != now || folded[1].SessionID != other || len(folded[1].Earlier) != 0 {
		t.Fatalf("folded: %+v", folded)
	}
	e := folded[0].Earlier
	if len(e) != 2 || e[0].SessionID != copied || e[0].Part != partCopy || e[1].SessionID != earlier || e[1].Part != partEarlier ||
		e[0].Where != "claude-code" || e[1].Title != "same name" {
		t.Fatalf("under %s: %+v", now, e)
	}
}
