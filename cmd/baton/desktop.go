package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/desklogin"
	"baton/internal/desktop"
)

// cmdDesktop says in which accounts' Claude desktop app a conversation is
// listed, and changes that (--show, --hide: baton's account names, or
// "desktop" for the account the desktop app is signed in to when it is none
// of baton's). It does what the window's "Choose desktop app accounts" dialog does.
func cmdDesktop(args []string) error {
	// A session id is never the word "login".
	if len(args) > 0 && args[0] == "login" {
		return cmdDesktopLogin(args[1:])
	}
	const usage = "baton desktop <session-id> [--show <accounts>] [--hide <accounts>]   (or: baton desktop login ...)"
	fs := newFlags("desktop", usage)
	show := fs.String("show", "", "list it in these accounts' desktop app too (names, comma-separated)")
	hide := fs.String("hide", "", "take it off these accounts' desktop app (names, comma-separated)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := oneName(pos, usage)
	if err != nil {
		return err
	}
	if err := refuseInsideSession("change where sessions show in the Claude desktop app"); err != nil {
		return err
	}
	if !sessionIDRx.MatchString(id) {
		return errors.New(say("a session id looks like 1b2c3d4e-...; see: baton sessions", "세션 id는 1b2c3d4e-... 형식입니다. baton sessions로 확인하세요"))
	}
	id = strings.ToLower(id)
	e, err := load()
	if err != nil {
		return err
	}
	// A change while a delete of it runs (here or in the window) would list
	// files on their way to the Recycle Bin in the desktop app again. A look
	// changes nothing, so it stays allowed.
	change := *show != "" || *hide != ""
	if change {
		if err := refuseDeleting(e, id); err != nil {
			return err
		}
	}
	dirs, _ := conversationDirs(e)
	file := claude.FindTranscript(dirs, id)
	if file == "" {
		return fmt.Errorf(say("no conversation %s yet (see: baton sessions); it can show in the desktop app after its first message",
			"%s 대화가 아직 없습니다 (baton sessions로 확인). 첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다"), id)
	}
	d := newDesk(e)
	conv := desktop.Conversation{ID: id, Transcript: file}
	v, err := d.Places(conv)
	if err != nil {
		return deskErr(err)
	}
	if !change {
		printPlaces(v)
		return nil
	}
	// Every tick stays as it is, also a locked one: a list the desktop app
	// took over stays ticked, and one deleted in the app since stays deleted
	// (see View.Ticked). Only an account named here is the user's say.
	want := v.Ticked()
	for _, c := range []struct {
		names string
		on    bool
	}{{*show, true}, {*hide, false}} {
		for _, name := range strings.Split(c.names, ",") {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			rows := v.Named(name)
			if len(rows) == 0 {
				return fmt.Errorf(say("no account %q here (see: baton desktop %s)", "%q 계정이 없습니다 (baton desktop %s로 확인)"), name, id)
			}
			if !c.on {
				// Every list of that name it shows in goes, also a former one
				// whose names a current row has now (see View.Hide).
				if out, ok := v.Hide(want, name); ok {
					want = out
					continue
				}
			}
			r := rows[0]
			switch {
			case r.Disabled || r.Key == "":
				return fmt.Errorf("%s: %s", name, placeNote(v, r))
			case r.Locked:
				return fmt.Errorf("%s: %s", name, placeNote(v, r))
			}
			want = slices.DeleteFunc(want, func(k string) bool { return k == r.Key || k == desktop.Kept(r.Key) })
			if c.on {
				want = append(want, r.Key)
			}
		}
	}
	slices.Sort(want)
	// Again just before the change: a delete may have begun meanwhile.
	if err := refuseDeleting(e, id); err != nil {
		return err
	}
	res, err := d.Apply(conv, want)
	if err != nil {
		return deskErr(err)
	}
	fmt.Println(doneLine(res))
	return nil
}

func printPlaces(v desktop.View) {
	if v.Title != "" {
		fmt.Println(v.Title)
	}
	if v.Split {
		fmt.Println(say("  WARNING: baton's conversation file and the desktop app's are no longer one file; baton stops updating it there",
			"  주의: baton의 대화 파일과 데스크톱 앱의 대화 파일이 갈라져 baton은 데스크톱 앱에서 더는 갱신하지 않습니다"))
	}
	if v.Problem != "" {
		fmt.Println("  " + problemText(v.Problem))
	}
	for _, r := range v.Rows {
		mark := "[ ]"
		switch {
		case r.Checked:
			mark = "[x]"
		case r.Disabled:
			mark = "[-]"
		}
		name := strings.Join(r.Accounts, ", ")
		if r.AppOnly {
			name = "desktop"
		}
		if r.Email != "" {
			name += " (" + r.Email + ")"
		}
		fmt.Printf("  %s %-32s %s\n", mark, name, placeNote(v, r))
	}
	if v.AppRunning {
		fmt.Println(say("The desktop app is open: for the account it is signed in to, a change shows after you quit it completely (also from its tray icon) and open it again.",
			"데스크톱 앱이 열려 있습니다. 로그인된 계정에서는 앱을 완전히 종료(트레이 아이콘에서도)했다가 다시 열어야 바뀐 내용이 보입니다."))
	}
	fmt.Println(say("Change it with:  baton desktop <id> --show <accounts>  /  --hide <accounts>",
		"바꾸기:  baton desktop <id> --show <계정>  /  --hide <계정>"))
}

// placeNote says what a row is, the way the window does.
func placeNote(v desktop.View, r desktop.Row) string {
	switch r.Why {
	case "api_key":
		return say("uses an API key, not a Claude sign-in, so the desktop app has no list for it",
			"Claude 로그인이 아닌 API 키를 쓰는 계정이라 데스크톱 앱 목록이 없습니다")
	case "cloud":
		return fmt.Sprintf(say("uses %s, not a Claude sign-in, so the desktop app has no list for it",
			"Claude 로그인이 아닌 %s을(를) 쓰는 계정이라 데스크톱 앱 목록이 없습니다"), cloudName(r.Provider))
	case "token":
		return say("signs in with a long-lived token, so baton cannot tell which Claude account it is",
			"장기 토큰으로 로그인하는 계정이라 baton이 어떤 Claude 계정인지 알 수 없습니다")
	case "gateway":
		return say("signs in through a gateway, not a Claude sign-in, so the desktop app has no list for it",
			"Claude 로그인이 아닌 게이트웨이로 로그인하는 계정이라 데스크톱 앱 목록이 없습니다")
	case "unknown":
		return say("baton does not know which Claude account this is yet; run: baton doctor",
			"baton이 아직 이 계정이 어떤 Claude 계정인지 모릅니다. baton doctor를 실행하세요")
	case "own":
		return say("made in the desktop app on this account, so it stays there (to hide it, archive it in the desktop app)",
			"이 계정의 데스크톱 앱에서 만든 대화라 그대로 둡니다 (숨기려면 데스크톱 앱에서 보관하세요)")
	case "archived":
		return say("archived in the desktop app on this account (unarchive it there to list it again)",
			"이 계정의 데스크톱 앱에서 보관한 대화입니다 (다시 보이게 하려면 데스크톱 앱에서 보관을 해제하세요)")
	case "released":
		return say("you went on with it in the desktop app on this account, so it is the desktop app's now (to hide it, archive it there)",
			"이 계정의 데스크톱 앱에서 이어 가서 이제 데스크톱 앱의 대화입니다 (숨기려면 데스크톱 앱에서 보관하세요)")
	case "deleted":
		return say("you deleted it in the desktop app; --show lists it again", "데스크톱 앱에서 삭제했습니다. --show로 다시 보이게 할 수 있습니다")
	}
	note := say("not used in the desktop app yet: it shows once you sign in with this account there",
		"아직 데스크톱 앱에서 쓴 적이 없는 계정입니다. 데스크톱 앱에서 이 계정으로 로그인하면 보입니다")
	switch {
	case r.Active:
		note = say("signed in to the desktop app now", "지금 데스크톱 앱에 로그인된 계정")
	case r.Used:
		note = say("shows when you switch to this account in the desktop app", "데스크톱 앱에서 이 계정으로 바꾸면 보입니다")
	}
	if r.Copy {
		note += say("; the desktop app keeps the original you opened there, this is baton's copy",
			". 데스크톱 앱에서 연 원래 대화는 그쪽에 남고, 이것은 baton의 사본입니다")
	}
	if len(r.Accounts) > 1 {
		note += fmt.Sprintf(say("; %s are the same Claude account", ". %s은(는) 같은 Claude 계정입니다"), strings.Join(r.Accounts, ", "))
	}
	if r.Former && !r.AppOnly {
		note += say("; no longer one of baton's accounts (--hide takes it off)", ". 더 이상 baton 계정이 아닙니다 (--hide로 뺄 수 있습니다)")
	}
	return note
}

func problemText(code string) string {
	switch code {
	case "conflict":
		return say("your own Claude Code folder already has a different conversation file with this id, so it cannot be shown (baton never replaces that file)",
			"평소 Claude Code 폴더에 같은 id의 다른 대화 파일이 이미 있어 표시할 수 없습니다 (baton은 그 파일을 덮어쓰지 않습니다)")
	case "other_drive":
		return say("this conversation is on another drive than your own Claude Code folder, so the desktop app cannot open it",
			"이 대화가 평소 Claude Code 폴더와 다른 드라이브에 있어 데스크톱 앱이 열 수 없습니다")
	case "no_file":
		return say("it can show in the desktop app after its first message", "첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다")
	}
	return say("baton could not make this conversation openable for the desktop app", "baton이 데스크톱 앱에서 이 대화를 열 수 있게 만들지 못했습니다")
}

func deskErr(err error) error {
	var de *desktop.Error
	if !errors.As(err, &de) {
		return err
	}
	switch de.Code {
	case "no_desktop":
		return errors.New(say("the Claude desktop app was not found on this PC", "이 PC에서 Claude 데스크톱 앱을 찾지 못했습니다"))
	case "split":
		return errors.New(say("baton's conversation file and the desktop app's split, so nothing new can be shown",
			"baton의 대화 파일과 데스크톱 앱의 대화 파일이 갈라져 새로 표시할 수 없습니다"))
	case "unknown_place":
		return errors.New(say("that account cannot be chosen now", "지금은 고를 수 없는 계정입니다"))
	case "deleting":
		// A delete began between refuseDeleting and the change (Desk.Apply
		// checks again under its lock).
		return errors.New(say("this conversation is being deleted; try again in a moment",
			"이 대화를 지우는 중입니다. 잠시 뒤 다시 시도하세요"))
	case "link_failed":
		return fmt.Errorf("%s (%s)", problemText(de.Code), de.Detail)
	}
	return errors.New(problemText(de.Code))
}

// cloudName names the cloud provider an account signs in through.
func cloudName(id string) string {
	switch id {
	case "bedrock":
		return "Amazon Bedrock"
	case "vertex":
		return "Google Vertex AI"
	case "foundry":
		return "Microsoft Foundry"
	}
	return say("a cloud provider", "클라우드 제공자")
}

// doneLine says what changed and when the desktop app shows it, as the
// window's message does: the account the app is signed in to when it opens
// (a running app is opened again), other accounts when the user switches to
// them there. A removal from the running app's own list is finished by
// baton's window once the app is quit.
func doneLine(r desktop.Result) string {
	if len(r.Added) == 0 && len(r.Removed) == 0 {
		return say("Nothing changed.", "바뀐 것이 없습니다.")
	}
	out := []string{say("Done.", "완료했습니다.")}
	switch {
	case r.ActiveAdded && r.AppRunning:
		out = append(out, say("Quit the Claude desktop app (also from its tray icon) and open it again to see the change.",
			"바뀐 내용을 보려면 Claude 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)했다가 다시 여세요."))
	case r.ActiveAdded:
		out = append(out, say("It shows the next time you open the Claude desktop app.", "다음에 Claude 데스크톱 앱을 열면 보입니다."))
	}
	if r.OtherAdded {
		out = append(out, say("It shows when you switch to that account in the desktop app.", "데스크톱 앱에서 그 계정으로 바꾸면 보입니다."))
	}
	switch {
	case r.ActiveRemoved:
		out = append(out, say("It disappears from the desktop app once you quit the app while baton's window is open.",
			"baton 창을 연 채로 데스크톱 앱을 종료하면 목록에서 사라집니다."))
	case len(r.Added) == 0:
		out = append(out, say("It is no longer listed for those accounts.", "그 계정에서는 더 이상 보이지 않습니다."))
	}
	return strings.Join(out, " ")
}

// newDesk is baton's desktop lists for e. With switching the desktop app's
// sign-in on, the sign-ins baton keeps name the app's accounts that are
// none of baton's (Desk.Known), as in the window.
func newDesk(e *env) *desktop.Desk {
	d := desktop.New(e.paths, e.reg)
	if e.cfg.DesktopLoginsOn() {
		d.Known = desklogin.New(e.paths, e.reg).Known
	}
	return d
}

// ---- baton desktop login: switching the desktop app's sign-in -------------

const loginUsage = `baton desktop login [list]                 the sign-ins baton keeps, and the one the app is on
  baton desktop login use <name> [--open]    switch the desktop app to that account (it must be fully quit)
  baton desktop login add [--open]           add an account: keep the app's sign-in, then empty it on this PC
  baton desktop login label <name> <label>   name a kept sign-in ("" takes the name off)
  baton desktop login forget <name> [--yes]  delete a kept sign-in
  baton desktop login forget --all [--yes]   delete every kept sign-in
  baton desktop login recover [--open]       put back a switch that was cut off
  baton desktop login recover --leave-as-is  leave the app as it is instead (it stays on the account it is on)
  baton desktop login tidy                   delete the copies a switch that stopped early left in the app's folder
<name>: a label, a baton account, an email, or the first 8 or more digits of an account id`

// cmdDesktopLogin switches the Claude desktop app's sign-in between the
// user's own accounts, when that is turned on (baton config desktop_logins
// true): what the window's "Claude desktop app sign-in" card does. Nothing
// here runs on its own; only these commands and the window switch.
func cmdDesktopLogin(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := newFlags("desktop login", loginUsage)
	open, assumeYes, all, asIs := new(bool), new(bool), new(bool), new(bool)
	switch sub {
	case "use", "add", "recover":
		open = fs.Bool("open", false, "open the desktop app afterwards")
		if sub == "recover" {
			asIs = fs.Bool("leave-as-is", false, "leave the desktop app as it is now instead of putting the switch back")
		}
	case "forget":
		assumeYes = fs.Bool("yes", false, "do not ask for confirmation")
		all = fs.Bool("all", false, "every sign-in baton keeps")
	case "list", "label", "tidy":
	default:
		return fmt.Errorf("usage: %s", loginUsage)
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if sub != "list" {
		// The model, or a prompt injection, never switches the app's sign-in.
		if err := refuseInsideSession("change the Claude desktop app's sign-in"); err != nil {
			return err
		}
	}
	e, err := load()
	if err != nil {
		return err
	}
	l := desklogin.New(e.paths, e.reg)
	switch sub {
	case "list":
		if len(pos) > 0 {
			return fmt.Errorf("usage: %s", loginUsage)
		}
		return listLogins(l)
	case "use":
		name, err := oneName(pos, "baton desktop login use <name> [--open]")
		if err != nil {
			return err
		}
		acct, err := resolveLogin(l, name)
		if err != nil {
			return err
		}
		res, err := l.Use(acct)
		if err != nil {
			return loginErr(err)
		}
		reportSwitch(l, res)
		opened := settleAndOpen(e, l, *open)
		if opened {
			fmt.Printf(say("The desktop app is now on %s. Opening it.\n", "데스크톱 앱이 이제 %s 계정입니다. 앱을 엽니다.\n"), loginLabel(l, res.To))
		} else {
			fmt.Printf(say("The desktop app is now on %s. Open it to check.\n", "데스크톱 앱이 이제 %s 계정입니다. 앱을 열어 확인하세요.\n"), loginLabel(l, res.To))
		}
		return nil
	case "add":
		if len(pos) > 0 {
			return errors.New("usage: baton desktop login add [--open]")
		}
		res, err := l.Blank()
		if desklogin.Code(err) == desklogin.CodeAlready {
			return errors.New(say("the desktop app is signed out on this PC already; open it and sign in to the account to add",
				"데스크톱 앱이 이미 이 PC에서 로그아웃된 상태입니다. 앱을 열어 추가할 계정으로 로그인하세요"))
		}
		if err != nil {
			return loginErr(err)
		}
		reportSwitch(l, res)
		if res.Kept {
			fmt.Printf(say("baton kept the sign-in of %s and emptied the desktop app's sign-in on this PC only. Nothing was sent to Claude, so that sign-in stays good where baton keeps it.\n",
				"%s의 로그인을 보관한 뒤 이 PC에 있는 데스크톱 앱의 로그인만 비웠습니다. Claude에는 아무것도 보내지 않았으므로 보관한 로그인은 그대로 유지됩니다.\n"), loginLabel(l, res.From))
		} else {
			fmt.Println(say("The desktop app's sign-in on this PC is empty now.", "이 PC에 있는 데스크톱 앱의 로그인을 비웠습니다."))
		}
		if settleAndOpen(e, l, *open) {
			fmt.Println(say("Opening the desktop app.", "데스크톱 앱을 엽니다."))
		}
		fmt.Println(say("Next: in the desktop app, sign in to the other account (an email code), then quit it completely (also from its tray icon). "+
			"Do not use Log out. Then switch between them with: baton desktop login use <name>",
			"다음: 데스크톱 앱에서 다른 계정으로 로그인(이메일 코드)한 뒤 앱을 완전히 종료(트레이 아이콘에서도)하세요. "+
				"로그아웃은 쓰지 마세요. 그다음 baton desktop login use <이름>으로 두 계정 사이를 바꾸세요."))
		return nil
	case "label":
		if len(pos) < 2 {
			return errors.New("usage: baton desktop login label <name> <label>")
		}
		acct, err := resolveLogin(l, pos[0])
		if err != nil {
			return err
		}
		if desktop.ValidID(acct) && !keeps(l, acct) {
			return fmt.Errorf(say("baton keeps no sign-in for %s yet; it can be named once baton keeps it (when you switch away from it)",
				"아직 %s의 로그인을 보관하지 않아 이름을 붙일 수 없습니다 (다른 계정으로 바꿀 때 보관합니다)"), pos[0])
		}
		label := strings.Join(pos[1:], " ")
		if err := l.Rename(acct, label); err != nil {
			return loginErr(err)
		}
		if strings.TrimSpace(label) == "" {
			fmt.Println(say("Took its name off.", "이름을 지웠습니다."))
		} else {
			fmt.Printf(say("It is called %s now.\n", "이제 이름이 %s입니다.\n"), strings.TrimSpace(label))
		}
		return nil
	case "forget":
		if *all {
			if len(pos) > 0 {
				return errors.New("usage: baton desktop login forget --all [--yes]")
			}
			return forgetAllLogins(l, *assumeYes)
		}
		name, err := oneName(pos, "baton desktop login forget <name> [--yes]   or: forget --all [--yes]")
		if err != nil {
			return err
		}
		acct, err := resolveLogin(l, name)
		if err != nil {
			return err
		}
		// A name can mean one of baton's accounts, or the one the app is on,
		// with nothing kept: there is then nothing to ask about.
		if desktop.ValidID(acct) && !keeps(l, acct) {
			return fmt.Errorf(say("baton keeps no sign-in for %s; there is nothing to forget",
				"baton이 보관한 %s의 로그인이 없어 지울 것이 없습니다"), name)
		}
		if desktop.ValidID(acct) {
			a := loginLabel(l, acct)
			fmt.Printf(say("baton deletes the sign-in of %s it keeps. The desktop app is not changed. To switch to %[1]s again, sign in to it in the app (an email code).\n"+
				"This does not sign the account out on Claude's servers. To end that sign-in too, switch the app to it, use Log out there, then forget it here.\n",
				"baton이 보관한 %s의 로그인을 지웁니다. 데스크톱 앱은 바뀌지 않습니다. 다시 %[1]s(으)로 바꾸려면 앱에서 로그인(이메일 코드)하세요.\n"+
					"보관 삭제는 Claude 서버의 로그인을 끝내지 않습니다. 로그인까지 끝내려면 앱을 그 계정으로 바꾼 뒤 앱에서 로그아웃하고, 여기서 보관을 삭제하세요.\n"), a)
		} else {
			fmt.Printf(say("baton deletes %s: what a signed-out desktop app had left (no sign-in token was recorded in it).\n",
				"baton이 %s을(를) 지웁니다. 로그아웃 상태의 데스크톱 앱에 남아 있던 것입니다 (로그인 토큰은 기록되어 있지 않았습니다).\n"), acct)
		}
		if !*assumeYes {
			ok, err := yes(say("Delete it? [y/N] ", "지울까요? [y/N] "))
			if err != nil {
				return fmt.Errorf("%w (or confirm with --yes)", err)
			}
			if !ok {
				return errors.New(say("cancelled", "취소했습니다"))
			}
		}
		if err := l.Drop(acct); err != nil {
			return loginErr(err)
		}
		fmt.Println(say("Deleted.", "지웠습니다."))
		return nil
	case "tidy":
		if len(pos) > 0 {
			return errors.New("usage: baton desktop login tidy")
		}
		n, err := l.Tidy()
		if err != nil {
			return loginErr(err)
		}
		if n == 0 {
			fmt.Println(say("No switch left copies in the desktop app's folder; there is nothing to delete.",
				"데스크톱 앱 폴더에 전환이 남긴 사본이 없어 지울 것이 없습니다."))
			return nil
		}
		fmt.Printf(say("Deleted %d copy(ies) a switch that stopped early had left in the desktop app's folder.\n",
			"중간에 멈춘 전환이 데스크톱 앱 폴더에 남긴 사본 %d개를 지웠습니다.\n"), n)
		return nil
	case "recover":
		if len(pos) > 0 {
			return errors.New("usage: baton desktop login recover [--open] [--leave-as-is]")
		}
		if *asIs {
			ok, err := l.Settle()
			if err != nil {
				return settleErr(err)
			}
			if !ok {
				fmt.Println(say("No switch was cut off; there is nothing to settle.", "끊긴 전환이 없어 정리할 것이 없습니다."))
				return nil
			}
			opened := settleAndOpen(e, l, *open)
			if v, _ := l.View(); v.Active != "" {
				fmt.Printf(say("Left the desktop app as it is: it stays signed in to %s.\n", "데스크톱 앱을 지금 그대로 두었습니다. 앱은 %s 계정에 로그인되어 있습니다.\n"),
					loginLabel(l, v.Active))
			} else {
				fmt.Println(say("Left the desktop app as it is: it is signed out.", "데스크톱 앱을 지금 그대로 두었습니다. 앱은 로그아웃 상태입니다."))
			}
			if opened {
				fmt.Println(say("Opening the desktop app.", "데스크톱 앱을 엽니다."))
			}
			return nil
		}
		ok, err := l.Recover()
		if err != nil {
			return recoverErr(err)
		}
		if !ok {
			fmt.Println(say("No switch was cut off; there is nothing to put back.", "끊긴 전환이 없어 되돌릴 것이 없습니다."))
			return nil
		}
		opened := settleAndOpen(e, l, *open)
		if v, _ := l.View(); v.Active != "" {
			fmt.Printf(say("A switch that was cut off was undone: the desktop app is back on %s.\n", "끊긴 전환을 되돌렸습니다. 데스크톱 앱은 다시 %s 계정입니다.\n"),
				loginLabel(l, v.Active))
		} else {
			fmt.Println(say("A switch that was cut off was undone: the desktop app is signed out again.", "끊긴 전환을 되돌렸습니다. 데스크톱 앱은 다시 로그아웃 상태입니다."))
		}
		if opened {
			fmt.Println(say("Opening the desktop app.", "데스크톱 앱을 엽니다."))
		}
		return nil
	}
	return nil
}

// listLogins shows the sign-ins baton keeps for the desktop app, the one it
// is on first, and what can be done.
func listLogins(l *desklogin.Logins) error {
	v, err := l.View()
	if err != nil {
		return err
	}
	if v.On {
		fmt.Printf(say("Claude desktop app sign-in (switching is on; kept in %s)\n", "Claude 데스크톱 앱 로그인 (전환 켜짐, 보관 위치: %s)\n"), v.Store)
	} else {
		fmt.Println(say("Switching the Claude desktop app's sign-in is off (turn it on with: baton config desktop_logins true).",
			"Claude 데스크톱 앱 로그인 전환이 꺼져 있습니다 (켜려면: baton config desktop_logins true)."))
	}
	if v.Problem != "" {
		fmt.Println("  " + loginText(v.Problem, ""))
	}
	if v.CutOff {
		fmt.Println(say("A switch of the desktop app's sign-in was cut off. Quit the desktop app completely, then put it back as it was: baton desktop login recover",
			"데스크톱 앱 로그인 전환이 중간에 끊겼습니다. 데스크톱 앱을 완전히 종료한 뒤 원래대로 되돌리세요: baton desktop login recover"))
	}
	if v.Leftover > 0 {
		fmt.Printf(say("A switch that stopped early left %d copy(ies) in the desktop app's folder (of the app's settings and a sign-in's files); delete them with: baton desktop login tidy\n",
			"중간에 멈춘 전환이 데스크톱 앱 폴더에 사본 %d개(앱 설정과 로그인 파일의 사본)를 남겼습니다. 지우려면: baton desktop login tidy\n"), v.Leftover)
	}
	if !v.On {
		if v.Kept+len(v.Unnamed) > 0 {
			fmt.Printf(say("baton still keeps this for the desktop app from before: %s (delete it: baton desktop login forget --all).\n",
				"예전에 데스크톱 앱용으로 보관한 것이 아직 남아 있습니다: %s (지우려면: baton desktop login forget --all).\n"), keptText(v.Kept, len(v.Unnamed)))
		}
		return nil
	}
	for _, g := range v.Logins {
		mark := " "
		if g.Active {
			mark = "*"
		}
		var who []string
		if g.Email != "" {
			who = append(who, g.Email)
		}
		if g.OrgName != "" {
			who = append(who, g.OrgName)
		}
		var notes []string
		if g.Active {
			notes = append(notes, say("signed in now", "지금 로그인됨"))
		}
		if len(g.Baton) > 0 {
			notes = append(notes, "baton: "+strings.Join(g.Baton, ", "))
		} else {
			notes = append(notes, say("not one of baton's accounts", "baton 계정 아님"))
		}
		switch {
		case g.Damaged:
			notes = append(notes, say("kept, but not whole: forget it and sign in to it again in the app", "보관했지만 온전하지 않음: 보관을 삭제하고 앱에서 다시 로그인하세요"))
		case g.Kept:
			notes = append(notes, fmt.Sprintf(say("kept %s", "%s 보관"), g.KeptAt.Local().Format("2006-01-02 15:04")))
		default:
			notes = append(notes, say("not kept yet: baton keeps it when you switch away", "아직 보관하지 않음: 다른 계정으로 바꿀 때 보관합니다"))
		}
		id := ""
		if g.Label != g.Account[:8] {
			id = "  [" + g.Account[:8] + "]" // the start of its id names it too
		}
		fmt.Printf("  %s %-20s %s%s\n      %s\n", mark, g.Label, strings.Join(who, " · "), id, strings.Join(notes, "; "))
	}
	for _, u := range v.Unnamed {
		fmt.Printf("    %-20s %s\n", u.Name, fmt.Sprintf(say("what a signed-out desktop app had left (no token recorded), set aside %s",
			"로그아웃 상태의 데스크톱 앱에 남아 있던 것 (토큰 기록 없음), %s 보관"), u.KeptAt.Local().Format("2006-01-02 15:04")))
	}
	if len(v.Logins) == 0 && len(v.Unnamed) == 0 {
		fmt.Println(say("  baton keeps no sign-in yet.", "  아직 보관한 로그인이 없습니다."))
	}
	if v.Running {
		fmt.Println(say("The desktop app is running. Quit it completely (also from its tray icon) to switch.",
			"데스크톱 앱이 실행 중입니다. 바꾸려면 완전히 종료(트레이 아이콘에서도)하세요."))
	}
	if v.Active == "" && v.Problem == "" {
		fmt.Println(say("The desktop app is signed out on this PC. Open it and sign in to the account to add.",
			"데스크톱 앱이 이 PC에서 로그아웃된 상태입니다. 앱을 열어 추가할 계정으로 로그인하세요."))
	}
	fmt.Println(say("Switch:          baton desktop login use <name> [--open]\n"+
		"Add an account:  baton desktop login add [--open]\n"+
		"Rename, forget:  baton desktop login label <name> <label>   /   forget <name> | --all",
		"바꾸기:      baton desktop login use <이름> [--open]\n"+
			"계정 추가:   baton desktop login add [--open]\n"+
			"이름, 삭제:  baton desktop login label <이름> <새 이름>   /   forget <이름> | --all"))
	fmt.Println(say("While this is on, use Log out in the Claude desktop app only for an account you want to stop using: Log out ends that sign-in on Claude's servers, "+
		"so baton's copy of it stops working too (signing in again needs an email code). To add an account, use: baton desktop login add",
		"이 기능을 켠 동안에는, 더 쓰지 않을 계정이 아니라면 Claude 데스크톱 앱에서 로그아웃하지 마세요. 로그아웃하면 Claude 서버에서도 로그인이 끝나 "+
			"baton이 보관한 로그인도 작동하지 않습니다(다시 로그인하려면 이메일 코드가 필요합니다). 계정을 추가하려면: baton desktop login add"))
	return nil
}

// reportSwitch says what a switch did besides the switch itself.
func reportSwitch(l *desklogin.Logins, res desklogin.Switched) {
	if res.Recovered {
		fmt.Println(say("A switch that was cut off was undone first.", "끊긴 전환을 먼저 되돌렸습니다."))
	}
	if res.Stashed != "" {
		fmt.Printf(say("What the signed-out desktop app had left was set aside as %s (no token recorded; delete it with: baton desktop login forget %[1]s).\n",
			"로그아웃 상태의 데스크톱 앱에 남아 있던 것을 %s(으)로 따로 보관했습니다 (토큰 기록 없음. 지우려면: baton desktop login forget %[1]s).\n"), res.Stashed)
	}
	if res.Kept && res.To != "" {
		fmt.Printf(say("baton kept the sign-in the app had (%s).\n", "앱에 있던 로그인(%s)을 보관했습니다.\n"), loginLabel(l, res.From))
	}
}

// settleAndOpen brings baton's desktop lists in line with the account the
// app is on now, before it opens (it reads them as it starts), then opens
// it when asked: whether it did. Opening is a convenience: when baton
// cannot, the user does.
func settleAndOpen(e *env, l *desklogin.Logins, open bool) bool {
	if err := newDesk(e).Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, say("baton: warning: baton's desktop lists could not be brought in line now (%v); baton's window does it when it opens\n",
			"baton: 주의: 지금은 데스크톱 앱 목록을 맞추지 못했습니다 (%v). baton 창을 열면 맞춥니다\n"), err)
	}
	if !open {
		return false
	}
	if err := l.Launch(); err != nil {
		if desklogin.Code(err) == desklogin.CodeSwitchUnrestored {
			fmt.Fprintln(os.Stderr, "baton:", loginErr(err))
		} else {
			// No "baton:" before it: the text names baton already.
			fmt.Fprintln(os.Stderr, say("baton cannot open the desktop app from here; open it yourself.", "baton이 여기서 데스크톱 앱을 열 수 없습니다. 직접 여세요."))
		}
		return false
	}
	return true
}

