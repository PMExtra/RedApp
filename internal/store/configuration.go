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
	"sort"
	"strings"
	"time"
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
		return fmt.Errorf("%w: positive revision is required", ErrInvalidDirectory)
	}
	return nil
}

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
type configurationState struct {
	DirectorySnapshot
	Configs       map[string]ownedConfig
	Templates     map[string]templateSnapshot
	Instructions  map[string]Instructions
	Policies      map[string]cachepolicy.Config
	Pending       map[string]int64
	Distributions map[string]trustedDistribution
	Taxonomy      map[string]TaxonomyItem
	Notes         map[string]AdminNotes
	Seeded        bool
}

// SetConfigurationPrepare installs the runtime coordinator. Every authoritative
// Store configuration writer serializes through configMu, including direct users.
func (s *Store) SetConfigurationPrepare(prepare func(DirectorySnapshot) (ConfigurationPublication, error)) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.prepareConfiguration = prepare
}

func object(value any) Object {
	b, _ := json.Marshal(value)
	var out Object
	_ = json.Unmarshal(b, &out)
	return out
}
func cloneObject(in Object) Object {
	if in == nil {
		return nil
	}
	return object(in)
}
func encode(value any) []byte             { b, _ := json.Marshal(value); return b }
func configKey(kind, uid string) string   { return kind + ":" + uid }
func templateKey(kind, key string) string { return kind + ":" + key }
func paths(kind string) []string {
	out := []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "proxy"}
	if kind == "Vendor" {
		return append(out, "localized_icons.en", "localized_icons.zh-CN")
	}
	return append(out, "instructions.en", "instructions.zh-CN", "base_url", "base_urls", "source_strategy", "cache_ttl_seconds", "http_policy.rules", "http_policy.auto_cleanup", "http_policy.stale_fallback", "retention", "prewarm", "category", "tags")
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
func merge(base, over Object) Object {
	out := cloneObject(base)
	if out == nil {
		out = Object{}
	}
	for key, value := range over {
		if key == "proxy" || key == "prewarm" || key == "retention" {
			out[key] = cloneObject(Object{"leaf": value})["leaf"]
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			old, _ := out[key].(map[string]any)
			out[key] = map[string]any(merge(Object(old), Object(nested)))
		} else {
			out[key] = value
		}
	}
	return out
}
func strict(value Object, out any) error {
	d := json.NewDecoder(bytes.NewReader(encode(value)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDirectory, err)
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
				return fmt.Errorf("%w: unknown override %s", ErrInvalidDirectory, p)
			}
			if err := walk(child, p+"."); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(map[string]any(over), "")
}

