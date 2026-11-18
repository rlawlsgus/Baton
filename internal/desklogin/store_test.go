package desklogin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"baton/internal/desktop"
)

// The splice keeps every other byte: escapes the app chose (<, & and
// U+2028 as they are, \u escapes as they are), non-ASCII text, numbers no
// float holds, nested indent, CRLF line ends and a byte-order mark.
func TestSpliceKeepsBytes(t *testing.T) {
	for _, nl := range []string{"\n", "\r\n"} {
		in := appCfg(withNL(loginOf(acctA, "A"), nl), nl)
		o, why := parseObject(in)
		if o == nil {
			t.Fatal(why)
		}
		if got, want := string(o.splice(nil)), string(appCfg(nil, nl)); got != want {
			t.Fatalf("without the six (%q):\n%s\nwant:\n%s", nl, got, want)
		}
		// The six put back where the app puts them: last but one there, last
		// here, each value as it was.
		b := withNL(loginOf(acctB, "B"), nl)
		out := o.splice(b)
		p, why := parseObject(out)
		if p == nil {
			t.Fatal(why)
		}
		if !sameRest(o, p, b) {
			t.Fatal("other members changed")
		}
		if !strings.Contains(string(out), "123456789012345678901234567890") || !strings.Contains(string(out), `\u2028 raw:`+string(rune(0x2028))) ||
			!strings.Contains(string(out), "한국어 ✓") || !strings.Contains(string(out), `<b>&amp;</b>`) {
			t.Fatalf("a value changed:\n%s", out)
		}
		if nl == "\r\n" && strings.Count(string(out), "\r\n") != strings.Count(string(out), "\n") {
			t.Fatal("a line end changed")
		}
	}
	// The six last already, as the app writes them: put back as they were,
	// the file is byte for byte the same.
	last := []byte("{\n  \"locale\": \"ko\",\n  \"oauth:tokenCache\": \"s\",\n  \"lastKnownAccountUuid\": \"" + acctA + "\"\n}")
	o, _ := parseObject(last)
	if got := o.splice(o.login()); string(got) != string(last) {
		t.Fatalf("round trip:\n%s", got)
	}
	// A byte-order mark stays.
	bom := append([]byte("\xef\xbb\xbf"), last...)
	o, _ = parseObject(bom)
	if got := o.splice(nil); !strings.HasPrefix(string(got), "\xef\xbb\xbf{\n  \"locale\": \"ko\"\n}") {
		t.Fatalf("bom: %q", got)
	}
	// Only the six, or nothing at all.
	only := []byte(`{"oauth:tokenCache":"s","lastKnownAccountUuid":"` + acctA + `"}`)
	o, _ = parseObject(only)
	if got := string(o.splice(nil)); got != "{}" {
		t.Fatalf("only the six: %q", got)
	}
	o, _ = parseObject([]byte("{}"))
	if got := string(o.splice([]kv{{"lastKnownAccountUuid", []byte(`"` + acctA + `"`)}})); got != "{\n  \"lastKnownAccountUuid\": \""+acctA+"\"\n}" {
		t.Fatalf("into an empty one: %q", got)
	}
	// One member, compact.
	o, _ = parseObject([]byte(`{"a":1}`))
	if got := string(o.splice([]kv{{"oauth:tokenCache", []byte(`"s"`)}})); got != `{"a":1,"oauth:tokenCache":"s"}` {
		t.Fatalf("compact: %q", got)
	}
}

func TestAppConfigShape(t *testing.T) {
	for name, c := range map[string]struct {
		in      string
		why     string
		account string
		token   bool
	}{
		"signed in":            {in: `{"oauth:tokenCacheV2":"s","lastKnownAccountUuid":"` + strings.ToUpper(acctA) + `"}`, account: acctA, token: true},
		"signed out":           {in: `{"locale":"ko"}`},
		"empty token":          {in: `{"oauth:tokenCache":"","lastKnownAccountUuid":""}`},
		"null account":         {in: `{"lastKnownAccountUuid":null}`},
		"a key twice":          {in: `{"a":1,"a":2}`, why: "duplicate_key a"},
		"a key twice, escaped": {in: `{"oauth:tokenCache":"s","oauth:tokenCache":"t"}`, why: "duplicate_key oauth:tokenCache"},
		"a new oauth key":      {in: `{"OAuth:Other":"x"}`, why: "unknown_key OAuth:Other"},
		"a token no string":    {in: `{"oauth:tokenCache":1}`, why: "not_a_string oauth:tokenCache"},
		"an account no id":     {in: `{"lastKnownAccountUuid":"x"}`, why: "not_an_account lastKnownAccountUuid"},
		"an account no string": {in: `{"lastKnownAccountUuid":5}`, why: "not_a_string lastKnownAccountUuid"},
		"an array":             {in: `[]`, why: "not_an_object"},
		"two values":           {in: `{} {}`, why: "not_json"},
		"cut":                  {in: `{"a":`, why: "not_json"},
	} {
		cfg, why := readAppConfig([]byte(c.in))
		if why != c.why {
			t.Errorf("%s: %q, want %q", name, why, c.why)
			continue
		}
		if why == "" && (cfg.account != c.account || cfg.token != c.token) {
			t.Errorf("%s: %+v", name, cfg)
		}
		// A reason names keys only, never a value.
		if strings.Contains(why, `"`) {
			t.Errorf("%s: %q quotes", name, why)
		}
	}
}

