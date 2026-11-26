package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Every conversation on this PC in every account's list (the setting
// desktop_show_all): baton lists each conversation the user's Claude Code
// folders hold in the list of every account the desktop app can be signed
// in to, so whichever account it is on, the same conversations show.
//
// Such a record is baton's like any other (a thread of its own, the second
// name, settle and reconcile), marked Auto: it was not a tick the user gave,
// and turning the setting off takes exactly those off again. The setting
// never goes against the user: a list the user took a conversation off in
// baton (Thread.Off), deleted it in the desktop app, or where the app has
// a record of its own for it, is left as it is; and nothing baton or the app
// put somewhere already is changed.

// AllResult says what ShowAll or HideAll did.
type AllResult struct {
	Added   int `json:"added"`   // records written
	Removed int `json:"removed"` // records taken off (or to go once the app is quit)
	// Conversations: how many conversations got a record somewhere new.
	Conversations int  `json:"conversations"`
	AppRunning    bool `json:"app_running"`
	// ActiveAdded: one went into the list of the account the running app is
	// signed in to; it shows there once the app is opened again.
	ActiveAdded bool `json:"active_added"`
	// ActiveRemoved: one was taken off a list the running app has in memory;
	// it goes from the app once the app is quit (baton's window takes it off
	// again then).
	ActiveRemoved bool `json:"active_removed"`
	// More: there were more to write than one call writes (see ShowAll); the
	// next call goes on.
	More bool `json:"more"`
	// Left: records HideAll could not take off now (another program held
	// one); baton's window tries again while the setting is off.
	Left int `json:"left"`
}

// off reports whether the user took the thread's conversation off list key
// in baton (see Thread.Off).
func (t *Thread) off(key string) bool { return slices.Contains(t.Off, key) }

func (t *Thread) setOff(key string, on bool) {
	t.Off = slices.DeleteFunc(t.Off, func(k string) bool { return k == key })
	if on {
		t.Off = append(t.Off, key)
		slices.Sort(t.Off)
	}
}

// allRows are the lists the setting fills: every list a baton account has
// (as the dialog offers them), and every list of the desktop app's own,
// also of an account it was signed in to before that is none of baton's.
// A folder baton made itself counts only as one of its accounts'.
func (d *Desk) allRows(ix *Index, pr probe) []Row {
	var out []Row
	seen := map[string]bool{}
	for _, r := range d.rows(pr) {
		if r.Key == "" || r.Disabled || seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		out = append(out, r)
	}
	root := filepath.Join(d.Data(), sessionsDir)
	accts, _ := os.ReadDir(root)
	for _, a := range accts {
		if !a.IsDir() {
			continue
		}
		orgs, _ := os.ReadDir(filepath.Join(root, a.Name()))
		for _, o := range orgs {
			key := strings.ToLower(a.Name()) + "/" + strings.ToLower(o.Name())
			if !o.IsDir() || !validKey(key) || seen[key] || !ix.appUsed(key, filepath.Join(root, a.Name(), o.Name())) {
				continue
			}
			seen[key] = true
			out = append(out, Row{Key: key, AppOnly: true})
		}
	}
	return out
}

// listing is what one list holds, read once per call: the names of its
// files, the conversations its records point at, and the earlier ones they
// went on from.
type listing struct {
	names  map[string]bool // file names, lower case
	cli    map[string]bool // conversations its records point at
	unread bool            // a record or the folder could not be read now: anything may be there
}

func readListing(dir string, prior map[string]bool) *listing {
	l := &listing{names: map[string]bool{}, cli: map[string]bool{}}
	ents, err := os.ReadDir(dir)
	if err != nil {
		l.unread = !os.IsNotExist(err) // no folder yet: nothing there
		return l
	}
	for _, e := range ents {
		n := e.Name()
		l.names[strings.ToLower(n)] = true
		rec, ok := strings.CutSuffix(n, ".json")
		if !ok {
			rec, ok = strings.CutSuffix(n, ".json.tmp") // made a record at the app's next start
		}
		if e.IsDir() || !ok || !strings.HasPrefix(rec, "local_") {
			continue
		}
		h, ok, err := readHeadErr(filepath.Join(dir, n))
		switch {
		case err != nil:
			l.unread = true
		case ok:
			if h.Cli != "" {
				l.cli[strings.ToLower(h.Cli)] = true
			}
			for _, p := range h.prior() {
				prior[strings.ToLower(p)] = true
			}
		}
	}
	return l
}

