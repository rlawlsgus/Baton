// Package ledger records per-account health (state.json), shared by every
// baton instance on the machine under an exclusive file lock.
package ledger

import (
	"errors"
	"os"
	"time"

	"baton/internal/fsx"
)

const (
	StatusOK         = "ok"
	StatusExhausted  = "exhausted"   // hit a usage limit; usable again after RetryAt
	StatusNeedsLogin = "needs_login" // authentication failed; run `baton login <id>`
	StatusBlocked    = "blocked"     // on hold / billing / org not allowed: never auto-used
)

type AccountState struct {
	Status    string    `json:"status"`
	LimitedAt time.Time `json:"limited_at,omitempty"`
	RetryAt   time.Time `json:"retry_at,omitempty"`
	Detail    string    `json:"detail,omitempty"` // limit or error text claude showed
}

type Switch struct {
	At      time.Time `json:"at"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Session string    `json:"session,omitempty"`
	Reason  string    `json:"reason"`
}

type State struct {
	Schema   int                      `json:"schema"`
	Accounts map[string]*AccountState `json:"accounts"`
	Switches []Switch                 `json:"switches"`
}

const maxSwitchLog = 200

func (s *State) Account(id string) *AccountState {
	if s.Accounts == nil {
		s.Accounts = map[string]*AccountState{}
	}
	a := s.Accounts[id]
	if a == nil {
		a = &AccountState{Status: StatusOK}
		s.Accounts[id] = a
	}
	return a
}

// StatusOf is a read-only lookup that defaults to ok.
func (s State) StatusOf(id string) AccountState {
	if a := s.Accounts[id]; a != nil {
		return *a
	}
	return AccountState{Status: StatusOK}
}

// Clone is a deep copy.
func (s State) Clone() State {
	c := s
	c.Accounts = make(map[string]*AccountState, len(s.Accounts))
	for id, a := range s.Accounts {
		if a != nil {
			x := *a
			c.Accounts[id] = &x
		}
	}
	c.Switches = append([]Switch(nil), s.Switches...)
	return c
}

// Cleared is the status an account returns to once its login or block problem
// is fixed: still limited if its usage limit has not reset yet.
func (a AccountState) Cleared(now time.Time) string {
	if now.Before(a.RetryAt) {
		return StatusExhausted
	}
	return StatusOK
}

func (s *State) LogSwitch(sw Switch) {
	s.Switches = append(s.Switches, sw)
	if len(s.Switches) > maxSwitchLog {
		s.Switches = s.Switches[len(s.Switches)-maxSwitchLog:]
	}
}

func Load(path string) (State, error) {
	s := State{Schema: 1, Accounts: map[string]*AccountState{}}
	if err := fsx.ReadJSON(path, &s); err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, err
	}
	if s.Accounts == nil {
		s.Accounts = map[string]*AccountState{}
	}
	return s, nil
}

// Update applies fn to the state under the lock and saves it.
func Update(path, lockPath string, fn func(*State) error) error {
	unlock, err := fsx.Lock(lockPath)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(&s); err != nil {
		return err
	}
	s.Schema = 1
	return fsx.WriteJSONAtomic(path, &s)
}
