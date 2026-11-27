package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The desktop app's records say which conversations a session went on in,
// in the lists of every account (also one none of baton's): the ones it was
// on before (and the oldest of them, kept apart), the one before a /clear
// and one it shows only the history of. An archived session brought back
// is on unarchivedCliSessionId. baton's threads say it too. What is no
// record, or names no conversation, says nothing.
func TestLineage(t *testing.T) {
	r := newRig(t)
	id := func(n byte) string { return "c0000000-0000-4000-8000-00000000000" + string(n) }
	r.writeJSON(filepath.Join(r.placeDir(keyA), "local_a.json"), map[string]any{"sessionId": "local_a", "cliSessionId": id('3'),
		"priorCliSessionIds": []string{id('1'), id('2')}, "overflowCliSessionIds": []string{id('0')},
		"preClearCliSessionId": id('4'), "historyOnlyCliSessionId": id('5')})
	// Another account's list went on apart from the same earlier one.
	r.writeJSON(filepath.Join(r.placeDir(keyB), "local_b.json"), map[string]any{"sessionId": "local_b", "cliSessionId": id('9'),
		"priorCliSessionIds": []string{id('2')}})
	r.writeJSON(filepath.Join(r.placeDir(keyX), "local_x.json"), map[string]any{"sessionId": "local_x",
		"unarchivedCliSessionId": id('7'), "priorCliSessionIds": []string{id('6')}})
	// Says nothing: no conversation now, not a record, ids that are none,
	// itself, a half-written record, a list of something else.
	r.writeJSON(filepath.Join(r.placeDir(keyA), "local_none.json"), map[string]any{"sessionId": "local_none", "priorCliSessionIds": []string{id('8')}})
	r.writeJSON(filepath.Join(r.placeDir(keyA), "scheduled-tasks.json"), map[string]any{"cliSessionId": id('8'), "priorCliSessionIds": []string{id('8')}})
	r.writeJSON(filepath.Join(r.placeDir(keyA), "local_bad.json"), map[string]any{"cliSessionId": "nope", "priorCliSessionIds": []any{3, "x", id('3')}})
	r.writeJSON(filepath.Join(r.placeDir(keyA), "local_self.json"), map[string]any{"cliSessionId": id('8'), "priorCliSessionIds": []string{id('8')}})
	must(t, os.WriteFile(filepath.Join(r.placeDir(keyA), "local_half.json.tmp"), []byte(`{"cliSessionId":"`+id('8')+`","priorCliSessionIds":["`+id('0')+`"]}`), 0o600))
	must(t, os.WriteFile(r.d.IndexPath, []byte(`{"schema":1,"threads":[{"session":"`+sid2+`","prior":["`+sid+`"]}]}`), 0o600))

	got := r.d.Lineage()
	for _, v := range got {
		slices.Sort(v)
	}
	want := map[string][]string{
		id('0'): {id('3')}, id('1'): {id('3')}, id('2'): {id('3'), id('9')}, id('4'): {id('3')}, id('5'): {id('3')},
		id('6'): {id('7')}, sid: {sid2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Fatalf("%s: %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}
