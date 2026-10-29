package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"baton/internal/app"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/envx"
	"baton/internal/gui"
	"baton/internal/update"
	"baton/internal/webview2"
	"baton/internal/winui"
)

// guiBuild is "1" in baton-app.exe, the same program built as a Windows GUI
// application (no console): -ldflags "-H windowsgui -X main.guiBuild=1".
var guiBuild string

const appExeName = "baton-app.exe"

// cmdGUI runs baton's window: a local web UI in a WebView2 window of baton's
// own, or in the default browser when WebView2 is not available.
func cmdGUI(args []string) error {
	fs := newFlags("gui", "baton gui [--browser]")
	browser := fs.Bool("browser", false, "open in the default browser instead of baton's window")
	noWindow, testSpawn := guiTestFlags(fs)
	afterPID := fs.Int("after-pid", 0, "wait for this process (the window being replaced) to close first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *afterPID > 0 {
		waitForExit(*afterPID, 15*time.Second)
	}
	// The window, COM and the folder dialog all live on this one thread.
	runtime.LockOSThread()
	if err := refuseInsideSession("open the baton window"); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	inst, first, err := winui.ClaimInstance(e.paths.Root)
	if err != nil {
		return err
	}
	if !first {
		return nil // the running GUI was asked to show a window
	}
	defer inst.Close()
	winui.EnableDPIAwareness()

	cli, err := cliExe()
	if err != nil {
		return err
	}
	lang := winui.UserLang()
	jobs := &winui.Jobs{}
	defer jobs.CloseAll() // hidden commands still running end with the window
	var window atomic.Pointer[webview2.Window]
	// quit ends the GUI: it closes baton's window, or in a browser ends the
	// loop that keeps the server up.
	quitCh := make(chan struct{})
	var quitOnce sync.Once
	quit := func() {
		if w := window.Load(); w != nil {
			w.Close()
		}
		quitOnce.Do(func() { close(quitCh) })
	}
	srv := gui.New(gui.Options{
		Paths:   e.paths,
		Spawner: exeSpawner{exe: cli, test: *testSpawn, jobs: jobs, lang: func() string { return uiLang(e.paths.Config(), lang) }},
		Version: version,
		Lang:    lang,
		Claude:  func() gui.ClaudeInfo { return claudeInfo(e.paths.Config()) },
		PickFolder: func() (string, error) {
			title := "Choose the project folder"
			if c, err := config.Load(e.paths.Config()); err == nil && (c.GUILanguage == "ko" || c.GUILanguage == "" && lang == "ko") {
				title = "작업할 폴더 선택"
			}
			w := window.Load()
			if w == nil {
				return winui.PickFolder(title)
			}
			// Modal to baton's window, on its thread.
			var p string
			var err error
			w.Call(func() { p, err = winui.PickFolderOwned(w.HWND(), title) })
			return p, err
		},
		Open:      winui.OpenFolder,
		Clipboard: winui.ClipboardText,
		OpenPrivate: func(u string) error {
			return winui.OpenPrivate(u, filepath.Join(e.paths.Root, "tmp"))
		},
		CheckUpdate: func(ctx context.Context) (update.Release, error) { return update.Latest(ctx, userAgent()) },
		Update:      installUpdate,
		Install: func() (string, error) {
			self, err := os.Executable()
			if err != nil {
				return "", err
			}
			return winui.Install(filepath.Dir(self), version, appUserModelID)
		},
		Relaunch: func(app string) error {
			if err := winui.StartDetached(app, "--after-pid", strconv.Itoa(os.Getpid())); err != nil {
				guiLog("could not open the installed baton: %v", err)
				return err
			}
			quit()
			return nil
		},
		Uninstall: func() error {
			// The window must close first: its data (WebView2) is in the
			// folder the uninstall deletes. The uninstall must not run in
			// the install folder either, or that folder cannot be removed.
			_, err := winui.StartConsole(func() *exec.Cmd {
				cmd := exec.Command(cli, "uninstall", "--wait-pid", strconv.Itoa(os.Getpid()))
				cmd.Dir = os.TempDir()
				cmd.Env = append(childEnv(true), "BATON_LANG="+uiLang(e.paths.Config(), lang))
				return cmd
			})
			if err != nil {
				guiLog("could not open the uninstall window: %v", err)
				return err
			}
			quit()
			return nil
		},
		Installed: func() (bool, bool) {
			self, err := os.Executable()
			if err != nil {
				return false, false
			}
			_, appErr := os.Stat(filepath.Join(filepath.Dir(self), appExeName))
			return winui.Installed(self), !winui.Installed(self) && appErr == nil
		},
	})
	winui.CleanSignInProfiles(filepath.Join(e.paths.Root, "tmp"))
	ln, err := srv.Listen()
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = hs.Shutdown(ctx)
	}()
	srv.StartCheck() // refresh every account's sign-in status
	shows := inst.ShowRequests()

	if !*noWindow && !*browser && webview2.Available() {
		webview2.SetAppID(appUserModelID)
		// Show requests go to the window while it runs, and to the browser
		// loop below if it could not start.
		wvDone := make(chan struct{})
		go func() {
			for {
				select {
				case <-shows:
					if w := window.Load(); w != nil {
						w.Restore()
					}
				case <-wvDone:
					return
				}
			}
		}()
		err := webview2.Run(webview2.Options{
			Title:       "baton",
			URL:         srv.WindowURL(),
			Origin:      srv.Origin(),
			UserDataDir: filepath.Join(e.paths.Root, "webview2"),
			Width:       1040,
			Height:      760,
			IconID:      appIconID,
			OpenOutside: func(u string) { _ = winui.OpenURL(u) },
			Log:         guiLog,
		}, func(w *webview2.Window) { window.Store(w) })
		close(wvDone)
		if err == nil || window.Load() != nil {
			return err // the window was shown, and is closed now
		}
		guiLog("WebView2 could not start (%v); using the default browser", err)
	}

	open := func() {
		url := srv.WindowURL()
		if *noWindow {
			fmt.Println(url)
			return
		}
		_ = winui.OpenURL(url)
	}
	open()
	if guiBuild != "1" && !*noWindow {
		fmt.Fprintln(os.Stderr, "baton is open in your browser. Close that tab (or press Ctrl+C here) to quit.")
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-interrupt:
			return nil
		case <-quitCh:
			return nil
		case <-shows:
			open()
		case <-tick.C:
			// In a browser baton cannot watch the tab: the page says when it
			// closes, and a long silence (a closed browser) ends it too.
			if srv.Closed() || srv.Idle() > 5*time.Minute {
				return nil
			}
		}
	}
}

