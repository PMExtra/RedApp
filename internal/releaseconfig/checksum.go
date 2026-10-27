package releaseconfig

import (
	"errors"
	"strings"
)

// ParseChecksum accepts one bare SHA256 or standard sha256sum lines. Multi-file
// lists require an exact configured filename; a URL basename is never guessed.
func ParseChecksum(body []byte, filename string) (string, error) {
	if len(body) == 0 || len(body) > MaxChecksumBytes {
		return "", errors.New("checksum response exceeds limits")
	}
	text := strings.TrimSpace(string(body))
	if ValidSHA256(strings.ToLower(text)) {
		return strings.ToLower(text), nil
	}
	found := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// sha256sum uses two spaces, or a space followed by '*'.
		if len(line) < 67 || line[64] != ' ' || (line[65] != ' ' && line[65] != '*') || !ValidSHA256(strings.ToLower(line[:64])) {
			return "", errors.New("unsupported checksum format")
		}
		if filename != "" && line[66:] == filename {
			if found != "" {
				return "", errors.New("duplicate checksum filename")
			}
			found = strings.ToLower(line[:64])
		}
	}
	if found == "" {
		return "", errors.New("checksum filename not found")
	}
	return found, nil
}