func (st *configurationState) effective(kind, uid string) (Object, error) {
	c, ok := st.Configs[configKey(kind, uid)]
	if !ok {
		return nil, fmt.Errorf("missing authoritative configuration for %s", uid)
	}
	var effective Object
	if c.Ref == nil {
		effective = cloneObject(c.Spec)
	} else {
		t, ok := st.Templates[templateKey(kind, *c.Ref)]
		if !ok {
			return nil, fmt.Errorf("%w: unknown template %s", ErrInvalidDirectory, *c.Ref)
		}
		effective = merge(t.Spec, c.Overrides)
	}
	if kind == "App" {
		var spec presets.AppSpec
		if err := strict(effective, &spec); err != nil {
			return nil, err
		}
		if spec.Tags == nil {
			spec.Tags = []string{}
		}
		effective["category"] = spec.Category
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
func (st *configurationState) view(kind, uid string, revision int64) (Configuration, error) {
	c := st.Configs[configKey(kind, uid)]
	effective, err := st.effective(kind, uid)
	if err != nil {
		return Configuration{}, err
	}
	out := Configuration{InstructionsRevision: st.Instructions[uid].Revision, Revision: revision, TemplateRef: c.Ref, Overrides: cloneObject(c.Overrides), Effective: effective, Fields: map[string]FieldOrigin{}}
	if out.Overrides == nil {
		out.Overrides = Object{}
	}
	if c.Ref != nil {
		t := st.Templates[templateKey(kind, *c.Ref)]
		out.Defaults = cloneObject(t.Spec)
		out.TemplateHash = &t.Hash
		out.TemplateMissing = !t.Present
	}
	for _, p := range paths(kind) {
		if (p == "retention" || p == "prewarm") && !presets.VersionsProvider(fmt.Sprint(effective["provider"])) {
			continue
		}
		if strings.HasPrefix(p, "http_policy.") && effective["provider"] != "http-cache" {
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
		out.ProxyEffective = st.ProxyScopes[uid].Proxy
	} else {
		proxy, _ := decodeProxy(effective["proxy"])
		out.ProxyEffective = networkproxy.Resolve(networkproxy.Inherit(), "", proxy, func() string {
			for _, v := range st.Vendors {
				if v.UID == uid {
					return v.ID
				}
			}
			return ""
		}(), st.GlobalProxy)
	}
	return out, nil
}

func readConfigurationState(tx *sql.Tx) (configurationState, error) {
	st := configurationState{Configs: map[string]ownedConfig{}, Templates: map[string]templateSnapshot{}, Instructions: map[string]Instructions{}, Policies: map[string]cachepolicy.Config{}, Pending: map[string]int64{}, Distributions: map[string]trustedDistribution{}}
	if err := tx.QueryRow(`SELECT seeded FROM directory_state WHERE id=1`).Scan(&st.Seeded); err != nil {
		return st, err
	}
	var notesErr error
	st.Notes, notesErr = readAllNotes(tx)
	if notesErr != nil {
		return st, notesErr
	}
	var taxonomyErr error
	st.Taxonomy, taxonomyErr = readTaxonomy(tx)
	if taxonomyErr != nil {
		return st, taxonomyErr
	}
	rows, err := tx.Query(`SELECT ` + vendorColumns + ` FROM vendors ORDER BY id`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		v, e := scanVendor(rows)
		if e != nil {
			rows.Close()
			return st, e
		}
		st.Vendors = append(st.Vendors, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT ` + applicationColumns + ` FROM applications a JOIN vendors v ON v.uid=a.vendor_uid ORDER BY v.id,a.id`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		a, e := scanApplication(rows)
		if e != nil {
			rows.Close()
			return st, e
		}
		st.Applications = append(st.Applications, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT ` + sourceColumns + sourceJoin + ` ORDER BY src.app_uid,src.epoch`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		a, e := scanSource(rows)
		if e != nil {
			rows.Close()
			return st, e
		}
		st.Sources = append(st.Sources, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return st, err
	}
	for _, kind := range []string{"Vendor", "App"} {
		table := "vendor_config"
		if kind == "App" {
			table = "application_config"
		}
		rows, err = tx.Query(`SELECT entity_uid,template_ref,overrides_json,spec_json FROM ` + table)
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var uid string
			var ref sql.NullString
			var over, spec []byte
			if err = rows.Scan(&uid, &ref, &over, &spec); err != nil {
				break
			}
			c := ownedConfig{}
			if ref.Valid {
				c.Ref = &ref.String
			}
			if err = json.Unmarshal(over, &c.Overrides); err != nil {
				break
			}
			if spec != nil {
				if err = json.Unmarshal(spec, &c.Spec); err != nil {
					break
				}
			}
			st.Configs[configKey(kind, uid)] = c
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return st, err
		}
	}
	rows, err = tx.Query(`SELECT kind,canonical_key,schema_version,metadata_json,spec_json,semantic_hash,present FROM template_snapshots`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var t templateSnapshot
		var metadata, spec []byte
		if err = rows.Scan(&t.Kind, &t.Key, &t.Schema, &metadata, &spec, &t.Hash, &t.Present); err != nil {
			break
		}
		if err = json.Unmarshal(metadata, &t.Metadata); err != nil {
			break
		}
		if err = json.Unmarshal(spec, &t.Spec); err != nil {
			break
		}
		st.Templates[templateKey(t.Kind, t.Key)] = t
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT app_uid,en,zh_cn,revision FROM application_instructions`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var uid string
		var i Instructions
		if err = rows.Scan(&uid, &i.En, &i.ZhCN, &i.Revision); err != nil {
			break
		}
		st.Instructions[uid] = i
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT app_id,payload FROM settings WHERE scope='app' AND key='http_policy'`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var key string
		var raw []byte
		var c cachepolicy.Config
		if err = rows.Scan(&key, &raw); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			break
		}
		st.Policies[key] = c
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT canonical_key,provider,descriptor_json,distribution_digest FROM trusted_distribution_snapshots`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var key string
		var raw []byte
		var dist trustedDistribution
		if err = rows.Scan(&key, &dist.Provider, &raw, &dist.Digest); err != nil {
			break
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err = d.Decode(&dist.Descriptor); err != nil {
			break
		}
		if dist.Descriptor.ID != key || dist.Digest != distributionDigest(dist.Descriptor) {
			err = fmt.Errorf("invalid trusted distribution snapshot %s", key)
			break
		}
		st.Distributions[key] = dist
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return st, err
	}
	rows, err = tx.Query(`SELECT app_uid,requested_revision FROM pending_application_deletes`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var uid string
		var revision int64
		if err = rows.Scan(&uid, &revision); err != nil {
			break
		}
		st.Pending[uid] = revision
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		var raw []byte
		st.GlobalProxy = networkproxy.Direct()
		err = tx.QueryRow(`SELECT revision,payload FROM settings WHERE scope='global' AND app_id='' AND key='upstream_proxy'`).Scan(&st.GlobalProxyRevision, &raw)
		if err == sql.ErrNoRows {
			err = nil
		} else if err == nil {
			st.GlobalProxy, err = parseGlobalProxy(raw)
		}
		if err == nil {
			err = st.refreshProxyScopes()
		}
	}
	if err == nil {
		err = st.refreshReviewedContracts()
	}
	return st, err
}
func (s *Store) configurationState() (configurationState, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return configurationState{}, err
	}
	defer tx.Rollback()
	st, err := readConfigurationState(tx)
	return st, err
}

func (s *Store) VendorConfiguration(id string) (Configuration, error) {
	st, err := s.configurationState()
	if err != nil {
		return Configuration{}, err
	}
	for _, v := range st.Vendors {
		if v.ID == id {
			return st.view("Vendor", v.UID, v.Revision)
		}
	}
	return Configuration{}, sql.ErrNoRows
}
func (s *Store) ApplicationConfiguration(key string) (Configuration, error) {
	st, err := s.configurationState()
	if err != nil {
		return Configuration{}, err
	}
	for _, a := range st.Applications {
		if a.Key == key {
			return st.view("App", a.UID, a.Revision)
		}
	}
	return Configuration{}, sql.ErrNoRows
}

func vendorSpec(v Vendor) Object {
	return object(presets.VendorSpec{Proxy: networkproxy.Inherit(), Name: presets.Text{En: v.Name.En, ZhCN: v.Name.ZhCN}, Description: presets.Text{En: v.Description.En, ZhCN: v.Description.ZhCN}, Icon: v.Icon, LocalizedIcons: presets.Text{En: v.LocalizedIcons.En, ZhCN: v.LocalizedIcons.ZhCN}})
}

type appConfigurationSpec = presets.AppSpec

func appSpec(a Application, instructions LocalizedText, policy cachepolicy.Config) Object {
	spec := appConfigurationSpec{Category: a.Category, Tags: append([]string{}, a.Tags...), Proxy: networkproxy.Inherit(), Name: presets.Text{En: a.Name.En, ZhCN: a.Name.ZhCN}, Description: presets.Text{En: a.Description.En, ZhCN: a.Description.ZhCN}, Icon: a.Icon, Provider: a.Provider, BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CacheTTLSeconds: a.CacheTTLSeconds, Instructions: presets.Text{En: instructions.En, ZhCN: instructions.ZhCN}}
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

func materialize(st *configurationState, before configurationState) error {
	for key, c := range st.Configs {
		if c.Overrides == nil {
			return ErrInvalidDirectory
		}
		kind, _, _ := strings.Cut(key, ":")
		if c.Ref == nil {
			if c.Spec == nil || !noNull(map[string]any(c.Spec)) || len(c.Overrides) != 0 {
				return ErrInvalidDirectory
			}
		} else if c.Spec != nil {
			return ErrInvalidDirectory
		}
		if err := validateOverrides(kind, c.Overrides); err != nil {
			return err
		}
	}
	for i := range st.Vendors {
		v := &st.Vendors[i]
		spec, err := st.effective("Vendor", v.UID)
		if err != nil {
			return err
		}
		if !noNull(map[string]any(spec)) {
			return ErrInvalidDirectory
		}
		var typed presets.VendorSpec
		if err = strict(spec, &typed); err != nil {
			return err
		}
		in := VendorInput{ID: v.ID, Name: LocalizedText{typed.Name.En, typed.Name.ZhCN}, Description: LocalizedText{typed.Description.En, typed.Description.ZhCN}, Icon: typed.Icon, LocalizedIcons: LocalizedText{typed.LocalizedIcons.En, typed.LocalizedIcons.ZhCN}, Enabled: v.Enabled}
		if err = validateVendor(in); err != nil {
			return err
		}
		v.Name, v.Description, v.Icon, v.LocalizedIcons = in.Name, in.Description, in.Icon, in.LocalizedIcons
	}
	for i := range st.Applications {
		a := &st.Applications[i]
		spec, err := st.effective("App", a.UID)
		if err != nil {
			return err
		}
		if !noNull(map[string]any(spec)) {
			return ErrInvalidDirectory
		}
		var typed appConfigurationSpec
		if err = strict(spec, &typed); err != nil {
			return err
		}
		tags, taxErr := presets.NormalizeTaxonomy(typed.Category, typed.Tags)
		if taxErr != nil {
			return ErrInvalidDirectory
		}
		if a.DeletedAt == nil {
			if err = st.validateTaxonomy(typed.Category, tags); err != nil {
				return err
			}
			a.Category, a.Tags = typed.Category, tags
		} else {
			a.Category, a.Tags = "", []string{}
		}
		if typed.Provider != a.Provider {
			return fmt.Errorf("%w: Provider is immutable", ErrInvalidDirectory)
		}
		in := ApplicationInput{ID: a.ID, Name: LocalizedText{typed.Name.En, typed.Name.ZhCN}, Description: LocalizedText{typed.Description.En, typed.Description.ZhCN}, Icon: typed.Icon, Provider: typed.Provider, BaseURL: typed.BaseURL, BaseURLs: typed.BaseURLs, SourceStrategy: typed.SourceStrategy, CacheTTLSeconds: typed.CacheTTLSeconds, Enabled: a.Enabled}
		if err = validateApplication(&in); err != nil {
			return err
		}
		instructions := LocalizedText{typed.Instructions.En, typed.Instructions.ZhCN}
		if err = validateInstructions(instructions); err != nil {
			return err
		}
		old := before.Instructions[a.UID]
		changed := old.LocalizedText != instructions
		for _, p := range []string{"instructions.en", "instructions.zh-CN"} {
			prev, ok := before.Configs[configKey("App", a.UID)]
			cur := st.Configs[configKey("App", a.UID)]
			_, was := leaf(prev.Overrides, p)
			_, now := leaf(cur.Overrides, p)
			if ok && was != now {
				changed = true
			}
			if prev.Ref != nil && cur.Ref != nil && !was && !now {
				oldDefault, _ := leaf(before.Templates[templateKey("App", *prev.Ref)].Spec, p)
				newDefault, _ := leaf(st.Templates[templateKey("App", *cur.Ref)].Spec, p)
				if !reflect.DeepEqual(oldDefault, newDefault) {
					changed = true
				}
			}
		}
		if old.Revision == 0 {
			old.Revision = 1
		} else if changed {
			old.Revision++
		}
		old.LocalizedText = instructions
		if st.Configs[configKey("App", a.UID)].Ref == nil && st.Instructions[a.UID].Revision == 0 && instructions == (LocalizedText{}) {
			delete(st.Instructions, a.UID)
		} else {
			st.Instructions[a.UID] = old
		}
		if typed.HTTPPolicy != nil && a.Provider != "http-cache" {
			return ErrInvalidDirectory
		}
		if a.Provider == "http-cache" {
			if typed.HTTPPolicy == nil {
				return ErrInvalidDirectory
			}
			policy, err := cachepolicy.Normalize(*typed.HTTPPolicy)
			if err != nil {
				return err
			}
			st.Policies[a.MetricsID()] = policy
		}
		exists := false
		for _, prev := range before.Applications {
			if prev.UID == a.UID {
				exists = true
			}
		}
		if exists && (a.BaseURL != in.BaseURL || !reflect.DeepEqual(a.BaseURLs, in.BaseURLs) || a.SourceStrategy != in.SourceStrategy) {
			a.SourceEpoch++
		}
		a.Name, a.Description, a.Icon, a.BaseURL, a.BaseURLs, a.SourceStrategy, a.CacheTTLSeconds = in.Name, in.Description, in.Icon, in.BaseURL, in.BaseURLs, in.SourceStrategy, in.CacheTTLSeconds
	}
	// One classifier owns all runtime revision changes, regardless of write endpoint.
	for i := range st.Vendors {
		v := &st.Vendors[i]
		v.RuntimeRevision = 1
		for _, prev := range before.Vendors {
			if prev.UID == v.UID {
				v.RuntimeRevision = prev.RuntimeRevision
				if prev.Enabled != v.Enabled || !reflect.DeepEqual(prev.DeletedAt, v.DeletedAt) {
					v.RuntimeRevision++
				}
			}
		}
	}
	for i := range st.Applications {
		a := &st.Applications[i]
		a.RuntimeRevision = 1
		for _, prev := range before.Applications {
			if prev.UID != a.UID {
				continue
			}
			a.RuntimeRevision = prev.RuntimeRevision
			changed := prev.Enabled != a.Enabled || !reflect.DeepEqual(prev.DeletedAt, a.DeletedAt) || prev.BaseURL != a.BaseURL || !reflect.DeepEqual(prev.BaseURLs, a.BaseURLs) || prev.SourceStrategy != a.SourceStrategy || prev.CacheTTLSeconds != a.CacheTTLSeconds || !reflect.DeepEqual(before.Policies[a.MetricsID()], st.Policies[a.MetricsID()])
			key := ""
			if a.Provider == "codex" {
				key = "openai/codex"
			}
			if a.Provider == "claude-code" {
				key = "anthropic/claude-code"
			}
			c := st.Configs[configKey("App", a.UID)]
			if c.Ref != nil {
				key = *c.Ref
			}
			if key != "" && before.Distributions[key].Digest != st.Distributions[key].Digest {
				changed = true
			}
			if changed {
				a.RuntimeRevision++
			}
		}
	}
	// Include both retained historical clients and proposed immutable namespaces.
	for _, a := range st.Applications {
		if a.Provider == "info" || a.Provider == "hosted" {
			continue
		}
		found := false
		for _, src := range st.Sources {
			if src.AppUID == a.UID && src.Epoch == a.SourceEpoch {
				found = true
			}
		}
		if !found {
			st.Sources = append(st.Sources, SourceRecord{AppUID: a.UID, Epoch: a.SourceEpoch, Provider: a.Provider, BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CreatedAt: time.Now().UTC()})
		}
	}
	return nil
}

func cloneState(st configurationState) configurationState {
	var out configurationState
	_ = json.Unmarshal(encode(st), &out)
	out.Taxonomy = map[string]TaxonomyItem{}
	for key, item := range st.Taxonomy {
		if item.Default != nil {
			copy := *item.Default
			item.Default = &copy
		}
		item.Override = cloneObject(item.Override)
		item.decorate()
		out.Taxonomy[key] = item
	}
	return out
}

// The private state has exported fields so deterministic JSON also supplies a
// complete CAS fingerprint: all entity revisions/epochs, configs and template hashes.
func (s *Store) changeConfiguration(change func(*configurationState) error) error {
	return s.changeConfigurationAtomic(change, nil)
}
func (s *Store) changeConfigurationAtomic(change func(*configurationState) error, finalize func(*sql.Tx) error) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	baseline, err := s.configurationState()
	if err != nil {
		return err
	}
	candidate := cloneState(baseline)
	if err = change(&candidate); err != nil {
		return err
	}
	if err = candidate.acceptMissingCompiledContracts(); err != nil {
		return err
	}
	if err = materialize(&candidate, baseline); err != nil {
		return err
	}
	if err = candidate.refreshProxyScopes(); err != nil {
		return err
	}
	if err = candidate.refreshReviewedContracts(); err != nil {
		return err
	}
	if s.validateDistributions != nil {
		if err = s.validateDistributions(candidate.ReviewedDescriptors); err != nil {
			return err
		}
	}
	if finalize == nil && bytes.Equal(encode(baseline), encode(candidate)) {
		return nil
	}
	var publication ConfigurationPublication
	if s.prepareConfiguration != nil {
		publication, err = s.prepareConfiguration(candidate.DirectorySnapshot)
		if err != nil {
			return err
		}
		defer func() {
			if publication != nil {
				publication.Abort()
			}
		}()
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actual, err := readConfigurationState(tx)
	if err != nil {
		return err
	}
	if !bytes.Equal(encode(actual), encode(baseline)) {
		return ErrConflict
	}
	if err = writeConfigurationState(tx, baseline, candidate); err != nil {
		return err
	}
	if finalize != nil {
		if err = finalize(tx); err != nil {
			return err
		}
	}
	if s.configurationFault != nil {
		if err = s.configurationFault("before_commit", tx); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if publication != nil {
		publication.Publish()
		publication = nil
	}
	return nil
}

func writeConfigurationState(tx *sql.Tx, old, next configurationState) error {
	taxonomyChanged := !reflect.DeepEqual(old.Taxonomy, next.Taxonomy)
	for key, item := range next.Taxonomy {
		if !reflect.DeepEqual(old.Taxonomy[key], item) {
			if err := writeTaxonomy(tx, item); err != nil {
				return err
			}
		}
	}

	if old.GlobalProxyRevision != next.GlobalProxyRevision {
		if _, err := tx.Exec(`INSERT INTO settings VALUES('global','','upstream_proxy',?,?) ON CONFLICT(scope,app_id,key) DO UPDATE SET revision=excluded.revision,payload=excluded.payload`, next.GlobalProxyRevision, encode(next.GlobalProxy)); err != nil {
			return err
		}
	}
	if old.Seeded != next.Seeded {
		if _, err := tx.Exec(`UPDATE directory_state SET seeded=? WHERE id=1`, next.Seeded); err != nil {
			return err
		}
	}
	for uid, revision := range next.Pending {
		if _, ok := old.Pending[uid]; !ok {
			if _, err := tx.Exec(`INSERT INTO pending_application_deletes VALUES(?,?)`, uid, revision); err != nil {
				return err
			}
		}
	}
	for key, t := range next.Templates {
		if reflect.DeepEqual(old.Templates[key], t) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO template_snapshots VALUES(?,?,?,?,?,?,?) ON CONFLICT(kind,canonical_key) DO UPDATE SET schema_version=excluded.schema_version,metadata_json=excluded.metadata_json,spec_json=excluded.spec_json,semantic_hash=excluded.semantic_hash,present=excluded.present`, t.Kind, t.Key, t.Schema, encode(t.Metadata), encode(t.Spec), t.Hash, t.Present); err != nil {
			return err
		}
		if t.Kind == "App" {
			if err := projectTemplateTaxonomy(tx, t); err != nil {
				return err
			}
		}
	}
	for key, d := range next.Distributions {
		if reflect.DeepEqual(old.Distributions[key], d) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO trusted_distribution_snapshots VALUES('App',?,?,?,?) ON CONFLICT(canonical_key) DO UPDATE SET provider=excluded.provider,descriptor_json=excluded.descriptor_json,distribution_digest=excluded.distribution_digest`, key, d.Provider, encode(d.Descriptor), d.Digest); err != nil {
			return err
		}
	}
	oldV := map[string]Vendor{}
	for _, v := range old.Vendors {
		oldV[v.UID] = v
	}
	oldA := map[string]Application{}
	for _, a := range old.Applications {
		oldA[a.UID] = a
	}
	for _, v := range next.Vendors {
		prev, exists := oldV[v.UID]
		if exists && reflect.DeepEqual(prev, v) {
			continue
		}
		if !exists {
			_, err := tx.Exec(`INSERT INTO vendors(`+vendorColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.UID, v.ID, v.Name.En, v.Name.ZhCN, v.Description.En, v.Description.ZhCN, v.Icon, v.Enabled, v.Revision, unixPointer(v.DeletedAt), v.LocalizedIcons.En, v.LocalizedIcons.ZhCN, v.RuntimeRevision)
			if err != nil {
				return err
			}
		} else {
			_, err := tx.Exec(`UPDATE vendors SET name_en=?,name_zh_cn=?,description_en=?,description_zh_cn=?,icon=?,enabled=?,revision=?,deleted_at_s=?,icon_en=?,icon_zh_cn=?,runtime_revision=? WHERE uid=?`, v.Name.En, v.Name.ZhCN, v.Description.En, v.Description.ZhCN, v.Icon, v.Enabled, v.Revision, unixPointer(v.DeletedAt), v.LocalizedIcons.En, v.LocalizedIcons.ZhCN, v.RuntimeRevision, v.UID)
			if err != nil {
				return err
			}
		}
	}
	for _, a := range next.Applications {
		prev, exists := oldA[a.UID]
		if exists && reflect.DeepEqual(prev, a) {
			continue
		}
		if !exists {
			_, err := tx.Exec(`INSERT INTO applications(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,cache_ttl_seconds,base_urls_json,source_strategy,enabled,revision,source_epoch,deleted_at_s,runtime_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.UID, a.VendorUID, a.ID, a.Name.En, a.Name.ZhCN, a.Description.En, a.Description.ZhCN, a.Icon, a.Provider, a.BaseURL, a.CacheTTLSeconds, string(encode(a.BaseURLs)), a.SourceStrategy, a.Enabled, a.Revision, a.SourceEpoch, unixPointer(a.DeletedAt), a.RuntimeRevision)
			if err != nil {
				return err
			}
		} else {
			_, err := tx.Exec(`UPDATE applications SET name_en=?,name_zh_cn=?,description_en=?,description_zh_cn=?,icon=?,base_url=?,base_urls_json=?,source_strategy=?,cache_ttl_seconds=?,enabled=?,revision=?,source_epoch=?,deleted_at_s=?,runtime_revision=? WHERE uid=?`, a.Name.En, a.Name.ZhCN, a.Description.En, a.Description.ZhCN, a.Icon, a.BaseURL, string(encode(a.BaseURLs)), a.SourceStrategy, a.CacheTTLSeconds, a.Enabled, a.Revision, a.SourceEpoch, unixPointer(a.DeletedAt), a.RuntimeRevision, a.UID)
			if err != nil {
				return err
			}
		}
	}
	for _, a := range next.Applications {
		prev, exists := oldA[a.UID]
		if !exists || !reflect.DeepEqual(prev, a) {
			if err := projectAppTaxonomy(tx, a); err != nil {
				return err
			}
		}
		if !exists || prev.Category != a.Category || !reflect.DeepEqual(prev.Tags, a.Tags) {
			taxonomyChanged = true
		}
		before, after := old.Configs[configKey("App", a.UID)], next.Configs[configKey("App", a.UID)]
		for _, path := range []string{"category", "tags"} {
			x, xok := leaf(before.Overrides, path)
			y, yok := leaf(after.Overrides, path)
			if xok != yok || !reflect.DeepEqual(x, y) {
				taxonomyChanged = true
			}
		}
	}
	if taxonomyChanged {
		if _, err := tx.Exec(`UPDATE taxonomy_state SET public_revision=public_revision+1 WHERE id=1`); err != nil {
			return err
		}
	}
	if err := writeAllNotes(tx, old.Notes, next.Notes); err != nil {
		return err
	}
	for key, c := range next.Configs {
		if reflect.DeepEqual(old.Configs[key], c) {
			continue
		}
		kind, uid, _ := strings.Cut(key, ":")
		table := "vendor_config"
		if kind == "App" {
			table = "application_config"
		}
		var spec any
		if c.Spec != nil {
			spec = encode(c.Spec)
		}
		if _, err := tx.Exec(`INSERT INTO `+table+` VALUES(?,?,?,?) ON CONFLICT(entity_uid) DO UPDATE SET template_ref=excluded.template_ref,overrides_json=excluded.overrides_json,spec_json=excluded.spec_json`, uid, c.Ref, encode(c.Overrides), spec); err != nil {
			return err
		}
	}
	for uid, i := range next.Instructions {
		if reflect.DeepEqual(old.Instructions[uid], i) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO application_instructions VALUES(?,?,?,?) ON CONFLICT(app_uid) DO UPDATE SET revision=excluded.revision,en=excluded.en,zh_cn=excluded.zh_cn`, uid, i.Revision, i.En, i.ZhCN); err != nil {
			return err
		}
	}
	for _, src := range next.Sources {
		exists := false
		for _, prev := range old.Sources {
			if prev.StorageID() == src.StorageID() {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO application_sources VALUES(?,?,?,?,?,?,?)`, src.AppUID, src.Epoch, src.Provider, src.BaseURL, src.CreatedAt.Unix(), string(encode(src.BaseURLs)), src.SourceStrategy); err != nil {
			return err
		}
	}
	for key, policy := range next.Policies {
		if reflect.DeepEqual(old.Policies[key], policy) {
			continue
		}
		var revision int64
		for _, a := range next.Applications {
			if a.MetricsID() == key {
				revision = a.Revision
			}
		}
		if _, err := tx.Exec(`INSERT INTO settings VALUES('app',?,'http_policy',?,?) ON CONFLICT(scope,app_id,key) DO UPDATE SET revision=excluded.revision,payload=excluded.payload`, key, revision, encode(policy)); err != nil {
			return err
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
		if p == "category" {
			var value string
			if json.Unmarshal(patch.Set[p], &value) != nil || bytes.Equal(bytes.TrimSpace(patch.Set[p]), []byte("null")) {
				return ErrInvalidDirectory
			}
		}
		if p == "tags" {
			var tags []string
			if json.Unmarshal(patch.Set[p], &tags) != nil || bytes.Equal(bytes.TrimSpace(patch.Set[p]), []byte("null")) {
				return ErrInvalidDirectory
			}
			normalized, err := presets.NormalizeTaxonomy("", tags)
			if err != nil {
				return ErrInvalidDirectory
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
			return fmt.Errorf("%w: field %s cannot be overridden", ErrInvalidDirectory, p)
		}
		used[p] = true
	}
	for _, p := range patch.Unset {
		if !allowed[p] || used[p] {
			return fmt.Errorf("%w: invalid or conflicting unset %s", ErrInvalidDirectory, p)
		}
		used[p] = true
		if c.Ref == nil {
			return fmt.Errorf("%w: independent configuration has no inheritance source", ErrInvalidDirectory)
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
func (s *Store) patchConfiguration(kind, key string, patch ConfigurationPatch, enabled *bool, instructionRevision *int64) error {
	return s.changeConfiguration(func(st *configurationState) error {
		var uid string
		var revision int64
		for _, v := range st.Vendors {
			if kind == "Vendor" && v.ID == key {
				uid, revision = v.UID, v.Revision
				if v.DeletedAt != nil {
					return ErrDirectoryDeleted
				}
			}
		}
		for _, a := range st.Applications {
			if kind == "App" && a.Key == key {
				uid, revision = a.UID, a.Revision
				if a.DeletedAt != nil {
					return ErrDirectoryDeleted
				}
			}
		}
		if uid == "" {
			return sql.ErrNoRows
		}
		if revision != patch.Revision {
			return ErrConflict
		}
		if instructionRevision != nil && st.Instructions[uid].Revision != *instructionRevision {
			return ErrConflict
		}
		if len(patch.Set) == 0 && len(patch.Unset) == 0 && enabled == nil {
			return nil
		}
		if kind == "App" {
			for _, a := range st.Applications {
				if a.UID == uid && a.Provider == "http-cache" {
					if raw, ok := patch.Set["base_url"]; ok {
						var base string
						if json.Unmarshal(raw, &base) != nil {
							return ErrInvalidDirectory
						}
						normalized, e := normalizeDirectoryBase(base)
						if e != nil {
							return e
						}
						if _, hasList := patch.Set["base_urls"]; !hasList && normalized != a.BaseURL {
							return fmt.Errorf("%w: HTTP cache sources use base_urls; base_url must match the first source", ErrInvalidDirectory)
						}
					}
				}
			}
		}
		c := st.Configs[configKey(kind, uid)]
		if err := applyPatch(&c, kind, patch); err != nil {
			return err
		}
		st.Configs[configKey(kind, uid)] = c
		if kind == "Vendor" {
			for i := range st.Vendors {
				if st.Vendors[i].UID == uid {
					st.Vendors[i].Revision++
					if enabled != nil {
						st.Vendors[i].Enabled = *enabled
					}
				}
			}
		} else {
			for i := range st.Applications {
				if st.Applications[i].UID == uid {
					st.Applications[i].Revision++
					if enabled != nil {
						st.Applications[i].Enabled = *enabled
					}
				}
			}
		}
		return nil
	})
}
func (s *Store) PatchVendorConfiguration(id string, patch ConfigurationPatch) (Configuration, error) {
	if err := s.patchConfiguration("Vendor", id, patch, nil, nil); err != nil {
		return Configuration{}, err
	}
	return s.VendorConfiguration(id)
}
func (s *Store) PatchApplicationConfiguration(key string, patch ConfigurationPatch) (Configuration, error) {
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
		normalized.Tags, _ = presets.NormalizeTaxonomy(normalized.Category, normalized.Tags)
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

// ReconcileTemplates validates all proposed effective entities before any write.
// Missing templates retain the last accepted snapshot and freeze effective data.
func (s *Store) ReconcileTemplates(set presets.Set) error {
	if err := set.ValidateTaxonomyReferences(); err != nil {
		return err
	}
	incoming, err := snapshots(set)
	if err != nil {
		return err
	}
	return s.changeConfiguration(func(st *configurationState) error {
		if err := st.reconcileTaxonomy(set.Taxonomy); err != nil {
			return err
		}
		previousDistributions := map[string]trustedDistribution{}
		for key, d := range st.Distributions {
			previousDistributions[key] = d
		}
		providers := map[string]string{}
		for _, a := range set.Apps {
			providers[a.Key()] = a.Spec.Provider
		}
		for _, d := range set.Descriptors() {
			st.Distributions[d.ID] = trustedDistribution{Provider: providers[d.ID], Descriptor: d, Digest: distributionDigest(d)}
		}
		st.Seeded = true
		previous := map[string]templateSnapshot{}
		for key, t := range st.Templates {
			previous[key] = t
			t.Present = false
			st.Templates[key] = t
		}
		for _, t := range incoming {
			key := templateKey(t.Kind, t.Key)
			if prev, ok := previous[key]; ok {
				if !reflect.DeepEqual(prev.Metadata, t.Metadata) {
					return fmt.Errorf("%w: template identity changed", ErrInvalidDirectory)
				}
				if t.Kind == "App" && prev.Spec["provider"] != t.Spec["provider"] {
					return fmt.Errorf("%w: template Provider changed", ErrInvalidDirectory)
				}
			}
			st.Templates[key] = t
		}
		for i := range st.Vendors {
			v := &st.Vendors[i]
			c := st.Configs[configKey("Vendor", v.UID)]
			if c.Ref != nil {
				key := templateKey("Vendor", *c.Ref)
				if previous[key].Hash != st.Templates[key].Hash {
					v.Revision++
				}
			}
		}
		for i := range st.Applications {
			a := &st.Applications[i]
			c := st.Configs[configKey("App", a.UID)]
			distributionKey := ""
			if a.Provider == "codex" {
				distributionKey = "openai/codex"
			}
			if a.Provider == "claude-code" {
				distributionKey = "anthropic/claude-code"
			}
			changed := false
			if c.Ref != nil {
				key := templateKey("App", *c.Ref)
				changed = previous[key].Hash != st.Templates[key].Hash
				distributionKey = *c.Ref
			}
			if distributionKey != "" && previousDistributions[distributionKey].Digest != st.Distributions[distributionKey].Digest {
				changed = true
			}
			if changed {
				a.Revision++
			}
		}
		for _, t := range incoming {
			if t.Kind != "Vendor" {
				continue
			}
			exists := false
			for _, v := range st.Vendors {
				if v.ID == t.Key {
					exists = true
				}
			}
			if exists {
				continue
			}
			uid, err := identity.NewUID()
			if err != nil {
				return err
			}
			ref := t.Key
			st.Vendors = append(st.Vendors, Vendor{UID: uid, ID: t.Key, Revision: 1})
			st.Configs[configKey("Vendor", uid)] = ownedConfig{Ref: &ref, Overrides: Object{}}
		}
		for _, t := range incoming {
			if t.Kind != "App" {
				continue
			}
			exists := false
			for _, a := range st.Applications {
				if a.Key == t.Key {
					exists = true
				}
			}
			if exists {
				continue
			}
			vendor, id, _ := strings.Cut(t.Key, "/")
			var parent Vendor
			for _, v := range st.Vendors {
				if v.ID == vendor {
					parent = v
				}
			}
			if parent.UID == "" || parent.DeletedAt != nil {
				continue
			}
			uid, err := identity.NewUID()
			if err != nil {
				return err
			}
			ref := t.Key
			spec := t.Spec
			st.Applications = append(st.Applications, Application{UID: uid, ID: id, Key: t.Key, VendorID: vendor, VendorUID: parent.UID, Provider: spec["provider"].(string), Revision: 1, SourceEpoch: 1})
			st.Configs[configKey("App", uid)] = ownedConfig{Ref: &ref, Overrides: Object{}}
		}
		return nil
	})
}

func (s *Store) CreateConfiguredVendor(in VendorInput, ref *string) (Vendor, error) {
	var result Vendor
	err := s.changeConfiguration(func(st *configurationState) error {
		for _, v := range st.Vendors {
			if v.ID == in.ID {
				return ErrDirectoryExists
			}
		}
		if err := validateVendor(in); err != nil {
			return err
		}
		uid, err := identity.NewUID()
		if err != nil {
			return err
		}
		result = Vendor{UID: uid, ID: in.ID, Name: in.Name, Description: in.Description, Icon: in.Icon, LocalizedIcons: in.LocalizedIcons, Enabled: in.Enabled, Revision: 1}
		c := ownedConfig{Overrides: Object{}, Spec: vendorSpec(result)}
		if ref != nil {
			t, ok := st.Templates[templateKey("Vendor", *ref)]
			if !ok || !t.Present {
				return fmt.Errorf("%w: unknown template", ErrInvalidDirectory)
			}
			c.Ref = ref
			c.Spec = nil
		}
		st.Vendors = append(st.Vendors, result)
		st.Configs[configKey("Vendor", uid)] = c
		return nil
	})
	if err != nil {
		return Vendor{}, err
	}
	return s.Vendor(in.ID)
}
func (s *Store) CreateConfiguredApplication(vendor string, in ApplicationInput, ref *string) (Application, error) {
	tags, taxErr := presets.NormalizeTaxonomy(in.Category, in.Tags)
	if taxErr != nil {
		return Application{}, ErrInvalidDirectory
	}
	in.Tags = tags
	err := s.changeConfiguration(func(st *configurationState) error {
		for _, a := range st.Applications {
			if a.Key == vendor+"/"+in.ID {
				return ErrDirectoryExists
			}
		}
		if err := validateApplication(&in); err != nil {
			return err
		}
		var parent Vendor
		for _, v := range st.Vendors {
			if v.ID == vendor {
				parent = v
			}
		}
		if parent.UID == "" {
			return sql.ErrNoRows
		}
		if parent.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		uid, err := identity.NewUID()
		if err != nil {
			return err
		}
		a := Application{Category: in.Category, Tags: in.Tags, UID: uid, ID: in.ID, Key: vendor + "/" + in.ID, VendorID: vendor, VendorUID: parent.UID, Name: in.Name, Description: in.Description, Icon: in.Icon, Provider: in.Provider, BaseURL: in.BaseURL, BaseURLs: in.BaseURLs, SourceStrategy: in.SourceStrategy, CacheTTLSeconds: in.CacheTTLSeconds, Enabled: in.Enabled, Revision: 1, SourceEpoch: 1}
		c := ownedConfig{Overrides: Object{}, Spec: appSpec(a, LocalizedText{}, cachepolicy.Empty())}
		if ref != nil {
			t, ok := st.Templates[templateKey("App", *ref)]
			if !ok || !t.Present || t.Spec["provider"] != in.Provider {
				return fmt.Errorf("%w: unknown or incompatible template", ErrInvalidDirectory)
			}
			c.Ref = ref
			c.Spec = nil
		}
		st.Applications = append(st.Applications, a)
		st.Configs[configKey("App", uid)] = c
		return nil
	})
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

// Template views follow the stored binding, independently of public identity.
func (s *Store) BoundApplicationTemplate(key string) (EntityTemplate, bool, error) {
	st, err := s.configurationState()
	if err != nil {
		return EntityTemplate{}, false, err
	}
	var owner Application
	for _, a := range st.Applications {
		if a.Key == key {
			owner = a
		}
	}
	if owner.UID == "" {
		return EntityTemplate{}, false, sql.ErrNoRows
	}
	c := st.Configs[configKey("App", owner.UID)]
	if c.Ref == nil {
		return EntityTemplate{}, false, nil
	}
	t := st.Templates[templateKey("App", *c.Ref)]
	var spec presets.AppSpec
	if err = strict(t.Spec, &spec); err != nil {
		return EntityTemplate{}, false, err
	}
	vendor, id, _ := strings.Cut(*c.Ref, "/")
	var vs presets.VendorSpec
	if parent, ok := st.Templates[templateKey("Vendor", vendor)]; ok {
		if err = strict(parent.Spec, &vs); err != nil {
			return EntityTemplate{}, false, err
		}
	}
	return EntityTemplate{Vendor: VendorInput{ID: vendor, Name: LocalizedText{vs.Name.En, vs.Name.ZhCN}, Description: LocalizedText{vs.Description.En, vs.Description.ZhCN}, Icon: vs.Icon, LocalizedIcons: LocalizedText{vs.LocalizedIcons.En, vs.LocalizedIcons.ZhCN}}, Application: ApplicationInput{ID: id, Name: LocalizedText{spec.Name.En, spec.Name.ZhCN}, Description: LocalizedText{spec.Description.En, spec.Description.ZhCN}, Icon: spec.Icon, Provider: spec.Provider, BaseURL: spec.BaseURL, BaseURLs: spec.BaseURLs, SourceStrategy: spec.SourceStrategy, CacheTTLSeconds: spec.CacheTTLSeconds}, Instructions: LocalizedText{spec.Instructions.En, spec.Instructions.ZhCN}}, true, nil
}
func (s *Store) BoundVendorTemplate(id string) (VendorInput, bool, error) {
	c, err := s.VendorConfiguration(id)
	if err != nil {
		return VendorInput{}, false, err
	}
	if c.TemplateRef == nil {
		return VendorInput{}, false, nil
	}
	var spec presets.VendorSpec
	if err = strict(c.Defaults, &spec); err != nil {
		return VendorInput{}, false, err
	}
	return VendorInput{ID: *c.TemplateRef, Name: LocalizedText{spec.Name.En, spec.Name.ZhCN}, Description: LocalizedText{spec.Description.En, spec.Description.ZhCN}, Icon: spec.Icon, LocalizedIcons: LocalizedText{spec.LocalizedIcons.En, spec.LocalizedIcons.ZhCN}}, true, nil
}
func (s *Store) canonicalApplicationProtected(key string) (bool, error) {
	if _, ok := BuiltinApplicationTemplate(key); ok {
		return true, nil
	}
	var count int
	err := s.DB.QueryRow(`SELECT count(*) FROM template_snapshots WHERE kind='App' AND canonical_key=?`, key).Scan(&count)
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
func (st *configurationState) refreshReviewedContracts() error {
	st.ReviewedDescriptors = nil
	st.TemplateBindings = map[string]string{}
	st.ProviderDefaults = map[string]string{}
	keys := make([]string, 0, len(st.Distributions))
	for key := range st.Distributions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		d := st.Distributions[key]
		template, ok := st.Templates[templateKey("App", key)]
		if !ok || template.Spec["provider"] != d.Provider {
			return fmt.Errorf("invalid trusted Provider binding for %s", key)
		}
		st.ReviewedDescriptors = append(st.ReviewedDescriptors, d.Descriptor)
		if (d.Provider == "codex" && key == "openai/codex") || (d.Provider == "claude-code" && key == "anthropic/claude-code") {
			st.ProviderDefaults[d.Provider] = d.Descriptor.Upstream
		}
	}
	for _, a := range st.Applications {
		c := st.Configs[configKey("App", a.UID)]
		if c.Ref != nil {
			st.TemplateBindings[a.UID] = *c.Ref
		}
		if a.Provider == "codex" || a.Provider == "claude-code" {
			key := st.TemplateBindings[a.UID]
			if key == "" {
				if a.Provider == "codex" {
					key = "openai/codex"
				} else {
					key = "anthropic/claude-code"
				}
			}
			d, ok := st.Distributions[key]
			if ok && d.Provider != a.Provider {
				return fmt.Errorf("missing trusted distribution for %s", a.Key)
			}
		}
	}
	return nil
}
func (s *Store) SetDistributionValidation(validate func([]presets.Descriptor) error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.validateDistributions = validate
}
func (s *Store) DirectoryConfigurationSnapshot() (DirectorySnapshot, error) {
	st, err := s.configurationState()
	return st.DirectorySnapshot, err
}

// Independent release instances use the canonical compiled contract. This accepts
// only missing trusted build records, and never changes an existing snapshot.
func (st *configurationState) acceptMissingCompiledContracts() error {
	set := presets.Embedded()
	incoming, err := snapshots(set)
	if err != nil {
		return err
	}
	byKey := map[string]templateSnapshot{}
	for _, t := range incoming {
		byKey[templateKey(t.Kind, t.Key)] = t
	}
	for _, a := range st.Applications {
		if a.Provider != "codex" && a.Provider != "claude-code" {
			continue
		}
		key := "openai/codex"
		if a.Provider == "claude-code" {
			key = "anthropic/claude-code"
		}
		c := st.Configs[configKey("App", a.UID)]
		if c.Ref != nil {
			key = *c.Ref
		}
		if _, ok := st.Distributions[key]; ok {
			continue
		}
		found := false
		for _, d := range set.Descriptors() {
			if d.ID != key {
				continue
			}
			t := byKey[templateKey("App", key)]
			if t.Spec["provider"] != a.Provider {
				return ErrInvalidDirectory
			}
			if _, ok := st.Templates[templateKey("App", key)]; !ok {
				st.Templates[templateKey("App", key)] = t
			}
			st.Distributions[key] = trustedDistribution{Provider: a.Provider, Descriptor: d, Digest: distributionDigest(d)}
			found = true
		}
		if !found {
			return fmt.Errorf("missing trusted distribution for %s", a.Key)
		}
	}
	return nil
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
	var legacy struct {
		Server string `json:"server"`
		Mode   string `json:"mode"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return networkproxy.Config{}, ErrInvalidDirectory
	}
	c := networkproxy.Config{Mode: legacy.Mode, URL: legacy.URL}
	if c.Mode == "" {
		c = networkproxy.Direct()
		if legacy.Server != "" {
			c = networkproxy.Config{Mode: "url", URL: legacy.Server}
		}
	}
	if err := c.Validate(false); err != nil {
		return c, ErrInvalidDirectory
	}
	return c, nil
}
func (st *configurationState) refreshProxyScopes() error {
	st.ProxyScopes = map[string]networkproxy.Scope{}
	if err := st.GlobalProxy.Validate(false); err != nil {
		return ErrInvalidDirectory
	}
	vendors := map[string]Vendor{}
	for _, v := range st.Vendors {
		vendors[v.UID] = v
		spec, err := st.effective("Vendor", v.UID)
		if err != nil {
			return err
		}
		if _, err = decodeProxy(spec["proxy"]); err != nil {
			return ErrInvalidDirectory
		}
	}
	for _, a := range st.Applications {
		v, ok := vendors[a.VendorUID]
		if !ok {
			return ErrInvalidDirectory
		}
		as, err := st.effective("App", a.UID)
		if err != nil {
			return err
		}
		vs, err := st.effective("Vendor", v.UID)
		if err != nil {
			return err
		}
		ap, err := decodeProxy(as["proxy"])
		if err != nil {
			return ErrInvalidDirectory
		}
		vp, err := decodeProxy(vs["proxy"])
		if err != nil {
			return ErrInvalidDirectory
		}
		st.ProxyScopes[a.UID] = networkproxy.Scope{VendorUID: v.UID, Proxy: networkproxy.Resolve(ap, a.Key, vp, v.ID, st.GlobalProxy), Allowed: a.DeletedAt == nil && v.DeletedAt == nil}
	}
	return nil
}
func (s *Store) PatchGlobalProxy(expected int64, c networkproxy.Config) (int64, error) {
	if err := c.Validate(false); err != nil {
		return 0, ErrInvalidDirectory
	}
	err := s.changeConfiguration(func(st *configurationState) error {
		if st.GlobalProxyRevision != expected {
			return ErrConflict
		}
		st.GlobalProxy = c
		st.GlobalProxyRevision++
		return nil
	})
	return expected + 1, err
}

// SetInitialConfigurationPrepare installs a startup coordinator without replacing
// the running server's composite publication callback.
func (s *Store) SetInitialConfigurationPrepare(prepare func(DirectorySnapshot) (ConfigurationPublication, error)) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.prepareConfiguration == nil {
		s.prepareConfiguration = prepare
	}
}
