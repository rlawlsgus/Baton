// Package gitinfo reads what changed in a session's folder, the way the
// desktop app's changes panel shows it: the files git sees as changed or new,
// with lines added and removed against the last commit.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// File is one changed file.
type File struct {
	Path    string `json:"path"`
	Status  string `json:"status"`  // modified | added | deleted | renamed | untracked | conflict
	Added   int    `json:"added"`   // lines; -1 when unknown (a binary file)
	Removed int    `json:"removed"` // lines; -1 when unknown
}

// Changes is a folder's uncommitted work.
type Changes struct {
	Repo    bool   `json:"repo"` // the folder is in a git repository
	Files   []File `json:"files"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// ErrNoGit: git is not installed (or not on PATH).
var ErrNoGit = errors.New("git not found")

// Read lists the changes in dir. It runs git without a window (the baton
// window has no console) and gives up after timeout.
func Read(dir string, timeout time.Duration) (Changes, error) {
	exe, err := exec.LookPath("git")
	if err != nil {
		return Changes{}, ErrNoGit
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, exe, append([]string{"-C", dir, "-c", "core.quotepath=false"}, args...)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return nil, errors.New(msg)
		}
		return out, nil
	}
	status, err := run("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return Changes{Repo: false, Files: []File{}}, nil
		}
		return Changes{}, err
	}
	c := Changes{Repo: true, Files: parseStatus(status)}
	// Lines against the last commit (staged and unstaged together); a
	// repository without commits yet has none to compare with.
	if numstat, err := run("diff", "HEAD", "--numstat", "-z"); err == nil {
		lines := parseNumstat(numstat)
		for i, f := range c.Files {
			if n, ok := lines[f.Path]; ok {
				c.Files[i].Added, c.Files[i].Removed = n[0], n[1]
			}
		}
	}
	// A new file's lines are all added (git counts them only once staged).
	if top, err := run("rev-parse", "--show-toplevel"); err == nil {
		root := strings.TrimSpace(string(top))
		for i, f := range c.Files {
			if f.Status == "untracked" {
				if n, ok := countLines(filepath.Join(root, filepath.FromSlash(f.Path))); ok {
					c.Files[i].Added, c.Files[i].Removed = n, 0
				}
			}
		}
	}
	for _, f := range c.Files {
		c.Added += max(0, f.Added)
		c.Removed += max(0, f.Removed)
	}
	sort.Slice(c.Files, func(i, j int) bool { return c.Files[i].Path < c.Files[j].Path })
	return c, nil
}

// countLines counts the lines of a text file of up to 1 MB (a binary or
// bigger file is left uncounted, as git does with binary files).
func countLines(path string) (int, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return 0, false
	}
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, true
}

// parseStatus reads `git status --porcelain=v1 -z`: "XY path\0", with the
// old path in a second field for a rename or copy.
func parseStatus(out []byte) []File {
	files := []File{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if len(e) < 4 {
			continue
		}
		xy, path := e[:2], e[3:]
		f := File{Path: path, Added: -1, Removed: -1}
		switch {
		case xy == "??":
			f.Status = "untracked"
		case xy[0] == 'U' || xy[1] == 'U' || xy == "AA" || xy == "DD":
			f.Status = "conflict"
		case xy[0] == 'R' || xy[0] == 'C':
			f.Status = "renamed"
			i++ // the old path
		case xy[0] == 'A':
			f.Status = "added"
		case xy[0] == 'D' || xy[1] == 'D':
			f.Status = "deleted"
		default:
			f.Status = "modified"
		}
		files = append(files, f)
	}
	return files
}

// parseNumstat reads `git diff --numstat -z`: "added\tremoved\tpath\0", or
// for a rename "added\tremoved\t\0old\0new\0". Binary files show "-".
func parseNumstat(out []byte) map[string][2]int {
	res := map[string][2]int{}
	fields := strings.Split(string(out), "\x00")
	num := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			return -1
		}
		return n
	}
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) { // rename: old, then new
			path = fields[i+2]
			i += 2
		}
		res[path] = [2]int{num(parts[0]), num(parts[1])}
	}
	return res
}
