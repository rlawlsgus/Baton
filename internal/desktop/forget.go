package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"baton/internal/fsx"
)

// What deleting a conversation needs of the desktop side. The desktop app
// opens a conversation from any record in any of its lists that names it,
// also as an earlier part of a session (priorCliSessionIds, rewindEdges and
// the like), so a conversation the app still has goes from the app first:
// the user deletes it there. baton takes off only its own records, on lists
// the running app does not hold in memory, and leaves everything of the
// app's as it is: its records, the deleted_<id> it leaves for a delete, and
// the <id>.desktop-released.json next to the conversation.

// Blocker is why the desktop side keeps a conversation from being deleted
// now, in a word the window explains.
type Blocker struct {
	// Code: desktop_app (a record of the app's names it: the user deletes it
	// there), desktop_busy (the running app holds a list of baton's that
	// shows it, or wrote baton's record back), desktop_waiting (another
	// session's record of baton's still points at it, until the app lets go
	// of that list) or cannot_check (a list or record of the app's could not
	// be read right now).
	Code string `json:"code"`
	// Detail, of desktop_app: own (made or imported in the app), archived
	// (the app's own, archived there), released (a record of baton's the app
	// took over), or prior (an earlier part of one of the app's sessions).
	Detail string `json:"detail,omitempty"`
}

// ForgetResult is what Forget found, and did unless it was asked only to
// look or something blocks.
type ForgetResult struct {
	Blockers []Blocker `json:"blockers"`
	// Off are the lists baton takes the conversation off (took it off), by
	// the names of baton's accounts on them; "" is the desktop app's own
	// account, which the caller words.
	Off []string `json:"off"`
	// Links are the second names baton gave the conversation's file in the
	// user's own folder that are still there afterwards (looking: now).
	// Something still keeps them (a Claude Code has the conversation open by
	// that name, a cut-off run offers it): a delete takes them first, with the
	// rest of the file's names.
	Links      []string `json:"links"`
	AppRunning bool     `json:"app_running"`
}

// Blocker codes and the details of desktop_app.
const (
	blockApp     = "desktop_app"
	blockBusy    = "desktop_busy"
	blockWaiting = "desktop_waiting"
	blockCheck   = "cannot_check"
)

// blockerOrder is the order blockers are given in: what the user does in
// the desktop app comes before what goes once the app lets go, and what
// could not be looked at last.
var blockerOrder = []Blocker{
	{blockApp, "own"}, {blockApp, "archived"}, {blockApp, "released"}, {blockApp, "prior"},
	{blockBusy, ""}, {blockWaiting, ""}, {blockCheck, ""},
}

// Forget takes conversation id off the desktop app as far as baton put it
// there, so the conversation can be deleted: baton's records of the session
// on it go (Gone keeps them known, so a copy the app writes back later goes
// too), and its second name once nothing uses it. It refuses, and changes
// nothing, while any list of any data folder of the app has a record of its
// own that names id, or the running app holds what would change. dry: only
// look (the index is not even settled on disk).
func (d *Desk) Forget(id string, dry bool) (ForgetResult, error) {
	id = strings.ToLower(id)
	if !validID(id) {
		return ForgetResult{Blockers: []Blocker{}, Off: []string{}, Links: []string{}}, fail("no_file", id)
	}
	if !isFile(d.IndexPath) {
		// baton never showed anything: there is nothing of baton's to take
		// off, only the app's lists to look through.
		ix := Index{Schema: 1}
		return d.judge(&ix, d.look(&ix), id), nil
	}
	var res ForgetResult
	err := d.update(func(ix *Index) error {
		before, err := json.Marshal(ix)
		if err != nil {
			return err
		}
		pr := d.look(ix)
		// What the app did since the last look is noted first, as Apply does:
		// a record it deleted is not baton's to take off, one it took over is
		// the app's.
		d.settleAll(ix, nil, pr)
		res = d.judge(ix, pr, id)
		if dry || len(res.Blockers) > 0 {
			// Not even what settle noted is saved: a look or a refusal leaves
			// the index as it was.
			var was Index
			if err := json.Unmarshal(before, &was); err != nil {
				return err
			}
			for _, t := range was.Threads {
				t.init()
			}
			*ix = was
			return nil
		}
		err = d.takeOff(ix, ix.Thread(id))
		res.Links = linksOf(ix, id)
		return err
	})
	return res, err
}

