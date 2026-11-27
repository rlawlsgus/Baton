package usage

import (
	"testing"
	"time"
)

// The payload shape is what Claude Code 2.1.309 builds for a status line:
// used_percentage is utilization*100 (one decimal), resets_at Unix seconds.
func TestFromStatusLine(t *testing.T) {
	now := time.Unix(1761600000, 0)
	payload := `{"model":{"id":"x"},"rate_limits":{"five_hour":{"used_percentage":34.5,"resets_at":1761607200},` +
		`"seven_day":{"used_percentage":120,"resets_at":1761900000}}}`
	u, ok := FromStatusLine([]byte(payload), now)
	if !ok || u.FiveHour == nil || u.FiveHour.Used != 34.5 || !u.FiveHour.ResetsAt.Equal(time.Unix(1761607200, 0)) ||
		u.SevenDay == nil || u.SevenDay.Used != 100 || !u.Updated.Equal(now) {
		t.Fatalf("got %+v %v", u, ok)
	}
	for _, p := range []string{`{}`, `{"rate_limits":{}}`, `{"rate_limits":{"five_hour":{"used_percentage":3}}}`, `not json`} {
		if _, ok := FromStatusLine([]byte(p), now); ok {
			t.Errorf("%s: read as usage", p)
		}
	}
	// A window that has reset since counts as unused.
	cur := u.Current(time.Unix(1761607200+60, 0))
	if cur.FiveHour.Used != 0 || cur.SevenDay.Used != 100 {
		t.Fatalf("current %+v %+v", cur.FiveHour, cur.SevenDay)
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Load(dir, "work"); ok {
		t.Fatal("usage before any was saved")
	}
	now := time.Now().UTC().Truncate(time.Second)
	u := Usage{FiveHour: &Window{Used: 10, ResetsAt: now.Add(time.Hour)}, Updated: now}
	if err := Save(dir, "work", u); err != nil {
		t.Fatal(err)
	}
	// The same reading a moment later is not written again; a new one is.
	if err := Save(dir, "work", Usage{FiveHour: u.FiveHour, Updated: now.Add(10 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir, "work"); !got.Updated.Equal(now) {
		t.Fatalf("an unchanged reading was written again: %v", got.Updated)
	}
	_ = Save(dir, "work", Usage{FiveHour: &Window{Used: 11, ResetsAt: now.Add(time.Hour)}, Updated: now.Add(20 * time.Second)})
	if got, _ := Load(dir, "work"); got.FiveHour.Used != 11 {
		t.Fatalf("a new reading was not written: %+v", got.FiveHour)
	}
	Remove(dir, "work")
	if _, ok := Load(dir, "work"); ok {
		t.Fatal("usage left after Remove")
	}
}

// The answer to get_usage is what Claude Code 2.1.331 sends (trimmed):
// utilization in percent, resets_at RFC 3339 with microseconds, and windows
// and fields baton does not read.
func TestFromReport(t *testing.T) {
	now := time.Unix(1795700000, 0)
	answer := `{"session":{"total_cost_usd":0},"subscription_type":"team","rate_limits_available":true,"rate_limits":{` +
		`"five_hour":{"utilization":15,"resets_at":"2026-11-27T05:39:59.607208+00:00","limit_dollars":null,"locked_reason":null},` +
		`"seven_day":{"utilization":3.5,"resets_at":"2026-12-03T23:59:59.607223+00:00"},"seven_day_opus":null,` +
		`"extra_usage":{"is_enabled":false}},"behaviors":null}`
	u, ok := FromReport([]byte(answer), now)
	want5 := time.Date(2026, 11, 27, 5, 39, 59, 607208000, time.UTC)
	if !ok || u.FiveHour == nil || u.FiveHour.Used != 15 || !u.FiveHour.ResetsAt.Equal(want5) ||
		u.SevenDay == nil || u.SevenDay.Used != 3.5 || !u.Updated.Equal(now) {
		t.Fatalf("got %+v %+v %v", u.FiveHour, u.SevenDay, ok)
	}
	// A window not started yet (nothing used, no reset): unused, with no time.
	u, ok = FromReport([]byte(`{"rate_limits":{"five_hour":{"utilization":0,"resets_at":null},"seven_day":{"utilization":7,"resets_at":"2026-12-03T23:59:59Z"}}}`), now)
	if !ok || u.FiveHour == nil || u.FiveHour.Used != 0 || !u.FiveHour.ResetsAt.IsZero() || u.SevenDay.Used != 7 {
		t.Fatalf("not started: %+v %+v %v", u.FiveHour, u.SevenDay, ok)
	}
	if cur := u.Current(now); cur.FiveHour.Used != 0 || cur.SevenDay.Used != 7 {
		t.Fatalf("current: %+v %+v", cur.FiveHour, cur.SevenDay)
	}
	for _, a := range []string{
		`{"rate_limits_available":false,"rate_limits":null}`, // an API key
		`{"rate_limits":{}}`,
		`{"rate_limits":{"five_hour":{"utilization":20,"resets_at":null}}}`,   // used, yet no reset
		`{"rate_limits":{"five_hour":{"utilization":20,"resets_at":"soon"}}}`, // a time baton cannot read
		`not json`,
	} {
		if _, ok := FromReport([]byte(a), now); ok {
			t.Errorf("%s: read as usage", a)
		}
	}
}
