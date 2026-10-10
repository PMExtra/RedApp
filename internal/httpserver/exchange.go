package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
)

type exchangePreview struct {
	plan            store.ImportPlan
	session, digest string
	until           time.Time
	images          map[string][]byte
}

// exchangeKinds maps the API kinds (vendor, app, category) to the store's.
var exchangeKinds = map[string]string{"vendor": "Vendor", "app": "App", "category": "categories"}

func apiExchangeKind(kind string) string {
	for api, internal := range exchangeKinds {
		if internal == kind {
			return api
		}
	}
	return kind
}

func exchangeOwner(session auth.Session) string {
	digest := sha256.Sum256([]byte(session.CSRF))
	return hex.EncodeToString(digest[:])
}

// importFailure maps a store error of an import preview or execution.
// Validation messages stay generic: they could echo an administrator's
// private URL or text from the package.
func importFailure(err error) *apiError {
	switch {
	case errors.Is(err, store.ErrDirectoryExists), errors.Is(err, store.ErrDirectoryDeleted):
		return directoryFailure(err, codeApplicationNotFound)
	case errors.Is(err, store.ErrInvalidDirectory), errors.Is(err, networkproxy.ErrRedactedMismatch), errors.Is(err, store.ErrNotFound):
		return newError(codeValidationFailed, err, "Invalid configuration, unavailable template or unresolved import choice; use an independent copy when the template is unavailable")
	}
	return storageError(err)
}

type exportSelectionRequest struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	IncludeApps *bool  `json:"include_apps"`
}

type exportRequest struct {
	Selection               []exportSelectionRequest `json:"selection"`
	Mode                    *string                  `json:"mode"`
	IncludeNotes            *bool                    `json:"include_notes"`
	IncludeProxyCredentials *bool                    `json:"include_proxy_credentials"`
}

