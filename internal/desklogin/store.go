package desklogin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"baton/internal/claude"
	"baton/internal/desktop"
	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// The store keeps one folder per account the desktop app was signed in to:
//
//	<store>\<accountUuid>\  meta.json, keys.json, and the login set as the app left it
//	<store>\.keep-<accountUuid>-<rand>\  a keep being written; meta.json, written last, says it is whole
//	<store>\.old-<accountUuid>-<rand>\   the copy a keep replaces, set aside until the new one is in
//	<store>\unnamed-<when>\  what a signed-out app left of a login set, set aside by a switch (see stash)
//
// A keep never leaves an account without a whole copy: the sweep, run under
// the lock before anything else, puts a whole .keep or the .old back when a
// stop came between the two renames, and only then deletes what is left.

// items are the login set: what of the desktop app's data folder belongs to
// the account it is signed in to, moved in this order. Folders move whole.
var items = []struct {
	name string
	dir  bool
}{
	{"Local Storage", true}, {"Session Storage", true}, {"IndexedDB", true}, {"WebStorage", true},
	{filepath.Join("Network", "Cookies"), false}, {filepath.Join("Network", "Cookies-journal"), false},
}

func knownItem(name string) bool {
	return slices.ContainsFunc(items, func(it struct {
		name string
		dir  bool
	}) bool {
		return it.name == name
	})
}

// maxSet is the most a login set may hold; more is not what baton knows.
const maxSet = 512 << 20

const metaSchema = 1

// meta is what baton notes of a kept sign-in. It holds no token, and no hash
// of one.
type meta struct {
	Schema     int       `json:"schema"`
	Account    string    `json:"account"`
	Org        string    `json:"org,omitempty"` // organization id, when known
	OrgName    string    `json:"org_name,omitempty"`
	Email      string    `json:"email,omitempty"`
	Label      string    `json:"label,omitempty"` // the name the user gave it
	FirstKept  time.Time `json:"first_kept"`
	Kept       time.Time `json:"kept"`
	Used       time.Time `json:"used,omitzero"`
	AppVersion string    `json:"app_version,omitempty"`
	Keys       []string  `json:"keys"`  // the members keys.json holds
	Has        []string  `json:"has"`   // the items of the login set it holds
	Items      []file    `json:"items"` // every file of them, to check it is whole
	Size       int64     `json:"size"`
}

type file struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func rnd() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func (l *Logins) acctDir(acct string) string { return filepath.Join(l.Store, acct) }

// readMeta reads the meta.json of a kept folder.
func readMeta(dir string) (*meta, error) {
	var m meta
	if err := fsx.ReadJSON(filepath.Join(dir, "meta.json"), &m); err != nil {
		return nil, err
	}
	if m.Schema != metaSchema {
		return nil, errors.New("meta.json: unknown schema")
	}
	return &m, nil
}

func writeMeta(dir string, m *meta) error {
	p := filepath.Join(dir, "meta.json")
	desktop.Guard("write", p)
	return fsx.WriteJSONAtomic(p, m)
}

// checkFolder says whether dir holds a whole kept sign-in of acct: its
// meta, keys.json with the account in it, and every file the meta lists at
// its size. "" when it does, else not_kept or damaged.
func checkFolder(dir, acct string) string {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() || fsx.IsReparsePoint(fi) {
		return CodeNotKept
	}
	m, err := readMeta(dir)
	if err != nil || m.Account != acct {
		return CodeDamaged
	}
	data, err := fsx.ReadFileShared(filepath.Join(dir, "keys.json"))
	if err != nil {
		return CodeDamaged
	}
	keys, ok := readKeys(data)
	if !ok || len(keys) != len(m.Keys) {
		return CodeDamaged
	}
	named := ""
	for i, k := range keys {
		if k.key != m.Keys[i] {
			return CodeDamaged
		}
		if k.key == accountKey {
			var s string
			if jsonString(k.raw, &s) {
				named = strings.ToLower(strings.TrimSpace(s))
			}
		}
	}
	if named != acct {
		return CodeDamaged
	}
	for _, f := range m.Items {
		if !safeRel(f.Path) {
			return CodeDamaged
		}
		fi, err := os.Lstat(filepath.Join(dir, f.Path))
		if err != nil || fi.IsDir() || fsx.IsReparsePoint(fi) || fi.Size() != f.Size {
			return CodeDamaged
		}
	}
	for _, h := range m.Has {
		if !knownItem(h) || !exists(filepath.Join(dir, h)) {
			return CodeDamaged
		}
	}
	return ""
}