// loginLabel is what the sign-in of account acct is called.
func loginLabel(l *desklogin.Logins, acct string) string {
	v, _ := l.View()
	return labelOf(v, l.Accounts, acct)
}

// labelOf is what the sign-in of account acct is called: as v lists it, else
// after the first of baton's accounts (accts) that is that Claude account, as
// the list would name it, else by the start of its id.
func labelOf(v desklogin.View, accts []accounts.Account, acct string) string {
	for _, g := range v.Logins {
		if g.Account == acct {
			return g.Label
		}
	}
	var best *accounts.Account
	for i, a := range accts {
		if !strings.EqualFold(a.AccountUUID, acct) {
			continue
		}
		if best == nil || a.Priority > best.Priority || a.Priority == best.Priority && a.ID < best.ID {
			best = &accts[i]
		}
	}
	if best != nil {
		return best.ID
	}
	if len(acct) > 8 {
		return acct[:8]
	}
	return acct
}

// keeps reports whether baton keeps a sign-in for account acct, whole or
// not.
func keeps(l *desklogin.Logins, acct string) bool {
	v, _ := l.View()
	return slices.ContainsFunc(v.Logins, func(g desklogin.Login) bool { return g.Account == acct && g.Kept })
}

// keptText counts what baton keeps for the desktop app: its sign-ins, and
// apart from them what signed-out apps left (no token recorded), so that neither is
// said to be the other.
func keptText(kept, unnamed int) string {
	var parts []string
	if kept > 0 || unnamed == 0 {
		parts = append(parts, fmt.Sprintf(say("%d sign-in(s)", "로그인 %d개"), kept))
	}
	if unnamed > 0 {
		parts = append(parts, fmt.Sprintf(say("%d item(s) a signed-out desktop app left (no token recorded)", "로그아웃 상태의 앱이 남긴 것 %d개(토큰 기록 없음)"), unnamed))
	}
	return strings.Join(parts, say(", and ", ", "))
}