func (s *Server) exportConfiguration(w http.ResponseWriter, r *http.Request) {
	var in exportRequest
	if e := decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if in.Selection == nil || in.Mode == nil || in.IncludeNotes == nil || in.IncludeProxyCredentials == nil {
		s.fail(w, r, codeInvalidRequest, nil, "selection, mode, include_notes and include_proxy_credentials are required")
		return
	}
	options := store.ExportOptions{Mode: *in.Mode, IncludeNotes: *in.IncludeNotes, IncludeProxyCredentials: *in.IncludeProxyCredentials}
	if options.Mode != "linked" && options.Mode != "independent" {
		s.fail(w, r, codeValidationFailed, nil, "mode must be linked or independent")
		return
	}
	if len(in.Selection) < 1 || len(in.Selection) > 1000 {
		s.fail(w, r, codeValidationFailed, nil, "selection must contain 1..1000 items")
		return
	}
	for _, sel := range in.Selection {
		valid := sel.Kind == "vendor" && identity.ValidVendor(sel.Key) || sel.Kind == "app" && identity.ValidKey(sel.Key) && sel.IncludeApps == nil
		if !valid {
			s.fail(w, r, codeValidationFailed, nil, "Each selection needs kind vendor with a vendor ID, or kind app with an application key and no include_apps")
			return
		}
		// include_apps is opt-in; the store's default includes them.
		includeApps := sel.IncludeApps != nil && *sel.IncludeApps
		options.Selection = append(options.Selection, store.ExportSelection{Kind: exchangeKinds[sel.Kind], Key: sel.Key, IncludeApps: &includeApps})
	}
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	p, err := s.store.ExportConfiguration(options)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	if err = s.exportImages(&p); err != nil {
		s.fail(w, r, codeValidationFailed, err, "A selected icon could not be exported as a controlled image")
		return
	}
	body, err := configexchange.ZIP(p)
	if err != nil {
		s.fail(w, r, codeValidationFailed, err, "The selection exceeds the configuration package limits")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="redapp-configuration.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

type importChoiceRequest struct {
	Kind               string               `json:"kind"`
	Key                string               `json:"key"`
	Action             string               `json:"action"`
	TargetVendor       string               `json:"target_vendor"`
	TargetID           string               `json:"target_id"`
	DictionaryUpdate   bool                 `json:"dictionary_update"`
	Proxy              *networkproxy.Config `json:"proxy"`
	KeepEffectiveProxy bool                 `json:"keep_effective_proxy"`
	DetachTemplate     bool                 `json:"detach_template"`
	UpdateNotes        bool                 `json:"update_notes"`
}

type importDifferenceDTO struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

type importItemDTO struct {
	Kind                 string                `json:"kind"`
	Key                  string                `json:"key"`
	Target               string                `json:"target"`
	Action               string                `json:"action"`
	UID                  string                `json:"uid,omitempty"`
	Revision             int64                 `json:"revision"`
	NotesRevision        int64                 `json:"notes_revision"`
	TemplateHashMismatch bool                  `json:"template_hash_mismatch"`
	TemplateMissing      bool                  `json:"template_missing"`
	DetachTemplate       bool                  `json:"detach_template"`
	OmittedFields        []string              `json:"omitted_fields"`
	Differences          []importDifferenceDTO `json:"differences"`
	Requirements         []string              `json:"requirements"`
}

type importPreviewDTO struct {
	ID                     string          `json:"id"`
	Digest                 string          `json:"digest"`
	ExpiresAt              time.Time       `json:"expires_at"`
	Ready                  bool            `json:"ready"`
	NeedsInstructionsTrust bool            `json:"needs_instructions_trust"`
	Items                  []importItemDTO `json:"items"`
}

// importPreviewDocument flattens a plan; both sides of a proxy difference are redacted.
func importPreviewDocument(id, digest string, until time.Time, plan store.ImportPlan) importPreviewDTO {
	out := importPreviewDTO{ID: id, Digest: digest, ExpiresAt: until.UTC().Truncate(time.Second), Ready: plan.Ready, NeedsInstructionsTrust: plan.NeedsTrust, Items: make([]importItemDTO, 0, len(plan.Items))}
	for _, item := range plan.Items {
		row := importItemDTO{Kind: apiExchangeKind(item.Kind), Key: item.Key, Target: item.Target, Action: item.Action, UID: item.UID, Revision: item.Revision, NotesRevision: item.NotesRevision,
			TemplateHashMismatch: item.TemplateHashMismatch, TemplateMissing: item.TemplateMissing, DetachTemplate: item.DetachTemplate,
			OmittedFields: nonNil(item.Omitted), Differences: make([]importDifferenceDTO, 0, len(item.Differences)), Requirements: nonNil(item.Requirements)}
		for _, d := range item.Differences {
			before, after := d.Before, d.After
			if d.Field == "proxy" {
				before, after = redactProxyValue(before), redactProxyValue(after)
			}
			row.Differences = append(row.Differences, importDifferenceDTO{Field: d.Field, Before: before, After: after})
		}
		out.Items = append(out.Items, row)
	}
	return out
}

// previewImport parses an uploaded package and keeps the plan in memory for
// the session that created it (at most 8 previews, 10 minutes each).
func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	raw, choices, e := readExchangeUpload(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	p, err := configexchange.Parse(raw)
	if err != nil {
		s.fail(w, r, codePackageInvalid, err, "Invalid configuration package")
		return
	}
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	now := time.Now()
	s.expireImportPreviews(now)
	if len(s.exchangePreviews) >= 8 {
		s.fail(w, r, codePreviewLimitExceeded, nil, "Too many active configuration previews; wait for one to expire")
		return
	}
	images, err := s.prepareImportImages(&p)
	if err != nil {
		s.fail(w, r, codePackageInvalid, err, "The package contains an invalid, missing or unreferenced image")
		return
	}
	plan, err := s.store.PreviewConfigurationImport(p.Documents, choices)
	if err != nil {
		s.writeError(w, r, importFailure(err))
		return
	}
	id, err := identity.NewUID()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	until := now.Add(10 * time.Minute)
	s.exchangePreviews[id] = exchangePreview{plan: plan, session: exchangeOwner(requestState(r).session), digest: digest, until: until, images: images}
	writeOK(w, importPreviewDocument(id, digest, until, plan))
}

// expireImportPreviews drops expired previews; exchangeMu is held.
func (s *Server) expireImportPreviews(now time.Time) {
	if s.exchangePreviews == nil {
		s.exchangePreviews = map[string]exchangePreview{}
	}
	for id, p := range s.exchangePreviews {
		if !p.until.After(now) {
			delete(s.exchangePreviews, id)
		}
	}
}

type importAppliedDTO struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	UID      string `json:"uid,omitempty"`
	Revision int64  `json:"revision"`
}

type importResultDTO struct {
	Applied bool               `json:"applied"`
	Items   []importAppliedDTO `json:"items"`
}

func importResultDocument(result store.ImportResult) importResultDTO {
	out := importResultDTO{Applied: result.Applied, Items: make([]importAppliedDTO, 0, len(result.Items))}
	for _, item := range result.Items {
		out.Items = append(out.Items, importAppliedDTO{Kind: apiExchangeKind(item.Kind), Key: item.Key, UID: item.UID, Revision: item.Revision})
	}
	return out
}

type importExecuteRequest struct {
	TrustInstructions *bool `json:"trust_instructions"`
}

// executeImport commits a preview. The preview ID is the idempotency key: a
// stored receipt is returned again for 24 hours, also to a new session.
func (s *Server) executeImport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("preview_id")
	if !identity.ValidUID(id) {
		s.fail(w, r, codeInvalidPath, nil, "Invalid preview ID")
		return
	}
	var in importExecuteRequest
	if e := decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if in.TrustInstructions == nil {
		s.fail(w, r, codeInvalidRequest, nil, "trust_instructions is required")
		return
	}
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	result, found, err := s.store.ImportReceipt(id)
	switch {
	case errors.Is(err, store.ErrExpired):
		s.fail(w, r, codePreviewNotFound, nil, "The import receipt expired; build a new preview")
		return
	case err != nil:
		s.writeError(w, r, storageError(err))
		return
	case found:
		writeOK(w, importResultDocument(result))
		return
	}
	s.expireImportPreviews(time.Now())
	owner := exchangeOwner(requestState(r).session)
	preview, ok := s.exchangePreviews[id]
	if !ok || preview.session != owner {
		s.fail(w, r, codePreviewNotFound, nil, "The preview is unknown, expired or belongs to another session; build a new preview")
		return
	}
	if !preview.plan.Ready {
		s.fail(w, r, codeImportNotReady, nil, "The preview has unresolved requirements; upload again with choices")
		return
	}
	if preview.plan.NeedsTrust && !*in.TrustInstructions {
		s.fail(w, r, codeInstructionsTrustRequired, nil, "The import changes usage instructions; set trust_instructions to confirm")
		return
	}
	needed := map[string][]byte{}
	for path, body := range preview.images {
		if preview.plan.ReferencesIcon(path) {
			needed[path] = body
		}
	}
	err = s.icons.ApplyImages(needed, s.store.IconReferenced, func() error {
		var e error
		result, e = s.store.ExecuteConfigurationImport(preview.plan, id, *in.TrustInstructions, func() bool {
			active, ok := s.auth.Session(r)
			return ok && exchangeOwner(active) == owner
		})
		return e
	})
	switch {
	case errors.Is(err, store.ErrImportGuard):
		s.fail(w, r, codePreviewNotFound, err, "The session changed during the import; nothing was imported")
	case errors.Is(err, store.ErrConflict):
		s.fail(w, r, codePreviewStale, err, "Objects changed since the preview; nothing was imported, build a new preview")
	case err != nil:
		s.writeError(w, r, importFailure(err))
	default:
		delete(s.exchangePreviews, id)
		writeOK(w, importResultDocument(result))
	}
}

