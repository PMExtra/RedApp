package httpserver

import (
	"net/http"
	"strings"
)

type authLevel int

const (
	authNone  authLevel = iota
	authAdmin           // session cookie; X-CSRF-Token on unsafe methods
)

// route is one operation of api/openapi.yaml. TestRouteTableMatchesSpec checks
// that this table and the specification list the same operations with the same
// query parameters, body limits and security.
type route struct {
	method    string
	path      string // path template exactly as in api/openapi.yaml
	operation string // operationId
	serve     http.HandlerFunc
	auth      authLevel
	query     []string // allowed query parameters (x-spa-routes / operation parameters)
	maxBody   int64    // x-max-body-bytes
	// handlerChecksQuery leaves query validation to the handler, for routes
	// whose error code depends on the resolved resource (distribution files).
	handlerChecksQuery bool
	// servedBy names the operation whose registration also answers this one:
	// HEAD through the GET pattern, installers through the file route.
	servedBy string
	// paths registers the handler for each listed path instead of path; used
	// for /admin/{ui_path}, which ServeMux cannot register next to /{vendor}/{app}.
	paths []string
	// legacy marks an admin operation still served by the pre-contract handler
	// (legacy.go). It keeps the old paths and response shapes until migrated.
	legacy bool
}

// routeTable is the registration table, grouped by migration package.
func (s *Server) routeTable() []route {
	return []route{
		// Foundation: health, public API, pages, assets, distribution, auth.
		{method: "GET", path: "/health/live", operation: "getLiveness", serve: s.getLiveness},
		{method: "GET", path: "/health/ready", operation: "getReadiness", serve: s.getReadiness},
		{method: "GET", path: "/api/bootstrap", operation: "getBootstrap", serve: s.getBootstrap},
		{method: "GET", path: "/api/home", operation: "getHome", serve: s.getHome},
		{method: "GET", path: "/api/catalog", operation: "listCatalog", serve: s.listCatalog, query: []string{"q", "vendor", "category", "page", "limit"}},
		{method: "GET", path: "/api/search", operation: "searchCatalog", serve: s.searchCatalog, query: []string{"q"}},
		{method: "GET", path: "/api/vendors/{vendor}", operation: "getPublicVendor", serve: s.getPublicVendor},
		{method: "GET", path: "/api/apps/{vendor}/{app}", operation: "getPublicApp", serve: s.getPublicApp},
		{method: "GET", path: "/api/apps/{vendor}/{app}/files", operation: "listPublicHostedFiles", serve: s.listPublicHostedFiles, query: []string{"page", "limit"}},
		{method: "GET", path: "/api/apps/{vendor}/{app}/instructions/document", operation: "getInstructionsDocument", serve: s.getInstructionsDocument, query: []string{"lang"}},
		{method: "GET", path: "/", operation: "getHomePage", serve: s.getHomePage},
		{method: "GET", path: "/all", operation: "getCatalogPage", serve: s.getCatalogPage, query: []string{"q", "category", "page"}},
		{method: "GET", path: "/admin", operation: "redirectAdmin", serve: s.redirectAdmin},
		{method: "GET", path: "/admin/{ui_path}", operation: "getAdminPage", serve: s.getAdminPage, query: []string{"returnTo", "q", "state", "page", "cleanup"}, paths: adminSPARoutes},
		{method: "GET", path: "/{vendor}", operation: "getVendorPage", serve: s.getVendorPage, query: []string{"q", "page"}},
		{method: "GET", path: "/{vendor}/{app}", operation: "getAppPage", serve: s.getAppPage},
		{method: "GET", path: "/assets/{file}", operation: "getBuildAsset", serve: s.getBuildAsset},
		{method: "GET", path: "/assets/icons/{icon_file}", operation: "getUploadedIcon", serve: s.getUploadedIcon},
		{method: "HEAD", path: "/assets/icons/{icon_file}", operation: "headUploadedIcon", servedBy: "getUploadedIcon"},
		{method: "GET", path: "/assets/presets/{preset_path}", operation: "getPresetImage", serve: s.getPresetImage},
		{method: "HEAD", path: "/assets/presets/{preset_path}", operation: "headPresetImage", servedBy: "getPresetImage"},
		{method: "GET", path: "/{vendor}/{app}/install.sh", operation: "getShellInstaller", servedBy: "getDistributionFile"},
		{method: "GET", path: "/{vendor}/{app}/install.ps1", operation: "getPowerShellInstaller", servedBy: "getDistributionFile"},
		{method: "GET", path: "/{vendor}/{app}/{file_path}", operation: "getDistributionFile", serve: s.getDistributionFile, handlerChecksQuery: true},
		{method: "HEAD", path: "/{vendor}/{app}/{file_path}", operation: "headDistributionFile", servedBy: "getDistributionFile"},
		{method: "POST", path: "/admin/api/session", operation: "createSession", serve: s.createSession, maxBody: 8192},
		{method: "GET", path: "/admin/api/session", operation: "getSession", serve: s.getSession, auth: authAdmin},
		{method: "DELETE", path: "/admin/api/session", operation: "deleteSession", serve: s.deleteSession, auth: authAdmin},
		{method: "POST", path: "/admin/api/password", operation: "changePassword", serve: s.changePassword, auth: authAdmin, maxBody: 8192},

		// Package 1: directory, configuration, admin notes, categories, exchange.
		{method: "GET", path: "/admin/api/providers", operation: "listProviders", serve: s.listProviders, auth: authAdmin},
		{method: "GET", path: "/admin/api/vendors", operation: "listVendors", serve: s.listVendors, auth: authAdmin, query: []string{"q", "state", "page", "limit"}},
		{method: "POST", path: "/admin/api/vendors", operation: "createVendor", serve: s.createVendor, auth: authAdmin, maxBody: 65536},
		{method: "GET", path: "/admin/api/vendors/{vendor}", operation: "getVendor", serve: s.getVendor, auth: authAdmin},
		{method: "PATCH", path: "/admin/api/vendors/{vendor}", operation: "updateVendor", serve: s.updateVendor, auth: authAdmin, maxBody: 8192},
		{method: "DELETE", path: "/admin/api/vendors/{vendor}", operation: "deleteVendor", serve: s.deleteVendor, auth: authAdmin},
		{method: "GET", path: "/admin/api/apps", operation: "listApps", serve: s.listApps, auth: authAdmin, query: []string{"vendor", "q", "state", "sort", "order", "lang", "page", "limit"}},
		{method: "POST", path: "/admin/api/apps", operation: "createApp", serve: s.createApp, auth: authAdmin, maxBody: 65536},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}", operation: "getApp", serve: s.getApp, auth: authAdmin},
		{method: "PATCH", path: "/admin/api/apps/{vendor}/{app}", operation: "updateApp", serve: s.updateApp, auth: authAdmin, maxBody: 8192},
		{method: "DELETE", path: "/admin/api/apps/{vendor}/{app}", operation: "deleteApp", serve: s.deleteApp, auth: authAdmin, query: []string{"confirm_uid"}},
		{method: "POST", path: "/admin/api/icons", operation: "uploadIcon", serve: s.uploadIcon, auth: authAdmin, maxBody: 2162688},
		{method: "GET", path: "/admin/api/vendors/{vendor}/configuration", operation: "getVendorConfiguration", serve: s.getVendorConfiguration, auth: authAdmin},
		{method: "PATCH", path: "/admin/api/vendors/{vendor}/configuration", operation: "patchVendorConfiguration", serve: s.patchVendorConfiguration, auth: authAdmin, maxBody: 262144},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/configuration", operation: "getAppConfiguration", serve: s.getAppConfiguration, auth: authAdmin},
		{method: "PATCH", path: "/admin/api/apps/{vendor}/{app}/configuration", operation: "patchAppConfiguration", serve: s.patchAppConfiguration, auth: authAdmin, maxBody: 262144},
		{method: "GET", path: "/admin/api/vendors/{vendor}/admin-notes", operation: "getVendorNotes", serve: s.getNotes, auth: authAdmin},
		{method: "PUT", path: "/admin/api/vendors/{vendor}/admin-notes", operation: "replaceVendorNotes", serve: s.replaceNotes, auth: authAdmin, maxBody: 131072},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/admin-notes", operation: "getAppNotes", serve: s.getNotes, auth: authAdmin},
		{method: "PUT", path: "/admin/api/apps/{vendor}/{app}/admin-notes", operation: "replaceAppNotes", serve: s.replaceNotes, auth: authAdmin, maxBody: 131072},
		{method: "GET", path: "/admin/api/categories", operation: "listCategories", serve: s.listCategories, auth: authAdmin, query: []string{"q", "page", "limit"}},
		{method: "GET", path: "/admin/api/categories/{category}", operation: "getCategory", serve: s.getCategory, auth: authAdmin},
		{method: "PATCH", path: "/admin/api/categories/{category}", operation: "patchCategory", serve: s.patchCategory, auth: authAdmin, maxBody: 8192},
		{method: "POST", path: "/admin/api/configuration/export", operation: "exportConfiguration", serve: s.exportConfiguration, auth: authAdmin, maxBody: 262144},
		{method: "POST", path: "/admin/api/configuration/import/preview", operation: "previewImport", serve: s.previewImport, auth: authAdmin, maxBody: 34603008},
		{method: "POST", path: "/admin/api/configuration/import/{preview_id}/execute", operation: "executeImport", serve: s.executeImport, auth: authAdmin, maxBody: 8192},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/copy", operation: "copyApp", serve: s.copyApp, auth: authAdmin, maxBody: 65536},

		// Package 2: releases, retention, prewarm, hosted file administration.
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/versions", operation: "listVersions", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"limit", "cursor"}},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/resources", operation: "listResources", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"version", "limit", "cursor"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/version-cleanup/preview", operation: "previewVersionCleanup", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/version-cleanup/{preview_id}/execute", operation: "executeVersionCleanup", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/retention/status", operation: "getRetentionStatus", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/retention/preview", operation: "previewRetention", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/retention/{preview_id}", operation: "getRetentionPreview", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/items", operation: "listRetentionPreviewItems", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"page", "limit"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/retention/{preview_id}/execute", operation: "executeRetention", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/prewarm/options", operation: "getPrewarmOptions", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/prewarm/jobs", operation: "startPrewarm", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8388608},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}", operation: "getPrewarmJob", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/items", operation: "listPrewarmItems", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"page", "limit"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/cancel", operation: "cancelPrewarmJob", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/prewarm/jobs/{job_id}/retry", operation: "retryPrewarmJob", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/files", operation: "listHostedFiles", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"page", "limit"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/files", operation: "uploadHostedFile", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"transfer_id"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/files/import", operation: "importHostedFile", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"transfer_id"}, maxBody: 16384},
		{method: "DELETE", path: "/admin/api/apps/{vendor}/{app}/files/{file_id}", operation: "deleteHostedFile", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", operation: "getHostedTransfer", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "DELETE", path: "/admin/api/apps/{vendor}/{app}/files/transfers/{transfer_id}", operation: "cancelHostedTransfer", serve: s.legacyAdmin, auth: authAdmin, legacy: true},

		// Package 3: HTTP cache administration, overview, settings.
		{method: "GET", path: "/admin/api/status", operation: "getStatus", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/history", operation: "getHistory", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"metric", "range"}},
		{method: "GET", path: "/admin/api/events", operation: "listEvents", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"limit", "cursor"}},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/status", operation: "getAppStatus", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/history", operation: "getAppHistory", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"metric", "range"}},
		{method: "GET", path: "/admin/api/settings/site", operation: "getSiteSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "PUT", path: "/admin/api/settings/site", operation: "replaceSiteSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/settings/public-url", operation: "getPublicUrlSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "PUT", path: "/admin/api/settings/public-url", operation: "replacePublicUrlSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/settings/proxy", operation: "getGlobalProxySettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "PUT", path: "/admin/api/settings/proxy", operation: "replaceGlobalProxySettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/settings/homepage", operation: "getHomepageSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "PUT", path: "/admin/api/settings/homepage", operation: "replaceHomepageSettings", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 16384},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/sources", operation: "listSources", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/cache/entries", operation: "listCacheEntries", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"source_epoch", "limit", "cursor"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/cache/refresh", operation: "refreshCacheEntry", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/cache/refresh/preview", operation: "previewCacheRefresh", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}", operation: "getCacheRefresh", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/items", operation: "listCacheRefreshItems", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"limit", "cursor"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/execute", operation: "executeCacheRefresh", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/cache/cleanup/preview", operation: "previewCacheCleanup", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}", operation: "getCacheCleanup", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/items", operation: "listCacheCleanupItems", serve: s.legacyAdmin, auth: authAdmin, legacy: true, query: []string{"limit", "cursor"}},
		{method: "POST", path: "/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/execute", operation: "executeCacheCleanup", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "GET", path: "/admin/api/cache/auto-cleanup", operation: "getAutoCleanupStatus", serve: s.legacyAdmin, auth: authAdmin, legacy: true},
		{method: "POST", path: "/admin/api/path-match", operation: "testPathMatch", serve: s.legacyAdmin, auth: authAdmin, legacy: true, maxBody: 8192},
	}
}

