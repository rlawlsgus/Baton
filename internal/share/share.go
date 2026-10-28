// Package share makes every account profile look like the user's normal
// Claude Code setup, so a session feels the same on every account:
//
//   - agents, commands, skills and output-styles are junctions to ~/.claude;
//   - settings.json, CLAUDE.md and keybindings.json are copied one way.
//
// Copies are three-way safe: a file is only (re)written while the profile's
// copy is still exactly what baton wrote last time, so edits made inside a
// profile are never overwritten. Credentials, plugins and MCP state are not
// shared.
package share

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"baton/internal/envx"
	"baton/internal/fsx"
)

var (
	LinkDirs  = []string{"agents", "commands", "skills", "output-styles"}
	CopyFiles = []string{"settings.json", "CLAUDE.md", "keybindings.json"}
)

const manifestName = ".baton-share.json"

// settings.json keys that would replace the profile's own login.
var authSettingKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "gcpAuthRefresh"}

// UserHome is the folder of the user's normal Claude Code setup. A
// CLAUDE_CONFIG_DIR that points into baton's own profiles folder (as it does
// for every process inside a supervised session) is not the normal setup.
func UserHome(profilesDir string) string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" && os.Getenv("BATON_RUN") == "" &&
		(profilesDir == "" || !fsx.IsWithin(d, profilesDir)) {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

type Result struct {
	Linked []string // new junctions
	Copied []string // files written
	Kept   []string // the profile has its own version; left alone
}

type manifest struct {
	Files map[string]string `json:"files"` // name -> sha256 of what baton last wrote
}

// Sync brings the profile in line with home. Call it only while no claude
// process uses the profile.
func Sync(home, profile string) (Result, error) {
	var res Result
	if home == "" || samePath(home, profile) {
		return res, nil
	}
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		return res, nil // no normal setup to share
	}
	for _, d := range LinkDirs {
		src, dst := filepath.Join(home, d), filepath.Join(profile, d)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			t := fsx.JunctionTarget(dst)
			switch {
			case t != "" && samePath(t, src):
				continue
			case t != "" && fsx.IsWithin(t, filepath.Dir(profile)):
				// Linked to a sibling profile by mistake (it would dangle once
				// that account is removed): relink it to the real setup.
				if err := os.Remove(dst); err != nil {
					return res, err
				}
			default:
				res.Kept = append(res.Kept, d)
				continue
			}
		}
		if err := fsx.CreateJunction(dst, src); err != nil {
			return res, err
		}
		res.Linked = append(res.Linked, d)
	}

	mpath := filepath.Join(profile, manifestName)
	var m manifest
	if err := fsx.ReadJSON(mpath, &m); err != nil && !errors.Is(err, os.ErrNotExist) {
		m = manifest{}
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	changed := false
	for _, name := range CopyFiles {
		raw, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			continue
		}
		want := raw
		if name == "settings.json" {
			if want, err = sanitizeSettings(raw); err != nil {
				res.Kept = append(res.Kept, name+" (not valid JSON)")
				continue
			}
		}
		wantSum := sum(want)
		dst := filepath.Join(profile, name)
		have, err := os.ReadFile(dst)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return res, err
		case sum(have) == wantSum:
			if m.Files[name] != wantSum {
				m.Files[name], changed = wantSum, true
			}
			continue
		case m.Files[name] != sum(have):
			res.Kept = append(res.Kept, name)
			continue
		}
		if err := fsx.WriteFileAtomic(dst, want, 0o600); err != nil {
			return res, err
		}
		m.Files[name], changed = wantSum, true
		res.Copied = append(res.Copied, name)
	}
	if changed {
		if err := fsx.WriteJSONAtomic(mpath, m); err != nil {
			return res, err
		}
	}
	return res, nil
}

// sanitizeSettings drops settings that would make claude use something other
// than the profile's own login (API key helpers, provider/credential env vars).
func sanitizeSettings(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(fsx.StripBOM(raw)))
	dec.UseNumber()
	var s map[string]any
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	dirty := false
	for _, k := range authSettingKeys {
		if _, ok := s[k]; ok {
			delete(s, k)
			dirty = true
		}
	}
	if env, ok := s["env"].(map[string]any); ok {
		for k := range env {
			if envx.Scrubbed(k) {
				delete(env, k)
				dirty = true
			}
		}
	}
	if !dirty {
		return raw, nil
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func samePath(a, b string) bool {
	ca, err1 := filepath.Abs(a)
	cb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}
