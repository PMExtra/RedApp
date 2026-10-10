package httpserver

import (
	"net/http"
	"sort"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
)

func (s *Server) numberedApplicationList(w http.ResponseWriter, r *http.Request, entry application.Entry, endpoint string) {
	allowed := []string{"page", "limit"}
	if endpoint == "resources" {
		allowed = append(allowed, "version")
	}
	if !queryAllowed(r, allowed...) {
		fail(w, 400, "Invalid page query")
		return
	}
	p, ok := positivePage(r.URL.Query().Get("page"), 1)
	limit, okLimit := positivePage(r.URL.Query().Get("limit"), 25)
	if !ok || !okLimit || limit > 100 || r.URL.Query().Get("page") == "" {
		fail(w, 400, "Invalid page or limit")
		return
	}
	if endpoint == "versions" {
		rows, err := s.store.VersionNumberPage(entry.StorageID(), p, limit)
		if err != nil {
			fail(w, 503, "Failed to read application versions")
			return
		}
		result := store.NewPage[listedVersion](rows.Page, rows.Limit, rows.Total)
		for _, v := range rows.Items {
			result.Items = append(result.Items, listedVersion{Version: v.Version, FirstSeen: v.FirstSeen, Requests: v.ArtifactRequests, Bytes: v.DownstreamBytes})
		}
		reply(w, 200, result)
		return
	}
	version := r.URL.Query().Get("version")
	if r.URL.Query().Has("version") {
		v, err := entry.Protocol.ValidateVersion(version)
		if err != nil || version != v {
			fail(w, 400, "Invalid canonical version filter")
			return
		}
	}
	if s.downloads == nil {
		fail(w, 503, "Download state is unavailable")
		return
	}
	views := s.downloads.SnapshotFor(entry.StorageID(), version)
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	result := store.NewPage[download.View](p, limit, int64(len(views)))
	start := min((result.Page-1)*limit, len(views))
	end := min(start+limit, len(views))
	result.Items = s.publicViews(views[start:end])
	reply(w, 200, result)
}
