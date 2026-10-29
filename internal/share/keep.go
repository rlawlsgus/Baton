package share

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"baton/internal/fsx"
)

// KeptDirs are the parts of an account profile that hold the user's own work:
// the conversations (projects\<folder>\<session>.jsonl, with each session's
// subagent and tool-result folder and the folder's memory) and the /rewind
// checkpoints (file-history).
var KeptDirs = []string{"projects", "file-history"}

// KeepConversations moves the conversations kept in an account's profile into
// the user's own Claude Code setup (userHome, normally %USERPROFILE%\.claude),
// before baton deletes the profile: there Claude Code finds them again
// (`claude --resume`). Nothing in userHome is overwritten: a file that is
// already there under the same name stays, and the profile's copy is moved
// next to it as "<name> (baton <account>)<ext>" unless the two are identical.
// Links are not followed. It returns how many conversations it moved.
func KeepConversations(profileDir, userHome, account string) (int, error) {
	if userHome == "" {
		return 0, errors.New("the folder of your own Claude Code setup is unknown")
	}
	conversations, _ := filepath.Glob(filepath.Join(profileDir, "projects", "*", "*.jsonl"))
	for _, d := range KeptDirs {
		if err := mergeMove(filepath.Join(profileDir, d), filepath.Join(userHome, d), account); err != nil {
			return 0, fmt.Errorf("moving %s to %s: %w", filepath.Join(profileDir, d), filepath.Join(userHome, d), err)
		}
	}
	return len(conversations), nil
}

// mergeMove moves the tree src into dst: a folder dst does not have yet in
// one rename, into one it has file by file.
func mergeMove(src, dst, account string) error {
	fi, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fsx.IsReparsePoint(fi) || !fi.IsDir() {
		return nil // a link baton made, or not what Claude Code keeps here
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		info, err := os.Lstat(from)
		if err != nil {
			return err
		}
		switch {
		case fsx.IsReparsePoint(info):
			continue
		case info.IsDir():
			if _, err := os.Lstat(to); errors.Is(err, os.ErrNotExist) && os.Rename(from, to) == nil {
				continue
			}
			if err := mergeMove(from, to, account); err != nil {
				return err
			}
		default:
			if err := moveFile(from, to, account); err != nil {
				return err
			}
		}
	}
	return nil
}

// moveFile moves one file; when to exists it keeps both (or drops the
// source when the two are identical).
func moveFile(from, to, account string) error {
	if _, err := os.Lstat(to); err == nil {
		if same, _ := sameContent(from, to); same {
			return os.Remove(from)
		}
		ext := filepath.Ext(to)
		base := strings.TrimSuffix(to, ext)
		for i := 1; ; i++ {
			alt := fmt.Sprintf("%s (baton %s)%s", base, account, ext)
			if i > 1 {
				alt = fmt.Sprintf("%s (baton %s %d)%s", base, account, i, ext)
			}
			if _, err := os.Lstat(alt); errors.Is(err, os.ErrNotExist) {
				to = alt
				break
			}
		}
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	// Another drive: copy, then remove.
	if err := copyFile(from, to); err != nil {
		_ = os.Remove(to)
		return err
	}
	return os.Remove(from)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameContent(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil || fa.Size() != fb.Size() {
		return false, err
	}
	da, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return string(da) == string(db), nil
}
