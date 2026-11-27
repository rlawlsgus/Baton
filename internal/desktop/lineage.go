package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Lineage says which conversations went on in which, as far as the desktop
// app's lists and baton's index tell: it maps a conversation (in lower case)
// to the ones it went on in. A session of the app's goes on in a new
// conversation now and then (when its context ran out and it goes on from a
// summary, after /clear), and its record names the ones it was on before
// (see head.earlier); the app shows the session once, under the one it is
// on now. A session of baton's went on from Thread.Prior. Every list of
// every data folder of the app is read, whichever account it is signed in
// to: a record of another account's names the same conversations.
func (d *Desk) Lineage() map[string][]string {
	out := map[string][]string{}
	add := func(from, to string) {
		from, to = strings.ToLower(from), strings.ToLower(to)
		if validID(from) && validID(to) && from != to && !slices.Contains(out[from], to) {
			out[from] = append(out[from], to)
		}
	}
	if d.IndexPath != "" && isFile(d.IndexPath) {
		if ix, err := LoadIndex(d.IndexPath); err == nil {
			for _, t := range ix.Threads {
				for _, p := range t.Prior {
					add(p, t.Session)
				}
			}
		}
	}
	dirs, _ := d.allData()
	for _, data := range dirs {
		root := filepath.Join(data, sessionsDir)
		accts, _ := os.ReadDir(root)
		for _, a := range accts {
			if !a.IsDir() {
				continue
			}
			orgs, _ := os.ReadDir(filepath.Join(root, a.Name()))
			for _, o := range orgs {
				if !o.IsDir() {
					continue
				}
				dir := filepath.Join(root, a.Name(), o.Name())
				recs, _ := os.ReadDir(dir)
				for _, e := range recs {
					n := e.Name()
					if e.IsDir() || !strings.HasPrefix(n, "local_") || !strings.HasSuffix(n, ".json") {
						continue
					}
					h, ok := readHead(filepath.Join(dir, n))
					if !ok || h.current() == "" {
						continue
					}
					for _, p := range h.earlier() {
						add(p, h.current())
					}
				}
			}
		}
	}
	return out
}