const (
	appUserModelID = "baton.gui"
	appIconID      = 1 // resource id of the icon in rsrc_windows_*.syso
)

// unzipFirst explains what to do when baton-app.exe was opened straight from
// inside the zip (Explorer runs it from a temporary copy without baton.exe).
func unzipFirst() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(self)
	if _, err := os.Stat(filepath.Join(dir, "baton.exe")); err == nil {
		return ""
	}
	tmp := strings.ToLower(filepath.Clean(os.TempDir()))
	if strings.HasPrefix(strings.ToLower(dir), tmp) || strings.Contains(strings.ToLower(dir), ".zip") {
		return "압축을 먼저 푸세요: zip 파일에서 마우스 오른쪽 버튼 → 모두 압축 풀기, 그다음 풀린 폴더의 baton-app.exe를 여세요.\n\n" +
			"Extract the zip first: right-click it → Extract All, then open baton-app.exe in the extracted folder."
	}
	return ""
}

// guiLog notes what the GUI did (in baton-app.exe it goes to logs\gui.log).
func guiLog(format string, a ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("2006-01-02 15:04:05.000 ")+format+"\n", a...)
}

// openGUILog points baton-app.exe's output and crash reports at
// <data folder>\logs\gui.log (the previous one is kept as gui.log.1).
func openGUILog() string {
	p, err := app.DefaultPaths()
	if err != nil {
		return ""
	}
	dir := filepath.Join(p.Root, "logs")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	path := filepath.Join(dir, "gui.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ""
	}
	os.Stdout, os.Stderr = f, f
	log.SetOutput(f) // net/http reports handler panics through the log package
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	guiLog("baton-app %s started", version)
	return path
}

