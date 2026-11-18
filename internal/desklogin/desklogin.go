// Package desklogin switches the Claude desktop app's sign-in between the
// user's own accounts, when the user turned that on and asks for it.
//
// The app keeps its sign-in in a few items of its data folder (cookies and
// site storage, the login set) and six values of its config.json. While the
// app is fully quit, a switch keeps what the app has now under its account
// in baton's store, then moves the chosen account's items in and splices its
// six values into config.json, the account last. Nothing is ever decrypted,
// read for its meaning or sent anywhere: the files move as they are.
//
// A switch is staged in the app's own folder (one volume, so each move is a
// rename) with a journal, and one that is cut off is undone (Recover), never
// finished on its own; only when the user chooses to is the app left as it
// is instead (Settle). Nothing here runs on its own: only the window's and
// the CLI's handlers call Use, Blank, Recover and Settle (see the rules test).
package desklogin

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/config"
	"baton/internal/desktop"
	"baton/internal/fsx"
	"baton/internal/share"

	"golang.org/x/sys/windows"
)

// Error codes, as *desktop.Error codes. The window and the CLI word them; a
// Detail never holds a value of the app's config.json (key names only).
const (
	CodeOff               = "dl_off"                // turned off, or its notice not accepted (as it reads now)
	CodeAppRunning        = "dl_app_running"        // the desktop app runs, or holds its lockfile
	CodeBusy              = "dl_busy"               // another change of the sign-in, or of baton's desktop lists, is under way
	CodeNotKept           = "dl_not_kept"           // baton keeps no sign-in for that account (or no account goes by that name)
	CodeAmbiguous         = "dl_ambiguous"          // the name fits more than one account (Detail lists them)
	CodeDamaged           = "dl_damaged"            // the kept sign-in is not whole, or its tokens are another account's
	CodeAlready           = "dl_already"            // the app is signed in to that account already (Blank: signed out already)
	CodeUnknownFormat     = "dl_unknown_format"     // the app keeps its sign-in in a way baton does not know (Detail: what)
	CodeUnsupportedLayout = "dl_unsupported_layout" // not a layout of the app's folders baton knows (Detail: Layout*)
	CodeUnsavedCurrent    = "dl_unsaved_current"    // the sign-in the app has now cannot be kept, so nothing changed
	CodeSwitchFailed      = "dl_switch_failed"      // the switch did not finish and was undone
	CodeSwitchUnrestored  = "dl_switch_unrestored"  // the switch did not finish and is not undone yet: Recover, or Settle when that keeps failing
	CodeUsedSince         = "dl_used_since"         // Recover: the app was used on the switched sign-in since, and that cannot be kept now (off, or a synced store): Settle
	CodeSyncedStore       = "dl_synced_store"       // baton's folder is in OneDrive or another synced folder
	CodeInsideClaude      = "dl_inside_claude"      // asked from inside a Claude Code (CLAUDECODE is set)
	CodeElevated          = "dl_elevated"           // baton runs as administrator
	CodeBadLabel          = "dl_bad_label"          // Detail: too_long, characters, looks_like_id or taken
	CodeLaunchUnsupported = "dl_launch_unsupported" // the app cannot be opened from here: the user opens it
)

// Why the app's folders are not a layout baton knows: the Detail of
// CodeUnsupportedLayout (see layout).
const (
	LayoutNoApp   = "no_app"  // baton finds no data folder of the desktop app
	LayoutSeveral = "several" // more than one Claude-Data\claude_* folder holds a config.json
	LayoutLink    = "link"    // the app's data folder is a link or junction
	LayoutOther   = "other"   // the app's data folder is somewhere else, or holds no config.json
)

func fail(code, detail string) error { return &desktop.Error{Code: code, Detail: detail} }

