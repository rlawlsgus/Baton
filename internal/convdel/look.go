package convdel

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"baton/internal/claude"
	"baton/internal/desktop"
	"baton/internal/fsx"
	"baton/internal/run"
)

// What a conversation leaves on disk, in the order it goes to the Recycle
// Bin.
const (
	kindOwnName = iota // a name of its file outside the profiles: the user's own folder
	kindName           // a name of its file in an account's profile
	kindSibling        // projects\*\<id>.*: what Claude Code keeps next to the file
	kindSession        // projects\*\<id>\: subagents, saved tool outputs...
	kindRoot           // file-history\<id>, session-env\<id>, tasks\<id>, debug\<id>.txt
	kindTemp           // <temp>\claude\*\<id>\: scratchpads, background output
	kindRun            // baton's runs of it that ended: their events hold its text
)

type item struct {
	path string
	kind int
	dir  bool
}

// name is a name of the conversation's file: <dir>\projects\*\<id>.jsonl.
type name struct {
	path string
	dir  string // its config folder
	fi   os.FileInfo
}

type found struct {
	names []name
	items []item
	links int // links and junctions where an item would be, left alone
	// hidden: a name of the file is where the scan can't see it (see
	// hiddenNames).
	hidden bool
	// unseen are the names hiddenNames found through a link: what the plan
	// says the conversation is when the scan saw no name of it.
	unseen []name
}

