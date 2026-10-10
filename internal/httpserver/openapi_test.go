package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// The contract harness: api/openapi.yaml is loaded once, its schemas are
// compiled as JSON Schema 2020-12 and every response of the shared test
// harness is checked against the operation it answers.

const specFile = "../../api/openapi.yaml"
const specURL = "file:///openapi.json"

var httpMethods = []string{"get", "put", "post", "patch", "delete", "head"}

type specOperation struct {
	method, path, id string
	segments         []string // path template segments
	greedy           map[string]bool
	raw              map[string]any
	errorCodes       map[string]bool // x-error-codes ∪ applicable groups
}

type contractSpec struct {
	doc        map[string]any
	operations []*specOperation
	catalog    map[string]struct {
		status    int
		retryable bool
	}
	routerCodes map[string]bool
	compiler    *jsonschema.Compiler
	mu          sync.Mutex
	compiled    map[string]*jsonschema.Schema
}

var (
	loadedSpec    *contractSpec
	loadedSpecErr error
	loadSpecOnce  sync.Once
)

func openAPISpec(t testing.TB) *contractSpec {
	t.Helper()
	loadSpecOnce.Do(func() { loadedSpec, loadedSpecErr = parseSpec() })
	if loadedSpecErr != nil {
		t.Fatal(loadedSpecErr)
	}
	return loadedSpec
}

func parseSpec() (*contractSpec, error) {
	raw, err := os.ReadFile(specFile)
	if err != nil {
		return nil, err
	}
	var tree any
	if err = yaml.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("specification is not JSON compatible: %w", err)
	}
	var doc map[string]any
	if err = json.Unmarshal(encoded, &doc); err != nil {
		return nil, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	s := &contractSpec{doc: doc, compiled: map[string]*jsonschema.Schema{}, routerCodes: map[string]bool{}}
	s.compiler = jsonschema.NewCompiler()
	s.compiler.DefaultDraft(jsonschema.Draft2020)
	s.compiler.AssertFormat()
	if err = s.compiler.AddResource(specURL, instance); err != nil {
		return nil, err
	}
	components := doc["components"].(map[string]any)
	s.catalog = map[string]struct {
		status    int
		retryable bool
	}{}
	for code, value := range components["x-error-codes"].(map[string]any) {
		entry := value.(map[string]any)
		s.catalog[code] = struct {
			status    int
			retryable bool
		}{int(entry["status"].(float64)), entry["retryable"].(bool)}
	}
	for _, code := range components["x-router-error-codes"].(map[string]any)["codes"].([]any) {
		s.routerCodes[code.(string)] = true
	}
	groups := components["x-error-code-groups"].(map[string]any)
	groupCodes := func(name string) []any { return groups[name].(map[string]any)["codes"].([]any) }
	for path, item := range doc["paths"].(map[string]any) {
		pathItem := item.(map[string]any)
		for _, method := range httpMethods {
			value, ok := pathItem[method]
			if !ok {
				continue
			}
			op := value.(map[string]any)
			o := &specOperation{method: strings.ToUpper(method), path: path, id: op["operationId"].(string), raw: op, segments: strings.Split(strings.TrimPrefix(path, "/"), "/"), greedy: map[string]bool{}, errorCodes: map[string]bool{}}
			for _, p := range s.parameters(pathItem, op) {
				if p["in"] == "path" && p["x-greedy"] == true {
					o.greedy[p["name"].(string)] = true
				}
			}
			add := func(codes []any) {
				for _, c := range codes {
					o.errorCodes[c.(string)] = true
				}
			}
			if codes, ok := op["x-error-codes"].([]any); ok {
				add(codes)
			}
			add(groupCodes("common"))
			if strings.HasPrefix(path, "/admin/api/") {
				add(groupCodes("admin_origin"))
			}
			for _, requirement := range asList(op["security"]) {
				for scheme := range requirement.(map[string]any) {
					add(groupCodes(map[string]string{"sessionCookie": "session", "csrfToken": "csrf"}[scheme]))
				}
			}
			if body, ok := op["requestBody"].(map[string]any); ok {
				content := body["content"].(map[string]any)
				if _, ok := content["application/json"]; ok {
					add(groupCodes("json_body"))
				}
				if _, ok := content["multipart/form-data"]; ok {
					add(groupCodes("multipart_body"))
				}
			}
			for _, p := range s.parameters(pathItem, op) {
				if p["in"] == "header" && p["name"] == "If-Match" {
					add(groupCodes("if_match"))
				}
			}
			s.operations = append(s.operations, o)
		}
	}
	return s, nil
}

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

// resolve follows a local $ref ("#/a/b") and returns the target and its JSON pointer.
func (s *contractSpec) resolve(value map[string]any, pointer string) (map[string]any, string) {
	for {
		ref, ok := value["$ref"].(string)
		if !ok {
			return value, pointer
		}
		pointer = strings.TrimPrefix(ref, "#")
		value = s.lookup(pointer).(map[string]any)
	}
}

func (s *contractSpec) lookup(pointer string) any {
	var node any = s.doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		node = node.(map[string]any)[part]
	}
	return node
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// parameters returns the resolved path-level and operation-level parameters.
func (s *contractSpec) parameters(pathItem, op map[string]any) []map[string]any {
	var out []map[string]any
	for _, list := range []any{pathItem["parameters"], op["parameters"]} {
		for _, p := range asList(list) {
			resolved, _ := s.resolve(p.(map[string]any), "")
			out = append(out, resolved)
		}
	}
	return out
}

func (s *contractSpec) schema(pointer string) (*jsonschema.Schema, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if compiled, ok := s.compiled[pointer]; ok {
		return compiled, nil
	}
	compiled, err := s.compiler.Compile(specURL + "#" + pointer)
	if err != nil {
		return nil, err
	}
	s.compiled[pointer] = compiled
	return compiled, nil
}