// tombstoned: the user deleted a record of conversation id in this list in
// the desktop app (it left deleted_<id> or deleted_local_<id>).
func (l *listing) tombstoned(id string) bool {
	for _, n := range tombstoneNames("local_" + id) {
		if l.names[strings.ToLower(n)] {
			return true
		}
	}
	return false
}

// pending is a record ShowAll writes once the index names it.
type pending struct {
	t *Thread
	p *Place
	a about
}

// ShowAll lists every conversation of convs (newest first) in every list
// allRows names, where nothing says otherwise. A conversation gets no record
// in a list where:
//
//   - baton keeps a place for it already, in any state: shown, being taken
//     off, deleted in the app, taken over by the app (the user's say, and
//     the app's, stand);
//   - the user took it off there in baton (Thread.Off);
//   - a record there points at it already (the app's own, archived or not,
//     one it took over, another thread's of baton's), or one that cannot be
//     read now might;
//   - it was deleted there in the desktop app;
//   - it is an earlier conversation of a session (/clear, or a copy, went
//     on from it): a record names it in priorCliSessionIds, or a session of
//     baton's went on from it (Thread.Prior), whose choice of lists went on
//     with it;
//   - the app's own record of it, taken over from baton, is kept there.
//
// It skips a conversation whose folder is gone (the app cannot go on with
// it there), one being deleted, one whose file split from its second name,
// and one that cannot get a second name in the user's own folder. It goes
// through the conversations newest first and stops, saying More, once it
// wrote limit records (0: no limit; a conversation is never left half
// done). on (nil: always) is asked once the index is locked: a HideAll that
// took its turn first, for the setting turned off meanwhile, is never
// undone.
func (d *Desk) ShowAll(convs []Conversation, limit int, on func() bool) (AllResult, error) {
	var res AllResult
	if d.Data() == "" {
		return res, fail("no_desktop", "")
	}
	err := d.update(func(ix *Index) error {
		if on != nil && !on() {
			return nil
		}
		pr := d.look(ix)
		res.AppRunning = pr.running
		rows := d.allRows(ix, pr)
		if len(rows) == 0 {
			return nil
		}
		prior := map[string]bool{}
		lists := map[string]*listing{}
		for _, r := range rows {
			lists[r.Key] = readListing(d.placeDir(r.Key), prior)
		}
		// Every record name baton's threads have in each list, so a new one
		// never takes one of them (another thread's, or one the app took over).
		taken := map[string]bool{}
		threads := map[string]*Thread{}
		for _, t := range ix.Threads {
			if threads[t.Session] == nil {
				threads[t.Session] = t
			}
			for _, p := range t.Prior {
				prior[strings.ToLower(p)] = true
			}
			for _, p := range t.Places {
				taken[strings.ToLower(p.Key+"|"+p.Record)] = true
			}
			for _, p := range t.Released {
				taken[strings.ToLower(p.Key+"|"+p.Record)] = true
			}
		}
		for _, g := range ix.Gone {
			taken[strings.ToLower(g.Key+"|"+g.Record)] = true
		}
		now := d.Now().UnixMilli()
		var todo []pending
		for _, c := range convs {
			id := strings.ToLower(c.ID)
			if !validID(id) || prior[id] || !isFile(c.Transcript) || d.deleting(id) {
				continue
			}
			t := threads[id]
			if t != nil && t.Split {
				continue
			}
			var want []Row
			for _, r := range rows {
				l := lists[r.Key]
				switch {
				case l.unread || l.cli[id] || l.tombstoned(id):
				case t != nil && (t.Places[r.Key] != nil || t.off(r.Key) || t.keptByApp(r.Key)):
				default:
					want = append(want, r)
				}
			}
			if len(want) == 0 {
				continue
			}
			if limit > 0 && len(todo) >= limit {
				res.More = true
				break
			}
			a := describe(c.Transcript)
			if a.cwd == "" || !isDir(a.cwd) || d.linkProblem(c.Transcript) != "" {
				continue
			}
			made := t == nil
			if made {
				t = &Thread{Session: id, Transcript: absPath(c.Transcript), Cwd: a.cwd, Title: a.title}
				t.init()
				ix.Threads = append(ix.Threads, t)
				threads[id] = t
			}
			if err := d.ensureLink(t, id, c.Transcript); err != nil {
				continue // an empty thread made here goes at the end of the change
			}
			added := false
			for _, r := range want {
				dir := d.placeDir(r.Key)
				if err := d.makeDir(ix, r.Key, dir); err != nil {
					continue
				}
				l := lists[r.Key]
				rec := "local_" + id
				if l.names[strings.ToLower(rec+".json")] || l.names[strings.ToLower(rec+".json.tmp")] || taken[strings.ToLower(r.Key+"|"+rec)] {
					rec = "local_" + newID()
				}
				p := &Place{Key: r.Key, Dir: dir, Record: rec, Cli: id, State: stateShown, Label: strings.Join(r.Accounts, ", "),
					Written: now, Auto: true}
				t.Places[r.Key] = p
				taken[strings.ToLower(r.Key+"|"+rec)] = true
				l.names[strings.ToLower(rec+".json")] = true
				l.cli[id] = true
				todo = append(todo, pending{t, p, a})
				added = true
			}
			if added {
				res.Conversations++
			}
		}
		if len(todo) == 0 {
			return nil
		}
		// The index names every record before any is written (as writePlace
		// does for one): a stop in between never leaves a record of baton's
		// that looks like the desktop app's own.
		if err := d.save(ix); err != nil {
			for _, w := range todo {
				delete(w.t.Places, w.p.Key)
			}
			return err
		}
		for _, w := range todo {
			if err := writeRecord(recordFile(w.p.Dir, w.p.Record), newRecord(w.p.Record, w.t.Session, w.a.cwd, w.a.title, w.a.created, w.a.last, now), false); err != nil {
				// Not written: the index must not name it (update saves it again).
				delete(w.t.Places, w.p.Key)
				continue
			}
			res.Added++
			res.ActiveAdded = res.ActiveAdded || pr.isActive(w.p.Key)
		}
		return nil
	})
	return res, err
}