// appLine says how deleting what baton keeps leaves the desktop app: on the
// account it is on, or signed out, when it then asks for a sign-in; never
// both. "" when baton cannot tell (no desktop app it knows here).
func appLine(v desklogin.View, accts []accounts.Account) string {
	switch {
	case v.Active != "":
		return fmt.Sprintf(say("The desktop app stays signed in to %s.", "데스크톱 앱은 %s 계정에 그대로 로그인되어 있습니다."), labelOf(v, accts, v.Active))
	case v.Problem == "":
		return say("The desktop app is signed out now, so it will ask you to sign in.", "데스크톱 앱이 지금 로그아웃 상태라 다음에 열면 로그인을 요청합니다.")
	}
	return ""
}

// resolveLogin is the account (or the leftovers) a name on the command line
// means.
func resolveLogin(l *desklogin.Logins, name string) (string, error) {
	acct, err := l.Resolve(name)
	var de *desktop.Error
	switch {
	case err == nil:
		return acct, nil
	case !errors.As(err, &de):
		return "", err
	case de.Code == desklogin.CodeNotKept:
		return "", fmt.Errorf(say("no account %q among the desktop app's sign-ins (see: baton desktop login)",
			"데스크톱 앱 로그인 중에 %q 계정이 없습니다 (baton desktop login으로 확인)"), name)
	case de.Code == desklogin.CodeAmbiguous:
		return "", fmt.Errorf(say("%q fits more than one account (%s); give more of its name",
			"%q에 맞는 계정이 여러 개입니다 (%s). 더 구체적으로 쓰세요"), name, de.Detail)
	}
	return "", loginErr(err)
}

