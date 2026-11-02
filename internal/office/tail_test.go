package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func collect(t *testing.T, tl *tailer) ([]string, bool) {
	t.Helper()
	var got []string
	shrunk, err := tl.next(func(line []byte) { got = append(got, string(line)) })
	if err != nil {
		t.Fatal(err)
	}
	return got, shrunk
}

// The tailer hands over whole rows only, as they are appended; skips the
// rows the office never looks at and rows too long to keep; and starts
// again near the end of a file that got shorter.
func TestTailer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, p, `{"a":1}`+"\n"+`{"type":"api-request-blob","message":"big"}`+"\n"+`{"b":`)
	tl := newTailer(p, 0)
	got, _ := collect(t, tl)
	if strings.Join(got, "|") != `{"a":1}` {
		t.Fatalf("first read %q", got)
	}
	// The rest of the row arrives, with more.
	appendTo(t, p, "2}\r\n"+`{"parentUuid":null,"type":"attachment","attachment":{"type":"date"}}`+"\n"+
		`{"parentUuid":null,"type":"attachment","attachment":{"type":"queued_command","commandMode":"task-notification"}}`+"\n")
	got, _ = collect(t, tl)
	if len(got) != 2 || got[0] != `{"b":2}` || !strings.Contains(got[1], "task-notification") {
		t.Fatalf("second read %q", got)
	}
	if got, _ = collect(t, tl); len(got) != 0 {
		t.Fatalf("nothing new, got %q", got)
	}

	// A row longer than rowMax is skipped, even when it arrives in parts.
	appendTo(t, p, `{"big":"`+strings.Repeat("x", rowMax/2))
	if got, _ = collect(t, tl); len(got) != 0 {
		t.Fatalf("partial big row %q", got)
	}
	appendTo(t, p, strings.Repeat("y", rowMax/2+10)+`"}`+"\n"+`{"c":3}`+"\n")
	if got, _ = collect(t, tl); len(got) != 1 || got[0] != `{"c":3}` {
		t.Fatalf("after a big row: %d rows", len(got))
	}
	if len(tl.partial) != 0 || tl.skip {
		t.Fatalf("left over: %d %v", len(tl.partial), tl.skip)
	}

	// The file is rewritten shorter: reading starts again from its end.
	if err := os.WriteFile(p, []byte(`{"d":4}`+"\n"+`{"e":5}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, shrunk := collect(t, tl)
	if !shrunk || strings.Join(got, "|") != `{"d":4}|{"e":5}` {
		t.Fatalf("after shrinking: %v %q", shrunk, got)
	}

	// Starting in the middle of a file drops the row it lands in, but not
	// a row that starts right there.
	data, _ := os.ReadFile(p)
	got, _ = collect(t, newTailer(p, 3))
	if strings.Join(got, "|") != `{"e":5}` {
		t.Fatalf("from the middle: %q", got)
	}
	got, _ = collect(t, newTailer(p, int64(strings.Index(string(data), `{"e"`))))
	if strings.Join(got, "|") != `{"e":5}` {
		t.Fatalf("from a row's start: %q", got)
	}
	if tailFrom(10, 4) != 6 || tailFrom(3, 4) != 0 {
		t.Fatal("tailFrom")
	}
}

// The tailer does not stop Claude Code from deleting or renaming the file.
func TestTailerSharesDelete(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, p, `{"a":1}`+"\n")
	f, err := openShared(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(p, p+".moved"); err != nil {
		t.Fatalf("rename while open: %v", err)
	}
	if err := os.Remove(p + ".moved"); err != nil {
		t.Fatalf("delete while open: %v", err)
	}
}

// Floors are numbered once and kept: on the first run by age (oldest 1F),
// later ones on top.
func TestFloorsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "office.json")
	f := loadFloors(path)
	if f.begun {
		t.Fatal("begun without a file")
	}
	now := time.Now()
	if !f.number(map[string]time.Time{`c:\new`: now, `c:\old`: now.Add(-48 * time.Hour), `c:\mid`: now.Add(-time.Hour)}) {
		t.Fatal("nothing numbered")
	}
	if f.of(`c:\old`) != 1 || f.of(`c:\mid`) != 2 || f.of(`c:\new`) != 3 {
		t.Fatalf("first numbering %v", f.m)
	}
	if err := f.save(); err != nil {
		t.Fatal(err)
	}
	g := loadFloors(path)
	if !g.begun || g.of(`c:\old`) != 1 || g.of(`c:\new`) != 3 {
		t.Fatalf("reloaded %v", g.m)
	}
	// A project older than all of them, seen later, still goes on top.
	if g.number(map[string]time.Time{`c:\old`: now, `c:\ancient`: now.Add(-1000 * time.Hour)}) != true || g.of(`c:\ancient`) != 4 || g.of(`c:\old`) != 1 {
		t.Fatalf("later floor %v", g.m)
	}
	if g.number(map[string]time.Time{`c:\old`: now}) {
		t.Fatal("renumbered a known floor")
	}
	// A broken file is not a first run.
	_ = os.WriteFile(path, []byte("{nope"), 0o600)
	if b := loadFloors(path); !b.begun || len(b.m) != 0 {
		t.Fatalf("broken file: %+v", b)
	}
	if projectKey(`C:\Work\Proj\`) != projectKey(`c:\work\proj`) || folderName(`C:\Work\Proj\`) != "Proj" {
		t.Fatal("project key")
	}
}
