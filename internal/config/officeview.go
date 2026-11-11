package config

import (
	"encoding/json"
	"regexp"
	"strings"
)

// OfficeView is how the window's office tab looks: the width of its side
// columns, per window (the tab, the pop-out), and the sessions and floors
// the user hid from it. The window keeps it here because its own storage
// starts empty every launch (its address has a new port each time). It is
// not a setting of `baton config`, so it is not in Keys.
type OfficeView struct {
	Tab    OfficePanes `json:"tab"`
	Popout OfficePanes `json:"popout"`
	// Hidden are blacklists, so a new conversation shows by default: floors
	// by project key (the folder in lower case), sessions by id. Newest last.
	HiddenFloors   []string `json:"hidden_floors,omitempty"`
	HiddenSessions []string `json:"hidden_sessions,omitempty"`
}

// OfficePanes are the office's two side columns in one window, in CSS px
// (0: the default width). A folded one is out of the way, opened over the
// stage when wanted. Last is the one the user sized last ("l" or "r"): when
// the window has no room for both widths, the other one gives way.
type OfficePanes struct {
	Left        int    `json:"left,omitempty"`
	Right       int    `json:"right,omitempty"`
	LeftFolded  bool   `json:"left_folded,omitempty"`
	RightFolded bool   `json:"right_folded,omitempty"`
	Last        string `json:"last,omitempty"`
}

const (
	// OfficeWidthMax only keeps a hand-edited width sane; the window fits
	// the columns to its own size anyway.
	OfficeWidthMax = 1200
	// OfficeHiddenSessionsMax and OfficeHiddenFloorsMax cap the lists:
	// config.json is read on every office poll.
	OfficeHiddenSessionsMax = 500
	OfficeHiddenFloorsMax   = 200
	officeFloorKeyMax       = 1024
)

// officeSessionRx is the shape of a session id (what office.ValidSession
// accepts); anything else in the list could never match one.
var officeSessionRx = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// UnmarshalJSON never fails: a broken office_view (a hand edit) only resets
// how the office looks, and must never stop config.json from loading. What
// can be read of it is kept.
func (v *OfficeView) UnmarshalJSON(b []byte) error {
	type plain OfficeView
	var p plain
	_ = json.Unmarshal(b, &p)
	*v = OfficeView(p)
	return nil
}

// Clamp keeps the widths within what any window could use, and Last one of
// the two columns (or none).
func (p OfficePanes) Clamp() OfficePanes {
	w := func(n int) int { return max(0, min(n, OfficeWidthMax)) }
	p.Left, p.Right = w(p.Left), w(p.Right)
	if p.Last != "l" && p.Last != "r" {
		p.Last = ""
	}
	return p
}

// Normalized tidies an office view read from disk (or just changed): widths
// clamped, the lists without blanks, duplicates or impossible ids, and no
// longer than their caps (the newest kept). nil when nothing is left.
func (v *OfficeView) Normalized() *OfficeView {
	if v == nil {
		return nil
	}
	n := OfficeView{Tab: v.Tab.Clamp(), Popout: v.Popout.Clamp()}
	n.HiddenSessions = tidyList(v.HiddenSessions, OfficeHiddenSessionsMax, func(s string) bool { return officeSessionRx.MatchString(s) })
	n.HiddenFloors = tidyList(v.HiddenFloors, OfficeHiddenFloorsMax, func(s string) bool { return len(s) <= officeFloorKeyMax })
	if n.Tab == (OfficePanes{}) && n.Popout == (OfficePanes{}) && len(n.HiddenSessions) == 0 && len(n.HiddenFloors) == 0 {
		return nil
	}
	return &n
}

// tidyList drops blanks, what ok refuses and repeats (the later one stays,
// as the newer), and keeps the last max.
func tidyList(in []string, max int, ok func(string) bool) []string {
	seen := map[string]bool{}
	var rev []string
	for i := len(in) - 1; i >= 0 && len(rev) < max; i-- {
		s := strings.TrimSpace(in[i])
		if s == "" || seen[s] || !ok(s) {
			continue
		}
		seen[s] = true
		rev = append(rev, s)
	}
	out := make([]string, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SetHidden hides (or shows again) one session or floor in the office. A
// hidden item moves to the end, as the newest.
func (v *OfficeView) SetHidden(floor bool, key string, hidden bool) {
	list := &v.HiddenSessions
	if floor {
		list = &v.HiddenFloors
	}
	out := (*list)[:0:0]
	for _, s := range *list {
		if s != key {
			out = append(out, s)
		}
	}
	if hidden {
		out = append(out, key)
	}
	*list = out
}
