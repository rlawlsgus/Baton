package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Transcript describes one stored conversation for a "continue where I left
// off" list. Only the first prompt is read, never the rest of the conversation.
type Transcript struct {
	Path        string    `json:"path"`
	SessionID   string    `json:"session_id"`
	FirstPrompt string    `json:"first_prompt"`
	Modified    time.Time `json:"modified"`
	ConfigDir   string    `json:"config_dir"` // the profile (or ~/.claude) that stores it
}

// ListTranscripts returns the newest conversations that ran in cwd, from all
// the given config folders, newest first.
func ListTranscripts(configDirs []string, cwd string, max int) []Transcript {
	slug := ProjectSlug(cwd)
	var out []Transcript
	for _, dir := range configDirs {
		projects := filepath.Join(dir, "projects")
		// claude names a project folder after the cwd with every character
		// that is not a letter or digit replaced by '-'. That is lossy, so the
		// cwd written in the file decides; the folder name only narrows the scan.
		folders := []string{filepath.Join(projects, slug)}
		if _, err := os.Stat(folders[0]); err != nil {
			folders, _ = filepath.Glob(filepath.Join(projects, "*"))
		}
		for _, folder := range folders {
			files, _ := filepath.Glob(filepath.Join(folder, "*.jsonl"))
			for _, p := range files {
				fi, err := os.Stat(p)
				if err != nil || fi.IsDir() {
					continue
				}
				c, first := transcriptHead(p)
				if c == "" || !strings.EqualFold(filepath.Clean(c), filepath.Clean(cwd)) {
					continue
				}
				out = append(out, Transcript{
					Path:        p,
					SessionID:   strings.TrimSuffix(filepath.Base(p), ".jsonl"),
					FirstPrompt: first,
					Modified:    fi.ModTime(),
					ConfigDir:   dir,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// ProjectSlug is the name claude gives a project folder for cwd.
func ProjectSlug(cwd string) string {
	b := []rune(cwd)
	for i, r := range b {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// transcriptHead reads the cwd and the first real user prompt from the start
// of a transcript (at most 200 rows / 256 KB).
func transcriptHead(path string) (cwd, prompt string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 256<<10))
	sc.Buffer(make([]byte, 64*1024), 256<<10)
	for i := 0; i < 200 && sc.Scan(); i++ {
		var row struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil {
			continue
		}
		if cwd == "" && row.Cwd != "" {
			cwd = row.Cwd
		}
		if prompt == "" && row.Type == "user" && !row.IsMeta && row.Message.Role == "user" {
			prompt = promptText(row.Message.Content)
		}
		if cwd != "" && prompt != "" {
			break
		}
	}
	return cwd, prompt
}

// promptText is the text of a user message, skipping slash-command and tool
// rows (which start with '<' or carry no text).
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				s = p.Text
				break
			}
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || strings.HasPrefix(s, "<") {
		return ""
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

// FirstPrompt is the first thing the user asked in a conversation.
func FirstPrompt(path string) string {
	_, p := transcriptHead(path)
	return p
}
