package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// office_view is tidied when read and never stops config.json from loading.
func TestOfficeViewNormalize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const a, b = "11111111-2222-4333-8444-555555555555", "22222222-2222-4333-8444-555555555555"
	write(`{"office":true,"office_view":{"tab":{"left":-5,"right":99999,"left_folded":true,"last":"r"},"popout":{"left":300,"last":"middle"},
		"hidden_sessions":["` + a + `"," ","..\\x","` + b + `","` + a + `"],
		"hidden_floors":["c:\\p","","c:\\p","` + strings.Repeat("x", 2000) + `","c:\\q"]}}`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	v := c.OfficeView
	if v == nil || v.Tab != (OfficePanes{Left: 0, Right: OfficeWidthMax, LeftFolded: true, Last: "r"}) || v.Popout != (OfficePanes{Left: 300}) || !c.Office {
		t.Fatalf("panes %+v", v)
	}
	// A repeat keeps its later (newer) place.
	if fmt.Sprint(v.HiddenSessions) != fmt.Sprint([]string{b, a}) || fmt.Sprint(v.HiddenFloors) != fmt.Sprint([]string{`c:\p`, `c:\q`}) {
		t.Fatalf("lists %q %q", v.HiddenSessions, v.HiddenFloors)
	}

	// Too many: the newest stay.
	var ids []string
	for i := 0; i < OfficeHiddenSessionsMax+20; i++ {
		ids = append(ids, fmt.Sprintf("%08x-0000-4000-8000-000000000000", i))
	}
	v = (&OfficeView{HiddenSessions: ids}).Normalized()
	if len(v.HiddenSessions) != OfficeHiddenSessionsMax || v.HiddenSessions[0] != ids[20] || v.HiddenSessions[len(v.HiddenSessions)-1] != ids[len(ids)-1] {
		t.Fatalf("capped to %d, from %s", len(v.HiddenSessions), v.HiddenSessions[0])
	}
	if (&OfficeView{HiddenFloors: []string{" "}}).Normalized() != nil {
		t.Fatal("an empty view is kept")
	}

	// Whatever is broken in it, the rest of the file still loads.
	for _, bad := range []string{`"nope"`, `5`, `[1,2]`, `{"tab":"wide","hidden_sessions":"` + a + `"}`, `{"tab":{"left":"x"},"popout":{"right":280}}`} {
		write(`{"office":true,"cooldown_seconds":42,"office_view":` + bad + `}`)
		c, err := Load(path)
		if err != nil || !c.Office || c.CooldownSeconds != 42 {
			t.Fatalf("%s: %v %+v", bad, err, c)
		}
		if strings.Contains(bad, "280") && (c.OfficeView == nil || c.OfficeView.Popout.Right != 280) {
			t.Fatalf("%s: lost what could be read: %+v", bad, c.OfficeView)
		}
	}

	// Saved through Update, it comes back; SetHidden moves an item to the end.
	lock := filepath.Join(dir, "config.lock")
	if _, err := Update(path, lock, func(c *Config) error {
		c.OfficeView = &OfficeView{Tab: OfficePanes{Left: 250}, HiddenSessions: []string{a, b}}
		c.OfficeView.SetHidden(false, a, true)
		c.OfficeView.SetHidden(true, `c:\p`, true)
		c.OfficeView.SetHidden(true, `c:\q`, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path)
	if v := c.OfficeView; v == nil || v.Tab.Left != 250 || fmt.Sprint(v.HiddenSessions) != fmt.Sprint([]string{b, a}) || fmt.Sprint(v.HiddenFloors) != `[c:\p]` {
		t.Fatalf("after Update: %+v", c.OfficeView)
	}
	c.OfficeView.SetHidden(false, b, false)
	if fmt.Sprint(c.OfficeView.HiddenSessions) != fmt.Sprint([]string{a}) {
		t.Fatalf("shown again: %q", c.OfficeView.HiddenSessions)
	}
	// Not a setting of `baton config`.
	for _, k := range Keys {
		if strings.HasPrefix(k, "office_") {
			t.Fatalf("%s in Keys", k)
		}
	}
}
