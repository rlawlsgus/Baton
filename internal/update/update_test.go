package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fakeGitHub serves a releases list and the files it names.
func fakeGitHub(t *testing.T, zips map[string][]byte, sums func(name string, data []byte) string) *httptest.Server {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	arch := runtime.GOARCH
	asset := func(name string) map[string]string {
		return map[string]string{"name": name, "browser_download_url": srv.URL + "/dl/" + name}
	}
	release := func(tag string, draft bool, names ...string) map[string]any {
		var as []map[string]string
		for _, n := range names {
			as = append(as, asset(n))
		}
		return map[string]any{"tag_name": tag, "draft": draft, "body": "notes for " + tag, "html_url": "https://example.com/" + tag, "assets": as}
	}
	list := []map[string]any{
		release("v0.6.0", true, "baton_0.6.0_windows_"+arch+".zip", "checksums.txt"), // a draft
		release("v0.5.1", false, "baton_0.5.1_windows_other.zip", "checksums.txt"),   // not for this PC
		release("v0.5.0", false, "baton_0.5.0_windows_"+arch+".zip", "checksums.txt"),
		release("v0.4.1", false, "baton_0.4.1_windows_"+arch+".zip", "checksums.txt"),
		release("nightly", false, "x.zip"),
	}
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		if name == "checksums.txt" {
			var lines []string
			for n, data := range zips {
				lines = append(lines, sums(n, data))
			}
			_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
			return
		}
		data, ok := zips[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	t.Setenv("BATON_UPDATE_API", srv.URL)
	return srv
}

func goodSum(name string, data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) + "  " + name
}

func TestLatestAndDownload(t *testing.T) {
	name := "baton_0.5.0_windows_" + runtime.GOARCH + ".zip"
	zips := map[string][]byte{name: zipOf(t, map[string]string{
		"baton.exe": "cli", "baton-app.exe": "app", "README.md": "readme", "docs/images/a.png": "png"})}
	fakeGitHub(t, zips, goodSum)
	rel, err := Latest(context.Background(), "baton-test")
	if err != nil || rel.Version != "0.5.0" || rel.Tag != "v0.5.0" || rel.Notes != "notes for v0.5.0" || rel.ZipName != name {
		t.Fatalf("latest %+v %v", rel, err)
	}
	dir, err := Download(context.Background(), rel, t.TempDir(), "baton-test")
	if err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]string{"baton.exe": "cli", "baton-app.exe": "app", filepath.Join("docs", "images", "a.png"): "png"} {
		if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != want {
			t.Errorf("%s = %q", f, b)
		}
	}
}

func TestDownloadRefusesABadZip(t *testing.T) {
	name := "baton_0.5.0_windows_" + runtime.GOARCH + ".zip"
	// A checksum that does not match: nothing is unpacked.
	zips := map[string][]byte{name: zipOf(t, map[string]string{"baton.exe": "cli", "baton-app.exe": "app"})}
	fakeGitHub(t, zips, func(n string, _ []byte) string { return strings.Repeat("0", 64) + "  " + n })
	rel, err := Latest(context.Background(), "baton-test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := Download(context.Background(), rel, dir, "baton-test"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, strings.TrimSuffix(name, ".zip"))); !os.IsNotExist(err) {
		t.Fatal("a zip that failed its checksum was unpacked")
	}
	// An entry that climbs out of the folder.
	zips[name] = zipOf(t, map[string]string{"baton.exe": "cli", "baton-app.exe": "app", "../evil.txt": "x"})
	fakeGitHub(t, zips, goodSum)
	rel, _ = Latest(context.Background(), "baton-test")
	dir = t.TempDir()
	if _, err := Download(context.Background(), rel, dir, "baton-test"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("zip slip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("an entry outside the folder was written")
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"0.5.0", "0.4.1", 1}, {"v0.4.1", "0.4.1", 0}, {"0.4.1-dev", "0.4.1", 0}, {"0.4.10", "0.4.9", 1}, {"0.3.0", "1.0.0", -1}} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d", c.a, c.b, got)
		}
	}
}
