package notify

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Registry keys under HKEY_CURRENT_USER (vars for tests).
var (
	appIDKey  = `Software\Classes\AppUserModelId\` + appID
	schemeKey = `Software\Classes\` + Scheme
)

// Register makes Windows show baton's toasts under its name and icon (a PNG
// file, if there is one) and makes a click on one run app with the link:
// app focus "baton-session:<run id>". Without app, toasts are not clickable.
// Values already in place are left alone, so this is cheap to call before
// every session.
func Register(app, icon string) error {
	if os.Getenv(testFileEnv) != "" {
		return nil // the end-to-end tests leave the user's registry alone
	}
	vals := map[string]string{"DisplayName": "baton"}
	if icon != "" {
		vals["IconUri"] = icon
	}
	if err := setValues(appIDKey, vals); err != nil {
		return err
	}
	if app == "" {
		return nil
	}
	if err := setValues(schemeKey, map[string]string{"": "URL:baton session", "URL Protocol": ""}); err != nil {
		return err
	}
	if err := setValues(schemeKey+`\DefaultIcon`, map[string]string{"": `"` + app + `",0`}); err != nil {
		return err
	}
	return setValues(schemeKey+`\shell\open\command`, map[string]string{"": `"` + app + `" focus "%1"`})
}

// Unregister removes what Register wrote (baton is being uninstalled).
func Unregister() {
	if os.Getenv(testFileEnv) != "" {
		return
	}
	deleteTree(appIDKey)
	deleteTree(schemeKey)
}

// Registered reports whether a click on a toast opens app.
func Registered(app string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, schemeKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return err == nil && strings.EqualFold(v, `"`+app+`" focus "%1"`)
}

func setValues(path string, vals map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range vals {
		if cur, _, err := k.GetStringValue(name); err == nil && cur == v {
			continue
		}
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	return nil
}

// deleteTree deletes a key with everything under it (registry.DeleteKey
// only takes keys without subkeys).
func deleteTree(path string) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS); err == nil {
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, s := range subs {
			deleteTree(path + `\` + s)
		}
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, path)
}

// Prepare registers baton for notifications (see Register) with its icon,
// which it saves in dataDir.
func Prepare(dataDir, app string) error {
	icon := filepath.Join(dataDir, "icon.png")
	if err := writeIcon(icon); err != nil {
		icon = "" // a build without baton's resources (tests): Windows' default icon
	}
	return Register(app, icon)
}

// iconID is the 64-pixel PNG that tools/winres puts in baton's resources.
const iconID = 7

// writeIcon saves the program's own icon to path, unless that file already
// holds it.
func writeIcon(path string) error {
	res, err := windows.FindResource(0, windows.ResourceID(iconID), windows.RT_ICON)
	if err != nil {
		return err
	}
	data, err := windows.LoadResourceData(0, res)
	if err != nil {
		return err
	}
	if len(data) < 8 || string(data[1:4]) != "PNG" {
		return errors.New("the program's icon is not a PNG")
	}
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
