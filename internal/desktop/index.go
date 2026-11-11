package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"baton/internal/fsx"
)

// What a place's record is to baton.
const (
	stateShown    = "shown"          // baton shows the session there with a record of its own
	stateHiding   = "hiding"         // unticked: the record is gone, but the running app may write it back
	stateDeleted  = "deleted_in_app" // the user deleted baton's record in the desktop app
	stateReleased = "released"       // the desktop app took baton's record over: never touched again
)

// Index is baton's own list of what it put into the desktop app
// (desktop.json): every record it wrote, and the second names it gave
// conversation files. A record not listed here is the desktop app's.
type Index struct {
	Schema  int       `json:"schema"`
	Threads []*Thread `json:"threads"`
	// Gone are records of baton's it took off a list (unticked). The desktop
	// app can write such a record back from its memory later, when baton
	// could not tell that it had that list open: it is still baton's then,
	// and goes again once that is safe. Kept for goneFor.
	Gone []Gone `json:"gone,omitempty"`
	// Made are the lists (place keys) whose folder baton made because the
	// desktop app had none for that account yet. Such a list counts as used
	// by the app only once the app has something of its own in it.
	Made []string `json:"made,omitempty"`

	// saved is what Desk.save wrote to disk in the middle of a change (nil:
	// nothing yet), so update saves the index again when the change ended
	// otherwise (a record write that failed and was undone).
	saved []byte
}

// Gone is a record baton took off a list.
type Gone struct {
	Key    string   `json:"key"`
	Dir    string   `json:"dir"`
	Record string   `json:"record"`
	Cli    []string `json:"cli"` // the conversations it may point to (its thread's)
	At     int64    `json:"at"`  // when (ms)
	// Loaded: baton took it off a list the running app has in memory (the
	// list was ticked again before the app let go of the record). The app
	// still lists it there, may open its conversation through its second
	// name, take it over, and writes it back as it quits: until the app let
	// go of that list, the second names of Cli stay (see Desk.linkUsed).
	Loaded bool `json:"loaded,omitempty"`
}

// goneFor is how long baton remembers a record it took off a list.
const goneFor = 14 * 24 * time.Hour

func (g Gone) knows(id string) bool {
	return id != "" && slices.ContainsFunc(g.Cli, func(c string) bool { return strings.EqualFold(c, id) })
}

// forget notes that baton took record p of thread t off its list; loaded:
// the running app has that list in memory (see Gone.Loaded).
func (ix *Index) forget(t *Thread, p *Place, now int64, loaded bool) {
	ids := append([]string{t.Session}, t.Prior...)
	if p.Cli != "" && !slices.ContainsFunc(ids, func(c string) bool { return strings.EqualFold(c, p.Cli) }) {
		ids = append(ids, p.Cli)
	}
	ix.Gone = slices.DeleteFunc(ix.Gone, func(g Gone) bool { return strings.EqualFold(g.Record, p.Record) && samePath(g.Dir, p.Dir) })
	ix.Gone = append(ix.Gone, Gone{Key: p.Key, Dir: p.Dir, Record: p.Record, Cli: ids, At: now, Loaded: loaded})
}

// loaded reports whether a record of baton's it took off a list the running
// app still has in memory may point at conversation id (see Gone.Loaded).
func (ix *Index) loaded(id string) bool {
	return slices.ContainsFunc(ix.Gone, func(g Gone) bool { return g.Loaded && g.knows(id) })
}

// gone is baton's record id in folder dir that it took off, if it did.
func (ix *Index) gone(dir, record string) *Gone {
	for i := range ix.Gone {
		if strings.EqualFold(ix.Gone[i].Record, record) && samePath(ix.Gone[i].Dir, dir) {
			return &ix.Gone[i]
		}
	}
	return nil
}

// made reports whether baton made the folder of list key.
func (ix *Index) made(key string) bool {
	return slices.Contains(ix.Made, key)
}

// Thread is one baton session as the desktop app shows it. The choice of
// accounts belongs to the session, not to one conversation: when the
// session goes on in another conversation (/clear, a copy because the
// conversation was opened elsewhere) the thread moves with it, as the
// desktop app's own records do.
type Thread struct {
	Session    string            `json:"session"`         // the conversation the session is on now
	Prior      []string          `json:"prior,omitempty"` // the ones it was on before
	Transcript string            `json:"transcript"`      // the conversation's file ("" until it has one)
	Cwd        string            `json:"cwd,omitempty"`
	Title      string            `json:"title,omitempty"`
	Places     map[string]*Place `json:"places,omitempty"` // by place key
	// Released are earlier records the desktop app took over, kept so
	// their conversation files stay where the app finds them.
	Released []Place `json:"released,omitempty"`
	// Links are the second names baton gave conversation files in the
	// user's own Claude Code folder, by conversation id.
	Links map[string]Link `json:"links,omitempty"`
	// Split: a second name stopped being the same file as baton's (something
	// replaced one of them), so the two sides no longer see each other's
	// turns. baton then leaves the thread's records alone.
	Split bool `json:"split,omitempty"`
}

