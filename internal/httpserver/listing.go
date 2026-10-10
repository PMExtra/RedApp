package httpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
)

type listCursor struct {
	Version     int    `json:"v"`
	Application string `json:"app"`
	Endpoint    string `json:"endpoint"`
	Filter      string `json:"filter"`
	Last        string `json:"last"`
	SourceEpoch int64  `json:"source_epoch,omitempty"`
}
type listPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type listedVersion struct {
	Version   string    `json:"version"`
	FirstSeen time.Time `json:"first_seen"`
	Requests  int64     `json:"requests"`
	Bytes     int64     `json:"bytes"`
}

func parseListQuery(r *http.Request, app, endpoint string, sourceEpoch ...int64) (limit int, version, last string, err error) {
	limit = 50
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		err = errors.New("Invalid list query")
		return
	}
	for key, values := range q {
		if len(values) != 1 || (key != "limit" && key != "cursor" && !(endpoint == "resources" && key == "version")) {
			err = errors.New("Unknown or repeated list query parameter")
			return
		}
		if values[0] == "" {
			err = errors.New("List query values must not be empty")
			return
		}
	}
	if raw, ok := q["limit"]; ok {
		limit, e = strconv.Atoi(raw[0])
		if e != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != raw[0] {
			err = errors.New("List limit must be between 1 and 100")
			return
		}
	}
	version = q.Get("version")
	if len(version) > 128 {
		err = errors.New("Invalid version filter")
		return
	}
	if encoded, ok := q["cursor"]; ok {
		if len(encoded[0]) > 2048 {
			err = errors.New("Invalid list cursor")
			return
		}
		raw, e := base64.RawURLEncoding.Strict().DecodeString(encoded[0])
		if e != nil || base64.RawURLEncoding.EncodeToString(raw) != encoded[0] || jsoncheck.Unique(raw) != nil {
			err = errors.New("Invalid list cursor")
			return
		}
		var c listCursor
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		epoch := int64(0)
		if len(sourceEpoch) > 0 {
			epoch = sourceEpoch[0]
		}
		if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || c.Version != 1 || c.Application != app || c.Endpoint != endpoint || c.Filter != version || c.Last == "" || len(c.Last) > 128 || c.SourceEpoch != epoch {
			err = errors.New("List cursor does not match this request")
			return
		}
		last = c.Last
	}
	return
}
func nextListCursor(app, endpoint, filter, last string, sourceEpoch ...int64) *string {
	var epoch int64
	if len(sourceEpoch) > 0 {
		epoch = sourceEpoch[0]
	}
	raw, _ := json.Marshal(listCursor{Version: 1, Application: app, Endpoint: endpoint, Filter: filter, Last: last, SourceEpoch: epoch})
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return &encoded
}

func (s *Server) applicationList(w http.ResponseWriter, r *http.Request, app, endpoint string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "List endpoints require GET")
		return
	}
	entry, ok := s.registry.LookupAny(app)
	if !ok || entry.Protocol == nil || endpoint != "versions" && endpoint != "resources" {
		fail(w, 404, "Application list not found")
		return
	}
	if r.URL.Query().Has("page") {
		s.numberedApplicationList(w, r, entry, endpoint)
		return
	}
	limit, version, last, err := parseListQuery(r, app, endpoint, entry.SourceEpoch)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if version != "" {
		canonical, err := entry.Protocol.ValidateVersion(version)
		if err != nil || canonical != version {
			fail(w, 400, "Invalid canonical version filter")
			return
		}
	}
	if endpoint == "versions" {
		if last != "" {
			canonical, err := entry.Protocol.ValidateVersion(last)
			if err != nil || canonical != last {
				fail(w, 400, "Invalid version cursor")
				return
			}
		}
		rows, err := s.store.VersionPage(entry.StorageID(), last, limit+1)
		if err != nil {
			fail(w, 503, "Failed to read application versions")
			return
		}
		page := listPage[listedVersion]{Items: make([]listedVersion, 0)}
		if len(rows) > limit {
			rows = rows[:limit]
			page.NextCursor = nextListCursor(app, endpoint, "", rows[len(rows)-1].Version, entry.SourceEpoch)
		}
		for _, row := range rows {
			page.Items = append(page.Items, listedVersion{Version: row.Version, FirstSeen: row.FirstSeen, Requests: row.ArtifactRequests, Bytes: row.DownstreamBytes})
		}
		reply(w, 200, page)
		return
	}
	if last != "" {
		_, err := hex.DecodeString(last)
		if err != nil || (len(last) != 32 && len(last) != 64) || strings.ToLower(last) != last {
			fail(w, 400, "Invalid resource cursor")
			return
		}
	}
	if s.downloads == nil {
		fail(w, 503, "Download state is unavailable")
		return
	}
	items := make([]download.View, 0)
	views, err := s.resourceViews()
	if err != nil {
		fail(w, 503, "Resource state is unavailable")
		return
	}
	for _, view := range views {
		if view.Resource.Application == entry.StorageID() && (version == "" || view.Resource.Version == version) && view.ID > last {
			items = append(items, view)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	page := listPage[download.View]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = nextListCursor(app, endpoint, version, page.Items[limit-1].ID, entry.SourceEpoch)
	}
	page.Items = s.publicViews(page.Items)
	reply(w, 200, page)
}
