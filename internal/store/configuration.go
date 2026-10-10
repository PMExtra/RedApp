package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
)

// ConfigurationPublication is prepared before the write transaction. Publish is
// an infallible memory operation; Abort releases reservations without publishing.
type ConfigurationPublication interface {
	Publish()
	Abort()
}

// DirectorySnapshot is the complete runtime input of the configuration: every
// vendor, application and source with the resolved proxy scopes and reviewed
// release contracts.
type DirectorySnapshot struct {
	GlobalProxy         networkproxy.Config
	GlobalProxyRevision int64
	ProxyScopes         map[string]networkproxy.Scope
	ReviewedDescriptors []presets.Descriptor
	TemplateBindings    map[string]string
	ProviderDefaults    map[string]string
	Vendors             []Vendor
	Applications        []Application
	Sources             []SourceRecord
}
type Object map[string]any

type Configuration struct {
	Revision             int64                  `json:"revision"`
	InstructionsRevision int64                  `json:"instructions_revision,omitempty"`
	ProxyEffective       networkproxy.Effective `json:"proxy_effective"`
	TemplateRef          *string                `json:"template_ref"`
	TemplateHash         *string                `json:"template_hash"`
	TemplateMissing      bool                   `json:"template_missing"`
	Defaults             Object                 `json:"defaults"`
	Overrides            Object                 `json:"overrides"`
	Effective            Object                 `json:"effective"`
	Fields               map[string]FieldOrigin `json:"fields"`
}
type FieldOrigin struct {
	Source  string `json:"source"`
	Differs *bool  `json:"differs_from_template"`
}
type ConfigurationPatch struct {
	Revision int64                      `json:"revision"`
	Set      map[string]json.RawMessage `json:"set"`
	Unset    []string                   `json:"unset"`
	// NewCategories are typed names added to the categories set in the same transaction.
	NewCategories []string `json:"new_categories,omitempty"`
}

// File/API format has no nullable field. Operation keys are validated separately.
func (p *ConfigurationPatch) UnmarshalJSON(raw []byte) error {
	type plain ConfigurationPatch
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode((*plain)(p)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalidDirectory
		}
	}
	if p.Revision < 1 {
		return invalidf("positive revision is required")
	}
	return nil
}

// ownedConfig is an entity's authoritative configuration: a template reference
// with sparse overrides, or an independent spec.
type ownedConfig struct {
	Ref       *string
	Overrides Object
	Spec      Object
}
type trustedDistribution struct {
	Provider   string
	Descriptor presets.Descriptor
	Digest     string
}

type templateSnapshot struct {
	Kind, Key string
	Schema    int
	Metadata  Object
	Spec      Object
	Hash      string
	Present   bool
}

// SetConfigurationPrepare installs the runtime coordinator called by every
// configuration write before it commits (see writeConfiguration).
func (s *Store) SetConfigurationPrepare(prepare func(DirectorySnapshot) (ConfigurationPublication, error)) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.prepareConfiguration = prepare
}

// SetInitialConfigurationPrepare installs a startup coordinator without replacing
// the running server's composite publication callback.
func (s *Store) SetInitialConfigurationPrepare(prepare func(DirectorySnapshot) (ConfigurationPublication, error)) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.prepareConfiguration == nil {
		s.prepareConfiguration = prepare
	}
}

func (s *Store) SetDistributionValidation(validate func([]presets.Descriptor) error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.validateDistributions = validate
}

// object converts a typed value to its generic JSON form.
func object(value any) Object {
	b, _ := json.Marshal(value)
	var out Object
	_ = json.Unmarshal(b, &out)
	return out
}

// cloneObject deep-copies a generic JSON object.
func cloneObject(in Object) Object {
	if in == nil {
		return nil
	}
	return Object(cloneValue(map[string]any(in)).(map[string]any))
}
func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneValue(item)
		}
		return out
	case Object:
		return map[string]any(cloneObject(v))
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		return slices.Clone(v)
	case nil, string, bool, float64, json.Number:
		return v
	}
	// Typed values (structs, typed slices) are converted to their JSON form.
	var out any
	_ = json.Unmarshal(encode(value), &out)
	return out
}
func encode(value any) []byte             { b, _ := json.Marshal(value); return b }
func configKey(kind, uid string) string   { return kind + ":" + uid }
func templateKey(kind, key string) string { return kind + ":" + key }
func paths(kind string) []string {
	out := []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "proxy"}
	if kind == "Vendor" {
		return append(out, "localized_icons.en", "localized_icons.zh-CN")
	}
	return append(out, "instructions.en", "instructions.zh-CN", "base_url", "base_urls", "source_strategy", "cache_ttl_seconds", "http_policy.rules", "http_policy.auto_cleanup", "http_policy.stale_fallback", "retention", "prewarm", "categories", "tags")
}

