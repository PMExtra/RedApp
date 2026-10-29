package httpserver

import (
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

// An application icon that brings its own background is flagged in the admin
// and public documents; a vendor icon standing in for a missing one never is.
func TestApplicationIconWithBackground(t *testing.T) {
	h := newHarness(t)
	h.login("")
	h.createVendor("acme")
	h.patchConfiguration("vendors/acme", map[string]any{"set": map[string]any{"icon": h.uploadIcon(svgIcon("#111111"), 201)}}, 200)
	app := h.createApp("acme", "framed", application.HttpCache, map[string]any{"base_url": "https://files.example/root"})
	icon := h.uploadIcon(svgIcon("#222222"), 201)
	h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"icon": icon, "icon_has_background": true}}, 200)
	flags := func() (bool, bool) {
		t.Helper()
		admin := getJSON[appDTO](h, "/admin/api/apps/"+app.Key)
		_, public := h.publicCatalog()
		return admin.IconHasBackground, public[app.Key].IconHasBackground
	}
	if admin, public := flags(); !admin || !public {
		t.Fatal("own icon with background", admin, public)
	}
	c := h.configuration("apps/" + app.Key)
	if c.Effective["icon_has_background"] != true || c.Fields["icon_has_background"].Source != "custom" {
		t.Fatal("configuration", c.Effective["icon_has_background"], c.Fields["icon_has_background"])
	}
	h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"icon": ""}}, 200)
	if admin, public := flags(); admin || public {
		t.Fatal("vendor icon flagged", admin, public)
	}
	h.patchConfiguration("apps/"+app.Key, map[string]any{"set": map[string]any{"icon_has_background": "yes"}}, 400)
}
