package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"baton/internal/claude"
	"baton/internal/desktop"
)

// cmdDesktop says in which accounts' Claude desktop app a conversation is
// listed, and changes that (--show, --hide: baton's account names, or
// "desktop" for the account the desktop app is signed in to when it is none
// of baton's). It does what the window's "Choose desktop app accounts" dialog does.
func cmdDesktop(args []string) error {
	const usage = "baton desktop <session-id> [--show <accounts>] [--hide <accounts>]"
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
	dirs, _ := conversationDirs(e)
	file := claude.FindTranscript(dirs, id)
	if file == "" {
		return fmt.Errorf(say("no conversation %s yet (see: baton sessions); it can show in the desktop app after its first message",
			"%s 대화가 아직 없습니다 (baton sessions로 확인). 첫 메시지를 보낸 뒤에 데스크톱 앱에 표시할 수 있습니다"), id)
	}
	d := desktop.New(e.paths, e.reg)
	conv := desktop.Conversation{ID: id, Transcript: file}
	v, err := d.Places(conv)
	if err != nil {
		return deskErr(err)
	}
	if *show == "" && *hide == "" {
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
