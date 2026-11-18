package desklogin

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"baton/internal/desktop"
	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// Switched says what a switch did.
type Switched struct {
	From      string `json:"from"`      // the account the app was on ("" signed out)
	To        string `json:"to"`        // the account it is on now ("" signed out: Blank)
	Kept      bool   `json:"kept"`      // the sign-in it had was kept first
	Recovered bool   `json:"recovered"` // a switch cut off before was undone first
	// Stashed names what the signed-out app had left, set aside in the store
	// first (see stash); "" when there was nothing.
	Stashed string `json:"stashed,omitempty"`
	// Quit: the app ran, and baton quit it for this switch (UseAfterQuit,
	// BlankAfterQuit). The caller opens it again, also when the switch then
	// failed or was refused: it is on the sign-in it had then.
	Quit bool `json:"quit,omitempty"`
}

// Use switches the desktop app to the sign-in kept for account acct. The
// app must be fully quit; what it had is kept first. The caller brings
// baton's desktop lists in line (Reconcile) and then, if the user wants,
// opens the app (Launch).
func (l *Logins) Use(acct string) (Switched, error) { return l.use(acct, false) }

// UseAfterQuit is Use for an app that may run: when it does, and nothing
// in it is working (Working), it is asked to quit first, as Windows asks
// a program to (QuitApp). Nothing is quit for a switch that would be
// refused. The caller opens the app again when res.Quit says it was quit.
func (l *Logins) UseAfterQuit(acct string) (Switched, error) { return l.use(acct, true) }

func (l *Logins) use(acct string, quitFirst bool) (Switched, error) {
	acct = strings.ToLower(strings.TrimSpace(acct))
	if !desktop.ValidID(acct) {
		return Switched{}, fail(CodeNotKept, "")
	}
	return l.switchTo(acct, quitFirst)
}

// Blank keeps the sign-in the desktop app has, then empties it on this PC
// only, so the user can sign in to another account there. Nothing is sent
// to Claude: the kept sign-in stays good.
func (l *Logins) Blank() (Switched, error) { return l.switchTo("", false) }

// BlankAfterQuit is Blank for an app that may run, as UseAfterQuit is Use.
func (l *Logins) BlankAfterQuit() (Switched, error) { return l.switchTo("", true) }

// The stage of a switch, in the app's data folder (one volume with what it
// moves, so every move is a rename):
//
//	<data>\.baton-switch-<rand>\  journal.json, old\ (what was moved out, and config.json as it was), new\ (what moves in)
//
// A restore adds its notes (restoringName, foreignPrefix) and out\, where
// what it takes out of the app goes (see putBack).
//
// The journal is written before the first move and deleted first when the
// switch is done, undone or settled (or, when it cannot be, a file named
// done is added), so a stage without one, or with done, is only a copy: it
// is deleted, never undone.
const stagePrefix = ".baton-switch-"

// Phases of a switch, in its journal.
const (
	phaseMoving = "moving" // items move; config.json is not touched
	phaseConfig = "config" // config.json is being written, or was
)

type journal struct {
	Schema    int    `json:"schema"`
	From      string `json:"from"`
	To        string `json:"to"`
	Phase     string `json:"phase"`
	CfgBefore string `json:"cfg_before"`          // sha256 of config.json as the switch read it
	CfgAfter  string `json:"cfg_after,omitempty"` // and as it writes it
	// Flipped: config.json as the switch wrote it is in place (noted right
	// after, as well as it can be). Once the app has rewritten config.json,
	// the hashes no longer say whether the write landed, and an app back on
	// From could be either: restore and Settle go by this then. A restore
	// that wrote config.json back, or may have (stage\restoring), cancels
	// it: From's values are in config.json again from then on.
	Flipped bool    `json:"flipped,omitempty"`
	Items   []jitem `json:"items"`
}

type jitem struct {
	Name   string `json:"name"`
	HadOld bool   `json:"had_old"`
	HasNew bool   `json:"has_new"`
	Stamp  string `json:"stamp,omitempty"` // how it looked once moved in
}

func (l *Logins) switchTo(target string, quitFirst bool) (res Switched, err error) {
	res.To = target
	data, err := l.gate()
	if err != nil {
		return res, err
	}
	if l.afterGate != nil {
		l.afterGate()
	}
	unlock, err := l.lock()
	if err != nil {
		return res, err
	}
	defer unlock()
	// Read again under the lock: a TurnOff that ran while this waited
	// deleted the store, which must stay deleted.
	if err := l.on(); err != nil {
		return res, err
	}
	if quitFirst && l.Running != nil && l.Running() {
		if err := l.mayQuit(data, target); err != nil {
			return res, err
		}
		if l.QuitApp == nil {
			return res, fail(CodeAppRunning, "")
		}
		if err := l.QuitApp(); err != nil {
			detail := ""
			if errors.Is(err, desktop.ErrQuitRefused) {
				detail = "refused"
			}
			return res, fail(CodeQuitFailed, detail)
		}
		res.Quit = true
	}
	release, err := l.quit(data)
	if err != nil {
		return res, err
	}
	defer release()
	l.sweep()
	if res.Recovered, err = l.recoverAll(data); err != nil {
		return res, err
	}
	before, cfg, err := readApp(data)
	if err != nil {
		return res, err
	}
	res.From = cfg.account
	if err := l.refuses(data, cfg, target); err != nil {
		return res, err
	}
	if !cfg.token && (cfg.account == "" && leftovers(data, cfg) || cfg.account != "" && itemsLeft(data)) {
		// No token, yet not empty: opened and quit without a sign-in (after
		// a Blank, say), the app makes cookies and site storage of its own;
		// after its own Log out it still names the account it was on. No
		// token was recorded, so it must not stop the user from going back
		// to a kept one; it is set aside all the same (never over a kept
		// sign-in), since baton never throws away what the app left. An app
		// that names an account and has nothing else (only its six values)
		// has nothing to set aside.
		if res.Stashed, err = l.stash(data, cfg); err != nil {
			if errors.Is(err, errCrash) || Code(err) != "" {
				return res, err
			}
			return res, fail(CodeUnsavedCurrent, "")
		}
	}
	if cfg.account != "" && cfg.token {
		// lastKnownAccountUuid is the only sign of whose sign-in this is. If
		// its tokens are another kept account's as they are, it is stale, and
		// keeping would put that account's tokens under this one. When that
		// cannot be told now, it is not kept now: nothing changes, and the
		// refusal names the kept sign-in it could not read.
		if another, err := l.tokensOfAnother(cfg); err != nil {
			return res, err
		} else if another {
			return res, fail(CodeDamaged, "tokens_of_another")
		}
		if err := l.keep(data, cfg.account, cfg); err != nil {
			if errors.Is(err, errCrash) || Code(err) != "" {
				return res, err
			}
			return res, fail(CodeUnsavedCurrent, "")
		}
		res.Kept = true
	}
	if err := l.swap(data, before, cfg, target); err != nil {
		if res.Stashed != "" && !errors.Is(err, errCrash) && Code(err) != CodeSwitchUnrestored {
			// Undone: the app has what was set aside again, as it was.
			removeTree(filepath.Join(l.Store, res.Stashed))
			res.Stashed = ""
		}
		return res, err
	}
	if target != "" {
		if m, err := readMeta(l.acctDir(target)); err == nil {
			m.Used = l.Now()
			_ = writeMeta(l.acctDir(target), m)
		}
	}
	return res, nil
}

