package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// `baton desktop <id> --hide <name>` takes the session off every list it
// shows in under that name, as the listing prints it: also a former row (no
// longer one of baton's accounts, or the desktop app's account before), whose
// names a current row may have now.
func TestHideReachesAFormerRow(t *testing.T) {
	const acctN, orgN = "dddddddd-dddd-4ddd-8ddd-dddddddddddd", "d0000000-0000-4000-8000-00000000000d"
	for _, name := range []string{"a", "a2"} {
		t.Run("its accounts are another Claude account now, --hide "+name, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			for i := range r.d.Accounts {
				if a := &r.d.Accounts[i]; a.ID == "a" || a.ID == "a2" {
					a.AccountUUID, a.OrgID = acctN, orgN
				}
			}
			r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctX})
			v := r.view(sid, file)
			if f := rowOf(v, keyA); !f.Former || !f.Checked || !slices.Contains(f.Accounts, name) {
				t.Fatalf("setup: %+v", f)
			}
			if n := v.Named(name); len(n) == 0 || n[0].Key != acctN+"/"+orgN {
				t.Fatalf("--show %s names %+v, not the current row first", name, n)
			}
			want, ok := v.Hide(v.Ticked(), name)
			res := r.apply(sid, file, want...)
			if p := r.place(sid, keyA); !ok || p != nil || r.record(keyA, "local_"+sid) != nil {
				t.Fatalf("baton desktop <id> --hide %s: %v %+v; the former list A still shows the session (place %+v)", name, ok, res, p)
			}
		})
	}
	t.Run("the desktop app's account before, --hide desktop", func(t *testing.T) {
		const acctY, orgY = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "e0000000-0000-4000-8000-00000000000e"
		keyY := acctY + "/" + orgY
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		must(t, os.MkdirAll(r.placeDir(keyX), 0o700)) // the app's own list
		r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctX})
		if x := rowOf(r.view(sid, file), keyX); !x.AppOnly {
			t.Fatalf("setup: %+v", x)
		}
		r.apply(sid, file, keyX)
		must(t, os.MkdirAll(r.placeDir(keyY), 0o700))
		r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctY})
		v := r.view(sid, file)
		if x, y := rowOf(v, keyX), rowOf(v, keyY); !x.Former || !x.AppOnly || !x.Checked || !y.AppOnly || y.Former {
			t.Fatalf("setup: former %+v, now %+v", x, y)
		}
		want, ok := v.Hide(v.Ticked(), "desktop")
		res := r.apply(sid, file, want...)
		if p := r.place(sid, keyX); !ok || p != nil {
			t.Fatalf("baton desktop <id> --hide desktop: %v %+v; the app's former account X still shows the session (place %+v)", ok, res, p)
		}
	})
	t.Run("an account of baton's named desktop", func(t *testing.T) {
		// The name is that account's: --hide desktop leaves the desktop app's
		// own account alone, as --show desktop picks the account.
		v := View{Rows: []Row{
			{Key: keyX, AppOnly: true, Checked: true},
			{Key: keyA, Accounts: []string{"desktop"}, Checked: true},
		}}
		if n := v.Named("desktop"); len(n) != 1 || n[0].Key != keyA {
			t.Fatalf("desktop names %+v, not only the account", n)
		}
		want, ok := v.Hide(v.Ticked(), "desktop")
		if !ok || !slices.Equal(want, []string{Kept(keyX)}) {
			t.Fatalf("--hide desktop: %v %v", ok, want)
		}
	})
	t.Run("a locked row is left", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.adopt(keyA, sid, 1)
		v := r.view(sid, file)
		if want, ok := v.Hide(v.Ticked(), "a"); ok || !slices.Equal(want, v.Ticked()) {
			t.Fatalf("--hide of a row the desktop app took over: %v %v", ok, want)
		}
	})
}
