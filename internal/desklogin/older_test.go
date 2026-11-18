package desklogin

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"baton/internal/desktop"
	"baton/internal/fsx"
)

// The layouts of the app's folders baton switches: the packaged app's one
// Claude-Data\claude_* folder with a config.json, and the older
// installer's %APPDATA%\Claude while no Claude-Data\claude_* folder holds
// one and the packaged app has not taken the data from it. Anything else
// is refused, and says why when baton can tell.
func TestLayouts(t *testing.T) {
	type setup struct {
		la, ad string
		mk     func(dir string, cfg bool) string
	}
	newSetup := func(t *testing.T) setup {
		root := t.TempDir()
		s := setup{la: filepath.Join(root, "local"), ad: filepath.Join(root, "roaming")}
		s.mk = func(dir string, cfg bool) string {
			must(t, os.MkdirAll(dir, 0o700))
			if cfg {
				must(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600))
			}
			return dir
		}
		return s
	}
	packaged := func(s setup, name string, cfg bool) string {
		return s.mk(filepath.Join(s.la, "Claude-Data", name), cfg)
	}
	older := func(s setup, cfg bool) string { return s.mk(filepath.Join(s.ad, "Claude"), cfg) }

	cases := []struct {
		name string
		run  func(t *testing.T, s setup) (la, ad, data string)
		want string
	}{
		{"packaged", func(t *testing.T, s setup) (string, string, string) {
			packaged(s, "claude_nothing", false) // holds no config: not the app's
			return s.la, s.ad, packaged(s, "claude_pzs8sxrjxfjjc", true)
		}, ""},
		{"packaged, and an older folder the app took its data from", func(t *testing.T, s setup) (string, string, string) {
			s.mk(filepath.Join(older(s, true), ".user-data-left.claude_pzs8sxrjxfjjc"), false)
			return s.la, s.ad, packaged(s, "claude_pzs8sxrjxfjjc", true)
		}, ""},
		{"two packaged folders", func(t *testing.T, s setup) (string, string, string) {
			packaged(s, "claude_second", true)
			return s.la, s.ad, packaged(s, "claude_pzs8sxrjxfjjc", true)
		}, LayoutSeveral},
		{"Claude-Data itself", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, s.mk(filepath.Join(s.la, "Claude-Data"), true)
		}, LayoutOther},
		{"no local app data", func(t *testing.T, s setup) (string, string, string) {
			return "", s.ad, packaged(s, "claude_pzs8sxrjxfjjc", true)
		}, LayoutOther},
		{"packaged folder without a config", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, packaged(s, "claude_pzs8sxrjxfjjc", false)
		}, LayoutOther},
		{"a link", func(t *testing.T, s setup) (string, string, string) {
			target := s.mk(filepath.Join(s.la, "elsewhere"), true)
			link := filepath.Join(s.la, "Claude-Data", "claude_pzs8sxrjxfjjc")
			must(t, os.MkdirAll(filepath.Dir(link), 0o700))
			must(t, fsx.CreateJunction(link, target))
			return s.la, s.ad, link
		}, LayoutLink},
		{"gone", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, filepath.Join(s.la, "Claude-Data", "claude_pzs8sxrjxfjjc")
		}, LayoutOther},
		{"older installer", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, older(s, true)
		}, ""},
		{"older installer, and a packaged folder with no config", func(t *testing.T, s setup) (string, string, string) {
			packaged(s, "claude_pzs8sxrjxfjjc", false)
			return s.la, s.ad, older(s, true)
		}, ""},
		{"older installer without a config", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, older(s, false)
		}, LayoutOther},
		{"older folder the packaged app took its data from", func(t *testing.T, s setup) (string, string, string) {
			data := older(s, true)
			s.mk(filepath.Join(data, ".user-data-left.claude_pzs8sxrjxfjjc"), false)
			return s.la, s.ad, data
		}, LayoutOther},
		{"older folder while a packaged folder holds a config", func(t *testing.T, s setup) (string, string, string) {
			packaged(s, "claude_pzs8sxrjxfjjc", true)
			return s.la, s.ad, older(s, true)
		}, LayoutOther},
		{"older folder, no roaming app data", func(t *testing.T, s setup) (string, string, string) {
			return s.la, "", older(s, true)
		}, LayoutOther},
		{"another folder named Claude", func(t *testing.T, s setup) (string, string, string) {
			return s.la, s.ad, s.mk(filepath.Join(s.la, "Claude"), true)
		}, LayoutOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			la, ad, data := c.run(t, newSetup(t))
			if got := layout(la, ad, data); got != c.want {
				t.Fatalf("layout = %q, want %q", got, c.want)
			}
		})
	}

	// While Claude-Data cannot be listed, a packaged folder in it may hold
	// a config.json: the older folder is not taken for the app's then.
	t.Run("older folder while Claude-Data is held", func(t *testing.T) {
		s := newSetup(t)
		packaged(s, "claude_pzs8sxrjxfjjc", false)
		data := older(s, true)
		release := holdFolder(t, filepath.Join(s.la, "Claude-Data"))
		if got := layout(s.la, s.ad, data); got != LayoutOther {
			t.Fatalf("held: layout = %q", got)
		}
		release()
		if got := layout(s.la, s.ad, data); got != "" {
			t.Fatalf("let go: layout = %q", got)
		}
	})
}

