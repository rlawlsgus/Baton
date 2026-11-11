package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"baton/internal/accounts"
	"baton/internal/claude"
)

// Row is one place a session can be shown: one account's list in the
// desktop app (an account and organization).
type Row struct {
	Key      string   `json:"key"`                // <accountUuid>/<orgUuid>; "" for a row that cannot be chosen
	Accounts []string `json:"accounts,omitempty"` // baton's accounts that are this Claude account
	Email    string   `json:"email,omitempty"`
	Org      string   `json:"org,omitempty"`
	AppOnly  bool     `json:"app_only,omitempty"` // the desktop app's own account, none of baton's
	Active   bool     `json:"active"`             // the desktop app is signed in to it now
	Used     bool     `json:"used"`               // the app has a list for it already
	Checked  bool     `json:"checked"`            // the session shows there
	Locked   bool     `json:"locked"`             // the desktop app's own record: baton leaves it as it is
	Disabled bool     `json:"disabled"`           // cannot be chosen
	// Why, for a locked or disabled row, or one with a note: own (made in
	// the desktop app), archived (the app's own, archived there), released
	// (the app took baton's record over), deleted (deleted in the app),
	// api_key, cloud (Provider says which) or unknown (no account ids yet).
	Why      string `json:"why,omitempty"`
	Provider string `json:"provider,omitempty"`
	Copy     bool   `json:"copy,omitempty"`     // the app keeps the original there; the tick is for baton's copy
	Archived bool   `json:"archived,omitempty"` // baton's record was archived in the app
	// Former: baton shows the session in this list, but the list is no
	// longer one of its accounts' (removed since) nor the app's current one;
	// it stays on offer so it can be unticked.
	Former bool `json:"former,omitempty"`
}

// View is what the window's dialog (and `baton desktop`) shows for a session.
type View struct {
	Session    string `json:"session_id"`
	Title      string `json:"title"`
	Rows       []Row  `json:"places"`
	AppRunning bool   `json:"app_running"`
	// Split: baton's conversation file and the desktop app's are no longer
	// one file (see Thread.Split); nothing can be added then.
	Split bool `json:"split"`
	// Problem: why it cannot be shown anywhere new (no_file, conflict,
	// other_drive, link_failed); "" when it can.
	Problem string `json:"problem,omitempty"`
	// Thread: baton has the session's thread on this conversation (it shows
	// it somewhere, or keeps something of it), and no other thread went on
	// from it before, so one that does while the dialog is open is this
	// one (see threadMark).
	Thread bool `json:"thread,omitempty"`
}

// Places is what can be chosen for conversation c, and where it shows now.
func (d *Desk) Places(c Conversation) (View, error) {
	if d.Data() == "" {
		return View{}, fail("no_desktop", "")
	}
	ix, err := LoadIndex(d.IndexPath)
	if err != nil {
		return View{}, err
	}
	pr := d.look(&ix)
	// What the app did since the last look, as the next look (or Apply)
	// notes it: on this copy of the index, which is never saved.
	d.settleAll(&ix, ix.Thread(c.ID), pr)
	return d.view(&ix, c, pr), nil
}

// keptPrefix marks a place a client sends back as its view showed it,
// ticked, and the user left it so (see Kept). The window's dialog spells it
// out too (app.js).
const keptPrefix = "kept:"

// Kept is how a client sends back place key when its view showed it ticked
// and the user left it so: Apply leaves that row as it is by then, which is
// not always as the view showed it. A row the desktop app took over since
// stays ticked, and one the user deleted in the app since (baton's record,
// one the app took over, or the app's own) stays deleted: it is never
// written again for a tick the user did not give. A plain key is a tick the
// user gave: it lists the session there (again).
func Kept(key string) string { return keptPrefix + key }

// threadMark is what a client sends back with the places when its view had
// the session's thread (View.Thread). The session can go on in another
// conversation while the dialog is open (/clear, or a copy because the app
// opened its record), and the dialog still names the one it was opened for:
// the ticks are the session's all the same (see Apply). A view of an earlier
// conversation, asked about on its own, has no thread, or one of its own
// that another went on from before: it is not sent then. The window's
// dialog spells it out too (app.js).
const threadMark = "thread"

// Ticked are the places a client sends back to leave view v as it is: every
// ticked row, also a locked one, each as Kept, and threadMark when the view
// had the session's thread. One the user unticked is left out (a row the app
// took over since the view was made is released then: the app keeps its
// record), and one the user ticked is sent as its key.
func (v View) Ticked() []string {
	var out []string
	for _, r := range v.Rows {
		if r.Key != "" && r.Checked {
			out = append(out, Kept(r.Key))
		}
	}
	if v.Thread {
		out = append(out, threadMark)
	}
	return out
}