// refuses is why a switch to target would be refused from what the app
// has now (cfg), before anything changes; nil when it would go ahead.
func (l *Logins) refuses(data string, cfg *appConfig, target string) error {
	cookies := exists(filepath.Join(data, "Network", "Cookies"))
	switch {
	case target != "" && cfg.account == target && cfg.token:
		return fail(CodeAlready, "")
	case target == "" && cfg.account == "" && !cfg.token && !cookies:
		return fail(CodeAlready, "")
	case cfg.account == "" && cfg.token:
		// Signed in, but the app has not said to which account yet (it was
		// quit right after a sign-in): there is nowhere to keep it, and it is
		// never thrown away.
		return fail(CodeUnsavedCurrent, "no_account")
	}
	if target != "" {
		if why := checkFolder(l.acctDir(target), target); why != "" {
			return fail(why, "")
		}
	}
	return nil
}

// mayQuit says whether the running app may be quit for a switch to
// target: the switch would go ahead as far as what the app has now tells
// (a switch refused would quit it for nothing; the switch looks again
// once it is quit), and nothing in the app is working (Working). Only
// config.json is read here: the running app writes its site storage all
// the time (files come and go), and readApp looks it over once it is quit.
// A config.json caught mid-write is read again.
func (l *Logins) mayQuit(data, target string) error {
	if len(stages(data, true)) > 0 {
		// A switch was cut off: it is put back (Recover) with the app quit
		// by the user, and the app must not open before that. Quitting it
		// here would leave it quit, with nothing to open it again.
		return fail(CodeSwitchUnrestored, "")
	}
	var cfg *appConfig
	why := "config.json"
	for try := 0; try < 5 && cfg == nil; try++ {
		if try > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		if raw, err := fsx.ReadFileShared(filepath.Join(data, "config.json")); err == nil {
			cfg, why = readAppConfig(raw)
		}
	}
	if cfg == nil {
		return fail(CodeUnknownFormat, why)
	}
	if err := l.refuses(data, cfg, target); err != nil {
		return err
	}
	if cfg.account != "" && cfg.token {
		// As switchTo will look: a sign-in whose tokens are another
		// account's is refused, so the app is not quit for it.
		if another, err := l.tokensOfAnother(cfg); err != nil {
			return err
		} else if another {
			return fail(CodeDamaged, "tokens_of_another")
		}
	}
	if w := l.Working(); len(w) > 0 {
		return &WorkingError{Sessions: w}
	}
	// Reading transcripts takes a moment: a session that started a reply
	// meanwhile is seen now, right before the quit.
	if w := l.working(false); len(w) > 0 {
		return &WorkingError{Sessions: w}
	}
	return nil
}

// quit makes sure the desktop app is fully quit, by two signals: no
// process of it runs, and its own single-instance lock (<data>\lockfile)
// can be taken. That lock is held until the end, so an app started
// meanwhile cannot take it. No setting skips it.
func (l *Logins) quit(data string) (func(), error) {
	if l.Running != nil && l.Running() {
		return nil, fail(CodeAppRunning, "")
	}
	release, err := holdAppLock(data, l.appLockWait)
	if err != nil {
		return nil, fail(CodeAppRunning, "lockfile")
	}
	if l.Running != nil && l.Running() {
		release()
		return nil, fail(CodeAppRunning, "")
	}
	return release, nil
}

// holdAppLock opens the app's lockfile as Chromium's single-instance lock
// does: for writing, shared only for reading, gone once closed. While the
// app holds it, that fails; as it quits it may refuse for a moment, so any
// failure is tried again for wait, then means the app runs.
func holdAppLock(data string, wait time.Duration) (func(), error) {
	path := filepath.Join(data, "lockfile")
	desktop.Guard("lock", path)
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		h, err := windows.CreateFile(p, windows.GENERIC_WRITE|windows.DELETE, windows.FILE_SHARE_READ, nil,
			windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
		if err == nil {
			return func() { windows.CloseHandle(h) }, nil
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// readApp reads the app's config.json and looks over its login set: any of
// these is a shape baton does not know, and it changes nothing then.
func readApp(data string) ([]byte, *appConfig, error) {
	before, err := fsx.ReadFileShared(filepath.Join(data, "config.json"))
	if err != nil {
		return nil, nil, fail(CodeUnknownFormat, "config.json")
	}
	cfg, why := readAppConfig(before)
	if cfg == nil {
		return nil, nil, fail(CodeUnknownFormat, why)
	}
	for _, n := range []string{"Cookies-wal", "Cookies-shm"} {
		if exists(filepath.Join(data, "Network", n)) {
			return nil, nil, fail(CodeUnknownFormat, n)
		}
	}
	if bad, ok := knownPartitions(data); !ok {
		// A web view in a partition of its own keeps its cookies there.
		return nil, nil, fail(CodeUnknownFormat, strings.TrimSpace("Partitions "+bad))
	}
	var size int64
	for _, it := range items {
		p := filepath.Join(data, it.name)
		if !exists(p) {
			continue
		}
		n, err := walkSet(p)
		if err != nil {
			if errors.Is(err, errReparse) {
				return nil, nil, fail(CodeUnknownFormat, "link "+it.name)
			}
			return nil, nil, fail(CodeUnknownFormat, it.name)
		}
		if size += n; size > maxSet {
			return nil, nil, fail(CodeUnknownFormat, "too_big")
		}
	}
	return before, cfg, nil
}

// knownPartitions reports whether every web view partition the app keeps on
// disk (Partitions\<name>, where Electron keeps "persist:<name>" in lower
// case) is one of those the app is known to make, none of them its sign-in:
// the app's own pages, its sign-in among them, use its default session
// (Network\Cookies and the rest of items). Those are its browser and its
// previews of sites (launch-preview-…), its file previews
// (cowork-file-preview) and its artifacts' pages (cowork-artifact-…, one
// for each account and organization). They stay where they are at a switch.
// Any other name may be where the sign-in went, and is named in bad.
func knownPartitions(data string) (bad string, ok bool) {
	p := filepath.Join(data, "Partitions")
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", true
	}
	if err != nil || !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "", false
	}
	des, err := os.ReadDir(p)
	if err != nil {
		return "", false
	}
	for _, de := range des {
		n := strings.ToLower(de.Name())
		known := strings.HasPrefix(n, "launch-preview-") || n == "cowork-file-preview" ||
			n == "cowork-artifact" || strings.HasPrefix(n, "cowork-artifact-")
		if !known || !de.IsDir() || de.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return de.Name(), false
		}
	}
	return "", true
}

