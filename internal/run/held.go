package run

import (
	"strings"
	"time"

	"baton/internal/claude"
)

// Held says who has a conversation open: the live claudes that hold it, and
// the baton session among them, if one of them is the claude a live run
// started. Two writers split a conversation, so a start that finds it held
// goes on in a copy, or brings that baton session's window forward instead.
type Held struct {
	Holders []claude.LiveSession
	Run     *Journal
}

// Entrypoint says what holds the conversation open (claude-desktop, cli,
// claude-vscode...), from the first holder that says; "" when none does.
func (h Held) Entrypoint() string {
	for _, o := range h.Holders {
		if o.Entrypoint != "" {
			return o.Entrypoint
		}
	}
	return ""
}

// HeldBy looks in the config folders' session registries for live claudes
// that have the conversation open, and in runsDir for the run whose claude
// one of them is. alive(pid, created) must check the supervisor's creation
// time, not just its pid. A registry file caught mid-rewrite is read again
// for about a second; one that stays unreadable counts as a holder (a copy
// costs less than a second writer).
func HeldBy(configDirs []string, runsDir, sessionID string, alive func(pid int, created time.Time) bool) Held {
	var hs []claude.LiveSession
	for try := 0; try < 5; try++ {
		if hs = claude.Holders(configDirs, sessionID); !onlyUnreadable(hs) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	h := Held{Holders: hs}
	if len(hs) == 0 {
		return h
	}
	for _, j := range Active(runsDir, alive) {
		if j.ChildPID <= 0 {
			continue
		}
		for _, o := range hs {
			if strings.EqualFold(o.SessionID, sessionID) && claude.StartedBy(o, j.ChildPID) {
				h.Run = &j
				return h
			}
		}
	}
	return h
}

func onlyUnreadable(hs []claude.LiveSession) bool {
	for _, o := range hs {
		if o.Status != "unknown" {
			return false
		}
	}
	return len(hs) > 0
}