type copyRequest struct {
	SourceUID     *string `json:"source_uid"`
	TargetVendor  *string `json:"target_vendor"`
	TargetID      *string `json:"target_id"`
	Mode          *string `json:"mode"`
	IncludeNotes  *bool   `json:"include_notes"`
	NotesRevision *int64  `json:"notes_revision"`
}

// copyApp creates a disabled application from an existing one. If-Match is
// the source revision; source_uid and notes_revision guard its identity and notes.
func (s *Server) copyApp(w http.ResponseWriter, r *http.Request) {
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
	var in copyRequest
	if e = decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if in.SourceUID == nil || in.TargetVendor == nil || in.TargetID == nil || in.Mode == nil || in.IncludeNotes == nil {
		s.fail(w, r, codeInvalidRequest, nil, "source_uid, target_vendor, target_id, mode and include_notes are required")
		return
	}
	input := store.CopyApplicationInput{SourceUID: *in.SourceUID, SourceRevision: revision, TargetVendor: *in.TargetVendor, TargetID: *in.TargetID, Mode: *in.Mode, IncludeNotes: *in.IncludeNotes}
	var invalid string
	switch {
	case !identity.ValidUID(input.SourceUID):
		invalid = "source_uid must be the source application UID"
	case !identity.ValidVendor(input.TargetVendor) || !identity.ValidSlug(input.TargetID):
		invalid = "target_vendor and target_id must be valid IDs"
	case input.Mode != "linked" && input.Mode != "independent":
		invalid = "mode must be linked or independent"
	case input.IncludeNotes && (in.NotesRevision == nil || *in.NotesRevision < 1):
		invalid = "notes_revision is required with include_notes"
	}
	if invalid != "" {
		s.fail(w, r, codeValidationFailed, nil, invalid)
		return
	}
	if input.IncludeNotes {
		input.NotesRevision = *in.NotesRevision
	}
	a, err := s.store.CopyApplication(key, input)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeApplicationNotFound))
		return
	}
	writeCreated(w, "/admin/api/apps/"+a.Key, a.Revision, appDocument(a))
}