// AppPathApplies reports whether an application configuration path (or a
// top-level spec field such as http_policy) applies to the provider: base_url
// to release providers, base_urls, source_strategy and http_policy to
// http-cache, retention and prewarm to release providers and
// cache_ttl_seconds to every provider with an upstream.
func AppPathApplies(provider, path string) bool {
	field, _, _ := strings.Cut(path, ".")
	switch field {
	case "base_url", "retention", "prewarm":
		return presets.VersionsProvider(provider)
	case "base_urls", "source_strategy", "http_policy":
		return provider == "http-cache"
	case "cache_ttl_seconds":
		return provider == "http-cache" || presets.VersionsProvider(provider)
	}
	return true
}
func leaf(in Object, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = in
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			if o, yes := cur.(Object); yes {
				m = map[string]any(o)
			} else {
				return nil, false
			}
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
func setLeaf(in Object, path string, value any) {
	parts := strings.Split(path, ".")
	cur := map[string]any(in)
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}
func unsetLeaf(in Object, path string) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 1 {
		delete(in, path)
		return
	}
	child, ok := in[parts[0]].(map[string]any)
	if !ok {
		return
	}
	delete(child, parts[1])
	if len(child) == 0 {
		delete(in, parts[0])
	}
}

// merge overlays sparse overrides on a template spec. Ordered lists, proxy,
// prewarm and retention are whole leaves; other objects merge per key.
func merge(base, over Object) Object {
	out := cloneObject(base)
	if out == nil {
		out = Object{}
	}
	for key, value := range over {
		if key == "proxy" || key == "prewarm" || key == "retention" {
			out[key] = cloneValue(value)
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			old, _ := out[key].(map[string]any)
			out[key] = map[string]any(merge(Object(old), Object(nested)))
		} else {
			out[key] = cloneValue(value)
		}
	}
	return out
}
func strict(value Object, out any) error {
	d := json.NewDecoder(bytes.NewReader(encode(value)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return invalidf("%v", err)
	}
	return nil
}
func noNull(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case map[string]any:
		for _, n := range x {
			if !noNull(n) {
				return false
			}
		}
	case []any:
		for _, n := range x {
			if !noNull(n) {
				return false
			}
		}
	}
	return true
}
func validateOverrides(kind string, over Object) error {
	allowed := map[string]bool{}
	for _, p := range paths(kind) {
		allowed[p] = true
	}
	var walk func(map[string]any, string) error
	walk = func(m map[string]any, prefix string) error {
		for key, v := range m {
			p := prefix + key
			if allowed[p] {
				if !noNull(v) {
					return ErrInvalidDirectory
				}
				continue
			}
			child, ok := v.(map[string]any)
			if !ok || len(child) == 0 {
				return invalidf("unknown override %s", p)
			}
			if err := walk(child, p+"."); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(map[string]any(over), "")
}

// validateOwnedConfig checks the shape of an authoritative configuration:
// exactly one of a template reference or a spec, and only known overrides.
func validateOwnedConfig(kind string, c ownedConfig) error {
	if c.Overrides == nil {
		return ErrInvalidDirectory
	}
	if c.Ref == nil {
		if c.Spec == nil || !noNull(map[string]any(c.Spec)) || len(c.Overrides) != 0 {
			return ErrInvalidDirectory
		}
	} else if c.Spec != nil {
		return ErrInvalidDirectory
	}
	return validateOverrides(kind, c.Overrides)
}

// effective merges the configuration over its template. Application specs get
// their provider defaults and normalized sources.
func (w *configSet) effective(kind string, c ownedConfig) (Object, error) {
	var effective Object
	if c.Ref == nil {
		effective = cloneObject(c.Spec)
	} else {
		t, err := w.template(kind, *c.Ref)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, invalidf("unknown template %s", *c.Ref)
		}
		effective = merge(t.Spec, c.Overrides)
	}
	if effective == nil {
		return nil, ErrInvalidDirectory
	}
	if kind == "App" {
		var spec presets.AppSpec
		if err := strict(effective, &spec); err != nil {
			return nil, err
		}
		if spec.Categories == nil {
			spec.Categories = []string{}
		}
		if spec.Tags == nil {
			spec.Tags = []string{}
		}
		effective["categories"] = spec.Categories
		effective["tags"] = spec.Tags
		if spec.Prewarm != nil {
			if err := spec.Prewarm.Validate(spec.Provider); err != nil {
				return nil, ErrInvalidDirectory
			}
		} else if presets.VersionsProvider(spec.Provider) {
			effective["prewarm"] = object(presets.DefaultPrewarm())
		}
		if spec.Retention != nil {
			if !presets.VersionsProvider(spec.Provider) || spec.Retention.Validate() != nil {
				return nil, ErrInvalidDirectory
			}
		} else if presets.VersionsProvider(spec.Provider) {
			effective["retention"] = object(presets.DefaultRetention())
		}
		input := ApplicationInput{ID: "effective", Name: LocalizedText{spec.Name.En, spec.Name.ZhCN}, Description: LocalizedText{spec.Description.En, spec.Description.ZhCN}, Icon: spec.Icon, Provider: spec.Provider, BaseURL: spec.BaseURL, BaseURLs: spec.BaseURLs, SourceStrategy: spec.SourceStrategy, CacheTTLSeconds: spec.CacheTTLSeconds}
		if err := validateApplication(&input); err != nil {
			return nil, err
		}
		effective["base_url"], effective["base_urls"], effective["source_strategy"] = input.BaseURL, input.BaseURLs, input.SourceStrategy
		if input.BaseURLs == nil {
			effective["base_urls"] = []string{}
		}
	}
	return effective, nil
}

// ownProxy is the entity's own proxy setting. proxy is a whole leaf, so it is
// the override when present and the template's or independent spec's otherwise.
func (w *configSet) ownProxy(kind string, c ownedConfig) (networkproxy.Config, error) {
	value, ok := c.Spec["proxy"]
	if c.Ref != nil {
		if value, ok = c.Overrides["proxy"]; !ok {
			t, err := w.template(kind, *c.Ref)
			if err != nil {
				return networkproxy.Config{}, err
			}
			if t == nil {
				return networkproxy.Config{}, invalidf("unknown template %s", *c.Ref)
			}
			value = t.Spec["proxy"]
		}
	}
	proxy, err := decodeProxy(value)
	if err != nil {
		return proxy, ErrInvalidDirectory
	}
	return proxy, nil
}

// objectProxy is the proxy an entity uses: an application's resolved through
// its vendor and the global setting, a vendor's resolved to the global
// setting. own replaces the entity's own setting when not nil.
func (w *configSet) objectProxy(kind, uid string, own *networkproxy.Config) (networkproxy.Config, error) {
	global, err := w.globalProxy()
	if err != nil {
		return networkproxy.Config{}, err
	}
	if kind == "Vendor" {
		v, err := w.vendor(uid)
		if err != nil || v == nil {
			return networkproxy.Config{}, missingEntity(err)
		}
		proxy := own
		if proxy == nil {
			p, err := w.ownProxy("Vendor", v.config)
			if err != nil {
				return p, err
			}
			proxy = &p
		}
		if proxy.Mode == "inherit" {
			return global.config, nil
		}
		return *proxy, nil
	}
	e, err := w.app(uid)
	if err != nil || e == nil {
		return networkproxy.Config{}, missingEntity(err)
	}
	effective, err := w.appProxy(e, own, global.config)
	return effective.Config, err
}

// appProxy resolves an application's proxy through its vendor and the global setting.
func (w *configSet) appProxy(e *appEntry, own *networkproxy.Config, global networkproxy.Config) (networkproxy.Effective, error) {
	v, err := w.vendor(e.VendorUID)
	if err != nil || v == nil {
		return networkproxy.Effective{}, missingEntity(err)
	}
	app := own
	if app == nil {
		p, err := w.ownProxy("App", e.config)
		if err != nil {
			return networkproxy.Effective{}, err
		}
		app = &p
	}
	vendor, err := w.ownProxy("Vendor", v.config)
	if err != nil {
		return networkproxy.Effective{}, err
	}
	return networkproxy.Resolve(*app, e.Key, vendor, v.ID, global), nil
}
func missingEntity(err error) error {
	if err != nil {
		return err
	}
	return ErrInvalidDirectory
}

// view renders the administrator's configuration document of one entity.
func (w *configSet) view(kind, uid string) (Configuration, error) {
	var c ownedConfig
	var revision, instructions int64
	var provider string
	if kind == "Vendor" {
		v, err := w.vendor(uid)
		if err != nil || v == nil {
			return Configuration{}, missingEntity(err)
		}
		c, revision = v.config, v.Revision
	} else {
		a, err := w.app(uid)
		if err != nil || a == nil {
			return Configuration{}, missingEntity(err)
		}
		c, revision, instructions, provider = a.config, a.Revision, a.instructions.Revision, a.Provider
	}
	effective, err := w.effective(kind, c)
	if err != nil {
		return Configuration{}, err
	}
	out := Configuration{InstructionsRevision: instructions, Revision: revision, TemplateRef: c.Ref, Overrides: cloneObject(c.Overrides), Effective: effective, Fields: map[string]FieldOrigin{}}
	if out.Overrides == nil {
		out.Overrides = Object{}
	}
	if c.Ref != nil {
		t, err := w.template(kind, *c.Ref)
		if err != nil {
			return Configuration{}, err
		}
		hash := t.Hash
		out.Defaults = cloneObject(t.Spec)
		out.TemplateHash = &hash
		out.TemplateMissing = !t.Present
	}
	if kind == "App" {
		provider = fmt.Sprint(effective["provider"])
	}
	for _, p := range paths(kind) {
		if (p == "retention" || p == "prewarm") && !presets.VersionsProvider(provider) {
			continue
		}
		if strings.HasPrefix(p, "http_policy.") && provider != "http-cache" {
			continue
		}
		f := FieldOrigin{Source: "custom"}
		if c.Ref != nil {
			if _, ok := leaf(c.Overrides, p); !ok {
				f.Source = "inherited"
			}
			a, _ := leaf(effective, p)
			b, _ := leaf(out.Defaults, p)
			diff := !reflect.DeepEqual(a, b)
			f.Differs = &diff
		}
		out.Fields[p] = f
	}
	if kind == "App" {
		a, _ := w.app(uid)
		global, err := w.globalProxy()
		if err != nil {
			return Configuration{}, err
		}
		if out.ProxyEffective, err = w.appProxy(a, nil, global.config); err != nil {
			return Configuration{}, err
		}
	} else {
		v, _ := w.vendor(uid)
		global, err := w.globalProxy()
		if err != nil {
			return Configuration{}, err
		}
		proxy, err := w.ownProxy("Vendor", c)
		if err != nil {
			return Configuration{}, err
		}
		out.ProxyEffective = networkproxy.Resolve(networkproxy.Inherit(), "", proxy, v.ID, global.config)
	}
	return out, nil
}

// readConfiguration runs read against a working set in one read transaction.
func (s *Store) readConfiguration(read func(*configSet) error) error {
	tx, err := s.read.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return read(newConfigSet(tx))
}

func (s *Store) VendorConfiguration(id string) (Configuration, error) {
	var out Configuration
	err := s.readConfiguration(func(w *configSet) error {
		v, err := w.vendorByID(id)
		if err != nil {
			return err
		}
		if v == nil {
			return sql.ErrNoRows
		}
		out, err = w.view("Vendor", v.UID)
		return err
	})
	return out, err
}
func (s *Store) ApplicationConfiguration(key string) (Configuration, error) {
	var out Configuration
	err := s.readConfiguration(func(w *configSet) error {
		a, err := w.appByKey(key)
		if err != nil {
			return err
		}
		if a == nil {
			return sql.ErrNoRows
		}
		out, err = w.view("App", a.UID)
		return err
	})
	return out, err
}

func vendorSpec(v Vendor) Object {
	return object(presets.VendorSpec{Proxy: networkproxy.Inherit(), Name: presets.Text{En: v.Name.En, ZhCN: v.Name.ZhCN}, Description: presets.Text{En: v.Description.En, ZhCN: v.Description.ZhCN}, Icon: v.Icon, LocalizedIcons: presets.Text{En: v.LocalizedIcons.En, ZhCN: v.LocalizedIcons.ZhCN}})
}

func appSpec(a Application, instructions LocalizedText, policy cachepolicy.Config) Object {
	spec := presets.AppSpec{Categories: append([]string{}, a.Categories...), Tags: append([]string{}, a.Tags...), Proxy: networkproxy.Inherit(), Name: presets.Text{En: a.Name.En, ZhCN: a.Name.ZhCN}, Description: presets.Text{En: a.Description.En, ZhCN: a.Description.ZhCN}, Icon: a.Icon, Provider: a.Provider, BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CacheTTLSeconds: a.CacheTTLSeconds, Instructions: presets.Text{En: instructions.En, ZhCN: instructions.ZhCN}}
	if presets.VersionsProvider(a.Provider) {
		spec.Retention = presets.DefaultRetention()
		spec.Prewarm = presets.DefaultPrewarm()
	}
	if spec.BaseURLs == nil {
		spec.BaseURLs = []string{}
	}
	if a.Provider == "http-cache" {
		spec.HTTPPolicy = &policy
	}
	return object(spec)
}
func validateInstructions(value LocalizedText) error {
	for _, text := range []string{value.En, value.ZhCN} {
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 12000 {
			return ErrInvalidDirectory
		}
		for _, r := range text {
			if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
				return ErrInvalidDirectory
			}
		}
	}
	return nil
}

