package jsonrelease

import "testing"

func TestCompareDisplayVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.95.0", "1.95.0", 0},
		{"1.95.0", "1.100.0", -1},
		{"2024.3.1", "2024.3", 1},
		{"2024.3", "2025.1", -1},
		{"1.2.0", "1.2.00", 0},
		{"2.0.0-beta", "2.0.0", 1},
		{"build-10", "build-9", 1},
	} {
		if got := CompareDisplayVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareDisplayVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareDisplayVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareDisplayVersions(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}