// Named are the rows `baton desktop` means by name: one of baton's account
// names, or "desktop" for the desktop app's own account when it is none of
// baton's. The first is the one --show ticks: a row of the accounts comes
// before a former one (see Row.Former), which has the names it had, and a
// current row may have those now. An account of baton's named "desktop"
// is that account: the app's own then goes by no name.
func (v View) Named(name string) []Row {
	app := name == "desktop" && !slices.ContainsFunc(v.Rows, func(r Row) bool { return slices.Contains(r.Accounts, name) })
	var now, former []Row
	for _, r := range v.Rows {
		if !slices.Contains(r.Accounts, name) && !(r.AppOnly && app) {
			continue
		}
		if r.Former {
			former = append(former, r)
		} else {
			now = append(now, r)
		}
	}
	return append(now, former...)
}

// Hide is want, the places a client sends back for view v, without every
// row `baton desktop --hide name` takes the session off: each of Named that
// shows it and that the user may untick (not the desktop app's, locked).
// That is also a former row, which the listing prints under the names it
// had and says --hide takes off, when a current row has those names now.
// ok: one was taken off.
func (v View) Hide(want []string, name string) (out []string, ok bool) {
	out = want
	for _, r := range v.Named(name) {
		if r.Key == "" || !r.Checked || r.Locked || r.Disabled {
			continue
		}
		n := len(out)
		out = slices.DeleteFunc(out, func(k string) bool { return k == r.Key || k == Kept(r.Key) })
		ok = ok || len(out) < n
	}
	return out, ok
}