func applyPatch(c *ownedConfig, kind string, patch ConfigurationPatch) error {
	allowed := map[string]bool{}
	for _, p := range paths(kind) {
		allowed[p] = true
	}
	used := map[string]bool{}
	for p := range patch.Set {
		if p == "categories" || p == "tags" {
			var values []string
			if json.Unmarshal(patch.Set[p], &values) != nil || bytes.Equal(bytes.TrimSpace(patch.Set[p]), []byte("null")) {
				return ErrInvalidDirectory
			}
			normalize := presets.NormalizeTags
			if p == "categories" {
				normalize = presets.NormalizeCategories
			}
			normalized, err := normalize(values)
			if err != nil {
				return invalidf("%s", err)
			}
			patch.Set[p] = encode(normalized)
		}
		if p == "prewarm" {
			var value presets.Prewarm
			if json.Unmarshal(patch.Set[p], &value) != nil {
				return ErrInvalidDirectory
			}
		}
		if p == "retention" {
			var value presets.Retention
			if json.Unmarshal(patch.Set[p], &value) != nil {
				return ErrInvalidDirectory
			}
		}
		if !allowed[p] {
			return invalidf("field %s cannot be overridden", p)
		}
		used[p] = true
	}
	for _, p := range patch.Unset {
		if !allowed[p] || used[p] {
			return invalidf("invalid or conflicting unset %s", p)
		}
		used[p] = true
		if c.Ref == nil {
			return invalidf("independent configuration has no inheritance source")
		}
	}
	if c.Overrides == nil {
		c.Overrides = Object{}
	}
	target := c.Overrides
	if c.Ref == nil {
		target = c.Spec
	}
	for p, raw := range patch.Set {
		var value any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&value); err != nil {
			return ErrInvalidDirectory
		}
		if d.Decode(new(any)) != io.EOF || !noNull(value) {
			return ErrInvalidDirectory
		}
		setLeaf(target, p, value)
	}
	for _, p := range patch.Unset {
		unsetLeaf(c.Overrides, p)
	}
	return validateOverrides(kind, c.Overrides)
}
func patchObject(spec Object, kind string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, p := range paths(kind) {
		if v, ok := leaf(spec, p); ok {
			out[p] = encode(v)
		}
	}
	return out
}

