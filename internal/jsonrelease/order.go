package jsonrelease

import "strings"

// CompareVersions orders versions by semantic versioning precedence, with any
// number of numeric core parts (2024.3 and 1.95.0.1 are versions too; a
// missing part counts as 0). A pre-release (-beta.2) precedes its release and
// build metadata (+abc) has no precedence. Versions outside this form follow
// those inside it, in string order. Equal precedence falls back to string
// order, so distinct versions never compare equal.
func CompareVersions(a, b string) int {
	x, okA := parseVersion(a)
	y, okB := parseVersion(b)
	switch {
	case okA && okB:
		if order := compareVersions(x, y); order != 0 {
			return order
		}
	case okA:
		return 1
	case okB:
		return -1
	}
	return strings.Compare(a, b)
}

type version struct {
	core       []string
	prerelease []string
}

func parseVersion(s string) (version, bool) {
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	v := version{core: strings.Split(core, ".")}
	for _, part := range v.core {
		if !numeric(part) {
			return version{}, false
		}
	}
	if hasPre {
		v.prerelease = strings.Split(pre, ".")
		for _, id := range v.prerelease {
			if id == "" {
				return version{}, false
			}
		}
	}
	return v, true
}

func compareVersions(x, y version) int {
	for i := range max(len(x.core), len(y.core)) {
		if order := compareNumbers(part(x.core, i), part(y.core, i)); order != 0 {
			return order
		}
	}
	// A release follows its pre-releases.
	switch {
	case len(x.prerelease) == 0 && len(y.prerelease) == 0:
		return 0
	case len(x.prerelease) == 0:
		return 1
	case len(y.prerelease) == 0:
		return -1
	}
	for i := range min(len(x.prerelease), len(y.prerelease)) {
		a, b := x.prerelease[i], y.prerelease[i]
		switch {
		case numeric(a) && numeric(b):
			if order := compareNumbers(a, b); order != 0 {
				return order
			}
		case numeric(a):
			return -1
		case numeric(b):
			return 1
		default:
			if order := strings.Compare(a, b); order != 0 {
				return order
			}
		}
	}
	return compareInts(len(x.prerelease), len(y.prerelease))
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return "0"
}

// compareNumbers compares digit strings of any length by value.
func compareNumbers(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if order := compareInts(len(a), len(b)); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
