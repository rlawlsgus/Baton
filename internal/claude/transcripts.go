package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Transcript describes one stored conversation for a "continue where I left
// off" list. Only the start and the end of the file are read, never the
// whole conversation.
type Transcript struct {
	Path        string    `json:"path"`
	SessionID   string    `json:"session_id"`
	FirstPrompt string    `json:"first_prompt"`
	Title       string    `json:"title"`  // the conversation's name (the desktop app names every session), if any
	Cwd         string    `json:"cwd"`    // the project folder it runs in
	Source      string    `json:"source"` // what it was made with: claude-desktop, cli, claude-vscode...
	Modified    time.Time `json:"modified"`
	ConfigDir   string    `json:"config_dir"` // the profile (or ~/.claude) that stores it

	asked bool // something was asked in it: a user or assistant row
}

// ListTranscripts returns the newest conversations that ran in cwd, from all
// the given config folders, newest first.
func ListTranscripts(configDirs []string, cwd string, max int) []Transcript {
	var out []Transcript
	for _, dir := range configDirs {
		for _, p := range transcriptFiles(dir, cwd) {
			fi, err := os.Stat(p)
			if err != nil || fi.IsDir() {
				continue
			}
			t := Describe(p, fi)
			if t.Cwd == "" || !strings.EqualFold(filepath.Clean(t.Cwd), filepath.Clean(cwd)) {
				continue
			}
			t.ConfigDir = dir
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// transcriptFiles are the transcripts in a config folder that can belong to
// cwd. claude names a project folder after the cwd with every character that
// is not a letter or digit replaced by '-'. That is lossy, so the cwd
// written in the file decides; the folder name only narrows the scan.
func transcriptFiles(configDir, cwd string) []string {
	projects := filepath.Join(configDir, "projects")
	slug := ProjectSlug(cwd)
	pattern := filepath.Join(projects, slug, "*.jsonl")
	if len(slug) > slugMax {
		pattern = filepath.Join(projects, slug[:slugMax]+"-*", "*.jsonl")
	}
	if dirs, _ := filepath.Glob(filepath.Dir(pattern)); len(dirs) == 0 {
		pattern = filepath.Join(projects, "*", "*.jsonl")
	}
	m, _ := filepath.Glob(pattern)
	return conversationFiles(m)
}

// conversationFiles leaves out subagent transcripts that older Claude Code
// versions kept next to the conversations (agent-<id>.jsonl).
func conversationFiles(paths []string) []string {
	out := paths[:0]
	for _, p := range paths {
		if !strings.HasPrefix(strings.ToLower(filepath.Base(p)), "agent-") {
			out = append(out, p)
		}
	}
	return out
}

// ListAllTranscripts returns the newest conversations of every folder, from
// all the given config folders, newest first. Only files that get listed are
// read; ones where nothing was asked yet are left out.
func ListAllTranscripts(configDirs []string, max int) []Transcript {
	type file struct {
		path, dir string
		fi        os.FileInfo
	}
	var files []file
	for _, dir := range configDirs {
		m, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "*.jsonl"))
		for _, p := range conversationFiles(m) {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				files = append(files, file{p, dir, fi})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].fi.ModTime().After(files[j].fi.ModTime()) })
	var out []Transcript
	for _, f := range files {
		if max > 0 && len(out) == max {
			break
		}
		t := Describe(f.path, f.fi)
		if t.Cwd == "" || !t.asked && t.Title == "" {
			continue
		}
		t.ConfigDir = f.dir
		out = append(out, t)
	}
	return out
}

// ProjectSlug is the name claude gives a project folder for cwd: every
// UTF-16 unit that is not a letter or digit becomes '-' (two for a character
// outside the BMP, such as an emoji). A name longer than slugMax is cut there
// and gets "-<hash>" appended, which inProject matches by the cut part.
func ProjectSlug(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r > 0xFFFF:
			b.WriteString("--")
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

const slugMax = 200

// described keeps what Describe read, by file size and time: lists are
// shown again and again while most conversations do not change.
var described struct {
	sync.Mutex
	m map[string]describedFile
}

type describedFile struct {
	size int64
	mod  time.Time
	t    Transcript
}

// Describe reads what a conversation list shows about the transcript at path,
// whose info is fi: the first prompt and what made it from the start of the
// file, and its project folder and latest name from the end (the desktop app
// moves a session to another folder and renames it as it goes). ConfigDir is
// left empty.
//
// A row's cwd is where Claude Code's shell was, which follows every `cd` the
// model runs. The project folder is the newest cwd that claude's folder
// for the file is named after; failing that, the one the conversation began in.
func Describe(path string, fi os.FileInfo) Transcript {
	described.Lock()
	e, ok := described.m[path]
	described.Unlock()
	if ok && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
		return e.t
	}
	slug := filepath.Base(filepath.Dir(path))
	h := transcriptHead(path, slug)
	// The desktop app names every session: look further back for the name.
	titleMax := int64(tailTitleMax)
	if h.source == "claude-desktop" {
		titleMax = tailMax
	}
	cwd, title, titled := transcriptTail(path, slug, titleMax)
	if cwd == "" {
		cwd = h.projectCwd
	}
	if cwd == "" {
		cwd = h.cwd
	}
	if !titled {
		title = h.title
	}
	t := Transcript{Path: path, SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), FirstPrompt: h.prompt,
		Title: oneLine(title), Cwd: cwd, Source: h.source, Modified: fi.ModTime(), asked: h.asked}
	described.Lock()
	if described.m == nil || len(described.m) > 4000 {
		described.m = map[string]describedFile{}
	}
	described.m[path] = describedFile{fi.Size(), fi.ModTime(), t}
	described.Unlock()
	return t
}

