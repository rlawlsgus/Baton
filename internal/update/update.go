// Package update finds baton's newest release on GitHub and gets it ready to
// install: the zip for this processor, checked against the release's
// checksums.txt and unpacked into a folder. Installing it is the same as the
// window's "Install to the Start menu" (winui.Install).
package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Repo is where baton is released.
const Repo = "rlawlsgus/Baton"

// Site is GitHub; BATON_UPDATE_SITE points baton at another one (a test
// server). baton reads the repository's releases feed and the releases'
// download links there, not GitHub's REST API: the API answers only 60
// requests an hour to everyone behind one address without signing in, which
// an office network uses up.
func Site() string {
	if v := os.Getenv("BATON_UPDATE_SITE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://github.com"
}

// Release is a version of baton that can be installed on this PC.
type Release struct {
	Version string    `json:"version"` // 0.5.0
	Tag     string    `json:"tag"`     // v0.5.0
	Notes   string    `json:"notes"`   // the release page's text, as plain text
	Page    string    `json:"page"`    // the release page
	Date    time.Time `json:"date"`
	ZipName string    `json:"zip_name"`
}

// asset is the download link of one of the release's files.
func (r Release) asset(name string) string {
	return Site() + "/" + Repo + "/releases/download/" + url.PathEscape(r.Tag) + "/" + name
}

var client = &http.Client{Timeout: 5 * time.Minute}

// statusError is an answer other than 200 OK.
type statusError struct {
	url, status string
	code        int
}

func (e *statusError) Error() string { return e.url + ": " + e.status }

func get(ctx context.Context, url, userAgent string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &statusError{url: url, status: resp.Status, code: resp.StatusCode}
	}
	return resp, nil
}

var versionRx = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// errNoZip: the release's checksums.txt names no zip for this processor.
var errNoZip = errors.New("no zip for this PC")

// Latest is the newest release that has a zip for this processor and a
// checksums.txt that lists it (pre-releases included: a preview is still the
// newest baton; drafts are not in the feed). userAgent names baton and its
// version.
func Latest(ctx context.Context, userAgent string) (Release, error) {
	resp, err := get(ctx, Site()+"/"+Repo+"/releases.atom", userAgent)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	var feed struct {
		Entries []struct {
			Updated time.Time `xml:"updated"`
			Link    struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&feed); err != nil {
		return Release{}, fmt.Errorf("reading the releases: %w", err)
	}
	var found []Release
	for _, e := range feed.Entries {
		tag, err := url.PathUnescape(path.Base(e.Link.Href))
		m := versionRx.FindStringSubmatch(tag)
		if err != nil || m == nil || !strings.Contains(e.Link.Href, "/releases/tag/") {
			continue
		}
		v := m[1] + "." + m[2] + "." + m[3]
		found = append(found, Release{Version: v, Tag: tag, Notes: plainText(e.Content),
			Page: Site() + "/" + Repo + "/releases/tag/" + url.PathEscape(tag), Date: e.Updated,
			ZipName: fmt.Sprintf("baton_%s_windows_%s.zip", v, runtime.GOARCH)})
	}
	sort.SliceStable(found, func(i, j int) bool { return Compare(found[i].Version, found[j].Version) > 0 })
	// The newest one built for this PC; a few are tried, so a release without
	// this processor's zip does not hide the one before it.
	for i, rel := range found {
		if i == 3 {
			break
		}
		_, err := checksum(ctx, rel, userAgent)
		var se *statusError
		if errors.Is(err, errNoZip) || errors.As(err, &se) && se.code == http.StatusNotFound {
			continue // not built for this PC, or not checkable
		}
		if err != nil {
			return Release{}, err
		}
		return rel, nil
	}
	return Release{}, errors.New("no release of baton for this PC was found")
}

var tagRx = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)[^>]*>`)

// plainText turns a release's notes, which the feed gives as HTML, back into
// text close to the Markdown they were written in.
func plainText(h string) string {
	var b strings.Builder
	depth, last, afterBreak := 0, 0, false
	text := func(s string) {
		if afterBreak { // GitHub writes "<br>\n"
			s, afterBreak = strings.TrimPrefix(s, "\n"), false
		}
		if strings.TrimSpace(s) == "" && strings.Contains(s, "\n") {
			return // the line breaks GitHub puts between blocks
		}
		if depth > 0 {
			s = strings.ReplaceAll(s, "\n", "\n"+strings.Repeat("  ", depth))
		}
		b.WriteString(s)
	}
	for _, m := range tagRx.FindAllStringSubmatchIndex(h, -1) {
		text(h[last:m[0]])
		last = m[1]
		closing := h[m[2]:m[3]] == "/"
		switch strings.ToLower(h[m[4]:m[5]]) {
		case "li":
			if closing {
				depth--
				b.WriteString("\n")
			} else {
				b.WriteString(strings.Repeat("  ", depth) + "- ")
				depth++
			}
		case "br":
			b.WriteString("\n" + strings.Repeat("  ", depth))
			afterBreak = true
		case "p", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote":
			if closing {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		case "ul", "ol":
			b.WriteString("\n")
		case "hr":
			b.WriteString("\n\n---\n\n")
		}
	}
	text(h[last:])
	lines := strings.Split(html.UnescapeString(b.String()), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

// Compare compares two versions ("0.5.0", "v0.5.0", "0.5.0-dev"; a -dev build
// counts as that version).
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

// Download fetches the release's zip into dir, checks it against the
// release's checksums.txt and unpacks it. It returns the folder that holds
// baton.exe and baton-app.exe.
func Download(ctx context.Context, rel Release, dir, userAgent string) (string, error) {
	if rel.Tag == "" || rel.ZipName == "" {
		return "", errors.New("this release has no download for this PC")
	}
	want, err := checksum(ctx, rel, userAgent)
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, rel.ZipName)
	resp, err := get(ctx, rel.asset(rel.ZipName), userAgent)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", rel.ZipName, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return "", fmt.Errorf("%s does not match checksums.txt (got %s, want %s); nothing was installed", rel.ZipName, got, want)
	}
	out := filepath.Join(dir, strings.TrimSuffix(rel.ZipName, ".zip"))
	if err := unzip(zipPath, out); err != nil {
		return "", err
	}
	for _, name := range []string{"baton.exe", "baton-app.exe"} {
		if fi, err := os.Stat(filepath.Join(out, name)); err != nil || fi.IsDir() {
			return "", fmt.Errorf("%s has no %s", rel.ZipName, name)
		}
	}
	return out, nil
}

// checksum is the SHA-256 that checksums.txt gives for the release's zip.
func checksum(ctx context.Context, rel Release, userAgent string) (string, error) {
	resp, err := get(ctx, rel.asset("checksums.txt"), userAgent)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == rel.ZipName && len(f[0]) == 64 {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no line for %s: %w", rel.ZipName, errNoZip)
}

// unzip unpacks a zip into dir, refusing any entry that would land outside it.
func unzip(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		name := filepath.FromSlash(strings.ReplaceAll(f.Name, `\`, "/"))
		target := filepath.Join(root, name)
		if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return fmt.Errorf("the zip has an entry outside its folder: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extract(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, 200<<20)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