// forgetAllLogins deletes every sign-in baton keeps for the desktop app,
// after asking. It works while switching is off and while the app runs.
func forgetAllLogins(l *desklogin.Logins, assumeYes bool) error {
	v, err := l.View()
	if err != nil {
		return err
	}
	if v.Kept+len(v.Unnamed) == 0 {
		fmt.Println(say("baton keeps no sign-in of the desktop app.", "baton이 보관한 데스크톱 앱 로그인이 없습니다."))
		return nil
	}
	fmt.Fprintf(os.Stderr, say("baton deletes everything it keeps for the Claude desktop app: %s.\n",
		"baton이 Claude 데스크톱 앱용으로 보관한 것을 모두 지웁니다: %s.\n"), keptText(v.Kept, len(v.Unnamed)))
	if s := appLine(v, l.Accounts); s != "" {
		fmt.Fprintln(os.Stderr, s)
	}
	if !assumeYes {
		ok, err := yes(say("Delete them? [y/N] ", "지울까요? [y/N] "))
		if err != nil {
			return fmt.Errorf("%w (or confirm with --yes)", err)
		}
		if !ok {
			return errors.New(say("cancelled", "취소했습니다"))
		}
	}
	n, err := l.DropAll()
	if err != nil {
		return loginErr(err)
	}
	fmt.Printf(say("Deleted what baton kept: %s.\n", "보관한 것을 지웠습니다: %s.\n"), keptText(n, len(v.Unnamed)))
	return nil
}

