package envx

import (
	"strings"
	"testing"
)

// What Claude Code needs to paste a picture (Alt+V runs Windows PowerShell
// from PATH and saves the image under its temp folder) reaches it.
func TestBuildKeepsPasteEnv(t *testing.T) {
	base := []string{`PATH=C:\Windows\System32\WindowsPowerShell\v1.0`, `SystemRoot=C:\Windows`, `windir=C:\Windows`,
		`TEMP=C:\t`, `TMP=C:\t`, `PSModulePath=C:\ps`, `CLAUDE_CODE_TMPDIR=C:\cc`, `WT_SESSION=1`}
	got, _ := Build(base, nil)
	joined := strings.Join(got, "\n")
	for _, kv := range base {
		if !strings.Contains(joined, kv) {
			t.Errorf("%q did not reach claude", kv)
		}
	}
}