// Code is the code of err when it is one of this package's, else "".
func Code(err error) string {
	var e *desktop.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Logins is the switching of the desktop app's sign-in, for one baton
// data folder.
type Logins struct {
	Store      string // baton's kept sign-ins (app.Paths.DesktopLogins)
	Lock       string // taken for every change of them, and every switch
	DeskLock   string // baton's desktop lists' lock, held only while items move
	ConfigPath string
	ConfigLock string
	UserHome   string // the user's own Claude Code folder, which may name the account
	Accounts   []accounts.Account
	Running    func() bool // whether the desktop app runs (desktop.AppRunning)
	Elevated   func() bool // whether baton runs as administrator
	Now        func() time.Time

	dataFn      func() string
	wait        time.Duration // for each lock
	appLockWait time.Duration // for the app's lockfile, which it lets go of as it quits
	failAt      func(step string) error
	afterGate   func()
}

// New is the switching of baton's data folder p, for the accounts in reg.
func New(p app.Paths, reg accounts.Registry) *Logins {
	return &Logins{Store: p.DesktopLogins(), Lock: p.DesktopLoginsLock(), DeskLock: p.DesktopLock(),
		ConfigPath: p.Config(), ConfigLock: p.ConfigLock(), UserHome: share.UserHome(p.ProfilesDir()),
		Accounts: reg.Accounts, Running: desktop.AppRunning, Elevated: elevated, Now: time.Now,
		dataFn: desktop.DataDir, wait: 10 * time.Second, appLockWait: 1500 * time.Millisecond}
}

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// errCrash is a test's stop at a step: everything is left as it is, as when
// baton is killed there.
var errCrash = errors.New("stopped here (test)")

func (l *Logins) hook(step string) error {
	if l.failAt != nil {
		return l.failAt(step)
	}
	return nil
}

// lock takes the lock every change here holds, waiting at most l.wait.
func (l *Logins) lock() (func(), error) {
	if err := mkdir(filepath.Dir(l.Lock)); err != nil {
		return nil, err
	}
	desktop.Guard("lock", l.Lock)
	unlock, err := fsx.LockWait(l.Lock, l.wait)
	if errors.Is(err, fsx.ErrLocked) {
		return nil, fail(CodeBusy, "")
	}
	return unlock, err
}

// on reports whether switching is on with its notice accepted, read again
// now: under the lock, so a TurnOff meanwhile is seen.
func (l *Logins) on() error {
	c, err := config.Load(l.ConfigPath)
	if err != nil {
		return err
	}
	if !c.DesktopLoginsOn() {
		return fail(CodeOff, "")
	}
	return nil
}

// dataDir is the desktop app's data folder, the one baton shows sessions
// in, when it is a layout baton knows (layout). BATON_DESKTOP_DATA names it
// instead (tests).
func (l *Logins) dataDir() (string, error) {
	d := ""
	if l.dataFn != nil {
		d = l.dataFn()
	}
	if d == "" {
		return "", fail(CodeUnsupportedLayout, LayoutNoApp)
	}
	if os.Getenv("BATON_DESKTOP_DATA") != "" {
		return d, nil
	}
	if why := layout(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), d); why != "" {
		return "", fail(CodeUnsupportedLayout, why)
	}
	return d, nil
}

// stageDir is the app's folder for what ends a switch that was cut off
// (Recover, Settle, and the looks for one): the one dataDir names. When the
// app's folders are no longer a layout baton knows (a second
// Claude-Data\claude_* folder holds a config.json, or the one lost it),
// it is the one folder of the app's that holds a switch cut off: its stage
// shows baton switched there, and what the switch set aside must not stay
// behind with nothing to settle it (uninstall would then stop for good).
// Two such folders are not a layout baton knows either.
func (l *Logins) stageDir() (string, error) {
	d, err := l.dataDir()
	if err == nil || Code(err) != CodeUnsupportedLayout {
		return d, err
	}
	var found []string
	for _, a := range l.appDirs() {
		if len(stages(a, true)) > 0 {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		return "", err
	}
	return found[0], nil
}

// appDirs are the folders the app may keep its data in, whatever their
// layout: the one baton would name, each Claude-Data\claude_* folder and
// the older installer's %APPDATA%\Claude (no link or junction).
func (l *Logins) appDirs() []string {
	out, _ := l.appDirsErr()
	return out
}

// appDirsErr is appDirs, with the error of a Claude-Data that is there but
// cannot be listed now (another program holds it without sharing it). The
// app's folder is found by listing Claude-Data, so then baton finds none of
// its folders there, which says nothing of whether one holds a switch cut
// off: CutOffNow must not take it for "none". A Claude-Data that is gone
// holds none.
func (l *Logins) appDirsErr() ([]string, error) {
	var out []string
	var lerr error
	add := func(d string) {
		if d == "" || slices.ContainsFunc(out, func(o string) bool { return samePath(o, d) }) {
			return
		}
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() && !fsx.IsReparsePoint(fi) {
			out = append(out, d)
		}
	}
	if l.dataFn != nil {
		add(l.dataFn())
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		root := filepath.Join(la, "Claude-Data")
		ents, err := os.ReadDir(root)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			lerr = err
		}
		for _, e := range ents {
			if ok, _ := filepath.Match("claude_*", e.Name()); ok {
				add(filepath.Join(root, e.Name()))
			}
		}
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		add(filepath.Join(ad, "Claude"))
	}
	return out, lerr
}

