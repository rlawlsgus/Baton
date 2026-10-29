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