// patchConfiguration applies a sparse patch to one vendor or application. It
// reads and writes only that entity (plus its template, its vendor's proxy and
// the categories it names) and fails with ErrConflict when the entity's
// revision is not the expected one.
func (s *Store) patchConfiguration(kind, key string, patch ConfigurationPatch, enabled *bool, instructionRevision *int64) error {
	return s.writeConfiguration(func(w *configSet) error {
		var c *ownedConfig
		var revision *int64
		var setEnabled, changed *bool
		var app *appEntry
		if kind == "Vendor" {
			v, err := w.vendorByID(key)
			if err != nil {
				return err
			}
			if v == nil {
				return sql.ErrNoRows
			}
			if v.DeletedAt != nil {
				return ErrDirectoryDeleted
			}
			c, revision, setEnabled, changed = &v.config, &v.Revision, &v.Enabled, &v.changed
		} else {
			a, err := w.appByKey(key)
			if err != nil {
				return err
			}
			if a == nil {
				return sql.ErrNoRows
			}
			if a.DeletedAt != nil {
				return ErrDirectoryDeleted
			}
			c, revision, setEnabled, changed, app = &a.config, &a.Revision, &a.Enabled, &a.changed, a
		}
		if *revision != patch.Revision {
			return ErrConflict
		}
		if instructionRevision != nil && app.instructions.Revision != *instructionRevision {
			return ErrConflict
		}
		if len(patch.NewCategories) > 0 {
			raw, ok := patch.Set["categories"]
			var ids []string
			if kind != "App" || !ok || json.Unmarshal(raw, &ids) != nil {
				return invalidf("new categories require the categories field")
			}
			created, err := w.resolveNewCategories(patch.NewCategories)
			if err != nil {
				return err
			}
			// Copy before adding resolved IDs so the caller's patch is never changed.
			set := make(map[string]json.RawMessage, len(patch.Set))
			for key, value := range patch.Set {
				set[key] = value
			}
			set["categories"] = encode(append(ids, created...))
			patch.Set = set
		}
		if len(patch.Set) == 0 && len(patch.Unset) == 0 && enabled == nil {
			return nil
		}
		if app != nil && app.Provider == "http-cache" {
			if raw, ok := patch.Set["base_url"]; ok {
				var base string
				if json.Unmarshal(raw, &base) != nil {
					return ErrInvalidDirectory
				}
				normalized, e := normalizeDirectoryBase(base)
				if e != nil {
					return e
				}
				if _, hasList := patch.Set["base_urls"]; !hasList && normalized != app.BaseURL {
					return invalidf("HTTP cache sources use base_urls; base_url must match the first source")
				}
			}
		}
		if raw, ok := patch.Set["proxy"]; ok {
			var submitted networkproxy.Config
			if json.Unmarshal(raw, &submitted) != nil {
				return ErrInvalidDirectory
			}
			saved, err := w.ownProxy(kind, *c)
			if err != nil {
				return err
			}
			kept, err := networkproxy.KeepRedactedPassword(submitted, saved)
			if err != nil {
				return invalidf("%w", err)
			}
			set := make(map[string]json.RawMessage, len(patch.Set))
			for key, value := range patch.Set {
				set[key] = value
			}
			set["proxy"] = encode(kept)
			patch.Set = set
		}
		if err := applyPatch(c, kind, patch); err != nil {
			return err
		}
		*revision++
		if enabled != nil {
			*setEnabled = *enabled
		}
		*changed = true
		return nil
	}, nil)
}
func (s *Store) PatchVendorConfiguration(id string, patch ConfigurationPatch) (Configuration, error) {
	if err := s.patchConfiguration("Vendor", id, patch, nil, nil); err != nil {
		return Configuration{}, err
	}
	return s.VendorConfiguration(id)
}

