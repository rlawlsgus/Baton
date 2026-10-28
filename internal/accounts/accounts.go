// Package accounts is the registry of logged-in accounts (accounts.json).
//
// It never holds credentials: each account is a CLAUDE_CONFIG_DIR profile that
// claude.exe logs in and refreshes itself. baton only records the non-secret
// fields of `claude auth status --json`.
package accounts

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"baton/internal/fsx"
)

var idRx = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

type Account struct {
	ID         string `json:"id"`
	ProfileDir string `json:"profile_dir"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
	// DisabledAsDuplicate: baton (not the user) disabled it because it signed
	// in as an account already registered; a later correct sign-in re-enables it.
	DisabledAsDuplicate bool `json:"disabled_as_duplicate,omitempty"`

	// Snapshot of `claude auth status --json`, refreshed by add/login/doctor.
	LoggedIn     bool      `json:"logged_in"`
	AuthMethod   string    `json:"auth_method,omitempty"`
	Email        string    `json:"email,omitempty"`
	AccountUUID  string    `json:"account_uuid,omitempty"`
	OrgID        string    `json:"org_id,omitempty"`
	OrgName      string    `json:"org_name,omitempty"`
	Subscription string    `json:"subscription,omitempty"`
	CheckedAt    time.Time `json:"checked_at,omitempty"`

	Added time.Time `json:"added"`
}

type Registry struct {
	Schema   int       `json:"schema"`
	Accounts []Account `json:"accounts"`
}

var reservedRx = regexp.MustCompile(`^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)

func ValidateID(id string) error {
	if !idRx.MatchString(id) {
		return fmt.Errorf("account id %q: use 1-32 lowercase letters, digits or '-' (starting with a letter or digit)", id)
	}
	if reservedRx.MatchString(id) {
		return fmt.Errorf("account id %q is a reserved Windows device name; choose another", id)
	}
	return nil
}

func Load(path string) (Registry, error) {
	r := Registry{Schema: 1}
	if err := fsx.ReadJSON(path, &r); err != nil && !errors.Is(err, os.ErrNotExist) {
		return r, err
	}
	return r, nil
}

func Save(path string, r Registry) error {
	r.Schema = 1
	sort.SliceStable(r.Accounts, func(i, j int) bool { return r.Accounts[i].ID < r.Accounts[j].ID })
	return fsx.WriteJSONAtomic(path, r)
}

// Update re-reads the registry under a lock, applies fn and saves it, so two
// baton commands running at once cannot undo each other's changes.
func Update(path, lockPath string, fn func(*Registry) error) (Registry, error) {
	unlock, err := fsx.Lock(lockPath)
	if err != nil {
		return Registry{}, err
	}
	defer unlock()
	r, err := Load(path)
	if err != nil {
		return r, err
	}
	if err := fn(&r); err != nil {
		return r, err
	}
	return r, Save(path, r)
}

// SameIdentity reports whether two accounts are known to be the same Claude
// account (the browser was still signed in as the first one during sign-in).
// One person can hold subscriptions in several organizations (a personal plan
// and a Team seat), each with its own quota: those are different accounts.
func SameIdentity(a, b Account) bool {
	if orgsDiffer(a, b) {
		return false
	}
	if a.AccountUUID != "" && b.AccountUUID != "" {
		return a.AccountUUID == b.AccountUUID
	}
	return a.Email != "" && b.Email != "" && strings.EqualFold(a.Email, b.Email)
}

func orgsDiffer(a, b Account) bool {
	if a.OrgID != "" && b.OrgID != "" {
		return a.OrgID != b.OrgID
	}
	return a.OrgName != "" && b.OrgName != "" && !strings.EqualFold(a.OrgName, b.OrgName)
}

func (r *Registry) Get(id string) *Account {
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			return &r.Accounts[i]
		}
	}
	return nil
}

func (r *Registry) Add(a Account) error {
	if err := ValidateID(a.ID); err != nil {
		return err
	}
	if r.Get(a.ID) != nil {
		return fmt.Errorf("account %q already exists", a.ID)
	}
	r.Accounts = append(r.Accounts, a)
	return nil
}

func (r *Registry) Remove(id string) bool {
	for i := range r.Accounts {
		if r.Accounts[i].ID == id {
			r.Accounts = append(r.Accounts[:i], r.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

// NextPriority returns a priority below every existing one, so accounts added
// later are preferred less by default.
func (r *Registry) NextPriority() int {
	p := 100
	for _, a := range r.Accounts {
		if a.Priority <= p {
			p = a.Priority - 10
		}
	}
	return p
}
