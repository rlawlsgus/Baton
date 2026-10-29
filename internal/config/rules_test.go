package config

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRuleFor(t *testing.T) {
	var c Config
	for _, r := range []FolderRule{
		{`C:\work`, []string{"work", "work2"}},
		{`C:\work\secret\`, []string{"work"}},
		{`"D:\프로젝트"`, []string{"personal"}},
	} {
		if err := c.SetRule(r.Folder, r.Accounts); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string][]string{
		`C:\work`:                {"work", "work2"},
		`c:\WORK\app`:            {"work", "work2"},
		`C:\work\secret`:         {"work"},
		`C:\work\secret\deep\er`: {"work"},
		`C:\workshop`:            nil, // not inside C:\work
		`C:\`:                    nil,
		`D:\프로젝트\앱`:              {"personal"},
	}
	for dir, want := range cases {
		r := c.RuleFor(dir)
		if (r == nil) != (want == nil) || r != nil && !slices.Equal(r.Accounts, want) {
			t.Errorf("RuleFor(%s) = %+v, want %v", dir, r, want)
		}
	}
	if !c.RuleFor(`C:\work\x`).Allows("work2") || c.RuleFor(`C:\work\secret`).Allows("work2") || !(*FolderRule)(nil).Allows("any") {
		t.Fatal("Allows")
	}
	// The rule handed out is a copy.
	c.RuleFor(`C:\work`).Accounts[0] = "changed"
	if c.FolderRules[0].Accounts[0] != "work" {
		t.Fatal("RuleFor returned the config's own slice")
	}
	// Replace, remove, validate.
	if err := c.SetRule(`C:\WORK\`, []string{"work", "work", " "}); err != nil || len(c.FolderRules) != 3 || !slices.Equal(c.FolderRules[0].Accounts, []string{"work"}) {
		t.Fatalf("replace: %v %+v", err, c.FolderRules)
	}
	if !c.RemoveRule(`c:\work`) || c.RemoveRule(`C:\work`) || len(c.FolderRules) != 2 {
		t.Fatalf("remove: %+v", c.FolderRules)
	}
	for _, bad := range []FolderRule{{`relative\dir`, []string{"a"}}, {`\\server\share`, []string{"a"}}, {`C:\x`, nil}, {`C:\x`, []string{" "}}} {
		if err := c.SetRule(bad.Folder, bad.Accounts); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	dup := Defaults()
	dup.FolderRules = []FolderRule{{`C:\a`, []string{"x"}}, {`c:\A\`, []string{"y"}}}
	if dup.Validate() == nil {
		t.Fatal("two rules for one folder accepted")
	}
}

func TestStricter(t *testing.T) {
	ab := &FolderRule{Accounts: []string{"a", "b"}}
	a := &FolderRule{Accounts: []string{"a"}}
	c := &FolderRule{Accounts: []string{"c"}}
	cases := []struct {
		next, cur *FolderRule
		want      bool
	}{
		{nil, nil, true}, {ab, nil, true}, {a, ab, true}, {ab, ab, true},
		{nil, ab, false}, {ab, a, false}, {c, ab, false},
	}
	for _, x := range cases {
		if got := Stricter(x.next, x.cur); got != x.want {
			t.Errorf("Stricter(%v, %v) = %v", x.next, x.cur, got)
		}
	}
}

func TestRulesSurviveSaveAndLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Defaults()
	_ = c.SetRule(`C:\work`, []string{"work"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.RuleFor(`C:\work\app`) == nil {
		t.Fatalf("%v %+v", err, got.FolderRules)
	}
}