// view is what the dialog shows for conversation c, from the index as
// settle left its thread: it reads that state, and looks at a record of
// baton's only to say it was archived.
func (d *Desk) view(ix *Index, c Conversation, pr probe) View {
	v := View{Session: c.ID, AppRunning: pr.running, Title: describe(c.Transcript).title, Rows: []Row{}}
	t := ix.Thread(c.ID)
	v.Thread = t != nil && len(ix.wentOnFrom(c.ID)) == 0
	if t != nil {
		cp := Thread{Links: t.Links, Split: t.Split}
		cp.checkSplit() // looked at again each time the dialog opens
		v.Split = cp.Split
	}
	if !v.Split { // a split says it all: the second name is another file now
		v.Problem = d.linkProblem(c.Transcript)
	}
	rows := d.rows(pr)
	if t != nil {
		var keys []string
		for k, p := range t.Places {
			if p.State != stateHiding && !slices.ContainsFunc(rows, func(r Row) bool { return r.Key == k }) {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		var former []Row
		for _, k := range keys {
			r := Row{Key: k, Former: true, AppOnly: t.Places[k].Label == "", Active: pr.isCurrent(k)}
			if l := t.Places[k].Label; l != "" {
				r.Accounts = strings.Split(l, ", ")
			}
			former = append(former, r)
		}
		// Before the rows that cannot be chosen, which come last.
		at := slices.IndexFunc(rows, func(r Row) bool { return r.Disabled })
		if at < 0 {
			at = len(rows)
		}
		rows = slices.Insert(rows, at, former...)
	}
	for _, r := range rows {
		if r.Key != "" {
			dir := d.placeDir(r.Key)
			r.Used = ix.appUsed(r.Key, dir)
			if p := placeOf(t, r.Key); p != nil {
				switch p.State {
				case stateShown:
					r.Checked, r.Copy = true, p.Copy
					// One not written yet has no file of its own: a file of
					// that name is someone else's.
					if p.Written > 0 {
						if h, ok := readHead(recordFile(p.Dir, p.Record)); ok && h.Archived {
							r.Archived = true
						}
					}
				case stateReleased:
					r.Checked, r.Locked, r.Why = true, true, "released"
				case stateDeleted:
					r.Why = "deleted"
				}
			}
			if !r.Checked && !r.Locked && isDir(dir) {
				d.ownRecord(ix, pr, &r, dir, c.ID)
			}
			if !r.Checked && !r.Locked && (v.Split || v.Problem != "") {
				r.Disabled = true
			}
		}
		v.Rows = append(v.Rows, r)
	}
	return v
}

func placeOf(t *Thread, key string) *Place {
	if t == nil {
		return nil
	}
	return t.Places[key]
}

// ownRecord marks the row when the desktop app has a record of its own for
// the conversation there: one made there or imported by the app, or one of
// baton's the app took over (released, run by the app, or moved on to a
// conversation of the app's), whichever of baton's sessions it was written
// for. baton never hides or changes that one, and never puts a second
// record of the conversation beside it (the row is locked, so a tick does
// nothing). A record that is still baton's is not the app's: another
// session's, waiting to be pointed at the conversation that session is on
// now, or one the app wrote back after baton took it off (it goes again),
// unless the app went on with it since (goneTaken).
// A record the app left half written (local_*.json.tmp: baton's own
// temporary files are named .baton-*) counts as the record it makes of it
// at its next start.
func (d *Desk) ownRecord(ix *Index, pr probe, r *Row, dir, id string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		rec, ok := strings.CutSuffix(n, ".json")
		if !ok {
			rec, ok = strings.CutSuffix(n, ".json.tmp")
		}
		if e.IsDir() || !ok || !strings.HasPrefix(rec, "local_") {
			continue
		}
		h, ok := readHead(filepath.Join(dir, n))
		if !ok || !strings.EqualFold(h.Cli, id) {
			continue
		}
		taken := false
		if o, p := ix.record(dir, rec); p != nil {
			switch {
			case p.State == stateReleased:
				taken = true
			case p.State == stateDeleted || p.Written == 0:
				// Deleted in the app, or not written by baton yet: a file of
				// that name is not baton's.
			case pr.takenOver(o, p):
				taken = true
			default:
				continue
			}
		} else if g := ix.gone(dir, rec); g != nil && !pr.goneTaken(g, h) {
			continue
		}
		r.Locked = true
		switch {
		case taken:
			r.Checked, r.Why = true, "released"
			return
		case !h.Archived:
			r.Checked, r.Why = true, "own"
			return
		}
		r.Why = "archived"
	}
}

// rows are the places to offer: one per Claude account (and organization)
// of baton's accounts, several of baton's accounts on one being one row,
// plus the desktop app's own account when it is none of them. Accounts that
// cannot have a list in the desktop app come last, as rows that cannot be
// chosen.
func (d *Desk) rows(pr probe) []Row {
	var rows, none []Row
	at := map[string]int{}
	for _, a := range byPriority(d.Accounts) {
		key, why, provider := accountPlace(a)
		if why != "" {
			none = append(none, Row{Accounts: []string{a.ID}, Email: a.Email, Org: a.OrgName, Disabled: true, Why: why, Provider: provider})
			continue
		}
		if i, ok := at[key]; ok {
			rows[i].Accounts = append(rows[i].Accounts, a.ID)
			continue
		}
		at[key] = len(rows)
		rows = append(rows, Row{Key: key, Accounts: []string{a.ID}, Email: a.Email, Org: a.OrgName})
	}
	if pr.active != "" && pr.activeOrg != "" {
		ours := slices.ContainsFunc(rows, func(r Row) bool { return strings.HasPrefix(r.Key, pr.active+"/") })
		if !ours {
			rows = append(rows, Row{Key: pr.active + "/" + pr.activeOrg, Email: pr.activeEmail, AppOnly: true})
		}
	}
	for i := range rows {
		rows[i].Active = pr.isCurrent(rows[i].Key)
	}
	return append(rows, none...)
}

// accountPlace is the desktop app's list a baton account has (its key), or
// why it has none: an API key, a cloud provider (provider: bedrock, vertex,
// foundry...) or a gateway has no claude.ai account; a long-lived token
// (claude setup-token) is one, but does not say which; and an account
// checked before baton read account ids has none known yet. A signed-out
// account keeps its ids: the desktop app signs in on its own.
func accountPlace(a accounts.Account) (key, why, provider string) {
	if m := strings.ToLower(a.AuthMethod); a.LoggedIn && m != "" && m != "claude.ai" {
		switch {
		case strings.Contains(m, "key"):
			return "", "api_key", ""
		case m == "oauth_token":
			return "", "token", ""
		case m == "third_party":
			return "", "gateway", ""
		}
		return "", "cloud", m
	}
	acct, org := strings.ToLower(a.AccountUUID), strings.ToLower(a.OrgID)
	if !validID(acct) || !validID(org) {
		return "", "unknown", ""
	}
	return acct + "/" + org, "", ""
}

// appAccount is what is known of the desktop app's account active: its
// organization, when that is plain (the app has a list for one only, or
// the user's own Claude Code, which the app's Code tab signs in, names it),
// and its email address from there (shown in the window, never kept).
//
// A folder baton made itself (to show a session there) says nothing of what
// the app uses, until the app has something of its own in it.
func (d *Desk) appAccount(ix *Index, active string) (org, email string) {
	var orgs []string
	ents, _ := os.ReadDir(filepath.Join(d.Data(), sessionsDir, active))
	for _, e := range ents {
		o := strings.ToLower(e.Name())
		if e.IsDir() && validID(o) && (ix == nil || ix.appUsed(active+"/"+o, filepath.Join(d.Data(), sessionsDir, active, e.Name()))) {
			orgs = append(orgs, o)
		}
	}
	if d.UserHome != "" {
		if acct, o, e := claude.OAuthIdentity(claude.UserClaudeJSONDir(d.UserHome)); acct == active {
			email = e
			if validID(o) {
				org = o
			}
		}
	}
	if org == "" && len(orgs) == 1 {
		org = orgs[0]
	}
	return org, email
}

// Result says what Apply changed, for the message the window shows.
type Result struct {
	Added      []string `json:"added"`
	Removed    []string `json:"removed"`
	AppRunning bool     `json:"app_running"`
	// ActiveAdded: shown in the list of the account the desktop app is
	// signed in to (it shows when the app opens; a running app needs to be
	// opened again). OtherAdded: shown for another account (it shows when
	// the user switches to it in the app).
	ActiveAdded bool `json:"active_added"`
	OtherAdded  bool `json:"other_added"`
	// ActiveRemoved: taken off a list the running app has in memory; it goes
	// from the app once the app is quit (baton takes it off again then).
	ActiveRemoved bool `json:"active_removed"`
}

// Apply shows conversation c in the desktop app for exactly the places
// want names, as far as baton decides: records the desktop app made or took
// over stay as they are. A place sent as Kept stays as it is by now: never
// taken off, never written. When want says the view had the session's
// thread (threadMark) and the session went on from c since, the change is
// the session's, for the conversation it is on now. Changes are made at
// once, also in the list of the account the running app is signed in to
// (it sees them when it starts again).
func (d *Desk) Apply(c Conversation, want []string) (Result, error) {
	res := Result{Added: []string{}, Removed: []string{}}
	if d.Data() == "" {
		return res, fail("no_desktop", "")
	}
	if !validID(strings.ToLower(c.ID)) {
		return res, fail("no_file", c.ID)
	}
	// wanted: ticked by the user. kept: shown ticked to the user, who left
	// it so (Kept). seen: the view had the session's thread (threadMark).
	wanted, kept, seen := map[string]bool{}, map[string]bool{}, false
	for _, w := range want {
		if strings.EqualFold(w, threadMark) {
			seen = true
			continue
		}
		k, keep := strings.CutPrefix(strings.ToLower(w), keptPrefix)
		if !validKey(k) {
			return res, fail("unknown_place", k)
		}
		if keep {
			kept[k] = true
		} else {
			wanted[k] = true
		}
	}
	err := d.update(func(ix *Index) error {
		pr := d.look(ix)
		t := ix.Thread(c.ID)
		if on := ix.wentOnFrom(c.ID); t == nil && seen && len(on) == 1 {
			// The view had the session's thread on c, the only one ever on it,
			// and the session went on in another conversation since (/clear,
			// or a copy): what the user picked is the session's, so an untick
			// takes it off there and a tick lists the conversation it is on
			// now, never the earlier one.
			t = on[0]
			d.locate(t)
			c = Conversation{ID: t.Session, Transcript: t.Transcript}
		}
		// What the app did since the last look is noted first, as the dialog
		// showed it: a row is judged one way everywhere. Every thread's, so a
		// tick that clears what a delete left (show) never clears the only
		// sign of another thread's delete.
		d.settleAll(ix, t, pr)
		v := d.view(ix, c, pr)
		res.AppRunning = v.AppRunning
		rows := map[string]Row{}
		for _, r := range v.Rows {
			if r.Key != "" {
				rows[r.Key] = r
			}
		}
		for k := range wanted {
			r, ok := rows[k]
			switch {
			case !ok:
				return fail("unknown_place", k)
			case r.Disabled && v.Split:
				return fail("split", "")
			case r.Disabled && v.Problem != "":
				return fail(v.Problem, c.Transcript)
			case r.Disabled:
				return fail("unknown_place", k)
			}
		}
		// Unticked: baton's records go. One the desktop app took over stays.
		if t != nil {
			for _, r := range v.Rows {
				p := placeOf(t, r.Key)
				if r.Key == "" || wanted[r.Key] || kept[r.Key] || p == nil {
					continue
				}
				if p.State == stateReleased {
					// Taken over by the app, yet left out: the clients send
					// every ticked row back, locked or not (View.Ticked), so a
					// dialog opened before the take-over showed it free and the
					// user unticked it. The app keeps its record, and no copy
					// follows the session there.
					t.release(r.Key)
					continue
				}
				if p.State != stateShown {
					continue
				}
				if p.Written == 0 {
					// Not written yet (it waits for its conversation's file):
					// a file of that name is not baton's.
					delete(t.Places, r.Key)
					res.Removed = append(res.Removed, r.Key)
					continue
				}
				if pr.takenOver(t, p) {
					t.release(r.Key) // the app keeps it; no copy follows the session there
					continue
				}
				if err := removeFile(recordFile(p.Dir, p.Record)); err != nil {
					return err
				}
				if pr.safe(r.Key) {
					ix.forget(t, p, d.Now().UnixMilli(), false)
					delete(t.Places, r.Key)
				} else {
					p.State = stateHiding // the running app may write it back from memory
					res.ActiveRemoved = true
				}
				res.Removed = append(res.Removed, r.Key)
			}
		}
		// Ticked by the user: a record of baton's own (a Kept row stays as
		// it is).
		a := describe(c.Transcript)
		if a.cwd == "" && slices.ContainsFunc(v.Rows, func(r Row) bool { return wanted[r.Key] && !r.Checked && !r.Locked }) {
			return fail("no_file", c.Transcript) // a record without its folder is skipped by the app
		}
		for _, r := range v.Rows {
			if r.Key == "" || !wanted[r.Key] || r.Checked || r.Locked {
				continue
			}
			if t == nil {
				t = &Thread{Session: strings.ToLower(c.ID), Transcript: absPath(c.Transcript), Cwd: a.cwd, Title: a.title}
				t.init()
				ix.Threads = append(ix.Threads, t)
			}
			if err := d.ensureLink(t, t.Session, c.Transcript); err != nil {
				return err
			}
			if err := d.show(ix, pr, t, r, a); err != nil {
				return err
			}
			res.Added = append(res.Added, r.Key)
			res.ActiveAdded = res.ActiveAdded || r.Active
			res.OtherAdded = res.OtherAdded || !r.Active
		}
		if t != nil {
			d.dropLinks(ix, t)
		}
		return nil
	})
	return res, err
}

// show writes baton's record of thread t into the list of row r. The
// index names the record before it is written, so a record of baton's is
// never taken for one of the desktop app's.
func (d *Desk) show(ix *Index, pr probe, t *Thread, r Row, a about) error {
	dir := d.placeDir(r.Key)
	if err := d.makeDir(ix, r.Key, dir); err != nil {
		return err
	}
	old := t.Places[r.Key]
	// Ticked (again) after a delete in the desktop app (Apply settled every
	// thread first, so before any look too, and another thread's record
	// deleted there stays deleted): what the delete left goes, as
	// the app's own import of a conversation does, also what it left for a
	// conversation of its own it had moved the record on to (Place.Moved).
	// Also when baton no longer keeps the place as deleted: an earlier
	// record the app took over is let go of once deleted, and one unticked
	// in baton may be deleted in the running app before it let go of it.
	// Only a tick the user gave comes here, so a delete the user did not
	// undo stays.
	names, ids := []string{"local_" + t.Session, t.Session}, []string{t.Session}
	if old != nil {
		names = append(append(names, old.Record, old.Cli), old.Moved...)
		ids = append(append(ids, old.Cli), old.Moved...)
	}
	if old != nil && old.State == stateDeleted || tombstone(dir, names...) > 0 {
		for _, n := range tombstoneNames(names...) {
			_ = removeFile(filepath.Join(dir, n))
		}
		for _, id := range ids {
			if validID(id) {
				_ = removeFile(d.releasedMarker(t, id))
			}
		}
	}
	label := strings.Join(r.Accounts, ", ")
	p := &Place{Key: r.Key, Dir: dir, Record: "local_" + t.Session, Cli: t.Session, State: stateShown, Label: label,
		Copy: t.keptByApp(r.Key)}
	if old != nil && old.State == stateHiding {
		if h, ok := readHead(recordFile(dir, old.Record)); ok && t.knows(h.Cli) && !ix.wroteElsewhere(t, old) {
			// Ticked again before the running app let go of the old one, which
			// it wrote back: still this thread's record.
			old.State, old.Label = stateShown, label
			return nil
		}
		if !ix.wroteElsewhere(t, old) {
			// The running app may still write the old one back: baton's, to
			// take off again then. Until the app lets go of the list it still
			// lists it, and may open the conversation it points at (perhaps
			// an earlier one of the thread's) through its second name, so
			// that name stays meanwhile (Gone.Loaded).
			ix.forget(t, old, d.Now().UnixMilli(), !pr.safe(r.Key))
		}
	}
	for _, g := range ix.Gone {
		// A record of baton's the app wrote back after it was taken off:
		// baton's again, brought up to date by the next look. Only one that
		// points at a conversation of this thread's: one that points at
		// another (the thread it was taken off went on since) goes again.
		// One the app went on with since is the app's (goneTaken).
		if g.Key != r.Key || !g.knows(t.Session) {
			continue
		}
		if h, ok := readHead(recordFile(dir, g.Record)); ok && g.knows(h.Cli) && t.knows(h.Cli) && !pr.goneTaken(&g, h) {
			t.Places[r.Key] = &Place{Key: r.Key, Dir: dir, Record: g.Record, Cli: h.Cli, State: stateShown, Label: label,
				Written: d.Now().UnixMilli()}
			ix.Gone = slices.DeleteFunc(ix.Gone, func(o Gone) bool { return o.Key == g.Key && strings.EqualFold(o.Record, g.Record) })
			return nil
		}
	}
	if g := ix.gone(dir, p.Record); isFile(recordFile(dir, p.Record)) || ix.wroteElsewhere(t, p) ||
		g != nil && slices.ContainsFunc(g.Cli, func(c string) bool { return !t.knows(c) }) {
		// Someone else's file, another thread's record (the running app may
		// write that back under its name), or one baton took off that the
		// running app may still write back pointing at a conversation this
		// thread never had: never one record for two.
		p.Record = "local_" + newID()
	}
	t.Places[r.Key] = p
	if ix.pointedAt(t, dir, t.Session) {
		// Another thread's record of baton's there still points at the
		// conversation (waiting to be pointed at the one its session is on
		// now): this one is written by a later look once that one moved on,
		// never a second record of the conversation beside it.
		return nil
	}
	if err := d.writePlace(ix, t, p, a); err != nil {
		t.Places[r.Key] = old
		if old == nil {
			delete(t.Places, r.Key)
		}
		return err
	}
	// A record of that name baton took off before (the old one, just
	// forgotten) is this one now.
	ix.Gone = slices.DeleteFunc(ix.Gone, func(g Gone) bool { return strings.EqualFold(g.Record, p.Record) && samePath(g.Dir, dir) })
	return nil
}

// writePlace writes a new record for place p of thread t. The index names
// the record as baton's before it is written (the lock is held), so a stop
// in between never leaves a record of baton's that looks like the desktop
// app's own. When the write fails, p is put back as it was, and the index
// on disk follows at the end of the change (update): it never names a
// record baton did not write, which a file of that name made by someone
// else would then be taken for.
func (d *Desk) writePlace(ix *Index, t *Thread, p *Place, a about) error {
	title := a.title
	if p.Copy {
		title += " (baton)"
	}
	now := d.Now().UnixMilli()
	cli, written := p.Cli, p.Written
	p.Cli, p.Written = t.Session, now
	if err := d.save(ix); err != nil {
		p.Cli, p.Written = cli, written
		return err
	}
	if err := writeRecord(recordFile(p.Dir, p.Record), newRecord(p.Record, t.Session, a.cwd, title, a.created, a.last, now), false); err != nil {
		p.Cli, p.Written = cli, written
		return err
	}
	return nil
}

// releasedMarker is where the desktop app notes that it let go of
// conversation id of thread t: next to the name it opens it by.
func (d *Desk) releasedMarker(t *Thread, id string) string {
	name := id + ".desktop-released.json"
	if d.inUserHome(t.Transcript) {
		return filepath.Join(filepath.Dir(t.Transcript), name)
	}
	return filepath.Join(filepath.Dir(d.linkPath(t.Transcript)), name)
}

// save writes the index now, in the middle of a change (the lock is held),
// and notes what it wrote: update writes it again at the end when the
// change ended otherwise, so a record that could not be written after all
// is not named on disk.
func (d *Desk) save(ix *Index) error {
	if err := writeIndex(d.IndexPath, ix); err != nil {
		return err
	}
	ix.saved, _ = json.Marshal(ix)
	return nil
}
