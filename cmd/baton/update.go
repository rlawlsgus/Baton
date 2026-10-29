package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"baton/internal/run"
	"baton/internal/update"
	"baton/internal/winui"
)

func userAgent() string { return "baton/" + version }

// installUpdate downloads a release, checks it against its checksums.txt and
// installs it like "Install to the Start menu". It returns the installed
// baton-app.exe.
func installUpdate(ctx context.Context, rel update.Release) (string, error) {
	dir, err := os.MkdirTemp("", "baton-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	folder, err := update.Download(ctx, rel, dir, userAgent())
	if err != nil {
		return "", err
	}
	return winui.Install(folder, rel.Version, appUserModelID)
}

// cmdUpdate installs the newest baton from GitHub.
func cmdUpdate(args []string) error {
	fs := newFlags("update", "baton update [--check]")
	check := fs.Bool("check", false, "only say whether a newer baton is out")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: baton update [--check]")
	}
	if err := refuseInsideSession("update baton"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rel, err := update.Latest(ctx, userAgent())
	if err != nil {
		return fmt.Errorf(say("could not check for updates: %v", "업데이트를 확인하지 못했습니다: %v"), err)
	}
	if update.Compare(rel.Version, version) <= 0 {
		fmt.Printf(say("baton %s is the newest version.\n", "baton %s이(가) 최신 버전입니다.\n"), version)
		return nil
	}
	fmt.Printf(say("baton %s is out (you have %s): %s\n", "baton %s이(가) 나왔습니다 (지금 %s): %s\n"), rel.Version, version, rel.Page)
	if notes := strings.TrimSpace(rel.Notes); notes != "" {
		fmt.Println()
		fmt.Println(firstLines(notes, 12))
		fmt.Println()
	}
	if *check {
		fmt.Println(say("Install it with: baton update", "설치하려면: baton update"))
		return nil
	}
	e, err := load()
	if err != nil {
		return err
	}
	if len(run.Active(e.paths.RunsDir(), supervisorAlive)) > 0 {
		return errors.New(say("end the running sessions first (they use baton.exe)", "실행 중인 세션을 먼저 끝내세요 (세션이 baton.exe를 쓰고 있습니다)"))
	}
	fmt.Fprintf(os.Stderr, say("Downloading %s...\n", "%s 내려받는 중...\n"), rel.ZipName)
	app, err := installUpdate(ctx, rel)
	if err != nil {
		return err
	}
	fmt.Printf(say("Installed baton %s (%s). Open baton from the Start menu; a baton window open now keeps the old version until you open it again.\n",
		"baton %s을(를) 설치했습니다 (%s). 시작 메뉴에서 baton을 여세요. 지금 열려 있는 baton 창은 다시 열 때까지 이전 버전입니다.\n"), rel.Version, app)
	return nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}
