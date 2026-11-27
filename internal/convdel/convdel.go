// Package convdel deletes a conversation: its file, every name of it, and
// what Claude Code and baton keep by its id, all moved to the Windows
// Recycle Bin in one go, never deleted for good. It refuses, and changes
// nothing, while anything has the conversation open or may open it soon,
// while the desktop app has it as its own, or when the bin cannot take every
// item. The window and the CLI share it: Look says what a delete would do,
// Delete does it.
package convdel

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/desktop"
	"baton/internal/fsx"
	"baton/internal/recycle"
	"baton/internal/run"
	"baton/internal/winproc"
)

// Request names the conversation and what the delete works with.
type Request struct {
	ID string // the conversation's session id
	// Where is the config folder (a Dirs entry's Where) the conversation
	// was picked in; its name there is the one the others are compared with.
	// "" (the CLI): the newest name anywhere, as claude.FindTranscript.
	Where string

	Paths app.Paths // baton's data folder: profiles, runs, config.json
	// Dirs are every config folder conversations live in: the accounts'
	// profiles first, then the user's own Claude Code folder.
	Dirs []Dir
	Desk *desktop.Desk // nil: no desktop side
	Bin  recycle.Bin   // nil: recycle.New()
	// Alive says whether a run's supervisor still runs (pid and creation
	// time); nil: checked against the process itself.
	Alive func(pid int, created time.Time) bool
	// Office lets go of the conversation's files before they move (the
	// window's office reads them); optional.
	Office func(id string)
	// TempRoot holds Claude Code's temporary folders (<TempRoot>\claude\*\<id>);
	// "" is os.TempDir().
	TempRoot string
}

// Dir is a config folder conversations live in.
type Dir struct {
	Where string // the window's name for it: an account id, or "claude-code"
	Path  string
}

// Plan is what a delete would do, as the window shows it. It names no
// file or folder: the paths stay inside (Paths gives them to the CLI).
type Plan struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	// MadeIn: "baton" when a name of the file is in an account's profile,
	// else "outside" (Source then says what made it, for the wording only:
	// a copy keeps its source's first rows).
	MadeIn   string    `json:"made_in"`
	Source   string    `json:"source"`
	OK       bool      `json:"ok"` // nothing blocks
	Blockers []Blocker `json:"blockers"`
	// DesktopOff are the desktop app's lists baton takes it off, by the
	// names of baton's accounts on them ("" is the app's own account).
	DesktopOff []string `json:"desktop_off"`
	Items      Counts   `json:"items"`
	Notes      []Note   `json:"notes"`
	// Files are the conversation's files when it has more than one: it went
	// on apart in two places, each with turns of its own. All of them go.
	Files []File `json:"files,omitempty"`

	items []item      // in the order they go to the Recycle Bin
	group []string    // the names of the conversation's files
	files [][2]uint64 // the files themselves (fsx.FileID), those known
}

// File is one of a conversation's files (Plan.Files).
type File struct {
	Where    []string  `json:"where"` // the config folders with a name of it (Dir.Where)
	Modified time.Time `json:"modified"`
	Bytes    int64     `json:"bytes"`
}

// Paths are the files and folders the delete moves, in the order they go.
func (p Plan) Paths() []string {
	out := make([]string, len(p.items))
	for i, it := range p.items {
		out[i] = it.path
	}
	return out
}

// Blocker is one thing that keeps the conversation from being deleted now.
type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"` // desktop_app: own, archived, released or prior
	Holder string `json:"holder,omitempty"` // open_elsewhere: what has it open (claude-desktop, cli, claude-vscode, ?)
	RunID  string `json:"run_id,omitempty"` // in_baton, copy_starting: the run
	Drive  string `json:"drive,omitempty"`  // no_recycle_bin: the drive
}

// Counts are how much goes to the Recycle Bin. Bytes counts the file once,
// whatever its names.
type Counts struct {
	Files   int   `json:"files"`
	Folders int   `json:"folders"`
	Runs    int   `json:"runs"`
	Bytes   int64 `json:"bytes"`
}

// Note is something the window says about a delete that does not stop it.
type Note struct {
	Code string `json:"code"`
	N    int    `json:"n,omitempty"`
}

// Note codes.
const (
	// NoteCopies: N other conversations went on from this one and point at
	// the large tool outputs saved with it, which go too.
	NoteCopies = "copies_use_outputs"
	// NoteNotFullyChecked: the conversations next to it were too many to read
	// through for NoteCopies.
	NoteNotFullyChecked = "not_fully_checked"
	// NoteLinksKept: N links or junctions stand where something of the
	// conversation would be; they and what they point at are left alone.
	NoteLinksKept = "links_kept"
)