// Place is baton's record in one account's list of the desktop app.
type Place struct {
	Key     string `json:"key"`        // <accountUuid>/<orgUuid>
	Dir     string `json:"dir"`        // the list's folder
	Record  string `json:"record"`     // the record's id and file name (local_<id>.json)
	Cli     string `json:"cli"`        // the conversation baton wrote into it
	State   string `json:"state"`      // see the states above
	Written int64  `json:"written_at"` // when baton wrote it (ms; 0: not yet)
	Label   string `json:"label,omitempty"`
	// Copy: the record is for baton's copy of a conversation the desktop
	// app keeps in the same list, and is named "<title> (baton)".
	Copy bool `json:"copy,omitempty"`
	// Moved: conversations of the desktop app's own it moved the record on
	// to (the user went on with it in one, or cleared it there). Deleting
	// the record in the app leaves deleted_<the one it pointed at>, which
	// names none of baton's (see Desk.noteMoved).
	Moved []string `json:"moved,omitempty"`
}

// movedTo reports whether the desktop app moved record p on to
// conversation id.
func (p *Place) movedTo(id string) bool {
	return slices.ContainsFunc(p.Moved, func(m string) bool { return strings.EqualFold(m, id) })
}

// claimed reports whether conversation id is one of baton's (a thread was
// on it, or a record baton took off may point at it), or the desktop app
// moved a record of baton's other than p on to it: a delete in the app that
// names it is not p's.
func (ix *Index) claimed(id string, p *Place) bool {
	for _, t := range ix.Threads {
		if t.knows(id) {
			return true
		}
		for _, q := range t.Places {
			if q != p && q.movedTo(id) {
				return true
			}
		}
		for i := range t.Released {
			if q := &t.Released[i]; q != p && q.movedTo(id) {
				return true
			}
		}
	}
	return slices.ContainsFunc(ix.Gone, func(g Gone) bool { return g.knows(id) })
}

// Link is a second name of a conversation file.
type Link struct {
	Path string `json:"path"` // in the user's own Claude Code folder
	File string `json:"file"` // baton's name for it, in an account's profile
}

// LoadIndex reads the index; a missing one is empty.
func LoadIndex(path string) (Index, error) {
	ix := Index{Schema: 1}
	if err := fsx.ReadJSON(path, &ix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ix, err
	}
	for _, t := range ix.Threads {
		t.init()
	}
	return ix, nil
}

func (t *Thread) init() {
	if t.Places == nil {
		t.Places = map[string]*Place{}
	}
	if t.Links == nil {
		t.Links = map[string]Link{}
	}
}

// Thread is the thread baton's session is on with conversation id now.
func (ix *Index) Thread(id string) *Thread {
	for _, t := range ix.Threads {
		if id != "" && strings.EqualFold(t.Session, id) {
			return t
		}
	}
	return nil
}

// wentOnFrom are the threads that were on conversation id before the one
// they are on now (/clear, or a copy). An earlier conversation shown on its
// own is a thread of its own, which can go on as well.
func (ix *Index) wentOnFrom(id string) []*Thread {
	var out []*Thread
	for _, t := range ix.Threads {
		if id != "" && !strings.EqualFold(t.Session, id) && t.knows(id) {
			out = append(out, t)
		}
	}
	return out
}

// owns reports whether the record id in folder dir is baton's: written by
// baton and not taken over by the desktop app since.
func (ix *Index) owns(dir, record string) bool {
	for _, t := range ix.Threads {
		for _, p := range t.Places {
			if p.State != stateReleased && strings.EqualFold(p.Record, record) && samePath(p.Dir, dir) {
				return true
			}
		}
	}
	return false
}

// wrote reports whether baton ever wrote the record id in folder dir, also
// one the desktop app took over since.
func (ix *Index) wrote(dir, record string) bool {
	if ix.owns(dir, record) {
		return true
	}
	for _, t := range ix.Threads {
		for _, p := range t.Places {
			if strings.EqualFold(p.Record, record) && samePath(p.Dir, dir) {
				return true
			}
		}
		for _, p := range t.Released {
			if strings.EqualFold(p.Record, record) && samePath(p.Dir, dir) {
				return true
			}
		}
	}
	return false
}

// record is baton's record id in folder dir, with its thread, when baton
// wrote it: one baton still has first, else one the desktop app took over
// (State released).
func (ix *Index) record(dir, record string) (*Thread, *Place) {
	var rt *Thread
	var rp *Place
	for _, t := range ix.Threads {
		for _, p := range t.Places {
			if !strings.EqualFold(p.Record, record) || !samePath(p.Dir, dir) {
				continue
			}
			if p.State != stateReleased {
				return t, p
			}
			if rp == nil {
				rt, rp = t, p
			}
		}
		for i := range t.Released {
			if p := &t.Released[i]; rp == nil && strings.EqualFold(p.Record, record) && samePath(p.Dir, dir) {
				rt, rp = t, p
			}
		}
	}
	return rt, rp
}