// configDesktopLogins is `baton config desktop_logins true|false`: on shows
// the notice and asks, off asks before every kept sign-in is deleted. The
// plain setter does neither (config.ErrDesktopLoginsTurnOn/TurnOff).
func configDesktopLogins(e *env, value string) error {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return errors.New(say("desktop_logins must be true or false", "desktop_logins 값은 true 또는 false여야 합니다"))
	}
	l := desklogin.New(e.paths, e.reg)
	if !on {
		v, err := l.View()
		if err != nil {
			return err
		}
		if v.Kept+len(v.Unnamed) > 0 {
			fmt.Fprintf(os.Stderr, say("Turning this off deletes what baton keeps for the Claude desktop app: %s.\n",
				"끄면 baton이 Claude 데스크톱 앱용으로 보관한 것을 지웁니다: %s.\n"), keptText(v.Kept, len(v.Unnamed)))
			if s := appLine(v, e.reg.Accounts); s != "" {
				fmt.Fprintln(os.Stderr, s)
			}
			ok, err := yes(say("Delete them and turn it off? [y/N] ", "지우고 끌까요? [y/N] "))
			if err != nil {
				// config has no --yes: deleting them first needs no question.
				return fmt.Errorf(say("%w (or first: baton desktop login forget --all --yes)", "%w (또는 먼저: baton desktop login forget --all --yes)"), err)
			}
			if !ok {
				return errors.New(say("desktop_logins not changed", "desktop_logins를 바꾸지 않았습니다"))
			}
		}
		n, err := desklogin.TurnOff(e.paths)
		if err != nil {
			return loginErr(err)
		}
		msg := say("Switching the desktop app's sign-in is off.", "데스크톱 앱 로그인 전환을 껐습니다.")
		if n+len(v.Unnamed) > 0 {
			msg += " " + fmt.Sprintf(say("Deleted what baton kept: %s.", "보관한 것을 지웠습니다: %s."), keptText(n, len(v.Unnamed)))
		}
		fmt.Println(msg)
		if s := appLine(v, e.reg.Accounts); v.Active != "" {
			fmt.Println(s + " " + say("To use another account there again, sign in to it in the app (an email code).",
				"다른 계정을 다시 쓰려면 앱에서 그 계정으로 로그인(이메일 코드)하세요."))
		} else if s != "" {
			fmt.Println(s)
		}
		if v.CutOff {
			fmt.Println(say("A switch that was cut off is still there. Quit the desktop app completely, then run baton desktop login recover --leave-as-is "+
				"(the app stays on the account it is on) or baton desktop login recover (back to before the switch).",
				"끊긴 전환이 아직 남아 있습니다. 데스크톱 앱을 완전히 종료한 뒤 baton desktop login recover --leave-as-is(앱은 지금 계정에 그대로 남음) "+
					"또는 baton desktop login recover(전환 전으로 되돌림)를 실행하세요."))
		}
		return nil
	}
	err = desklogin.TurnOn(e.paths, false)
	if errors.Is(err, config.ErrNeedsDesktopAcknowledgement) {
		fmt.Fprint(os.Stderr, desktopLoginNotice(e.paths.DesktopLogins()))
		ok, aerr := yes(say("Have you read this, do you accept the risk to your accounts, and do you want baton to switch the desktop app's sign-in when you ask? [y/N] ",
			"안내를 읽었고, 계정에 대한 위험을 본인이 지는 데 동의하며, 요청할 때 baton이 데스크톱 앱의 로그인을 바꾸기를 원하십니까? [y/N] "))
		if aerr != nil {
			return aerr
		}
		if !ok {
			return errors.New(say("desktop_logins not changed", "desktop_logins를 바꾸지 않았습니다"))
		}
		err = desklogin.TurnOn(e.paths, true)
	}
	if err != nil {
		return loginErr(err)
	}
	fmt.Println(say("Switching the desktop app's sign-in is on. See and switch with: baton desktop login",
		"데스크톱 앱 로그인 전환을 켰습니다. 보기와 바꾸기: baton desktop login"))
	return nil
}

