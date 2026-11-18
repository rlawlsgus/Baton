package desklogin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"baton/internal/desktop"
	"baton/internal/fsx"
)

// The desktop app's config.json holds its window and app settings and,
// among them, six values that belong to the account it is signed in to: its
// two sealed token caches, which account that is, and three small caches of
// that account's. A switch moves exactly these, as the app wrote them, and
// leaves every other byte of the file as it is: the file is spliced, never
// encoded again (an encoder would escape <, & and U+2028, compact or
// re-indent values, and round big numbers).

// loginKeys are the six, in the order baton writes them back.
var loginKeys = []string{"oauth:tokenCache", "oauth:tokenCacheV2", "lastKnownAccountUuid",
	"bootSideSessions", "bootSidebarPlaceholder", "sshTransportFlagLastAnswer"}

// tokenKeys are the two that hold the sign-in itself: the only "oauth:" keys
// the app is known to write.
var tokenKeys = []string{"oauth:tokenCache", "oauth:tokenCacheV2"}

const accountKey = "lastKnownAccountUuid"

// member is one top-level member of a JSON object, by offsets into the
// whole file (a byte-order mark included).
type member struct {
	key      string // decoded
	start    int    // its key's opening quote
	keyEnd   int    // just after its key's closing quote
	valStart int
	end      int // just after its value
}

// object is a JSON object as read, with its top-level members.
type object struct {
	data    []byte
	open    int // its '{'
	close   int // its '}'
	members []member
}

// kv is one member to write: a key and its value's raw bytes.
type kv struct {
	key string
	raw []byte
}

// parseObject reads data as one JSON object. A file that is not one, or
// names a top-level key twice (which one the app reads is not known), is
// not a shape baton knows. The reason never quotes a value (key names only).
func parseObject(data []byte) (*object, string) {
	body := fsx.StripBOM(data)
	bom := len(data) - len(body)
	if !json.Valid(body) {
		return nil, "not_json"
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return nil, "not_an_object"
	}
	o := &object{data: data, open: bom + int(dec.InputOffset()) - 1}
	seen := map[string]bool{}
	for dec.More() {
		prev := int(dec.InputOffset())
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return nil, "not_an_object"
		}
		keyEnd := int(dec.InputOffset())
		start := prev
		for start < keyEnd && body[start] != '"' {
			start++ // whitespace and the comma before it
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, "not_an_object"
		}
		end := int(dec.InputOffset())
		if seen[key] {
			return nil, "duplicate_key " + key
		}
		seen[key] = true
		o.members = append(o.members, member{key: key, start: bom + start, keyEnd: bom + keyEnd,
			valStart: bom + end - len(raw), end: bom + end})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, "not_an_object"
	}
	o.close = bom + int(dec.InputOffset()) - 1
	if _, err := dec.Token(); err != io.EOF {
		return nil, "not_an_object"
	}
	return o, ""
}

func (o *object) text(m member) []byte { return o.data[m.start:m.end] }
func (o *object) raw(m member) []byte  { return o.data[m.valStart:m.end] }

func (o *object) get(key string) (member, bool) {
	i := slices.IndexFunc(o.members, func(m member) bool { return m.key == key })
	if i < 0 {
		return member{}, false
	}
	return o.members[i], true
}

// login are the six members present, as raw values, in loginKeys order.
func (o *object) login() []kv {
	var out []kv
	for _, k := range loginKeys {
		if m, ok := o.get(k); ok {
			out = append(out, kv{k, o.raw(m)})
		}
	}
	return out
}

// rest are the members other than the six, as their bytes, in order.
func (o *object) rest() [][]byte {
	var out [][]byte
	for _, m := range o.members {
		if !slices.Contains(loginKeys, m.key) {
			out = append(out, o.text(m))
		}
	}
	return out
}

