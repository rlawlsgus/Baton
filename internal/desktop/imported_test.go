package desktop

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A conversation is made in one list, but the desktop app also brings one
// into a list itself: its resume picker and a resume link look only at the
// list signed in, so they import one that another account's list has
// already. Both rows are the app's own and locked, but only the first says
// the conversation was made there; the other says the app brought it in.
// The app keeps fromCliTranscript and may drop adoptedFromOtherSurface
// later: either says so. An archived one says only that it is archived.
func TestImportedRecordSaysSo(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.appRecord(keyA, "local_d0000000-0000-4000-8000-0000000000d0", sid) // made in a's list
	imported := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")    // the app names its imports so
	for i, c := range []struct {
		name  string
		marks map[string]any
		why   string
	}{
		{"imported", map[string]any{"fromCliTranscript": true, "adoptedFromOtherSurface": true}, "imported"},
		{"its adopted mark dropped", map[string]any{"fromCliTranscript": true}, "imported"},
		{"only adopted", map[string]any{"adoptedFromOtherSurface": true}, "imported"},
		{"no marks", map[string]any{"fromCliTranscript": false, "adoptedFromOtherSurface": "yes"}, "own"},
		{"archived", map[string]any{"fromCliTranscript": true, "isArchived": true}, "archived"},
	} {
		rec := map[string]any{"sessionId": "local_" + sid, "cliSessionId": sid, "cwd": r.proj, "title": "went on in b"}
		maps.Copy(rec, c.marks)
		r.writeJSON(imported, rec)
		at := time.Unix(1_790_000_000+int64(i), 0) // read again, whatever its size
		must(t, os.Chtimes(imported, at, at))
		v := r.view(sid, file)
		if a := rowOf(v, keyA); !a.Checked || !a.Locked || a.Why != "own" {
			t.Fatalf("%s: the list it was made in: %+v", c.name, a)
		}
		if b := rowOf(v, keyB); !b.Locked || b.Checked != (c.why != "archived") || b.Why != c.why {
			t.Fatalf("%s: the list the app brought it into: %+v", c.name, b)
		}
	}
}