// desktopLoginNotice is what the user reads before turning switching the
// desktop app's sign-in on; the window shows the same points. store is
// where the sign-ins would be kept.
func desktopLoginNotice(store string) string {
	return fmt.Sprintf(say(`
Before baton switches the Claude desktop app's sign-in, read this:

  * This lets baton switch the Claude desktop app between your own Claude
    accounts without signing in again. It stays off unless you turn it on.
  * How: while the desktop app is fully quit, baton moves the app's sign-in
    (its cookies, its site storage and six entries of its settings file, two
    of them encrypted tokens) into %s
    and puts the chosen account's back. baton only copies and moves these
    files as they are: it never decrypts them and never sends them anywhere.
    The cookies and tokens stay encrypted as the app keeps them, usable only
    from your Windows account on this PC; the site storage is not encrypted,
    in the app either. Some of the app's own preferences kept in its site
    storage follow the account too.
  * This is not an Anthropic feature. No Anthropic source says it is allowed,
    and it is not how the app is meant to be used. Anthropic may treat it,
    especially switching after a usage limit, as getting around its products
    or limits, and can suspend accounts. baton does not hide that one PC uses
    several accounts. The risk to your accounts is yours.
  * It is unofficial and can stop working with any update of the desktop app;
    the app may then ask you to sign in again (an email code). baton changes
    nothing when the app looks different from what it knows.
  * It switches only when you ask, and only while the desktop app is fully
    quit (also from its tray icon). It never switches on its own, and never
    because of a usage limit.
  * Only your own accounts. While this is on, use Log out in the Claude
    desktop app only for an account you want to stop using: Log out ends that
    sign-in on Claude's servers, so baton's copy of it stops working too
    (signing in again needs an email code). To add an account, use
    "Add an account to the desktop app" on baton's Accounts tab
    (or: baton desktop login add).
  * Turning this off, or uninstalling baton, deletes every sign-in baton
    kept. The desktop app stays signed in to the account it is on. This does
    not sign the accounts out on Claude's servers: to end a sign-in too,
    switch the app to it, use Log out there, then forget it in baton.
`, `
baton이 Claude 데스크톱 앱의 로그인을 바꾸기 전에 꼭 읽어 주세요:

  * 이 기능을 켜면 baton이 Claude 데스크톱 앱의 로그인을 내 Claude 계정들
    사이에서 다시 로그인하지 않고 바꿉니다. 직접 켜기 전에는 꺼져 있습니다.
  * 방법: 데스크톱 앱이 완전히 종료된 동안 baton이 앱의 로그인(쿠키, 사이트
    저장소, 설정 파일의 항목 6개 — 그중 2개는 암호화된 토큰)을
    %s
    폴더로 옮겨 보관하고, 고른 계정의 것을 되돌려 놓습니다. baton은 이
    파일들을 있는 그대로 복사하고 옮기기만 하며, 복호화하거나 어디로도 보내지
    않습니다. 쿠키와 토큰은 앱이 저장한 그대로 암호화되어 이 PC의 내 Windows
    계정에서만 쓸 수 있습니다. 사이트 저장소는 앱에서도 암호화되어 있지
    않습니다. 사이트 저장소에 있는 앱 설정 일부도 계정을 따라갑니다.
  * 이것은 Anthropic의 기능이 아닙니다. 이를 허용한다는 Anthropic의 공식
    입장은 없고, 앱이 의도한 사용 방식도 아닙니다. 특히 사용 한도에 걸린 뒤
    계정을 바꾸는 것은 제품이나 한도를 우회하는 것으로 볼 수 있으며,
    Anthropic은 계정을 정지할 수 있습니다. baton은 한 PC에서 여러 계정을
    쓴다는 사실을 숨기지 않습니다. 계정에 대한 위험은 본인이 집니다.
  * 비공식 방식이라 데스크톱 앱이 업데이트되면 언제든 작동하지 않을 수 있고,
    그러면 앱이 다시 로그인(이메일 코드)을 요청할 수 있습니다. 앱이 baton이
    아는 모습과 다르면 baton은 아무것도 바꾸지 않습니다.
  * 내가 요청할 때만, 그리고 데스크톱 앱이 완전히 종료된 동안(트레이
    아이콘에서도)만 바꿉니다. 스스로 바꾸거나 사용 한도 때문에 바꾸는 일은
    없습니다.
  * 본인 소유의 계정만 쓰세요. 이 기능을 켠 동안에는, 더 쓰지 않을 계정이
    아니라면 Claude 데스크톱 앱에서 로그아웃하지 마세요. 로그아웃하면 Claude
    서버에서도 로그인이 끝나 baton이 보관한 로그인도 작동하지 않습니다(다시
    로그인하려면 이메일 코드가 필요합니다). 계정을 추가하려면 baton 계정
    탭의 ‘데스크톱 앱에 계정 추가’를 쓰세요(또는 baton desktop login add).
  * 이 기능을 끄거나 baton을 제거하면 baton이 보관한 로그인은 모두
    지워집니다. 데스크톱 앱은 그때 로그인된 계정에 그대로 남습니다. 보관
    삭제는 Claude 서버의 로그인을 끝내지 않습니다. 로그인까지 끝내려면 앱을
    그 계정으로 바꾼 뒤 앱에서 로그아웃하고, baton에서 보관을 삭제하세요.
`), store)
}

