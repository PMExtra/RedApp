package httpcache

import (
	"testing"
	"time"
)

// Listings of nginx autoindex, Caddy browse and RedApp report names, kinds,
// sizes and times; directories never carry a size.
func TestParseIndexReadsListingFormats(t *testing.T) {
	modified := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	type want struct {
		href      string
		directory bool
		size      int64
		dated     bool
	}
	for name, tc := range map[string]struct {
		body, contentType string
		want              []want
	}{
		"nginx": {`[{"name":"d","type":"directory","mtime":"Wed, 28 Oct 2026 10:00:00 GMT"},{"name":"a b","type":"file","mtime":"Wed, 28 Oct 2026 10:00:00 GMT","size":3},{"name":"l","type":"other"}]`, "application/json",
			[]want{{"d/", true, -1, true}, {"a%20b", false, 3, true}}},
		"caddy": {`[{"name":"d/","size":4096,"url":"./d/","mod_time":"2026-10-28T10:00:00Z","is_dir":true},{"name":"f","size":5,"url":"./f","mod_time":"2026-10-28T10:00:00.5Z","is_dir":false}]`, "application/json",
			[]want{{"./d/", true, -1, true}, {"./f", false, 5, true}}},
		"redapp": {` {"path":"/","stale":false,"truncated":false,"entries":[{"name":"d","type":"directory","modified":"2026-10-28T10:00:00Z"},{"name":"f","type":"file","size":7}]}`, "application/json; charset=utf-8",
			[]want{{"d/", true, -1, true}, {"f", false, 7, false}}},
		"html": {`<a href="x/">x/</a><a href="y">y</a>`, "text/html",
			[]want{{"x/", true, -1, false}, {"y", false, -1, false}}},
	} {
		t.Run(name, func(t *testing.T) {
			links, err := parseIndex([]byte(tc.body), tc.contentType)
			if err != nil || len(links) != len(tc.want) {
				t.Fatalf("%+v %v", links, err)
			}
			for i, w := range tc.want {
				l := links[i]
				size := int64(-1)
				if l.Size != nil {
					size = *l.Size
				}
				if l.Href != w.href || l.Directory != w.directory || size != w.size || (l.Modified != nil) != w.dated {
					t.Fatalf("link %d: %+v size=%d", i, l, size)
				}
				if l.Modified != nil && !l.Modified.Truncate(time.Second).Equal(modified) {
					t.Fatalf("link %d time %v", i, l.Modified)
				}
			}
		})
	}
	if _, err := parseIndex([]byte(`{"files":[]}`), "application/json"); err == nil {
		t.Fatal("an object without entries was read as a listing")
	}
}
