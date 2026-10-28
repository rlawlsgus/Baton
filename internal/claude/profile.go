package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"baton/internal/fsx"
)

// TrustKey is how claude keys a folder in .claude.json "projects": the cwd
// string exactly as the process got it, with '\' turned into '/'. Drive-letter
// and path case matter; a trailing slash or an 8.3 short path does not match.
func TrustKey(cwd string) string {
	return strings.ReplaceAll(cwd, `\`, "/")
}

func claudeJSON(profileDir string) string { return filepath.Join(profileDir, ".claude.json") }

func readClaudeJSON(profileDir string) (map[string]any, error) {
	data, err := os.ReadFile(claudeJSON(profileDir))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(fsx.StripBOM(data)))
	dec.UseNumber() // keep large integers (timestamps) byte-exact
	m := map[string]any{}
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// TrustKeys lists the .claude.json keys claude accepts as trusting cwd,
// nearest first. Inside a git repository claude only looks from cwd up to the
// repository root (and, for a worktree, the main repository's root); outside
// one it looks all the way up to the drive root.
func TrustKeys(cwd string) []string {
	root, mainRoot := gitRoots(cwd)
	var keys []string
	stop := ""
	if root != "" {
		stop = TrustKey(root)
	}
	for key := TrustKey(cwd); key != ""; key = parentKey(key) {
		keys = append(keys, key)
		if stop != "" && key == stop {
			break
		}
	}
	if mainRoot != "" {
		keys = append(keys, TrustKey(mainRoot))
	}
	return keys
}

// gitRoots finds the repository containing dir: the folder holding ".git",
// and when ".git" is a file (a linked worktree) the main repository's root.
// Paths keep dir's own spelling so they compare with claude's keys.
func gitRoots(dir string) (root, mainRoot string) {
	d := filepath.Clean(dir)
	for {
		fi, err := os.Lstat(filepath.Join(d, ".git"))
		if err == nil {
			if !fi.IsDir() {
				mainRoot = mainWorktreeRoot(filepath.Join(d, ".git"))
			}
			return d, mainRoot
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ""
		}
		d = parent
	}
}

func mainWorktreeRoot(gitFile string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	gitdir, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	gitdir = filepath.Clean(strings.TrimSpace(gitdir))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(gitFile), gitdir)
	}
	// <main>\.git\worktrees\<name>
	sep := string(filepath.Separator)
	if i := strings.LastIndex(strings.ToLower(gitdir), strings.ToLower(sep+".git"+sep+"worktrees"+sep)); i > 0 {
		return gitdir[:i]
	}
	return ""
}

// IsTrusted reports whether claude would start in cwd under this profile
// without asking "Do you trust this folder?".
func IsTrusted(profileDir, cwd string) bool {
	m, err := readClaudeJSON(profileDir)
	if err != nil {
		return false
	}
	projects, _ := m["projects"].(map[string]any)
	for _, key := range TrustKeys(cwd) {
		if p, ok := projects[key].(map[string]any); ok {
			if v, _ := p["hasTrustDialogAccepted"].(bool); v {
				return true
			}
		}
	}
	return false
}

func parentKey(key string) string {
	k := strings.TrimSuffix(key, "/")
	i := strings.LastIndex(k, "/")
	if i < 0 {
		return ""
	}
	if i == 2 && k[1] == ':' { // "C:/x" -> "C:/"
		if len(k) == 3 {
			return ""
		}
		return k[:3]
	}
	return k[:i]
}

// SeedResult says what SeedProfile changed.
type SeedResult struct {
	Onboarding bool
	Trust      bool
}

// SeedProfile makes a profile launchable without first-run dialogs in cwd:
// it can mark onboarding complete and trust cwd. Without this an unattended
// relaunch stops on the trust dialog, whose default answer exits claude. Call
// it only while no claude session uses the profile; every other key is kept,
// and claude's own config lock is held while writing.
func SeedProfile(profileDir, cwd string, onboarding, trust bool) (SeedResult, error) {
	var res SeedResult
	unlock, err := lockClaudeConfig(profileDir)
	if err != nil {
		return res, err
	}
	defer unlock()
	m, err := readClaudeJSON(profileDir)
	if err != nil {
		return res, err
	}
	if v, _ := m["hasCompletedOnboarding"].(bool); onboarding && !v {
		m["hasCompletedOnboarding"] = true
		res.Onboarding = true
	}
	if trust && !IsTrusted(profileDir, cwd) {
		projects, _ := m["projects"].(map[string]any)
		if projects == nil {
			projects = map[string]any{}
		}
		key := TrustKey(cwd)
		entry, _ := projects[key].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		entry["hasTrustDialogAccepted"] = true
		projects[key] = entry
		m["projects"] = projects
		res.Trust = true
	}
	if !res.Onboarding && !res.Trust {
		return res, nil
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return res, err
	}
	return res, fsx.WriteFileAtomic(claudeJSON(profileDir), append(data, '\n'), 0o600)
}

// lockClaudeConfig takes the lock claude itself uses for .claude.json (a
// "<file>.lock" directory, proper-lockfile style; stale after 10 s).
func lockClaudeConfig(profileDir string) (func(), error) {
	lock := claudeJSON(profileDir) + ".lock"
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			return func() { os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(lock); serr == nil && time.Since(fi.ModTime()) > 10*time.Second {
			os.Remove(lock) // left behind by a crashed writer
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("claude is writing " + claudeJSON(profileDir) + "; try again")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ProfileHasTranscripts reports whether conversation data lives in this
// profile: transcripts (a conversation stays in the profile that started it,
// even after other accounts continue it) or per-session folders such as saved
// tool outputs and subagent logs that transcripts elsewhere point to.
func ProfileHasTranscripts(profileDir string) bool {
	if m, _ := filepath.Glob(filepath.Join(profileDir, "projects", "*", "*.jsonl")); len(m) > 0 {
		return true
	}
	for _, sub := range []string{"tool-results", "subagents"} {
		if m, _ := filepath.Glob(filepath.Join(profileDir, "projects", "*", "*", sub)); len(m) > 0 {
			return true
		}
	}
	return false
}
