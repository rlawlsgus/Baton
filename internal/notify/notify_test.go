package notify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestToastXML(t *testing.T) {
	got := toastXML(Note{Title: `baton · a<b>`, Body: `"x" & 'y'`, Launch: "baton-session:20261029-010203-abcdef"})
	want := `<toast activationType="protocol" launch="baton-session:20261029-010203-abcdef"><visual><binding template="ToastGeneric">` +
		`<text>baton · a&lt;b&gt;</text><text>&#34;x&#34; &amp; &#39;y&#39;</text></binding></visual></toast>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := toastXML(Note{Title: "t", Body: "b"}); strings.Contains(got, "launch") {
		t.Fatalf("a toast without a link has one: %s", got)
	}
}

// The end-to-end tests read toasts from a file instead of the screen.
func TestShowToTestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.jsonl")
	t.Setenv(testFileEnv, p)
	if err := Show(Note{Title: "T", Body: "B", Tag: "done", Group: "run"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil || got["title"] != "T" || got["body"] != "B" || got["tag"] != "done" {
		t.Fatalf("%s %v", data, err)
	}
}

func useTestKeys(t *testing.T) {
	appIDKey, schemeKey = `Software\baton-test-notify\appid`, `Software\baton-test-notify\scheme`
	t.Cleanup(func() {
		deleteTree(`Software\baton-test-notify`)
		appIDKey, schemeKey = `Software\Classes\AppUserModelId\`+appID, `Software\Classes\`+Scheme
	})
}

func TestRegister(t *testing.T) {
	useTestKeys(t)
	app := `C:\Program Files\baton\baton-app.exe`
	if err := Register(app, `C:\x\icon.png`); err != nil {
		t.Fatal(err)
	}
	if !Registered(app) || Registered(`C:\other\baton-app.exe`) {
		t.Fatal("Registered is wrong")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, appIDKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	name, _, _ := k.GetStringValue("DisplayName")
	icon, _, _ := k.GetStringValue("IconUri")
	k.Close()
	if name != "baton" || icon != `C:\x\icon.png` {
		t.Fatalf("app id values: %q %q", name, icon)
	}
	if err := Register(app, ""); err != nil { // again: nothing to change
		t.Fatal(err)
	}
	Unregister()
	if Registered(app) {
		t.Fatal("still registered")
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, appIDKey, registry.QUERY_VALUE); err == nil {
		t.Fatal("app id key left")
	}
}

// A real toast, on the screen of whoever runs it:
//
//	BATON_LIVE_TOAST=1 go test ./internal/notify -run Live -v
//
// Windows' own notification history then has to list it.
func TestLiveToast(t *testing.T) {
	if os.Getenv("BATON_LIVE_TOAST") == "" {
		t.Skip("shows a real notification; set BATON_LIVE_TOAST=1")
	}
	appID = "baton.livetest"
	appIDKey, schemeKey = `Software\Classes\AppUserModelId\baton.livetest`, `Software\baton-test-notify\scheme`
	defer func() {
		Unregister()
		appID, appIDKey, schemeKey = AppID, `Software\Classes\AppUserModelId\`+AppID, `Software\Classes\`+Scheme
	}()
	if err := Register("", ""); err != nil {
		t.Fatal(err)
	}
	if err := Show(Note{Title: "baton test", Body: "This is how baton's notifications look.", Tag: "test", Group: "live"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	ps := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null; ` +
		`[Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory('baton.livetest') | ForEach-Object { $_.Tag + '/' + $_.Group }`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	t.Logf("history: %s", out)
	if err != nil || !strings.Contains(string(out), "test/live") {
		t.Fatalf("the toast is not in Windows' notification history (%v)", err)
	}
}
