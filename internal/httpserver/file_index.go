package httpserver

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/store"
)

// maxIndexEntries bounds one directory listing; a longer directory is
// truncated rather than paginated, like common autoindex implementations.
const maxIndexEntries = 10000

// fileIndex is the listing of one directory of a hosted or http-cache
// application with auto_index. Path is application-relative with a leading
// and a trailing slash.
type fileIndex struct {
	Path      string           `json:"path"`
	Stale     bool             `json:"stale"`
	Truncated bool             `json:"truncated"`
	Entries   []fileIndexEntry `json:"entries"`
}

type fileIndexEntry struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"` // file or directory
	Size     *int64     `json:"size,omitempty"`
	Modified *time.Time `json:"modified,omitempty"`
}

//go:embed file_index.css
var fileIndexCSS string

//go:embed file_index.js
var fileIndexJS string

// The page has no inline code other than these two blocks, allowed by hash.
var fileIndexPolicy = "default-src 'none'; style-src '" + cspHash(fileIndexCSS) + "'; script-src '" + cspHash(fileIndexJS) + "'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func cspHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

// wantsJSON reports whether the client prefers application/json to HTML. A
// tie, wildcards alone or no Accept header select HTML.
func wantsJSON(r *http.Request) bool {
	var jsonQ, htmlQ float64
	for _, part := range strings.Split(strings.Join(r.Header.Values("Accept"), ","), ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		q := 1.0
		if raw, ok := params["q"]; ok {
			if q, err = strconv.ParseFloat(raw, 64); err != nil || q < 0 || q > 1 {
				continue
			}
		}
		switch media {
		case "application/json":
			jsonQ = max(jsonQ, q)
		case "text/html", "text/*", "*/*":
			htmlQ = max(htmlQ, q)
		}
	}
	return jsonQ > 0 && jsonQ > htmlQ
}

// indexHref is the relative link of an entry. The "./" prefix keeps a name
// such as "a:b" from being read as a URL scheme.
func indexHref(e fileIndexEntry) string {
	href := "./" + url.PathEscape(e.Name)
	if e.Type == "directory" {
		href += "/"
	}
	return href
}

type indexText struct {
	Lang, Back, Parent, Name, Size, Modified, Filter, NoMatch, Empty, Truncated, Stale string
}

var indexTexts = map[string]indexText{
	"en":    {Lang: "en", Back: "Back to application", Parent: "Parent directory", Name: "Name", Size: "Size", Modified: "Modified", Filter: "Filter this directory", NoMatch: "No entries match.", Empty: "This directory is empty.", Truncated: "This directory has more entries; only the first %d are shown.", Stale: "The source is unavailable; this listing was fetched at %s."},
	"zh-CN": {Lang: "zh-CN", Back: "返回应用", Parent: "上级目录", Name: "名称", Size: "大小", Modified: "修改时间", Filter: "筛选当前目录", NoMatch: "没有匹配的项。", Empty: "此目录为空。", Truncated: "目录项过多，只显示前 %d 项。", Stale: "来源暂不可用，显示的是 %s 获取的列表。"},
}

// indexLanguage picks Simplified Chinese when the first supported language in
// Accept-Language is Chinese, and English otherwise.
func indexLanguage(r *http.Request) string {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case strings.HasPrefix(tag, "zh"):
			return "zh-CN"
		case strings.HasPrefix(tag, "en"):
			return "en"
		}
	}
	return "en"
}

func formatSize(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value, unit := float64(n), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + units[unit]
}

var fileIndexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="{{.Text.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.App}} – {{.Index.Path}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<main>
<header>
<h1>{{.App}} <span class="path">{{.Index.Path}}</span></h1>
<a class="back" href="{{.AppPath}}">← {{.Text.Back}}</a>
</header>
{{if .Stale}}<p class="notice">{{.Stale}}</p>{{end}}
{{if .Index.Truncated}}<p class="notice">{{.Truncated}}</p>{{end}}
<input id="filter" class="search" type="search" placeholder="{{.Text.Filter}}" aria-label="{{.Text.Filter}}" hidden>
<table>
<thead><tr><th>{{.Text.Name}}</th><th class="size">{{.Text.Size}}</th><th class="time">{{.Text.Modified}}</th></tr></thead>
<tbody>
{{if ne .Index.Path "/"}}<tr><td class="name"><a href="../">../</a></td><td class="size"></td><td class="time">{{.Text.Parent}}</td></tr>
{{end}}{{range .Rows}}<tr data-name="{{.Name}}"><td class="name"><a href="{{.Href}}">{{.Label}}</a></td><td class="size"{{if .Bytes}} title="{{.Bytes}}"{{end}}>{{.Size}}</td><td class="time">{{.Modified}}</td></tr>
{{end}}</tbody>
</table>
{{if not .Rows}}<p class="empty">{{.Text.Empty}}</p>{{else}}<p id="no-match" class="empty" hidden>{{.Text.NoMatch}}</p>{{end}}
</main>
<script>{{.JS}}</script>
</body>
</html>
`))

type indexRow struct {
	Name, Label, Href, Size, Bytes, Modified string
}

// writeFileIndex answers a directory request with JSON or an HTML page.
func (s *Server) writeFileIndex(w http.ResponseWriter, r *http.Request, entry application.Entry, index fileIndex, fetchedAt time.Time) {
	h := w.Header()
	h.Set("Vary", "Accept")
	h.Set("Cache-Control", "no-cache")
	if wantsJSON(r) {
		if index.Entries == nil {
			index.Entries = []fileIndexEntry{}
		}
		writeOK(w, index)
		return
	}
	text := indexTexts[indexLanguage(r)]
	rows := make([]indexRow, 0, len(index.Entries))
	for _, e := range index.Entries {
		row := indexRow{Name: e.Name, Label: e.Name, Href: indexHref(e)}
		if e.Type == "directory" {
			row.Label += "/"
		}
		if e.Size != nil {
			row.Size, row.Bytes = formatSize(*e.Size), strconv.FormatInt(*e.Size, 10)
		}
		if e.Modified != nil {
			row.Modified = e.Modified.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, row)
	}
	appName := entry.Descriptor.Name[text.Lang]
	if appName == "" {
		appName = entry.Descriptor.Name["en"]
	}
	data := map[string]any{
		"Text":    text,
		"App":     appName,
		"AppPath": "/" + entry.Descriptor.ID,
		"Index":   index,
		"Rows":    rows,
		"CSS":     template.CSS(fileIndexCSS),
		"JS":      template.JS(fileIndexJS),
	}
	if index.Truncated {
		data["Truncated"] = fmt.Sprintf(text.Truncated, maxIndexEntries)
	}
	if index.Stale {
		data["Stale"] = fmt.Sprintf(text.Stale, fetchedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	var body bytes.Buffer
	if err := fileIndexTemplate.Execute(&body, data); err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", fileIndexPolicy)
	h.Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

// redirectToDirectory answers a directory path without its trailing slash. The
// redirect is temporary: the directory may later be replaced by a file.
func redirectToDirectory(w http.ResponseWriter, entry application.Entry, relative string) {
	location := url.URL{Path: "/" + entry.Descriptor.ID + "/" + relative + "/"}
	w.Header().Set("Location", location.EscapedPath())
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// serveFileIndex lists dir ("" is the root) of a hosted or http-cache
// application with auto_index. Listings are not downloads: they are not
// ranked and take no download slot.
func (s *Server) serveFileIndex(w http.ResponseWriter, r *http.Request, entry application.Entry, dir string) {
	if dir != "" && !store.ValidHostedPath(dir) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid directory path")
		return
	}
	if !s.countRequest(w, r) {
		return
	}
	r, finish, ok := s.applicationLease(w, r, entry.StorageID())
	if !ok {
		return
	}
	defer finish()
	index := fileIndex{Path: "/", Entries: []fileIndexEntry{}}
	if dir != "" {
		index.Path = "/" + dir + "/"
	}
	var fetchedAt time.Time
	switch entry.Provider {
	case application.Hosted:
		entries, more, err := s.store.HostedDirectory(entry.UID, dir, maxIndexEntries)
		if err != nil {
			s.writeError(w, r, storageError(err))
			return
		}
		if dir != "" && len(entries) == 0 {
			s.fail(w, r, codeFileNotFound, nil, "Directory not found")
			return
		}
		index.Truncated = more
		for _, e := range entries {
			item := fileIndexEntry{Name: e.Name, Type: "file", Modified: &e.CreatedAt}
			if e.Directory {
				item.Type = "directory"
			} else {
				item.Size = &e.SizeBytes
			}
			index.Entries = append(index.Entries, item)
		}
	case application.HttpCache:
		if dir != "" {
			if _, err := entry.Upstream.RelativeURL(dir); err != nil {
				s.fail(w, r, codeInvalidPath, nil, "Invalid directory path")
				return
			}
		}
		listing, err := s.httpCache.Index(r.Context(), entry, dir, maxIndexEntries)
		if err != nil {
			if errors.Is(err, httpcache.ErrIndexUnsupported) {
				s.fail(w, r, codeIndexUnsupported, err, "The source does not list this directory")
			} else {
				s.writeError(w, r, cacheFileError(err))
			}
			return
		}
		index.Stale, index.Truncated, fetchedAt = listing.Stale, listing.Truncated, listing.FetchedAt
		for _, e := range listing.Entries {
			item := fileIndexEntry{Name: e.Name, Type: "file", Size: e.Size, Modified: e.Modified}
			if e.Directory {
				item.Type = "directory"
			}
			index.Entries = append(index.Entries, item)
		}
	default:
		s.fail(w, r, codeFileNotFound, nil, "File not found")
		return
	}
	s.writeFileIndex(w, r, entry, index, fetchedAt)
}
