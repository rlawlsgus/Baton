package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"baton/internal/convdel"
	"baton/internal/run"
	"baton/internal/share"
)

// refuseDeleting stops a start on a conversation while a delete of it runs
// (here or in the baton window): it would resume files on their way to the
// Recycle Bin, or begin a new file under the id being deleted.
func refuseDeleting(e *env, ids ...string) error {
	for _, id := range ids {
		if run.Deleting(e.paths.Root, id) {
			return fmt.Errorf(say("conversation %s is being deleted", "%s 대화를 삭제하는 중입니다"), strings.ToLower(id))
		}
	}
	return nil
}

// sessionOf is the conversation id in a transcript's file name.
func sessionOf(transcript string) string {
	return strings.TrimSuffix(filepath.Base(transcript), filepath.Ext(transcript))
}

const deleteUsage = "baton sessions delete <id> [--dry-run] [--yes]"

// cmdSessionsDelete moves a conversation, and what Claude Code and baton
// keep by its id, to the Recycle Bin, as the window's "Delete conversation"
// does. It says first what goes and what stays, and asks.
func cmdSessionsDelete(args []string) error {
	fs := newFlags("sessions delete", deleteUsage)
	dry := fs.Bool("dry-run", false, "only say what would go to the Recycle Bin; change nothing")
	assumeYes := fs.Bool("yes", false, "do not ask for confirmation")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := oneName(pos, deleteUsage)
	if err != nil {
		return err
	}
	if !sessionIDRx.MatchString(id) {
		return errors.New(say("a session id looks like 1b2c3d4e-...; see: baton sessions", "세션 id는 1b2c3d4e-... 형식입니다. baton sessions로 확인하세요"))
	}
	id = strings.ToLower(id)
	// Even a look tidies baton's desktop lists (see convdel.Look).
	if err := refuseInsideSession("delete conversations"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	req := deleteRequest(e, id)
	plan, err := convdel.Look(req)
	if err != nil {
		return deleteErr(id, err)
	}
	printDeletePlan(plan)
	if !plan.OK {
		return blockerErr(id, plan.Blockers[0])
	}
	if *dry {
		fmt.Println(say("Nothing was changed (--dry-run).", "아무것도 바꾸지 않았습니다 (--dry-run)."))
		return nil
	}
	if !*assumeYes {
		ok, err := yes(say("Move it to the Recycle Bin? [y/N] ", "휴지통으로 옮길까요? [y/N] "))
		if err != nil {
			return fmt.Errorf("%w (or confirm with --yes)", err)
		}
		if !ok {
			return errors.New(say("cancelled", "취소했습니다"))
		}
	}
	res, err := convdel.Delete(req)
	if err != nil {
		return deleteErr(id, err)
	}
	fmt.Printf(say("Moved the conversation to the Recycle Bin (%d item(s)). You can restore it from there.\n",
		"대화를 휴지통으로 옮겼습니다 (%d개). 휴지통에서 되살릴 수 있습니다.\n"), res.Moved)
	if len(res.DesktopOff) > 0 {
		fmt.Printf(say("It no longer shows in the Claude desktop app for: %s.\n", "Claude 데스크톱 앱의 다음 계정 목록에서도 뺐습니다: %s.\n"), deskLabels(res.DesktopOff))
	}
	if res.Warning == convdel.WarnReopened {
		fmt.Println(say("Something opened it again while it went, so a new file of it may be there now (see: baton sessions).",
			"옮기는 동안 무언가가 이 대화를 다시 열어 새 파일이 생겼을 수 있습니다 (baton sessions로 확인)."))
	}
	return nil
}

// deleteRequest is a delete of conversation id from every folder baton
// knows: the accounts' profiles, then the user's own Claude Code folder.
func deleteRequest(e *env, id string) convdel.Request {
	var dirs []convdel.Dir
	for _, a := range e.reg.Accounts {
		dirs = append(dirs, convdel.Dir{Where: a.ID, Path: a.ProfileDir})
	}
	if home := share.UserHome(e.paths.ProfilesDir()); home != "" {
		dirs = append(dirs, convdel.Dir{Where: "claude-code", Path: home})
	}
	return convdel.Request{ID: id, Paths: e.paths, Dirs: dirs, Desk: newDesk(e), Alive: supervisorAlive}
}

// printDeletePlan says what a delete moves to the Recycle Bin and what it
// leaves, as the window's dialog does.
func printDeletePlan(p convdel.Plan) {
	// A file in a linked folder, or in one that can't be read, is not among
	// what the plan would move: the conversation is there all the same (the
	// lists show it), and baton leaves it, so it is no leftover and nothing
	// is counted as going. The window's dialog decides the same way (leftover
	// in internal/gui).
	linked := slices.ContainsFunc(p.Blockers, func(b convdel.Blocker) bool { return b.Code == convdel.CodeLinkedFolder })
	leftover := !linked // only what a delete that stopped half way left
	for _, path := range p.Paths() {
		if strings.EqualFold(filepath.Base(path), p.SessionID+".jsonl") {
			leftover = false
		}
	}
	switch {
	case leftover:
		fmt.Printf(say("Conversation %s: only what an earlier delete left behind is still there; it goes too.\n",
			"%s 대화: 앞서 삭제하다 남은 것만 있습니다. 이것도 함께 옮깁니다.\n"), p.SessionID)
	default:
		title := p.Title
		if title == "" {
			title = say("(no title)", "(제목 없음)")
		}
		fmt.Printf("%s  %s\n", p.SessionID, title)
		if p.Cwd != "" {
			fmt.Printf(say("    in %s\n", "    폴더: %s\n"), p.Cwd)
		}
		// With no title, folder or source baton read nothing of the
		// conversation (its file behind a link it could not read), so it
		// does not know where it was made either: MadeIn stays "outside"
		// when no name of the file was seen.
		if p.MadeIn != "baton" && (p.Title != "" || p.Cwd != "" || p.Source != "") {
			fmt.Printf(say("It was made outside baton (%s); it goes from there too.\n",
				"baton 밖(%s)에서 만든 대화입니다. 그곳 목록에서도 사라집니다.\n"), sourceName(p.Source))
		}
		if len(p.Files) > 1 {
			fmt.Printf(say("It went on apart in two places, so it has %d different files; all of them go:\n",
				"두 곳에서 따로 이어 가서 이 대화의 파일이 서로 다른 %d개입니다. 모두 옮깁니다:\n"), len(p.Files))
			for _, f := range p.Files {
				var where []string
				for _, w := range f.Where {
					if w == "claude-code" {
						where = append(where, say("your usual Claude Code", "평소 Claude Code"))
					} else {
						where = append(where, fmt.Sprintf(say("account %s", "%s 계정"), w))
					}
				}
				fmt.Printf(say("    %s: changed %s, %s\n", "    %s: %s에 바뀜, %s\n"), strings.Join(where, ", "),
					f.Modified.Local().Format("2006-01-02 15:04"), sizeText(f.Bytes))
			}
		}
	}
	fmt.Println(say("baton moves the conversation and what Claude Code saved with it (subagents, saved tool outputs, /rewind checkpoints) "+
		"to the Recycle Bin. It disappears from baton, from Claude Code's resume list and from the office. You can restore it from the Recycle Bin.",
		"대화와 Claude Code가 함께 저장한 것(서브에이전트, 저장된 도구 출력, /rewind 체크포인트)을 휴지통으로 옮깁니다. "+
			"baton, Claude Code의 이어 가기 목록, 사무실에서 사라집니다. 휴지통에서 되살릴 수 있습니다."))
	if !linked {
		c := p.Items
		what := fmt.Sprintf(say("%d file(s), %d folder(s), %s", "파일 %d개, 폴더 %d개, %s"), c.Files, c.Folders, sizeText(c.Bytes))
		if c.Runs > 0 {
			what += fmt.Sprintf(say(", and the records of %d baton session(s) that ran it", ", 이 대화를 실행한 baton 세션 기록 %d개"), c.Runs)
		}
		fmt.Printf(say("  To the Recycle Bin: %s.\n", "  휴지통으로: %s.\n"), what)
	}
	if len(p.DesktopOff) > 0 {
		fmt.Printf(say("  baton also takes it off the Claude desktop app for: %s.\n", "  Claude 데스크톱 앱의 다음 계정 목록에서도 뺍니다: %s.\n"), deskLabels(p.DesktopOff))
	}
	for _, n := range p.Notes {
		switch n.Code {
		case convdel.NoteCopies:
			fmt.Printf(say("  %d other conversation(s) went on from this one. They keep everything said, but can no longer open the large tool outputs saved with this one.\n",
				"  이 대화에서 이어 간 대화가 %d개 있습니다. 내용은 그대로지만, 이 대화와 함께 저장된 큰 도구 출력은 더 이상 열 수 없습니다.\n"), n.N)
		case convdel.NoteNotFullyChecked:
			fmt.Println(say("  baton could not read through every conversation next to it, so others may use its saved tool outputs too.",
				"  옆의 대화를 모두 읽어 보지는 못해, 다른 대화도 이 대화의 저장된 도구 출력을 쓰고 있을 수 있습니다."))
		case convdel.NoteLinksKept:
			fmt.Printf(say("  %d link(s) or junction(s) stand where something of it would be; they, and what they point at, stay.\n",
				"  이 대화의 항목 자리에 링크나 정션이 %d개 있습니다. 링크와 그 대상은 그대로 둡니다.\n"), n.N)
		}
	}
	fmt.Println(say("  Your project files, the folder's memory, Claude Code's prompt history and your other conversations stay as they are.",
		"  프로젝트 파일, 폴더 메모리, Claude Code 입력 기록, 다른 대화는 그대로 둡니다."))
}

// deskLabels names the desktop app's lists a conversation leaves: baton's
// account names, "" being the app's own account.
func deskLabels(labels []string) string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l
		if l == "" {
			out[i] = say("the desktop app's own account (not one of baton's)", "baton 계정이 아닌 데스크톱 앱 계정")
		}
	}
	return strings.Join(out, ", ")
}

