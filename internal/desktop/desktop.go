// Package desktop shows baton's sessions in the Claude desktop app's Code
// tab, for the accounts the user picks.
//
// The desktop app lists a session for an account when that account's
// folder in its data folder holds a record for it:
//
//	<data>\claude-code-sessions\<accountUuid>\<orgUuid>\local_<id>.json
//
// baton writes records shaped exactly like the app's own record for a
// conversation it imports from Claude Code, and removes only records it
// wrote itself (its index, desktop.json, lists them). The app opens a
// conversation from the user's own Claude Code folder (~/.claude), while a
// baton session's conversation lives in an account's profile: baton gives
// the file a second name there (an NTFS hard link), so both sides read and
// write one file.
//
// The app reads a list only when it starts or switches to that account, and
// keeps the list of the account it is signed in to in memory, writing
// records back from there. So while the app runs, baton changes the active
// account's list only when the user asks (the window says to restart the
// app), and its own background tidying waits until it is safe.
package desktop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/run"
	"baton/internal/share"
	"baton/internal/winproc"
)

// Desk is the desktop feature for one baton data folder.
type Desk struct {
	IndexPath string // baton's index (desktop.json)
	LockPath  string
	UserHome  string // the user's own Claude Code folder, where the app finds conversations
	RunsDir   string // baton's runs: their journals say which file each session writes by
	Accounts  []accounts.Account
	Running   func() bool      // whether the desktop app runs (AppRunning)
	Now       func() time.Time // the time records are written at

	dataOnce sync.Once
	data     string
	dataFn   func() string
}

// New is the desktop feature of baton's data folder p, for the accounts in reg.
func New(p app.Paths, reg accounts.Registry) *Desk {
	return &Desk{IndexPath: p.Desktop(), LockPath: p.DesktopLock(), UserHome: share.UserHome(p.ProfilesDir()),
		RunsDir: p.RunsDir(), Accounts: reg.Accounts, Running: AppRunning, Now: time.Now, dataFn: DataDir}
}

// Data is the desktop app's data folder ("" without the app). It is looked
// for once, and only when needed.
func (d *Desk) Data() string {
	d.dataOnce.Do(func() {
		if d.dataFn != nil {
			d.data = d.dataFn()
		}
	})
	return d.data
}

// SetData names the desktop app's data folder (tests).
func (d *Desk) SetData(dir string) {
	d.dataOnce.Do(func() {})
	d.data = dir
}

// Conversation is the session asked about: its conversation id and file.
type Conversation struct {
	ID         string
	Transcript string
}

