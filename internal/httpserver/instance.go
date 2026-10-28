package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/PMExtra/RedApp/internal/application"
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

// loopDetected refuses a request that the application itself sent: its
// source leads back to the same application, which would wait on itself.
// It runs only where a request can read the application's source. Chains
// across several applications or instances are refused when mirrors are
// bound (upstream_chain), because shared transfers do not carry the Via of
// the requests that started them. It writes the error and reports true.
func (s *Server) loopDetected(w http.ResponseWriter, r *http.Request, entry application.Entry) bool {
	if !distributor.ViaContains(r.Header.Values("Via"), distributor.ViaPseudonym(s.instanceUID, entry.UID)) {
		return false
	}
	s.fail(w, r, codeLoopDetected, nil, "The request came from this application: its source leads back to itself")
	return true
}

type mirrorVendorDTO struct {
	ID          string        `json:"id"`
	Name        localizedText `json:"name"`
	Description localizedText `json:"description"`
	Icon        string        `json:"icon"`
}

type mirrorAppDTO struct {
	InstanceID     string          `json:"instance_id"`
	Key            string          `json:"key"`
	Provider       string          `json:"provider"`
	Vendor         mirrorVendorDTO `json:"vendor"`
	Name           localizedText   `json:"name"`
	Description    localizedText   `json:"description"`
	Icon           string          `json:"icon"`
	Instructions   localizedText   `json:"instructions"`
	AutoIndex      bool            `json:"auto_index"`
	Channels       []string        `json:"channels"`
	DefaultChannel *string         `json:"default_channel"`
	UpstreamChain  []string        `json:"upstream_chain"`
	SourceEpoch    int64           `json:"source_epoch"`
	Revision       string          `json:"revision"`
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
		SourceEpoch:   e.SourceEpoch,
	}
	if e.Protocol != nil {
		out.Channels = append(out.Channels, e.Descriptor.Channels...)
	}
	// "latest" names the default channel; JSON releases configure which.
	if p, ok := e.Protocol.(application.SnapshotProtocol); ok {
		if channel, err := p.CanonicalChannel("latest"); err == nil {
			out.DefaultChannel = &channel
		}
	}
	// A mirrored application passes on its own upstream and that one's chain.
	instance, chain, mirrored, err := s.store.MirroredFrom(e.UID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	if mirrored {
		out.UpstreamChain = append([]string{instance}, chain...)
	}
	identity, _ := json.Marshal(out)
	digest := sha256.Sum256(identity)
	out.Revision = hex.EncodeToString(digest[:16])
	writeOK(w, out)
}
