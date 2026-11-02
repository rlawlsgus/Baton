// Package office watches Claude Code at work for the baton window's office
// view: every conversation on this PC shown live as people in an office.
//
// It only reads: Claude Code's session registry and transcripts (with its
// subagents' transcripts) and baton's run journals, which the caller hands
// it. It never writes into Claude's folders; its one file of its own is
// office.json in baton's folder, the building's floor numbers.
//
// The work is done while someone looks: a tracker ticks every 250 ms while a
// page polled it in the last minute, and sleeps otherwise.
package office

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"baton/internal/claude"
)

const (
	tickEvery     = 250 * time.Millisecond
	discoverEvery = time.Second      // the registry, runs and transcripts' sizes
	scanEvery     = 20 * time.Second // the list of recent conversations (the building)
	idleAfter     = time.Minute      // nobody polled this long: stop looking, forget the views
	waitMax       = 15 * time.Second // the longest a poll is held
	readyWait     = 8 * time.Second  // the first poll waits this long for a first look around
	maxSessions   = 4                // conversations followed at once (the window, a pop-out, ...)
	recentMax     = 200
	// followLinger: in follow mode, a conversation whose turn just ended is
	// kept a moment before the office moves to another that is busy.
	followLinger = 10 * time.Second
)

// Options set a tracker up.
type Options struct {
	// Root is baton's data folder; office.json lives there.
	Root string
	// Sources says where to look. It is called about once a second while
	// the office is watched.
	Sources func() Sources
	// Me is the user's name, on the name plate of their desk.
	Me string
}

// Sources are where conversations live and the baton sessions running now.
type Sources struct {
	Dirs     []Dir    // every config folder: each account's profile, and the user's own
	Accounts []string // account ids in accounts.json order (it sets their colours)
	Runs     []Run    // baton sessions whose supervisor runs now
}

// Dir is a config folder with its conversations and session registry.
type Dir struct {
	Path    string
	Account string // the account whose profile it is; "" for the user's own setup
}

// Run is a baton session running now, from its journal.
type Run struct {
	ID             string
	Account        string
	State          string // running | stopping | switching | waiting | asking | ending
	SessionID      string
	TranscriptPath string
	Cwd            string
	Window         bool // its console window is known (it can be brought forward)
	Started        time.Time
}

// Query is one poll.
type Query struct {
	Session string // "" follows the most recently active conversation
	Since   int64  // the last event the client has
	Wait    bool   // hold the answer until there is news (up to 15 s)
}

// Response is the answer to a poll.
type Response struct {
	Seq      int64     `json:"seq"`
	Reset    bool      `json:"reset"`
	Me       string    `json:"me"`
	Now      time.Time `json:"now"`
	Building Building  `json:"building"`
	View     *View     `json:"view"`
	Events   []Event   `json:"events"`
	// Accounts are the account ids in accounts.json order, which account
	// colours (and account_index) go by.
	Accounts []string `json:"accounts"`
}

// Building is every project folder as a floor.
type Building struct {
	Total  int     `json:"total"`
	Floors []Floor `json:"floors"`
}

type Floor struct {
	Floor    int           `json:"floor"`
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Live     bool          `json:"live"`
	Busy     bool          `json:"busy"`
	Last     time.Time     `json:"last"`
	Sessions []SessionItem `json:"sessions"`
}

// SessionItem is a conversation in a floor's list.
type SessionItem struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Last    time.Time `json:"last"`
	Live    bool      `json:"live"`
	Status  string    `json:"status,omitempty"` // idle | busy | waiting, while live
	Account string    `json:"account,omitempty"`
	Source  string    `json:"source"` // baton | cli | desktop
}

var sessionRx = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// ValidSession says whether id can be a session id (and so is safe in a path).
func ValidSession(id string) bool { return sessionRx.MatchString(id) }

// Tracker watches the conversations for the office.
type Tracker struct {
	opt     Options
	wake    chan struct{}
	done    chan struct{}
	closing sync.Once
	ready   chan struct{} // closed after the first look around
	rescan  atomic.Bool   // the conversation list was read again: look around now

	// Owned by the loop goroutine.
	discoveredAt time.Time
	found        map[string]foundPath // transcript lookups, by session id

	mu        sync.Mutex
	changed   chan struct{} // closed (and replaced) when there is news
	seq       int64
	polled    time.Time
	sleeping  bool
	readyDone bool
	dirty     bool

	dirs      []Dir
	accounts  []string
	info      map[string]*sessionInfo // what discovery knows, by session id
	procs     map[string]string       // a Claude Code process (or baton run) -> its conversation
	successor map[string]string       // a conversation -> the one its process went on with (/clear)
	clears    map[string]string       // a successor -> its predecessor, until a view tells it
	follow    string
	recent    []claude.Transcript
	scanAt    time.Time
	scanning  bool
	scanned   bool
	floors    *floors
	building  Building
	bDigest   uint64
	sessions  map[string]*session
	missing   map[string]time.Time // ids asked for that have nothing to show, and when
}

type foundPath struct {
	path   string
	at     time.Time
	misses int // looks that found nothing in a row
}