// sizeText is a size the way Explorer shows it, in the user's language:
// Korean Windows writes KB, MB and GB as English does, but bytes as a word
// of its own (73바이트).
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf(say("%d bytes", "%d바이트"), n)
}

// deleteErr says why a delete did not happen, or only in part.
func deleteErr(id string, err error) error {
	var e *convdel.Error
	if !errors.As(err, &e) {
		return err
	}
	var msg error
	switch {
	case e.Code == convdel.CodePartly:
		msg = fmt.Errorf(say("only part of it went to the Recycle Bin (%d moved, %d left); run the same command again to move the rest",
			"일부만 휴지통으로 옮겼습니다 (%d개 옮김, %d개 남음). 같은 명령을 다시 실행하면 나머지를 옮깁니다"), e.Moved, e.Left)
	case len(e.Blockers) > 0:
		msg = blockerErr(id, e.Blockers[0])
	default:
		msg = blockerErr(id, convdel.Blocker{Code: e.Code, Drive: e.Drive})
	}
	if len(e.DesktopOff) > 0 {
		msg = fmt.Errorf(say("%v\nIt no longer shows in the Claude desktop app for: %s.", "%v\nClaude 데스크톱 앱의 다음 계정 목록에서는 이미 뺐습니다: %s."),
			msg, deskLabels(e.DesktopOff))
	}
	return msg
}

