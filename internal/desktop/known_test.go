package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// What baton keeps of the desktop app's sign-ins (Desk.Known) names the
// accounts none of baton's: right after a switch to one, the user's own
// Claude Code still names the account before, and the app may have been on
// several of them.
func TestKnownNamesTheAppsAccounts(t *testing.T) {
	const acctY, orgY = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "e0000000-0000-4000-8000-00000000000e"
	keyY := acctY + "/" + orgY
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	known := map[string][3]string{
		acctX: {orgX, "x@example.com", "kept"},
		acctY: {orgY, "y@example.com", "kept"},
		acctA: {orgA, "a@example.com", "kept"},
	}
	r.d.Known = func(acct string) (string, string, bool) {
		k, ok := known[acct]
		return k[0], k[1], ok
	}
	// The app is on X: nothing else names X's organization or email (two
	// lists, and the user's own Claude Code names no account).
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctX})
	must(t, os.MkdirAll(r.placeDir(keyX), 0o700))
	must(t, os.MkdirAll(filepath.Join(r.data, sessionsDir, acctX, orgA), 0o700))
	v := r.view(sid, file)
	x := rowOf(v, keyX)
	if !x.AppOnly || !x.Active || x.Email != "x@example.com" || !x.Kept {
		t.Fatalf("the app's account from what is kept: %+v", v.Rows)
	}
	if a := rowOf(v, keyA); !a.Kept {
		t.Fatalf("A is kept: %+v", a)
	}
	if b := rowOf(v, keyB); b.Kept {
		t.Fatalf("B is not kept: %+v", b)
	}
	r.apply(sid, file, keyX)

	// Switched to Y, none of baton's either: X is a former row, named by its
	// email; each goes by its own email, and "desktop" by both.
	must(t, os.MkdirAll(r.placeDir(keyY), 0o700))
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctY})
	v = r.view(sid, file)
	x, y := rowOf(v, keyX), rowOf(v, keyY)
	if !x.Former || !x.AppOnly || x.Email != "x@example.com" || !y.AppOnly || !y.Active || y.Email != "y@example.com" {
		t.Fatalf("two of the app's accounts: %+v", v.Rows)
	}
	if n := v.Named("x@example.com"); len(n) != 1 || n[0].Key != keyX {
		t.Fatalf("by X's email: %+v", n)
	}
	if n := v.Named("Y@example.com"); len(n) != 1 || n[0].Key != keyY {
		t.Fatalf("by Y's email: %+v", n)
	}
	if n := v.Named("desktop"); len(n) != 2 {
		t.Fatalf("desktop: %+v", n)
	}
	want, ok := v.Hide(v.Ticked(), "x@example.com")
	r.apply(sid, file, want...)
	if !ok || r.place(sid, keyX) != nil {
		t.Fatal("--hide by email left X")
	}
}

// Badges name each of the app's accounts by what is kept of it, once there
// can be two.
func TestBadgesKnown(t *testing.T) {
	const acctY = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	ix := Index{Threads: []*Thread{{Session: sid, Places: map[string]*Place{
		keyA:               {Key: keyA, State: stateShown, Label: "a"},
		keyX:               {Key: keyX, State: stateShown},
		acctY + "/" + orgX: {Key: acctY + "/" + orgX, State: stateReleased},
		"ffffffff-ffff-4fff-8fff-ffffffffffff/" + orgX: {Key: "ffffffff-ffff-4fff-8fff-ffffffffffff/" + orgX, State: stateHiding},
	}}}}
	if b := ix.Badges()[sid]; !slices.Equal(b.Places, []string{"", "", "a"}) {
		t.Fatalf("without what is kept: %v", b.Places)
	}
	known := func(acct string) (string, string, bool) {
		if acct == acctX {
			return orgX, "x@example.com", true
		}
		return "", "", false
	}
	if b := ix.BadgesKnown(known)[sid]; !slices.Equal(b.Places, []string{"a", "eeeeeeee", "x@example.com"}) {
		t.Fatalf("with what is kept: %v", b.Places)
	}
	one := Index{Threads: []*Thread{{Session: sid, Places: map[string]*Place{keyX: {Key: keyX, State: stateShown}}}}}
	if b := one.BadgesKnown(func(string) (string, string, bool) { return "", "", false })[sid]; !slices.Equal(b.Places, []string{""}) {
		t.Fatalf("one of the app's, unknown: %v", b.Places)
	}
}