// takeOff takes thread t's records off the lists (judge found that safe)
// and then its second names nothing uses any more. A record the user
// deleted in the app stays deleted: its place stays, as it tells other
// threads whose delete a deleted_ was.
func (d *Desk) takeOff(ix *Index, t *Thread) error {
	if t == nil {
		return nil
	}
	if d.Data() != "" {
		keys := make([]string, 0, len(t.Places))
		for k := range t.Places {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, key := range keys {
			p := t.Places[key]
			if p.State != stateShown {
				continue
			}
			if p.Written > 0 {
				if err := removeFile(recordFile(p.Dir, p.Record)); err != nil {
					return err
				}
				// Known as baton's still, so a copy the running app writes back
				// (it may run unnoticed) is taken off again.
				ix.forget(t, p, d.Now().UnixMilli(), false)
			}
			// Not written yet: a file of that name is someone else's.
			delete(t.Places, key)
		}
	}
	d.dropLinks(ix, t)
	return nil
}

// judge says what keeps conversation id from going, from the settled index
// and every list of every data folder of the desktop app, and which of
// baton's lists it is taken off.
func (d *Desk) judge(ix *Index, pr probe, id string) ForgetResult {
	res := ForgetResult{Blockers: []Blocker{}, Off: []string{}, AppRunning: pr.running}
	add := func(code, detail string) {
		if b := (Blocker{code, detail}); !slices.Contains(res.Blockers, b) {
			res.Blockers = append(res.Blockers, b)
		}
	}
	t := ix.Thread(id)
	if t != nil {
		for key, p := range t.Places {
			switch p.State {
			case stateShown:
				if d.Data() != "" {
					res.Off = append(res.Off, p.Label)
				}
				// The running app keeps that list in memory and would write
				// the record back; one not written yet it never had.
				if p.Written > 0 && !pr.safe(key) {
					add(blockBusy, "")
				}
			case stateHiding:
				add(blockBusy, "") // taken off, but the running app may write it back
			case stateReleased:
				add(blockApp, "released")
			}
		}
	}
	for _, o := range ix.Threads {
		for i := range o.Released {
			if q := &o.Released[i]; strings.EqualFold(q.Cli, id) && isFile(recordFile(q.Dir, q.Record)) {
				add(blockApp, "released")
			}
		}
		if o == t {
			continue
		}
		for _, q := range o.Places {
			if !strings.EqualFold(q.Cli, id) {
				continue
			}
			switch {
			case q.State == stateReleased && isFile(recordFile(q.Dir, q.Record)):
				add(blockApp, "released")
			case q.State == stateHiding, q.State == stateShown && q.Written > 0:
				// Another session's record, waiting to be pointed at the
				// conversation that session is on now (or written back).
				add(blockWaiting, "")
			}
		}
	}
	if ix.loaded(id) {
		add(blockBusy, "")
	}
	for i := range ix.Gone {
		g := &ix.Gone[i]
		if !g.knows(id) {
			continue
		}
		// A record of baton's it took off that the app wrote back: it goes
		// again once the app lets go of the list. One the app went on with
		// since is the app's, which the lists below say.
		path := recordFile(g.Dir, g.Record)
		h, ok, err := readHeadErr(path)
		switch {
		case err != nil:
			add(blockCheck, "")
		case isFile(path) && (!ok || !pr.goneTaken(g, h)):
			add(blockBusy, "")
		}
	}
	dirs, unread := d.allData()
	if unread {
		add(blockCheck, "")
	}
	for _, data := range dirs {
		lookThrough(ix, pr, t, id, filepath.Join(data, sessionsDir), add)
	}
	slices.Sort(res.Off)
	res.Off = slices.Compact(res.Off)
	slices.SortStableFunc(res.Blockers, func(a, b Blocker) int {
		return slices.Index(blockerOrder, a) - slices.Index(blockerOrder, b)
	})
	res.Links = linksOf(ix, id)
	return res
}

// lookThrough reads every record in every list under root (a data folder's
// claude-code-sessions), also one the app left half written
// (local_*.json.tmp, which it makes a record at its next start), whole and
// whatever its size, and says what each one that names conversation id
// anywhere means. The id is matched in any case: a uuid is unique, and a
// match that is no reference only refuses. A list or record that cannot be
// read now may name it too.
func lookThrough(ix *Index, pr probe, t *Thread, id, root string, add func(code, detail string)) {
	readDir := func(dir string) []os.DirEntry {
		ents, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			add(blockCheck, "")
		}
		return ents
	}
	want := []byte(id)
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
				n := strings.ToLower(e.Name())
				tmp := strings.HasSuffix(n, ".json.tmp")
				if e.IsDir() || !strings.HasPrefix(n, "local_") || !tmp && !strings.HasSuffix(n, ".json") {
					continue
				}
				data, err := fsx.ReadFileShared(filepath.Join(dir, e.Name()))
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						add(blockCheck, "")
					}
					continue
				}
				if !bytes.Contains(bytes.ToLower(data), want) {
					continue
				}
				rec := e.Name()[:len(e.Name())-len(".json")]
				if tmp {
					rec = e.Name()[:len(e.Name())-len(".json.tmp")]
				}
				if code, detail := whose(ix, pr, t, id, dir, rec, tmp, data); code != "" {
					add(code, detail)
				}
			}
		}
	}
}

