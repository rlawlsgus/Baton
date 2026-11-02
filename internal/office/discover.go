package office

import (
	"hash/fnv"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"baton/internal/claude"
)

// sessionInfo is what the office knows of a conversation apart from its
// transcript's rows: from the conversation list, the session registry and
// baton's runs.
type sessionInfo struct {
	id          string
	title       string // its name in the conversation list
	firstPrompt string
	regName     string // its live name in the session registry
	cwd         string // its project folder
	path        string // its transcript
	entry       string // what runs it: claude-desktop, cli, claude-vscode...
	account     string // the account whose profile holds it ("" for the user's own setup)
	run         *Run   // the baton session running it
	live        bool   // a Claude Code process (or a baton session) has it now
	status      string // the registry's idle | busy | waiting
	waitingFor  string
	started     time.Time
	last        time.Time // when it last wrote or changed status
	floor       int
}

// source is what runs the conversation: baton (a baton session, or an
// account's profile), the desktop app, or the Claude Code CLI (or an editor).
func (i *sessionInfo) source() string {
	switch {
	case i.run != nil || i.account != "":
		return "baton"
	case i.entry == "claude-desktop":
		return "desktop"
	}
	return "cli"
}

func (i *sessionInfo) shownTitle() string {
	switch {
	case i.regName != "":
		return i.regName
	case i.title != "":
		return i.title
	}
	return i.firstPrompt
}

func (i *sessionInfo) busy() bool {
	return i.live && (i.status == "busy" || i.status == "waiting")
}