// HideAll takes off every record the setting put in the desktop app (Auto)
// that is still baton's, as an untick in the dialog does: one the desktop
// app took over stays the app's, and one in the list the running app has in
// memory goes from the app once it is quit. Records the user ticked stay.
// One that cannot be removed now (another program holds it) stays as it
// is, counted in Left, and the rest go.
func (d *Desk) HideAll() (AllResult, error) {
	var res AllResult
	if d.Data() == "" || !isFile(d.IndexPath) {
		return res, nil
	}
	err := d.update(func(ix *Index) error {
		pr := d.look(ix)
		res.AppRunning = pr.running
		// What the app did since the last look first, as Apply does: one it
		// took over is the app's now, one deleted there stays deleted.
		d.settleAll(ix, nil, pr)
		for _, t := range ix.Threads {
			keys := make([]string, 0, len(t.Places))
			for k, p := range t.Places {
				if p.Auto && p.State == stateShown {
					keys = append(keys, k)
				}
			}
			slices.Sort(keys)
			for _, key := range keys {
				p := t.Places[key]
				switch {
				case p.Written == 0:
					delete(t.Places, key) // not written yet: a file of that name is not baton's
					continue
				case pr.takenOver(t, p):
					t.release(key)
					continue
				}
				if err := removeFile(recordFile(p.Dir, p.Record)); err != nil {
					res.Left++ // as it was: tried again later
					continue
				}
				if pr.safe(key) {
					ix.forget(t, p, d.Now().UnixMilli(), false)
					delete(t.Places, key)
				} else {
					p.State = stateHiding // the running app may write it back from memory
					res.ActiveRemoved = true
				}
				res.Removed++
			}
			d.dropLinks(ix, t)
		}
		return nil
	})
	return res, err
}