// layout says why data, the folder baton shows the app's sessions in, is
// not a layout of the app's folders baton knows ("" when it is). It knows
// two:
//
//   - the app as a package (MSIX), which keeps its data in
//     %LOCALAPPDATA%\Claude-Data\claude_<publisher id>: data is the one
//     folder there that holds a config.json;
//   - the app from the older installer (%LOCALAPPDATA%\AnthropicClaude),
//     which keeps it in %APPDATA%\Claude: data is that folder, and no
//     Claude-Data\claude_* folder holds a config.json. The packaged app
//     takes the data from there as it first starts, and leaves a
//     .user-data-left.* note behind; the folder is not the app's then.
//
// Neither is a link or junction, and each holds a config.json. While
// Claude-Data is there but cannot be listed now (another program holds it
// without sharing it), baton cannot tell which of the two it is.
func layout(localAppData, appData, data string) string {
	fi, err := os.Lstat(data)
	switch {
	case err == nil && fsx.IsReparsePoint(fi):
		return LayoutLink
	case err != nil || !fi.IsDir():
		return LayoutOther
	case !isFile(filepath.Join(data, "config.json")):
		return LayoutOther
	}
	var holds []string
	if localAppData != "" {
		root := filepath.Join(localAppData, "Claude-Data")
		ents, err := os.ReadDir(root)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return LayoutOther
		}
		for _, e := range ents {
			f := filepath.Join(root, e.Name())
			if ok, _ := filepath.Match("claude_*", e.Name()); ok && isDir(f) && isFile(filepath.Join(f, "config.json")) {
				holds = append(holds, f)
			}
		}
	}
	switch {
	case len(holds) > 1:
		return LayoutSeveral
	case len(holds) == 1 && samePath(holds[0], data):
		return ""
	case len(holds) == 0 && olderInstall(appData, data):
		return ""
	}
	return LayoutOther
}

// olderInstall reports whether data is the data folder of the app from the
// older installer: %APPDATA%\Claude, which the packaged app has not taken
// its data from (no .user-data-left.* note).
func olderInstall(appData, data string) bool {
	if appData == "" || !samePath(data, filepath.Join(appData, "Claude")) {
		return false
	}
	ents, err := os.ReadDir(data)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if strings.HasPrefix(strings.ToLower(e.Name()), ".user-data-left.") {
			return false
		}
	}
	return true
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// problem is why switching cannot work here at all, as a code and its
// detail ("" when it can): the window greys the setting out with it.
func (l *Logins) problem() (code, detail string) {
	if _, err := l.dataDir(); err != nil {
		var e *desktop.Error
		if errors.As(err, &e) {
			return e.Code, e.Detail
		}
		return CodeUnsupportedLayout, ""
	}
	if synced(l.Store) {
		return CodeSyncedStore, ""
	}
	if l.Elevated != nil && l.Elevated() {
		return CodeElevated, ""
	}
	return "", ""
}

// gate is what every switch checks before it waits for anything: asked by
// the user, not from inside a Claude Code (an agent, baton's or the user's
// own, never switches); turned on; a layout baton knows; a store that stays
// on this PC; and not as administrator (what it writes would be owned by
// Administrators then).
func (l *Logins) gate() (string, error) {
	if os.Getenv("CLAUDECODE") != "" {
		return "", fail(CodeInsideClaude, "")
	}
	if err := l.on(); err != nil {
		return "", err
	}
	data, err := l.dataDir()
	if err != nil {
		return "", err
	}
	if synced(l.Store) {
		return "", fail(CodeSyncedStore, "")
	}
	if l.Elevated != nil && l.Elevated() {
		return "", fail(CodeElevated, "")
	}
	return data, nil
}

