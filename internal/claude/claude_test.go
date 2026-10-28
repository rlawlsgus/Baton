package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	a, err := SplitArgs([]string{"--model", "opus", "--add-dir", "x", "y", "--permission-mode=plan", "fix the bug", "-n", "work"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--model", "opus", "--add-dir", "x", "y", "-n", "work"}; !reflect.DeepEqual(a.Persistent, want) {
		t.Errorf("Persistent = %q, want %q", a.Persistent, want)
	}
	// --permission-mode is start-only: a resumed session restores its last mode.
	if want := []string{"--permission-mode", "plan", "fix the bug"}; !reflect.DeepEqual(a.FirstOnly, want) || a.Resumes {
		t.Errorf("FirstOnly = %q resumes=%v", a.FirstOnly, a.Resumes)
	}
	if want := []string{"--model", "opus", "--add-dir", "x", "y", "--permission-mode", "plan", "fix the bug", "-n", "work"}; !reflect.DeepEqual(a.Original, want) {
		t.Errorf("Original = %q, want the user's order %q", a.Original, want)
	}

	a, _ = SplitArgs([]string{"-r", "abc", "--model", "sonnet"})
	if !a.Resumes || !reflect.DeepEqual(a.FirstOnly, []string{"-r", "abc"}) || !reflect.DeepEqual(a.Persistent, []string{"--model", "sonnet"}) {
		t.Errorf("resume split wrong: %+v", a)
	}
	a, _ = SplitArgs([]string{"-c"})
	if !a.Resumes {
		t.Error("-c must count as resume")
	}
	a, _ = SplitArgs([]string{"--session-id", "u-1"})
	if a.Resumes || !reflect.DeepEqual(a.FirstOnly, []string{"--session-id", "u-1"}) {
		t.Errorf("--session-id split wrong: %+v", a)
	}
	for _, bad := range [][]string{{"-p", "hi"}, {"--print"}, {"--settings", "x.json"}, {"--bare"}, {"--safe-mode"}, {"--model"}, {"--worktree"}} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) should fail", bad)
		}
	}
}

func TestTrustKeyAndParents(t *testing.T) {
	if got := TrustKey(`C:\Users\me\proj`); got != "C:/Users/me/proj" {
		t.Fatal(got)
	}
	var chain []string
	for k := "C:/Users/me/proj"; k != ""; k = parentKey(k) {
		chain = append(chain, k)
	}
	if want := []string{"C:/Users/me/proj", "C:/Users/me", "C:/Users", "C:/"}; !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain = %q", chain)
	}
}

func TestSeedProfilePreservesKeysAndTrustsExactCwd(t *testing.T) {
	dir := t.TempDir()
	orig := `{"firstStartTime": 1761450000123, "userID": "abc", "projects": {"D:/other": {"hasTrustDialogAccepted": true, "allowedTools": ["x"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := `C:\Users\홍길동\내 프로젝트`
	res, err := SeedProfile(dir, cwd, true, true)
	if err != nil || !res.Onboarding || !res.Trust {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if !strings.Contains(string(data), "1761450000123") {
		t.Errorf("large integer was rewritten: %s", data)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	projects := m["projects"].(map[string]any)
	if _, ok := projects["D:/other"]; !ok {
		t.Error("existing project entry lost")
	}
	e, ok := projects["C:/Users/홍길동/내 프로젝트"].(map[string]any)
	if !ok || e["hasTrustDialogAccepted"] != true {
		t.Errorf("cwd not trusted with the exact key: %v", projects)
	}
	if m["userID"] != "abc" || m["hasCompletedOnboarding"] != true {
		t.Errorf("keys wrong: %v", m)
	}
	if !IsTrusted(dir, cwd) || !IsTrusted(dir, cwd+`\sub`) {
		t.Error("IsTrusted should accept the folder and its children")
	}
	if IsTrusted(dir, `c:\Users\홍길동\내 프로젝트`) {
		t.Error("trust is case-sensitive in claude; a different drive case must not match")
	}
	// Second call is a no-op.
	if res, _ := SeedProfile(dir, cwd, true, true); res.Onboarding || res.Trust {
		t.Errorf("second seed changed something: %+v", res)
	}
}

func TestSeedProfileFlags(t *testing.T) {
	dir := t.TempDir()
	res, err := SeedProfile(dir, `C:\p`, false, true)
	if err != nil || res.Onboarding || !res.Trust {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	m, _ := readClaudeJSON(dir)
	if _, ok := m["hasCompletedOnboarding"]; ok {
		t.Error("onboarding=false must leave onboarding alone")
	}
	dir2 := t.TempDir()
	if _, err := SeedProfile(dir2, `C:\p`, true, false); err != nil {
		t.Fatal(err)
	}
	if IsTrusted(dir2, `C:\p`) {
		t.Error("trust=false must not trust")
	}
}

func TestRunSettingsExecFormHooks(t *testing.T) {
	s := RunSettings(`C:\Program Files\baton\baton.exe`, `C:\data\runs\r1`, nil)
	if s["autoContinueAtUsageLimit"] != false {
		t.Error("autoContinueAtUsageLimit must be false")
	}
	if _, ok := s["statusLine"]; ok {
		t.Error("must not set a statusLine (it would replace the user's)")
	}
	hooks := s["hooks"].(map[string]any)
	for _, ev := range HookEvents {
		entry := hooks[ev].([]any)[0].(map[string]any)
		if _, ok := entry["matcher"]; ok {
			t.Errorf("%s: no matcher expected", ev)
		}
		h := entry["hooks"].([]any)[0].(map[string]any)
		if h["command"] != `C:\Program Files\baton\baton.exe` {
			t.Errorf("%s command = %v", ev, h["command"])
		}
		args := h["args"].([]string)
		if !reflect.DeepEqual(args, []string{"hook", ev, "--run-dir", `C:\data\runs\r1`}) {
			t.Errorf("%s args = %q", ev, args)
		}
	}
}

func TestParseAuthStatus(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"loggedIn":true,"authMethod":"claude.ai","email":"me@example.com","orgName":"Me","subscriptionType":"max","configDirectory":"C:\\p"}`), &raw)
	s := parseAuthStatus(raw)
	if !s.LoggedIn || s.Email != "me@example.com" || s.Subscription != "max" || s.OrgName != "Me" || s.AuthMethod != "claude.ai" {
		t.Fatalf("%+v", s)
	}
	_ = json.Unmarshal([]byte(`{"loggedIn":false,"authMethod":"none"}`), &raw)
	if s := parseAuthStatus(raw); s.LoggedIn {
		t.Fatal("logged out profile parsed as logged in")
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("2.1.308", "2.1.268") <= 0 || CompareVersions("2.1.9", "2.1.10") >= 0 || CompareVersions("2.1.0", "2.1.0") != 0 {
		t.Fatal("version compare broken")
	}
	if _, err := CheckVersion("2.1.100"); err == nil {
		t.Error("too-old version accepted")
	}
	if w, err := CheckVersion("9.0.0"); err != nil || w == "" {
		t.Error("newer-than-tested version should warn, not fail")
	}
}

func TestFindTranscriptAcrossProfiles(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	p := filepath.Join(a, "projects", "C--proj", "sid-1.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("{}\n"), 0o600)
	if got := FindTranscript([]string{b, a}, "sid-1"); got != p {
		t.Fatalf("got %q want %q", got, p)
	}
	if got := FindTranscript([]string{b}, "sid-1"); got != "" {
		t.Fatalf("got %q", got)
	}
}