// tokensOfAnother reports whether a token in cfg is, byte for byte, the one
// kept for another account. A keys.json that is there but cannot be read now
// (cannotRead) is an error: it may be that account's, and keeping the app's
// tokens under the account it names would then put them under the wrong one.
// The error names that kept sign-in (keptUnreadable).
func (l *Logins) tokensOfAnother(cfg *appConfig) (bool, error) {
	mine := map[string][]byte{}
	for _, k := range cfg.obj.login() {
		if slices.Contains(tokenKeys, k.key) && string(k.raw) != `""` {
			mine[k.key] = k.raw
		}
	}
	ents, _ := os.ReadDir(l.Store)
	for _, e := range ents {
		if !e.IsDir() || !desktop.ValidID(e.Name()) || e.Name() == cfg.account {
			continue
		}
		data, err := fsx.ReadFileShared(filepath.Join(l.acctDir(e.Name()), "keys.json"))
		if cannotRead(err) {
			return false, l.keptUnreadable(e.Name())
		}
		if err != nil {
			continue
		}
		keys, _ := readKeys(data)
		for _, k := range keys {
			if m, ok := mine[k.key]; ok && bytes.Equal(m, k.raw) {
				return true, nil
			}
		}
	}
	return false, nil
}

// keptUnreadable is the refusal while the keys.json kept for acct cannot be
// read (tokensOfAnother): dl_unsaved_current, kept_unreadable:<its label>.
// Trying again helps only while another program holds it; when it never
// can be read (its permissions, or a folder in its place), forgetting that
// kept sign-in does, so the user is told which one.
func (l *Logins) keptUnreadable(acct string) error {
	name := acct[:8]
	for _, g := range l.logins("") {
		if g.Account == acct {
			name = g.Label
		}
	}
	return fail(CodeUnsavedCurrent, "kept_unreadable:"+name)
}

// swap moves target's kept sign-in into the app (none: Blank) and what the
// app had into the stage, then writes config.json, the account last. Any
// failure is undone; one the undo cannot finish leaves the stage for
// Recover.
func (l *Logins) swap(data string, before []byte, cfg *appConfig, target string) error {
	stage := filepath.Join(data, stagePrefix+rnd())
	j := &journal{Schema: 1, From: cfg.account, To: target, Phase: phaseMoving, CfgBefore: sum(before)}
	var add []kv
	err := func() error {
		if err := mkdir(filepath.Join(stage, "new")); err != nil {
			return err
		}
		if err := mkdir(filepath.Join(stage, "old")); err != nil {
			return err
		}
		p := filepath.Join(stage, "old", "config.json")
		desktop.Guard("write", p)
		if err := fsx.WriteFileAtomic(p, before, 0o600); err != nil {
			return err
		}
		if target != "" {
			src := l.acctDir(target)
			for _, it := range items {
				if exists(filepath.Join(src, it.name)) {
					if _, _, err := copyTree(filepath.Join(src, it.name), filepath.Join(stage, "new", it.name), it.name); err != nil {
						return err
					}
				}
			}
			kd, err := fsx.ReadFileShared(filepath.Join(src, "keys.json"))
			if err != nil {
				return err
			}
			var ok bool
			if add, ok = readKeys(kd); !ok {
				return errors.New("keys.json is not whole")
			}
		}
		if err := l.hook("stage"); err != nil {
			return err
		}
		for _, it := range items {
			j.Items = append(j.Items, jitem{Name: it.name, HadOld: exists(filepath.Join(data, it.name)),
				HasNew: exists(filepath.Join(stage, "new", it.name))})
		}
		return writeJournal(stage, j)
	}()
	if errors.Is(err, errCrash) {
		return err
	}
	if err != nil {
		removeStage(stage)
		return fail(CodeSwitchFailed, "stage")
	}
	// From here a stop leaves the journal, and the switch is undone.
	desktop.Guard("lock", l.DeskLock)
	unlockDesk, err := fsx.LockWait(l.DeskLock, l.wait)
	if err != nil {
		// Nothing has moved yet: the stage is only a copy. Ended, not only
		// deleted: a journal another program holds open would otherwise stay
		// pending, a switch cut off that moved nothing, and undoing it would
		// delete items the app made since.
		endStage(stage)
		if errors.Is(err, fsx.ErrLocked) {
			return fail(CodeBusy, "desktop")
		}
		return fail(CodeSwitchFailed, "desktop")
	}
	defer unlockDesk()
	err = l.move(data, stage, j, before, cfg, add)
	if errors.Is(err, errCrash) {
		return err
	}
	if err != nil {
		if rerr := l.restore(data, stage); rerr != nil {
			return fail(CodeSwitchUnrestored, "")
		}
		return fail(CodeSwitchFailed, err.Error())
	}
	if err := l.commit(stage); err != nil {
		if errors.Is(err, errCrash) {
			return err
		}
		// The journal says the switch is under way, so the next Recover
		// undoes it: the app must not be opened before.
		return fail(CodeSwitchUnrestored, "commit")
	}
	return nil
}

// commit ends a switch that is done (endStage). Only when that fails is the
// switch not done: it is reported, never as a success.
func (l *Logins) commit(stage string) error {
	if err := l.hook("commit"); err != nil {
		return err
	}
	return endStage(stage)
}

// endStage ends a stage that needs nothing more: a switch that is done, or
// one undone (restore) or settled. The journal goes first, so what is left
// is never undone again. Another program holding it open (an antivirus, the
// indexer) stops that for a moment, so it is tried again for about two
// seconds; then the stage is marked done instead, by a new file, which a
// hold on the journal does not stop. Only when neither works does the stage
// stay pending, for another try. A stage left behind once it is ended is
// only a copy, for the next switch, Tidy, TurnOff or DropAll to delete.
func endStage(stage string) error {
	jp := filepath.Join(stage, "journal.json")
	if err := retry(func() error { return removeFile(jp) }); err != nil {
		p := filepath.Join(stage, doneName)
		desktop.Guard("write", p)
		if err := fsx.WriteFileAtomic(p, []byte("done\r\n"), 0o600); err != nil {
			return err
		}
	}
	removeStage(stage)
	return nil
}