// synced reports whether path is in a folder a cloud service copies off
// this PC: OneDrive (as Windows names it) or a folder named after one.
// The site storage of a sign-in is not encrypted.
func synced(path string) bool {
	p := longPath(path)
	for _, v := range []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		if d := os.Getenv(v); d != "" && under(p, longPath(d)) {
			return true
		}
	}
	for _, seg := range strings.Split(filepath.Clean(p), string(filepath.Separator)) {
		s := strings.ToLower(seg)
		switch {
		case strings.HasPrefix(s, "onedrive"), s == "dropbox", strings.HasPrefix(s, "dropbox ("),
			s == "google drive", s == "googledrive", s == "my drive", s == "icloud drive", s == "iclouddrive",
			s == "box", s == "box sync", s == "pcloud drive", s == "mega":
			return true
		}
	}
	return false
}

// ---- the view -----------------------------------------------------------------

// View is what the window's card and `baton desktop login` show.
type View struct {
	On      bool   `json:"on"`
	Store   string `json:"store"`             // where the sign-ins are kept
	Problem string `json:"problem,omitempty"` // why switching cannot work here (a code), "" when it can
	// ProblemDetail says more of Problem: for CodeUnsupportedLayout, which
	// layout it is (Layout*).
	ProblemDetail string `json:"problem_detail,omitempty"`
	Running       bool   `json:"running"`
	Active        string `json:"active"` // the account the app is signed in to ("" signed out)
	// CutOff: a switch was cut off and is not undone yet; Recover undoes it
	// (Settle leaves the app as it is instead). Never while a switch is
	// under way.
	CutOff bool `json:"cut_off"`
	// Leftover counts what switches left in the app's folder that is only a
	// copy (one stopped before it moved anything, or whose delete failed):
	// a copy of the app's config.json and of a sign-in's files. Tidy deletes
	// them; so do the next switch, TurnOff and DropAll. Never while a switch
	// is under way.
	Leftover int     `json:"leftover,omitempty"`
	Logins   []Login `json:"logins"`
	Kept     int     `json:"kept"` // how many sign-ins baton keeps
	// Unnamed are what the app left while no token was recorded in it, set
	// aside by switches: no account was recorded with them, so they are
	// never switched to; Drop or DropAll deletes them.
	Unnamed []Unnamed `json:"unnamed"`
}

// Unnamed is what the app left of a login set while no token was recorded
// in it (signed out, or quit before it recorded one), set aside in the
// store by a switch (unnamed-<when>).
type Unnamed struct {
	Name   string    `json:"name"`
	KeptAt time.Time `json:"kept_at"`
	Size   int64     `json:"size,omitempty"`
}

// Login is one account the desktop app was signed in to: one kept, or the
// one it is on now, kept or not yet.
type Login struct {
	Account   string    `json:"account"`
	Label     string    `json:"label"`           // what to call it: Named, else a baton account's id, else its email, else the start of its id
	Named     string    `json:"named,omitempty"` // the label the user gave it
	Email     string    `json:"email,omitempty"`
	Org       string    `json:"org,omitempty"`
	OrgName   string    `json:"org_name,omitempty"`
	Baton     []string  `json:"baton,omitempty"` // baton's accounts that are this Claude account
	Active    bool      `json:"active"`
	Kept      bool      `json:"kept"`
	Damaged   bool      `json:"damaged,omitempty"`
	KeptAt    time.Time `json:"kept_at,omitzero"`
	FirstKept time.Time `json:"first_kept,omitzero"`
	Used      time.Time `json:"used,omitzero"`
	Size      int64     `json:"size,omitempty"`
}

// View reads what is kept and where the app is. It changes nothing and
// takes no lock, so it works while off and while a switch runs.
func (l *Logins) View() (View, error) {
	c, err := config.Load(l.ConfigPath)
	if err != nil {
		return View{}, err
	}
	v := View{On: c.DesktopLoginsOn(), Store: l.Store, Logins: []Login{}}
	v.Problem, v.ProblemDetail = l.problem()
	v.Running = l.Running != nil && l.Running()
	if data, _ := l.dataDir(); data != "" {
		v.Active = desktop.Active(data)
	}
	// Also after the layout changed: a switch cut off before is shown, and
	// Recover and Settle work there (stageDir).
	if data, _ := l.stageDir(); data != "" {
		v.CutOff, v.Leftover = l.stuck(data)
	}
	v.Logins = l.logins(v.Active)
	v.Unnamed = l.stashes()
	for _, g := range v.Logins {
		if g.Kept {
			v.Kept++
		}
	}
	return v, nil
}