// safeRel: p names something inside the folder it is read against.
func safeRel(p string) bool {
	return p != "" && !filepath.IsAbs(p) && filepath.VolumeName(p) == "" && fsx.IsWithin(filepath.Join(`C:\x`, p), `C:\x`)
}

// keep copies the sign-in the desktop app has now (data folder data, its
// config cfg, the account acct) into the store, replacing what was kept of
// acct before: its tokens may have been renewed since. The copy is whole
// before the old one goes.
func (l *Logins) keep(data, acct string, cfg *appConfig) error {
	if err := l.makeStore(); err != nil {
		return err
	}
	l.readme()
	dst := l.acctDir(acct)
	old, _ := readMeta(dst)
	tmp := filepath.Join(l.Store, ".keep-"+acct+"-"+rnd())
	if err := mkdir(tmp); err != nil {
		return err
	}
	m := &meta{Schema: metaSchema, Account: acct, Kept: l.Now(), AppVersion: appVersion(data)}
	err := func() error {
		for _, it := range items {
			src := filepath.Join(data, it.name)
			if !exists(src) {
				continue
			}
			files, size, err := copyTree(src, filepath.Join(tmp, it.name), it.name)
			if err != nil {
				return err
			}
			m.Has = append(m.Has, it.name)
			m.Items = append(m.Items, files...)
			m.Size += size
		}
		login := cfg.obj.login()
		for _, k := range login {
			m.Keys = append(m.Keys, k.key)
		}
		p := filepath.Join(tmp, "keys.json")
		desktop.Guard("write", p)
		if err := fsx.WriteFileAtomic(p, keysFile(login), 0o600); err != nil {
			return err
		}
		m.Org, m.OrgName, m.Email = l.identify(data, acct, old)
		m.FirstKept = m.Kept
		if old != nil {
			m.Label, m.Used = old.Label, old.Used
			if !old.FirstKept.IsZero() {
				m.FirstKept = old.FirstKept
			}
		}
		if err := l.hook("keep"); err != nil {
			return err
		}
		// Written last: a .keep with its meta is whole.
		return writeMeta(tmp, m)
	}()
	if errors.Is(err, errCrash) {
		return err
	}
	if err == nil {
		if why := checkFolder(tmp, acct); why != "" {
			err = errors.New("the copy is not whole")
		}
	}
	if err != nil {
		removeTree(tmp)
		return err
	}
	if !exists(dst) {
		return rename(tmp, dst)
	}
	aside := filepath.Join(l.Store, ".old-"+acct+"-"+rnd())
	if err := rename(dst, aside); err != nil {
		removeTree(tmp)
		return err
	}
	err = l.hook("keep_aside")
	if errors.Is(err, errCrash) {
		return err
	}
	if err == nil {
		err = rename(tmp, dst)
	}
	if err != nil {
		if rename(aside, dst) != nil {
			return err // the sweep puts one of them in place
		}
		removeTree(tmp)
		return err
	}
	removeTree(aside)
	return nil
}

// stashPrefix names what the app left of a login set while no token was
// recorded in it (signed out, or quit before it recorded one), set aside in
// the store by a switch: unnamed-<when, local time>[-n]. No account was
// recorded with it, so it is never put back; it goes only when the user
// forgets it, or every kept sign-in.
const stashPrefix = "unnamed-"