// loginErr says why the desktop app's sign-in was not switched or changed,
// as the window does, with baton's commands where the window has buttons.
// No value of the app's settings is ever in it.
func loginErr(err error) error {
	var de *desktop.Error
	if !errors.As(err, &de) {
		return err
	}
	if msg := loginText(de.Code, de.Detail); msg != "" {
		return errors.New(msg)
	}
	return err
}

// recoverErr is loginErr for a switch that was cut off and Recover would not
// put back: where waiting for the app cannot help the user, it also says how
// to leave the app as it is (Settle). dl_used_since and dl_switch_unrestored
// say so already.
func recoverErr(err error) error {
	msg := loginErr(err)
	var de *desktop.Error
	if errors.As(err, &de) && (de.Code == desklogin.CodeUnsavedCurrent && (de.Detail == "no_account" || strings.HasPrefix(de.Detail, "kept_unreadable:")) ||
		de.Code == desklogin.CodeDamaged && de.Detail == "tokens_of_another") {
		return fmt.Errorf(say("%v\nOr leave the desktop app as it is (it stays on the sign-in it has now): baton desktop login recover --leave-as-is",
			"%v\n또는 데스크톱 앱을 지금 그대로 둘 수 있습니다(앱은 지금 로그인된 계정에 남음): baton desktop login recover --leave-as-is"), msg)
	}
	return msg
}

// settleErr is loginErr for leaving the app as it is (Settle). There
// dl_switch_unrestored is not the switch's own failure, whose text sends
// the user to recover and then to this very command: Settle fails so while
// another program holds one of the app's files (it changes nothing then) or
// a move cannot finish yet, which passes. The window says the same.
func settleErr(err error) error {
	if desklogin.Code(err) == desklogin.CodeSwitchUnrestored {
		return errors.New(say("could not leave the desktop app as it is yet: another program may be using one of the app's files for a moment. "+
			"Do not open the desktop app; try again in a moment",
			"데스크톱 앱을 지금 그대로 두지 못했습니다. 다른 프로그램이 앱의 파일을 잠시 쓰고 있을 수 있습니다. 데스크톱 앱을 열지 말고 잠시 뒤 다시 시도하세요"))
	}
	return loginErr(err)
}