// readExchangeUpload reads the ImportUpload multipart body: one file part and
// an optional choices part.
func readExchangeUpload(r *http.Request) ([]byte, []store.ImportChoice, *apiError) {
	reader, e := multipartBody(r)
	if e != nil {
		return nil, nil, e
	}
	var raw []byte
	var choices []store.ImportChoice
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, newError(codeInvalidRequest, err, "Malformed multipart body")
		}
		name := part.FormName()
		if seen[name] || name != "file" && name != "choices" {
			part.Close()
			return nil, nil, newError(codeInvalidRequest, nil, "Only one file part and one optional choices part are allowed")
		}
		seen[name] = true
		limit := int64(configexchange.MaxUpload)
		if name == "choices" {
			limit = 1 << 20
		}
		body, tooLarge, e := readPart(part, limit)
		part.Close()
		switch {
		case e != nil:
			return nil, nil, e
		case tooLarge && name == "file":
			return nil, nil, newError(codePackageInvalid, nil, "Configuration packages are limited to 32 MiB")
		case tooLarge:
			return nil, nil, newError(codeInvalidRequest, nil, "choices is limited to 1 MiB")
		case name == "file":
			raw = body
		default:
			if choices, e = parseImportChoices(body); e != nil {
				return nil, nil, e
			}
		}
	}
	if len(raw) == 0 {
		return nil, nil, newError(codeInvalidRequest, nil, "A non-empty file part is required")
	}
	return raw, choices, nil
}

// parseImportChoices strictly decodes the choices part, a JSON array of ImportChoice.
func parseImportChoices(body []byte) ([]store.ImportChoice, *apiError) {
	if err := jsoncheck.Strict(body); err != nil {
		return nil, newError(codeInvalidRequest, err, "choices must be strict JSON")
	}
	var in []importChoiceRequest
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil || d.More() {
		return nil, newError(codeInvalidRequest, err, "choices must be an array of import choices")
	}
	out := make([]store.ImportChoice, 0, len(in))
	for _, c := range in {
		kind, ok := exchangeKinds[c.Kind]
		if !ok || c.Key == "" {
			return nil, newError(codeInvalidRequest, nil, "Each choice needs kind vendor, app or category and a key")
		}
		out = append(out, store.ImportChoice{Kind: kind, Key: c.Key, Action: c.Action, TargetVendor: c.TargetVendor, TargetID: c.TargetID, DictionaryUpdate: c.DictionaryUpdate,
			Proxy: c.Proxy, KeepEffectiveProxy: c.KeepEffectiveProxy, DetachTemplate: c.DetachTemplate, UpdateNotes: c.UpdateNotes})
	}
	return out, nil
}