// look makes the plan. own: this delete holds the deleting marker itself.
func (j *job) look(own bool) (Plan, error) {
	if j.Desk != nil {
		// What became safe to take off goes first (see Look); a failure here
		// shows in Forget below.
		_ = j.Desk.Reconcile()
	}
	runs, unread := readRuns(j.Paths.RunsDir())
	var f found
	for _, d := range j.dirs {
		j.scanProjects(d, &f)
		j.scanRoot(d, &f)
	}
	for _, d := range j.dirs {
		j.hiddenNames(d, &f)
	}
	j.scanTemp(&f)
	var bs []Blocker
	for _, r := range runs {
		switch {
		case !strings.EqualFold(r.j.SessionID, j.id) || j.active(r.j):
		case j.alive(r.j.SupervisorPID, r.j.SupervisorCreated):
			// It ended, but its baton lives on: its window shows the last of
			// the session until the user closes it, and holds its log in the
			// run's folder, which would stop the delete half way.
			bs = append(bs, Blocker{Code: CodeSessionWindow, RunID: runID(r)})
		default:
			f.items = append(f.items, item{path: r.dir, kind: kindRun, dir: true})
		}
	}
	lingers := len(bs) > 0 // a run of it is still there, to go once its window closes

	p := Plan{SessionID: j.id, MadeIn: "outside", Blockers: []Blocker{}, DesktopOff: []string{}, Notes: []Note{}}
	var items []item
	if sel := j.selected(f.names); sel != nil {
		t := claude.Describe(sel.path, sel.fi)
		p.Title, p.Cwd, p.Source = t.Name(), t.Cwd, t.Source
		// Delete tells by it whether the file is still there afterwards,
		// under whatever name.
		p.file, p.known = fsx.FileID(sel.path)
		var home, prof []item
		for _, n := range f.names {
			if !os.SameFile(n.fi, sel.fi) {
				// Another file by its name: Claude Code may resume either, and
				// the folders kept by id belong to both.
				bs = append(bs, Blocker{Code: CodeTwoFiles})
				continue
			}
			p.group = append(p.group, n.path)
			if fsx.IsWithin(n.path, j.Paths.ProfilesDir()) {
				p.MadeIn = "baton"
				prof = append(prof, item{path: n.path, kind: kindName})
			} else {
				home = append(home, item{path: n.path, kind: kindOwnName})
			}
		}
		// A name outside Claude Code's folders would keep the file alive. A
		// name in the Recycle Bin does not: an earlier delete that stopped
		// half way (or the user) put it there, and it stays a hard link of
		// the file.
		switch n := int(fsx.LinkCount(sel.path)); {
		case n == 0:
			bs = append(bs, Blocker{Code: CodeCannotCheck})
		case n > len(p.group) && n-j.bin.Holds(sel.path) > len(p.group):
			bs = append(bs, Blocker{Code: CodeOtherName})
		}
		items = append(home, prof...)
	} else {
		if len(f.items) == 0 && !f.hidden && !lingers {
			return Plan{}, &Error{Code: CodeGone}
		}
		// Only temporary folders: Claude Code keeps them by id for any config
		// folder, also one baton never looks in, where the conversation may
		// be open right now. They are a delete's to finish only when baton's
		// own delete of it stopped half way.
		if !f.hidden && !lingers && !run.Left(j.Paths.Root, j.id) && !slices.ContainsFunc(f.items, func(it item) bool { return it.kind != kindTemp }) {
			return Plan{}, &Error{Code: CodeGone}
		}
		// No name left, but what a delete that stopped half way did not
		// move: doing it again finishes it.
		for _, it := range f.items {
			if it.kind == kindRun || fsx.IsWithin(it.path, j.Paths.ProfilesDir()) {
				p.MadeIn = "baton"
			}
		}
		// Its file is there, only through a link (the refusal says so): the
		// plan says which conversation it is, not what an earlier delete
		// left behind.
		if n := j.selected(f.unseen); n != nil {
			t := claude.Describe(n.path, n.fi)
			p.Title, p.Cwd, p.Source = t.Name(), t.Cwd, t.Source
			if fsx.IsWithin(n.path, j.Paths.ProfilesDir()) {
				p.MadeIn = "baton"
			}
		}
	}
	slices.SortStableFunc(f.items, func(a, b item) int { return a.kind - b.kind })
	for _, it := range f.items {
		if !slices.ContainsFunc(items, func(o item) bool { return strings.EqualFold(o.path, it.path) }) {
			items = append(items, it)
		}
	}
	p.items = items

	if f.hidden {
		bs = append(bs, Blocker{Code: CodeLinkedFolder})
	}
	if !own && run.Deleting(j.Paths.Root, j.id) {
		bs = append(bs, Blocker{Code: CodeDeleting})
	}
	bs = append(bs, j.inUse(p.group, runs, unread)...)
	if j.Desk != nil {
		res, err := j.Desk.Forget(j.id, true)
		if err != nil {
			// desktop.json could not be read: what baton showed there is unknown.
			bs = append(bs, Blocker{Code: CodeCannotCheck})
		} else {
			bs = append(bs, fromDesk(res.Blockers)...)
			p.DesktopOff = res.Off
		}
	}
	if err := j.bin.Preflight(p.Paths()); err != nil {
		b, ok := binBlocker(err)
		if !ok {
			b = Blocker{Code: CodeRecycleFailed}
		}
		bs = append(bs, b)
	}
	p.Blockers = sorted(bs)
	p.OK = len(p.Blockers) == 0

	p.Items = count(p.items)
	n, partial := j.copies(p.items)
	if n > 0 {
		p.Notes = append(p.Notes, Note{Code: NoteCopies, N: n})
	}
	if partial {
		p.Notes = append(p.Notes, Note{Code: NoteNotFullyChecked})
	}
	if f.links > 0 {
		p.Notes = append(p.Notes, Note{Code: NoteLinksKept, N: f.links})
	}
	return p, nil
}

// selected is the name the others are compared with: the newest in the
// config folder the conversation was picked in, else the newest anywhere
// (a name there may have gone in a delete that stopped half way). nil: no
// name.
func (j *job) selected(names []name) *name {
	pick := func(in func(name) bool) *name {
		var best *name
		for i := range names {
			n := &names[i]
			if in(*n) && (best == nil || n.fi.ModTime().After(best.fi.ModTime()) && !os.SameFile(n.fi, best.fi)) {
				best = n
			}
		}
		return best
	}
	if i := slices.IndexFunc(j.Dirs, func(d Dir) bool { return d.Where == j.Where }); j.Where != "" && i >= 0 {
		if n := pick(func(n name) bool { return strings.EqualFold(n.dir, j.Dirs[i].Path) }); n != nil {
			return n
		}
	}
	return pick(func(name) bool { return true })
}

