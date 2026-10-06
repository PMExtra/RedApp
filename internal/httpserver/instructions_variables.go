package httpserver

import (
	"html"
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
)

// Substitute scalar values after Markdown rendering: code retains literal text,
// while HTML escaping prevents a value from introducing markup or attributes.
// Replacement is single-pass; unknown variables remain literal.
func (s *Server) interpolateInstructionVariables(entry application.Entry, rendered, origin, lang string) string {
	version := "<version>"
	if strings.Contains(rendered, "{{latest_version}}") {
		if known, _, err := s.latestKnownVersion(entry); err == nil && known != "" {
			version = known
		}
	}
	key := entry.Descriptor.ID
	return strings.NewReplacer(
		"{{base_url}}", html.EscapeString(strings.TrimRight(origin, "/")),
		"{{app_path}}", html.EscapeString("/"+key),
		"{{latest_version}}", html.EscapeString(version),
		"{{app_name}}", html.EscapeString(entry.Descriptor.Name[lang]),
		"{{app_key}}", html.EscapeString(key),
		"{{public_origin}}", html.EscapeString(origin),
		"{{app_url}}", html.EscapeString(origin+"/"+key),
	).Replace(rendered)
}