// knows reports whether id is a conversation the thread was ever on.
func (t *Thread) knows(id string) bool {
	return strings.EqualFold(id, t.Session) || slices.ContainsFunc(t.Prior, func(p string) bool { return strings.EqualFold(p, id) })
}

// shown reports whether baton shows the thread's conversation anywhere.
func (t *Thread) shown() bool {
	for _, p := range t.Places {
		if p.State == stateShown {
			return true
		}
	}
	return false
}

// release keeps the place of key, which the user unticked while the desktop
// app had its record, only as an earlier record the app took over: its
// conversation file keeps its second name, the row stays the app's, and no
// copy follows the session into that list.
func (t *Thread) release(key string) {
	rel := *t.Places[key]
	rel.State = stateReleased
	t.Released = append(t.Released, rel)
	delete(t.Places, key)
}

// keptByApp: the desktop app keeps a record of the thread's it took over in
// the list of key, so a record of baton's there is its copy ("(baton)").
func (t *Thread) keptByApp(key string) bool {
	return slices.ContainsFunc(t.Released, func(q Place) bool { return q.Key == key && isFile(recordFile(q.Dir, q.Record)) })
}

// Badge is what a session's row in the window says about the desktop app.
type Badge struct {
	// Places: the lists that show it, by the names of baton's accounts on
	// them ("" for the desktop app's own account).
	Places []string `json:"places"`
	Split  bool     `json:"split,omitempty"`
}

// Badges are the window's badges, by conversation id in lower case: for
// every session shown in the desktop app, or whose file split.
func (ix Index) Badges() map[string]Badge {
	out := map[string]Badge{}
	for _, t := range ix.Threads {
		b := Badge{Places: []string{}, Split: t.Split}
		for _, p := range t.Places {
			if p.State == stateShown || p.State == stateReleased {
				b.Places = append(b.Places, p.Label)
			}
		}
		slices.Sort(b.Places)
		if len(b.Places) > 0 || b.Split {
			out[strings.ToLower(t.Session)] = b
		}
	}
	return out
}

// Listed reports whether the desktop app lists the thread's session for some
// account: with baton's record, or one the app took over.
func (t *Thread) Listed() bool {
	for _, p := range t.Places {
		if p.State == stateShown || p.State == stateReleased {
			return true
		}
	}
	return false
}

// Waiting reports whether a change to the list of the account the desktop
// app (data folder dataDir) is signed in to waits until the app is closed:
// a record of baton's to take off it, one to point at the conversation its
// session is on now, or one baton took off that the app wrote back or still
// has in memory (Gone.Loaded). The app writes that list back from memory as
// it quits and reads it again as soon as it is opened, so the window looks
// every second meanwhile.
func (ix Index) Waiting(dataDir string) bool {
	var keys []string
	for _, t := range ix.Threads {
		for k, p := range t.Places {
			stale := p.State == stateShown && p.Written > 0 && !strings.EqualFold(p.Cli, t.Session) &&
				!t.Split && isFile(t.Transcript)
			if p.State == stateHiding || stale {
				keys = append(keys, k)
			}
		}
	}
	for _, g := range ix.Gone {
		if g.Loaded || isFile(recordFile(g.Dir, g.Record)) {
			keys = append(keys, g.Key)
		}
	}
	if len(keys) == 0 {
		return false
	}
	active := Active(dataDir)
	return active == "" || slices.ContainsFunc(keys, func(k string) bool { return strings.HasPrefix(k, active+"/") })
}

// empty: nothing of the thread is left in the desktop app or on disk.
func (t *Thread) empty() bool {
	return len(t.Places) == 0 && len(t.Released) == 0 && len(t.Links) == 0
}

// update changes the index under its lock (the window and every session
// write it) and saves it when it is not on disk as it ended: it changed, or
// it was saved part way through (see Desk.save) and ended otherwise.
func (d *Desk) update(fn func(ix *Index) error) error {
	guard("lock", d.LockPath)
	unlock, err := fsx.Lock(d.LockPath)
	if err != nil {
		return err
	}
	defer unlock()
	ix, err := LoadIndex(d.IndexPath)
	if err != nil {
		return err
	}
	ondisk, _ := json.Marshal(ix)
	ferr := fn(&ix)
	ix.Threads = slices.DeleteFunc(ix.Threads, (*Thread).empty)
	after, _ := json.Marshal(ix)
	if ix.saved != nil {
		ondisk = ix.saved
	}
	if !bytes.Equal(ondisk, after) {
		if err := writeIndex(d.IndexPath, ix); err != nil && ferr == nil {
			ferr = err
		}
	}
	return ferr
}

// samePath reports whether a and b name one path, however each is spelled
// (8.3 short names, case, relative).
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return strings.EqualFold(longPath(a), longPath(b))
}