// whose says what record rec in list folder dir, which names conversation
// id, means for deleting it ("" code: nothing in the way). baton's own
// record (written by baton, not taken over) of the session on id goes with
// it; another session's names id only as an earlier conversation, unless it
// waits to be pointed on from it. A record of baton's it took off that the
// app wrote back is said by the Gone entries. Everything else is the
// desktop app's: also a half-written record, which baton never leaves (its
// own temporary files are named .baton-*).
func whose(ix *Index, pr probe, t *Thread, id, dir, rec string, tmp bool, data []byte) (code, detail string) {
	var h head
	ok := false
	if b := bytes.TrimSpace(fsx.StripBOM(data)); bytes.HasPrefix(b, []byte("{")) && json.Unmarshal(b, &h) == nil {
		ok = true
	}
	points := ok && strings.EqualFold(h.Cli, id)
	batons, gone := false, (*Gone)(nil)
	if !tmp {
		o, p := ix.record(dir, rec)
		// A place deleted in the app, or not written yet: a file of that
		// name is not baton's.
		batons = p != nil && p.Written > 0 && p.State != stateDeleted
		if batons && p.State != stateReleased && !pr.takenOver(o, p) {
			switch {
			case o == t:
				return "", ""
			case points || strings.EqualFold(p.Cli, id):
				return blockWaiting, ""
			}
			return "", ""
		}
		if gone = ix.gone(dir, rec); !batons && gone != nil && ok && gone.knows(id) && !pr.goneTaken(gone, h) {
			return "", ""
		}
	}
	switch {
	case ok && !points:
		return blockApp, "prior"
	case batons || gone != nil:
		return blockApp, "released"
	case ok && h.Archived:
		return blockApp, "archived"
	}
	return blockApp, "own"
}

// linksOf are the second names of conversation id's file baton's threads
// keep that are still there.
func linksOf(ix *Index, id string) []string {
	out := []string{}
	for _, t := range ix.Threads {
		for k, l := range t.Links {
			if !strings.EqualFold(k, id) || slices.ContainsFunc(out, func(p string) bool { return samePath(p, l.Path) }) {
				continue
			}
			if _, err := os.Lstat(l.Path); err == nil {
				out = append(out, l.Path)
			}
		}
	}
	slices.Sort(out)
	return out
}