// scanProjects finds the conversation's names in config folder d, and what
// Claude Code keeps by its id next to them. <id>.desktop-released.json is
// the desktop app's: it judges other conversations' deletes by it.
func (j *job) scanProjects(d string, f *found) {
	projects := filepath.Join(d, "projects")
	if !realDir(projects) {
		return
	}
	slugs, _ := os.ReadDir(projects)
	for _, s := range slugs {
		folder := filepath.Join(projects, s.Name())
		if !realDir(folder) {
			continue
		}
		ents, _ := os.ReadDir(folder)
		for _, e := range ents {
			n := strings.ToLower(e.Name())
			kind := kindSibling
			switch {
			case n == j.id+".jsonl":
				kind = kindName
			case n == j.id:
				kind = kindSession
			case n == j.id+".desktop-released.json", !strings.HasPrefix(n, j.id+"."):
				continue
			}
			p := filepath.Join(folder, e.Name())
			fi, ok := j.itemAt(p, f)
			switch {
			case !ok:
			case kind == kindName && fi.Mode().IsRegular():
				f.names = append(f.names, name{path: p, dir: d, fi: fi})
			case kind == kindName:
				f.items = append(f.items, item{path: p, kind: kindSibling, dir: fi.IsDir()})
			default:
				f.items = append(f.items, item{path: p, kind: kind, dir: fi.IsDir()})
			}
		}
	}
}

// hiddenNames looks for a name of the conversation's file in config folder
// d that scanProjects could not see: in a projects or project folder that is
// a link or junction (the user moved it to another drive), in one that
// can't be read, or by a name that is a link itself. baton's lists and
// Claude Code's resume find it through the link, so it is the conversation
// all the same; moving only what the scan saw would leave the file behind,
// without its checkpoints and runs, and say it went. A project folder that
// is a link and holds nothing of it does not count.
func (j *job) hiddenNames(d string, f *found) {
	projects := filepath.Join(d, "projects")
	slugs, err := os.ReadDir(projects) // through a link, as the lists look
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		f.hidden = true
		return
	}
	for _, s := range slugs {
		if s.Type().IsRegular() {
			continue
		}
		fi, err := os.Stat(filepath.Join(projects, s.Name(), j.id+".jsonl"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			f.hidden = true // a project folder that can't be read may hold it
		case !fi.Mode().IsRegular():
		case !slices.ContainsFunc(f.names, func(n name) bool { return os.SameFile(n.fi, fi) }):
			f.hidden = true
			f.unseen = append(f.unseen, name{path: filepath.Join(projects, s.Name(), j.id+".jsonl"), dir: d, fi: fi})
		}
	}
}

// scanRoot finds the folders config folder d keeps by the conversation's
// id: its /rewind checkpoints, hook environment, task list (only the one by
// its exact id: a task list can be shared under another name) and debug log.
func (j *job) scanRoot(d string, f *found) {
	for _, sub := range []string{"file-history", "session-env", "tasks"} {
		if base := filepath.Join(d, sub); realDir(base) {
			j.add(f, filepath.Join(base, j.id), kindRoot)
		}
	}
	if base := filepath.Join(d, "debug"); realDir(base) {
		j.add(f, filepath.Join(base, j.id+".txt"), kindRoot)
	}
}

// scanTemp finds Claude Code's temporary folders of the conversation, one
// per project.
func (j *job) scanTemp(f *found) {
	base := filepath.Join(j.temp, "claude")
	if !realDir(base) {
		return
	}
	ents, _ := os.ReadDir(base)
	for _, e := range ents {
		if dir := filepath.Join(base, e.Name()); realDir(dir) {
			j.add(f, filepath.Join(dir, j.id), kindTemp)
		}
	}
}

