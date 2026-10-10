package httpserver

import (
	"encoding/json"
	"testing"

	"github.com/PMExtra/RedApp/internal/store"
)

// Fixtures of the maintenance tests (releases, retention, prewarm, hosted
// files). They change configuration through the store, so these tests do
// not depend on the directory and configuration API.

func configValue(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// patchApp sets configuration paths of key at its current revision and
// returns the new revision.
func (h *harness) patchApp(key string, set map[string]any) int64 {
	h.t.Helper()
	a, err := h.store.Application(key)
	if err != nil {
		h.t.Fatal(err)
	}
	patch := store.ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{}}
	for path, value := range set {
		patch.Set[path] = configValue(h.t, value)
	}
	c, err := h.store.PatchApplicationConfiguration(key, patch)
	if err != nil {
		h.t.Fatal(err)
	}
	return c.Revision
}

// setAppEnabled enables or disables key.
func (h *harness) setAppEnabled(key string, enabled bool) {
	h.t.Helper()
	a, err := h.store.Application(key)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err = h.store.PatchApplicationFields(key, a.Revision, nil, &enabled); err != nil {
		h.t.Fatal(err)
	}
}

// setVendorEnabled enables or disables vendor.
func (h *harness) setVendorEnabled(vendor string, enabled bool) {
	h.t.Helper()
	v, err := h.store.Vendor(vendor)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err = h.store.PatchVendorFields(vendor, v.Revision, nil, &enabled); err != nil {
		h.t.Fatal(err)
	}
}

// markDeleted starts the deletion of key without finishing it, leaving the
// application deleted and read-only.
func (h *harness) markDeleted(key string) {
	h.t.Helper()
	a, err := h.store.Application(key)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, _, err = h.store.PrepareApplicationDeletion(key, a.Revision); err != nil {
		h.t.Fatal(err)
	}
}

// appRevision is the revision (ETag) of key's configuration.
func (h *harness) appRevision(key string) int64 {
	h.t.Helper()
	a, err := h.store.Application(key)
	if err != nil {
		h.t.Fatal(err)
	}
	return a.Revision
}

func ifMatchHeader(revision int64) map[string]string {
	return map[string]string{"If-Match": etag(revision)}
}
