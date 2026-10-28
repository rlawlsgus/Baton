package classify

import (
	"testing"
	"time"
)

func TestError(t *testing.T) {
	cases := []struct {
		err, msg string
		want     Kind
	}{
		{"rate_limit", "You've hit your session limit · resets 3pm (Asia/Seoul)", UsageLimit},
		{"rate_limit", "API Error: Request rejected (429) · This request would exceed your account's rate limit.", UsageLimit},
		{"rate_limit", "Server is temporarily limiting requests (not your usage limit)", Transient},
		{"authentication_failed", "OAuth token has expired", NeedsLogin},
		{"account_on_hold", "", Blocked},
		{"billing_error", "", Blocked},
		{"oauth_org_not_allowed", "", Blocked},
		{"server_error", "API Error: Repeated 529 Overloaded errors", Ignore},
		{"overloaded", "", Ignore},
		{"", "", Ignore},
	}
	for _, c := range cases {
		if got := Error(c.err, c.msg); got != c.want {
			t.Errorf("Error(%q, %q) = %v, want %v", c.err, c.msg, got, c.want)
		}
	}
}

func TestResetTime(t *testing.T) {
	seoul, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 10, 26, 13, 10, 0, 0, seoul)
	cases := []struct {
		msg  string
		want time.Time
	}{
		{"You've hit your session limit · resets 3pm (Asia/Seoul)", time.Date(2026, 10, 26, 15, 0, 0, 0, seoul)},
		{"You've hit your session limit · resets 3:30pm (Asia/Seoul) · progress saved", time.Date(2026, 10, 26, 15, 30, 0, 0, seoul)},
		{"resets 11am (Asia/Seoul)", time.Date(2026, 10, 27, 11, 0, 0, 0, seoul)}, // already past today
		{"resets 12am (Asia/Seoul)", time.Date(2026, 10, 27, 0, 0, 0, 0, seoul)},
		{"You've hit your weekly limit · resets Oct 29, 9am (Asia/Seoul)", time.Date(2026, 10, 29, 9, 0, 0, 0, seoul)},
		{"resets 3pm (America/New_York)", time.Date(2026, 10, 26, 15, 0, 0, 0, mustLoc("America/New_York"))},
	}
	for _, c := range cases {
		got, ok := ResetTime(c.msg, now)
		if !ok || !got.Equal(c.want) {
			t.Errorf("ResetTime(%q) = %v,%v want %v", c.msg, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "API Error: Request rejected (429)", "resets 3pm (Not/AZone)", "resets 13pm (Asia/Seoul)"} {
		if _, ok := ResetTime(bad, now); ok {
			t.Errorf("ResetTime(%q) should fail", bad)
		}
	}
}

func TestResetTimeYearWrap(t *testing.T) {
	loc := mustLoc("Asia/Seoul")
	now := time.Date(2026, 12, 30, 10, 0, 0, 0, loc)
	got, ok := ResetTime("resets Jan 2, 9am (Asia/Seoul)", now)
	if want := time.Date(2027, 1, 2, 9, 0, 0, 0, loc); !ok || !got.Equal(want) {
		t.Fatalf("got %v %v, want %v", got, ok, want)
	}
}

func TestResetTimeYearAndBoundaries(t *testing.T) {
	loc := mustLoc("Asia/Seoul")
	now := time.Date(2026, 12, 30, 13, 0, 30, 0, loc)
	if got, ok := ResetTime("resets Jan 2, 2027, 9am (Asia/Seoul)", now); !ok || !got.Equal(time.Date(2027, 1, 2, 9, 0, 0, 0, loc)) {
		t.Errorf("year form: %v %v", got, ok)
	}
	if got, ok := ResetTime("resets 1pm (Asia/Seoul)", now); !ok || !got.Equal(now.Add(JustPassedRetry)) {
		t.Errorf("a reset that just passed means 'retry shortly', not tomorrow or an hour: %v %v", got, ok)
	}
	// A weekly limit hit at 15:30 that resets "3pm" (tomorrow; claude omits the date under 24 h).
	at1530 := time.Date(2026, 12, 30, 15, 30, 0, 0, loc)
	if got, ok := ResetTime("You've hit your weekly limit · resets 3pm (Asia/Seoul)", at1530); !ok || !got.Equal(time.Date(2026, 12, 31, 15, 0, 0, 0, loc)) {
		t.Errorf("undated reset 30 min ago must be tomorrow: %v %v", got, ok)
	}
	if _, ok := ResetTime("resets Dec 30, 2026, 9am (Asia/Seoul)", now); ok {
		t.Error("a dated reset in the past must count as unknown")
	}
	if got := Error("authentication_failed", "OAuth token has been revoked"); got != Blocked {
		t.Errorf("revoked token = %v, want blocked", got)
	}
}

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// On a DST fall-back night 1:00-1:59 happens twice; a reset in the second pass
// must not be read as tomorrow.
func TestResetTimeRepeatedHour(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	est := time.FixedZone("EST", -5*3600)
	edt := time.FixedZone("EDT", -4*3600)
	cases := []struct {
		now  time.Time
		msg  string
		want time.Time
	}{
		{time.Date(2026, 11, 1, 1, 10, 0, 0, est), "resets 1:30am (America/New_York)", time.Date(2026, 11, 1, 1, 30, 0, 0, est)},
		{time.Date(2026, 11, 1, 1, 30, 0, 0, edt), "resets 1am (America/New_York)", time.Date(2026, 11, 1, 1, 0, 0, 0, est)},
		// The first pass still works as before.
		{time.Date(2026, 11, 1, 1, 10, 0, 0, edt), "resets 1:30am (America/New_York)", time.Date(2026, 11, 1, 1, 30, 0, 0, edt)},
	}
	for _, c := range cases {
		got, ok := ResetTime(c.msg, c.now.In(ny))
		if !ok || !got.Equal(c.want) {
			t.Errorf("now %v %q: got %v want %v", c.now, c.msg, got, c.want)
		}
	}
}
