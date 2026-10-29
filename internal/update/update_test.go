package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html"
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

// fakeGitHub serves a releases feed like GitHub's and the releases' files:
// v0.5.2 has no checksums.txt, v0.5.1 has one without a zip for this PC, and
// "nightly" is not a version.
func fakeGitHub(t *testing.T, zips map[string][]byte, sums func(name string, data []byte) string) *httptest.Server {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	entry := func(tag string) string {
		notes := "<p>notes for " + tag + "</p>\n<ul>\n<li><strong>One.</strong> It&#39;s<br>\nlong.</li>\n<li>Two</li>\n</ul>"
		return `<entry><id>tag:github.com,2008:Repository/1/` + tag + `</id><updated>2026-10-29T04:33:47Z</updated>` +
			`<link rel="alternate" type="text/html" href="https://github.com/` + Repo + `/releases/tag/` + tag + `"/>` +
			`<title>Baton ` + tag + `</title><content type="html">` + html.EscapeString(notes) + `</content></entry>`
	}
	feed := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom">` +
		entry("v0.4.1") + entry("nightly") + entry("v0.5.2") + entry("v0.5.1") + entry("v0.5.0") + `</feed>`
	mux.HandleFunc("/"+Repo+"/releases.atom", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(feed))
	})
	mux.HandleFunc("/"+Repo+"/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		tag, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"+Repo+"/releases/download/"), "/")
		v := strings.TrimPrefix(tag, "v")
		if name == "checksums.txt" && tag != "v0.5.2" {
			lines := []string{strings.Repeat("1", 64) + "  baton_" + v + "_windows_other.zip"}
			for n, data := range zips {
				if strings.Contains(n, "_"+v+"_") {
					lines = append(lines, sums(n, data))
				}
			}
			_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
			return
		}
		data, ok := zips[name]
		if !ok || !strings.Contains(name, "_"+v+"_") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	t.Setenv("BATON_UPDATE_SITE", srv.URL)
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
	srv := fakeGitHub(t, zips, goodSum)
	rel, err := Latest(context.Background(), "baton-test")
	notes := "notes for v0.5.0\n\n- One. It's\n  long.\n- Two"
	if err != nil || rel.Version != "0.5.0" || rel.Tag != "v0.5.0" || rel.Notes != notes || rel.ZipName != name ||
		rel.Page != srv.URL+"/"+Repo+"/releases/tag/v0.5.0" {
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

// TestPlainText uses the shape of GitHub's feed for baton 0.5.0's notes.
func TestPlainText(t *testing.T) {
	in := "<p>baton can now update itself.</p>\n<h2>New</h2>\n<ul>\n<li><strong>Updates.</strong> <strong>Help → Update baton</strong> asks GitHub<br>\n" +
		"changed. <code>baton update</code> &amp; <code>&lt;name&gt;</code>.</li>\n<li>Second</li>\n</ul>\n<hr>\n<h2>한국어</h2>\n" +
		"<p><strong>새 기능</strong></p>\n<ul>\n<li>업데이트</li>\n</ul>\n<h2>SHA-256</h2>\n<pre><code>aaaa  baton_0.5.0_windows_amd64.zip\nbbbb  baton_0.5.0_windows_arm64.zip\n</code></pre>"
	want := "baton can now update itself.\n\nNew\n\n- Updates. Help → Update baton asks GitHub\n  changed. baton update & <name>.\n- Second\n\n---\n\n" +
		"한국어\n\n새 기능\n\n- 업데이트\n\nSHA-256\n\naaaa  baton_0.5.0_windows_amd64.zip\nbbbb  baton_0.5.0_windows_arm64.zip"
	if got := plainText(in); got != want {
		t.Errorf("plainText:\n%s\n-- want --\n%s", got, want)
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