// match returns the operation answering method and path with ServeMux
// precedence: the template with the most literal segments wins; HEAD uses a
// HEAD operation when declared and the GET operation otherwise.
func (s *contractSpec) match(method, path string) *specOperation {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	candidates := []string{method}
	if method == http.MethodHead {
		candidates = append(candidates, http.MethodGet)
	}
	for _, want := range candidates {
		var best *specOperation
		bestLiterals := -1
		for _, o := range s.operations {
			if o.method != want {
				continue
			}
			if literals, ok := o.matches(segments); ok && literals > bestLiterals {
				best, bestLiterals = o, literals
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

func (o *specOperation) matches(segments []string) (int, bool) {
	literals := 0
	for i, template := range o.segments {
		if strings.HasPrefix(template, "{") {
			if o.greedy[strings.Trim(template, "{}")] {
				return literals, len(segments) > i && i == len(o.segments)-1
			}
			if i >= len(segments) || segments[i] == "" {
				return 0, false
			}
			continue
		}
		if i >= len(segments) || segments[i] != template {
			return 0, false
		}
		literals++
	}
	return literals, len(segments) == len(o.segments)
}

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// validateResponse checks one response against the operation it answers. It
// returns every violation; an empty slice means the response conforms.
func (s *contractSpec) validateResponse(o *specOperation, method string, status int, header http.Header, body []byte, bodyRead bool) []string {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if !requestIDPattern.MatchString(header.Get("X-Request-Id")) {
		report("X-Request-Id %q is not 16 lowercase hex characters", header.Get("X-Request-Id"))
	}
	if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Referrer-Policy") != "same-origin" || header.Get("X-Frame-Options") == "" {
		report("missing security headers: %v", header)
	}
	media, _, _ := mime.ParseMediaType(header.Get("Content-Type"))
	var errorDoc struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	isError := status >= 400 && media == "application/json" && json.Unmarshal(body, &errorDoc) == nil && errorDoc.Error.Code != ""
	if isError {
		code := errorDoc.Error.Code
		class, known := s.catalog[code]
		switch {
		case !known:
			report("error code %s is not in the catalog", code)
		case class.status != status || class.retryable != errorDoc.Error.Retryable:
			report("error code %s returned with status %d retryable %v; catalog says %d %v", code, status, errorDoc.Error.Retryable, class.status, class.retryable)
		}
		if errorDoc.Error.RequestID != header.Get("X-Request-Id") {
			report("error.request_id %q differs from X-Request-Id %q", errorDoc.Error.RequestID, header.Get("X-Request-Id"))
		}
		if s.routerCodes[code] && (o == nil || code == "NOT_FOUND" || code == "METHOD_NOT_ALLOWED") {
			if code == "METHOD_NOT_ALLOWED" && header.Get("Allow") == "" {
				report("METHOD_NOT_ALLOWED without Allow")
			}
			return append(problems, s.validateBody("/components/schemas/Error", body)...)
		}
		if o != nil && !o.errorCodes[code] {
			report("%s does not declare error code %s", o.id, code)
		}
	}
	if o == nil {
		return append(problems, "no operation matches the request")
	}
	responses := o.raw["responses"].(map[string]any)
	key := strconv.Itoa(status)
	value, ok := responses[key]
	if !ok {
		if value, ok = responses["default"]; !ok {
			return append(problems, fmt.Sprintf("%s does not declare status %d", o.id, status))
		}
		key = "default"
	}
	response, pointer := s.resolve(value.(map[string]any), "/paths/"+escapePointer(o.path)+"/"+strings.ToLower(o.method)+"/responses/"+key)
	if headers, ok := response["headers"].(map[string]any); ok {
		for name, declared := range headers {
			got := header.Get(name)
			if got == "" {
				continue
			}
			resolved, headerPointer := s.resolve(declared.(map[string]any), pointer+"/headers/"+escapePointer(name))
			if _, ok := resolved["schema"]; ok {
				instance, _ := json.Marshal(got)
				problems = append(problems, prefix("header "+name, s.validateBody(headerPointer+"/schema", instance))...)
			}
		}
	}
	content, declared := response["content"].(map[string]any)
	if method == http.MethodHead || status == http.StatusNotModified || status == http.StatusPartialContent {
		return problems
	}
	if !declared {
		if (len(body) != 0 || !bodyRead && media != "") && status != http.StatusRequestedRangeNotSatisfiable && status != http.StatusPreconditionFailed {
			report("status %d declares no content but the response has a %q body", status, media)
		}
		return problems
	}
	if _, ok := content[media]; !ok {
		report("media type %q is not declared for status %d (%v)", media, status, keys(content))
		return problems
	}
	if schema, _ := s.resolve(content[media].(map[string]any)["schema"].(map[string]any), ""); media == "application/json" && schema["type"] != "string" {
		// A string schema marks an opaque document (release metadata bytes).
		problems = append(problems, s.validateBody(pointer+"/content/application~1json/schema", body)...)
	}
	return problems
}

func (s *contractSpec) validateBody(pointer string, body []byte) []string {
	schema, err := s.schema(pointer)
	if err != nil {
		return []string{fmt.Sprintf("compile %s: %v", pointer, err)}
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return []string{fmt.Sprintf("body is not JSON: %v", err)}
	}
	if err = schema.Validate(instance); err != nil {
		return []string{fmt.Sprintf("body does not match %s: %v\n%s", pointer, err, body)}
	}
	return nil
}

func prefix(label string, problems []string) []string {
	for i := range problems {
		problems[i] = label + ": " + problems[i]
	}
	return problems
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