// Result is what a delete did.
type Result struct {
	Moved int `json:"moved"` // items that went to the Recycle Bin
	// Warning "reopened": something opened the conversation again while it
	// went (a Claude Code writes a new file for it then).
	Warning    string   `json:"warning,omitempty"`
	DesktopOff []string `json:"desktop_off"` // the lists it was taken off
}

// WarnReopened is Result.Warning when the conversation came back.
const WarnReopened = "reopened"

// Codes of Error and Blocker.
const (
	CodeBadSession     = "bad_session"     // the id is no session id, or Where names no config folder
	CodeGone           = "gone"            // there is no such conversation
	CodeDeleting       = "deleting"        // another delete of it is running
	CodeInBaton        = "in_baton"        // a baton session runs it, or will (RunID)
	CodeCopyStarting   = "copy_starting"   // a copy of it is starting and needs it until its first turn (RunID)
	CodeSessionWindow  = "session_window"  // a baton session of it ended, but its window is still open (RunID)
	CodeOpenElsewhere  = "open_elsewhere"  // a Claude Code has it open (Holder)
	CodeCannotCheck    = "cannot_check"    // what has it open could not be read right now
	CodeOtherName      = "other_name"      // its file has a name outside Claude Code's folders
	CodeLinkedFolder   = "linked_folder"   // its file is in a folder reached through a link or junction, or one that can't be read
	CodeDesktopApp     = "desktop_app"     // the desktop app has it as its own (Detail)
	CodeDesktopBusy    = "desktop_busy"    // the running desktop app holds a list that shows it
	CodeDesktopWaiting = "desktop_waiting" // another session's record of baton's still points at it
	CodeNoRecycleBin   = "no_recycle_bin"  // the Recycle Bin can't take it (Drive)
	CodePathTooLong    = "path_too_long"   // a path is too long for the Recycle Bin
	CodeInUse          = "in_use"          // another program kept one of its files open
	CodeRecycleFailed  = "recycle_failed"  // the Recycle Bin did not take it, for a reason baton does not know
	CodePartly         = "partly"          // only part of it went to the Recycle Bin (Left)
	CodeChanged        = "changed"         // its files moved elsewhere while it was being deleted (Moved)
)

// order is the order blockers are given in: the first one is what the
// window says. Another delete says it all; then what the user ends or
// closes; a file baton would not get all of; the desktop app; what may pass
// in a moment; the Recycle Bin.
var order = []string{CodeDeleting, CodeInBaton, CodeCopyStarting, CodeSessionWindow, CodeOpenElsewhere, CodeOtherName, CodeLinkedFolder,
	CodeDesktopApp, CodeDesktopBusy, CodeDesktopWaiting, CodeCannotCheck, CodeNoRecycleBin, CodePathTooLong, CodeInUse, CodeRecycleFailed}

// Error is why a delete did not happen, or happened only in part.
type Error struct {
	Code     string
	Blockers []Blocker // what refused it, the first one's code is Code
	Drive    string    // no_recycle_bin
	Moved    int       // partly, changed: the items that went
	Left     int       // partly: the items still there
	// DesktopOff: the delete took the conversation off these lists of the
	// desktop app before it stopped, so it no longer shows there.
	DesktopOff []string
	Err        error
}

