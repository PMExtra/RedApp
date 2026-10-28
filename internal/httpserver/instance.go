package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/PMExtra/RedApp/internal/distributor"
)

type instanceDTO struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
}

// getInstance identifies this RedApp instance to another one that uses it as
// an upstream, for example to refuse binding an instance to itself.
func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	writeOK(w, instanceDTO{InstanceID: s.instanceUID, Version: s.version})
}

// loopDetected refuses a request that already passed through this instance,
// before it can trigger another upstream request: a chain of instances whose
// sources lead back to itself. It writes the error and reports true.
func (s *Server) loopDetected(w http.ResponseWriter, r *http.Request) bool {
	if !distributor.ViaContains(r.Header.Values("Via"), distributor.ViaPseudonym(s.instanceUID)) {
		return false
	}
	s.fail(w, r, codeLoopDetected, nil, "The request already passed through this RedApp instance")
	return true
}

type mirrorVendorDTO struct {
	ID          string        `json:"id"`
	Name        localizedText `json:"name"`
	Description localizedText `json:"description"`
	Icon        string        `json:"icon"`
}

type mirrorAppDTO struct {
	InstanceID    string          `json:"instance_id"`
	Key           string          `json:"key"`
	Provider      string          `json:"provider"`
	Vendor        mirrorVendorDTO `json:"vendor"`
	Name          localizedText   `json:"name"`
	Description   localizedText   `json:"description"`
	Icon          string          `json:"icon"`
	Instructions  localizedText   `json:"instructions"`
	AutoIndex     bool            `json:"auto_index"`
	Channels      []string        `json:"channels"`
	UpstreamChain []string        `json:"upstream_chain"`
	Revision      string          `json:"revision"`
}

// getMirrorApp describes a published application for a downstream RedApp that
// mirrors it: the display fields it follows (with the instructions source, not
// the rendered document) and what it needs to reproduce the content. The
// upstream chain lists the instances the content passes through before this
// one, so a downstream refuses a loop.
func (s *Server) getMirrorApp(w http.ResponseWriter, r *http.Request) {
	e, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	instructions, err := s.store.Instructions(e.UID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := mirrorAppDTO{
		InstanceID:    s.instanceUID,
		Key:           e.Descriptor.ID,
		Provider:      e.Provider,
		Vendor:        mirrorVendorDTO{ID: e.VendorID, Name: fromLocalized(e.VendorName), Description: fromLocalized(e.VendorDescription), Icon: publicIcon(e.VendorIcon)},
		Name:          fromLocalized(e.Descriptor.Name),
		Description:   fromLocalized(e.Descriptor.Summary),
		Icon:          publicIcon(e.Descriptor.Icon),
		Instructions:  localizedText{En: instructions.En, ZhCN: instructions.ZhCN},
		AutoIndex:     e.AutoIndex,
		Channels:      []string{},
		UpstreamChain: []string{},
	}
	if e.Protocol != nil {
		out.Channels = append(out.Channels, e.Descriptor.Channels...)
	}
	identity, _ := json.Marshal(out)
	digest := sha256.Sum256(identity)
	out.Revision = hex.EncodeToString(digest[:16])
	writeOK(w, out)
}