// cliExe is the console baton.exe the GUI runs for every change: this
// program itself, or baton.exe next to baton-app.exe.
func cliExe() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if guiBuild != "1" {
		return self, nil
	}
	p := filepath.Join(filepath.Dir(self), "baton.exe")
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return "", fmt.Errorf("baton.exe must be in the same folder as %s (%s)", appExeName, filepath.Dir(self))
	}
	return p, nil
}

func claudeInfo(configPath string) gui.ClaudeInfo {
	cfg, err := config.Load(configPath)
	if err != nil {
		return gui.ClaudeInfo{Error: err.Error()}
	}
	base, _ := envx.Build(os.Environ(), nil)
	inst, skipped, err := claude.ResolveUsable(cfg.ClaudePath, cfg.DesktopBundleAllowed(), base)
	info := gui.ClaudeInfo{Path: inst.Path, Source: inst.Source, Version: inst.Version, DesktopApp: claude.DesktopAppInstalled(),
		BundleOff: cfg.ClaudePath == "" && !cfg.DesktopBundleAllowed() && claude.DesktopBundle() != ""}
	if err != nil {
		var old *claude.TooOldError
		info.Error, info.TooOld = err.Error(), errors.As(err, &old)
		return info
	}
	info.Found = true
	info.Warning, _ = claude.CheckVersion(inst.Version)
	for _, s := range skipped {
		if inst.Source == "desktop" && s.Version != "" {
			info.OldCLI = s.Version // an old CLI the desktop app's copy is used instead of
			break
		}
	}
	return info
}

// exeSpawner runs baton.exe for the GUI.
type exeSpawner struct {
	exe  string
	test bool          // testing: sign-ins run hidden, sessions are only printed
	jobs *winui.Jobs   // hidden commands
	lang func() string // the window's language, passed on to the windows it opens
}

// uiLang is the window's language: the setting, else Windows'.
func uiLang(configPath, windowsLang string) string {
	if c, err := config.Load(configPath); err == nil && c.GUILanguage != "" {
		return c.GUILanguage
	}
	return windowsLang
}

func (x exeSpawner) Console(dir string, args []string) (func() int, error) {
	if x.test {
		if len(args) > 0 && args[0] == "run" {
			fmt.Printf("test-spawn: %q in %s\n", args, dir)
			return func() int { return 0 }, nil
		}
		cmd := exec.Command(x.exe, args...)
		cmd.Dir, cmd.Env, cmd.SysProcAttr = dir, childEnv(false), winui.HiddenProcAttr()
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return waiter(cmd), nil
	}
	lang := x.lang()
	cmd, err := winui.StartConsole(func() *exec.Cmd {
		cmd := exec.Command(x.exe, args...)
		cmd.Dir, cmd.Env = dir, append(childEnv(true), "BATON_LANG="+lang)
		return cmd
	})
	if err != nil {
		return nil, err
	}
	return waiter(cmd), nil
}

func (x exeSpawner) Hidden(args []string) (string, int, error) {
	timeout := 2 * time.Minute
	if len(args) > 0 && args[0] == "doctor" {
		timeout = 10 * time.Minute // asks claude about every account
	}
	// Its messages reach the window as errors, in the window's language.
	return x.jobs.Run(x.exe, args, append(childEnv(false), "BATON_LANG="+x.lang()), timeout)
}

func waiter(cmd *exec.Cmd) func() int {
	return func() int {
		err := cmd.Wait()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			return -1
		}
		return 0
	}
}

// childEnv is the GUI's environment for baton.exe; newConsole makes it adopt
// the console window it was started in.
func childEnv(newConsole bool) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, newConsoleEnv) || strings.EqualFold(k, "BATON_RUN") || strings.EqualFold(k, "BATON_LANG") {
			continue
		}
		env = append(env, kv)
	}
	if newConsole {
		env = append(env, newConsoleEnv+"=1")
	}
	return env
}

// startApp starts baton-app.exe next to this baton.exe, if it is there.
func startApp() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	p := filepath.Join(filepath.Dir(self), appExeName)
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return false
	}
	cmd := exec.Command(p)
	cmd.Env = childEnv(false)
	if r := os.Getenv("BATON_RUN"); r != "" {
		cmd.Env = append(cmd.Env, "BATON_RUN="+r) // the window refuses inside a session too
	}
	return cmd.Start() == nil
}