// PatchApplicationConfiguration applies an administrator patch. Every set or
// unset path must apply to the application's provider (AppPathApplies).
func (s *Store) PatchApplicationConfiguration(key string, patch ConfigurationPatch) (Configuration, error) {
	app, err := s.Application(key)
	if err != nil {
		return Configuration{}, err
	}
	for path := range patch.Set {
		if !AppPathApplies(app.Provider, path) {
			return Configuration{}, invalidf("%s does not apply to the %s provider", path, app.Provider)
		}
	}
	for _, path := range patch.Unset {
		if !AppPathApplies(app.Provider, path) {
			return Configuration{}, invalidf("%s does not apply to the %s provider", path, app.Provider)
		}
	}
	if err := s.patchConfiguration("App", key, patch, nil, nil); err != nil {
		return Configuration{}, err
	}
	return s.ApplicationConfiguration(key)
}

func semanticHash(schema int, kind string, metadata, spec Object) string {
	payload := struct {
		Schema   int    `json:"schema_version"`
		Kind     string `json:"kind"`
		Metadata Object `json:"metadata"`
		Spec     Object `json:"spec"`
	}{schema, kind, metadata, spec}
	sum := sha256.Sum256(encode(payload))
	return hex.EncodeToString(sum[:])
}
func snapshots(set presets.Set) ([]templateSnapshot, error) {
	var out []templateSnapshot
	for _, v := range set.Vendors {
		v.Spec.Proxy = normalizedProxy(v.Spec.Proxy)
		spec := object(v.Spec)
		hash := semanticHash(v.SchemaVersion, v.Kind, object(v.Metadata), spec)
		icon, err := set.Icon(v.Spec.Icon)
		if err != nil {
			return nil, err
		}
		spec["icon"] = icon
		for _, lang := range []string{"en", "zh-CN"} {
			p := "localized_icons." + lang
			raw, _ := leaf(spec, p)
			icon, err = set.Icon(raw.(string))
			if err != nil {
				return nil, err
			}
			setLeaf(spec, p, icon)
		}
		out = append(out, templateSnapshot{Kind: "Vendor", Key: v.Metadata.ID, Schema: v.SchemaVersion, Metadata: object(v.Metadata), Spec: spec, Hash: hash, Present: true})
	}
	for _, a := range set.Apps {
		normalized := a.Spec
		normalized.Categories, _ = presets.NormalizeCategories(normalized.Categories)
		normalized.Tags, _ = presets.NormalizeTags(normalized.Tags)
		normalized.Proxy = normalizedProxy(normalized.Proxy)
		input := ApplicationInput{ID: a.Metadata.ID, Name: LocalizedText{a.Spec.Name.En, a.Spec.Name.ZhCN}, Description: LocalizedText{a.Spec.Description.En, a.Spec.Description.ZhCN}, Provider: a.Spec.Provider, BaseURL: a.Spec.BaseURL, BaseURLs: a.Spec.BaseURLs, SourceStrategy: a.Spec.SourceStrategy, CacheTTLSeconds: a.Spec.CacheTTLSeconds}
		if err := validateApplication(&input); err != nil {
			return nil, err
		}
		normalized.BaseURL, normalized.BaseURLs, normalized.SourceStrategy = input.BaseURL, input.BaseURLs, input.SourceStrategy
		if normalized.BaseURLs == nil {
			normalized.BaseURLs = []string{}
		}
		if normalized.Provider == "http-cache" {
			policy := cachepolicy.Empty()
			if normalized.HTTPPolicy != nil {
				policy = *normalized.HTTPPolicy
			}
			var err error
			policy, err = cachepolicy.Normalize(policy)
			if err != nil {
				return nil, err
			}
			normalized.HTTPPolicy = &policy
		}
		if presets.VersionsProvider(normalized.Provider) && normalized.Retention == nil {
			normalized.Retention = presets.DefaultRetention()
		}
		if normalized.Retention != nil && (!presets.VersionsProvider(normalized.Provider) || normalized.Retention.Validate() != nil) {
			return nil, ErrInvalidDirectory
		}
		if presets.VersionsProvider(normalized.Provider) && normalized.Prewarm == nil {
			normalized.Prewarm = presets.DefaultPrewarm()
		}
		if normalized.Prewarm != nil && normalized.Prewarm.Validate(normalized.Provider) != nil {
			return nil, ErrInvalidDirectory
		}
		spec := object(normalized)
		if _, ok := spec["base_urls"]; !ok {
			spec["base_urls"] = []any{}
		}
		if a.Spec.Provider == "http-cache" {
			if _, ok := spec["http_policy"]; !ok {
				spec["http_policy"] = object(cachepolicy.Empty())
			}
		}
		hash := semanticHash(a.SchemaVersion, a.Kind, object(a.Metadata), spec)
		icon, err := set.Icon(a.Spec.Icon)
		if err != nil {
			return nil, err
		}
		spec["icon"] = icon
		out = append(out, templateSnapshot{Kind: "App", Key: a.Key(), Schema: a.SchemaVersion, Metadata: object(a.Metadata), Spec: spec, Hash: hash, Present: true})
	}
	sort.Slice(out, func(i, j int) bool {
		return templateKey(out[i].Kind, out[i].Key) < templateKey(out[j].Kind, out[j].Key)
	})
	return out, nil
}