func (e *Error) Error() string {
	s := "convdel: " + e.Code
	switch {
	case e.Code == CodePartly:
		s += fmt.Sprintf(" (%d moved, %d left)", e.Moved, e.Left)
	case len(e.Blockers) > 0:
		b := e.Blockers[0]
		for _, v := range []string{b.Detail, b.Holder, b.RunID, b.Drive} {
			if v != "" {
				s += " " + v
			}
		}
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// Status is the HTTP status the window answers with.
func (e *Error) Status() int {
	switch e.Code {
	case CodeBadSession:
		return http.StatusBadRequest
	case CodeGone:
		return http.StatusNotFound
	case CodePartly, CodeRecycleFailed:
		return http.StatusInternalServerError
	}
	return http.StatusConflict
}

// refused is the error for blockers (sorted).
func refused(bs []Blocker, off []string) *Error {
	return &Error{Code: bs[0].Code, Blockers: bs, Drive: bs[0].Drive, DesktopOff: off}
}

// idRx: Claude Code names a conversation's files by its id in lower case.
var idRx = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// job is a Request checked and filled in.
type job struct {
	Request
	id    string
	dirs  []string // the config folders' paths, in order
	alive func(pid int, created time.Time) bool
	bin   recycle.Bin
	temp  string
}

func prepare(req Request) (*job, error) {
	id := strings.ToLower(req.ID)
	if !idRx.MatchString(id) {
		return nil, &Error{Code: CodeBadSession, Err: fmt.Errorf("%q is not a session id", req.ID)}
	}
	if req.Where != "" && !slices.ContainsFunc(req.Dirs, func(d Dir) bool { return d.Where == req.Where }) {
		return nil, &Error{Code: CodeBadSession, Err: fmt.Errorf("no config folder %q", req.Where)}
	}
	if req.Paths.Root == "" {
		return nil, errors.New("convdel: no baton data folder")
	}
	j := &job{Request: req, id: id, alive: req.Alive, bin: req.Bin, temp: req.TempRoot}
	j.Dirs = slices.Clone(req.Dirs)
	for i, d := range j.Dirs {
		if d.Path == "" {
			continue
		}
		abs, err := filepath.Abs(d.Path)
		if err != nil {
			return nil, err
		}
		j.Dirs[i].Path = abs
		if !slices.ContainsFunc(j.dirs, func(p string) bool { return strings.EqualFold(p, abs) }) {
			j.dirs = append(j.dirs, abs)
		}
	}
	if j.alive == nil {
		j.alive = supervisorAlive
	}
	if j.temp == "" {
		j.temp = os.TempDir()
	}
	if j.bin == nil {
		j.bin = recycle.New()
	}
	return j, nil
}

// Look says what deleting the conversation would do and what keeps it from
// going now. It changes nothing, but for the desktop side's own tidying
// first (Desk.Reconcile), which takes off what became safe to take off: the
// CLI has no loop that does it, and a record left so would refuse the
// delete. A conversation that is not there is an *Error (gone), as is a bad
// request (bad_session); blockers are in the plan.
func Look(req Request) (Plan, error) {
	j, err := prepare(req)
	if err != nil {
		return Plan{}, err
	}
	return j.look(false)
}

// Delete deletes the conversation: everything Look lists goes to the
// Recycle Bin, in one go, once nothing blocks. In order:
//
//  1. it takes the conversation's deleting marker, so baton starts nothing
//     on it meanwhile and two deletes never run at once;
//  2. Look, and any blocker refuses with nothing changed;
//  3. the desktop side takes baton's records of it off (all or nothing);
//  4. the office lets go of its files;
//  5. what has it open is checked again: the desktop app and terminals are
//     outside baton's locks;
//  6. everything goes to the Recycle Bin: second names in the user's own
//     folder first (a session of baton's still tracking one would take it
//     for the conversation once baton's name went), then the other names,
//     the files next to them, its folders, the temporary ones and its runs;
//  7. the office no longer hides it (nothing left to hide).
//
// A failure after step 3 leaves it, or part of it, in place, off the
// desktop app (Error.DesktopOff); a partly error once anything went. Doing
// it again finishes the rest: what is left is looked for again, also once
// every name of the file went (a note says it is a delete's to finish). When
// its files moved elsewhere meanwhile, the error says so (changed); when
// something opened it again meanwhile, the result warns (reopened).
func Delete(req Request) (Result, error) {
	j, err := prepare(req)
	if err != nil {
		return Result{}, err
	}
	release, err := run.MarkDeleting(j.Paths.Root, j.id)
	if err != nil {
		if errors.Is(err, run.ErrDeleting) {
			return Result{}, &Error{Code: CodeDeleting, Blockers: []Blocker{{Code: CodeDeleting}}, Err: err}
		}
		return Result{}, err
	}
	defer release()

	p, err := j.look(true)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == CodeGone {
			run.ClearLeft(j.Paths.Root, j.id) // nothing left to finish
		}
		return Result{}, err
	}
	if !p.OK {
		return Result{}, refused(p.Blockers, nil)
	}
	off := []string{}
	if j.Desk != nil {
		res, err := j.Desk.Forget(j.id, false)
		if err != nil {
			return Result{}, &Error{Code: CodeCannotCheck, Blockers: []Blocker{{Code: CodeCannotCheck}}, Err: err}
		}
		if len(res.Blockers) > 0 {
			return Result{}, refused(sorted(fromDesk(res.Blockers)), nil)
		}
		off = res.Off
	}
	if j.Office != nil {
		j.Office(j.id)
	}
	runs, unread := readRuns(j.Paths.RunsDir())
	if bs := sorted(j.inUse(p.group, runs, unread)); len(bs) > 0 {
		return Result{DesktopOff: off}, refused(bs, off)
	}

	paths := p.Paths()
	moved, err := j.bin.Recycle(paths)
	if err != nil {
		j.stopped(moved)
		return Result{Moved: moved, DesktopOff: off}, binFailed(err, moved, paths, off)
	}
	if j.Desk != nil {
		// Its second names are gone now: the desktop side forgets them.
		_ = j.Desk.Reconcile()
	}
	if j.changed(p, moved) {
		j.stopped(moved)
		return Result{Moved: moved, DesktopOff: off}, &Error{Code: CodeChanged, Blockers: []Blocker{{Code: CodeChanged}},
			Moved: moved, DesktopOff: off}
	}
	run.ClearLeft(j.Paths.Root, j.id)
	j.unhide()
	res := Result{Moved: moved, DesktopOff: off}
	if j.reopened() {
		res.Warning = WarnReopened
	}
	return res, nil
}

// stopped notes a delete that stopped half way, for the next one to finish
// (see run.NoteLeft); one that moved nothing left nothing of its own.
func (j *job) stopped(moved int) {
	if moved > 0 {
		_ = run.NoteLeft(j.Paths.Root, j.id)
	}
}

// changed: what the delete was to move went elsewhere meanwhile, so the bin
// skipped it and the conversation is still there: removing an account moves
// its profile's conversations into the user's own folder, and the CLI's
// removal does not wait for a delete the window runs (nor the other way
// round). Seen as nothing moved where the plan had something, or the file
// still there by a name of its.
func (j *job) changed(p Plan, moved int) bool {
	if moved == 0 && len(p.items) > 0 {
		return true
	}
	if len(p.files) == 0 {
		return false
	}
	var f found
	for _, d := range j.dirs {
		j.scanProjects(d, &f)
	}
	return slices.ContainsFunc(f.names, func(n name) bool {
		id, ok := fsx.FileID(n.path)
		return ok && slices.Contains(p.files, id)
	})
}

// binFailed is the error for a Recycle that stopped: partly once anything
// went, else why the bin did not take the first item.
func binFailed(err error, moved int, paths []string, off []string) error {
	left := 0
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			left++
		}
	}
	if errors.Is(err, recycle.ErrPartial) || moved > 0 {
		return &Error{Code: CodePartly, Moved: moved, Left: left, DesktopOff: off, Err: err}
	}
	b, ok := binBlocker(err)
	if !ok {
		// Nothing moved, for a reason baton can't put in words: the bin's
		// own text names files, so it stays in Err (for logs).
		b = Blocker{Code: CodeRecycleFailed}
	}
	return &Error{Code: b.Code, Blockers: []Blocker{b}, Drive: b.Drive, Left: left, DesktopOff: off, Err: err}
}