// Error says why a change cannot be made, in a word the window explains.
type Error struct {
	Code   string // no_desktop | no_file | unknown_place | split | conflict | other_drive | link_failed
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

func fail(code, detail string) error { return &Error{Code: code, Detail: detail} }

// about is what a record says about a conversation.
type about struct {
	cwd, title    string
	created, last int64
}

func describe(path string) about {
	fi, err := os.Stat(path)
	if err != nil {
		return about{}
	}
	t := claude.Describe(path, fi)
	a := about{cwd: t.Cwd, title: t.Name()}
	if a.title == "" {
		a.title = filepath.Base(t.Cwd)
	}
	a.created, a.last = times(path)
	return a
}

// placeDir is the folder of the desktop app's list for a place key.
func (d *Desk) placeDir(key string) string {
	acct, org, _ := strings.Cut(key, "/")
	return filepath.Join(d.Data(), sessionsDir, acct, org)
}

// validKey accepts <accountUuid>/<orgUuid>.
func validKey(key string) bool {
	acct, org, ok := strings.Cut(key, "/")
	return ok && validID(acct) && validID(org)
}

// profileDirs are baton's accounts' profiles.
func (d *Desk) profileDirs() []string {
	var out []string
	for _, a := range d.Accounts {
		if a.ProfileDir != "" {
			out = append(out, a.ProfileDir)
		}
	}
	return out
}

// byPriority is the accounts in the order baton uses them.
func byPriority(in []accounts.Account) []accounts.Account {
	out := append([]accounts.Account{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ---- the second name ---------------------------------------------------------

// inUserHome: the file is in the user's own Claude Code folder already,
// where the desktop app finds it (a conversation brought in as it is). Both
// are spelled out in full first: one folder can be named in 8.3 form
// (C:\Users\JOHNDO~1) on one side and not on the other.
func (d *Desk) inUserHome(p string) bool {
	return d.UserHome != "" && p != "" && under(longPath(p), longPath(filepath.Join(d.UserHome, "projects")))
}

// linkPath is the second name for a conversation file: the same project
// folder name and file name, under the user's own projects folder. The
// folder name is kept as it is, never made again from the cwd (Claude
// Code's name for a folder is lossy).
func (d *Desk) linkPath(file string) string {
	return filepath.Join(d.UserHome, "projects", filepath.Base(filepath.Dir(file)), filepath.Base(file))
}

// linkProblem says why file cannot get its second name ("" when it can).
func (d *Desk) linkProblem(file string) string {
	switch {
	case file == "" || !isFile(file):
		return "no_file"
	case d.inUserHome(file):
		return ""
	case d.UserHome == "":
		return "link_failed"
	}
	dst := d.linkPath(file)
	if _, err := os.Lstat(dst); err == nil && !sameFile(dst, file) {
		return "conflict"
	}
	if !strings.EqualFold(filepath.VolumeName(cleanPath(dst)), filepath.VolumeName(cleanPath(file))) {
		return "other_drive"
	}
	return ""
}

// ensureLink gives conversation id's file its second name, unless it has
// one or needs none. Something else under that name is never replaced. A
// name that is there already, the same file, is taken on as it is (another
// thread of baton's may have made it and need it too): removing one is
// always up to linkUsed, which asks every thread and everything else that
// may open it.
func (d *Desk) ensureLink(t *Thread, id, file string) error {
	if p := d.linkProblem(file); p != "" {
		return fail(p, file)
	}
	if d.inUserHome(file) {
		return nil
	}
	dst := d.linkPath(file)
	if samePath(dst, file) {
		return nil // the user's own name already, spelled another way
	}
	if l, ok := t.Links[id]; ok && sameFile(l.Path, file) && !samePath(l.Path, file) {
		t.Links[id] = Link{Path: l.Path, File: file}
		return nil
	}
	if _, err := os.Lstat(dst); err == nil {
		// The same file already (made by baton before its list was lost).
		t.Links[id] = Link{Path: dst, File: file}
		return nil
	}
	if err := mkdirAll(filepath.Dir(dst)); err != nil {
		return fail("link_failed", err.Error())
	}
	if err := link(file, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fail("conflict", dst)
		}
		return fail("link_failed", err.Error())
	}
	t.Links[id] = Link{Path: dst, File: file}
	return nil
}

// linkNeeded: a record of the thread's shows conversation id, or will once
// it is written, or one the desktop app took over from it does.
func (t *Thread) linkNeeded(id string) bool {
	if strings.EqualFold(id, t.Session) && t.shown() {
		return true
	}
	for _, p := range t.Places {
		if strings.EqualFold(p.Cli, id) && (p.State == stateShown || p.State == stateHiding || p.State == stateReleased) {
			return true
		}
	}
	for _, p := range t.Released {
		if strings.EqualFold(p.Cli, id) {
			return true
		}
	}
	return false
}

// dropLinks removes the thread's second names nothing uses any more (see
// linkUsed). A name is removed only while baton's own name, a different
// path, is the same file and the file has more than one name (so it is
// never the only one left); otherwise it waits for the next look. An entry
// that names one file twice, or a file in the user's own folder as
// baton's, is forgotten and never removed: that name is the conversation.
func (d *Desk) dropLinks(ix *Index, t *Thread) {
	for id, l := range t.Links {
		if samePath(l.Path, l.File) || d.inUserHome(l.File) {
			delete(t.Links, id)
			continue
		}
		if ix.linkNeeded(id) || ix.loaded(id) {
			continue
		}
		if _, err := os.Lstat(l.Path); errors.Is(err, os.ErrNotExist) {
			delete(t.Links, id)
			continue
		}
		if !sameFile(l.Path, l.File) || linkCount(l.Path) < 2 {
			continue
		}
		if d.linkUsed(ix, id, l.Path) {
			continue
		}
		if removeFile(l.Path) == nil {
			delete(t.Links, id)
		}
	}
}

// linkUsed is the one rule for removing a second name: it reports whether
// anything may still open or write conversation id through its second name
// path. That is:
//
//   - a thread of baton's (any of them: two threads share a name when one's
//     earlier conversation is shown on its own) whose record shows id, will
//     once written, or was taken over by the desktop app;
//   - a record of baton's it took off a list the running app still has in
//     memory (Gone.Loaded), which may point at id: the app lists it there
//     and opens it from there, takes it over, or writes it back as it quits
//     (it is read on disk then), so the name stays until the app let go of
//     that list;
//   - a record in any of the desktop app's lists that points at id, whoever
//     made it, also one the app left half written (it makes that a record
//     at its next start): the app opens a record's conversation from the
//     user's own folder;
//   - a Claude Code that has id open: in the user's own folder (the desktop
//     app's), in an account's profile (one of baton's sessions, unless its
//     run says it writes by another name), and a run of baton's that writes
//     by path, also while its claude is being started again, or that was
//     cut off and is offered for reopening by that path.
//
// A name one of these keeps is asked about at every look, for as long as it
// is kept, so the looks are cheap first: the record that kept it last time
// is read again before anything else, runs are read again only once they
// changed, and every list is read only when nothing else keeps the name
// (then it goes, unless a record turns up).
func (d *Desk) linkUsed(ix *Index, id, path string) bool {
	return ix.linkNeeded(id) || ix.loaded(id) || d.listedStill(id) || d.claudeHas(id, path) || d.listed(id)
}

// linkNeeded reports whether a thread of baton's needs conversation id's
// second name (see Thread.linkNeeded).
func (ix *Index) linkNeeded(id string) bool {
	return slices.ContainsFunc(ix.Threads, func(t *Thread) bool { return t.linkNeeded(id) })
}

// claudeHas reports whether a Claude Code may have conversation id open by
// its second name path, or a run of baton's writes by it. A Claude Code in
// the user's own folder opens it from there. One in an account's profile
// is a session of baton's: it may have been resumed by that path, so it
// counts unless its run (started it, and still runs) names another file.
// One whose registry entry cannot be read counts too. A run that was cut
// off counts while baton offers to reopen it, which it does by the path its
// journal names.
func (d *Desk) claudeHas(id, path string) bool {
	if d.UserHome != "" && len(claude.Holders([]string{d.UserHome}, id)) > 0 {
		return true
	}
	all, unread := d.journals()
	if unread {
		// A run of baton's whose journal cannot be read now may write by
		// path: the name stays, and is asked about again at the next look.
		return true
	}
	var runs []run.Journal
	for _, j := range all {
		if runAlive(j) {
			runs = append(runs, j)
		}
	}
	for _, j := range runs {
		if samePath(j.TranscriptPath, path) {
			return true
		}
	}
	for _, s := range claude.Holders(d.profileDirs(), id) {
		ours := slices.ContainsFunc(runs, func(j run.Journal) bool {
			return j.ChildPID > 0 && j.TranscriptPath != "" && strings.EqualFold(j.SessionID, id) && claude.StartedBy(s, j.ChildPID)
		})
		if !ours {
			return true
		}
	}
	for _, j := range run.Unfinished(all, runAlive, time.Now()) {
		if samePath(j.TranscriptPath, path) {
			return true
		}
	}
	return false
}

// runAlive: the run is not done and its supervisor still runs.
func runAlive(j run.Journal) bool {
	if j.State == run.StateDone {
		return false
	}
	got := winproc.ProcessCreated(j.SupervisorPID)
	return !got.IsZero() && got.Sub(j.SupervisorCreated) > -time.Second && got.Sub(j.SupervisorCreated) < time.Second
}

// journalsSeen keeps the run journals read, by path, size and time: a
// second name a run keeps is asked about at every look, and of a month of
// runs nearly all are done and never change again.
var journalsSeen struct {
	sync.Mutex
	dir string
	m   map[string]seenJournal
}

type seenJournal struct {
	size int64
	mod  time.Time
	j    run.Journal
}

// journals are baton's runs, each read again only once it changed. unread:
// a run, or the runs folder, cannot be read right now (another program
// holds it, or it may not be opened), so a run may write by any path. A
// journal that is gone, or is no journal (not JSON), is skipped as before:
// it never keeps a name for good.
func (d *Desk) journals() (out []run.Journal, unread bool) {
	if d.RunsDir == "" {
		return nil, false
	}
	ents, err := os.ReadDir(d.RunsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		unread = true
	}
	journalsSeen.Lock()
	defer journalsSeen.Unlock()
	old := journalsSeen.m
	if !strings.EqualFold(journalsSeen.dir, d.RunsDir) {
		old = nil
	}
	seen := map[string]seenJournal{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(d.RunsDir, e.Name())
		fi, err := os.Stat(run.JournalPath(dir))
		if err != nil {
			unread = unread || !errors.Is(err, os.ErrNotExist)
			continue
		}
		if fi.IsDir() {
			continue
		}
		c, hit := old[dir]
		if !hit || c.size != fi.Size() || !c.mod.Equal(fi.ModTime()) {
			j, err := run.LoadJournal(dir)
			// One that cannot be opened or read now (an open or read error)
			// may say anything; one that is no JSON is skipped. Neither is
			// kept, so the next look reads it again.
			var pe *os.PathError
			held := err != nil && errors.As(err, &pe) && !errors.Is(err, os.ErrNotExist)
			switch {
			case err == nil:
				c = seenJournal{fi.Size(), fi.ModTime(), j}
			case hit:
				// As it was read last, and read again next time.
				c.size = -1
				unread = unread || held
			default:
				unread = unread || held
				continue
			}
		}
		seen[dir] = c
		out = append(out, c.j)
	}
	journalsSeen.dir, journalsSeen.m = d.RunsDir, seen
	return out, unread
}

// listedAt remembers, for the desktop app's data folder, where the last
// read of every list found a record of each conversation: a second name such
// a record keeps then costs one read at each look, not a read of every list.
var listedAt struct {
	sync.Mutex
	data string
	at   map[string]string // conversation id (lower case) -> a record of it
	// held: the records and list folders that could not be read then
	// (another program held them, or they may not be opened). Any of them
	// may point at any conversation.
	held []unreadable
}

// unreadable is a record, or a list folder (dir), that could not be read.
type unreadable struct {
	path string
	dir  bool
}

// still reports whether u still cannot be read, or (a record) points at
// conversation id now. A folder that can be read again says nothing yet:
// every list is read again then.
func (u unreadable) still(id string) bool {
	if u.dir {
		f, err := os.Open(u.path)
		if err == nil {
			_, err = f.ReadDir(1)
			f.Close()
		}
		return err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrNotExist)
	}
	h, ok, err := readHeadErr(u.path)
	return err != nil || ok && strings.EqualFold(h.Cli, id)
}

// listedStill reports whether the record that pointed at conversation id
// at the last read of every list still does, or one that could not be read
// then still cannot be (or points at id now). It reads only those.
func (d *Desk) listedStill(id string) bool {
	if d.Data() == "" {
		return false
	}
	listedAt.Lock()
	path, held := "", []unreadable(nil)
	if strings.EqualFold(listedAt.data, d.Data()) {
		path, held = listedAt.at[strings.ToLower(id)], listedAt.held
	}
	listedAt.Unlock()
	if path != "" {
		h, ok, err := readHeadErr(path)
		if err != nil || ok && strings.EqualFold(h.Cli, id) {
			return true
		}
	}
	return slices.ContainsFunc(held, func(u unreadable) bool { return u.still(id) })
}

// listed reports whether a record in any list of the desktop app points at
// conversation id: one of the app's own, one it took over, one of baton's
// still on disk, or one the app left half written (local_*.json.tmp, which
// it makes a record at its next start). A record or list folder that cannot
// be read right now may point at it too, so it counts: the name stays and
// is asked about again at the next look. One that is gone, or is no record
// (not JSON), does not. It reads every list, and notes where it found each
// conversation, and what it could not read, for listedStill.
func (d *Desk) listed(id string) bool {
	if d.Data() == "" {
		return false
	}
	at := map[string]string{}
	var held []unreadable
	readDir := func(dir string) []os.DirEntry {
		ents, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			held = append(held, unreadable{path: dir, dir: true})
		}
		return ents
	}
	root := filepath.Join(d.Data(), sessionsDir)
	for _, a := range readDir(root) {
		if !a.IsDir() {
			continue
		}
		for _, o := range readDir(filepath.Join(root, a.Name())) {
			if !o.IsDir() {
				continue
			}
			dir := filepath.Join(root, a.Name(), o.Name())
			for _, e := range readDir(dir) {
				n := e.Name()
				if e.IsDir() || !strings.HasPrefix(n, "local_") || !strings.HasSuffix(n, ".json") && !strings.HasSuffix(n, ".json.tmp") {
					continue
				}
				path := filepath.Join(dir, n)
				if h, ok, err := readHeadErr(path); err != nil {
					held = append(held, unreadable{path: path})
				} else if ok && h.Cli != "" {
					at[strings.ToLower(h.Cli)] = path
				}
			}
		}
	}
	listedAt.Lock()
	listedAt.data, listedAt.at, listedAt.held = d.Data(), at, held
	listedAt.Unlock()
	return len(held) > 0 || at[strings.ToLower(id)] != ""
}

// checkSplit marks the thread split when a second name stopped being the
// same file as baton's: a rename-replace by one side (Claude Code's opt-in
// transcript trimming, an editor) leaves each side with a file of its own.
// It is never mended by itself.
func (t *Thread) checkSplit() {
	for _, l := range t.Links {
		if isFile(l.Path) && isFile(l.File) && !sameFile(l.Path, l.File) {
			t.Split = true
		}
	}
}

// locate finds the thread's conversation file when it moved or is not known
// yet: in the accounts' profiles, else under its second name (Claude Code's
// cleanup removes old files per folder, so one name can outlive the other).
//
// The session may run from the second name (resumed by that path, or found
// there by a lookup): baton keeps to its own name while that is the same
// file, and once that is gone the second name is the conversation, which
// baton then never removes.
func (d *Desk) locate(t *Thread) {
	if l, ok := t.Links[t.Session]; ok && t.Transcript != "" && samePath(t.Transcript, l.Path) {
		switch {
		case !isFile(l.File):
			delete(t.Links, t.Session) // its only name now
		case !samePath(l.File, l.Path) && sameFile(l.File, l.Path):
			t.Transcript = l.File
		}
		// Otherwise the two names split, which checkSplit says.
	}
	if t.Transcript != "" && isFile(t.Transcript) && strings.EqualFold(strings.TrimSuffix(filepath.Base(t.Transcript), ".jsonl"), t.Session) {
		if l, ok := t.Links[t.Session]; ok && !d.inUserHome(t.Transcript) && !samePath(l.File, t.Transcript) &&
			!samePath(l.Path, t.Transcript) && sameFile(l.Path, t.Transcript) {
			t.Links[t.Session] = Link{Path: l.Path, File: t.Transcript} // moved with its folder (a worktree)
		}
		return
	}
	if p := claude.FindTranscript(d.profileDirs(), t.Session); p != "" {
		t.Transcript = p
		return
	}
	if l, ok := t.Links[t.Session]; ok && isFile(l.Path) {
		// Its only name now: it is the conversation, never removed by baton.
		t.Transcript = l.Path
		delete(t.Links, t.Session)
	}
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

func cleanPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
