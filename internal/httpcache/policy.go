package httpcache

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

func directives(h http.Header) map[string]string {
	out := map[string]string{}
	for _, value := range h.Values("Cache-Control") {
		for _, item := range directiveItems(value) {
			key, value, _ := strings.Cut(strings.TrimSpace(item), "=")
			key = strings.ToLower(key)
			if _, duplicate := out[key]; duplicate && (key == "max-age" || key == "s-maxage") {
				out[key] = ""
			} else {
				out[key] = strings.Trim(value, "\" ")
			}
		}
	}
	return out
}

// Quoted field-name arguments may contain commas. They must never be parsed
// as independent directives or freshness lifetimes.
func directiveItems(value string) []string {
	var out []string
	start := 0
	quoted, escaped := false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
			continue
		}
		if c == ',' && !quoted {
			out = append(out, value[start:i])
			start = i + 1
		}
	}
	return append(out, value[start:])
}

// cacheBlockReason distinguishes a source cache directive from representation
// isolation. A path rule may override no-store/private, but never cookies or
// unsupported Vary (including Authorization-dependent representations).
func cacheBlockReason(h http.Header, decision CacheDecision) string {
	if len(h.Values("Set-Cookie")) > 0 {
		return "set-cookie"
	}
	for _, v := range h.Values("Vary") {
		for _, key := range strings.Split(v, ",") {
			if key = strings.TrimSpace(key); key != "" && !strings.EqualFold(key, "Accept-Encoding") {
				return "unsupported-vary"
			}
		}
	}
	if !decision.Explicit {
		d := directives(h)
		for _, key := range []string{"no-store", "private"} {
			if _, found := d[key]; found {
				return "source-" + key
			}
		}
	}
	return ""
}

// Only directive names enter events. Field-name arguments (private="...") and
// all other response header values can contain source-specific sensitive data.
func overrideDirectives(h http.Header) []string {
	d := directives(h)
	var out []string
	for _, key := range []string{"no-store", "private"} {
		if _, found := d[key]; found {
			out = append(out, key)
		}
	}
	return out
}

func (s *Service) recordOverride(f fill, h http.Header, result fetchResult, err error) (fetchResult, error) {
	if err != nil || result.row == nil {
		return result, err
	}
	decision := f.decision()
	overrides := overrideDirectives(h)
	if !decision.Explicit || decision.TTLSeconds == 0 || len(overrides) == 0 {
		return result, nil
	}
	err = s.db.RecordEvent(store.Event{AppID: f.entry.MetricsID(), ResourceKey: f.path, Category: "warning", Code: "cache_rule_override", Message: fmt.Sprintf("Cache rule %q at revision %d overrides source Cache-Control: %s", decision.RuleID, decision.Revision, strings.Join(overrides, ", "))})
	if err != nil {
		s.unpin(result.row.GenerationID)
		return fetchResult{}, err
	}
	return result, nil
}

// Pattern TTLs are handled before this helper. A source freshness lifetime
// takes precedence over the application default. The default applies only when
// Cache-Control is wholly absent, and starts at local validation time.
func freshness(h http.Header, ttl int, now time.Time) time.Time {
	if len(h.Values("Cache-Control")) == 0 {
		return now.Add(time.Duration(ttl) * time.Second)
	}
	d := directives(h)
	value, ok := d["s-maxage"]
	if !ok {
		value, ok = d["max-age"]
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if !ok || err != nil || seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
		return now
	}
	age, _ := strconv.ParseInt(h.Get("Age"), 10, 64)
	if date, err := http.ParseTime(h.Get("Date")); err == nil {
		if apparent := int64(now.Sub(date) / time.Second); apparent > age {
			age = apparent
		}
	}
	if age < 0 {
		age = 0
	}
	seconds -= age
	if seconds < 0 {
		seconds = 0
	}
	// Saturate hostile source lifetimes before converting seconds to Duration.
	if seconds > int64((1<<63-1)/time.Second) {
		seconds = int64((1<<63 - 1) / time.Second)
	}
	return now.Add(time.Duration(seconds) * time.Second)
}
func representationHeaders(h http.Header) http.Header {
	out := http.Header{}
	for _, key := range []string{"ETag", "Last-Modified", "Cache-Control", "Vary", "Date", "Age", "Expires", "Content-Length", "Content-Range"} {
		if values, ok := h[http.CanonicalHeaderKey(key)]; ok {
			out[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		}
	}
	return out
}
func mergedHeaders(old, fresh http.Header) http.Header {
	result := old.Clone()
	for key, values := range representationHeaders(fresh) {
		result[key] = append([]string(nil), values...)
	}
	// Age belongs to a response instance, never to a previous validation response.
	if fresh.Get("Age") == "" {
		result.Del("Age")
	}
	return result
}

// Preconditions apply to a complete existing representation, including a HEAD
// response for which no local body exists. Ranges are handled separately.
func preconditionStatus(r *http.Request, h http.Header) int {
	tag := h.Get("ETag")
	modified, modErr := http.ParseTime(h.Get("Last-Modified"))
	if match := r.Header.Get("If-Match"); match != "" {
		if !etagMatch(match, tag, false) {
			return http.StatusPreconditionFailed
		}
	} else if value := r.Header.Get("If-Unmodified-Since"); value != "" && modErr == nil {
		if when, err := http.ParseTime(value); err == nil && modified.After(when) {
			return http.StatusPreconditionFailed
		}
	}
	if match := r.Header.Get("If-None-Match"); match != "" {
		if etagMatch(match, tag, true) {
			return http.StatusNotModified
		}
	} else if value := r.Header.Get("If-Modified-Since"); value != "" && modErr == nil {
		if when, err := http.ParseTime(value); err == nil && !modified.After(when) {
			return http.StatusNotModified
		}
	}
	return http.StatusOK
}
func etagMatch(list, tag string, weak bool) bool {
	for list = strings.TrimSpace(list); list != ""; list = strings.TrimSpace(list) {
		if list == "*" {
			return true
		}
		start := 0
		if strings.HasPrefix(list, "W/") {
			start = 2
		}
		if len(list) <= start || list[start] != '"' {
			return false
		}
		end := strings.IndexByte(list[start+1:], '"')
		if end < 0 {
			return false
		}
		end += start + 2
		candidate := list[:end]
		if weak {
			if strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(tag, "W/") {
				return true
			}
		} else if !strings.HasPrefix(candidate, "W/") && !strings.HasPrefix(tag, "W/") && candidate == tag {
			return true
		}
		list = strings.TrimSpace(list[end:])
		if list == "" {
			return false
		}
		if list[0] != ',' {
			return false
		}
		list = list[1:]
	}
	return false
}

func validNotModified(old *Row, h http.Header) bool {
	if tag := h.Get("ETag"); tag != "" && tag != old.headers.Get("ETag") {
		return false
	}
	if old.headers.Get("ETag") == "" && h.Get("Last-Modified") != "" && old.headers.Get("Last-Modified") != "" && h.Get("Last-Modified") != old.headers.Get("Last-Modified") {
		return false
	}
	if value := h.Get("Content-Length"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n != old.SizeBytes {
			return false
		}
	}
	return true
}
