package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests' notifications go to a file, never to the screen (nor do they
// register anything with Windows).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "baton-supervisor-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("BATON_NOTIFY_FILE", filepath.Join(dir, "notifications.jsonl"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
