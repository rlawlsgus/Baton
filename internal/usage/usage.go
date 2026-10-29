// Package usage keeps each account's plan usage (the 5-hour and weekly
// limits) as Claude Code last reported it. Claude Code passes it to the
// status line command of a session, which in a baton session is
// `baton statusline`; baton never asks the API for it itself.
package usage

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"time"

	"baton/internal/fsx"
)

// Window is one limit window.
type Window struct {
	Used     float64   `json:"used"` // percent of the window used, 0-100
	ResetsAt time.Time `json:"resets_at"`
}

// Usage is an account's usage at one moment.
type Usage struct {
	FiveHour *Window   `json:"five_hour,omitempty"`
	SevenDay *Window   `json:"seven_day,omitempty"`
	Updated  time.Time `json:"updated"`
}

// FromStatusLine reads the usage from a status line payload: rate_limits
// with five_hour and seven_day, each {used_percentage, resets_at (Unix
// seconds)}. Claude Code sends them only for a Claude subscription, and only
// after the session's first reply.
func FromStatusLine(data []byte, now time.Time) (Usage, bool) {
	type window struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       *float64 `json:"resets_at"`
	}
	var p struct {
		RateLimits *struct {
			FiveHour *window `json:"five_hour"`
			SevenDay *window `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(data, &p) != nil || p.RateLimits == nil {
		return Usage{}, false
	}
	conv := func(w *window) *Window {
		if w == nil || w.UsedPercentage == nil || w.ResetsAt == nil || math.IsNaN(*w.UsedPercentage) {
			return nil
		}
		used := math.Max(0, math.Min(100, *w.UsedPercentage))
		return &Window{Used: used, ResetsAt: time.Unix(int64(*w.ResetsAt), 0).UTC()}
	}
	u := Usage{FiveHour: conv(p.RateLimits.FiveHour), SevenDay: conv(p.RateLimits.SevenDay), Updated: now.UTC()}
	if u.FiveHour == nil && u.SevenDay == nil {
		return Usage{}, false
	}
	return u, true
}

// Current is u as of now: a window that has reset since counts as unused
// until Claude Code reports it again.
func (u Usage) Current(now time.Time) Usage {
	fix := func(w *Window) *Window {
		if w == nil || now.Before(w.ResetsAt) {
			return w
		}
		return &Window{Used: 0, ResetsAt: w.ResetsAt}
	}
	return Usage{FiveHour: fix(u.FiveHour), SevenDay: fix(u.SevenDay), Updated: u.Updated}
}

func path(dir, account string) string { return filepath.Join(dir, account+".json") }

// Load reads an account's last known usage.
func Load(dir, account string) (Usage, bool) {
	var u Usage
	if fsx.ReadJSON(path(dir, account), &u) != nil || u.FiveHour == nil && u.SevenDay == nil {
		return Usage{}, false
	}
	return u, true
}

// Save stores an account's usage. The status line runs often, so an
// unchanged reading is written only once a minute (to keep "as of" fresh).
func Save(dir, account string, u Usage) error {
	if old, ok := Load(dir, account); ok && same(old.FiveHour, u.FiveHour) && same(old.SevenDay, u.SevenDay) &&
		u.Updated.Sub(old.Updated) < time.Minute {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fsx.WriteJSONAtomic(path(dir, account), u)
}

// Remove forgets an account's usage (the account was removed).
func Remove(dir, account string) {
	_ = os.Remove(path(dir, account))
}

func same(a, b *Window) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Used == b.Used && a.ResetsAt.Equal(b.ResetsAt)
}
