package supervisor

import (
	"regexp"
	"slices"
	"testing"
)

// A translation must take the same arguments in the same order.
func TestKoreanNoticesKeepVerbs(t *testing.T) {
	verb := regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z]`)
	for en, ko := range koreanNotices {
		if !slices.Equal(verb.FindAllString(en, -1), verb.FindAllString(ko, -1)) {
			t.Errorf("verbs differ:\n  %q\n  %q", en, ko)
		}
	}
}