// add adds what is at p, if anything, as an item of kind.
func (j *job) add(f *found, p string, kind int) {
	if fi, ok := j.itemAt(p, f); ok {
		f.items = append(f.items, item{path: p, kind: kind, dir: fi.IsDir()})
	}
}

// itemAt says whether what is at p is an item: a regular file or a real
// folder. A link or junction is never one, nor followed: the bin would move
// the link, but what it points at is not the conversation's (a profile
// links the user's own agents and skills). It is counted, and left.
func (j *job) itemAt(p string, f *found) (os.FileInfo, bool) {
	fi, err := os.Lstat(p)
	switch {
	case err != nil:
		return nil, false
	case fsx.IsReparsePoint(fi):
		f.links++
		return nil, false
	}
	return fi, fi.Mode().IsRegular() || fi.IsDir()
}

// realDir: p is a folder, not a link or junction to one.
func realDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir() && !fsx.IsReparsePoint(fi)
}

// runDir is one of baton's runs and its journal.
type runDir struct {
	dir string
	j   run.Journal
}

// readRuns reads every run's journal. unread: the runs folder, or a
// journal, could not be read right now (another program holds it), so a run
// may have the conversation. A journal that is gone or is no JSON is
// skipped, as the desktop side does.
func readRuns(runsDir string) (out []runDir, unread bool) {
	ents, err := os.ReadDir(runsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		unread = true
	}
	for _, e := range ents {
		dir := filepath.Join(runsDir, e.Name())
		if !realDir(dir) {
			continue
		}
		j, err := run.LoadJournal(dir)
		if err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) && !errors.Is(err, fs.ErrNotExist) {
				unread = true
			}
			continue
		}
		out = append(out, runDir{dir: dir, j: j})
	}
	return out, unread
}

// active: the run has not ended and its supervisor runs.
func (j *job) active(r run.Journal) bool {
	return r.State != run.StateDone && j.alive(r.SupervisorPID, r.SupervisorCreated)
}

// inUse says what has the conversation open, or will open it: a baton
// session (its claude has it open, or the run will --resume it or start a
// file under its id, also while it waits, asks or switches with no claude),
// a copy starting from it (a relaunch before the copy's first turn forks it
// again), a Claude Code anywhere else, and what could not be read.
func (j *job) inUse(group []string, runs []runDir, unread bool) []Blocker {
	var out []Blocker
	h := run.HeldBy(j.dirs, j.Paths.RunsDir(), j.id, j.alive)
	if h.Run != nil {
		out = append(out, Blocker{Code: CodeInBaton, RunID: h.Run.RunID})
	}
	for _, o := range h.Holders {
		switch {
		case h.Run != nil && h.Run.ChildPID > 0 && claude.StartedBy(o, h.Run.ChildPID):
		case o.Status == "unknown":
			// A registry entry that stayed unreadable may be anyone's.
			out = append(out, Blocker{Code: CodeCannotCheck})
		default:
			holder := o.Entrypoint
			if holder == "" {
				holder = "?"
			}
			out = append(out, Blocker{Code: CodeOpenElsewhere, Holder: holder})
		}
	}
	for _, r := range runs {
		if !j.active(r.j) {
			continue
		}
		id := runID(r)
		// Opens: one that waits for an account before it resumes it.
		if strings.EqualFold(r.j.SessionID, j.id) || named(r.j.TranscriptPath, group) || named(r.j.Opens, group) {
			out = append(out, Blocker{Code: CodeInBaton, RunID: id})
		}
		// The supervisor keeps the path as it got it, maybe typed in capitals.
		if named(r.j.ForkOf, group) {
			out = append(out, Blocker{Code: CodeCopyStarting, RunID: id})
		}
	}
	if unread {
		out = append(out, Blocker{Code: CodeCannotCheck})
	}
	return out
}

// runID is a run's id, as the window and `baton stop --run` know it.
func runID(r runDir) string {
	if r.j.RunID != "" {
		return r.j.RunID
	}
	return filepath.Base(r.dir)
}

