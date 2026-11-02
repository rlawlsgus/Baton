package office

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Subagents keep their own transcripts next to the conversation's:
//
//	<config>\projects\<project>\<session>\subagents\agent-<id>.jsonl (+ .meta.json)
//	<config>\projects\<project>\<session>\subagents\workflows\<run>\agent-<id>.jsonl
//
// After a baton account switch they go to the new account's folder while
// the conversation's own file stays where it began, so every config folder
// is looked in.
const (
	agentDirsEvery = 2 * time.Second  // looking for the session's subagents folders
	agentGlobEvery = 30 * time.Second // searching every project folder for them
	agentScanEvery = time.Second      // listing them (every tick while a subagent was just hired)
	maxAgentFiles  = 4000             // a long workflow leaves hundreds of finished ones behind
	// lateQuiet: of a subagent transcript first seen after the office began
	// watching, rows older than this are read for where the agent is, not
	// told as news.
	lateQuiet = 30 * time.Second
)

// agentFile is one subagent's transcript.
type agentFile struct {
	path, id string
	size     int64
	mod      time.Time
	metaMod  time.Time
	meta     agentMeta
	actor    string // the person it shows ("" until then)
	tail     *tailer
	pending  bool // to follow once its .meta.json is there
	done     bool // finished, or not worth following
}

// agentMeta is the part of agent-<id>.meta.json the office reads.
type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"` // the Agent tool use that started it (none for a workflow's agents)
	Finished    bool   `json:"finished"`
}

// findAgentDirs looks for the session's subagents folders in every config
// folder. They sit in the conversation's own project folder, so looking
// there is cheap; searching every project folder lists them all, which
// costs tens of milliseconds with a long history, so that is done when
// attaching and every 30 s (for a conversation that moved, or whose
// transcript is not known), keeping what it found.
func (s *session) findAgentDirs(dirs []string, now time.Time) {
	burst := now.Before(s.burstUntil) && len(s.agentDirs) == 0
	if !s.dirsAt.IsZero() && now.Sub(s.dirsAt) < agentDirsEvery && !burst {
		return
	}
	s.dirsAt = now
	project := ""
	if s.info.path != "" {
		project = filepath.Base(filepath.Dir(s.info.path)) // the folder Claude Code made, not one rebuilt from the cwd
	}
	if project == "" || s.globAt.IsZero() || now.Sub(s.globAt) >= agentGlobEvery {
		s.globAt = now
		var found []string
		for _, d := range dirs {
			m, _ := filepath.Glob(filepath.Join(d, "projects", "*", s.id, "subagents"))
			found = append(found, m...)
		}
		s.agentDirs = found
		return
	}
	seen := map[string]bool{}
	var found []string
	add := func(p string) {
		if k := strings.ToLower(p); !seen[k] {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				seen[k] = true
				found = append(found, p)
			}
		}
	}
	for _, d := range dirs {
		add(filepath.Join(d, "projects", project, s.id, "subagents"))
	}
	for _, p := range s.agentDirs {
		add(p)
	}
	s.agentDirs = found
}

// scanAgents lists the subagent transcripts and starts following new ones.
// The first scan (when the session is attached) follows only agents still
// at work; later ones follow every agent that appears or writes.
func (s *session) scanAgents(now time.Time) {
	if s.scanned && now.Sub(s.scanAt) < agentScanEvery && !now.Before(s.burstUntil) {
		return
	}
	first := !s.scanned
	s.scanAt, s.scanned = now, true
	for _, d := range s.agentDirs {
		s.listAgents(d, now, first)
		wfs, _ := os.ReadDir(filepath.Join(d, "workflows"))
		for _, wf := range wfs {
			if wf.IsDir() {
				s.listAgents(filepath.Join(d, "workflows", wf.Name()), now, first)
			}
		}
	}
}