// splice is the file with the six left out and add put in after the rest,
// every other byte as it was: the members kept keep their text and the
// separator before them, and the file its line ends and indent.
func (o *object) splice(add []kv) []byte {
	d := o.data
	lead, sep, colon, tail := []byte("\n  "), []byte(",\n  "), []byte(": "), []byte("\n")
	if n := len(o.members); n > 0 {
		lead, tail = d[o.open+1:o.members[0].start], d[o.members[n-1].end:o.close]
		colon = d[o.members[0].keyEnd:o.members[0].valStart]
		if n > 1 {
			sep = d[o.members[0].end:o.members[1].start]
		} else {
			sep = append([]byte(","), lead...)
		}
	}
	var out bytes.Buffer
	out.Write(d[:o.open+1])
	first := true
	emit := func(pre []byte, parts ...[]byte) {
		if first {
			out.Write(lead)
		} else {
			out.Write(pre)
		}
		for _, p := range parts {
			out.Write(p)
		}
		first = false
	}
	for i, m := range o.members {
		if slices.Contains(loginKeys, m.key) {
			continue
		}
		pre := sep
		if i > 0 {
			pre = d[o.members[i-1].end:m.start]
		}
		emit(pre, o.text(m))
	}
	for _, a := range add {
		emit(sep, []byte(`"`+a.key+`"`), colon, a.raw)
	}
	if !first {
		out.Write(tail)
	}
	out.Write(d[o.close:])
	return out.Bytes()
}

// appConfig is what a switch needs of the desktop app's config.json.
type appConfig struct {
	obj     *object
	account string // lastKnownAccountUuid in lower case ("" signed out)
	token   bool   // a sealed token is there: the app is signed in
}

// readAppConfig checks that data is a config.json shaped as baton knows it:
// one object, no key twice, no "oauth:" key but the two known (a new one may
// hold the sign-in now, which baton would leave behind), those two strings,
// and lastKnownAccountUuid an account id or empty. why says what is not
// (key names only, never a value).
func readAppConfig(data []byte) (cfg *appConfig, why string) {
	o, why := parseObject(data)
	if o == nil {
		return nil, why
	}
	cfg = &appConfig{obj: o}
	for _, m := range o.members {
		if strings.HasPrefix(strings.ToLower(m.key), "oauth:") && !slices.Contains(tokenKeys, m.key) {
			return nil, "unknown_key " + m.key
		}
	}
	for _, k := range tokenKeys {
		m, ok := o.get(k)
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(o.raw(m), &s) != nil {
			return nil, "not_a_string " + k
		}
		cfg.token = cfg.token || s != ""
	}
	if m, ok := o.get(accountKey); ok {
		var s *string
		if json.Unmarshal(o.raw(m), &s) != nil {
			return nil, "not_a_string " + accountKey
		}
		if s != nil {
			a := strings.ToLower(strings.TrimSpace(*s))
			if a != "" && !desktop.ValidID(a) {
				return nil, "not_an_account " + accountKey
			}
			cfg.account = a
		}
	}
	return cfg, ""
}

// keysFile is the six members of a kept sign-in as keys.json holds them:
// one object, each value as the app wrote it.
func keysFile(login []kv) []byte {
	var b bytes.Buffer
	b.WriteString("{")
	for i, a := range login {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n  \"" + a.key + "\": ")
		b.Write(a.raw)
	}
	b.WriteString("\n}\n")
	return b.Bytes()
}

// readKeys reads keys.json back: only the six, each once.
func readKeys(data []byte) ([]kv, bool) {
	o, why := parseObject(data)
	if o == nil || why != "" {
		return nil, false
	}
	for _, m := range o.members {
		if !slices.Contains(loginKeys, m.key) {
			return nil, false
		}
	}
	return o.login(), true
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// sameRest reports whether b keeps every member of a other than the six,
// byte for byte and in order, and has the six exactly as want.
func sameRest(a, b *object, want []kv) bool {
	ra, rb := a.rest(), b.rest()
	if len(ra) != len(rb) {
		return false
	}
	for i := range ra {
		if !bytes.Equal(ra[i], rb[i]) {
			return false
		}
	}
	got := b.login()
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].key != want[i].key || !bytes.Equal(got[i].raw, want[i].raw) {
			return false
		}
	}
	return true
}