// named: path is one of the names in group, by spelling or by file.
func named(path string, group []string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	for _, g := range group {
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(g)) {
			return true
		}
		if err == nil {
			if gi, gerr := os.Stat(g); gerr == nil && os.SameFile(fi, gi) {
				return true
			}
		}
	}
	return false
}

func fromDesk(in []desktop.Blocker) []Blocker {
	out := make([]Blocker, 0, len(in))
	for _, b := range in {
		out = append(out, Blocker{Code: b.Code, Detail: b.Detail})
	}
	return out
}

// sorted is bs without repeats, in the order blockers are given in.
func sorted(bs []Blocker) []Blocker {
	out := []Blocker{}
	for _, b := range bs {
		if !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b Blocker) int { return slices.Index(order, a.Code) - slices.Index(order, b.Code) })
	return out
}

// count says how much the items are. The names of the file are one file.
func count(items []item) Counts {
	var c Counts
	once := false
	for _, it := range items {
		switch {
		case it.kind == kindOwnName || it.kind == kindName:
			if !once {
				once = true
				c.Files++
				c.Bytes += size(it.path)
			}
		case it.kind == kindRun:
			c.Runs++
			c.Bytes += size(it.path)
		case it.dir:
			c.Folders++
			c.Bytes += size(it.path)
		default:
			c.Files++
			c.Bytes += size(it.path)
		}
	}
	return c
}

// size is what a file, or everything in a folder, holds. Links inside are
// not followed.
func size(path string) int64 {
	var n int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// copiesRead caps what copies reads: the conversations next to one can be
// many and long.
var copiesRead int64 = 256 << 20

// copies counts the other conversations in the folders of this one that
// point at the large tool outputs saved with it (a copy, or one /rewind or
// the desktop app went on from, keeps its rows, paths and all), which go
// with it. partial: there was more to read than copiesRead.
func (j *job) copies(items []item) (n int, partial bool) {
	slugs := map[string]bool{}
	outputs := false
	for _, it := range items {
		switch it.kind {
		case kindOwnName, kindName:
			slugs[strings.ToLower(filepath.Base(filepath.Dir(it.path)))] = true
		case kindSession:
			slugs[strings.ToLower(filepath.Base(filepath.Dir(it.path)))] = true
			outputs = outputs || realDir(filepath.Join(it.path, "tool-results"))
		}
	}
	if !outputs {
		return 0, false
	}
	// A row names the folder in JSON: a backslash is written twice.
	needles := [][]byte{[]byte(j.id + `\\tool-results`), []byte(j.id + "/tool-results")}
	budget := copiesRead
	var seen []os.FileInfo
	for _, d := range j.dirs {
		projects := filepath.Join(d, "projects")
		if !realDir(projects) {
			continue
		}
		folders, _ := os.ReadDir(projects)
		for _, s := range folders {
			folder := filepath.Join(projects, s.Name())
			if !slugs[strings.ToLower(s.Name())] || !realDir(folder) {
				continue
			}
			ents, _ := os.ReadDir(folder)
			for _, e := range ents {
				low := strings.ToLower(e.Name())
				if !strings.HasSuffix(low, ".jsonl") || strings.HasPrefix(low, j.id) {
					continue
				}
				path := filepath.Join(folder, e.Name())
				fi, err := os.Lstat(path)
				if err != nil || !fi.Mode().IsRegular() || slices.ContainsFunc(seen, func(o os.FileInfo) bool { return os.SameFile(o, fi) }) {
					continue
				}
				seen = append(seen, fi) // a copy shown in the desktop app has two names
				if fi.Size() > budget {
					return n, true
				}
				budget -= fi.Size()
				data, err := fsx.ReadFileShared(path)
				if err == nil && (bytes.Contains(data, needles[0]) || bytes.Contains(data, needles[1])) {
					n++
				}
			}
		}
	}
	return n, false
}
