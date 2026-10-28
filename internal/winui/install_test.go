package winui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Install into temporary folders and a test registry key: the user's real
// Start menu and installed-apps list are never touched.
func TestInstallAndRemoveEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	uninstallKey = `Software\baton-test-uninstall`
	defer func() { _ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey) }()

	src := filepath.Join(root, "src")
	_ = os.MkdirAll(src, 0o755)
	for _, f := range []string{"baton-app.exe", "baton.exe", "README.md"} {
		_ = os.WriteFile(filepath.Join(src, f), []byte(f), 0o644)
	}
	app, err := Install(src, "9.9.9", "baton.test")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "local", "Programs", "baton", "baton-app.exe"); app != want {
		t.Fatalf("app = %s, want %s", app, want)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(app), "baton.exe")); string(b) != "baton.exe" {
		t.Fatal("baton.exe not copied")
	}
	if fi, err := os.Stat(ShortcutPath()); err != nil || fi.Size() == 0 {
		t.Fatalf("shortcut: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := k.GetStringValue("UninstallString")
	v, _, _ := k.GetStringValue("DisplayVersion")
	k.Close()
	if v != "9.9.9" || u != `"`+filepath.Join(filepath.Dir(app), "baton.exe")+`" uninstall` {
		t.Fatalf("registry: %q %q", v, u)
	}
	if !Installed(app) || Installed(filepath.Join(src, "baton-app.exe")) {
		t.Fatal("Installed() wrong")
	}
	RemoveInstallEntries()
	if _, err := os.Stat(ShortcutPath()); !os.IsNotExist(err) {
		t.Fatal("shortcut left")
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE); err == nil {
		t.Fatal("registry entry left")
	}
	// The finisher deletes only baton's install folder.
	if err := FinishUninstall(src, 0); err == nil {
		t.Fatal("deleted a folder that is not the install folder")
	}
	if err := FinishUninstall(filepath.Dir(app), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(app)); !os.IsNotExist(err) {
		t.Fatal("install folder left")
	}
}

func TestSignInURL(t *testing.T) {
	cases := map[string]string{
		"https://claude.ai/oauth/authorize?a=1&b=2":           "https://claude.ai/oauth/authorize?a=1&b=2",
		"see: https://claude.com/cai/oauth/authorize?x\n=y z": "https://claude.com/cai/oauth/authorize?x=yz",
		"https://evil.example/oauth":                          "",
		"https://claude.ai.evil.example/oauth":                "",
		"http://claude.ai/oauth":                              "",
		"https://claude.ai/new":                               "",
	}
	for in, want := range cases {
		got, ok := SignInURL(in)
		if (want == "") == ok || got != want {
			t.Errorf("%q -> %q %v, want %q", in, got, ok, want)
		}
	}
}

// Updating while a session runs the installed baton.exe: the running file is
// renamed aside instead of overwritten, so the update still completes, and
// the uninstall later removes what is left.
func TestInstallOverARunningProgram(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	uninstallKey = `Software\baton-test-uninstall-running`
	defer func() { _ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey) }()

	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	exe, err := os.ReadFile(cmdExe)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "src")
	_ = os.MkdirAll(src, 0o755)
	write := func(readme string) {
		_ = os.WriteFile(filepath.Join(src, "baton-app.exe"), []byte("app"), 0o644)
		_ = os.WriteFile(filepath.Join(src, "baton.exe"), exe, 0o755)
		_ = os.WriteFile(filepath.Join(src, "README.md"), []byte(readme), 0o644)
	}
	write("v1")
	app, err := Install(src, "1.0.0", "baton.test")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(app)
	running := exec.Command(filepath.Join(dir, "baton.exe"), "/d", "/c", "ping -n 20 127.0.0.1 >nul")
	running.SysProcAttr = HiddenProcAttr()
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Process.Kill(); _, _ = running.Process.Wait() }()

	write("v2")
	if _, err := Install(src, "2.0.0", "baton.test"); err != nil {
		t.Fatalf("update while baton.exe runs: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "v2" {
		t.Fatalf("README not updated: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "baton.exe.old")); err != nil {
		t.Fatalf("the running program was not set aside: %v", err)
	}
	for _, n := range []string{"baton-app.exe.new", "baton.exe.new", "README.md.new"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Fatalf("%s left", n)
		}
	}
	// A second update while that program still runs as baton.exe.old.
	write("v3")
	if _, err := Install(src, "3.0.0", "baton.test"); err != nil {
		t.Fatalf("second update while the set-aside program runs: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "v3" {
		t.Fatalf("README not updated: %q", b)
	}
	_ = running.Process.Kill()
	_, _ = running.Process.Wait()
	time.Sleep(300 * time.Millisecond)
	if err := FinishUninstall(dir, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("install folder left")
	}
}