// row is the part of a transcript row these lists read.
type row struct {
	Type         string `json:"type"`
	Cwd          string `json:"cwd"`
	RelocatedCwd string `json:"relocatedCwd"` // the desktop app moved the session to this folder
	IsMeta       bool   `json:"isMeta"`
	IsSidechain  bool   `json:"isSidechain"`
	Entrypoint   string `json:"entrypoint"`
	CustomTitle  string `json:"customTitle"`
	Message      struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// inProject says whether cwd is the folder claude named slug after.
func inProject(cwd, slug string) bool {
	if cwd == "" {
		return false
	}
	s := ProjectSlug(filepath.Clean(cwd))
	if len(s) > slugMax {
		return len(slug) > slugMax+1 && strings.EqualFold(slug[:slugMax+1], s[:slugMax]+"-")
	}
	return strings.EqualFold(s, slug)
}

type head struct {
	cwd        string // the first cwd
	projectCwd string // the first cwd the project folder is named after
	prompt     string
	source     string
	title      string
	titled     bool
	asked      bool
}

// Limits on what is read from the start of a transcript. Rows can be large
// (a pasted screenshot is stored inline), so a long row is read too, up to
// headRowMax, and skipped beyond that.
const (
	headRows   = 200
	headBytes  = 16 << 20
	headRowMax = 8 << 20
)

// transcriptHead reads the cwd, what made the conversation and the first
// real user prompt from the start of a transcript.
func transcriptHead(path, slug string) head {
	var h head
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	br := bufio.NewReaderSize(io.LimitReader(f, headBytes), 64<<10)
	for i := 0; i < headRows; i++ {
		line, err := readRow(br, headRowMax)
		if len(line) > 0 {
			var r row
			if json.Unmarshal(line, &r) == nil {
				if h.cwd == "" && r.Cwd != "" {
					h.cwd = r.Cwd
				}
				if h.projectCwd == "" && inProject(r.Cwd, slug) {
					h.projectCwd = r.Cwd
				}
				if h.source == "" && r.Entrypoint != "" {
					h.source = r.Entrypoint
				}
				if r.Type == "custom-title" {
					h.title, h.titled = r.CustomTitle, true
				}
				if (r.Type == "user" || r.Type == "assistant") && !r.IsMeta && !r.IsSidechain {
					h.asked = true
				}
				if h.prompt == "" && r.Type == "user" && !r.IsMeta && r.Message.Role == "user" {
					h.prompt = promptText(r.Message.Content)
				}
			}
		}
		// A desktop session's first name comes a few rows later: keep it as
		// the fallback for a name the end of the file does not have.
		if err != nil || h.prompt != "" && (h.projectCwd != "" || slug == "") && (h.titled || h.source != "claude-desktop") {
			break
		}
	}
	return h
}

// readRow reads one line; a line longer than max comes back empty (the rest
// of it is skipped).
func readRow(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		part, err := br.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(part) > max {
				tooLong, line = true, nil
			} else {
				line = append(line, part...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimSpace(line), err
	}
}

// Limits on what is read backwards from the end of a transcript: the newest
// project folder is looked for in up to tailMax bytes, the newest name in up
// to tailTitleMax, or tailMax for a desktop session (the desktop app writes
// the name again every few turns, but a long turn can push it far back).
const (
	tailChunk    = 128 << 10
	tailTitleMax = 1 << 20
	tailMax      = 4 << 20
)

// transcriptTail reads the newest project folder and name from the end of a
// transcript, going backwards until it has both or reaches its limits.
// titled says a name row was found (its name can be empty).
func transcriptTail(path, slug string, titleMax int64) (cwd, title string, titled bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", "", false
	}
	end := fi.Size()
	var carry []byte // the start of a row cut by the previous chunk
	for read := int64(0); end > 0 && (cwd == "" || !titled); {
		if read >= tailMax || cwd != "" && read >= titleMax {
			break
		}
		off := max(0, end-tailChunk)
		buf := make([]byte, end-off, end-off+int64(len(carry)))
		if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			break
		}
		read += end - off
		buf = append(buf, carry...)
		lines := bytes.Split(buf, []byte("\n"))
		carry = nil
		if off > 0 {
			carry = append([]byte(nil), lines[0]...) // completed by the next chunk
			lines = lines[1:]
		}
		for i := len(lines) - 1; i >= 0 && (cwd == "" || !titled); i-- {
			l := lines[i]
			if !bytes.Contains(l, []byte(`"cwd"`)) && !bytes.Contains(l, []byte(`"custom-title"`)) && !bytes.Contains(l, []byte(`"relocatedCwd"`)) {
				continue
			}
			var r row
			if json.Unmarshal(l, &r) != nil {
				continue
			}
			if !titled && r.Type == "custom-title" {
				title, titled = r.CustomTitle, true
			}
			if cwd == "" && inProject(r.RelocatedCwd, slug) {
				cwd = r.RelocatedCwd
			}
			if cwd == "" && inProject(r.Cwd, slug) {
				cwd = r.Cwd
			}
		}
		if len(carry) > headRowMax {
			carry = nil // a row this long is skipped
		}
		end = off
	}
	return cwd, title, titled
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
	s = oneLine(s)
	if strings.HasPrefix(s, "<") {
		return ""
	}
	return s
}

// oneLine puts text on one line of at most 120 characters.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