// blockerErr says what keeps conversation id from being deleted now, and
// what to do about it.
func blockerErr(id string, b convdel.Blocker) error {
	switch b.Code {
	case convdel.CodeGone:
		return fmt.Errorf(say("no conversation %s (see: baton sessions)", "%s 대화가 없습니다 (baton sessions로 확인)"), id)
	case convdel.CodeBadSession:
		return errors.New(say("a session id looks like 1b2c3d4e-...; see: baton sessions", "세션 id는 1b2c3d4e-... 형식입니다. baton sessions로 확인하세요"))
	case convdel.CodeDeleting:
		return errors.New(say("another delete of this conversation is running; try again in a moment",
			"이 대화를 지우는 작업이 이미 진행 중입니다. 잠시 뒤 다시 시도하세요"))
	case convdel.CodeInBaton:
		if b.RunID != "" {
			return fmt.Errorf(say("it is running in baton (session %s); end that session first (baton stop --run %[1]s)",
				"baton에서 실행 중입니다 (세션 %s). 먼저 그 세션을 끝내세요 (baton stop --run %[1]s)"), b.RunID)
		}
		return errors.New(say("it is running in baton; end that session first", "baton에서 실행 중입니다. 먼저 그 세션을 끝내세요"))
	case convdel.CodeCopyStarting:
		return errors.New(say("a copy of it is starting in baton; try again after the copy's first message",
			"이 대화의 사본이 baton에서 시작하는 중입니다. 사본에 첫 메시지를 보낸 뒤 다시 시도하세요"))
	case convdel.CodeSessionWindow:
		if b.RunID != "" {
			return fmt.Errorf(say("a baton session that had it has ended, but its window (session %s) is still open; close that window, then try again",
				"이 대화를 쓰던 baton 세션(%s)은 끝났지만 그 창이 아직 열려 있습니다. 그 창을 닫은 뒤 다시 시도하세요"), b.RunID)
		}
		return errors.New(say("a baton session that had it has ended, but its window is still open; close that window, then try again",
			"이 대화를 쓰던 baton 세션은 끝났지만 그 창이 아직 열려 있습니다. 그 창을 닫은 뒤 다시 시도하세요"))
	case convdel.CodeOpenElsewhere:
		if b.Holder == "claude-desktop" {
			return errors.New(say("it is open in the Claude desktop app; quit the desktop app completely (also from its tray icon) first",
				"Claude 데스크톱 앱에서 열려 있습니다. 먼저 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)하세요"))
		}
		return fmt.Errorf(say("it is open in %s; close it there first", "%s에서 열려 있습니다. 먼저 닫으세요"), holderName(b.Holder))
	case convdel.CodeCannotCheck:
		return errors.New(say("baton couldn't check whether it is open right now; try again in a moment",
			"지금 열려 있는지 확인하지 못했습니다. 잠시 뒤 다시 시도하세요"))
	case convdel.CodeOtherName:
		return errors.New(say("its file has another name outside Claude Code's folders, which would keep it; baton leaves it as it is",
			"이 대화 파일에 Claude Code 폴더 밖의 다른 이름이 있어 지워도 남게 되므로 그대로 둡니다"))
	case convdel.CodeLinkedFolder:
		return errors.New(say("its file is in a folder reached through a link or junction, or in one baton can't read, so baton leaves it as it is",
			"대화 파일이 링크(정션)로 연결된 폴더나 baton이 읽을 수 없는 폴더에 있어 그대로 둡니다"))
	case convdel.CodeDesktopApp:
		if b.Detail == "prior" {
			return errors.New(say("the Claude desktop app keeps it as an earlier part of one of its sessions; delete that session in the desktop app first",
				"Claude 데스크톱 앱이 자기 세션의 앞부분으로 갖고 있습니다. 데스크톱 앱에서 그 세션을 먼저 삭제하세요"))
		}
		return errors.New(say("the Claude desktop app has it as its own session; delete it in the desktop app",
			"Claude 데스크톱 앱이 자기 세션으로 갖고 있습니다. 데스크톱 앱에서 삭제하세요"))
	case convdel.CodeDesktopBusy, convdel.CodeDesktopWaiting:
		return errors.New(say("the Claude desktop app has a list open that shows it; quit the desktop app completely (also from its tray icon), then try again",
			"이 대화를 표시하는 목록을 Claude 데스크톱 앱이 열어 두고 있습니다. 데스크톱 앱을 완전히 종료(트레이 아이콘에서도)한 뒤 다시 시도하세요"))
	case convdel.CodeNoRecycleBin:
		if b.Drive != "" {
			return fmt.Errorf(say("Windows can't put it in the Recycle Bin (%s), so nothing was deleted", "Windows가 휴지통에 넣을 수 없어(%s) 아무것도 지우지 않았습니다"), b.Drive)
		}
		return errors.New(say("Windows can't put it in the Recycle Bin, so nothing was deleted", "Windows가 휴지통에 넣을 수 없어 아무것도 지우지 않았습니다"))
	case convdel.CodePathTooLong:
		return errors.New(say("one of its paths is too long for the Recycle Bin, so nothing was deleted",
			"경로가 너무 길어 휴지통에 넣을 수 없는 항목이 있어 아무것도 지우지 않았습니다"))
	case convdel.CodeInUse:
		return errors.New(say("another program is using one of its files; try again in a moment",
			"다른 프로그램이 파일을 쓰고 있습니다. 잠시 뒤 다시 시도하세요"))
	case convdel.CodeRecycleFailed:
		return errors.New(say("Windows could not move it to the Recycle Bin, so nothing was deleted; try again",
			"Windows가 휴지통으로 옮기지 못해 아무것도 지우지 않았습니다. 다시 시도하세요"))
	case convdel.CodeChanged:
		return errors.New(say("its files moved while it was being deleted, so it is still there; run the same command again",
			"삭제하는 동안 파일이 옮겨져 대화가 아직 남아 있습니다. 같은 명령을 다시 실행하세요"))
	}
	return fmt.Errorf(say("baton could not delete it (%s)", "삭제하지 못했습니다 (%s)"), b.Code)
}
