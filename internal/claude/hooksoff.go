package claude

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"baton/internal/fsx"
)

// HooksBlocked returns why claude would ignore baton's hooks in this profile
// and folder, or "" if nothing known blocks them. Without hooks baton cannot
// see usage limits, so the caller should warn before starting.
func HooksBlocked(profileDir, cwd string) string {
	type source struct {
		name    string
		data    []byte
		managed bool
	}
	var srcs []source
	addFile := func(path string, managed bool) {
		if data, err := os.ReadFile(path); err == nil {
			srcs = append(srcs, source{path, data, managed})
		}
	}
	addFile(filepath.Join(profileDir, "settings.json"), false)
	addFile(filepath.Join(cwd, ".claude", "settings.json"), false)
	addFile(filepath.Join(cwd, ".claude", "settings.local.json"), false)
	if root, _ := gitRoots(cwd); root != "" && root != cwd {
		addFile(filepath.Join(root, ".claude", "settings.json"), false)
		addFile(filepath.Join(root, ".claude", "settings.local.json"), false)
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		addFile(filepath.Join(pf, "ClaudeCode", "managed-settings.json"), true)
		m, _ := filepath.Glob(filepath.Join(pf, "ClaudeCode", "managed-settings.d", "*.json"))
		for _, p := range m {
			addFile(p, true)
		}
	}
	// Group Policy: managed settings as JSON in a "Settings" registry value.
	for _, root := range []struct {
		key  registry.Key
		name string
	}{{registry.LOCAL_MACHINE, `HKLM`}, {registry.CURRENT_USER, `HKCU`}} {
		k, err := registry.OpenKey(root.key, `SOFTWARE\Policies\ClaudeCode`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		if v, _, err := k.GetStringValue("Settings"); err == nil {
			srcs = append(srcs, source{root.name + `\SOFTWARE\Policies\ClaudeCode`, []byte(v), true})
		}
		k.Close()
	}
	for _, s := range srcs {
		var m map[string]any
		if json.Unmarshal(fsx.StripBOM(s.data), &m) != nil {
			continue
		}
		if v, _ := m["disableAllHooks"].(bool); v {
			return s.name + " sets disableAllHooks"
		}
		if !s.managed {
			continue
		}
		if v, _ := m["allowManagedHooksOnly"].(bool); v {
			return s.name + " sets allowManagedHooksOnly"
		}
		if pluginOnlyHooks(m["strictPluginOnlyCustomization"]) {
			return s.name + " allows hooks only from plugins (strictPluginOnlyCustomization)"
		}
	}
	return ""
}

// pluginOnlyHooks: strictPluginOnlyCustomization is either true (everything,
// hooks included, must come from plugins) or a list of restricted kinds.
func pluginOnlyHooks(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case []any:
		for _, e := range x {
			if s, _ := e.(string); s == "hooks" {
				return true
			}
		}
	}
	return false
}
