package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListTranscripts(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "내 프로젝트")
	other := filepath.Join(root, "other")
	write := func(cfg, sid, cwdIn, body string, age time.Duration) string {
		p := filepath.Join(cfg, "projects", ProjectSlug(cwdIn), sid+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(body), 0o600)
		ts := time.Now().Add(-age)
		_ = os.Chtimes(p, ts, ts)
		return p
	}
	row := func(cwdIn, content string) string {
		return `{"type":"user","cwd":` + strconv.Quote(cwdIn) + `,"message":{"role":"user","content":` + content + `}}` + "\n"
	}
	a := filepath.Join(root, "a")
	home := filepath.Join(root, "home")
	older := write(home, "s-old", cwd, row(cwd, `"<command-name>/clear</command-name>"`)+
		row(cwd, `[{"type":"text","text":"고쳐 줘\n  두 줄"}]`), 3*time.Hour)
	newer := write(a, "s-new", cwd, row(cwd, `"add tests"`), time.Hour)
	write(a, "s-elsewhere", other, row(other, `"not this folder"`), 0)

	got := ListTranscripts([]string{a, home}, cwd, 10)
	if len(got) != 2 || got[0].Path != newer || got[1].Path != older {
		t.Fatalf("got %+v", got)
	}
	if got[0].FirstPrompt != "add tests" || got[1].FirstPrompt != "고쳐 줘 두 줄" || got[1].SessionID != "s-old" {
		t.Fatalf("prompts %q %q", got[0].FirstPrompt, got[1].FirstPrompt)
	}
	long := write(a, "s-long", cwd, row(cwd, strconv.Quote(strings.Repeat("가", 300))), 2*time.Hour)
	for _, tr := range ListTranscripts([]string{a}, cwd, 10) {
		if tr.Path == long && len([]rune(tr.FirstPrompt)) != 121 {
			t.Fatalf("long prompt not cut: %d runes", len([]rune(tr.FirstPrompt)))
		}
	}
	if n := len(ListTranscripts([]string{a, home}, cwd, 1)); n != 1 {
		t.Fatalf("max ignored: %d", n)
	}
}