func (s *session) listAgents(dir string, now time.Time, first bool) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	metas := map[string]os.DirEntry{}
	for _, e := range ents {
		if n := e.Name(); strings.HasPrefix(n, "agent-") && strings.HasSuffix(n, ".meta.json") {
			metas[n] = e
		}
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, name)
		f := s.files[p]
		isNew := f == nil
		if isNew {
			if len(s.files) >= maxAgentFiles {
				continue
			}
			f = &agentFile{path: p, id: strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl")}
			s.files[p] = f
		}
		if me := metas[strings.TrimSuffix(name, ".jsonl")+".meta.json"]; me != nil {
			if mi, err := me.Info(); err == nil && !mi.ModTime().Equal(f.metaMod) {
				f.metaMod = mi.ModTime()
				if data, err := readSmall(filepath.Join(dir, me.Name()), 64<<10); err == nil {
					_ = json.Unmarshal(data, &f.meta)
				}
			}
		}
		changed := !isNew && (fi.Size() != f.size || !fi.ModTime().Equal(f.mod))
		f.size, f.mod = fi.Size(), fi.ModTime()
		if f.meta.Finished && f.tail != nil {
			if a := s.actors[f.actor]; a != nil {
				s.finish(a, now)
			}
		}
		if f.done || f.tail != nil || f.meta.Finished {
			continue
		}
		var want bool
		if first {
			// Already there when the office came: follow it only if it is at work.
			want = s.active(now) && now.Sub(f.mod) < 10*time.Minute
		} else {
			want = isNew || changed || f.pending
		}
		if !want {
			continue
		}
		// Its .meta.json says whose it is; it is written just after the
		// transcript, so wait a moment for it rather than hire a stranger.
		if f.metaMod.IsZero() && now.Sub(f.mod) < 5*time.Second {
			f.pending = true
			continue
		}
		f.pending = false
		s.follow(f, now, first)
	}
}

// follow starts following a subagent's transcript: the person it belongs to
// (hired when Claude's Agent call was read, or now) gets its rows.
func (s *session) follow(f *agentFile, now time.Time, attaching bool) {
	if id := s.agentIDs[f.id]; id != "" && s.actors[id] != nil && f.meta.ToolUseID == "" {
		f.meta.ToolUseID = id
	}
	id := f.meta.ToolUseID
	if id == "" {
		id = f.id
	}
	a := s.actors[id]
	if a != nil && (a.kind != kindAgent || a.file != "" && a.file != f.path || !a.reportAt.IsZero() || !a.leaveAt.IsZero()) {
		f.done = true // someone else's, or an agent that already finished
		return
	}
	size, err := fileSize(f.path)
	if err != nil {
		return
	}
	mark := len(s.replay)
	if a == nil {
		typ := f.meta.AgentType
		if typ == "" {
			typ = "general-purpose"
		}
		a = s.hire(id, typ, short(f.meta.Description, 60), now)
	}
	a.file, f.actor = f.path, a.id
	s.agentIDs[f.id] = a.id
	f.tail = newTailer(f.path, tailFrom(size, agentWindow))
	if !attaching {
		s.quiet = now.Add(-lateQuiet)
	}
	_, _ = f.tail.next(func(line []byte) { s.row(line, a, now) })
	s.quiet = time.Time{}
	if attaching && s.mode == modeReplay {
		// It was hired before the rows read now: date its arrival to them.
		first := now
		for _, e := range s.replay[mark:] {
			if e.Actor == a.id && e.Kind != "hire" && e.T.Before(first) {
				first = e.T
			}
		}
		for i := mark; i < len(s.replay); i++ {
			if e := &s.replay[i]; e.Actor == a.id && e.Kind == "hire" {
				e.T = utc(first.Add(-time.Millisecond))
				a.hiredAt, a.arriveTo = e.T, e.T.Add(arriveFor)
			}
		}
	}
}

// tailAgents reads what the followed subagents wrote since the last tick.
func (s *session) tailAgents(now time.Time) {
	for _, f := range s.files {
		if f.tail == nil || f.done {
			continue
		}
		a := s.actors[f.actor]
		if a == nil || !a.leaveAt.IsZero() {
			f.done = true
			continue
		}
		_, _ = f.tail.next(func(line []byte) { s.row(line, a, now) })
	}
}