// logins are the kept sign-ins and the account the app is on: that one
// first, then those of baton's accounts in baton's order, then the rest by
// label.
func (l *Logins) logins(active string) []Login {
	byAcct := map[string]*Login{}
	ents, _ := os.ReadDir(l.Store)
	for _, e := range ents {
		acct := e.Name()
		if !e.IsDir() || !desktop.ValidID(acct) {
			continue
		}
		g := &Login{Account: acct, Kept: true}
		if m, err := readMeta(l.acctDir(acct)); err == nil {
			g.Named, g.Email, g.Org, g.OrgName = m.Label, m.Email, m.Org, m.OrgName
			g.KeptAt, g.FirstKept, g.Used, g.Size = m.Kept, m.FirstKept, m.Used, m.Size
		}
		g.Damaged = checkFolder(l.acctDir(acct), acct) != ""
		byAcct[acct] = g
	}
	if active != "" && byAcct[active] == nil {
		byAcct[active] = &Login{Account: active}
	}
	rank := map[string]int{}
	for i, a := range byPriority(l.Accounts) {
		acct := strings.ToLower(a.AccountUUID)
		g := byAcct[acct]
		if g == nil {
			continue
		}
		g.Baton = append(g.Baton, a.ID)
		if _, ok := rank[acct]; !ok {
			rank[acct] = i
		}
		if g.Email == "" {
			g.Email = a.Email
		}
		if g.OrgName == "" && (g.Org == "" || strings.EqualFold(g.Org, a.OrgID)) {
			g.OrgName = a.OrgName
		}
	}
	out := []Login{}
	for _, g := range byAcct {
		g.Active = g.Account == active
		g.Label = label(g)
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Active != b.Active {
			return a.Active
		}
		ra, oka := rank[a.Account]
		rb, okb := rank[b.Account]
		if oka != okb {
			return oka
		}
		if oka && ra != rb {
			return ra < rb
		}
		if la, lb := strings.ToLower(a.Label), strings.ToLower(b.Label); la != lb {
			return la < lb
		}
		return a.Account < b.Account
	})
	return out
}

func label(g *Login) string {
	switch {
	case g.Named != "":
		return g.Named
	case len(g.Baton) > 0:
		return g.Baton[0]
	case g.Email != "":
		return g.Email
	}
	return g.Account[:8]
}

// Known is what is kept of account acct for baton's desktop lists
// (desktop.Desk.Known): its organization and email, and whether a sign-in
// is kept for it. It reads only the kept meta.json.
func (l *Logins) Known(acct string) (org, email string, kept bool) {
	if !desktop.ValidID(acct) {
		return "", "", false
	}
	m, err := readMeta(l.acctDir(acct))
	if err != nil {
		return "", "", false
	}
	return m.Org, m.Email, true
}

// Resolve is the account name means: a label, one of baton's account ids
// that is that Claude account, an email, or an account id or the first 8 or
// more of its hex digits. A name that fits more than one account is
// refused, with the labels of those it fits. The name of something a
// signed-out app left (unnamed-<when>) is itself, for Drop.
func (l *Logins) Resolve(name string) (string, error) {
	n := strings.TrimSpace(name)
	low := strings.ToLower(n)
	if n == "" {
		return "", fail(CodeNotKept, "")
	}
	if isStash(low) {
		if !exists(l.acctDir(low)) {
			return "", fail(CodeNotKept, "")
		}
		return low, nil
	}
	active := ""
	if data, err := l.dataDir(); err == nil {
		active = desktop.Active(data)
	}
	var hits []string
	labels := map[string]string{}
	add := func(acct, lbl string) {
		if !slices.Contains(hits, acct) {
			hits = append(hits, acct)
			labels[acct] = lbl
		}
	}
	prefix := hexPrefix(low)
	for _, g := range l.logins(active) {
		if strings.EqualFold(g.Named, n) || strings.EqualFold(g.Label, n) || strings.EqualFold(g.Email, n) ||
			slices.ContainsFunc(g.Baton, func(id string) bool { return strings.EqualFold(id, n) }) ||
			g.Account == low || prefix && strings.HasPrefix(g.Account, low) {
			add(g.Account, g.Label)
		}
	}
	for _, a := range byPriority(l.Accounts) {
		acct := strings.ToLower(a.AccountUUID)
		if desktop.ValidID(acct) && (strings.EqualFold(a.ID, n) || a.Email != "" && strings.EqualFold(a.Email, n)) {
			add(acct, a.ID)
		}
	}
	if desktop.ValidID(low) {
		add(low, low[:8])
	}
	switch len(hits) {
	case 0:
		return "", fail(CodeNotKept, "")
	case 1:
		return hits[0], nil
	}
	var names []string
	for _, h := range hits {
		names = append(names, labels[h])
	}
	return "", fail(CodeAmbiguous, strings.Join(names, ", "))
}

