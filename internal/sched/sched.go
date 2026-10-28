// Package sched picks which account a session should run on.
package sched

import (
	"sort"
	"time"

	"baton/internal/accounts"
	"baton/internal/ledger"
)

// Eligible reports whether an account may be used now, and if not, when it
// will be (zero when it never will without the user's help).
func Eligible(a accounts.Account, st ledger.AccountState, now time.Time) (bool, time.Time) {
	if !a.Enabled || !a.LoggedIn {
		return false, time.Time{}
	}
	switch st.Status {
	case ledger.StatusBlocked, ledger.StatusNeedsLogin:
		return false, time.Time{}
	case ledger.StatusExhausted:
		if now.Before(st.RetryAt) {
			return false, st.RetryAt
		}
	}
	return true, time.Time{}
}

// Pick returns the best eligible account other than exclude: highest priority
// first, then the one limited longest ago. When none is eligible it returns
// the account that frees up soonest and when.
// Accounts that are the same Claude identity as exclude are skipped too:
// "switching" to them would land on the same exhausted quota.
func Pick(reg accounts.Registry, st ledger.State, now time.Time, exclude string) (best *accounts.Account, soonest *accounts.Account, soonestAt time.Time) {
	var ok []accounts.Account
	cur := reg.Get(exclude)
	for _, a := range reg.Accounts {
		if a.ID == exclude || (cur != nil && accounts.SameIdentity(a, *cur)) {
			continue
		}
		yes, at := Eligible(a, st.StatusOf(a.ID), now)
		if yes {
			ok = append(ok, a)
			continue
		}
		if !at.IsZero() && (soonestAt.IsZero() || at.Before(soonestAt)) {
			a := a
			soonest, soonestAt = &a, at
		}
	}
	if len(ok) == 0 {
		return nil, soonest, soonestAt
	}
	sort.SliceStable(ok, func(i, j int) bool {
		if ok[i].Priority != ok[j].Priority {
			return ok[i].Priority > ok[j].Priority
		}
		li, lj := st.StatusOf(ok[i].ID).LimitedAt, st.StatusOf(ok[j].ID).LimitedAt
		if !li.Equal(lj) {
			return li.Before(lj)
		}
		return ok[i].ID < ok[j].ID
	})
	return &ok[0], nil, time.Time{}
}

// RateGuard limits how often one run switches, so a misclassified error or an
// account that is limited again right away cannot cause a switch storm.
type RateGuard struct {
	Cooldown   time.Duration
	MaxPerHour int
	history    []time.Time
}

// Allow reports whether a switch may happen now, and if not, when.
func (g *RateGuard) Allow(now time.Time) (bool, time.Time) {
	var recent []time.Time
	for _, t := range g.history {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	g.history = recent
	if n := len(recent); n > 0 {
		if next := recent[n-1].Add(g.Cooldown); now.Before(next) {
			return false, next
		}
	}
	if g.MaxPerHour > 0 && len(recent) >= g.MaxPerHour {
		return false, recent[0].Add(time.Hour)
	}
	return true, time.Time{}
}

func (g *RateGuard) Record(now time.Time) { g.history = append(g.history, now) }
