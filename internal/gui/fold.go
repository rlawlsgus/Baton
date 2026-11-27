package gui

import (
	"slices"
	"strings"
)

// What a conversation folded under another's row is (conversationView.Part).
const (
	partEarlier  = "earlier"  // the session went on from it in another conversation
	partFile     = "file"     // another file of the same conversation (it went on apart in two places)
	partCopy     = "copy"     // a copy of one of them
	partOriginal = "original" // one of them is a copy of it
	partBranch   = "branch"   // the session went on apart from an earlier part, in another list of the desktop app
)

// foldParts folds the conversations of one session under one row, as the
// desktop app shows a session once: the ones it went on from (later, see
// desktop.Lineage), copies (a copy's first rows are its source's), and
// other files of one conversation. Only what the conversations and the
// lists say joins two, never a name: a chain through a conversation no
// longer on disk still joins. The row is the newest of them (list is newest
// first), with the others under it, newest first.
func foldParts(list []conversationView, later map[string][]string) []conversationView {
	parent := map[string]string{}
	var find func(id string) string
	find = func(id string) string {
		p, ok := parent[id]
		if !ok || p == id {
			parent[id] = id
			return id
		}
		root := find(p)
		parent[id] = root
		return root
	}
	union := func(a, b string) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[ra] = rb
		}
	}
	for from, tos := range later {
		for _, to := range tos {
			union(from, to)
		}
	}
	copies := map[string]bool{} // conversations a listed one is a copy of
	for _, c := range list {
		if c.from != "" {
			union(strings.ToLower(c.SessionID), c.from)
			copies[c.from] = true
		}
	}
	at := map[string]int{} // a group's row in out
	var out []conversationView
	for _, c := range list {
		id := strings.ToLower(c.SessionID)
		root := find(id)
		i, ok := at[root]
		if !ok {
			at[root] = len(out)
			out = append(out, c)
			continue
		}
		top := &out[i]
		switch {
		case strings.EqualFold(c.SessionID, top.SessionID):
			c.Part = partFile
		case len(later[id]) > 0:
			c.Part = partEarlier
		case c.from != "":
			c.Part = partCopy
		case copies[id]:
			c.Part = partOriginal
		default:
			c.Part = partBranch
		}
		top.Earlier = append(top.Earlier, c)
	}
	for i := range out {
		slices.SortStableFunc(out[i].Earlier, func(a, b conversationView) int { return b.Modified.Compare(a.Modified) })
	}
	return out
}
