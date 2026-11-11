package desktop

import (
	"os"
	"slices"
	"strings"
)

// settle notes in the index what the desktop app did to thread t's records
// since baton last looked. It is the one place that judges it: every look
// (reconcile), Follow, Apply (before it reads the ticks) and the listing
// (Places, on a copy of the index it never saves) start here, so what the
// dialog shows is what any of them then does.
//
//   - A record the user deleted in the app (gone, and the app's deleted_<id>
//     newer than baton's write) is deleted: also one the app had taken over,
//     or moved on to a conversation of its own (whose deleted_ it is then:
//     noteMoved), and also while the app's Claude Code for it is still
//     registered (a delete wins). baton never writes it again unless it is
//     ticked again.
//   - A record of baton's the app took over (it runs it, completed a turn of
//     its own in it, or moved it on to a conversation of its own) is the
//     app's for good: released, still ticked. One unticked already (hiding)
//     is released with no tick: no copy follows the session there.
//   - A record of baton's it took off, which the app wrote back and went on
//     with since, pointing at the conversation baton's record there points
//     at now (the running app still listed it when the list was ticked
//     again), is the app's record of it there: baton's own goes, and the
//     place is the app's, released (takenTwin).
//   - A released record the session has moved on past (after /clear) stays
//     the app's, and the session gets a record of its own beside it, named
//     "<title> (baton)", which the next look writes: what Follow does when
//     it notices the take-over first.
//
// A record baton has not written yet is someone else's file: never taken
// over or deleted. settle only reads files.
//
// Apply and the listing settle every thread (settleAll), not only the one
// asked about.
func (d *Desk) settle(ix *Index, t *Thread, pr probe) {
	// The app's records of earlier conversations first: a delete that names
	// a conversation of the app's is theirs before it is anyone else's.
	for i := range t.Released {
		d.noteMoved(ix, t, &t.Released[i])
	}
	keys := make([]string, 0, len(t.Places))
	for k := range t.Places {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		p := t.Places[key]
		if p.State == stateShown || p.State == stateReleased {
			d.noteMoved(ix, t, p)
		}
		switch {
		case (p.State == stateShown || p.State == stateReleased) && deletedInApp(p):
			p.State = stateDeleted
			continue
		case p.State == stateHiding:
			// A record of that name another thread wrote since is not this
			// one's to call taken over (reconcile lets go of the name).
			if !ix.wroteElsewhere(t, p) && pr.takenOver(t, p) {
				t.release(key)
			}
			continue
		case p.State == stateShown && p.Written > 0 && pr.takenOver(t, p):
			p.State = stateReleased
		case p.State == stateShown && p.Written > 0:
			if rec := ix.takenTwin(pr, p); rec != "" {
				// Never two records of one conversation in one list: baton's
				// own goes (a look takes it off once that is safe), and the
				// place is the app's record, which baton wrote once.
				now := d.Now().UnixMilli()
				ix.forget(t, p, now, !pr.safe(key))
				p = &Place{Key: key, Dir: p.Dir, Record: rec, Cli: p.Cli, State: stateReleased, Label: p.Label, Written: now}
				t.Places[key] = p
			}
		}
		if p.State == stateReleased && movedPast(t, p) {
			t.Released = append(t.Released, *p)
			t.Places[key] = &Place{Key: key, Dir: p.Dir, Record: "local_" + t.Session, Cli: t.Session, State: stateShown,
				Label: p.Label, Copy: true}
		}
	}
	// An earlier record the app took over, deleted there since: nothing of
	// it is left to keep (the app removed its name for the conversation too).
	t.Released = slices.DeleteFunc(t.Released, func(q Place) bool { return deletedInApp(&q) })
}

// settleAll settles thread t (nil: none) and then every other thread, as
// Apply and the listing do. A tick the user gives clears what a delete in
// the app left for the conversation (deleted_<id>), which can be the only
// sign that another thread's record there was deleted: the session went on
// from that conversation (/clear) while the running app had the list, so
// its record still pointed at it. Settled first, that record is deleted
// for good, and the next look never writes it again. t comes first, so the
// listing and Apply guess alike which delete is whose (noteMoved).
func (d *Desk) settleAll(ix *Index, t *Thread, pr probe) {
	if t != nil {
		d.settle(ix, t, pr)
	}
	for _, o := range ix.Threads {
		if o != t {
			d.settle(ix, o, pr)
		}
	}
}