// hexPrefix: s is the start of an account id, 8 hex digits or more.
func hexPrefix(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			digits++
		case r == '-':
		default:
			return false
		}
	}
	return digits >= 8
}

// ---- changes of the store -----------------------------------------------------

// Rename gives the sign-in kept for acct a label ("" takes it off). It must
// read plainly, and never be another account's label, baton id or email,
// or look like an account id.
func (l *Logins) Rename(acct, lbl string) error {
	acct = strings.ToLower(acct)
	lbl = strings.TrimSpace(lbl)
	if !desktop.ValidID(acct) {
		return fail(CodeNotKept, "")
	}
	switch {
	case utf8.RuneCountInString(lbl) > 40:
		return fail(CodeBadLabel, "too_long")
	case strings.ContainsFunc(lbl, func(r rune) bool {
		return unicode.IsControl(r) || r == ' ' || r == ' ' || r == utf8.RuneError || r == ','
	}):
		return fail(CodeBadLabel, "characters")
	case hexPrefix(strings.ToLower(lbl)):
		return fail(CodeBadLabel, "looks_like_id")
	}
	unlock, err := l.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err := l.on(); err != nil {
		return err
	}
	dir := l.acctDir(acct)
	m, err := readMeta(dir)
	if err != nil {
		if !exists(dir) {
			return fail(CodeNotKept, "")
		}
		return fail(CodeDamaged, "")
	}
	if lbl != "" {
		for _, g := range l.logins("") {
			if g.Account != acct && (strings.EqualFold(g.Label, lbl) || strings.EqualFold(g.Email, lbl)) {
				return fail(CodeBadLabel, "taken")
			}
		}
		for _, a := range l.Accounts {
			if !strings.EqualFold(a.AccountUUID, acct) && (strings.EqualFold(a.ID, lbl) || a.Email != "" && strings.EqualFold(a.Email, lbl)) {
				return fail(CodeBadLabel, "taken")
			}
		}
	}
	m.Label = lbl
	return writeMeta(dir, m)
}

// Drop deletes the sign-in kept for acct, or what a signed-out app left
// (acct is then its unnamed-<when> name), for good. It works while the
// feature is off and while the app runs: it touches only baton's store.
// The sign-in is not ended on Claude's servers.
func (l *Logins) Drop(acct string) error {
	acct = strings.ToLower(acct)
	if !desktop.ValidID(acct) && !isStash(acct) {
		return fail(CodeNotKept, "")
	}
	unlock, err := l.lock()
	if err != nil {
		return err
	}
	defer unlock()
	l.sweep()
	dir := l.acctDir(acct)
	if !exists(dir) {
		return fail(CodeNotKept, "")
	}
	return removeTree(dir)
}

// DropAll deletes every sign-in kept, and the store with them: how many
// there were. It works while off and while the app runs. Of the switches'
// stages in the app's folder it deletes those that are only copies (Tidy);
// one that was cut off stays, for Recover or Settle.
func (l *Logins) DropAll() (int, error) {
	unlock, err := l.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	return l.dropAll()
}

