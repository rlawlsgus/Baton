package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A conversation baton shows in the Claude desktop app has a second name in
// the user's own setup (a hard link to the file in the account's profile).
// Every list shows it once, under the folder listed first (the profile), a
// lookup finds that name, and the other one when it is all that is left. A
// copy under the same name is another conversation and is listed too.
func TestSecondNameListedOnce(t *testing.T) {
	root := t.TempDir()
	prof, home, proj := filepath.Join(root, "a"), filepath.Join(root, "home"), filepath.Join(root, "Proj")
	const sid, other = "11111111-2222-4333-8444-000000000001", "11111111-2222-4333-8444-000000000002"
	write := func(cfg, id string) string {
		p := filepath.Join(cfg, "projects", ProjectSlug(proj), id+".jsonl")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"message":{"role":"user","content":"hi `+id+`"}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	file := write(prof, sid)
	second := filepath.Join(home, "projects", ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(second), 0o700)
	if err := os.Link(file, second); err != nil {
		t.Skip("no hard links here:", err)
	}
	write(home, other)
	dirs := []string{prof, home}
	where := func(list []Transcript) map[string]string {
		m := map[string]string{}
		for _, x := range list {
			if _, dup := m[x.SessionID]; dup {
				t.Fatalf("%s listed twice: %+v", x.SessionID, list)
			}
			m[x.SessionID] = x.ConfigDir
		}
		return m
	}
	for name, list := range map[string][]Transcript{"ListTranscripts": ListTranscripts(dirs, proj, 0), "ListAllTranscripts": ListAllTranscripts(dirs, 0)} {
		if m := where(list); len(m) != 2 || m[sid] != prof || m[other] != home {
			t.Fatalf("%s: %v", name, m)
		}
	}
	if got := FindTranscript(dirs, sid); got != file {
		t.Fatalf("FindTranscript = %s, want the profile's name", got)
	}
	if !HasTranscripts([]string{home}, proj) {
		t.Fatal("HasTranscripts")
	}

	// A copy under the same id is another file: both are listed.
	copied := filepath.Join(home, "projects", "elsewhere", sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(copied), 0o700)
	b, _ := os.ReadFile(file)
	if err := os.WriteFile(copied, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := len(ListAllTranscripts(dirs, 0)); n != 3 {
		t.Fatalf("a copy was taken for the same file: %d listed", n)
	}
	if err := os.Remove(copied); err != nil {
		t.Fatal(err)
	}

	// Claude Code's cleanup took the profile's name: the other one stands in.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if got := FindTranscript(dirs, sid); got != second {
		t.Fatalf("FindTranscript = %s, want the second name", got)
	}
	if m := where(ListAllTranscripts(dirs, 0)); m[sid] != home {
		t.Fatalf("after the cleanup: %v", m)
	}
}

func TestOAuthIdentity(t *testing.T) {
	dir := t.TempDir()
	if a, o, e := OAuthIdentity(dir); a != "" || o != "" || e != "" {
		t.Fatal("no .claude.json")
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte("\xef\xbb\xbf"+`{"oauthAccount":{"accountUuid":"AAAA-1","organizationUuid":" Org-2 ","emailAddress":"Me@Example.com","displayName":"x"},"projects":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if a, o, e := OAuthIdentity(dir); a != "aaaa-1" || o != "org-2" || e != "Me@Example.com" {
		t.Fatalf("%q %q %q", a, o, e)
	}
}