// deletedInApp: the user deleted baton's record p in the desktop app since
// baton wrote it (it is gone, and the app's deleted_<id> holds a later
// time: for the record, its conversation, or a conversation of the app's
// own it was moved on to), also after the app took it over.
func deletedInApp(p *Place) bool {
	return p.Written > 0 && !isFile(recordFile(p.Dir, p.Record)) &&
		tombstone(p.Dir, append([]string{p.Record, p.Cli}, p.Moved...)...) > p.Written
}

// noteMoved notes on baton's record p a conversation of the desktop app's
// own it moved p on to (Place.Moved): p points at one baton's thread never
// had. Deleting p in the app then leaves deleted_<that one>, which names
// nothing of baton's.
//
// When baton never saw the move (p is gone already, with no deleted_ of its
// own nor of a conversation it saw p moved on to), the delete is still the
// app's: a conversation the app deleted in p's list since baton wrote p is
// taken for the one p was moved on to when it is of the thread's project
// (the app's <id>.desktop-released.json is next to the conversation's name
// in the user's own folder) and nothing else of baton's has it (claimed).
// One delete leaves one deleted_, so p takes one only: the latest, as the
// user's delete of p comes after anything the app deleted there before it.
// Any other is another record's (perhaps another of baton's missing there,
// which takes it when it is looked at) or the app's own. A record merely
// missing, in a list where only another project's conversation was deleted
// since, is not deleted.
func (d *Desk) noteMoved(ix *Index, t *Thread, p *Place) {
	if p.Written == 0 {
		return // not written by baton yet: a file of that name is someone else's
	}
	path := recordFile(p.Dir, p.Record)
	h, ok, err := readHeadErr(path)
	switch {
	case err != nil:
		return // cannot be read right now: says nothing yet
	case ok:
		if h.Cli != "" && !t.knows(h.Cli) {
			p.addMoved(h.Cli)
		}
		return
	case isFile(path) || deletedInApp(p):
		return // no record (yet), or deleted with a deleted_ that names it
	}
	best, at := "", int64(0)
	ents, _ := os.ReadDir(p.Dir)
	for _, e := range ents {
		id, ok := strings.CutPrefix(e.Name(), "deleted_")
		id = strings.ToLower(id)
		if !ok || e.IsDir() || !validID(id) || p.movedTo(id) || ix.claimed(id, p) || !isFile(d.releasedMarker(t, id)) {
			continue
		}
		if ms := tombstone(p.Dir, id); ms > p.Written && ms > at {
			best, at = id, ms
		}
	}
	if best != "" {
		p.addMoved(best)
	}
}

func (p *Place) addMoved(id string) {
	if !p.movedTo(id) {
		p.Moved = append(p.Moved, strings.ToLower(id))
	}
}

// takenTwin is the name of a record of baton's it took off p's list (Gone)
// that the desktop app wrote back and went on with since (goneTaken),
// pointing at the conversation p's record points at: baton took it off
// while the running app still listed it there, and wrote p under another
// name, so the list has two records of the conversation, the app's and
// baton's. "" when there is none. A record a thread names again is that
// thread's, not a twin.
func (ix *Index) takenTwin(pr probe, p *Place) string {
	for i := range ix.Gone {
		g := &ix.Gone[i]
		if g.Key != p.Key || !samePath(g.Dir, p.Dir) || strings.EqualFold(g.Record, p.Record) || ix.wrote(g.Dir, g.Record) {
			continue
		}
		if h, ok := readHead(recordFile(g.Dir, g.Record)); ok && strings.EqualFold(h.Cli, p.Cli) && pr.goneTaken(g, h) {
			return g.Record
		}
	}
	return ""
}

// movedPast: the session went on (after /clear) past released record p,
// which points at an earlier conversation of the session's or at one of the
// app's, not at the one the session is on now. A record that cannot be read
// right now says nothing yet.
func movedPast(t *Thread, p *Place) bool {
	if strings.EqualFold(p.Cli, t.Session) {
		return false
	}
	h, ok, err := readHeadErr(recordFile(p.Dir, p.Record))
	return err == nil && (!ok || !strings.EqualFold(h.Cli, t.Session))
}

// goneTaken: record g, which baton took off a list and the desktop app
// wrote back (its file reads h), is the app's now: it points at a
// conversation baton's thread never had, the app completed a turn of its
// own in it, or the app's Claude Code runs it.
func (pr probe) goneTaken(g *Gone, h head) bool {
	return !g.knows(h.Cli) || h.wentOn() || pr.hosts[strings.ToLower(g.Record)] && (pr.active == "" || pr.isActive(g.Key))
}
