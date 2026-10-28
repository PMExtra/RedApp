package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/upstream"
)

type upstreamDTO struct {
	UID        string    `json:"uid"`
	Name       string    `json:"name"`
	Origin     string    `json:"origin"`
	InstanceID string    `json:"instance_id"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	Mirrors    int       `json:"mirrors"`
}

func upstreamDocument(u store.Upstream) upstreamDTO {
	return upstreamDTO{UID: u.UID, Name: u.Name, Origin: u.Origin, InstanceID: u.InstanceID, Revision: u.Revision, CreatedAt: u.CreatedAt.UTC(), Mirrors: u.Bindings}
}

// upstreamFailure maps store and upstream errors of the mirror operations.
func upstreamFailure(err error) *apiError {
	switch {
	case errors.Is(err, store.ErrUpstreamNotFound):
		return newError(codeUpstreamNotFound, nil, "Upstream not found")
	case errors.Is(err, store.ErrMirrorNotFound):
		return newError(codeMirrorNotFound, nil, "The application is not mirrored")
	case errors.Is(err, store.ErrUpstreamInUse):
		return newError(codeUpstreamInUse, nil, "Applications are mirrored from this upstream; delete or unbind them first")
	case errors.Is(err, store.ErrMirrorExists):
		return newError(codeAlreadyExists, nil, "The upstream application or the local application is already mirrored")
	case errors.Is(err, store.ErrDirectoryExists):
		return newError(codeAlreadyExists, nil, "This address or ID is already taken")
	case errors.Is(err, upstream.ErrLoop):
		return newError(codeLoopDetected, err, "The upstream chain leads back to this instance")
	case errors.Is(err, upstream.ErrRemoteNotFound):
		return newError(codeApplicationNotFound, err, "The upstream does not publish this application")
	case errors.Is(err, upstream.ErrIncompatible):
		return newError(codeUpstreamIncompatible, err, "The upstream is not a compatible RedApp instance or application")
	case errors.Is(err, upstream.ErrUnavailable):
		return newError(codeUpstreamUnavailable, err, "The upstream is unavailable")
	}
	return directoryFailure(err, codeApplicationNotFound)
}

func upstreamParam(r *http.Request) (string, *apiError) {
	uid := r.PathValue("upstream")
	if !identity.ValidUID(uid) {
		return "", newError(codeUpstreamNotFound, nil, "Upstream not found")
	}
	return uid, nil
}

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Upstreams()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := struct {
		Items []upstreamDTO `json:"items"`
	}{Items: make([]upstreamDTO, 0, len(items))}
	for _, u := range items {
		out.Items = append(out.Items, upstreamDocument(u))
	}
	writeOK(w, out)
}

type upstreamRequest struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) {
	var in upstreamRequest
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	u, err := s.upstreams.Create(r.Context(), in.Name, in.Origin)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeCreated(w, "/admin/api/upstreams/"+u.UID, u.Revision, upstreamDocument(u))
}

func (s *Server) getUpstream(w http.ResponseWriter, r *http.Request) {
	uid, e := upstreamParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	u, err := s.store.Upstream(uid)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeRevision(w, http.StatusOK, u.Revision, upstreamDocument(u))
}

func (s *Server) updateUpstream(w http.ResponseWriter, r *http.Request) {
	uid, e := upstreamParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var in upstreamRequest
	if e = decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	u, err := s.upstreams.Update(r.Context(), uid, revision, in.Name, in.Origin)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeRevision(w, http.StatusOK, u.Revision, upstreamDocument(u))
}

func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	uid, e := upstreamParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if err := s.store.DeleteUpstream(uid, revision); err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeNoContent(w)
}

type upstreamCatalogItemDTO struct {
	Key         string        `json:"key"`
	Provider    string        `json:"provider"`
	Name        localizedText `json:"name"`
	Description localizedText `json:"description"`
	Icon        string        `json:"icon"`
	Supported   bool          `json:"supported"`
	Mirror      *string       `json:"mirror"`
}

// listUpstreamCatalog reads a page of the upstream's published applications,
// with the local mirror of each.
func (s *Server) listUpstreamCatalog(w http.ResponseWriter, r *http.Request) {
	uid, e := upstreamParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	q, e := queryText(r, "q", 128)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	page, limit, e := pageQuery(r, 25)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	result, err := s.upstreams.Catalog(r.Context(), uid, q, page, limit)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	out := pageDTO[upstreamCatalogItemDTO]{Items: make([]upstreamCatalogItemDTO, 0, len(result.Items)), Page: result.Page, Limit: result.Limit, Total: result.Total, TotalPages: result.TotalPages}
	for _, item := range result.Items {
		dto := upstreamCatalogItemDTO{Key: item.Key, Provider: item.Provider, Name: localizedText{En: item.Name.En, ZhCN: item.Name.ZhCN}, Description: localizedText{En: item.Description.En, ZhCN: item.Description.ZhCN}, Icon: item.Icon, Supported: item.Supported}
		if item.Mirror != "" {
			mirror := item.Mirror
			dto.Mirror = &mirror
		}
		out.Items = append(out.Items, dto)
	}
	writeOK(w, out)
}

type mirrorRequest struct {
	RemoteKey string `json:"remote_key"`
	Vendor    string `json:"vendor"`
	App       string `json:"app"`
	// Revision adopts the existing application vendor/app at that revision.
	Revision *int64 `json:"revision"`
}

// createMirror mirrors an upstream application as a new local application,
// or adopts an existing one of the same provider.
func (s *Server) createMirror(w http.ResponseWriter, r *http.Request) {
	uid, e := upstreamParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var in mirrorRequest
	if e = decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if !identity.ValidKey(in.RemoteKey) || !identity.ValidKey(in.Vendor+"/"+in.App) {
		s.fail(w, r, codeValidationFailed, nil, "Invalid application identity")
		return
	}
	input := upstream.BindInput{RemoteKey: in.RemoteKey, Vendor: in.Vendor, ID: in.App}
	if in.Revision != nil {
		input.Adopt, input.Revision = true, *in.Revision
	}
	a, err := s.upstreams.Bind(r.Context(), uid, input)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeCreated(w, "/admin/api/apps/"+a.Key+"/mirror", a.Revision, appDocument(a))
}

type appMirrorDTO struct {
	Upstream       upstreamDTO `json:"upstream"`
	RemoteKey      string      `json:"remote_key"`
	RemoteRevision string      `json:"remote_revision"`
	UpstreamChain  []string    `json:"upstream_chain"`
	Available      bool        `json:"available"`
	SyncedAt       time.Time   `json:"synced_at"`
	CheckedAt      time.Time   `json:"checked_at"`
	Error          *string     `json:"error"`
}

func (s *Server) appMirrorDocument(b store.Binding) (appMirrorDTO, error) {
	u, err := s.store.Upstream(b.UpstreamUID)
	if err != nil {
		return appMirrorDTO{}, err
	}
	return appMirrorDTO{Upstream: upstreamDocument(u), RemoteKey: b.RemoteKey, RemoteRevision: b.RemoteRevision, UpstreamChain: b.Chain, Available: b.Available, SyncedAt: b.SyncedAt, CheckedAt: b.CheckedAt, Error: optionalText(b.ErrorCode)}, nil
}

func (s *Server) writeAppMirror(w http.ResponseWriter, r *http.Request, b store.Binding) {
	out, err := s.appMirrorDocument(b)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeOK(w, out)
}

func (s *Server) getAppMirror(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if _, err := s.store.Application(key); err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	b, err := s.store.Binding(key)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	s.writeAppMirror(w, r, b)
}

// syncAppMirror checks the upstream application now. An upstream failure is
// part of the returned state, not an error of the request.
func (s *Server) syncAppMirror(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	if _, err := s.store.Application(key); err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	b, err := s.upstreams.Sync(r.Context(), key)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	s.writeAppMirror(w, r, b)
}

// deleteAppMirror turns a mirrored application into an ordinary one that
// keeps its current values.
func (s *Server) deleteAppMirror(w http.ResponseWriter, r *http.Request) {
	key, e := appParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	a, err := s.store.UnbindApplication(key, revision)
	if err != nil {
		s.writeError(w, r, upstreamFailure(err))
		return
	}
	writeRevision(w, http.StatusOK, a.Revision, appDocument(a))
}