// dropAll is DropAll under the lock (taken already: a second take by this
// process would wait on itself).
func (l *Logins) dropAll() (int, error) {
	_, err := l.tidy()
	n := 0
	ents, rerr := os.ReadDir(l.Store)
	if errors.Is(rerr, os.ErrNotExist) {
		return 0, err
	}
	for _, e := range ents {
		if e.IsDir() && desktop.ValidID(e.Name()) {
			n++
		}
	}
	if rerr := removeTree(l.Store); err == nil {
		err = rerr
	}
	return n, err
}

// Tidy deletes what switches left in the app's folder that is only a copy
// (View.Leftover): how many. A stage without a journal never moved
// anything, and one marked done is what a finished switch could not
// delete; the app never reads either, so this works while it runs, and
// while off. A stage that was cut off stays, for Recover or Settle.
func (l *Logins) Tidy() (int, error) {
	unlock, err := l.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	return l.tidy()
}

// tidy is Tidy under the lock. The lock is what makes it safe: a switch
// holds it from start to end, so a stage without a journal is never one
// still being filled.
func (l *Logins) tidy() (int, error) {
	data, err := l.dataDir()
	if err != nil {
		return 0, nil // no app folder baton knows: no stage of baton's either
	}
	n := 0
	var first error
	for _, s := range stages(data, false) {
		if pending(s) {
			continue
		}
		if err := removeStage(s); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		n++
	}
	return n, first
}

// TurnOn turns switching on for baton's data folder p. acknowledged: the
// user has just accepted its notice; without it, and with no notice as it
// reads now accepted before, it returns config.ErrNeedsDesktopAcknowledgement.
// A store that would sync off this PC, or a layout baton does not know, is
// refused, and so is one that cannot get permissions of its own
// (dl_unsaved_current, store_permissions): it could never keep anything.
func TurnOn(p app.Paths, acknowledged bool) error {
	l := New(p, accounts.Registry{})
	if _, err := l.dataDir(); err != nil {
		return err
	}
	if synced(l.Store) {
		return fail(CodeSyncedStore, "")
	}
	cur, err := config.Load(p.Config())
	if err != nil {
		return err
	}
	set, err := config.DesktopLoginsOnSetter(cur, acknowledged)
	if err != nil {
		return err
	}
	// The store is made now, as every keep makes it: where it cannot be
	// given its permissions (a drive or share that keeps none), every keep
	// would be refused, so it says so here, and the folder it made goes.
	had := exists(l.Store)
	if err := l.makeStore(); err != nil {
		if !had {
			_ = removeFile(l.Store) // only while empty
		}
		return err
	}
	_, err = config.Update(p.Config(), p.ConfigLock(), func(c *config.Config) error {
		set(c)
		return nil
	})
	return err
}

// TurnOff turns switching off for baton's data folder p and deletes every
// sign-in kept, and what switches left in the app's folder that is only a
// copy: how many sign-ins. Turning it on again shows the notice again. The
// desktop app stays signed in to the account it is on; a switch that was
// cut off stays for Recover or Settle.
func TurnOff(p app.Paths) (int, error) { return New(p, accounts.Registry{}).turnOff() }

func (l *Logins) turnOff() (int, error) {
	unlock, err := l.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	if _, err := config.Update(l.ConfigPath, l.ConfigLock, func(c *config.Config) error {
		c.DesktopLogins, c.DesktopLoginsAcknowledged = false, 0
		return nil
	}); err != nil {
		return 0, err
	}
	return l.dropAll()
}

// ---- small things ---------------------------------------------------------------

func byPriority(in []accounts.Account) []accounts.Account {
	out := append([]accounts.Account{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// longPath spells p out in full: 8.3 short names in the part that exists
// are expanded, so two spellings of one folder compare.
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	rest := ""
	for d := abs; ; {
		if u, err := windows.UTF16PtrFromString(d); err == nil {
			buf := make([]uint16, 1024)
			if n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf))); err == nil && n > 0 && int(n) <= len(buf) {
				return filepath.Join(windows.UTF16ToString(buf[:n]), rest)
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs
		}
		rest = filepath.Join(filepath.Base(d), rest)
		d = parent
	}
}

func under(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if strings.EqualFold(path, dir) {
		return true
	}
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir) &&
		(path[len(dir)] == filepath.Separator || strings.HasSuffix(dir, string(filepath.Separator)))
}

func samePath(a, b string) bool { return strings.EqualFold(longPath(a), longPath(b)) }