// New starts a tracker. It sleeps until polled.
func New(opt Options) *Tracker {
	t := newTracker(opt)
	go t.loop()
	return t
}

func newTracker(opt Options) *Tracker {
	// seq starts at 1: a client that has nothing yet asks with since=0, which
	// must never look like the place of a session with no events so far, or
	// such a session would be answered with a reset at once, forever.
	return &Tracker{opt: opt, seq: 1, wake: make(chan struct{}, 1), done: make(chan struct{}), ready: make(chan struct{}),
		found: map[string]foundPath{}, changed: make(chan struct{}), info: map[string]*sessionInfo{},
		procs: map[string]string{}, successor: map[string]string{}, clears: map[string]string{},
		floors: loadFloors(filepath.Join(opt.Root, "office.json")), sessions: map[string]*session{}, missing: map[string]time.Time{}}
}

// Close stops the tracker; polls being held answer at once.
func (t *Tracker) Close() {
	t.closing.Do(func() {
		close(t.done)
		t.mu.Lock()
		t.broadcast()
		t.mu.Unlock()
	})
}

func (t *Tracker) closed() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Tracker) kick() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// broadcast wakes every poll being held. Call with mu held.
func (t *Tracker) broadcast() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// Poll answers one poll: the building, the conversation shown and its events
// after q.Since. With q.Wait it holds the answer until there is news, 15 s
// pass, ctx ends or the tracker closes.
func (t *Tracker) Poll(ctx context.Context, q Query) Response {
	t.mu.Lock()
	wasAsleep := t.sleeping || t.polled.IsZero()
	t.polled = time.Now()
	ready := t.ready
	t.mu.Unlock()
	if wasAsleep {
		t.kick()
	}
	select {
	case <-ready:
	case <-ctx.Done():
	case <-t.done:
	case <-time.After(readyWait):
	}
	deadline := time.Now().Add(waitMax)
	var start uint64
	for first := true; ; first = false {
		now := time.Now()
		t.mu.Lock()
		t.polled = now
		s := t.viewFor(q.Session, now)
		digest := t.bDigest
		reset, news := false, false
		if s != nil {
			s.polled = now
			digest = digest*1099511628211 ^ s.digest
			reset = !s.knows(q.Since)
			news = !reset && s.lastSeq > q.Since
		}
		if first {
			start = digest
		}
		if !q.Wait || reset || news || digest != start || !now.Before(deadline) || t.closed() || ctx.Err() != nil {
			resp := t.answer(s, q.Since, reset, now)
			t.mu.Unlock()
			return resp
		}
		ch := t.changed
		t.mu.Unlock()
		wait := time.NewTimer(min(tickEvery, time.Until(deadline)))
		select {
		case <-ch:
		case <-wait.C:
		case <-ctx.Done():
		case <-t.done:
		}
		wait.Stop()
	}
}

// answer builds a response. Call with mu held. Views and buildings are
// never changed once made, so the response can be encoded after unlocking.
func (t *Tracker) answer(s *session, since int64, reset bool, now time.Time) Response {
	r := Response{Seq: t.seq, Reset: reset, Me: t.opt.Me, Now: utc(now), Building: t.building, Events: []Event{},
		Accounts: append([]string{}, t.accounts...)}
	if s == nil {
		return r
	}
	r.View, r.Seq = s.view, s.lastSeq
	if reset {
		r.Events = s.last(seedEvents)
	} else {
		r.Events = s.after(since)
	}
	return r
}

// viewFor is the conversation a poll asks for: the one followed, or the
// one pinned (or, after a /clear, the one its process went on with). It is
// attached when first asked for. Call with mu held.
func (t *Tracker) viewFor(pinned string, now time.Time) *session {
	id := t.follow
	if pinned != "" {
		id = t.resolve(pinned)
	}
	if id == "" {
		return nil
	}
	if s := t.sessions[id]; s != nil {
		return s
	}
	if at, ok := t.missing[id]; ok && now.Sub(at) < 2*time.Second {
		return nil
	}
	s := t.attach(id, now)
	if s == nil {
		if len(t.missing) > 64 {
			clear(t.missing)
		}
		t.missing[id] = now
	}
	return s
}

// resolve follows a conversation to the one its process went on with.
func (t *Tracker) resolve(id string) string {
	for i := 0; i < 16; i++ {
		n := t.successor[id]
		if n == "" || n == id {
			break
		}
		id = n
	}
	return id
}

func (t *Tracker) loop() {
	for {
		t.mu.Lock()
		awake := !t.polled.IsZero() && time.Since(t.polled) < idleAfter
		t.sleeping = !awake
		if !awake {
			t.rest()
		}
		t.mu.Unlock()
		if !awake {
			select {
			case <-t.done:
				return
			case <-t.wake:
			}
			continue
		}
		t.tick(time.Now())
		next := time.NewTimer(tickEvery)
		select {
		case <-t.done:
			next.Stop()
			return
		case <-t.wake:
			next.Stop()
		case <-next.C:
		}
	}
}

