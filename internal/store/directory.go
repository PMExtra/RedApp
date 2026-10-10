package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
)

var (
	ErrInvalidDirectory      = errors.New("Invalid vendor or application configuration")
	ErrDirectoryExists       = errors.New("Vendor or application ID is already reserved")
	ErrDirectoryDeleted      = errors.New("Vendor or application is deleted")
	ErrVendorHasApplications = errors.New("Delete the vendor's applications first")
	ErrSourceInactive        = errors.New("Application source is no longer active")
	// ErrVendorNotFound and ErrApplicationNotFound name the missing object when
	// an operation involves both kinds; both match sql.ErrNoRows.
	ErrVendorNotFound      = fmt.Errorf("vendor not found: %w", sql.ErrNoRows)
	ErrApplicationNotFound = fmt.Errorf("application not found: %w", sql.ErrNoRows)
)

// ValidationError is an ErrInvalidDirectory with a detail that names the
// invalid field. The detail never contains stored secrets, so the HTTP layer
// may show it to the administrator.
type ValidationError struct{ err error }

func invalidf(format string, args ...any) error {
	return &ValidationError{fmt.Errorf(format, args...)}
}
func (e *ValidationError) Error() string   { return ErrInvalidDirectory.Error() + ": " + e.err.Error() }
func (e *ValidationError) Detail() string  { return e.err.Error() }
func (e *ValidationError) Unwrap() []error { return []error{ErrInvalidDirectory, e.err} }

