package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"baton/internal/claude"
	"baton/internal/envx"
	"baton/internal/fsx"
	"baton/internal/run"
	"baton/internal/share"
	"baton/internal/winui"

	"golang.org/x/sys/windows"
)

// cmdUninstall removes baton for the current user: the conversations that
// started on baton's accounts are moved into the user's own Claude Code,
// every account is signed out through Claude Code, baton's data folder is
// deleted, and so are the Start menu entry, the installed-apps entry and the
// install folder.
func cmdUninstall(args []string) error {
	fs := newFlags("uninstall", "baton uninstall [--yes]")
	assumeYes := fs.Bool("yes", false, "do not ask for confirmation")
	waitPID := fs.Int("wait-pid", 0, "wait for this process (the baton window) to close first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := refuseInsideSession("uninstall baton"); err != nil {
		return err
	}
	if *waitPID > 0 {
		waitForExit(*waitPID, 30*time.Second)
	}
	e, err := load()
	if err != nil {
		return err
	}
	if active := run.Active(e.paths.RunsDir(), supervisorAlive); len(active) > 0 {
		return errors.New(say("end the running sessions first (see: baton status)", "실행 중인 세션을 먼저 끝내세요"))
	}
	for _, a := range e.reg.Accounts {
		if claude.BusyProfile(a.ProfileDir) {
			return fmt.Errorf(say("%q is in use by a running Claude Code; quit it first", "%q 계정을 쓰는 Claude Code가 실행 중입니다. 먼저 끝내세요"), a.ID)
		}
	}
	// Only a folder that is clearly baton's own is deleted: BATON_HOME could
	// point anywhere.
	deleteData := strings.EqualFold(filepath.Base(e.paths.Root), "baton")
	home := share.UserHome(e.paths.ProfilesDir())
	conversations := 0
	for _, a := range e.reg.Accounts {
		if claude.ProfileHasTranscripts(a.ProfileDir) {
			conversations++
		}
	}
	fmt.Fprintf(os.Stderr, say("This signs out %d account(s) and deletes %s.\n", "계정 %d개를 로그아웃하고 %s 폴더를 지웁니다.\n"), len(e.reg.Accounts), e.paths.Root)
	if conversations > 0 {
		fmt.Fprintf(os.Stderr, say("Conversations that started on %d account(s) are moved to your own Claude Code (%s) first; claude --resume finds them there.\n",
			"계정 %d개에서 시작한 대화는 먼저 평소 Claude Code 폴더(%s)로 옮겨 보존합니다. claude --resume으로 다시 열 수 있습니다.\n"), conversations, orDash(home))
	}
	fmt.Fprintln(os.Stderr, say("Your Claude accounts, your own Claude Code setup and the Claude desktop app's sessions are not affected.",
		"Claude 계정 자체와 평소 쓰던 Claude Code 설정, Claude 데스크톱 앱의 세션은 그대로입니다."))
	if !*assumeYes {
		ok, err := yes(say("Uninstall baton? [y/N] ", "baton을 제거할까요? [y/N] "))
		if err != nil {
			return fmt.Errorf("%w (or confirm with --yes)", err)
		}
		if !ok {
			return errors.New(say("cancelled", "취소했습니다"))
		}
	}
	// Keep the user's work before anything is deleted: a conversation that
	// cannot be moved stops the uninstall.
	moved := 0
	for _, a := range e.reg.Accounts {
		n, err := share.KeepConversations(a.ProfileDir, home, a.ID)
		moved += n
		if err != nil {
			return fmt.Errorf(say("could not move the conversations of %q to your own Claude Code (%v); nothing was deleted",
				"%q 계정의 대화를 평소 Claude Code 폴더로 옮기지 못했습니다 (%v). 아무것도 지우지 않았습니다"), a.ID, err)
		}
	}
	if moved > 0 {
		fmt.Fprintf(os.Stderr, say("baton: moved %d conversation(s) to %s\n", "baton: 대화 %d개를 %s 폴더로 옮겼습니다\n"), moved, filepath.Join(home, "projects"))
	}
	if inst, err := e.resolveClaude(); err == nil {
		for _, a := range e.reg.Accounts {
			pe, _ := envx.ForProfile(a.ProfileDir, nil)
			if _, err := claude.RunInteractive(inst.Path, []string{"auth", "logout"}, pe, a.ProfileDir); err != nil {
				fmt.Fprintf(os.Stderr, "baton: warning: logout of %q failed: %v\n", a.ID, err)
			}
		}
	}
	leftovers := false
	if deleteData {
		if err := fsx.RemoveTreeSafe(e.paths.Root); err != nil {
			leftovers = true
			fmt.Fprintf(os.Stderr, say("baton: some files in %s could not be deleted (%v); delete the folder by hand\n",
				"baton: %s 폴더의 일부 파일을 지우지 못했습니다 (%v). 직접 지우세요\n"), e.paths.Root, err)
		}
	} else {
		fmt.Fprintf(os.Stderr, say("baton: %s is not named baton, so it was left; delete it by hand\n",
			"baton: %s 폴더는 이름이 baton이 아니라 지우지 않았습니다. 직접 지우세요\n"), e.paths.Root)
	}
	winui.RemoveInstallEntries()
	if self, err := os.Executable(); err == nil && winui.Installed(self) {
		// The install folder holds this very program: a copy outside it
		// deletes the folder once this process has ended.
		helper := filepath.Join(os.TempDir(), fmt.Sprintf("baton-uninstall-%d.exe", os.Getpid()))
		err := copyExe(self, helper)
		if err == nil {
			err = winui.StartDetached(helper, "uninstall-finish", filepath.Dir(self), strconv.Itoa(os.Getpid()))
		}
		if err != nil {
			leftovers = true
			fmt.Fprintf(os.Stderr, say("baton: %s could not be removed (%v); delete it by hand once this window is closed\n",
				"baton: %s 폴더를 지우지 못했습니다 (%v). 이 창을 닫은 뒤 직접 지우세요\n"), filepath.Dir(self), err)
		} else if leftovers {
			fmt.Fprintf(os.Stderr, say("baton: the program folder %s goes once this window is closed\n",
				"baton: 프로그램 폴더 %s 는 이 창을 닫으면 지워집니다\n"), filepath.Dir(self))
		}
	} else if dir := winui.InstallDir(); dir != "" {
		// Run from another copy (the unzipped folder): the installed one
		// goes too, since its Start menu and installed-apps entries are gone.
		if _, err := os.Stat(filepath.Join(dir, "baton.exe")); err == nil {
			if err := winui.FinishUninstall(dir, 0); err != nil {
				leftovers = true
				fmt.Fprintf(os.Stderr, say("baton: %s could not be removed (%v); delete it by hand\n",
					"baton: %s 폴더를 지우지 못했습니다 (%v). 직접 지우세요\n"), dir, err)
			}
		}
	}
	if leftovers {
		// An error keeps the window open, so the message above can be read.
		return errors.New(say("baton has been removed, but some files are left (see above)",
			"baton을 제거했지만 일부 파일이 남았습니다 (위 안내 참고)"))
	}
	fmt.Fprintln(os.Stderr, say("baton has been removed.", "baton을 제거했습니다."))
	return nil
}

// cmdUninstallFinish is the helper's job: delete the install folder.
func cmdUninstallFinish(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: baton uninstall-finish <dir> <pid>")
	}
	pid, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	err = winui.FinishUninstall(args[0], pid)
	if self, serr := os.Executable(); serr == nil && strings.HasPrefix(strings.ToLower(filepath.Base(self)), "baton-uninstall-") {
		_ = winui.DeleteAfterExit(self) // this helper is a copy in %TEMP%
	}
	return err
}

func waitForExit(pid int, d time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(d/time.Millisecond))
}

func copyExe(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}
