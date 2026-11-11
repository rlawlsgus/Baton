package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"baton/internal/claude"
)

// probe is what baton looks at once before it changes lists: whether the
// desktop app runs, which account it is signed in to, and which of its
// records a Claude Code of the app runs for now.
type probe struct {
	running   bool
	active    string // the account the app is signed in to ("" not known)
	activeOrg string // its organization, when that is plain ("" not known)
	// activeEmail: its email address, as the user's own Claude Code has it
	// (shown in the window, never kept).
	activeEmail string
	hosts       map[string]bool
}

func (d *Desk) look(ix *Index) probe {
	pr := probe{running: d.Running != nil && d.Running(), hosts: map[string]bool{}}
	if d.Data() != "" {
		if pr.active = Active(d.Data()); pr.active != "" {
			pr.activeOrg, pr.activeEmail = d.appAccount(ix, pr.active)
		}
	}
	if d.UserHome != "" {
		for _, s := range claude.ReadSessions(d.UserHome) {
			if s.HostSessionID != "" && s.Alive() {
				pr.hosts[strings.ToLower(s.HostSessionID)] = true
			}
		}
	}
	return pr
}

// isActive: key is a list of the account the desktop app is signed in to,
// in any of its organizations. Which organization the app is on is never
// known for sure (the user's own Claude Code, which names one, can be on
// another), so for what baton may change on its own every one counts.
func (pr probe) isActive(key string) bool {
	acct, _, _ := strings.Cut(key, "/")
	return pr.active != "" && acct == pr.active
}

// isCurrent: key is the list the app shows when it runs: the account it is
// signed in to, in the organization it is on when that is plain (every
// organization of it when not). The window says which one that is.
func (pr probe) isCurrent(key string) bool {
	_, org, _ := strings.Cut(key, "/")
	return pr.isActive(key) && (pr.activeOrg == "" || org == pr.activeOrg)
}

// safe: baton may change the list of key on its own. The running app keeps
// the list of its account in memory and writes records back from there; it
// reads any other list again when it switches to that account. While it
// runs and its account is not known, no list is safe.
func (pr probe) safe(key string) bool {
	if !pr.running {
		return true
	}
	return pr.active != "" && !pr.isActive(key)
}

// takenOver reports whether the desktop app took baton's record p over: a
// Claude Code of the app runs for it, or the app moved it on to a
// conversation baton's session never had (the user went on, or cleared it,
// in the app). baton never touches such a record again. One the user
// deleted in the app is gone, not taken over, even while the app's Claude
// Code for it is still registered.
func (pr probe) takenOver(t *Thread, p *Place) bool {
	if deletedInApp(p) {
		return false
	}
	// Record ids repeat from list to list; the app runs sessions of the
	// list it is signed in to (rarely, one left running from another).
	if pr.hosts[strings.ToLower(p.Record)] && (pr.active == "" || pr.isActive(p.Key)) {
		return true
	}
	// It went on with the record in a conversation of its own, or completed a
	// turn of its own in it (which it notes in the record for good, so this
	// holds after the app's Claude Code is gone too).
	h, ok := readHead(recordFile(p.Dir, p.Record))
	return ok && (h.Cli != "" && !t.knows(h.Cli) || h.wentOn())
}

// Follow moves the thread of conversation oldID on to newID, the
// conversation baton's session goes on in now (after /clear, or in a copy
// because the conversation was opened elsewhere); transcript is its file,
// if known yet. Each list keeps its record, which points to the new
// conversation once that has a file (the old id goes into the record's
// priorCliSessionIds, as the desktop app does). A record the app has open
// or took over stays the app's, and baton's session gets a record of its
// own beside it there, named "<title> (baton)"; one deleted in the app
// stays deleted (see settle).
func (d *Desk) Follow(oldID, newID, transcript string) error {
	if oldID == "" || newID == "" || !isFile(d.IndexPath) {
		return nil
	}
	return d.update(func(ix *Index) error {
		t := ix.Thread(oldID)
		if t == nil || ix.Thread(newID) != nil && !strings.EqualFold(oldID, newID) {
			return nil
		}
		pr := d.look(ix)
		if !strings.EqualFold(oldID, newID) {
			t.Prior = append(t.Prior, t.Session)
			t.Session = strings.ToLower(newID)
			t.Transcript = absPath(transcript)
			// For the conversation the session is on now: a record the app
			// took over gets the session's "(baton)" record beside it.
			d.settle(ix, t, pr)
			// The index says the thread moved on before any record of baton's
			// is pointed at the new conversation: a stop in between never
			// leaves one pointing at a conversation the index does not know
			// (which would then be taken for one the desktop app took over).
			if err := d.save(ix); err != nil {
				return err
			}
		} else if transcript != "" {
			t.Transcript = absPath(transcript)
		}
		d.reconcile(ix, t, pr)
		return nil
	})
}