type LocalizedText struct {
	En   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

type VendorInput struct {
	ID             string        `json:"id"`
	Name           LocalizedText `json:"name"`
	Description    LocalizedText `json:"description"`
	Icon           string        `json:"icon"`
	LocalizedIcons LocalizedText `json:"localized_icons"`
	Enabled        bool          `json:"enabled"`
}

type VendorChanges struct {
	Name           LocalizedText `json:"name"`
	Description    LocalizedText `json:"description"`
	Icon           string        `json:"icon"`
	LocalizedIcons LocalizedText `json:"localized_icons"`
	Enabled        bool          `json:"enabled"`
}

type Vendor struct {
	HasTemplate     bool          `json:"has_template"`
	UID             string        `json:"uid"`
	ID              string        `json:"id"`
	Name            LocalizedText `json:"name"`
	Description     LocalizedText `json:"description"`
	Icon            string        `json:"icon"`
	LocalizedIcons  LocalizedText `json:"localized_icons"`
	Enabled         bool          `json:"enabled"`
	Revision        int64         `json:"revision"`
	RuntimeRevision int64         `json:"runtime_revision"`
	DeletedAt       *time.Time    `json:"deleted_at"`
}

type ApplicationInput struct {
	Categories      []string      `json:"categories"`
	Tags            []string      `json:"tags"`
	ID              string        `json:"id"`
	Name            LocalizedText `json:"name"`
	Description     LocalizedText `json:"description"`
	Icon            string        `json:"icon"`
	Provider        string        `json:"provider"`
	BaseURL         string        `json:"base_url"`
	BaseURLs        []string      `json:"base_urls"`
	SourceStrategy  string        `json:"source_strategy"`
	CacheTTLSeconds int           `json:"cache_ttl_seconds"`
	Enabled         bool          `json:"enabled"`
}

type ApplicationChanges struct {
	Name            LocalizedText `json:"name"`
	Description     LocalizedText `json:"description"`
	Icon            string        `json:"icon"`
	BaseURL         string        `json:"base_url"`
	BaseURLs        []string      `json:"base_urls"`
	SourceStrategy  string        `json:"source_strategy"`
	CacheTTLSeconds int           `json:"cache_ttl_seconds"`
	Enabled         bool          `json:"enabled"`
}

type Application struct {
	Categories      []string      `json:"categories"`
	Tags            []string      `json:"tags"`
	BuiltinTemplate bool          `json:"builtin_template"`
	UID             string        `json:"uid"`
	ID              string        `json:"id"`
	Key             string        `json:"key"`
	VendorUID       string        `json:"vendor_uid"`
	VendorID        string        `json:"vendor_id"`
	Name            LocalizedText `json:"name"`
	Description     LocalizedText `json:"description"`
	Icon            string        `json:"icon"`
	Provider        string        `json:"provider"`
	BaseURL         string        `json:"base_url"`
	BaseURLs        []string      `json:"base_urls"`
	SourceStrategy  string        `json:"source_strategy"`
	CacheTTLSeconds int           `json:"cache_ttl_seconds"`
	Enabled         bool          `json:"enabled"`
	Revision        int64         `json:"revision"`
	RuntimeRevision int64         `json:"runtime_revision"`
	SourceEpoch     int64         `json:"source_epoch"`
	DeletedAt       *time.Time    `json:"deleted_at"`
}

func (a Application) StorageID() string { return identity.StorageID(a.UID, a.SourceEpoch) }
func (a Application) MetricsID() string { return identity.MetricsID(a.UID) }

// SourceFence captures the runtime revisions of an application and its vendor
// when work was admitted. A source epoch alone cannot fence work admitted
// before a disable and re-enable, and configuration revisions change on edits
// that do not affect admission.
type SourceFence struct{ AppRuntimeRevision, VendorRuntimeRevision int64 }

// Provider/BaseURL/BaseURLs/SourceStrategy/CreatedAt are the immutable source
// snapshot. The revision and
// Active fields describe its current eligibility, not its historical eligibility.
type SourceRecord struct {
	AppUID                string    `json:"app_uid"`
	Epoch                 int64     `json:"epoch"`
	Provider              string    `json:"provider"`
	BaseURL               string    `json:"base_url"`
	BaseURLs              []string  `json:"base_urls"`
	SourceStrategy        string    `json:"source_strategy"`
	CreatedAt             time.Time `json:"created_at"`
	AppRuntimeRevision    int64     `json:"app_runtime_revision"`
	VendorRuntimeRevision int64     `json:"vendor_runtime_revision"`
	Active                bool      `json:"active"`
}

func (r SourceRecord) StorageID() string { return identity.StorageID(r.AppUID, r.Epoch) }
func (r SourceRecord) MetricsID() string { return identity.MetricsID(r.AppUID) }
func (r SourceRecord) Fence() SourceFence {
	return SourceFence{r.AppRuntimeRevision, r.VendorRuntimeRevision}
}

var iconPath = regexp.MustCompile(`^/assets/icons/[0-9a-f]{64}\.(?:png|jpg|svg)$`)

func validatePresentation(name, description LocalizedText, icon string) error {
	for _, value := range []string{name.En, name.ZhCN} {
		if strings.TrimSpace(value) == "" || len(value) > 256 || strings.ContainsRune(value, 0) {
			return invalidf("both localized names are required (at most 256 bytes each)")
		}
	}
	for _, value := range []string{description.En, description.ZhCN} {
		if len(value) > 16384 || strings.ContainsRune(value, 0) {
			return invalidf("description is too long or contains NUL")
		}
	}
	if icon != "" && !builtinTemplateIcon(icon) && !iconPath.MatchString(icon) && !frozenPresetIcon(icon) {
		return invalidf("icon must reference a stored image or reviewed seed asset")
	}
	return nil
}

func validateVendor(v VendorInput) error {
	if !identity.ValidVendor(v.ID) {
		return invalidf("invalid or reserved vendor ID")
	}
	return validateVendorPresentation(v.Name, v.Description, v.Icon, v.LocalizedIcons)
}

func validateApplication(a *ApplicationInput) error {
	if !identity.ValidSlug(a.ID) {
		return invalidf("invalid application ID")
	}
	if err := validatePresentation(a.Name, a.Description, a.Icon); err != nil {
		return err
	}
	switch a.Provider {
	case "info", "hosted":
		if a.BaseURL != "" || len(a.BaseURLs) != 0 || a.SourceStrategy != "" || a.CacheTTLSeconds != 0 {
			return invalidf("Content applications cannot configure upstream caching")
		}
		return nil
	case "http-cache", "codex", "claude-code":
	default:
		return invalidf("unknown provider")
	}
	if a.CacheTTLSeconds < 0 || a.CacheTTLSeconds > 86400 || (a.Provider != "http-cache" && a.CacheTTLSeconds == 0) {
		return invalidf("cache TTL must be 0..86400 seconds (release providers require at least 1)")
	}
	if a.Provider != "http-cache" {
		if len(a.BaseURLs) != 0 || a.SourceStrategy != "" {
			return invalidf("multiple sources are supported only by GeneralHttp")
		}
		base, err := normalizeDirectoryBase(a.BaseURL)
		a.BaseURL, a.BaseURLs = base, nil
		return err
	}
	bases := a.BaseURLs
	if bases == nil && a.BaseURL != "" {
		bases = []string{a.BaseURL}
	}
	if len(bases) < 1 || len(bases) > 16 {
		return invalidf("GeneralHttp requires 1..16 base URLs")
	}
	if a.SourceStrategy == "" {
		a.SourceStrategy = "ordered"
	}
	if a.SourceStrategy != "ordered" && a.SourceStrategy != "round_robin" && a.SourceStrategy != "random" {
		return invalidf("invalid source strategy")
	}
	normalized := make([]string, len(bases))
	seen := make(map[string]bool, len(bases))
	for i, value := range bases {
		base, err := normalizeDirectoryBase(value)
		if err != nil {
			return err
		}
		if seen[base] {
			return invalidf("duplicate source URL")
		}
		seen[base], normalized[i] = true, base
	}
	a.BaseURLs, a.BaseURL = normalized, normalized[0]
	return nil
}

// This is a shape boundary, not a public-network-only policy. Configured HTTP
// and private enterprise origins are supported; transport validation is separate.
func normalizeDirectoryBase(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || len(value) > 4096 || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(value, "\r\n\t#") {
		return "", invalidf("BaseURL requires an HTTP(S) origin and optional path without credentials, query, or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalidf("invalid BaseURL port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", invalidf("invalid BaseURL port")
	}
	if !utf8.ValidString(u.Path) || strings.ContainsAny(u.Path, "\\%") || strings.Contains(u.Path, "//") {
		return "", invalidf("invalid BaseURL path")
	}
	for _, ch := range u.Path {
		if unicode.IsControl(ch) {
			return "", invalidf("invalid BaseURL path")
		}
	}
	for _, segment := range strings.Split(u.EscapedPath(), "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\\x00\r\n") {
			return "", invalidf("invalid BaseURL path")
		}
	}
	// Directory-prefix spelling is stable so adding a trailing slash alone does not
	// manufacture a new source epoch.
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

type directoryQuerier interface{ QueryRow(string, ...any) *sql.Row }
type directoryScanner interface{ Scan(...any) error }

const vendorColumns = `uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,icon_en,icon_zh_cn,enabled,revision,runtime_revision,deleted_at_s`
const applicationColumns = `a.uid,a.id,v.id||'/'||a.id,a.vendor_uid,v.id,a.name_en,a.name_zh_cn,a.description_en,a.description_zh_cn,a.icon,a.provider,a.base_url,a.base_urls_json,a.source_strategy,a.cache_ttl_seconds,a.enabled,a.revision,a.source_epoch,a.deleted_at_s,a.runtime_revision,EXISTS(SELECT 1 FROM template_snapshots t WHERE t.kind='App' AND t.canonical_key=v.id||'/'||a.id),COALESCE((SELECT json_group_array(category_id) FROM (SELECT category_id FROM application_categories WHERE app_uid=a.uid ORDER BY category_id)),'[]'),COALESCE((SELECT json_group_array(tag) FROM (SELECT tag FROM application_tags WHERE app_uid=a.uid ORDER BY ordinal)),'[]')`

func scanVendor(row directoryScanner) (Vendor, error) {
	var v Vendor
	var deleted sql.NullInt64
	err := row.Scan(&v.UID, &v.ID, &v.Name.En, &v.Name.ZhCN, &v.Description.En, &v.Description.ZhCN, &v.Icon, &v.LocalizedIcons.En, &v.LocalizedIcons.ZhCN, &v.Enabled, &v.Revision, &v.RuntimeRevision, &deleted)
	_, v.HasTemplate = BuiltinVendorTemplate(v.ID)
	v.DeletedAt = timePointer(deleted)
	return v, err
}
func scanApplication(row directoryScanner) (Application, error) {
	var a Application
	var deleted sql.NullInt64
	var bases, categories, tags []byte
	err := row.Scan(&a.UID, &a.ID, &a.Key, &a.VendorUID, &a.VendorID, &a.Name.En, &a.Name.ZhCN, &a.Description.En, &a.Description.ZhCN, &a.Icon, &a.Provider, &a.BaseURL, &bases, &a.SourceStrategy, &a.CacheTTLSeconds, &a.Enabled, &a.Revision, &a.SourceEpoch, &deleted, &a.RuntimeRevision, &a.BuiltinTemplate, &categories, &tags)
	if err == nil {
		err = json.Unmarshal(bases, &a.BaseURLs)
		if err == nil {
			err = json.Unmarshal(categories, &a.Categories)
		}
		if err == nil {
			err = json.Unmarshal(tags, &a.Tags)
		}
	}
	if _, ok := BuiltinApplicationTemplate(a.Key); ok {
		a.BuiltinTemplate = true
	}
	a.DeletedAt = timePointer(deleted)
	return a, err
}
func readVendor(q directoryQuerier, id string) (Vendor, error) {
	return scanVendor(q.QueryRow(`SELECT `+vendorColumns+` FROM vendors WHERE id=?`, id))
}
func readApplication(q directoryQuerier, key string) (Application, error) {
	vendor, app, ok := strings.Cut(key, "/")
	if !ok || !identity.ValidKey(key) {
		return Application{}, ErrInvalidDirectory
	}
	return scanApplication(q.QueryRow(`SELECT `+applicationColumns+` FROM applications a JOIN vendors v ON v.uid=a.vendor_uid WHERE v.id=? AND a.id=?`, vendor, app))
}

func (s *Store) Vendor(id string) (Vendor, error)            { return readVendor(s.DB, id) }
func (s *Store) Application(key string) (Application, error) { return readApplication(s.DB, key) }
func (s *Store) Vendors(includeDeleted bool) ([]Vendor, error) {
	query := `SELECT ` + vendorColumns + ` FROM vendors`
	if !includeDeleted {
		query += ` WHERE deleted_at_s IS NULL`
	}
	rows, err := s.DB.Query(query + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Vendor{}
	for rows.Next() {
		v, err := scanVendor(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Store) Applications(includeDeleted bool) ([]Application, error) {
	query := `SELECT ` + applicationColumns + ` FROM applications a JOIN vendors v ON v.uid=a.vendor_uid`
	if !includeDeleted {
		query += ` WHERE a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL`
	}
	rows, err := s.DB.Query(query + ` ORDER BY v.id,a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Application{}
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *Store) CreateVendor(in VendorInput) (Vendor, error) {
	return s.CreateConfiguredVendor(in, nil)
}
func (s *Store) UpdateVendor(id string, revision int64, in VendorChanges) (Vendor, error) {
	spec := object(presets.VendorSpec{Name: presets.Text{En: in.Name.En, ZhCN: in.Name.ZhCN}, Description: presets.Text{En: in.Description.En, ZhCN: in.Description.ZhCN}, Icon: in.Icon, LocalizedIcons: presets.Text{En: in.LocalizedIcons.En, ZhCN: in.LocalizedIcons.ZhCN}})
	set := patchObject(spec, "Vendor")
	delete(set, "proxy")
	return s.PatchVendorFields(id, revision, set, &in.Enabled)
}

func (s *Store) DeleteVendor(id string, revision int64) error {
	return s.changeConfiguration(func(st *configurationState) error { return st.softDeleteVendor(id, revision) })
}
func (st *configurationState) softDeleteVendor(id string, revision int64) error {
	for i := range st.Vendors {
		v := &st.Vendors[i]
		if v.ID != id {
			continue
		}
		if v.Revision != revision {
			return ErrConflict
		}
		if v.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		for _, a := range st.Applications {
			if a.VendorUID == v.UID && a.DeletedAt == nil {
				return ErrVendorHasApplications
			}
		}
		now := time.Now().UTC()
		v.Enabled = false
		v.DeletedAt = &now
		v.Revision++
		return nil
	}
	return sql.ErrNoRows
}

func (s *Store) CreateApplication(vendorID string, in ApplicationInput) (Application, error) {
	return s.CreateConfiguredApplication(vendorID, in, nil)
}

// UpdateApplication replaces mutable presentation and configuration under CAS.
// For GeneralHttp, a non-nil BaseURLs explicitly replaces the ordered list; an
// empty list is invalid. Legacy BaseURL-only edits preserve the existing list
// when the normalized first URL is unchanged, or replace it with a single source
// when it changes. An omitted SourceStrategy preserves the existing strategy.
func (s *Store) UpdateApplication(key string, revision int64, in ApplicationChanges) (Application, error) {
	a, err := s.Application(key)
	if err != nil {
		return Application{}, err
	}
	validated := ApplicationInput{ID: a.ID, Name: in.Name, Description: in.Description, Icon: in.Icon, Provider: a.Provider, BaseURL: in.BaseURL, BaseURLs: in.BaseURLs, SourceStrategy: in.SourceStrategy, CacheTTLSeconds: in.CacheTTLSeconds, Enabled: in.Enabled}
	if a.Provider == "http-cache" && validated.BaseURLs == nil {
		base, err := normalizeDirectoryBase(in.BaseURL)
		if err != nil {
			return Application{}, err
		}
		if base == a.BaseURL {
			validated.BaseURLs = a.BaseURLs
		}
	}
	if a.Provider == "http-cache" && validated.SourceStrategy == "" {
		validated.SourceStrategy = a.SourceStrategy
	}
	if err = validateApplication(&validated); err != nil {
		return Application{}, err
	}
	if validated.BaseURLs == nil {
		validated.BaseURLs = []string{}
	}
	set := map[string]json.RawMessage{"name.en": encode(in.Name.En), "name.zh-CN": encode(in.Name.ZhCN), "description.en": encode(in.Description.En), "description.zh-CN": encode(in.Description.ZhCN), "icon": encode(in.Icon), "base_url": encode(validated.BaseURL), "base_urls": encode(validated.BaseURLs), "source_strategy": encode(validated.SourceStrategy), "cache_ttl_seconds": encode(validated.CacheTTLSeconds)}
	return s.PatchApplicationFields(key, revision, set, &in.Enabled)
}

func (s *Store) DeleteApplication(key string, revision int64) error {
	if _, ok := BuiltinApplicationTemplate(key); ok {
		return ErrBuiltinTemplate
	}
	return s.changeConfiguration(func(st *configurationState) error { return st.softDeleteApplication(key, revision) })
}
func (st *configurationState) softDeleteApplication(key string, revision int64) error {
	if _, ok := st.Templates[templateKey("App", key)]; ok {
		return ErrBuiltinTemplate
	}
	for i := range st.Applications {
		a := &st.Applications[i]
		if a.Key != key {
			continue
		}
		if a.Revision != revision {
			return ErrConflict
		}
		if a.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		now := time.Now().UTC()
		a.Enabled = false
		a.DeletedAt = &now
		a.Revision++
		return nil
	}
	return sql.ErrNoRows
}

const sourceColumns = `src.app_uid,src.epoch,src.provider,src.base_url,src.base_urls_json,src.source_strategy,src.created_at_s,a.runtime_revision,v.runtime_revision,(a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL AND src.epoch=a.source_epoch)`
const sourceJoin = ` FROM application_sources src JOIN applications a ON a.uid=src.app_uid JOIN vendors v ON v.uid=a.vendor_uid`

func scanSource(row directoryScanner) (SourceRecord, error) {
	var r SourceRecord
	var created int64
	var bases []byte
	err := row.Scan(&r.AppUID, &r.Epoch, &r.Provider, &r.BaseURL, &bases, &r.SourceStrategy, &created, &r.AppRuntimeRevision, &r.VendorRuntimeRevision, &r.Active)
	if err == nil {
		err = json.Unmarshal(bases, &r.BaseURLs)
	}
	r.CreatedAt = time.Unix(created, 0).UTC()
	return r, err
}
func (s *Store) Source(storageID string) (SourceRecord, error) {
	uid, epoch, ok := identity.ParseStorageID(storageID)
	if !ok {
		return SourceRecord{}, ErrInvalidDirectory
	}
	return scanSource(s.DB.QueryRow(`SELECT `+sourceColumns+sourceJoin+` WHERE src.app_uid=? AND src.epoch=?`, uid, epoch))
}
func (s *Store) Sources() ([]SourceRecord, error) {
	rows, err := s.DB.Query(`SELECT ` + sourceColumns + sourceJoin + ` ORDER BY src.app_uid,src.epoch`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SourceRecord{}
	for rows.Next() {
		r, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func checkSourceActive(q directoryQuerier, storageID string, expected []SourceFence) error {
	uid, epoch, ok := identity.ParseStorageID(storageID)
	// Static provider fixtures and their typed storage APIs remain usable without
	// a dynamic directory. Runtime-created applications always use app/<uid>-eN;
	// never treat a malformed private namespace as a legacy public key.
	if !ok && !strings.HasPrefix(storageID, "app/") && identity.ValidKey(storageID) {
		return nil
	}
	if !ok || len(expected) > 1 {
		return ErrInvalidDirectory
	}
	r, err := scanSource(q.QueryRow(`SELECT `+sourceColumns+sourceJoin+` WHERE src.app_uid=? AND src.epoch=?`, uid, epoch))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSourceInactive
	}
	if err != nil {
		return err
	}
	if !r.Active || (len(expected) == 1 && r.Fence() != expected[0]) {
		return ErrSourceInactive
	}
	return nil
}

// RequireSourceActive checks within the same transaction as a publication. Callers
// publishing admitted work must supply the captured fence, not a freshly read one.
func (s *Store) RequireSourceActive(tx *sql.Tx, storageID string, expected ...SourceFence) error {
	if tx == nil {
		return ErrInvalidDirectory
	}
	return checkSourceActive(tx, storageID, expected)
}
func (s *Store) CheckSourceActive(storageID string, expected ...SourceFence) error {
	return checkSourceActive(s.DB, storageID, expected)
}
func (s *Store) SourceActive(storageID string) (bool, error) {
	err := s.CheckSourceActive(storageID)
	if errors.Is(err, ErrSourceInactive) {
		return false, nil
	}
	return err == nil, err
}

// Only reviewed embedded images may bypass uploaded-image paths.
func builtinTemplateIcon(path string) bool {
	_, ok := presets.Embedded().Image(path)
	return ok
}

func validateVendorPresentation(name, description LocalizedText, icon string, localized LocalizedText) error {
	for _, value := range []string{icon, localized.En, localized.ZhCN} {
		if err := validatePresentation(name, description, value); err != nil {
			return err
		}
	}
	return nil
}

func frozenPresetIcon(icon string) bool {
	relative := strings.TrimPrefix(icon, presets.ImagePrefix)
	if relative == icon || !fs.ValidPath(relative) || strings.ContainsAny(relative, "\\:?#") {
		return false
	}
	switch path.Ext(relative) {
	case ".svg", ".png", ".jpg", ".jpeg":
		return true
	}
	return false
}