func loginText(code, detail string) string {
	switch code {
	case desklogin.CodeOff:
		return say("switching the desktop app's sign-in is turned off; turn it on with: baton config desktop_logins true",
			"데스크톱 앱 로그인 전환이 꺼져 있습니다. 켜려면: baton config desktop_logins true")
	case desklogin.CodeAppRunning:
		return say("the Claude desktop app is still running; quit it completely (also from its tray icon), then try again",
			"Claude 데스크톱 앱이 아직 실행 중입니다. 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요")
	case desklogin.CodeBusy:
		return say("another change to the desktop app is under way; try again in a moment",
			"데스크톱 앱에 대한 다른 작업이 진행 중입니다. 잠시 뒤 다시 시도하세요")
	case desklogin.CodeNotKept:
		return say("baton keeps no sign-in for that account; sign in to it once in the desktop app (baton desktop login add), then you can switch to it",
			"그 계정의 로그인을 보관하고 있지 않습니다. 데스크톱 앱에서 한 번 로그인하면(baton desktop login add) 바꿀 수 있습니다")
	case desklogin.CodeAmbiguous:
		return fmt.Sprintf(say("that name fits more than one account (%s)", "그 이름에 맞는 계정이 여러 개입니다 (%s)"), detail)
	case desklogin.CodeDamaged:
		if detail == "tokens_of_another" {
			return say("the sign-in the desktop app has now carries the tokens baton keeps for another account, so whose it is cannot be told; nothing was changed. "+
				"Open the desktop app once, quit it completely (also from its tray icon), then try again",
				"데스크톱 앱의 지금 로그인이 baton이 다른 계정용으로 보관한 토큰과 같아 누구의 로그인인지 알 수 없어 아무것도 바꾸지 않았습니다. "+
					"데스크톱 앱을 한 번 열었다가 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요")
		}
		return say("the sign-in baton kept for that account is incomplete, so nothing was changed; forget it and sign in to the account again in the app",
			"보관한 그 계정의 로그인이 온전하지 않아 아무것도 바꾸지 않았습니다. 보관을 삭제하고 앱에서 다시 로그인하세요")
	case desklogin.CodeAlready:
		return say("the desktop app is already signed in to that account", "데스크톱 앱이 이미 그 계정으로 로그인되어 있습니다")
	case desklogin.CodeUnknownFormat:
		msg := say("the desktop app keeps its sign-in in a way this baton does not know (the app may have been updated). "+
			"Nothing was changed, and the app stays signed in to the account it is on. Update baton before switching again",
			"데스크톱 앱이 이 baton이 모르는 방식으로 로그인을 저장하고 있습니다(앱이 업데이트되었을 수 있습니다). "+
				"아무것도 바꾸지 않았고, 앱은 지금 계정에 그대로 로그인되어 있습니다. 다시 바꾸려면 먼저 baton을 업데이트하세요")
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return msg
	case desklogin.CodeUnsupportedLayout:
		return say("switching the sign-in is not supported for this kind of Claude desktop app install yet",
			"이 방식으로 설치된 Claude 데스크톱 앱은 아직 로그인 전환을 지원하지 않습니다")
	case desklogin.CodeUnsavedCurrent:
		if detail == "no_account" {
			return say("the desktop app is signed in but has not said to which account yet, so baton cannot keep that sign-in; nothing was changed. "+
				"Open the desktop app once, wait until it shows your account, quit it completely (also from its tray icon), then try again",
				"데스크톱 앱이 로그인되어 있지만 아직 어느 계정인지 기록하지 않아 그 로그인을 보관할 수 없어 아무것도 바꾸지 않았습니다. "+
					"데스크톱 앱을 한 번 열어 계정이 보일 때까지 기다린 뒤 완전히 종료(트레이 아이콘에서도)하고 다시 시도하세요")
		}
		if detail == "store_permissions" {
			return say("baton could not give the folder where it keeps sign-ins permissions of its own (only you and the system), so it kept nothing and changed nothing",
				"baton이 로그인을 보관하는 폴더에 전용 권한(나와 시스템만)을 줄 수 없어 아무것도 보관하지 않았고 바꾸지도 않았습니다")
		}
		if name, ok := strings.CutPrefix(detail, "kept_unreadable:"); ok {
			// Another account's kept sign-in cannot be read: named, so it can
			// be forgotten when trying again does not help.
			msg := fmt.Sprintf(say("baton could not read the sign-in it keeps for %s, so it cannot tell whose sign-in the desktop app has now; nothing was changed. "+
				"Try again in a moment. If that keeps failing, forget the sign-in kept for %[1]s",
				"baton이 보관한 %s의 로그인을 읽지 못해 데스크톱 앱의 지금 로그인이 누구의 것인지 알 수 없어 아무것도 바꾸지 않았습니다. "+
					"잠시 뒤 다시 시도하세요. 계속 실패하면 보관한 그 로그인을 지우세요"), name)
			if arg, ok := shellArg(name); ok {
				return msg + ": baton desktop login forget " + arg
			}
			return msg + say(": find its id, shown in [ ] on its line in baton desktop login list, then run: baton desktop login forget <id>",
				". baton desktop login list에서 그 로그인 옆 [ ] 안의 ID를 확인한 뒤 baton desktop login forget <ID>를 실행하세요")
		}
		return say("baton could not keep the sign-in the desktop app has now, so nothing was changed",
			"데스크톱 앱의 지금 로그인을 보관하지 못해 아무것도 바꾸지 않았습니다")
	case desklogin.CodeSwitchFailed:
		return say("the switch did not finish, and baton put the desktop app back as it was",
			"전환을 마치지 못해 데스크톱 앱을 원래대로 되돌려 놓았습니다")
	case desklogin.CodeSwitchUnrestored:
		return say("the switch did not finish and baton could not put everything back yet. Do not open the desktop app; run: baton desktop login recover. "+
			"If that keeps failing, you can leave the app as it is: baton desktop login recover --leave-as-is",
			"전환을 마치지 못했고 아직 모두 되돌리지 못했습니다. 데스크톱 앱을 열지 말고 baton desktop login recover를 실행하세요. "+
				"계속 실패하면 앱을 지금 그대로 둘 수 있습니다: baton desktop login recover --leave-as-is")
	case desklogin.CodeUsedSince:
		return say("the desktop app has been used on the sign-in a cut-off switch put in, and baton cannot keep that sign-in now (switching is off, or baton's folder syncs); "+
			"putting the switch back would throw it away, so nothing was changed. To leave the app as it is, quit it completely, then run: baton desktop login recover --leave-as-is",
			"끊긴 전환이 넣어 둔 로그인으로 데스크톱 앱을 이미 사용했는데, 지금은 baton이 그 로그인을 보관할 수 없습니다(전환이 꺼져 있거나 baton 폴더가 동기화됨). "+
				"되돌리면 그 로그인이 사라지므로 아무것도 바꾸지 않았습니다. 앱을 지금 그대로 두려면 앱을 완전히 종료한 뒤 baton desktop login recover --leave-as-is를 실행하세요")
	case desklogin.CodeSyncedStore:
		return say("baton's data folder is in OneDrive or another folder that syncs off this PC, so baton keeps no sign-in there; move it (BATON_HOME) to a folder that stays on this PC",
			"baton 데이터 폴더가 OneDrive처럼 이 PC 밖으로 동기화되는 폴더 안에 있어 로그인을 보관하지 않습니다. 이 PC에만 있는 폴더로 옮기세요 (BATON_HOME)")
	case desklogin.CodeInsideClaude:
		return say("this cannot run from inside Claude Code (CLAUDECODE is set): only you switch the desktop app's sign-in; run it in another terminal",
			"Claude Code 안에서는 실행할 수 없습니다(CLAUDECODE가 설정됨). 데스크톱 앱 로그인 전환은 다른 터미널에서 직접 실행하세요")
	case desklogin.CodeElevated:
		return say("baton is running as administrator; switch the desktop app's sign-in from a terminal that is not",
			"baton이 관리자 권한으로 실행 중입니다. 관리자 권한이 아닌 터미널에서 데스크톱 앱 로그인을 바꾸세요")
	case desklogin.CodeBadLabel:
		switch detail {
		case "too_long":
			return say("a name can be at most 40 characters", "이름은 40자까지 쓸 수 있습니다")
		case "characters":
			return say("a name cannot hold control characters, line separators or commas", "이름에는 제어 문자, 줄 구분 문자, 쉼표를 쓸 수 없습니다")
		case "looks_like_id":
			return say("a name cannot look like an account id (8 or more hex digits)", "이름이 계정 id처럼 보이면 안 됩니다 (16진수 8자리 이상)")
		case "taken":
			return say("another account already goes by that name", "다른 계정이 이미 그 이름을 쓰고 있습니다")
		}
		return say("that name cannot be used", "그 이름은 쓸 수 없습니다")
	case desklogin.CodeLaunchUnsupported:
		return say("baton cannot open the desktop app from here; open it yourself", "baton이 여기서 데스크톱 앱을 열 수 없습니다. 직접 여세요")
	}
	return ""
}

// shellArg is name written so that, pasted into cmd or PowerShell after a
// baton command, baton gets it whole as one argument; false when no one way
// of writing it does that in both. A label may hold almost any character:
// bare, R&D runs "forget R" and then a command D in cmd, and PowerShell
// refuses the line. Letters, digits and . _ + @ - go bare, but not a leading
// @ (PowerShell splats it). Anything else goes in double quotes, which keep
// it whole in both, unless it holds a " (or the “ ” „ PowerShell reads as
// one), a $ or ` (PowerShell expands them inside), a % (cmd does), or ends
// in \ (which escapes the closing quote). A leading - is read as an option
// however it is written.
func shellArg(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, "-") {
		return "", false
	}
	plain := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || strings.ContainsRune("._+@-", r)
	}
	switch {
	case !strings.HasPrefix(name, "@") && !strings.ContainsFunc(name, func(r rune) bool { return !plain(r) }):
		return name, true
	case strings.ContainsAny(name, "\"“”„$`%") || strings.HasSuffix(name, `\`):
		return "", false
	}
	return `"` + name + `"`, true
}
