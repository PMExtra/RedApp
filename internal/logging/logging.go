// Package logging holds the conventions shared by RedApp's structured logs:
// every background component tags its records with a component attribute,
// errors are logged with URL credentials masked, and a component constructed
// without a logger discards its records.
package logging

import (
	"log/slog"
	"regexp"
)

// For returns log tagged with component=name, or a discarding logger when
// log is nil (the default in tests).
func For(log *slog.Logger, name string) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return log.With(slog.String("component", name))
}

// credentialURL matches a scheme and everything after it up to the last "@"
// before the next whitespace. A password may contain "/", "?", "#" or "@"
// when the URL is malformed (for example an unencoded proxy password in a
// parse error), so the match deliberately runs to the last "@": masking part
// of a host or path is preferable to logging part of a password.
var credentialURL = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)\S*@`)

// Redact masks the user information (proxy and upstream credentials) of every
// URL in text, including malformed URLs.
func Redact(text string) string {
	return credentialURL.ReplaceAllString(text, "${1}****@")
}

// Error is the "error" attribute of err with URL credentials masked.
func Error(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "")
	}
	return slog.String("error", Redact(err.Error()))
}
