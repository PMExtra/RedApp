package httpserver

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestErrorCatalogMatchesSpec(t *testing.T) {
	spec := openAPISpec(t)
	for code, class := range spec.catalog {
		got, ok := errorCatalog[errorCode(code)]
		if !ok {
			t.Errorf("spec error code %s has no Go constant", code)
		} else if got.status != class.status || got.retryable != class.retryable {
			t.Errorf("%s: Go %d/%v, spec %d/%v", code, got.status, got.retryable, class.status, class.retryable)
		}
	}
	for code := range errorCatalog {
		if _, ok := spec.catalog[string(code)]; !ok {
			t.Errorf("Go error code %s is not in components.x-error-codes", code)
		}
	}
	enum := spec.lookup("/components/schemas/ErrorCode/enum").([]any)
	if len(enum) != len(spec.catalog) {
		t.Errorf("ErrorCode enum has %d codes, catalog %d", len(enum), len(spec.catalog))
	}
	for _, code := range enum {
		if _, ok := spec.catalog[code.(string)]; !ok {
			t.Errorf("ErrorCode enum value %s is not in the catalog", code)
		}
	}
}

// TestRouteTableMatchesSpec keeps the registration table equal to the
// specification: same operations, and for migrated routes the same query
// allow-list, body limit and security.
func TestRouteTableMatchesSpec(t *testing.T) {
	spec := openAPISpec(t)
	h := newHarness(t)
	routes := map[string]route{}
	for _, rt := range h.server.routes {
		if _, dup := routes[rt.operation]; dup {
			t.Errorf("operation %s registered twice", rt.operation)
		}
		routes[rt.operation] = rt
	}
	pathItems := spec.doc["paths"].(map[string]any)
	for _, op := range spec.operations {
		rt, ok := routes[op.id]
		if !ok {
			t.Errorf("spec operation %s (%s %s) has no route", op.id, op.method, op.path)
			continue
		}
		delete(routes, op.id)
		if rt.method != op.method || rt.path != op.path {
			t.Errorf("%s: route %s %s, spec %s %s", op.id, rt.method, rt.path, op.method, op.path)
		}
		if rt.servedBy != "" {
			if other := spec.operationByID(rt.servedBy); other == nil {
				t.Errorf("%s: servedBy unknown operation %s", op.id, rt.servedBy)
			}
		} else if rt.serve == nil {
			t.Errorf("%s: no handler", op.id)
		}
		var query []string
		for _, p := range spec.parameters(pathItems[op.path].(map[string]any), op.raw) {
			if p["in"] == "query" {
				query = append(query, p["name"].(string))
			}
		}
		if !sameSet(query, rt.query) {
			t.Errorf("%s: route query %v, spec %v", op.id, rt.query, query)
		}
		limit := int64(0)
		if v, ok := op.raw["x-max-body-bytes"].(float64); ok {
			limit = int64(v)
		}
		if rt.maxBody != limit {
			t.Errorf("%s: route body limit %d, spec %d", op.id, rt.maxBody, limit)
		}
		wantAuth := authNone
		if len(asList(op.raw["security"])) > 0 {
			wantAuth = authAdmin
		}
		if rt.auth != wantAuth {
			t.Errorf("%s: route auth %d, spec %d", op.id, rt.auth, wantAuth)
		}
	}
	for id := range routes {
		t.Errorf("route %s is not in the specification", id)
	}
}