// Touch looks at the thread of conversation id again after a turn (its
// file may have moved: a worktree moves it with its folder) and brings its
// records up to date.
func (d *Desk) Touch(id, transcript string) error {
	if id == "" || !isFile(d.IndexPath) {
		return nil
	}
	return d.update(func(ix *Index) error {
		t := ix.Thread(id)
		if t == nil {
			return nil
		}
		if transcript != "" && isFile(transcript) {
			t.Transcript = absPath(transcript)
		}
		d.reconcile(ix, t, d.look(ix))
		return nil
	})
}

// Reconcile brings every list in line with baton's index: the window does
// it when it opens, every few seconds while it is open, and after a change.
// It costs a look at the index when baton shows nothing.
func (d *Desk) Reconcile() error {
	if !isFile(d.IndexPath) {
		return nil
	}
	if ix, err := LoadIndex(d.IndexPath); err != nil || len(ix.Threads) == 0 && len(ix.Gone) == 0 || d.Data() == "" {
		return err
	}
	return d.update(func(ix *Index) error {
		pr := d.look(ix)
		for _, t := range ix.Threads {
			d.reconcile(ix, t, pr)
		}
		d.settleGone(ix, pr)
		d.sweepTemp(ix)
		return nil
	})
}

// reconcile brings one thread's records in line. On its own, baton changes
// only lists that are safe to change: it removes a record of its own the
// app wrote back for a list that was unticked, writes a record again that
// went missing (not deleted in the app), points records at the
// conversation the session is on now and keeps their time and name
// current. In any list it notices what the app did: a delete, a take-over
// (settle), and a conversation file that split from its second name. It
// writes new records (ticked, waiting for their conversation's file) in any
// list: the running app does not have those in memory.
func (d *Desk) reconcile(ix *Index, t *Thread, pr probe) {
	d.locate(t)
	t.checkSplit()
	d.settle(ix, t, pr)
	var a about
	ready := !t.Split && t.Transcript != "" && isFile(t.Transcript)
	if ready {
		a = describe(t.Transcript)
		if a.cwd != "" {
			t.Cwd = a.cwd
		}
		if a.title != "" {
			t.Title = a.title
		}
		ready = a.cwd != ""
	}
	if ready && t.shown() && d.ensureLink(t, t.Session, t.Transcript) != nil {
		ready = false
	}
	for key, p := range t.Places {
		if p.State != stateShown && p.State != stateHiding {
			continue
		}
		path := recordFile(p.Dir, p.Record)
		_, err := os.Stat(path)
		present := err == nil
		switch {
		case p.State == stateHiding && ix.wroteElsewhere(t, p):
			// Another thread's record has that name now: not this one's to
			// take off, nor to call taken over.
			delete(t.Places, key)
			continue
		case p.State == stateShown && p.Written == 0:
			if present || ix.wroteElsewhere(t, p) {
				// Not written by baton yet, so a file of that name is
				// someone else's (or another thread's record): baton's
				// record takes another name.
				p.Record, present = "local_"+newID(), false
				path = recordFile(p.Dir, p.Record)
			}
		}
		safe := pr.safe(key)
		if p.State == stateHiding {
			if safe && (!present || removeFile(path) == nil) {
				ix.forget(t, p, d.Now().UnixMilli(), false)
				delete(t.Places, key)
			}
			continue
		}
		if !present {
			switch {
			case deletedInApp(p):
				p.State = stateDeleted // deleted in the app since settle looked
			case ready && (p.Written == 0 || safe) && !ix.wroteElsewhere(t, p) && !ix.pointedAt(t, p.Dir, t.Session):
				if d.makeDir(ix, key, p.Dir) == nil {
					_ = d.writePlace(ix, t, p, a)
				}
			}
			continue
		}
		if safe && ready {
			d.refresh(t, p, a)
		}
	}
	d.dropLinks(ix, t)
}

// wroteElsewhere: another thread's record has p's file name, also one the
// desktop app took over from it (never written over, taken or removed).
func (ix *Index) wroteElsewhere(t *Thread, p *Place) bool {
	for _, o := range ix.Threads {
		if o == t {
			continue
		}
		for _, q := range o.Places {
			if strings.EqualFold(q.Record, p.Record) && samePath(q.Dir, p.Dir) {
				return true
			}
		}
		for _, q := range o.Released {
			if strings.EqualFold(q.Record, p.Record) && samePath(q.Dir, p.Dir) {
				return true
			}
		}
	}
	return false
}

// pointedAt: another thread's record of baton's in list folder dir points at
// conversation id: one waiting to be pointed at the conversation its
// session is on now, one the app wrote back after it was unticked, or one
// the app took over. One that cannot be read right now (another program
// holds it) may point at it too. A record of id's own waits there until
// none does: never two records of one conversation in one list. It reads
// the records of other threads in that one folder (cached).
func (ix *Index) pointedAt(t *Thread, dir, id string) bool {
	at := func(q *Place) bool {
		if q.State == stateDeleted || q.Written == 0 || !samePath(q.Dir, dir) {
			return false
		}
		h, ok, err := readHeadErr(recordFile(q.Dir, q.Record))
		return err != nil || ok && strings.EqualFold(h.Cli, id)
	}
	for _, o := range ix.Threads {
		if o == t {
			continue
		}
		for _, q := range o.Places {
			if at(q) {
				return true
			}
		}
		for i := range o.Released {
			if at(&o.Released[i]) {
				return true
			}
		}
	}
	return false
}