// rest is the tracker going to sleep: nobody looks, so what was read is read
// again when someone does, and the first poll then waits for a fresh look
// around (what was found before the sleep is old), as the very first one
// does. Call with mu held.
func (t *Tracker) rest() {
	clear(t.sessions)
	if t.readyDone {
		t.ready, t.readyDone = make(chan struct{}), false
	}
}

// tick looks around (once a second) and reads what the watched
// conversations wrote, then tells waiting polls if anything changed.
func (t *Tracker) tick(now time.Time) {
	if now.Sub(t.discoveredAt) >= discoverEvery || t.rescan.Swap(false) {
		t.discover(now)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	dirs := t.dirPaths()
	changed := t.dirty
	t.dirty = false
	for id, s := range t.sessions {
		if now.Sub(s.polled) > idleAfter {
			delete(t.sessions, id)
			continue
		}
		s.tailAll(dirs, now)
		s.advance(now)
		if s.refresh(now, t.accountIndex, t.opt.Me) {
			changed = true
		}
	}
	if changed {
		t.broadcast()
	}
}

// attach starts showing a conversation: the end of its transcript (and of
// its subagents' that are at work) is read to see where everyone is, and
// the newest events seed its log. Call with mu held.
func (t *Tracker) attach(id string, now time.Time) *session {
	var info sessionInfo
	if i := t.info[id]; i != nil {
		info = *i
	} else {
		var ok bool
		if info, ok = t.describe(id); !ok {
			return nil
		}
	}
	s := newSession(id, now, &t.seq)
	s.info = info
	s.mode = modeReplay
	if info.path != "" {
		s.open(info.path, true)
		if s.main != nil {
			_, _ = s.main.next(func(line []byte) { s.row(line, s.mainActor(), now) })
		}
	}
	s.findAgentDirs(t.dirPaths(), now)
	s.scanAgents(now)
	s.advance(now)
	if !s.active(now) {
		// Nobody works here any more: an empty office with its history.
		for _, aid := range append([]string(nil), s.order...) {
			s.drop(aid)
		}
		s.closeAll(s.mainActor())
		s.mainActor().thinking = false
	}
	s.seed()
	s.wasLive = info.live
	if s.asking = info.live && info.status == "waiting"; s.asking {
		s.askAt = now
	}
	if r := info.run; r != nil {
		s.runAcct, s.runSt = r.Account, r.State
	}
	if old := t.clears[id]; old != "" {
		delete(t.clears, id)
		s.emit(Event{T: now, Actor: mainID, Kind: "clear", Room: roomSeat})
	}
	s.refresh(now, t.accountIndex, t.opt.Me)
	for len(t.sessions) >= maxSessions {
		var oldest *session
		for _, o := range t.sessions {
			if oldest == nil || o.polled.Before(oldest.polled) {
				oldest = o
			}
		}
		delete(t.sessions, oldest.id)
	}
	t.sessions[id] = s
	return s
}

// open starts reading a conversation's transcript. When attaching, its end
// is read for where things are; a transcript that appears later is a new
// conversation (after /clear, or its first turn) and is read from the start,
// as news, unless it is big already.
func (s *session) open(path string, attaching bool) {
	size, err := fileSize(path)
	if err != nil {
		return
	}
	switch {
	case attaching:
		s.main = newTailer(path, tailFrom(size, attachWindow))
	case size <= 256<<10:
		s.main = newTailer(path, 0)
	default:
		s.main = newTailer(path, tailFrom(size, attachWindow))
		s.mode = modeQuiet
		_, _ = s.main.next(func(line []byte) { s.row(line, s.mainActor(), time.Now()) })
		s.mode = modeLive
	}
}

// tailAll reads what the conversation and its subagents wrote since the
// last tick.
func (s *session) tailAll(dirs []string, now time.Time) {
	if s.main == nil && s.info.path != "" {
		s.open(s.info.path, false)
	}
	if s.main != nil {
		_, _ = s.main.next(func(line []byte) { s.row(line, s.mainActor(), now) })
	}
	s.findAgentDirs(dirs, now)
	s.scanAgents(now)
	s.tailAgents(now)
}

func (t *Tracker) dirPaths() []string {
	out := make([]string, 0, len(t.dirs))
	for _, d := range t.dirs {
		if d.Path != "" {
			out = append(out, d.Path)
		}
	}
	return out
}

// accountIndex is an account's place in accounts.json, from 0 (-1: none),
// which picks its colour.
func (t *Tracker) accountIndex(id string) int {
	if id == "" {
		return -1
	}
	for i, a := range t.accounts {
		if a == id {
			return i
		}
	}
	return -1
}

// accountOf is the account whose config folder holds path ("" for the
// user's own).
func (t *Tracker) accountOf(path string) string {
	p := strings.ToLower(filepath.Clean(path))
	for _, d := range t.dirs {
		if d.Path == "" {
			continue
		}
		dp := strings.ToLower(filepath.Clean(d.Path))
		if p == dp || strings.HasPrefix(p, dp+`\`) {
			return d.Account
		}
	}
	return ""
}
