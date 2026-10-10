package httpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/store"
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

func (s *Server) eventList(w http.ResponseWriter, r *http.Request, app string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "List endpoints require GET")
		return
	}
	metricScope := app
	if app != "" {
		entry, ok := s.registry.LookupAny(app)
		if !ok {
			fail(w, 404, "Application not found")
			return
		}
		metricScope = entry.MetricsID()
	}
	limit, _, last, err := parseListQuery(r, app, "events")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	var before int64
	if last != "" {
		before, err = strconv.ParseInt(last, 10, 64)
		if err != nil || before <= 0 || strconv.FormatInt(before, 10) != last {
			fail(w, 400, "Invalid event cursor")
			return
		}
	}
	rows, err := s.store.EventPage(metricScope, before, limit+1)
	if err != nil {
		fail(w, 503, "Failed to read events")
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		next = nextListCursor(app, "events", "", strconv.FormatInt(rows[limit-1].ID, 10))
	}
	scopes := s.publicScopes()
	for i := range rows {
		rows[i].AppID = publicScope(rows[i].AppID, scopes)
	}
	reply(w, 200, listPage[store.ListedEvent]{Items: rows, NextCursor: next})
}
