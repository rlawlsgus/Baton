package sched

import (
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/ledger"
)

func reg(as ...accounts.Account) accounts.Registry { return accounts.Registry{Accounts: as} }

func acct(id string, prio int) accounts.Account {
	return accounts.Account{ID: id, Priority: prio, Enabled: true, LoggedIn: true}
}

func TestPickPriorityAndExclusion(t *testing.T) {
	now := time.Now()
	r := reg(acct("a", 10), acct("b", 20), acct("c", 5))
	st := ledger.State{}
	if best, _, _ := Pick(r, st, now, ""); best == nil || best.ID != "b" {
		t.Fatalf("want b, got %+v", best)
	}
	if best, _, _ := Pick(r, st, now, "b"); best == nil || best.ID != "a" {
		t.Fatalf("want a when b excluded, got %+v", best)
	}
}

func TestPickSkipsUnusable(t *testing.T) {
	now := time.Now()
	d := acct("d", 99)
	d.Enabled = false
	r := reg(acct("a", 10), acct("b", 20), acct("c", 30), d)
	st := ledger.State{Accounts: map[string]*ledger.AccountState{
		"c": {Status: ledger.StatusBlocked},
		"b": {Status: ledger.StatusExhausted, RetryAt: now.Add(time.Hour)},
	}}
	if best, _, _ := Pick(r, st, now, ""); best == nil || best.ID != "a" {
		t.Fatalf("want a, got %+v", best)
	}
	// After b's reset it is preferred again.
	if best, _, _ := Pick(r, st, now.Add(2*time.Hour), ""); best == nil || best.ID != "b" {
		t.Fatalf("want b after reset, got %+v", best)
	}
}

func TestPickReportsSoonest(t *testing.T) {
	now := time.Now()
	r := reg(acct("a", 10), acct("b", 20))
	st := ledger.State{Accounts: map[string]*ledger.AccountState{
		"a": {Status: ledger.StatusExhausted, RetryAt: now.Add(3 * time.Hour)},
		"b": {Status: ledger.StatusExhausted, RetryAt: now.Add(time.Hour)},
	}}
	best, soonest, at := Pick(r, st, now, "")
	if best != nil || soonest == nil || soonest.ID != "b" || !at.Equal(now.Add(time.Hour)) {
		t.Fatalf("got best=%v soonest=%v at=%v", best, soonest, at)
	}
	// Blocked or logged-out accounts never come back on their own.
	st.Accounts["a"].Status = ledger.StatusNeedsLogin
	st.Accounts["b"].Status = ledger.StatusBlocked
	if _, soonest, _ := Pick(r, st, now, ""); soonest != nil {
		t.Fatalf("want no soonest, got %v", soonest)
	}
}

func TestPickTieBreaksOnLeastRecentlyLimited(t *testing.T) {
	now := time.Now()
	r := reg(acct("a", 10), acct("b", 10))
	st := ledger.State{Accounts: map[string]*ledger.AccountState{
		"a": {Status: ledger.StatusOK, LimitedAt: now.Add(-time.Hour)},
		"b": {Status: ledger.StatusOK, LimitedAt: now.Add(-5 * time.Hour)},
	}}
	if best, _, _ := Pick(r, st, now, ""); best.ID != "b" {
		t.Fatalf("want b (limited longer ago), got %s", best.ID)
	}
}

func TestPickSkipsSignedOutAndSameIdentity(t *testing.T) {
	now := time.Now()
	a := acct("a", 30)
	a.Email = "me@x.com"
	b := acct("b", 20)
	b.Email = "ME@x.com" // the same Claude account signed in twice
	c := acct("c", 10)
	c.LoggedIn = false
	d := acct("d", 5)
	if best, _, _ := Pick(reg(a, b, c, d), ledger.State{}, now, "a"); best == nil || best.ID != "d" {
		t.Fatalf("want d (b is a's identity, c is signed out), got %+v", best)
	}
}

func TestRateGuard(t *testing.T) {
	g := RateGuard{Cooldown: time.Minute, MaxPerHour: 2}
	t0 := time.Now()
	if ok, _ := g.Allow(t0); !ok {
		t.Fatal("first switch must be allowed")
	}
	g.Record(t0)
	if ok, at := g.Allow(t0.Add(30 * time.Second)); ok || !at.Equal(t0.Add(time.Minute)) {
		t.Fatalf("cooldown not enforced: ok=%v at=%v", ok, at)
	}
	g.Record(t0.Add(2 * time.Minute))
	if ok, at := g.Allow(t0.Add(10 * time.Minute)); ok || !at.Equal(t0.Add(time.Hour)) {
		t.Fatalf("hourly cap not enforced: ok=%v at=%v", ok, at)
	}
	if ok, _ := g.Allow(t0.Add(61 * time.Minute)); !ok {
		t.Fatal("cap must lift after an hour")
	}
}