// binBlocker says what a Recycle Bin refusal means for the user.
func binBlocker(err error) (Blocker, bool) {
	var e *recycle.Error
	switch {
	case errors.Is(err, recycle.ErrUnavailable):
		b := Blocker{Code: CodeNoRecycleBin}
		if errors.As(err, &e) {
			b.Drive = e.Drive
		}
		return b, true
	case errors.Is(err, recycle.ErrTooLong):
		return Blocker{Code: CodePathTooLong}, true
	case errors.Is(err, recycle.ErrInUse):
		return Blocker{Code: CodeInUse}, true
	}
	return Blocker{}, false
}

// unhide takes the conversation off the office's hidden sessions: there is
// nothing left to hide, and the list is capped. config.json is not written
// when it does not list it.
func (j *job) unhide() {
	path, lock := j.Paths.Config(), j.Paths.ConfigLock()
	c, err := config.Load(path)
	if err != nil || c.OfficeView == nil || !slices.ContainsFunc(c.OfficeView.HiddenSessions, j.is) {
		return
	}
	_, _ = config.Update(path, lock, func(c *config.Config) error {
		if c.OfficeView == nil {
			return errUnchanged
		}
		var drop []string
		for _, s := range c.OfficeView.HiddenSessions {
			if j.is(s) {
				drop = append(drop, s)
			}
		}
		if len(drop) == 0 {
			return errUnchanged
		}
		// SetHidden compares exactly; a hand edit may have capitals.
		for _, s := range drop {
			c.OfficeView.SetHidden(false, s, false)
		}
		c.OfficeView = c.OfficeView.Normalized()
		return nil
	})
}

var errUnchanged = errors.New("unchanged")

func (j *job) is(id string) bool { return strings.EqualFold(id, j.id) }

// reopened: a name of the conversation is there again, or a Claude Code has
// it open: one that opened it while it went writes a new file.
func (j *job) reopened() bool {
	var f found
	for _, d := range j.dirs {
		j.scanProjects(d, &f)
	}
	return len(f.names) > 0 || len(claude.OpenElsewhere(j.dirs, j.id)) > 0
}

// supervisorAlive: the process pid runs and was created at created (to
// the second), as the window checks a run's supervisor.
func supervisorAlive(pid int, created time.Time) bool {
	got := winproc.ProcessCreated(pid)
	if got.IsZero() {
		return false
	}
	d := got.Sub(created)
	return d > -time.Second && d < time.Second
}