func templateHash(t *templateSnapshot) string {
	if t == nil {
		return ""
	}
	return t.Hash
}
func distributionDigestOf(d *trustedDistribution) string {
	if d == nil {
		return ""
	}
	return d.Digest
}

// ReconcileTemplates validates all proposed effective entities before any write.
// Missing templates retain the last accepted snapshot and freeze effective data.
// A template can affect every entity bound to it, so this is the one
// configuration write that loads the whole configuration.
func (s *Store) ReconcileTemplates(set presets.Set) error {
	if err := set.ValidateTaxonomyReferences(); err != nil {
		return err
	}
	incoming, err := snapshots(set)
	if err != nil {
		return err
	}
	return s.writeConfiguration(func(w *configSet) error {
		if err := w.loadEverything(); err != nil {
			return err
		}
		if err := w.reconcileTaxonomy(set.Taxonomy); err != nil {
			return err
		}
		previousDistributions := map[string]string{}
		for key, d := range w.distributions {
			previousDistributions[key] = distributionDigestOf(d)
		}
		providers := map[string]string{}
		for _, a := range set.Apps {
			providers[a.Key()] = a.Spec.Provider
		}
		for _, d := range set.Descriptors() {
			w.putDistribution(trustedDistribution{Provider: providers[d.ID], Descriptor: d, Digest: distributionDigest(d)})
		}
		previous := map[string]*templateSnapshot{}
		for key, t := range w.templates {
			if t == nil {
				continue
			}
			previous[key] = t
			missing := *t
			missing.Present = false
			w.templates[key] = &missing
		}
		for _, t := range incoming {
			key := templateKey(t.Kind, t.Key)
			if prev := previous[key]; prev != nil {
				if !reflect.DeepEqual(prev.Metadata, t.Metadata) {
					return invalidf("template identity changed")
				}
				if t.Kind == "App" && prev.Spec["provider"] != t.Spec["provider"] {
					return invalidf("template Provider changed")
				}
			}
			w.putTemplate(t)
		}
		for _, v := range w.sortedVendors() {
			if v.config.Ref != nil {
				key := templateKey("Vendor", *v.config.Ref)
				if templateHash(previous[key]) != templateHash(w.templates[key]) {
					v.Revision++
					v.changed = true
				}
			}
		}
		for _, a := range w.sortedApps() {
			changed := false
			if a.config.Ref != nil {
				key := templateKey("App", *a.config.Ref)
				changed = templateHash(previous[key]) != templateHash(w.templates[key])
			}
			if key := distributionKey(a); key != "" && previousDistributions[key] != distributionDigestOf(w.distributions[key]) {
				changed = true
			}
			if changed {
				a.Revision++
				a.changed = true
			}
		}
		for _, t := range incoming {
			if t.Kind != "Vendor" {
				continue
			}
			if existing, err := w.vendorByID(t.Key); err != nil {
				return err
			} else if existing != nil {
				continue
			}
			uid, err := identity.NewUID()
			if err != nil {
				return err
			}
			ref := t.Key
			w.addVendor(Vendor{UID: uid, ID: t.Key, Revision: 1}, ownedConfig{Ref: &ref, Overrides: Object{}})
		}
		for _, t := range incoming {
			if t.Kind != "App" {
				continue
			}
			if existing, err := w.appByKey(t.Key); err != nil {
				return err
			} else if existing != nil {
				continue
			}
			vendor, id, _ := strings.Cut(t.Key, "/")
			parent, err := w.vendorByID(vendor)
			if err != nil {
				return err
			}
			if parent == nil || parent.DeletedAt != nil {
				continue
			}
			uid, err := identity.NewUID()
			if err != nil {
				return err
			}
			ref := t.Key
			w.addApp(Application{UID: uid, ID: id, Key: t.Key, VendorID: vendor, VendorUID: parent.UID, Provider: t.Spec["provider"].(string), Revision: 1, SourceEpoch: 1}, ownedConfig{Ref: &ref, Overrides: Object{}})
		}
		return nil
	}, nil)
}