// discover looks around: which Claude Code processes and baton sessions run,
// what their transcripts are and when they were written. It also starts a
// fresh read of the conversation list when that is old. The reading is done
// without the lock; what it found is applied with it.
func (t *Tracker) discover(now time.Time) {
	t.discoveredAt = now
	var src Sources
	if t.opt.Sources != nil {
		src = t.opt.Sources()
	}
	var dirs []string
	for _, d := range src.Dirs {
		if d.Path != "" {
			dirs = append(dirs, d.Path)
		}
	}
	type liveEntry struct {
		ls      claude.LiveSession
		account string
	}
	live := map[string]liveEntry{}
	procs := map[string]string{}
	for _, d := range src.Dirs {
		if d.Path == "" {
			continue
		}
		for _, ls := range claude.ReadSessions(d.Path) {
			if ls.SessionID == "" || !ls.Alive() {
				continue
			}
			if old, ok := live[ls.SessionID]; ok && old.ls.StatusUpdatedAt > ls.StatusUpdatedAt {
				continue
			}
			live[ls.SessionID] = liveEntry{ls, d.Account}
			// A process is its registry file and its start: a pid used again
			// by a new claude is another process.
			procs[strings.ToLower(ls.File)+"|"+strconv.FormatInt(ls.StartedAt, 10)] = ls.SessionID
		}
	}
	runs := map[string]Run{}
	for _, r := range src.Runs {
		if r.SessionID != "" {
			runs[r.SessionID] = r
		}
		procs["run|"+r.ID] = r.SessionID
	}

	// The transcripts of what runs, and of what is watched.
	t.mu.Lock()
	ids := map[string]bool{}
	for id := range t.sessions {
		ids[id] = true
	}
	if t.follow != "" {
		ids[t.follow] = true
	}
	t.mu.Unlock()
	for id := range live {
		ids[id] = true
	}
	for id := range runs {
		ids[id] = true
	}
	paths := map[string]string{}
	writes := map[string]time.Time{}
	for id := range ids {
		r := runs[id]
		if p, mod := t.transcriptOf(id, r.TranscriptPath, dirs, now); p != "" {
			paths[id], writes[id] = p, mod
		}
	}
	for id := range t.found {
		if !ids[id] {
			delete(t.found, id)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.dirs, t.accounts = src.Dirs, src.Accounts
	// /clear (or /resume) in a running Claude Code: its process, or its
	// baton session, holds another conversation now.
	for k, id := range procs {
		if old := t.procs[k]; old != "" && id != "" && old != id {
			if len(t.successor) > 256 {
				clear(t.successor)
			}
			t.successor[old] = id
			if t.sessions[old] != nil || t.follow == old {
				t.clears[id] = old
			}
		}
	}
	t.procs = procs

	info := map[string]*sessionInfo{}
	get := func(id string) *sessionInfo {
		i := info[id]
		if i == nil {
			i = &sessionInfo{id: id}
			info[id] = i
		}
		return i
	}
	for _, c := range t.recent {
		i := get(c.SessionID)
		i.title, i.firstPrompt, i.cwd, i.path, i.entry, i.last = c.Title, c.FirstPrompt, c.Cwd, c.Path, c.Source, c.Modified
		i.account = t.accountOf(c.ConfigDir)
	}
	for id, e := range live {
		i := get(id)
		i.live, i.status, i.waitingFor, i.regName = true, e.ls.Status, e.ls.WaitingFor, strings.TrimSpace(e.ls.Name)
		if i.entry == "" {
			i.entry = e.ls.Entrypoint
		}
		if i.cwd == "" {
			i.cwd = e.ls.Cwd
		}
		if i.account == "" {
			i.account = e.account
		}
		if e.ls.StartedAt > 0 {
			i.started = time.UnixMilli(e.ls.StartedAt)
		}
		if at := time.UnixMilli(e.ls.StatusUpdatedAt); e.ls.StatusUpdatedAt > 0 && at.After(i.last) {
			i.last = at
		}
	}
	for id, r := range runs {
		i := get(id)
		run := r
		i.run, i.live, i.account = &run, true, r.Account
		if i.cwd == "" {
			i.cwd = r.Cwd
		}
		if i.started.IsZero() {
			i.started = r.Started
		}
	}
	for id, p := range paths {
		i := get(id)
		if i.path == "" {
			i.path = p
		}
		if w := writes[id]; w.After(i.last) {
			i.last = w
		}
		// A conversation watched but not in the list (an old one asked for
		// by id) keeps what was learnt of it when it was attached.
		if s := t.sessions[id]; s != nil && i.cwd == "" {
			i.title, i.firstPrompt, i.cwd = s.info.title, s.info.firstPrompt, s.info.cwd
			if i.entry == "" {
				i.entry = s.info.entry
			}
			if i.account == "" {
				i.account = s.info.account
			}
		}
	}
	t.info = info
	t.updateBuilding()
	t.updateFollow(now)
	for _, s := range t.sessions {
		t.update(s, now)
	}
	for id, old := range t.clears {
		if s := t.sessions[id]; s != nil {
			delete(t.clears, id)
			s.emit(Event{T: now, Actor: mainID, Kind: "clear", Room: roomSeat})
		} else if t.sessions[old] == nil && t.follow != id {
			delete(t.clears, id) // nobody watches it any more
		}
	}
	t.startScan(dirs)
	if t.scanned && !t.readyDone {
		t.readyDone = true
		close(t.ready)
	}
}

// transcriptOf finds a conversation's transcript: the one its baton session
// names, or the newest <id>.jsonl in any config folder. While there is none
// yet (a conversation before its first turn) it is looked for again less and
// less often, from 3 s up to 30 s: each look lists every project folder.
func (t *Tracker) transcriptOf(id, runPath string, dirs []string, now time.Time) (string, time.Time) {
	if runPath != "" {
		if fi, err := os.Stat(runPath); err == nil && !fi.IsDir() {
			return runPath, fi.ModTime()
		}
	}
	f, ok := t.found[id]
	if ok && f.path != "" {
		if fi, err := os.Stat(f.path); err == nil && !fi.IsDir() {
			return f.path, fi.ModTime()
		}
	}
	if ok && f.path == "" && now.Sub(f.at) < min(3*time.Second<<f.misses, 30*time.Second) {
		return "", time.Time{}
	}
	p := claude.FindTranscript(dirs, id)
	if p == "" {
		misses := 0
		if ok && f.path == "" {
			misses = min(f.misses+1, 4)
		}
		t.found[id] = foundPath{at: now, misses: misses}
		return "", time.Time{}
	}
	t.found[id] = foundPath{path: p, at: now}
	fi, err := os.Stat(p)
	if err != nil {
		return "", time.Time{}
	}
	return p, fi.ModTime()
}

// startScan reads the conversation list again in the background when it is
// old (it reads the start and end of up to 200 files, cached by size and time).
// Call with mu held.
func (t *Tracker) startScan(dirs []string) {
	if t.scanning || !t.scanAt.IsZero() && time.Since(t.scanAt) < scanEvery {
		return
	}
	t.scanning = true
	go func() {
		list := claude.ListAllTranscripts(dirs, recentMax)
		t.mu.Lock()
		t.recent, t.scanAt, t.scanning, t.scanned = list, time.Now(), false, true
		t.mu.Unlock()
		t.rescan.Store(true)
		t.kick()
	}()
}

// updateFollow picks the conversation follow mode shows: the one shown stays
// while it is busy (or waits for the user); otherwise the busy one that was
// active last, then the one shown while it is live, then the newest.
// Call with mu held.
func (t *Tracker) updateFollow(now time.Time) {
	cur := t.resolve(t.follow)
	last := func(id string) time.Time {
		if i := t.info[id]; i != nil {
			return i.last
		}
		return time.Time{}
	}
	newer := func(a, b string) bool { // a is newer than b
		if la, lb := last(a), last(b); !la.Equal(lb) {
			return la.After(lb)
		}
		return a > b
	}
	var busy, newest string
	for id, i := range t.info {
		if i.busy() && (busy == "" || newer(id, busy)) {
			busy = id
		}
		if newest == "" || newer(id, newest) {
			newest = id
		}
	}
	ci := t.info[cur]
	switch {
	case ci != nil && ci.busy():
	case busy != "" && !(ci != nil && ci.live && now.Sub(ci.last) < followLinger):
		cur = busy
	case ci != nil && ci.live:
	case newest != "":
		cur = newest
	}
	t.follow = cur
}

// update tells a watched conversation what discovery found: an account
// switch, a question for the user, its Claude Code gone. Call with mu held.
func (t *Tracker) update(s *session, now time.Time) {
	var next sessionInfo
	if i := t.info[s.id]; i != nil {
		next = *i
	} else {
		next = s.info
		next.live, next.status, next.waitingFor, next.regName, next.run = false, "", "", "", nil
	}
	s.info = next
	m := s.mainActor()
	if r := next.run; r != nil {
		handover := func(st string) bool { return st == "switching" || st == "waiting" }
		switch {
		case s.runAcct != "" && r.Account != "" && r.Account != s.runAcct:
			// Shift change: the next account's Claude comes in.
			s.emit(Event{T: now, Actor: mainID, Kind: "shift", Room: roomElevator, Detail: s.runAcct + " → " + r.Account})
			m.hiredAt, m.arriveTo = now, now.Add(arriveFor)
		case handover(r.State) && !handover(s.runSt):
			// Claude goes home; who comes next is not known yet.
			s.emit(Event{T: now, Actor: mainID, Kind: "shift", Room: roomElevator, Detail: r.Account + " →"})
		}
		s.runAcct, s.runSt = r.Account, r.State
	}
	asking := next.live && next.status == "waiting"
	if asking && !s.asking {
		s.askAt = now
		s.emit(Event{T: now, Actor: mainID, Kind: "ask", Room: roomDesk, Detail: waitTool(next.waitingFor)})
	}
	s.asking = asking
	if s.wasLive && !next.live {
		s.emit(Event{T: now, Actor: mainID, Kind: "end", Room: roomLounge})
		s.ended(now)
		s.endAt = now
	}
	if next.live {
		s.endAt = time.Time{}
	}
	s.wasLive = next.live
}

// describe is what is known of a conversation that is neither running nor
// in the conversation list (an old one asked for by id). Call with mu held.
func (t *Tracker) describe(id string) (sessionInfo, bool) {
	p := claude.FindTranscript(t.dirPaths(), id)
	if p == "" {
		return sessionInfo{}, false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return sessionInfo{}, false
	}
	c := claude.Describe(p, fi)
	i := sessionInfo{id: id, title: c.Title, firstPrompt: c.FirstPrompt, cwd: c.Cwd, path: p, entry: c.Source,
		last: fi.ModTime(), account: t.accountOf(p)}
	if k := projectKey(i.cwd); k != "" {
		if t.floors.of(k) == 0 && (t.floors.begun || t.scanned) && t.floors.number(map[string]time.Time{k: i.last}) {
			_ = t.floors.save()
		}
		i.floor = t.floors.of(k)
	}
	return i, true
}

const floorSessions = 40 // conversations listed per floor

// updateBuilding lays out the floors: one per project folder, numbered once
// and kept. Call with mu held.
func (t *Tracker) updateBuilding() {
	type floor struct {
		Floor
		last time.Time
	}
	byKey := map[string]*floor{}
	oldest := map[string]time.Time{}
	for id, i := range t.info {
		k := projectKey(i.cwd)
		if k == "" {
			continue
		}
		f := byKey[k]
		if f == nil {
			f = &floor{Floor: Floor{Name: folderName(i.cwd), Path: i.cwd}}
			byKey[k] = f
		}
		item := SessionItem{ID: id, Title: i.shownTitle(), Last: utc(i.last), Live: i.live, Account: i.account, Source: i.source()}
		if i.live {
			item.Status = i.status
			f.Live = true
		}
		f.Busy = f.Busy || i.busy()
		f.Sessions = append(f.Sessions, item)
		if f.last.IsZero() || i.last.After(f.last) {
			f.last, f.Path = i.last, i.cwd
		}
		if o, ok := oldest[k]; !ok || i.last.Before(o) {
			oldest[k] = i.last
		}
	}
	// The very first time, wait for the conversation list: the floors are
	// numbered by age, the oldest project on 1F.
	if (t.scanned || t.floors.begun) && t.floors.number(oldest) {
		_ = t.floors.save()
	}
	list := make([]Floor, 0, len(byKey))
	for k, f := range byKey {
		f.Floor.Floor = t.floors.of(k)
		f.Last = utc(f.last)
		sort.Slice(f.Sessions, func(a, b int) bool {
			if !f.Sessions[a].Last.Equal(f.Sessions[b].Last) {
				return f.Sessions[a].Last.After(f.Sessions[b].Last)
			}
			return f.Sessions[a].ID < f.Sessions[b].ID
		})
		if len(f.Sessions) > floorSessions {
			f.Sessions = f.Sessions[:floorSessions]
		}
		list = append(list, f.Floor)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].Floor != list[b].Floor {
			return list[a].Floor > list[b].Floor
		}
		return list[a].Last.After(list[b].Last)
	})
	for _, i := range t.info {
		i.floor = t.floors.of(projectKey(i.cwd))
	}
	h := fnv.New64a()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	for _, f := range list {
		w(strconv.Itoa(f.Floor), f.Name, f.Path, strconv.FormatBool(f.Live), strconv.FormatBool(f.Busy))
		for _, s := range f.Sessions {
			w(s.ID, s.Title, strconv.FormatBool(s.Live), s.Status, s.Account, s.Source)
		}
	}
	if d := h.Sum64(); d != t.bDigest {
		t.bDigest, t.dirty = d, true
	}
	t.building = Building{Total: len(list), Floors: list}
}
