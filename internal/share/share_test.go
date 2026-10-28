package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"baton/internal/fsx"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSyncLinksCopiesAndRespectsLocalEdits(t *testing.T) {
	home, profile := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "profile")
	write(t, filepath.Join(home, "skills", "s1", "SKILL.md"), "skill")
	write(t, filepath.Join(home, "CLAUDE.md"), "v1")
	write(t, filepath.Join(home, "settings.json"),
		`{"model":"opus","apiKeyHelper":"echo k","env":{"ANTHROPIC_API_KEY":"x","MY_VAR":"1"},"statusLine":{"type":"command","command":"sl"}}`)
	_ = os.MkdirAll(profile, 0o700)

	res, err := Sync(home, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Linked, []string{"skills"}) || !slices.Contains(res.Copied, "CLAUDE.md") || !slices.Contains(res.Copied, "settings.json") {
		t.Fatalf("res = %+v", res)
	}
	if fsx.JunctionTarget(filepath.Join(profile, "skills")) == "" {
		t.Fatal("skills is not a junction")
	}
	if read(t, filepath.Join(profile, "skills", "s1", "SKILL.md")) != "skill" {
		t.Fatal("junction does not resolve")
	}
	var s map[string]any
	_ = json.Unmarshal([]byte(read(t, filepath.Join(profile, "settings.json"))), &s)
	if _, ok := s["apiKeyHelper"]; ok {
		t.Error("apiKeyHelper must not be shared")
	}
	env := s["env"].(map[string]any)
	if _, ok := env["ANTHROPIC_API_KEY"]; ok || env["MY_VAR"] != "1" || s["model"] != "opus" || s["statusLine"] == nil {
		t.Errorf("settings not sanitized correctly: %v", s)
	}

	// Home changes propagate while the profile copy is untouched...
	write(t, filepath.Join(home, "CLAUDE.md"), "v2")
	if _, err := Sync(home, profile); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(profile, "CLAUDE.md")) != "v2" {
		t.Fatal("update not propagated")
	}
	// ...but never over an edit made inside the profile.
	write(t, filepath.Join(profile, "CLAUDE.md"), "profile edit")
	write(t, filepath.Join(home, "CLAUDE.md"), "v3")
	res, _ = Sync(home, profile)
	if read(t, filepath.Join(profile, "CLAUDE.md")) != "profile edit" || !slices.Contains(res.Kept, "CLAUDE.md") {
		t.Fatalf("profile edit overwritten (res=%+v)", res)
	}
}

func TestSyncKeepsExistingProfileFilesAndDirs(t *testing.T) {
	home, profile := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "profile")
	write(t, filepath.Join(home, "settings.json"), `{"model":"opus"}`)
	write(t, filepath.Join(home, "agents", "a.md"), "a")
	write(t, filepath.Join(profile, "settings.json"), `{"apiKeyHelper":"echo mine"}`)
	write(t, filepath.Join(profile, "agents", "own.md"), "own")
	res, err := Sync(home, profile)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(profile, "settings.json")) != `{"apiKeyHelper":"echo mine"}` {
		t.Fatal("pre-existing profile settings overwritten")
	}
	if _, err := os.Stat(filepath.Join(profile, "agents", "own.md")); err != nil || !slices.Contains(res.Kept, "agents") {
		t.Fatalf("own agents dir replaced (res=%+v)", res)
	}
}

func TestSyncNoHome(t *testing.T) {
	profile := t.TempDir()
	if res, err := Sync(filepath.Join(profile, "missing"), profile); err != nil || len(res.Copied)+len(res.Linked) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if res, _ := Sync(profile, profile); len(res.Copied) != 0 {
		t.Fatal("syncing a profile onto itself must be a no-op")
	}
}
