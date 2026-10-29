package run

import (
	"testing"
	"time"
)

func TestStatusFromLine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st, ok := StatusFromLine([]byte(`{"model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},`+
		`"context_window":{"used_percentage":42.5,"context_window_size":200000}}`), now)
	if !ok || st.Model != "Opus 5.5" || st.Context == nil || *st.Context != 42.5 || st.ContextSize != 200000 {
		t.Fatalf("%+v %v", st, ok)
	}
	// Before the first reply there is no usage yet.
	st, ok = StatusFromLine([]byte(`{"model":{"id":"claude-opus-5-5"},"context_window":{"used_percentage":null}}`), now)
	if !ok || st.Model != "claude-opus-5-5" || st.Context != nil {
		t.Fatalf("%+v %v", st, ok)
	}
	if _, ok := StatusFromLine([]byte(`{}`), now); ok {
		t.Fatal("empty payload read as a status")
	}
	dir := t.TempDir()
	v := 10.0
	if err := SaveStatus(dir, Status{Model: "Opus 5.5", Context: &v, Updated: now}); err != nil {
		t.Fatal(err)
	}
	_ = SaveStatus(dir, Status{Model: "Opus 5.5", Context: &v, Updated: now.Add(5 * time.Second)})
	if got, _ := LoadStatus(dir); !got.Updated.Equal(now) {
		t.Fatalf("an unchanged status was written again: %v", got.Updated)
	}
	w := 11.0
	_ = SaveStatus(dir, Status{Model: "Opus 5.5", Context: &w, Updated: now.Add(6 * time.Second)})
	if got, _ := LoadStatus(dir); got.Context == nil || *got.Context != 11 {
		t.Fatalf("a new status was not written: %+v", got)
	}
}