// A name is a label, a baton account's id, an email, or an account id or
// 8 or more of its first hex digits; one that fits two is refused.
func TestResolve(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.signIn(acctC, "C") // the app is on C now, kept or not
	if err := r.l.Rename(acctB, "Laptop"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"work": acctA, "WORK": acctA, "a@example.com": acctA, "home": acctB, "b@example.com": acctB, "laptop": acctB,
		acctB: acctB, strings.ToUpper(acctB): acctB, "bbbbbbbb": acctB, " bbbbbbbb-b ": acctB, "aaaaaaaa-c": acctC,
		"aaaaaaaa-aaaa": acctA, "dddddddd-dddd-4ddd-8ddd-dddddddddddd": "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	} {
		got, err := r.l.Resolve(name)
		if err != nil || got != want {
			t.Errorf("%q: %s %v, want %s", name, got, err, want)
		}
	}
	for name, code := range map[string]string{"aaaaaaaa": CodeAmbiguous, "bbbb": CodeNotKept, "nobody": CodeNotKept, "": CodeNotKept} {
		_, err := r.l.Resolve(name)
		wantCode(t, name, err, code)
	}
	_, err := r.l.Resolve("aaaaaaaa")
	if d := err.(*desktop.Error).Detail; !strings.Contains(d, "work") || !strings.Contains(d, "aaaaaaaa") {
		t.Fatalf("ambiguous names: %q", d)
	}
}

func TestLabels(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	for lbl, why := range map[string]string{
		"home": "taken", "B@example.com": "taken", strings.Repeat("x", 41): "too_long", "a\nb": "characters",
		"a,b": "characters", "deadbeef": "looks_like_id", "dead-beef-0": "looks_like_id",
	} {
		err := r.l.Rename(acctA, lbl)
		if e, ok := err.(*desktop.Error); !ok || e.Code != CodeBadLabel || e.Detail != why {
			t.Errorf("%q: %v, want %s", lbl, err, why)
		}
	}
	must(t, r.l.Rename(acctA, "  Office "))
	wantCode(t, "a label another has", r.l.Rename(acctB, "office"), CodeBadLabel)
	must(t, r.l.Rename(acctA, "work")) // its own baton id
	wantCode(t, "one not kept", r.l.Rename(acctC, "x"), CodeNotKept)
	must(t, r.l.Rename(acctA, "Office"))
	v, err := r.l.View()
	must(t, err)
	if v.Logins[0].Account != acctA || v.Logins[0].Label != "Office" || v.Logins[0].Named != "Office" {
		t.Fatalf("view: %+v", v.Logins[0])
	}
	must(t, r.l.Rename(acctA, ""))
	if v, _ := r.l.View(); v.Logins[0].Label != "work" {
		t.Fatalf("label taken off: %+v", v.Logins[0])
	}
}

// The view: the account the app is on first (kept or not yet), then
// baton's accounts in baton's order, then the rest by label.
func TestView(t *testing.T) {
	r := newRig(t)
	v, err := r.l.View()
	must(t, err)
	if !v.On || v.Active != acctA || v.Kept != 0 || len(v.Logins) != 1 || v.Logins[0].Kept || !v.Logins[0].Active ||
		v.Logins[0].Label != "work" || v.CutOff || v.Running || v.Problem != "" || v.Store != r.l.Store {
		t.Fatalf("before anything is kept: %+v", v)
	}
	r.keptAll()
	r.signIn(acctC, "C")
	_, err = r.l.Blank() // C is kept too, none of baton's
	must(t, err)
	r.signIn(acctB, "B2")
	v, err = r.l.View()
	must(t, err)
	var got []string
	for _, g := range v.Logins {
		got = append(got, g.Label)
	}
	if strings.Join(got, " ") != "home work aaaaaaaa" || v.Kept != 3 || !v.Logins[0].Active || !v.Logins[0].Kept {
		t.Fatalf("order %v, %+v", got, v)
	}
	if g := v.Logins[1]; g.Email != "a@example.com" || g.Org != orgA || g.OrgName != "A org" || len(g.Baton) != 1 || g.KeptAt.IsZero() {
		t.Fatalf("A: %+v", g)
	}
	org, email, kept := r.l.Known(acctA)
	if org != orgA || email != "a@example.com" || !kept {
		t.Fatalf("known A: %s %s %v", org, email, kept)
	}
	if _, _, kept := r.l.Known("dddddddd-dddd-4ddd-8ddd-dddddddddddd"); kept {
		t.Fatal("known: one never kept")
	}
	t.Setenv("BATON_DESKTOP_DATA", "")
	if v, _ := r.l.View(); v.Problem != CodeUnsupportedLayout {
		t.Fatalf("problem: %q", v.Problem)
	}
}

// Drop deletes one kept sign-in, DropAll all of them and the store; both
// work while the app runs.
func TestDrop(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	must(t, r.l.Drop(strings.ToUpper(acctB)))
	if exists(filepath.Join(r.l.Store, acctB)) {
		t.Fatal("B is still kept")
	}
	wantCode(t, "drop of one not kept", r.l.Drop(acctB), CodeNotKept)
	wantCode(t, "drop of no id", r.l.Drop(`..\x`), CodeNotKept)
	if n, err := r.l.DropAll(); err != nil || n != 1 || exists(r.l.Store) {
		t.Fatalf("drop all: %d %v", n, err)
	}
	if n, err := r.l.DropAll(); err != nil || n != 0 {
		t.Fatalf("drop all of nothing: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(r.data, "config.json")); err != nil {
		t.Fatal(err)
	}
}

// withNL is login with nl as the line end inside its values, as an app that
// writes CRLF would have them.
func withNL(login []kv, nl string) []kv {
	out := make([]kv, len(login))
	for i, k := range login {
		out[i] = kv{k.key, []byte(strings.ReplaceAll(string(k.raw), "\n", nl))}
	}
	return out
}