// The app from the older installer keeps its data in %APPDATA%\Claude: a
// switch there works as in the packaged app's folder. Once the packaged
// app took the data from there, the folder is not the app's any more, and
// a switch changes nothing.
func TestOlderInstallSwitches(t *testing.T) {
	r := newRigAt(t, filepath.Join("roaming", "Claude"))
	// The app's folders as baton reads them for real.
	t.Setenv("BATON_DESKTOP_DATA", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(r.root, "local"))
	r.l.dataFn = func() string { return r.data }
	if v, err := r.l.View(); err != nil || v.Problem != "" || v.Active != acctA {
		t.Fatalf("view: %+v %v", v, err)
	}
	r.keptAll()
	if _, err := r.l.Use(acctB); err != nil {
		t.Fatal(err)
	}
	if desktop.Active(r.data) != acctB || r.read(filepath.Join("Network", "Cookies")) != "cookies-B" {
		t.Fatal("not switched to B")
	}
	for p, c := range untouched {
		if r.read(p) != c {
			t.Fatalf("%s changed", p)
		}
	}
	r.noStage()

	r.write(".user-data-left.claude_pzs8sxrjxfjjc", "")
	before := itemsIn(t, r.data)
	v, err := r.l.View()
	if err != nil || v.Problem != CodeUnsupportedLayout || v.ProblemDetail != LayoutOther {
		t.Fatalf("view once the packaged app took the data: %+v %v", v, err)
	}
	wantCode(t, "use", useErr(r.l.Use(acctA)), CodeUnsupportedLayout)
	if desktop.Active(r.data) != acctB || !mapsEqual(itemsIn(t, r.data), before) {
		t.Fatal("the folder changed")
	}
}

// With no data folder of the app's at all, the refusal says so.
func TestNoAppSaysSo(t *testing.T) {
	r := newRig(t)
	t.Setenv("BATON_DESKTOP_DATA", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(r.root, "none"))
	r.l.dataFn = func() string { return "" }
	v, err := r.l.View()
	if err != nil || v.Problem != CodeUnsupportedLayout || v.ProblemDetail != LayoutNoApp {
		t.Fatalf("view: %+v %v", v, err)
	}
	_, err = r.l.Blank()
	var e *desktop.Error
	if !errors.As(err, &e) || e.Code != CodeUnsupportedLayout || e.Detail != LayoutNoApp {
		t.Fatalf("blank: %v", err)
	}
}

// The older installer's app is opened by its Update.exe, which starts the
// newest version it installed; its version is that folder's.
func TestOlderInstallOpensAndVersion(t *testing.T) {
	root := t.TempDir()
	la, ad := filepath.Join(root, "local"), filepath.Join(root, "roaming")
	data := filepath.Join(ad, "Claude")
	inst := filepath.Join(la, "AnthropicClaude")
	file := func(p string) {
		must(t, os.MkdirAll(filepath.Dir(p), 0o700))
		must(t, os.WriteFile(p, nil, 0o600))
	}
	file(filepath.Join(data, "config.json"))
	file(filepath.Join(inst, "Update.exe"))
	for _, v := range []string{"5.11492.0", "5.11492.10", "5.9.1"} {
		file(filepath.Join(inst, "app-"+v, "claude.exe"))
	}
	must(t, os.MkdirAll(filepath.Join(inst, "app-5.99.0"), 0o700)) // no app in it
	file(filepath.Join(inst, "app-6.0-beta", "claude.exe"))        // not a version
	file(filepath.Join(inst, "app-", "claude.exe"))
	must(t, os.MkdirAll(filepath.Join(inst, "packages"), 0o700))

	if dir, v := newestApp(inst); dir != filepath.Join(inst, "app-5.11492.10") || v != "5.11492.10" {
		t.Fatalf("newest: %s %s", dir, v)
	}
	if up := olderUpdater(la, ad, data); up != filepath.Join(inst, "Update.exe") {
		t.Fatalf("updater: %q", up)
	}
	t.Setenv("BATON_DESKTOP_DATA", "")
	t.Setenv("LOCALAPPDATA", la)
	t.Setenv("APPDATA", ad)
	if v := appVersion(data); v != "5.11492.10" {
		t.Fatalf("version: %q", v)
	}
	packagedDir := filepath.Join(la, "Claude-Data", "claude_pzs8sxrjxfjjc")
	file(filepath.Join(packagedDir, "config.json"))
	if up := olderUpdater(la, ad, packagedDir); up != "" {
		t.Fatalf("updater for the packaged app's folder: %q", up)
	}
	file(filepath.Join(data, ".user-data-left.claude_pzs8sxrjxfjjc"))
	if up, v := olderUpdater(la, ad, data), appVersion(data); up != "" || v != "" {
		t.Fatalf("once the packaged app took the data: %q %q", up, v)
	}
	must(t, os.Remove(filepath.Join(data, ".user-data-left.claude_pzs8sxrjxfjjc")))
	must(t, os.Remove(filepath.Join(inst, "Update.exe")))
	if up := olderUpdater(la, ad, data); up != "" {
		t.Fatalf("no Update.exe: %q", up)
	}

	for v, want := range map[string][]int{"5.11492.5": {5, 11492, 5}, "7": {7}, "": nil, "5..1": nil, "+5.1": nil, "5.1-beta": nil, "-1.2": nil} {
		if got := versionNums(v); !slices.Equal(got, want) || (got == nil) != (want == nil) {
			t.Errorf("versionNums(%q) = %v", v, got)
		}
	}
}
