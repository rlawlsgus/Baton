package office

import (
	"errors"
	"os"
	"sort"
	"time"

	"baton/internal/fsx"
)

// floors numbers the building: one floor per project folder, numbered once
// and kept in <baton>\office.json, so a project stays on the floor the user
// knows it by.
type floors struct {
	path  string
	m     map[string]int // project key -> floor
	begun bool           // numbering has begun (office.json exists, or was just written)
}

type floorsFile struct {
	Floors map[string]int `json:"floors"`
}

func loadFloors(path string) *floors {
	f := &floors{path: path, m: map[string]int{}}
	var file floorsFile
	err := fsx.ReadJSON(path, &file)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		f.begun = true // an unreadable file is not a first run: numbering goes on from scratch
	}
	for k, n := range file.Floors {
		if k != "" && n > 0 {
			f.m[k] = n
		}
	}
	return f
}

// of is a project's floor (0: not numbered yet).
func (f *floors) of(key string) int { return f.m[key] }

// number gives each new project folder a floor, the one with the oldest
// activity first: the lowest number left vacant (a project gone from the
// building whose conversations are all gone too, as vacant says), else one
// on top of the building. A floor still in the building never moves. On the
// very first run every project known is new, so the oldest becomes 1F. It
// reports whether any was added. vacant may be nil: always on top.
func (f *floors) number(oldest map[string]time.Time, vacant func(key string) bool) bool {
	var add []string
	for k := range oldest {
		if k != "" && f.m[k] == 0 {
			add = append(add, k)
		}
	}
	if len(add) == 0 {
		return false
	}
	sort.Slice(add, func(i, j int) bool {
		if a, b := oldest[add[i]], oldest[add[j]]; !a.Equal(b) {
			return a.Before(b)
		}
		return add[i] < add[j]
	})
	top := 0
	type held struct {
		key string
		n   int
	}
	var gone []held // numbered projects not in the building now, lowest first
	for k, n := range f.m {
		top = max(top, n)
		if _, here := oldest[k]; !here {
			gone = append(gone, held{k, n})
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].n < gone[j].n })
	next := 0 // each one is asked about once
	for _, k := range add {
		n := 0
		for vacant != nil && n == 0 && next < len(gone) {
			g := gone[next]
			next++
			if vacant(g.key) {
				delete(f.m, g.key)
				n = g.n
			}
		}
		if n == 0 {
			top++
			n = top
		}
		f.m[k] = n
	}
	return true
}

// save writes office.json (atomically, like baton's other files).
func (f *floors) save() error {
	f.begun = true
	return fsx.WriteJSONAtomic(f.path, floorsFile{Floors: f.m})
}