func (s *Store) CreateConfiguredVendor(in VendorInput, ref *string) (Vendor, error) {
	err := s.writeConfiguration(func(w *configSet) error {
		if existing, err := w.vendorByID(in.ID); err != nil {
			return err
		} else if existing != nil {
			return ErrDirectoryExists
		}
		if err := validateVendor(in); err != nil {
			return err
		}
		uid, err := identity.NewUID()
		if err != nil {
			return err
		}
		v := Vendor{UID: uid, ID: in.ID, Name: in.Name, Description: in.Description, Icon: in.Icon, LocalizedIcons: in.LocalizedIcons, Enabled: in.Enabled, Revision: 1}
		c := ownedConfig{Overrides: Object{}, Spec: vendorSpec(v)}
		if ref != nil {
			t, err := w.template("Vendor", *ref)
			if err != nil {
				return err
			}
			if t == nil || !t.Present {
				return invalidf("unknown template")
			}
			c.Ref = ref
			c.Spec = nil
		}
		w.addVendor(v, c)
		return nil
	}, nil)
	if err != nil {
		return Vendor{}, err
	}
	return s.Vendor(in.ID)
}
func (s *Store) CreateConfiguredApplication(vendor string, in ApplicationInput, ref *string) (Application, error) {
	categories, catErr := presets.NormalizeCategories(in.Categories)
	tags, tagErr := presets.NormalizeTags(in.Tags)
	if catErr != nil || tagErr != nil {
		return Application{}, ErrInvalidDirectory
	}
	in.Categories, in.Tags = categories, tags
	err := s.writeConfiguration(func(w *configSet) error {
		if existing, err := w.appByKey(vendor + "/" + in.ID); err != nil {
			return err
		} else if existing != nil {
			return ErrDirectoryExists
		}
		if err := validateApplication(&in); err != nil {
			return err
		}
		parent, err := w.vendorByID(vendor)
		if err != nil {
			return err
		}
		if parent == nil {
			return ErrVendorNotFound
		}
		if parent.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		uid, err := identity.NewUID()
		if err != nil {
			return err
		}
		a := Application{Categories: in.Categories, Tags: in.Tags, UID: uid, ID: in.ID, Key: vendor + "/" + in.ID, VendorID: vendor, VendorUID: parent.UID, Name: in.Name, Description: in.Description, Icon: in.Icon, Provider: in.Provider, BaseURL: in.BaseURL, BaseURLs: in.BaseURLs, SourceStrategy: in.SourceStrategy, CacheTTLSeconds: in.CacheTTLSeconds, Enabled: in.Enabled, Revision: 1, SourceEpoch: 1}
		c := ownedConfig{Overrides: Object{}, Spec: appSpec(a, LocalizedText{}, cachepolicy.Empty())}
		if ref != nil {
			t, err := w.template("App", *ref)
			if err != nil {
				return err
			}
			if t == nil || !t.Present || t.Spec["provider"] != in.Provider {
				return invalidf("unknown or incompatible template")
			}
			c.Ref = ref
			c.Spec = nil
		}
		w.addApp(a, c)
		return nil
	}, nil)
	if err != nil {
		return Application{}, err
	}
	return s.Application(vendor + "/" + in.ID)
}
func (s *Store) PatchVendorFields(id string, revision int64, set map[string]json.RawMessage, enabled *bool) (Vendor, error) {
	if err := s.patchConfiguration("Vendor", id, ConfigurationPatch{Revision: revision, Set: set}, enabled, nil); err != nil {
		return Vendor{}, err
	}
	return s.Vendor(id)
}
func (s *Store) PatchApplicationFields(key string, revision int64, set map[string]json.RawMessage, enabled *bool) (Application, error) {
	if err := s.patchConfiguration("App", key, ConfigurationPatch{Revision: revision, Set: set}, enabled, nil); err != nil {
		return Application{}, err
	}
	return s.Application(key)
}

