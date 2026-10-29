package jsonrelease

import (
	"slices"
	"testing"
)

func TestCompareVersionsBySemverPrecedence(t *testing.T) {
	// Oldest first: semver's own precedence example, then versions with other
	// numbers of core parts, then versions outside the form.
	ordered := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
		"1.9.2", "1.10.0", "2.0.0-rc.1", "2.0.0", "2024.3", "2024.3.0.1", "2024.3.1", "2024.10",
	}
	shuffled := slices.Clone(ordered)
	slices.Reverse(shuffled)
	slices.SortFunc(shuffled, CompareVersions)
	if !slices.Equal(shuffled, ordered) {
		t.Fatalf("sorted %v\nwant   %v", shuffled, ordered)
	}
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.0.0+build.2", "1.0.0+build.1", 1}, // build metadata only breaks ties
		{"1.0", "1.0.0", -1},                  // equal precedence: string order
		{"01.2", "1.2", -1},
		{"1.0.0", "nightly", 1}, // outside the form: older
		{"build-10", "build-9", -1},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}
