package run

import (
	"encoding/json"
	"math"
	"time"

	"baton/internal/fsx"
)

// Status is what the session's status line last showed about it: the model
// and how full its context window is. `baton statusline` writes it into the
// run's folder for the baton window.
type Status struct {
	Model       string    `json:"model,omitempty"`
	Context     *float64  `json:"context,omitempty"`      // percent of the context window used
	ContextSize int       `json:"context_size,omitempty"` // tokens the window holds
	Updated     time.Time `json:"updated"`
}

// StatusFromLine reads a status line payload (Claude Code's model and
// context_window fields).
func StatusFromLine(data []byte, now time.Time) (Status, bool) {
	var p struct {
		Model *struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
		ContextWindow *struct {
			Used *float64 `json:"used_percentage"`
			Size int      `json:"context_window_size"`
		} `json:"context_window"`
	}
	if json.Unmarshal(data, &p) != nil || p.Model == nil && p.ContextWindow == nil {
		return Status{}, false
	}
	st := Status{Updated: now.UTC()}
	if p.Model != nil {
		st.Model = p.Model.DisplayName
		if st.Model == "" {
			st.Model = p.Model.ID
		}
	}
	if w := p.ContextWindow; w != nil {
		st.ContextSize = w.Size
		if w.Used != nil && !math.IsNaN(*w.Used) {
			v := math.Max(0, math.Min(100, *w.Used))
			st.Context = &v
		}
	}
	return st, true
}

// SaveStatus writes a run's status; an unchanged one only every 30 s.
func SaveStatus(dir string, st Status) error {
	if old, ok := LoadStatus(dir); ok && old.Model == st.Model && old.ContextSize == st.ContextSize &&
		(old.Context == nil) == (st.Context == nil) && (old.Context == nil || *old.Context == *st.Context) &&
		st.Updated.Sub(old.Updated) < 30*time.Second {
		return nil
	}
	return fsx.WriteJSONAtomic(StatusPath(dir), st)
}

func LoadStatus(dir string) (Status, bool) {
	var st Status
	if fsx.ReadJSON(StatusPath(dir), &st) != nil {
		return Status{}, false
	}
	return st, true
}