var stashRx = regexp.MustCompile(`^unnamed-[0-9]{8}-[0-9]{6}(-[0-9]+)?$`)

// isStash: name is a stash's folder name.
func isStash(name string) bool { return stashRx.MatchString(name) }

// leftovers: the app's folder holds something of a login set, or config.json
// one of the six values.
func leftovers(data string, cfg *appConfig) bool {
	return len(cfg.obj.login()) > 0 || itemsLeft(data)
}

// itemsLeft: the app's folder holds something of a login set (its six
// values aside).
func itemsLeft(data string) bool {
	return slices.ContainsFunc(items, func(it struct {
		name string
		dir  bool
	}) bool {
		return exists(filepath.Join(data, it.name))
	})
}

// stash copies what the app left of a login set with no token recorded
// (items, and those of the six values it wrote) into the store as
// unnamed-<when>, before the switch moves it out of the app: its name. It
// is written as a keep is, in a .keep- folder the sweep deletes if a stop
// leaves it, with meta.json last.
func (l *Logins) stash(data string, cfg *appConfig) (string, error) {
	if err := l.makeStore(); err != nil {
		return "", err
	}
	l.readme()
	tmp := filepath.Join(l.Store, ".keep-"+stashPrefix+rnd())
	if err := mkdir(tmp); err != nil {
		return "", err
	}
	now := l.Now()
	m := &meta{Schema: metaSchema, Kept: now, FirstKept: now, AppVersion: appVersion(data)}
	err := func() error {
		for _, it := range items {
			src := filepath.Join(data, it.name)
			if !exists(src) {
				continue
			}
			files, size, err := copyTree(src, filepath.Join(tmp, it.name), it.name)
			if err != nil {
				return err
			}
			m.Has = append(m.Has, it.name)
			m.Items = append(m.Items, files...)
			m.Size += size
		}
		login := cfg.obj.login()
		for _, k := range login {
			m.Keys = append(m.Keys, k.key)
		}
		p := filepath.Join(tmp, "keys.json")
		desktop.Guard("write", p)
		if err := fsx.WriteFileAtomic(p, keysFile(login), 0o600); err != nil {
			return err
		}
		if err := l.hook("stash"); err != nil {
			return err
		}
		return writeMeta(tmp, m)
	}()
	if errors.Is(err, errCrash) {
		return "", err
	}
	if err != nil {
		removeTree(tmp)
		return "", err
	}
	base := stashPrefix + now.Local().Format("20060102-150405")
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		if exists(filepath.Join(l.Store, name)) {
			continue
		}
		if err := rename(tmp, filepath.Join(l.Store, name)); err != nil {
			removeTree(tmp)
			return "", err
		}
		return name, nil
	}
}