// refresh points baton's record at the conversation the session is on now
// and brings its last activity and name up to date. Every other field
// stays as it is; a name the user gave it in the app stays too. The index
// says the record points at the new conversation only once it does, so the
// old conversation's second name stays while the record still needs it.
func (d *Desk) refresh(t *Thread, p *Place, a about) {
	path := recordFile(p.Dir, p.Record)
	h, ok := readHead(path)
	if !ok {
		return
	}
	if strings.EqualFold(h.Cli, t.Session) {
		// Pointed on already (a stop came before the index said so).
		p.Cli = t.Session
	}
	title := a.title
	if p.Copy {
		title += " (baton)"
	}
	stale := !strings.EqualFold(h.Cli, t.Session)
	auto := h.TitleSource == "" || h.TitleSource == "auto"
	if !stale && number(h.Last) >= a.last && (!auto || h.Title == title || title == "") {
		return
	}
	err := rewrite(path, func(m map[string]any) {
		if stale {
			prior, _ := m["priorCliSessionIds"].([]any)
			had := false
			for _, v := range prior {
				if s, _ := v.(string); strings.EqualFold(s, h.Cli) {
					had = true
				}
			}
			if !had && h.Cli != "" {
				prior = append(prior, h.Cli)
			}
			m["priorCliSessionIds"] = prior
			m["cliSessionId"] = t.Session
		}
		if number(m["lastActivityAt"]) < a.last {
			m["lastActivityAt"] = a.last
		}
		if auto && title != "" {
			m["title"] = title
		}
	})
	if err == nil && stale {
		p.Cli = t.Session
	}
}

// settleGone takes records of baton's off again that the desktop app wrote
// back after baton took them off, once that is safe. One the app went on
// with since (it points at a conversation baton never had, the app
// completed a turn of its own in it, or runs it: goneTaken) is the app's:
// baton forgets it, as it does every one after goneFor
// (not while the running app still has it in memory). Once the list is safe
// the app let go of it: what it had there is on disk, read there from then
// on, and the record no longer keeps its conversations' second names.
func (d *Desk) settleGone(ix *Index, pr probe) {
	now := d.Now()
	ix.Gone = slices.DeleteFunc(ix.Gone, func(g Gone) bool {
		if !g.Loaded && now.Sub(time.UnixMilli(g.At)) > goneFor || ix.owns(g.Dir, g.Record) {
			return true
		}
		path := recordFile(g.Dir, g.Record)
		h, ok := readHead(path)
		switch {
		case !ok:
			return false // not there: nothing to do
		case pr.goneTaken(&g, h):
			return true
		}
		if pr.safe(g.Key) {
			_ = removeFile(path)
		}
		return false
	})
	// After the look above, so one written back as the app quit is taken off
	// before the record can expire.
	for i := range ix.Gone {
		if ix.Gone[i].Loaded && pr.safe(ix.Gone[i].Key) {
			ix.Gone[i].Loaded = false
		}
	}
}

// sweepTemp removes baton's own temporary files left in the lists it
// writes (a write cut off by a crash), once they are an hour old. Nothing
// else is ever removed there this way.
func (d *Desk) sweepTemp(ix *Index) {
	dirs := map[string]string{}
	for _, t := range ix.Threads {
		for _, p := range t.Places {
			dirs[strings.ToLower(p.Dir)] = p.Dir
		}
	}
	for _, dir := range dirs {
		m, _ := filepath.Glob(filepath.Join(dir, ".baton-*.tmp"))
		for _, f := range m {
			if time.Since(modTime(f)) > time.Hour {
				_ = removeFile(f)
			}
		}
	}
}

// makeDir makes the folder of list key when the desktop app has none yet,
// and notes that baton made it.
func (d *Desk) makeDir(ix *Index, key, dir string) error {
	if isDir(dir) {
		return nil
	}
	if err := mkdirAll(dir); err != nil {
		return err
	}
	if !ix.made(key) {
		ix.Made = append(ix.Made, key)
	}
	return nil
}

// appUsed reports whether the desktop app uses the list folder dir of key:
// it made the folder, or has something of its own in one baton made (any
// file but baton's records and temporary files).
func (ix *Index) appUsed(key, dir string) bool {
	if !isDir(dir) {
		return false
	}
	if !ix.made(key) {
		return true
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".baton-") {
			continue
		}
		if rec, ok := strings.CutSuffix(n, ".json"); ok && strings.HasPrefix(rec, "local_") && (ix.wrote(dir, rec) || ix.gone(dir, rec) != nil) {
			continue
		}
		return true
	}
	return false
}

// newID is a random uuid, for a record whose usual name is taken.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// absPath is p made absolute ("" stays "").
func absPath(p string) string {
	if p == "" {
		return ""
	}
	return cleanPath(p)
}
