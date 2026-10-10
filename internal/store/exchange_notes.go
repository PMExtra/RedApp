package store

import (
	"unicode"
	"unicode/utf8"
)

func validNotes(text string) bool {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 12000 {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