// stashes are what signed-out apps left, set aside: oldest first.
func (l *Logins) stashes() []Unnamed {
	ents, _ := os.ReadDir(l.Store)
	out := []Unnamed{}
	for _, e := range ents {
		if !e.IsDir() || !isStash(e.Name()) {
			continue
		}
		u := Unnamed{Name: e.Name()}
		if m, err := readMeta(filepath.Join(l.Store, e.Name())); err == nil {
			u.KeptAt, u.Size = m.Kept, m.Size
		}
		out = append(out, u)
	}
	slices.SortStableFunc(out, func(a, b Unnamed) int {
		if c := a.KeptAt.Compare(b.KeptAt); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// sweep finishes what a keep left when it was stopped: an account without
// its folder gets a whole .keep of it, else the copy set aside. Only then
// are the rest deleted. Under the lock.
func (l *Logins) sweep() {
	ents, err := os.ReadDir(l.Store)
	if err != nil {
		return
	}
	type left struct{ keeps, olds []string }
	by := map[string]*left{}
	var all []string
	for _, e := range ents {
		n := e.Name()
		kind, rest, ok := strings.Cut(n, "-")
		if !ok || kind != ".keep" && kind != ".old" {
			continue
		}
		all = append(all, filepath.Join(l.Store, n))
		if len(rest) < 37 || !desktop.ValidID(rest[:36]) || rest[36] != '-' {
			continue
		}
		acct := rest[:36]
		if by[acct] == nil {
			by[acct] = &left{}
		}
		if kind == ".keep" {
			by[acct].keeps = append(by[acct].keeps, filepath.Join(l.Store, n))
		} else {
			by[acct].olds = append(by[acct].olds, filepath.Join(l.Store, n))
		}
	}
	for acct, lf := range by {
		dst := l.acctDir(acct)
		for _, k := range lf.keeps {
			if !exists(dst) && checkFolder(k, acct) == "" {
				_ = rename(k, dst)
			}
		}
		for _, o := range lf.olds {
			if !exists(dst) {
				_ = rename(o, dst)
			}
		}
	}
	for _, p := range all {
		if exists(p) {
			removeTree(p)
		}
	}
}

// readme notes in the store what it is, for someone who finds the folder.
func (l *Logins) readme() {
	p := filepath.Join(l.Store, "README.txt")
	if exists(p) {
		return
	}
	desktop.Guard("write", p)
	_ = fsx.WriteFileAtomic(p, []byte("baton keeps the Claude desktop app's sign-ins here, one folder per account, "+
		"because switching the app's sign-in is turned on in baton.\r\n"+
		"They are deleted when that is turned off or baton is uninstalled. Do not copy them anywhere else.\r\n"), 0o600)
}

// identify is what is known of account acct for its row: its organization
// and email, from the user's own Claude Code when that names the account,
// else its single list in the app, else one of baton's accounts that is it,
// else what was kept before.
func (l *Logins) identify(data, acct string, old *meta) (org, orgName, email string) {
	if l.UserHome != "" {
		if a, o, e := claude.OAuthIdentity(claude.UserClaudeJSONDir(l.UserHome)); a == acct {
			email = e
			if desktop.ValidID(o) {
				org = o
			}
		}
	}
	if org == "" {
		ents, _ := os.ReadDir(filepath.Join(data, "claude-code-sessions", acct))
		var orgs []string
		for _, e := range ents {
			if o := strings.ToLower(e.Name()); e.IsDir() && desktop.ValidID(o) {
				orgs = append(orgs, o)
			}
		}
		if len(orgs) == 1 {
			org = orgs[0]
		}
	}
	for _, a := range byPriority(l.Accounts) {
		if strings.ToLower(a.AccountUUID) != acct {
			continue
		}
		if email == "" {
			email = a.Email
		}
		if o := strings.ToLower(a.OrgID); org == "" && desktop.ValidID(o) {
			org = o
		}
		if orgName == "" && strings.EqualFold(a.OrgID, org) {
			orgName = a.OrgName
		}
	}
	if old != nil {
		if org == "" {
			org = old.Org
		}
		if orgName == "" && old.Org == org {
			orgName = old.OrgName
		}
		if email == "" {
			email = old.Email
		}
	}
	return org, orgName, email
}

// makeStore makes the store, with permissions of its own: the user and
// SYSTEM only, not inherited from the folder it is in. BATON_HOME may be on
// a drive whose folders every user of this PC can read, and a sign-in's
// site storage is not encrypted. A store that cannot get them keeps
// nothing. A store made before is given them once (a protected DACL says
// so); its kept sign-ins inherit them.
func (l *Logins) makeStore() error {
	if err := mkdir(l.Store); err != nil {
		return err
	}
	if err := protect(l.Store); err != nil {
		return fail(CodeUnsavedCurrent, "store_permissions")
	}
	return nil
}

// protect gives dir a DACL of its own, not inherited, that lets only the
// current user and SYSTEM in, and passes it on to everything inside.
func protect(dir string) error {
	if sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION); err == nil {
		if c, _, err := sd.Control(); err == nil && c&windows.SE_DACL_PROTECTED != 0 {
			return nil
		}
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + u.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	desktop.Guard("write", dir)
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// ---- files ------------------------------------------------------------------

func mkdir(p string) error {
	desktop.Guard("create", p)
	return os.MkdirAll(p, 0o700)
}

func removeTree(p string) error {
	desktop.Guard("remove", p)
	return fsx.RemoveTreeSafe(p)
}

func rename(from, to string) error {
	desktop.Guard("move", from)
	desktop.Guard("move", to)
	return moveNew(from, to)
}

// moveNew renames from to to, never over something there. A folder with a
// file held open inside (an indexer, an antivirus) cannot be renamed for a
// moment, so it is tried again for about two seconds.
func moveNew(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(moveWait)
	for {
		err = windows.MoveFileEx(f, t, 0)
		if err == nil {
			return nil
		}
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) ||
			errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) ||
			!time.Now().Before(deadline) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var moveWait = 2 * time.Second

// errReparse: a junction or link inside a login set. baton never follows
// one, and a set that has one is not one it knows.
var errReparse = errors.New("a junction or link is in the way")

// walkSet is the size of the item at p, refusing any junction or link.
func walkSet(p string) (int64, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return 0, err
	}
	if fsx.IsReparsePoint(fi) {
		return 0, errReparse
	}
	if !fi.IsDir() {
		return fi.Size(), nil
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, e := range ents {
		s, err := walkSet(filepath.Join(p, e.Name()))
		if err != nil {
			return 0, err
		}
		n += s
	}
	return n, nil
}

// copyTree copies the file or folder src to dst (which must not exist),
// keeping times, refusing any junction or link, and checking every file's
// size against what was read. rel names src in the list of files returned.
func copyTree(src, dst, rel string) ([]file, int64, error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return nil, 0, err
	}
	if fsx.IsReparsePoint(fi) {
		return nil, 0, errReparse
	}
	if !fi.IsDir() {
		if err := copyFile(src, dst, fi); err != nil {
			return nil, 0, err
		}
		return []file{{Path: rel, Size: fi.Size()}}, fi.Size(), nil
	}
	if err := mkdir(dst); err != nil {
		return nil, 0, err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return nil, 0, err
	}
	var out []file
	var size int64
	for _, e := range ents {
		f, n, err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), filepath.Join(rel, e.Name()))
		if err != nil {
			return nil, 0, err
		}
		out, size = append(out, f...), size+n
	}
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return out, size, nil
}

func copyFile(src, dst string, fi os.FileInfo) error {
	desktop.Guard("write", dst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != fi.Size() {
		err = errors.New("copied a different size than was read: " + src)
	}
	if err != nil {
		return err
	}
	return os.Chtimes(dst, fi.ModTime(), fi.ModTime())
}

// stamp is what an item of the login set looks like now: its size, its
// files and its latest change. The app changes it when it runs on it.
func stamp(p string) string {
	var size, files int64
	var last time.Time
	var walk func(string)
	walk = func(p string) {
		fi, err := os.Lstat(p)
		if err != nil {
			return
		}
		if fi.ModTime().After(last) {
			last = fi.ModTime()
		}
		if !fi.IsDir() || fsx.IsReparsePoint(fi) {
			size, files = size+fi.Size(), files+1
			return
		}
		ents, _ := os.ReadDir(p)
		for _, e := range ents {
			walk(filepath.Join(p, e.Name()))
		}
	}
	walk(p)
	if files == 0 && last.IsZero() {
		return ""
	}
	return strconv.FormatInt(size, 10) + "/" + strconv.FormatInt(files, 10) + "/" + strconv.FormatInt(last.UnixNano(), 10)
}