// legacyCatchAll keeps the pre-contract admin paths that no longer exist in the
// specification reachable until their area is migrated. Delete it together
// with legacy.go.
var legacyCatchAll = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// muxPattern converts a specification path template into a ServeMux pattern:
// greedy parameters (x-greedy) take the rest of the path and "/" matches only
// itself.
func muxPattern(method, path string) string {
	if path == "/" {
		return method + " /{$}"
	}
	for _, greedy := range []string{"{ui_path}", "{preset_path}", "{file_path}"} {
		path = strings.Replace(path, greedy, greedy[:len(greedy)-1]+"...}", 1)
	}
	return method + " " + path
}

// register installs every route on mux. HEAD is answered by GET patterns.
func (s *Server) register(mux *http.ServeMux) {
	for i := range s.routes {
		rt := &s.routes[i]
		if rt.servedBy != "" {
			continue
		}
		paths := rt.paths
		if paths == nil {
			paths = []string{rt.path}
		}
		for _, p := range paths {
			mux.Handle(muxPattern(rt.method, p), s.handler(rt))
		}
	}
	legacy := &route{path: "/admin/api/", operation: "legacyAdmin", serve: s.legacyAdmin, auth: authAdmin, legacy: true}
	for _, method := range legacyCatchAll {
		mux.Handle(method+" /admin/api/", s.handler(legacy))
	}
}