func (s *Store) canonicalApplicationProtected(key string) (bool, error) {
	if _, ok := BuiltinApplicationTemplate(key); ok {
		return true, nil
	}
	var count int
	err := s.read.QueryRow(`SELECT count(*) FROM template_snapshots WHERE kind='App' AND canonical_key=?`, key).Scan(&count)
	return count != 0, err
}

func distributionDigest(d presets.Descriptor) string {
	contract := struct {
		Protocol   string              `json:"protocol"`
		Channels   []string            `json:"channels"`
		Trust      int64               `json:"trust_revision"`
		Validator  string              `json:"installer_validator"`
		Installers []presets.Installer `json:"installers"`
		Assets     []presets.Asset     `json:"assets"`
		Policy     presets.Localized   `json:"update_policy"`
	}{d.Protocol, d.Channels, d.TrustRevision, d.InstallerValidator, d.Installers, d.Assets, d.UpdatePolicy}
	sum := sha256.Sum256(encode(contract))
	return hex.EncodeToString(sum[:])
}

// distributionKey names the trusted distribution an application runs with: its
// template reference, or the provider's built-in template for an independent
// configuration. Applications of content providers without a template have none.
func distributionKey(a *appEntry) string {
	if a.config.Ref != nil {
		return *a.config.Ref
	}
	key, _ := presets.ReleaseTemplateKey(a.Provider)
	return key
}

// acceptMissingContract gives an independent release instance the canonical
// compiled contract when no trusted build record exists yet. It never changes
// an existing snapshot.
func (w *configSet) acceptMissingContract(a *appEntry) error {
	if !presets.VersionsProvider(a.Provider) {
		return nil
	}
	key := distributionKey(a)
	if d, err := w.distribution(key); err != nil || d != nil {
		return err
	}
	set := presets.Embedded()
	for _, d := range set.Descriptors() {
		if d.ID != key {
			continue
		}
		incoming, err := snapshots(set)
		if err != nil {
			return err
		}
		for _, t := range incoming {
			if t.Kind != "App" || t.Key != key {
				continue
			}
			if t.Spec["provider"] != a.Provider {
				return ErrInvalidDirectory
			}
			if existing, err := w.template("App", key); err != nil {
				return err
			} else if existing == nil {
				w.putTemplate(t)
			}
		}
		w.putDistribution(trustedDistribution{Provider: a.Provider, Descriptor: d, Digest: distributionDigest(d)})
		return nil
	}
	return fmt.Errorf("missing trusted distribution for %s", a.Key)
}

func (s *Store) DirectoryConfigurationSnapshot() (DirectorySnapshot, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	view, err := s.reloadView()
	if err != nil {
		return DirectorySnapshot{}, err
	}
	return view.snapshot(), nil
}

func normalizedProxy(c networkproxy.Config) networkproxy.Config {
	if c.Mode == "" {
		return networkproxy.Inherit()
	}
	return c
}
func decodeProxy(value any) (networkproxy.Config, error) {
	var c networkproxy.Config
	err := json.Unmarshal(encode(value), &c)
	return c, err
}
func parseGlobalProxy(raw []byte) (networkproxy.Config, error) {
	var c networkproxy.Config
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || c.Validate(false) != nil {
		return networkproxy.Config{}, ErrInvalidDirectory
	}
	return c, nil
}

// PatchGlobalProxy changes the global proxy under CAS on its own revision. It
// rewrites no vendor or application; their effective proxies and runtime
// transports follow from the published view.
func (s *Store) PatchGlobalProxy(expected int64, c networkproxy.Config) (int64, error) {
	if err := c.Validate(false); err != nil {
		return 0, ErrInvalidDirectory
	}
	err := s.writeConfiguration(func(w *configSet) error {
		g, err := w.globalProxy()
		if err != nil {
			return err
		}
		if g.revision != expected {
			return ErrConflict
		}
		kept, err := networkproxy.KeepRedactedPassword(c, g.config)
		if err != nil {
			return invalidf("%w", err)
		}
		g.config = kept
		g.revision++
		return nil
	}, nil)
	if err != nil {
		return 0, err
	}
	return expected + 1, nil
}