func (s *contractSpec) operationByID(id string) *specOperation {
	for _, o := range s.operations {
		if o.id == id {
			return o
		}
	}
	return nil
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func TestSPARoutesMatchSpec(t *testing.T) {
	spec := openAPISpec(t)
	var admin []string
	for _, r := range spec.lookup("/x-spa-routes/routes").([]any) {
		path := r.(string)
		if strings.HasPrefix(path, "/admin/") {
			admin = append(admin, path)
		} else if spec.match("GET", strings.NewReplacer("{vendor}", "v", "{app}", "a").Replace(path)) == nil {
			t.Errorf("public SPA route %s has no operation", path)
		}
	}
	if !sameSet(admin, adminSPARoutes) {
		t.Fatalf("adminSPARoutes %v differ from x-spa-routes %v", adminSPARoutes, admin)
	}
	var query []string
	for _, q := range spec.lookup("/x-spa-routes/allowed_query").([]any) {
		query = append(query, q.(string))
	}
	for _, op := range []string{"getAdminPage", "getCatalogPage", "getVendorPage"} {
		for _, p := range spec.parameters(spec.doc["paths"].(map[string]any)[spec.operationByID(op).path].(map[string]any), spec.operationByID(op).raw) {
			if p["in"] == "query" && !slices.Contains(query, p["name"].(string)) {
				t.Errorf("%s query %s is not in x-spa-routes.allowed_query", op, p["name"])
			}
		}
	}
}

var (
	camelCase = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	// Dotted names are configuration paths (name.zh-CN, http_policy.rules).
	snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.([a-z][a-z0-9_]*|zh-CN))*$`)
)

// TestSpecStructure is the structural check of api/openapi.yaml itself.
func TestSpecStructure(t *testing.T) {
	spec := openAPISpec(t)
	tags := map[string]bool{}
	for _, tag := range spec.doc["tags"].([]any) {
		tags[tag.(map[string]any)["name"].(string)] = true
	}
	ids := map[string]bool{}
	paths := spec.doc["paths"].(map[string]any)
	for _, op := range spec.operations {
		if !camelCase.MatchString(op.id) || ids[op.id] {
			t.Errorf("operationId %q is duplicated or not camelCase", op.id)
		}
		ids[op.id] = true
		for _, tag := range asList(op.raw["tags"]) {
			if !tags[tag.(string)] {
				t.Errorf("%s: undefined tag %s", op.id, tag)
			}
		}
		declared := map[string]bool{}
		for _, p := range spec.parameters(paths[op.path].(map[string]any), op.raw) {
			if p["in"] == "path" {
				declared[p["name"].(string)] = true
			}
		}
		for _, segment := range op.segments {
			if strings.HasPrefix(segment, "{") {
				name := strings.Trim(segment, "{}")
				if !declared[name] {
					t.Errorf("%s: path parameter %s is not declared", op.id, name)
				}
				delete(declared, name)
			}
		}
		for name := range declared {
			t.Errorf("%s: declared path parameter %s is not in the path", op.id, name)
		}
		responses := op.raw["responses"].(map[string]any)
		for _, code := range asList(op.raw["x-error-codes"]) {
			class, ok := spec.catalog[code.(string)]
			if !ok {
				t.Errorf("%s: error code %s is not in the catalog", op.id, code)
				continue
			}
			if _, ok := responses[strconv.Itoa(class.status)]; !ok {
				t.Errorf("%s: error code %s needs status %d, which is not declared", op.id, code, class.status)
			}
		}
	}
	var refs []string
	walkSpec(spec.doc, "", func(pointer string, node map[string]any) {
		if ref, ok := node["$ref"].(string); ok {
			refs = append(refs, ref)
			if !strings.HasPrefix(ref, "#/") || !resolvable(spec, strings.TrimPrefix(ref, "#")) {
				t.Errorf("%s: unresolvable $ref %s", pointer, ref)
			}
		}
		if props, ok := node["properties"].(map[string]any); ok && strings.Contains(pointer, "schema") {
			for name := range props {
				if !snakeCase.MatchString(name) && name != "zh-CN" {
					t.Errorf("%s: property %q is not snake_case", pointer, name)
				}
			}
		}
	})
	if len(refs) == 0 {
		t.Fatal("no $ref found; the walker is broken")
	}
	schemas := spec.doc["components"].(map[string]any)["schemas"].(map[string]any)
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := spec.schema("/components/schemas/" + escapePointer(name)); err != nil {
			t.Errorf("schema %s does not compile: %v", name, err)
		}
	}
}

func resolvable(spec *contractSpec, pointer string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return spec.lookup(pointer) != nil
}

func walkSpec(node any, pointer string, visit func(string, map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		visit(pointer, v)
		for key, child := range v {
			walkSpec(child, pointer+"/"+escapePointer(key), visit)
		}
	case []any:
		for i, child := range v {
			walkSpec(child, fmt.Sprintf("%s/%d", pointer, i), visit)
		}
	}
}

// The validator must reject what it is meant to catch; otherwise every
// contract assertion in the suite would pass vacuously.
func TestContractValidatorRejectsNonConformingResponses(t *testing.T) {
	spec := openAPISpec(t)
	header := func(contentType string) http.Header {
		return http.Header{"X-Request-Id": {"0123456789abcdef"}, "X-Content-Type-Options": {"nosniff"}, "Referrer-Policy": {"same-origin"}, "X-Frame-Options": {"DENY"}, "Content-Type": {contentType}}
	}
	bootstrap := spec.match("GET", "/api/bootstrap")
	valid := `{"version":"v","os":"linux","arch":"amd64","site":{"title":{"en":"a","zh-CN":"a"},"subtitle":{"en":"","zh-CN":""},"disclaimer":{"en":"","zh-CN":""}},"public_url":"http://x","revision":"r"}`
	if problems := spec.validateResponse(bootstrap, "GET", 200, header("application/json"), []byte(valid), true); len(problems) != 0 {
		t.Fatal(problems)
	}
	for name, tc := range map[string]struct {
		status int
		body   string
		header http.Header
	}{
		"missing field":      {200, strings.Replace(valid, `"os":"linux",`, "", 1), header("application/json")},
		"extra field":        {200, strings.Replace(valid, `"os":"linux"`, `"os":"linux","apps":[]`, 1), header("application/json")},
		"undeclared status":  {409, `{"error":{"code":"REVISION_CONFLICT","message":"m","request_id":"0123456789abcdef","retryable":false}}`, header("application/json")},
		"foreign error code": {404, `{"error":{"code":"APPLICATION_NOT_FOUND","message":"m","request_id":"0123456789abcdef","retryable":false}}`, header("application/json")},
		"status mismatch":    {503, `{"error":{"code":"INVALID_QUERY","message":"m","request_id":"0123456789abcdef","retryable":false}}`, header("application/json")},
		"request id differs": {400, `{"error":{"code":"INVALID_QUERY","message":"m","request_id":"ffffffffffffffff","retryable":false}}`, header("application/json")},
		"media type":         {200, valid, header("text/plain")},
		"no request id":      {200, valid, http.Header{"Content-Type": {"application/json"}}},
	} {
		if problems := spec.validateResponse(bootstrap, "GET", tc.status, tc.header, []byte(tc.body), true); len(problems) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}
