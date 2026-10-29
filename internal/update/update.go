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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is where baton is released.
const Repo = "rlawlsgus/Baton"

// API is GitHub's REST API; BATON_UPDATE_API points baton at another one
// (a test server).
func API() string {
	if v := os.Getenv("BATON_UPDATE_API"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}

// Release is a version of baton that can be installed on this PC.
type Release struct {
	Version string    `json:"version"` // 0.5.0
	Tag     string    `json:"tag"`     // v0.5.0
	Notes   string    `json:"notes"`   // the release page's text (Markdown)
	Page    string    `json:"page"`    // the release page
	Date    time.Time `json:"date"`
	ZipName string    `json:"zip_name"`
	zipURL  string
	sumsURL string
}

var client = &http.Client{Timeout: 5 * time.Minute}

func get(ctx context.Context, url, userAgent string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

var versionRx = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Latest is the newest release that has a zip for this processor (pre-releases
// included: a preview is still the newest baton). userAgent names baton and
// its version, as GitHub asks.
func Latest(ctx context.Context, userAgent string) (Release, error) {
	resp, err := get(ctx, API()+"/repos/"+Repo+"/releases?per_page=20", userAgent)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	var list []struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		Page      string    `json:"html_url"`
		Draft     bool      `json:"draft"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&list); err != nil {
		return Release{}, fmt.Errorf("reading the releases: %w", err)
	}
	var best Release
	for _, r := range list {
		m := versionRx.FindStringSubmatch(r.Tag)
		if r.Draft || m == nil {
			continue
		}
		v := m[1] + "." + m[2] + "." + m[3]
		rel := Release{Version: v, Tag: r.Tag, Notes: r.Body, Page: r.Page, Date: r.Published,
			ZipName: fmt.Sprintf("baton_%s_windows_%s.zip", v, runtime.GOARCH)}
		for _, a := range r.Assets {
			switch a.Name {
			case rel.ZipName:
				rel.zipURL = a.URL
			case "checksums.txt":
				rel.sumsURL = a.URL
			}
		}
		if rel.zipURL == "" || rel.sumsURL == "" {
			continue // not built for this PC, or not checkable
		}
		if best.Version == "" || Compare(rel.Version, best.Version) > 0 {
			best = rel
		}
	}
	if best.Version == "" {
		return Release{}, errors.New("no release of baton for this PC was found")
	}
	return best, nil
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
	if rel.zipURL == "" || rel.sumsURL == "" {
		return "", errors.New("this release has no download for this PC")
	}
	want, err := checksum(ctx, rel, userAgent)
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, rel.ZipName)
	resp, err := get(ctx, rel.zipURL, userAgent)
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
	resp, err := get(ctx, rel.sumsURL, userAgent)
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
	return "", fmt.Errorf("checksums.txt has no line for %s", rel.ZipName)
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