// retry runs f until it works, for about two seconds (moveWait): a file
// another program holds open cannot go for a moment.
func retry(f func() error) error {
	deadline := time.Now().Add(moveWait)
	for {
		err := f()
		if err == nil || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// removeStage deletes a stage that is only a copy, trying again for a
// moment. Its journal goes first, and the rest only once it is gone: a
// stage marked done keeps its mark for as long as it keeps its journal
// (the copies in it go all the same).
func removeStage(stage string) error {
	return retry(func() error {
		if err := removeFile(filepath.Join(stage, "journal.json")); err != nil {
			removeTree(filepath.Join(stage, "old"))
			removeTree(filepath.Join(stage, "new"))
			removeTree(filepath.Join(stage, "out"))
			return err
		}
		return removeTree(stage)
	})
}

// move is the switch proper, under baton's desktop lock: the items, then
// config.json, then a look that all is as meant. Its error says which step
// (no value of the app's).
func (l *Logins) move(data, stage string, j *journal, before []byte, cfg *appConfig, add []kv) error {
	for _, it := range j.Items {
		if err := l.hook("move " + it.Name); err != nil {
			return err
		}
		x := filepath.Join(data, it.Name)
		if it.HadOld {
			o := filepath.Join(stage, "old", it.Name)
			if err := mkdir(filepath.Dir(o)); err != nil {
				return errors.New("move " + it.Name)
			}
			if err := rename(x, o); err != nil {
				return errors.New("move " + it.Name)
			}
		}
		if it.HasNew {
			if err := mkdir(filepath.Dir(x)); err != nil {
				return errors.New("move " + it.Name)
			}
			if err := rename(filepath.Join(stage, "new", it.Name), x); err != nil {
				return errors.New("move " + it.Name)
			}
		}
	}
	if err := l.hook("config"); err != nil {
		return err
	}
	cp := filepath.Join(data, "config.json")
	now, err := fsx.ReadFileShared(cp)
	if err != nil || !bytes.Equal(now, before) {
		return errors.New("config changed")
	}
	after := cfg.obj.splice(add)
	if _, why := readAppConfig(after); why != "" {
		return errors.New("config patch")
	}
	for i := range j.Items {
		if j.Items[i].HasNew {
			j.Items[i].Stamp = stamp(filepath.Join(data, j.Items[i].Name))
		}
	}
	j.Phase, j.CfgAfter = phaseConfig, sum(after)
	if err := writeJournal(stage, j); err != nil {
		return errors.New("journal")
	}
	if err := l.hook("flip"); err != nil {
		return err
	}
	if err := writeConfig(cp, after); err != nil {
		return errors.New("config write")
	}
	// Only a help to Settle: when a stop, or a journal held open, keeps this
	// note out, Settle goes by the account config.json names instead.
	j.Flipped = true
	_ = writeJournal(stage, j)
	if err := l.hook("verify"); err != nil {
		return err
	}
	got, err := fsx.ReadFileShared(cp)
	if err != nil {
		return errors.New("verify")
	}
	gotObj, _ := parseObject(got)
	if gotObj == nil || !sameRest(cfg.obj, gotObj, add) || desktop.Active(data) != j.To {
		return errors.New("verify config")
	}
	for _, it := range j.Items {
		if exists(filepath.Join(data, it.Name)) != it.HasNew {
			return errors.New("verify " + it.Name)
		}
	}
	return nil
}

func writeJournal(stage string, j *journal) error {
	p := filepath.Join(stage, "journal.json")
	desktop.Guard("write", p)
	return fsx.WriteJSONAtomic(p, j)
}

func readJournal(stage string) (*journal, error) {
	var j journal
	if err := fsx.ReadJSON(filepath.Join(stage, "journal.json"), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// writeConfig writes the app's config.json, trying again for about two
// seconds: a reader that holds it open without sharing delete blocks the
// rename for a moment.
func writeConfig(path string, data []byte) error {
	desktop.Guard("write", path)
	deadline := time.Now().Add(moveWait)
	for {
		err := fsx.WriteFileAtomic(path, data, 0o600)
		if err == nil || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func removeFile(p string) error {
	desktop.Guard("remove", p)
	err := os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// doneName marks a stage whose switch is done, undone or settled but whose
// journal could not be deleted (see endStage): only a copy, never undone.
const doneName = "done"

// restoringName marks a stage whose config.json a restore has begun to
// write back. From then on the journal's Flipped no longer says where the
// app is: config.json names From again, whatever its items are, and a
// restore that stopped part way must be finished, never left as it is. A
// new file of its own, not a change of the journal: another program
// holding the journal open cannot keep it out.
const restoringName = "restoring"

// markRestoring notes, before restore writes config.json back, that it has
// begun to (restoringName). When that fails, nothing has changed.
func markRestoring(stage string) error {
	p := filepath.Join(stage, restoringName)
	if exists(p) {
		return nil
	}
	desktop.Guard("write", p)
	return fsx.WriteFileAtomic(p, []byte("restoring\r\n"), 0o600)
}

// writeBack writes config.json (cp) back for a restore, as b, noting first
// that it has begun to (markRestoring), and, with notes, which items in the
// app are not the sign-in's it puts back (noteForeign). A write that fails
// leaves config.json as it was (writeConfig fails only before its rename),
// so the notes this call made go again: left, the restoring one would
// cancel a flip known to be in place (landed), and a later restore would
// put the old items under a sign-in the app made since. A note an earlier
// restore made stays: that one may have written config.json. Only a stop in
// between (baton killed) leaves a note of a write that never came.
func (l *Logins) writeBack(stage, cp string, b []byte, notes func() ([]string, error)) error {
	p := filepath.Join(stage, restoringName)
	made := !exists(p)
	if err := markRestoring(stage); err != nil {
		return err
	}
	var noted []string
	undo := func() {
		for _, n := range noted {
			_ = removeFile(n)
		}
		if made {
			_ = removeFile(p)
		}
	}
	if notes != nil {
		var err error
		if noted, err = notes(); err != nil {
			undo()
			return err
		}
	}
	if err := l.hook("restore config"); err != nil {
		return err
	}
	if err := writeConfig(cp, b); err != nil {
		undo()
		return err
	}
	return nil
}

// Items the sign-in a switch left did not have (HadOld false) need care
// when they are put back: whatever the app has in their place is taken out
// of it into the stage's out\, never deleted at once, and out\<item> says
// it was, so what the app makes there afterwards, on that sign-in, is its
// own (a put back that stopped, then the app run on it).
//
// foreignPrefix notes such an item (foreign-<its place in the journal>)
// that a restore found in the app off that sign-in (putBack's foreign:
// another sign-in, kept first, or the app on another account, or none, or
// with no token), before it writes config.json back. From that write on
// the app names that sign-in again, and an item the restore did not take
// out yet (it stopped before) would pass for its own: the note says it is
// not, until it is taken out. A file of its own, as restoringName is.
const foreignPrefix = "foreign-"

// noteForeign notes each item the sign-in did not have that the app has now
// (foreignPrefix): the notes it made. One taken out before, which the app
// made again since, goes as the copy taken out (out\) does: the note is for
// this one.
func noteForeign(data, stage string, its []jitem) ([]string, error) {
	var made []string
	for i, it := range its {
		if it.HadOld || !exists(filepath.Join(data, it.Name)) {
			continue
		}
		if out := filepath.Join(stage, "out", it.Name); exists(out) {
			if err := removeTree(out); err != nil {
				return made, err
			}
		}
		p := filepath.Join(stage, foreignPrefix+strconv.Itoa(i))
		if exists(p) {
			continue
		}
		desktop.Guard("write", p)
		if err := fsx.WriteFileAtomic(p, []byte("foreign\r\n"), 0o600); err != nil {
			return made, err
		}
		made = append(made, p)
	}
	return made, nil
}

// landed: config.json as the switch wrote it is known to be in place: the
// journal noted it, and no restore has begun to write it back since.
func landed(stage string, j *journal) bool {
	return j.Flipped && !exists(filepath.Join(stage, restoringName))
}

// leftSignIn: the switch staged in stage left a sign-in in the app (the
// config.json it read, stage\old, holds a token). One from an app with no
// token (signed out by its own Log out, which still names the account)
// left none: the items it set aside are what that app had (stashed in the
// store before), and a token in the app now is a sign-in made since, also
// when it names the same account. A copy that is gone, or not shaped as
// baton knows, says nothing, and counts as a sign-in left. One that is
// there but cannot be read now (cannotRead) is an error: tried again, it
// may say there is none.
func leftSignIn(stage string) (bool, error) {
	b, err := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return leftBy(b, err), nil
}

// leftBy is leftSignIn of the copy as it was read: b, or err when it is
// gone.
func leftBy(b []byte, err error) bool {
	if err != nil {
		return true
	}
	c, _ := readAppConfig(b)
	return c == nil || c.token
}

// cannotRead: err is a failure to open or read a file that is there (another
// program holds it without sharing it for reading, say), not that it is gone
// nor that what was read is not whole. It says nothing of the file, so what
// goes by it is tried again, never decided on it: taken for "gone" or "not
// what baton wrote", a restore or Settle would put the wrong sign-in's items
// under the app's tokens, or delete what the stage holds.
func cannotRead(err error) bool {
	var pe *os.PathError
	return errors.As(err, &pe) && !errors.Is(err, os.ErrNotExist)
}

// rawAccount is the account config.json b names (lastKnownAccountUuid, in
// lower case), read also when its shape is one baton does not know
// (readAppConfig refuses it): "" when it names none, or is not one object.
func rawAccount(b []byte) string {
	o, _ := parseObject(b)
	if o == nil {
		return ""
	}
	m, ok := o.get(accountKey)
	if !ok {
		return ""
	}
	var s string
	if !jsonString(o.raw(m), &s) {
		return ""
	}
	if a := strings.ToLower(strings.TrimSpace(s)); desktop.ValidID(a) {
		return a
	}
	return ""
}

// unknownShape: config.json b is one object, but not shaped as baton knows
// it (readAppConfig): the app (an update, say) rewrote it in a new way,
// which may hold a token baton cannot see.
func unknownShape(b []byte) bool {
	if c, _ := readAppConfig(b); c != nil {
		return false
	}
	o, _ := parseObject(b)
	return o != nil
}

// namesLeft: config.json b names the account whose sign-in the switch left
// (From, with a token: left, as leftSignIn says), so the app is on that
// account's config.json, whatever its shape.
func namesLeft(b []byte, j *journal, left bool) bool {
	return j.From != "" && rawAccount(b) == j.From && left
}

// errUnknownShape: config.json changed into a shape baton does not know,
// and whose sign-in it holds cannot be told: restore changes nothing.
var errUnknownShape = errors.New("config.json changed into a shape baton does not know")

// pending: stage holds a journal of a switch that is not done, so things
// may have moved out of the app into it. Any other stage is only a copy.
func pending(stage string) bool {
	return exists(filepath.Join(stage, "journal.json")) && !exists(filepath.Join(stage, doneName))
}

// stages are the switches' stages in the app's data folder; withJournal
// only those a switch may have moved things out of (pending).
func stages(data string, withJournal bool) []string {
	out, _ := stagesErr(data, withJournal)
	return out
}

// stagesErr is stages, with the error of a folder that is there but cannot
// be listed now (another program holds it without sharing it). That says
// nothing of whether a switch was cut off, so what must be sure of it
// (CutOffNow, Recover, Settle, every switch) is tried again, never decided
// on "none": uninstall would delete the kept copies past a switch cut off,
// and a switch would keep a half switched app as a sign-in. A folder that
// is gone holds none.
func stagesErr(data string, withJournal bool) ([]string, error) {
	ents, err := os.ReadDir(data)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), stagePrefix) {
			continue
		}
		p := filepath.Join(data, e.Name())
		if fi, err := os.Lstat(p); err != nil || fsx.IsReparsePoint(fi) {
			continue
		}
		if withJournal && !pending(p) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// stuck looks at the stages in the app's folder while it holds the lock for
// a moment (a switch holds it from start to end, in this process or
// another), so never at one under way; while the lock is taken it sees
// nothing. cut: a switch was cut off and is not undone; left: how many are
// only copies (Tidy).
func (l *Logins) stuck(data string) (cut bool, left int) {
	if len(stages(data, false)) == 0 || !exists(filepath.Dir(l.Lock)) {
		return false, 0
	}
	desktop.Guard("lock", l.Lock)
	unlock, err := fsx.LockWait(l.Lock, 0)
	if err != nil {
		return false, 0
	}
	defer unlock()
	for _, s := range stages(data, false) {
		if pending(s) {
			cut = true
		} else {
			left++
		}
	}
	return cut, left
}

// CutOffNow says whether a switch was cut off and is not undone yet, as
// View.CutOff does, but under the lock, waiting for it as a change does
// (l.wait): View's look sees nothing while another baton holds the lock for
// a moment (its own look, say), and uninstall must not take that for "none".
// When the app's folders are no longer a layout baton knows, it looks in
// each of them (appDirs): a switch cut off before the layout changed is
// still there, and uninstall must settle it, never delete the kept copies
// past it. No folder of the app's means no stage of baton's either; one it
// cannot list now (stagesErr) is an error, never "none", and so is a
// Claude-Data it cannot list now (appDirsErr): the app's folder is found by
// listing it, so none is found then.
func (l *Logins) CutOffNow() (bool, error) {
	dirs, lerr := l.appDirsErr()
	if data, err := l.dataDir(); err == nil {
		dirs = []string{data}
	} else if lerr != nil {
		return false, lerr
	}
	if len(dirs) == 0 {
		return false, nil
	}
	unlock, err := l.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	for _, d := range dirs {
		s, err := stagesErr(d, true)
		if err != nil {
			return false, err
		}
		if len(s) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// Recover undoes a switch that was cut off, when the user asks: the app must
// be fully quit. It works while the feature is off. ok: one was undone. It
// never throws away a sign-in the app made or renewed since (see restore):
// when that cannot be kept it changes nothing and says why, and the user
// may then leave the app as it is instead (Settle). Like Settle, it works in
// the folder the switch was cut off in also after the layout changed
// (stageDir).
func (l *Logins) Recover() (ok bool, err error) {
	if os.Getenv("CLAUDECODE") != "" {
		return false, fail(CodeInsideClaude, "")
	}
	data, err := l.stageDir()
	if err != nil {
		return false, err
	}
	unlock, err := l.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	release, err := l.quit(data)
	if err != nil {
		return false, err
	}
	defer release()
	l.sweep()
	return l.recoverAll(data)
}

// recoverAll undoes every stage in the app's folder, under baton's desktop
// lock (the lock and the app's are held). A stage that is only a copy is
// deleted. A refusal of restore's (a code) is passed on as it is: it
// changed nothing, and says why. A folder it cannot list now (stagesErr)
// changes nothing either, and is tried again: a switch would otherwise go
// on past one cut off.
func (l *Logins) recoverAll(data string) (bool, error) {
	all, err := stagesErr(data, false)
	if err != nil {
		return false, fail(CodeSwitchUnrestored, "")
	}
	if len(all) == 0 {
		return false, nil
	}
	desktop.Guard("lock", l.DeskLock)
	unlockDesk, err := fsx.LockWait(l.DeskLock, l.wait)
	if err != nil {
		if errors.Is(err, fsx.ErrLocked) {
			return false, fail(CodeBusy, "desktop")
		}
		return false, err
	}
	defer unlockDesk()
	undone := false
	for _, s := range all {
		had := pending(s)
		if err := l.restore(data, s); err != nil {
			if Code(err) != "" {
				return undone, err
			}
			return undone, fail(CodeSwitchUnrestored, "")
		}
		undone = undone || had
	}
	return undone, nil
}

// restore undoes the switch staged in stage: the one rule for a rollback
// and for Recover.
//
//   - The app was used since when config.json, or an item moved in, changed
//     after the switch wrote it; before the flip, when config.json holds a
//     token of an account other than the one the switch left (or of one
//     not named yet). A switch from an app with no token left no sign-in
//     (leftSignIn): any token there now is the app's own since, also of the
//     account it names. A token it holds now is never thrown away: a
//     sign-in other than the one the switch left is kept first, under the
//     account the app names (its tokens may be renewed, and the copy kept
//     before would ask for a sign-in). When it cannot be kept, nothing
//     changes and the stage stays, with a code that says why: the app has
//     not named the account yet (dl_unsaved_current, no_account), its
//     tokens are another kept account's (dl_damaged, tokens_of_another) or
//     another account's kept tokens cannot be read (dl_unsaved_current,
//     kept_unreadable), or switching is off or the store syncs
//     (dl_used_since; Settle leaves the app as it is).
//   - What of the sign-in the switch left was still in the app then (cut
//     while moving, or put back by a restore that stopped) belongs to that
//     other sign-in now: it comes from the kept copy instead, which the
//     switch made just before it moved anything. When that copy cannot
//     give it, nothing changes (Settle leaves the app as it is).
//   - config.json: as the switch wrote it, it goes back byte for byte; as
//     it was read, it stays (before the flip baton never writes it).
//     Changed by the app since, only the six values are put back as they
//     were, and every other setting the app changed stays; when it names
//     the account the switch left, they stay too, unless it holds no token
//     while all that sign-in's items are still set aside (signed out since
//     by its own Log out): then they go back, and the sign-in with them,
//     whole (its tokens are in no other place). Before the first write a
//     file notes that a restore has begun (restoringName; writeBack takes
//     it away again when the write fails). Changed into a shape baton does
//     not know, nothing changes, unless it names the account the switch
//     left while the flip may never have landed (namesLeft): then it stays
//     as the app wrote it, and the items go back.
//   - Once the flip is known to have landed (landed), an app that names the
//     account the switch left, signed in, went back to it by itself (signed
//     in again): it is left as it is, as Settle leaves it. Otherwise the
//     flip may never have landed, and the app may have run on the items
//     the switch moved in: those are put back. Signed in again there, the
//     app would keep its tokens with the older items of the same account,
//     never another account's.
//   - Each item moved out goes back, over whatever is there now (a copy of
//     a kept one, or what the app made meanwhile); an item moved in where
//     there was none goes (the store still has it), into the stage (out\):
//     once out, what the app makes there on that sign-in since is its own,
//     also when this restore stops and runs again. When another sign-in was
//     kept first, or the app is off the sign-in the switch left (it names
//     another account or none, or holds no token: onLeft), every item there
//     that sign-in did not have is not its own, and goes too: left, it
//     would sit under the tokens put back. Each is noted so before
//     config.json names that sign-in again (foreignPrefix), for a try after
//     a stop.
//   - A file it goes by (the journal, config.json, the config.json the
//     switch read, baton's own config.json) that is there but cannot be
//     read now (cannotRead: another program holds it without sharing it
//     for reading) is never taken for gone, nor for switching off:
//     nothing changes, for another try.
//   - The journal goes last (endStage: while another program holds it, the
//     stage is marked done instead); when anything fails before, it stays,
//     for another try. What is left once it is gone is only a copy (Tidy).
func (l *Logins) restore(data, stage string) error {
	if !pending(stage) {
		// Nothing was moved yet, or the switch was done: only a copy. One
		// that cannot be deleted now stops nothing (View.Leftover).
		removeStage(stage)
		return nil
	}
	j, err := readJournal(stage)
	if err != nil {
		return err
	}
	for _, it := range j.Items {
		if !knownItem(it.Name) {
			return errors.New("the journal names an unknown item")
		}
	}
	cp := filepath.Join(data, "config.json")
	left, err := leftSignIn(stage)
	if err != nil {
		return err
	}
	var now, cur []byte // cur: config.json as the app has it (nil: there is none)
	h, ran := "", false
	if j.Phase != phaseConfig {
		// Before the flip baton never writes config.json, nor needs it (it
		// may be gone). Only a token of an account other than the one the
		// switch left, or of one not named yet, or any token when the switch
		// left none (leftSignIn), says the app was signed in since: that
		// sign-in is its own, as after the flip. A shape baton does not know
		// may hold one it cannot see: unless it names the account the switch
		// left (namesLeft), nothing changes. One that is there but cannot be
		// read now says nothing either way: nothing changes, for another try
		// (taken for gone, a sign-in made since would get the old items).
		if b, err := fsx.ReadFileShared(cp); err == nil {
			cur = b
			c, _ := readAppConfig(b)
			switch {
			case c != nil && c.token && (c.account == "" || c.account != j.From || !left):
				now, h, ran = b, sum(b), true
			case unknownShape(b) && !namesLeft(b, j, left):
				return errUnknownShape
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if now, err = fsx.ReadFileShared(cp); err != nil {
			return err
		}
		cur, h = now, sum(now)
		ran = h != j.CfgAfter && h != j.CfgBefore
		for _, it := range j.Items {
			if it.HasNew && stamp(filepath.Join(data, it.Name)) != it.Stamp {
				ran = true
			}
		}
	}
	var nowCfg *appConfig
	others := false // the app was on another sign-in, kept first
	if ran {
		nowCfg, _ = readAppConfig(now)
		// A token whose account the app has not named yet is never the sign-in
		// the switch left, also when that was a signed-out app (From ""): a
		// switch never starts from a token with no account. It is the app's own
		// since, so it is not taken for "the flip never landed" below, where
		// putting the old items back would delete its cookies. Nor is any
		// token when the switch left none (an app its own Log out left naming
		// the account): signed in to that account again since, it is kept
		// first as well.
		if nowCfg != nil && nowCfg.token && (nowCfg.account != j.From || nowCfg.account == "" || !left) {
			if nowCfg.account == "" {
				return fail(CodeUnsavedCurrent, "no_account")
			}
			another, err := l.tokensOfAnother(nowCfg)
			// Whether switching is on, asked only once the tokens are known
			// to be no other's (the refusals keep their order). baton's
			// config.json that cannot be read now is no answer either: tried
			// again, never taken for off (dl_used_since).
			var onErr error
			if err == nil && !another {
				onErr = l.on()
			}
			switch {
			case err != nil:
				return err // cannot be told now: nothing changes, for another try
			case another:
				return fail(CodeDamaged, "tokens_of_another")
			case onErr != nil && Code(onErr) != CodeOff:
				return onErr
			case onErr != nil || synced(l.Store):
				return fail(CodeUsedSince, "")
			}
			fill, err := l.toFill(stage, j)
			if err != nil {
				return err
			}
			if err := l.keep(data, nowCfg.account, nowCfg); err != nil {
				return err
			}
			if err := l.fillStage(stage, j.From, fill); err != nil {
				return err
			}
			others = true
		}
	}
	// What the app has in an item the sign-in the switch left did not have
	// is that sign-in's own only when the app was on it (a put back that
	// stopped, then the app run on it): kept first as another's, or made
	// while it named another account or none, or held no token, it goes,
	// and is noted so before config.json names that sign-in again.
	foreign := others || left && !onLeft(cur, j)
	var notes func() ([]string, error)
	if foreign {
		notes = func() ([]string, error) { return noteForeign(data, stage, j.Items) }
	}
	// config.json before the items: once it is back, a try after a stop
	// in between never takes the items for the app's.
	switch {
	case j.Phase != phaseConfig && !ran, h == j.CfgBefore:
		// As it was read: baton never wrote it.
	case h == j.CfgAfter:
		old, err := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
		if err != nil {
			return err
		}
		if err := l.writeBack(stage, cp, old, notes); err != nil {
			return err
		}
	default:
		if nowCfg == nil {
			// A shape baton does not know cannot be spliced. One that names
			// the account the switch left, while the flip may never have
			// landed, is that account's config.json, run on since: it stays,
			// and only the items go back, as when it is shaped as baton knows.
			if landed(stage, j) || !namesLeft(now, j, left) {
				return errUnknownShape
			}
			break
		}
		// Signed out since (its own Log out) while every item of the sign-in
		// the switch left is still set aside: its six go back with them,
		// whole, as over any other account's config.json. Left as the app
		// wrote it, that sign-in's tokens would go with the stage. Had the
		// Log out ended it (the app was on it), the app asks for a sign-in
		// again; nothing is lost either way.
		outWhole := left && !nowCfg.token && len(missing(stage, j)) == 0
		if nowCfg.account == j.From && (left || !nowCfg.token) && !outWhole {
			if landed(stage, j) && j.From != "" && nowCfg.token {
				// Signed in to that account again since the flip: left as it
				// is. What the switch set aside is its older sign-in, kept only
				// when the copy kept before is not whole (and never deleted on
				// a read that failed: tried again instead).
				old, err := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
				if err == nil {
					err = l.keepFromStage(stage, old, j)
				} else if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
				if err != nil {
					return err
				}
				return endStage(stage)
			}
			break // the flip never landed, or may not have: what the app wrote stays
		}
		old, err := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
		if err != nil {
			return err
		}
		oldObj, _ := parseObject(old)
		if oldObj == nil {
			return errors.New("the old config.json is not whole")
		}
		if err := l.writeBack(stage, cp, nowCfg.obj.splice(oldObj.login()), notes); err != nil {
			return err
		}
	}
	if err := putBack(data, stage, j.Items, foreign); err != nil {
		return err
	}
	return endStage(stage)
}

// onLeft: config.json as the app has it (b) is on the sign-in the switch
// left (From, which held a token), as far as can be told: it names From
// with a token, or in a shape baton does not know (whose token it cannot
// see). One that is gone, or is not one object, tells nothing, and counts
// as on it (restore never gets this far with one it cannot read now).
func onLeft(b []byte, j *journal) bool {
	if b == nil {
		return true
	}
	if o, _ := parseObject(b); o == nil {
		return true
	}
	c, _ := readAppConfig(b)
	return rawAccount(b) == j.From && (c == nil || c.token)
}

// missing are the items the app had before the switch that are not set
// aside in the stage: never moved out (cut while moving), or put back by a
// restore that stopped.
func missing(stage string, j *journal) []string {
	var out []string
	for _, it := range j.Items {
		if it.HadOld && !exists(filepath.Join(stage, "old", it.Name)) {
			out = append(out, it.Name)
		}
	}
	return out
}

// toFill are the items the sign-in a switch left needs from its kept copy
// to go back whole, once the app was signed in to another account since:
// those that were still in the app (missing). Its kept copy is the one the
// switch made just before it moved anything (its tokens say so); when it
// cannot give them, an error, and nothing has changed. A switch from a
// signed-out app left no sign-in: what it set aside goes back as it is.
func (l *Logins) toFill(stage string, j *journal) ([]string, error) {
	miss := missing(stage, j)
	if len(miss) == 0 {
		return nil, nil
	}
	b, err := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
	if err != nil {
		return nil, err
	}
	old, _ := readAppConfig(b)
	if old == nil || !old.token || old.account == "" || old.account != j.From {
		return nil, nil
	}
	dir := l.acctDir(j.From)
	notWhole := errors.New("part of the sign-in the switch left is no longer in the app, and its kept copy cannot give it")
	m, err := readMeta(dir)
	if err != nil || checkFolder(dir, j.From) != "" {
		return nil, notWhole
	}
	kb, err := fsx.ReadFileShared(filepath.Join(dir, "keys.json"))
	if err != nil {
		return nil, notWhole
	}
	kept, _ := readKeys(kb)
	for _, k := range old.obj.login() {
		if slices.Contains(tokenKeys, k.key) && !slices.ContainsFunc(kept, func(c kv) bool { return c.key == k.key && bytes.Equal(c.raw, k.raw) }) {
			return nil, notWhole // kept since: another sign-in of that account
		}
	}
	for _, n := range miss {
		if !slices.Contains(m.Has, n) {
			return nil, notWhole
		}
	}
	return miss, nil
}

// fillStage copies each of names from the kept copy of acct into the
// stage's old\, as the switch would have moved it there, so that putting
// back gives that sign-in whole. Each goes in under a temporary name first:
// a stop leaves no half item where putBack looks.
func (l *Logins) fillStage(stage, acct string, names []string) error {
	for _, n := range names {
		dst := filepath.Join(stage, "old", n)
		if exists(dst) {
			continue
		}
		tmp := filepath.Join(stage, ".fill-"+rnd())
		_, _, err := copyTree(filepath.Join(l.acctDir(acct), n), tmp, n)
		if err == nil {
			err = mkdir(filepath.Dir(dst))
		}
		if err == nil {
			err = rename(tmp, dst)
		}
		if err != nil {
			removeTree(tmp)
			return err
		}
	}
	return nil
}

// putBack moves each item a switch moved out of the app back in, over
// whatever is there now, and takes out each it moved in where there was
// none: the last first. foreign: the app was not on the sign-in put back
// since (restore), so an item there that this sign-in did not have is not
// its own, and goes too (see foreignPrefix).
func putBack(data, stage string, its []jitem, foreign bool) error {
	for i := len(its) - 1; i >= 0; i-- {
		it := its[i]
		x, o, n := filepath.Join(data, it.Name), filepath.Join(stage, "old", it.Name), filepath.Join(stage, "new", it.Name)
		out, noted := filepath.Join(stage, "out", it.Name), filepath.Join(stage, foreignPrefix+strconv.Itoa(i))
		switch {
		case it.HadOld && exists(o):
			if exists(x) {
				if err := removeTree(x); err != nil {
					return err
				}
			}
			if err := rename(o, x); err != nil {
				return err
			}
		case !it.HadOld && exists(x) && (foreign || !exists(out) && (it.HasNew && !exists(n) || exists(noted))):
			// An item From did not have: the switch's copy, never taken out
			// yet, or what the app made off From's sign-in (another's is kept
			// first). Taken out into out\, not deleted: once it is there, what
			// the app makes in its place on From's sign-in (a put back that
			// stopped, then the app run on From) is From's own.
			if exists(out) {
				if err := removeTree(x); err != nil {
					return err
				}
				continue
			}
			if err := mkdir(filepath.Dir(out)); err != nil {
				return err
			}
			if err := rename(x, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// Settle leaves the desktop app as it is now, for a switch that was cut off
// and that Recover will not undo (dl_used_since: the app was used on the
// sign-in it switched to, which cannot be kept now) or cannot (the app's
// files changed in a way baton does not know): only when the user asks,
// with the app fully quit. It works while the feature is off, and it is
// what uninstall does with a switch that was cut off, so the app stays
// signed in to the account it is on, as uninstall says. ok: one was settled.
//
// A stage whose config.json flip landed (or whose config.json is gone after
// it, or whose journal is not whole), or whose app was signed in to another
// account since, is left as the app has it: the app stays on the sign-in
// its config.json names. What the switch moved out of the app is the
// sign-in it was on, kept in the store before the switch; while switching
// is on and that kept copy is not whole, it is kept from the stage first
// (when the stage holds all of it). Then the stage goes. Before the flip
// (also when the app ran since on the config.json the switch never wrote),
// and once a restore has begun to write config.json back, the app is on the
// config.json of the account the switch left, so its items go back as
// Recover puts them, which always works. A config.json the app rewrote in a
// shape baton does not know is left as the app wrote it; only when it names
// that account (and the flip may never have landed) do the items go back
// under it.
//
// While another program holds one of the files it goes by without sharing
// it for reading (cannotRead), or the app's folder so that it cannot be
// listed (stagesErr), it changes nothing and fails with
// dl_switch_unrestored: try again.
//
// It works in the folder the switch was cut off in also when the app's
// folders are no longer a layout baton knows (stageDir): otherwise nothing
// could settle it, and uninstall would stop there for good.
func (l *Logins) Settle() (ok bool, err error) {
	if os.Getenv("CLAUDECODE") != "" {
		return false, fail(CodeInsideClaude, "")
	}
	data, err := l.stageDir()
	if err != nil {
		return false, err
	}
	unlock, err := l.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	release, err := l.quit(data)
	if err != nil {
		return false, err
	}
	defer release()
	l.sweep()
	all, err := stagesErr(data, false)
	if err != nil {
		return false, fail(CodeSwitchUnrestored, "")
	}
	if len(all) == 0 {
		return false, nil
	}
	desktop.Guard("lock", l.DeskLock)
	unlockDesk, err := fsx.LockWait(l.DeskLock, l.wait)
	if err != nil {
		if errors.Is(err, fsx.ErrLocked) {
			return false, fail(CodeBusy, "desktop")
		}
		return false, err
	}
	defer unlockDesk()
	for _, s := range all {
		had := pending(s)
		if err := l.settle(data, s); err != nil {
			if Code(err) != "" {
				return ok, err
			}
			return ok, fail(CodeSwitchUnrestored, "")
		}
		ok = ok || had
	}
	return ok, nil
}

// settle is Settle for one stage, under every lock.
func (l *Logins) settle(data, stage string) error {
	if !pending(stage) {
		return l.restore(data, stage) // only a copy: deleted
	}
	j, jerr := readJournal(stage)
	now, cerr := fsx.ReadFileShared(filepath.Join(data, "config.json"))
	old, oerr := fsx.ReadFileShared(filepath.Join(stage, "old", "config.json"))
	// A file that is there but cannot be read now (another program holds it
	// without sharing it for reading) says nothing of where the app is:
	// nothing changes, and Settle says to try again. Taken for one that is
	// gone, or not whole, it would leave the app half switched, or delete
	// the sign-in the stage holds. A journal that is not whole (read, but
	// not what baton wrote) is an answer: it never will be.
	for _, err := range []error{jerr, cerr, oerr} {
		if cannotRead(err) {
			return err
		}
	}
	left := leftBy(old, oerr)
	// Whether the app is on the config.json of the account the switch left
	// (noFlip): its items go back then, as Recover puts them.
	//   - Before the flip baton never wrote config.json, so the app is on it,
	//     unless it was signed in since to another account, or to one it has
	//     not named yet: that sign-in is left as it is, as after the flip
	//     (Recover would keep it and put the one the switch left back, and
	//     uninstall would then delete the copy it kept).
	//   - config.json as the switch read it says so.
	//   - Once the app has run on it and rewritten it, it still names the
	//     account the switch left (as restore reads it), unless the journal
	//     says the write landed (landed): then the app went back to that
	//     account itself, and is left as it is. A restore that has begun to
	//     write config.json back ends that note: it is finished instead.
	//   - A switch from a signed-out app names no account, so an app signed
	//     in since that has not named its account yet could be either: it is
	//     left as it is, and its sign-in with it.
	//   - A switch from an app with no token left no sign-in (leftSignIn), so
	//     a token there now is a sign-in made since, also when it names the
	//     account the switch started from: it is left as it is.
	//   - A config.json in a shape baton does not know says only which
	//     account it names: the account the switch left means its items go
	//     back (config.json stays as the app wrote it); any other, or none,
	//     may be a sign-in baton cannot see, which is left as it is.
	noFlip := false
	if jerr == nil && cerr == nil {
		c, _ := readAppConfig(now)
		switch h := sum(now); {
		case j.Phase != phaseConfig:
			switch {
			case c != nil:
				noFlip = !c.token || c.account != "" && c.account == j.From && left
			case unknownShape(now):
				noFlip = namesLeft(now, j, left)
			default:
				noFlip = true // not one object: it says nothing, as when it is gone
			}
		case h == j.CfgBefore:
			noFlip = true
		case h != j.CfgAfter && !landed(stage, j):
			if c == nil {
				noFlip = namesLeft(now, j, left)
			} else {
				noFlip = c.account == j.From && (c.account != "" || !c.token) && (!c.token || left)
			}
		}
	}
	switch {
	case jerr == nil && (noFlip || j.Phase != phaseConfig && cerr != nil):
		// Before the flip config.json plays no part (it may be gone).
		return l.restore(data, stage)
	case jerr != nil && cerr == nil && oerr == nil && bytes.Equal(now, old):
		// No journal to go by, but config.json is as the switch read it:
		// what it moved out goes back.
		var its []jitem
		for _, it := range items {
			its = append(its, jitem{Name: it.name, HadOld: true})
		}
		if err := putBack(data, stage, its, false); err != nil {
			return err
		}
	default:
		if oerr == nil {
			var jj *journal
			if jerr == nil {
				jj = j
			}
			if err := l.keepFromStage(stage, old, jj); err != nil {
				return err
			}
		}
	}
	return endStage(stage)
}

// keepFromStage keeps what a switch moved out of the app (stage\old, with
// the six values of the config.json it read, old) as the sign-in of the
// account that names, before the stage is deleted with the app left as it
// is: only while switching is on (off, kept sign-ins are deleted), with a
// token, and when the copy kept before the switch is not whole. Only a
// whole sign-in is kept: when part of it never left the app (cut while
// moving) or went back into it (a restore that stopped), the app has had
// another sign-in since, and what the stage holds is not one (the journal,
// j, says; nil when it cannot be read). baton's config.json that cannot be
// read now (another program holds it) says nothing of whether switching is
// on: it is an error, tried again, never taken for off, which would delete
// with the stage the only whole copy of that sign-in.
func (l *Logins) keepFromStage(stage string, old []byte, j *journal) error {
	if err := l.on(); err != nil && Code(err) != CodeOff {
		return err
	} else if err != nil || synced(l.Store) {
		return nil
	}
	cfg, _ := readAppConfig(old)
	if cfg == nil || cfg.account == "" || !cfg.token || checkFolder(l.acctDir(cfg.account), cfg.account) == "" {
		return nil
	}
	if j != nil && len(missing(stage, j)) > 0 {
		return nil
	}
	return l.keep(filepath.Join(stage, "old"), cfg.account, cfg)
}

func jsonString(raw []byte, s *string) bool { return json.Unmarshal(raw, s) == nil }
