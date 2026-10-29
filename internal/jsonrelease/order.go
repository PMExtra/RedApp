package jsonrelease

import "strings"

// CompareDisplayVersions orders display versions such as 1.95.0 or 2024.3.1
// naturally: digit runs compare as numbers, other runs as text, and a version
// that is a prefix of another is older. It is a presentation order only;
// release identities have no order.
func CompareDisplayVersions(a, b string) int {
	for a != "" && b != "" {
		x, restA := nextRun(a)
		y, restB := nextRun(b)
		if order := compareRuns(x, y); order != 0 {
			return order
		}
		a, b = restA, restB
	}
	switch {
	case a == b:
		return 0
	case a == "":
		return -1
	default:
		return 1
	}
}

func nextRun(s string) (string, string) {
	digits := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == digits {
		i++
	}
	return s[:i], s[i:]
}

func compareRuns(x, y string) int {
	if isDigit(x[0]) && isDigit(y[0]) {
		x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(x, y)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
