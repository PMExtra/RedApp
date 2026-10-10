package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
)

type querier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

// configSet is the working set of one configuration operation: the rows it has
// read and the changes it made to them, in memory. Loaders read single rows (or
// the applications of one vendor) on demand, so an operation costs what it
// touches rather than the size of the configuration. Only template
// reconciliation loads everything, because a template can affect every entity
// bound to it.
//
// Every loaded row keeps the values it was read with. write stores only the
// rows that changed, each conditioned on the revision it was read with.
type configSet struct {
	q querier

	vendors   map[string]*vendorEntry // by UID
	vendorIDs map[string]string       // ID → UID; "" records a known miss
	apps      map[string]*appEntry    // by UID
	appKeys   map[string]string       // key → UID; "" records a known miss

	templates       map[string]*templateSnapshot // templateKey → nil when absent
	templatesBefore map[string]*templateSnapshot
	allTemplates    bool

	distributions       map[string]*trustedDistribution // canonical key → nil when absent
	distributionsBefore map[string]*trustedDistribution
	allDistributions    bool

	categories       map[string]*TaxonomyItem // ID → nil when absent
	categoriesBefore map[string]*TaxonomyItem
	allCategories    bool

	notes  map[string]*noteEntry // configKey(kind, uid)
	global *globalEntry

	// removedApps and removedVendors are purged by the operation's finalize
	// step; the runtime view drops them once the purge has committed.
	removedApps, removedVendors []string
}

type vendorEntry struct {
	Vendor
	before               *Vendor // nil for a vendor created by this operation
	config, configBefore ownedConfig
	// proxy is the vendor's own proxy setting, derived by materialize.
	proxy   networkproxy.Config
	changed bool
}

type appEntry struct {
	Application
	before                           *Application // nil for an application created by this operation
	config, configBefore             ownedConfig
	instructions, instructionsBefore Instructions // Revision 0: no row
	policy, policyBefore             *cachepolicy.Config
	pendingBefore, pending           bool
	pendingRevision                  int64
	// proxy and newSource are derived by materialize.
	proxy     networkproxy.Config
	newSource *SourceRecord
	changed   bool
}

type noteEntry struct {
	AdminNotes
	stored *AdminNotes // nil when no row exists
}
type globalEntry struct {
	config   networkproxy.Config
	revision int64
	stored   int64 // revision of the stored row; 0 when none exists
}

func newConfigSet(q querier) *configSet {
	return &configSet{
		q:                   q,
		vendors:             map[string]*vendorEntry{},
		vendorIDs:           map[string]string{},
		apps:                map[string]*appEntry{},
		appKeys:             map[string]string{},
		templates:           map[string]*templateSnapshot{},
		templatesBefore:     map[string]*templateSnapshot{},
		distributions:       map[string]*trustedDistribution{},
		distributionsBefore: map[string]*trustedDistribution{},
		categories:          map[string]*TaxonomyItem{},
		categoriesBefore:    map[string]*TaxonomyItem{},
		notes:               map[string]*noteEntry{},
	}
}

// extraScanner scans a row whose leading columns another scanner understands.
type extraScanner struct {
	row   directoryScanner
	extra []any
}

func (s extraScanner) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra...)...) }

func decodeOwnedConfig(uid string, ref sql.NullString, overrides, spec []byte) (ownedConfig, error) {
	var c ownedConfig
	if overrides == nil {
		return c, fmt.Errorf("missing authoritative configuration for %s", uid)
	}
	if ref.Valid {
		value := ref.String
		c.Ref = &value
	}
	if err := json.Unmarshal(overrides, &c.Overrides); err != nil {
		return c, err
	}
	if spec != nil {
		if err := json.Unmarshal(spec, &c.Spec); err != nil {
			return c, err
		}
	}
	return c, nil
}

const vendorEntryQuery = `SELECT ` + vendorColumns + `,c.template_ref,c.overrides_json,c.spec_json FROM vendors LEFT JOIN vendor_config c ON c.entity_uid=vendors.uid`

func (w *configSet) loadVendors(where string, args ...any) ([]*vendorEntry, error) {
	rows, err := w.q.Query(vendorEntryQuery+` WHERE `+where+` ORDER BY vendors.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*vendorEntry
	for rows.Next() {
		var ref sql.NullString
		var overrides, spec []byte
		v, err := scanVendor(extraScanner{rows, []any{&ref, &overrides, &spec}})
		if err != nil {
			return nil, err
		}
		if e := w.vendors[v.UID]; e != nil {
			out = append(out, e)
			continue
		}
		e := &vendorEntry{Vendor: v}
		before := v
		e.before = &before
		if e.config, err = decodeOwnedConfig(v.UID, ref, overrides, spec); err != nil {
			return nil, err
		}
		e.configBefore, _ = decodeOwnedConfig(v.UID, ref, overrides, spec)
		w.vendors[v.UID], w.vendorIDs[v.ID] = e, v.UID
		out = append(out, e)
	}
	return out, rows.Err()
}

const appEntryQuery = `SELECT ` + applicationColumns + `,c.template_ref,c.overrides_json,c.spec_json,i.revision,i.en,i.zh_cn,p.payload,d.requested_revision FROM applications a JOIN vendors v ON v.uid=a.vendor_uid LEFT JOIN application_config c ON c.entity_uid=a.uid LEFT JOIN application_instructions i ON i.app_uid=a.uid LEFT JOIN application_http_policies p ON p.app_uid=a.uid LEFT JOIN pending_application_deletes d ON d.app_uid=a.uid`

func (w *configSet) loadApps(where string, args ...any) ([]*appEntry, error) {
	rows, err := w.q.Query(appEntryQuery+` WHERE `+where+` ORDER BY v.id,a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*appEntry
	for rows.Next() {
		var ref, en, zh sql.NullString
		var overrides, spec, policy []byte
		var instructions, pending sql.NullInt64
		a, err := scanApplication(extraScanner{rows, []any{&ref, &overrides, &spec, &instructions, &en, &zh, &policy, &pending}})
		if err != nil {
			return nil, err
		}
		if e := w.apps[a.UID]; e != nil {
			out = append(out, e)
			continue
		}
		e := &appEntry{Application: a}
		before := a
		e.before = &before
		if e.config, err = decodeOwnedConfig(a.UID, ref, overrides, spec); err != nil {
			return nil, err
		}
		e.configBefore, _ = decodeOwnedConfig(a.UID, ref, overrides, spec)
		if instructions.Valid {
			e.instructions = Instructions{LocalizedText: LocalizedText{En: en.String, ZhCN: zh.String}, Revision: instructions.Int64}
		}
		e.instructionsBefore = e.instructions
		if policy != nil {
			var c, stored cachepolicy.Config
			if err = json.Unmarshal(policy, &c); err != nil {
				return nil, err
			}
			_ = json.Unmarshal(policy, &stored)
			e.policy, e.policyBefore = &c, &stored
		}
		e.pendingBefore, e.pending, e.pendingRevision = pending.Valid, pending.Valid, pending.Int64
		w.apps[a.UID], w.appKeys[a.Key] = e, a.UID
		out = append(out, e)
	}
	return out, rows.Err()
}

func (w *configSet) vendorByID(id string) (*vendorEntry, error) {
	if uid, ok := w.vendorIDs[id]; ok {
		return w.vendors[uid], nil
	}
	found, err := w.loadVendors(`vendors.id=?`, id)
	if err != nil || len(found) == 0 {
		w.vendorIDs[id] = ""
		return nil, err
	}
	return found[0], nil
}
func (w *configSet) vendor(uid string) (*vendorEntry, error) {
	if e := w.vendors[uid]; e != nil {
		return e, nil
	}
	found, err := w.loadVendors(`vendors.uid=?`, uid)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// appByKey returns nil for a key that names no application, including an
// invalid one.
func (w *configSet) appByKey(key string) (*appEntry, error) {
	if uid, ok := w.appKeys[key]; ok {
		return w.apps[uid], nil
	}
	vendor, id, ok := strings.Cut(key, "/")
	if !ok || !identity.ValidKey(key) {
		return nil, nil
	}
	found, err := w.loadApps(`v.id=? AND a.id=?`, vendor, id)
	if err != nil || len(found) == 0 {
		w.appKeys[key] = ""
		return nil, err
	}
	return found[0], nil
}
func (w *configSet) app(uid string) (*appEntry, error) {
	if e := w.apps[uid]; e != nil {
		return e, nil
	}
	found, err := w.loadApps(`a.uid=?`, uid)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// vendorApps returns the vendor's applications, including ones this operation created.
func (w *configSet) vendorApps(vendorUID string) ([]*appEntry, error) {
	if _, err := w.loadApps(`a.vendor_uid=?`, vendorUID); err != nil {
		return nil, err
	}
	var out []*appEntry
	for _, a := range w.apps {
		if a.VendorUID == vendorUID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// loadEverything loads the whole configuration; only template reconciliation
// and the runtime view need it.
func (w *configSet) loadEverything() error {
	if _, err := w.loadVendors(`1`); err != nil {
		return err
	}
	if _, err := w.loadApps(`1`); err != nil {
		return err
	}
	if err := w.loadAllTemplates(); err != nil {
		return err
	}
	if err := w.loadAllDistributions(); err != nil {
		return err
	}
	return w.loadAllCategories()
}
func (w *configSet) sortedVendors() []*vendorEntry {
	out := make([]*vendorEntry, 0, len(w.vendors))
	for _, v := range w.vendors {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (w *configSet) sortedApps() []*appEntry {
	out := make([]*appEntry, 0, len(w.apps))
	for _, a := range w.apps {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VendorID != out[j].VendorID {
			return out[i].VendorID < out[j].VendorID
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func (w *configSet) addVendor(v Vendor, c ownedConfig) *vendorEntry {
	e := &vendorEntry{Vendor: v, config: c, changed: true}
	w.vendors[v.UID], w.vendorIDs[v.ID] = e, v.UID
	return e
}
func (w *configSet) addApp(a Application, c ownedConfig) *appEntry {
	e := &appEntry{Application: a, config: c, changed: true}
	w.apps[a.UID], w.appKeys[a.Key] = e, a.UID
	return e
}

// entity looks up a vendor by ID or an application by key.
func (w *configSet) entity(kind, key string) (uid string, revision int64, deleted bool, err error) {
	if kind == "Vendor" {
		v, err := w.vendorByID(key)
		if err != nil || v == nil {
			return "", 0, false, err
		}
		return v.UID, v.Revision, v.DeletedAt != nil, nil
	}
	a, err := w.appByKey(key)
	if err != nil || a == nil {
		return "", 0, false, err
	}
	return a.UID, a.Revision, a.DeletedAt != nil, nil
}

// config returns the configuration of an entity loaded in this set.
func (w *configSet) config(kind, uid string) ownedConfig {
	if kind == "Vendor" {
		if v := w.vendors[uid]; v != nil {
			return v.config
		}
	} else if a := w.apps[uid]; a != nil {
		return a.config
	}
	return ownedConfig{}
}
func (w *configSet) setConfig(kind, uid string, c ownedConfig) {
	if kind == "Vendor" {
		v := w.vendors[uid]
		v.config, v.changed = c, true
	} else {
		a := w.apps[uid]
		a.config, a.changed = c, true
	}
}

func (w *configSet) loadTemplates(where string, args ...any) error {
	rows, err := w.q.Query(`SELECT kind,canonical_key,schema_version,metadata_json,spec_json,semantic_hash,present FROM template_snapshots WHERE `+where, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t templateSnapshot
		var metadata, spec []byte
		if err = rows.Scan(&t.Kind, &t.Key, &t.Schema, &metadata, &spec, &t.Hash, &t.Present); err != nil {
			return err
		}
		if err = json.Unmarshal(metadata, &t.Metadata); err != nil {
			return err
		}
		if err = json.Unmarshal(spec, &t.Spec); err != nil {
			return err
		}
		key := templateKey(t.Kind, t.Key)
		if _, loaded := w.templatesBefore[key]; loaded {
			continue
		}
		before := t
		w.templates[key], w.templatesBefore[key] = &t, &before
	}
	return rows.Err()
}
func (w *configSet) template(kind, key string) (*templateSnapshot, error) {
	k := templateKey(kind, key)
	if t, ok := w.templates[k]; ok || w.allTemplates {
		return t, nil
	}
	if err := w.loadTemplates(`kind=? AND canonical_key=?`, kind, key); err != nil {
		return nil, err
	}
	if _, ok := w.templates[k]; !ok {
		w.templates[k], w.templatesBefore[k] = nil, nil
	}
	return w.templates[k], nil
}
func (w *configSet) loadAllTemplates() error {
	if w.allTemplates {
		return nil
	}
	err := w.loadTemplates(`1`)
	w.allTemplates = err == nil
	return err
}
func (w *configSet) putTemplate(t templateSnapshot) {
	key := templateKey(t.Kind, t.Key)
	if _, loaded := w.templatesBefore[key]; !loaded {
		w.templatesBefore[key] = nil
	}
	w.templates[key] = &t
}

func (w *configSet) loadDistributions(where string, args ...any) error {
	rows, err := w.q.Query(`SELECT canonical_key,provider,descriptor_json,distribution_digest FROM trusted_distribution_snapshots WHERE `+where, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		var d trustedDistribution
		if err = rows.Scan(&key, &d.Provider, &raw, &d.Digest); err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&d.Descriptor); err != nil {
			return err
		}
		if d.Descriptor.ID != key || d.Digest != distributionDigest(d.Descriptor) {
			return fmt.Errorf("invalid trusted distribution snapshot %s", key)
		}
		if _, loaded := w.distributionsBefore[key]; loaded {
			continue
		}
		before := d
		w.distributions[key], w.distributionsBefore[key] = &d, &before
	}
	return rows.Err()
}
func (w *configSet) distribution(key string) (*trustedDistribution, error) {
	if d, ok := w.distributions[key]; ok || w.allDistributions {
		return d, nil
	}
	if err := w.loadDistributions(`canonical_key=?`, key); err != nil {
		return nil, err
	}
	if _, ok := w.distributions[key]; !ok {
		w.distributions[key], w.distributionsBefore[key] = nil, nil
	}
	return w.distributions[key], nil
}
func (w *configSet) loadAllDistributions() error {
	if w.allDistributions {
		return nil
	}
	err := w.loadDistributions(`1`)
	w.allDistributions = err == nil
	return err
}
func (w *configSet) putDistribution(d trustedDistribution) {
	key := d.Descriptor.ID
	if _, loaded := w.distributionsBefore[key]; !loaded {
		w.distributionsBefore[key] = nil
	}
	w.distributions[key] = &d
}
func (w *configSet) distributionsChanged() bool {
	for key, d := range w.distributions {
		if distributionDigestOf(d) != distributionDigestOf(w.distributionsBefore[key]) || d != nil && w.distributionsBefore[key] != nil && d.Provider != w.distributionsBefore[key].Provider {
			return true
		}
	}
	return false
}

func (item TaxonomyItem) clone() TaxonomyItem {
	if item.Default != nil {
		value := *item.Default
		item.Default = &value
	}
	item.Override = cloneObject(item.Override)
	item.decorate()
	return item
}
func (w *configSet) loadCategories(where string, args ...any) error {
	rows, err := w.q.Query(`SELECT `+taxonomyColumns+` FROM categories WHERE `+where, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanTaxonomy(rows)
		if err != nil {
			return err
		}
		if _, loaded := w.categoriesBefore[item.ID]; loaded {
			continue
		}
		current := item.clone()
		w.categories[item.ID], w.categoriesBefore[item.ID] = &current, &item
	}
	return rows.Err()
}

// category returns a copy of the category, or nil when it does not exist.
func (w *configSet) category(id string) (*TaxonomyItem, error) {
	if _, ok := w.categories[id]; !ok && !w.allCategories {
		if err := w.loadCategories(`id=?`, id); err != nil {
			return nil, err
		}
		if _, ok = w.categories[id]; !ok {
			w.categories[id], w.categoriesBefore[id] = nil, nil
		}
	}
	if item := w.categories[id]; item != nil {
		copy := item.clone()
		return &copy, nil
	}
	return nil, nil
}
func (w *configSet) loadAllCategories() error {
	if w.allCategories {
		return nil
	}
	err := w.loadCategories(`1`)
	w.allCategories = err == nil
	return err
}

// categoryIDs lists the existing categories this set has loaded.
func (w *configSet) categoryIDs() []string {
	var out []string
	for id, item := range w.categories {
		if item != nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func (w *configSet) putCategory(item TaxonomyItem) {
	if _, loaded := w.categoriesBefore[item.ID]; !loaded {
		w.categoriesBefore[item.ID] = nil
	}
	w.categories[item.ID] = &item
}

// dropCategory forgets a category this operation created.
func (w *configSet) dropCategory(id string) {
	if w.categoriesBefore[id] == nil {
		w.categories[id] = nil
	}
}
func (w *configSet) requireCategories(ids []string) error {
	for _, id := range ids {
		item, err := w.category(id)
		if err != nil {
			return err
		}
		if item == nil {
			return invalidf("unknown category %s", id)
		}
	}
	return nil
}

func notesTable(kind string) string {
	if kind == "Vendor" {
		return "vendor_admin_notes"
	}
	return "application_admin_notes"
}
func (w *configSet) note(kind, uid string) (*noteEntry, error) {
	key := configKey(kind, uid)
	if n := w.notes[key]; n != nil {
		return n, nil
	}
	n := &noteEntry{}
	var stored AdminNotes
	err := w.q.QueryRow(`SELECT text,revision FROM `+notesTable(kind)+` WHERE entity_uid=?`, uid).Scan(&stored.Text, &stored.Revision)
	switch {
	case err == nil:
		n.AdminNotes, n.stored = stored, &stored
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	w.notes[key] = n
	return n, nil
}

func (w *configSet) globalProxy() (*globalEntry, error) {
	if w.global != nil {
		return w.global, nil
	}
	g := &globalEntry{config: networkproxy.Direct()}
	var raw []byte
	err := w.q.QueryRow(`SELECT revision,payload FROM settings WHERE key='upstream_proxy'`).Scan(&g.revision, &raw)
	switch {
	case err == nil:
		if g.config, err = parseGlobalProxy(raw); err != nil {
			return nil, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	g.stored = g.revision
	w.global = g
	return g, nil
}

// changed reports whether the operation changed any stored row.
func (w *configSet) changed() bool {
	if w.global != nil && w.global.revision != w.global.stored {
		return true
	}
	for _, v := range w.vendors {
		if v.changed {
			return true
		}
	}
	for _, a := range w.apps {
		if a.changed {
			return true
		}
	}
	for _, n := range w.notes {
		if n.dirty() {
			return true
		}
	}
	for key, t := range w.templates {
		if t != nil && !reflect.DeepEqual(t, w.templatesBefore[key]) {
			return true
		}
	}
	for id, item := range w.categories {
		if item != nil && !reflect.DeepEqual(item, w.categoriesBefore[id]) {
			return true
		}
	}
	return w.distributionsChanged()
}
func (n *noteEntry) dirty() bool {
	return n.Revision != 0 && (n.stored == nil || *n.stored != n.AdminNotes)
}

// materialize validates every changed entity and derives what is stored or
// published from its configuration: the directory columns, instructions,
// HTTP policy, source epoch and runtime revision.
func (w *configSet) materialize() error {
	for _, v := range w.sortedVendors() {
		if v.changed {
			if err := w.materializeVendor(v); err != nil {
				return err
			}
		}
	}
	for _, a := range w.sortedApps() {
		if err := w.acceptMissingContract(a); err != nil {
			return err
		}
		if a.changed {
			if err := w.materializeApp(a); err != nil {
				return err
			}
		}
	}
	return w.checkContracts()
}

func (w *configSet) materializeVendor(v *vendorEntry) error {
	if err := validateOwnedConfig("Vendor", v.config); err != nil {
		return err
	}
	spec, err := w.effective("Vendor", v.config)
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
	if v.proxy, err = decodeProxy(spec["proxy"]); err != nil {
		return ErrInvalidDirectory
	}
	v.Name, v.Description, v.Icon, v.LocalizedIcons = in.Name, in.Description, in.Icon, in.LocalizedIcons
	_, v.HasTemplate = BuiltinVendorTemplate(v.ID)
	// One classifier owns all runtime revision changes, regardless of write endpoint.
	v.RuntimeRevision = 1
	if v.before != nil {
		v.RuntimeRevision = v.before.RuntimeRevision
		if v.before.Enabled != v.Enabled || !reflect.DeepEqual(v.before.DeletedAt, v.DeletedAt) {
			v.RuntimeRevision++
		}
	}
	return nil
}

func (w *configSet) materializeApp(a *appEntry) error {
	if err := validateOwnedConfig("App", a.config); err != nil {
		return err
	}
	vendor, err := w.vendor(a.VendorUID)
	if err != nil {
		return err
	}
	if vendor == nil {
		return ErrInvalidDirectory
	}
	spec, err := w.effective("App", a.config)
	if err != nil {
		return err
	}
	if !noNull(map[string]any(spec)) {
		return ErrInvalidDirectory
	}
	var typed presets.AppSpec
	if err = strict(spec, &typed); err != nil {
		return err
	}
	categories, catErr := presets.NormalizeCategories(typed.Categories)
	tags, tagErr := presets.NormalizeTags(typed.Tags)
	if catErr != nil || tagErr != nil {
		return ErrInvalidDirectory
	}
	if a.DeletedAt == nil {
		if err = w.requireCategories(categories); err != nil {
			return err
		}
		a.Categories, a.Tags = categories, tags
	} else {
		a.Categories, a.Tags = []string{}, []string{}
	}
	if typed.Provider != a.Provider {
		return invalidf("Provider is immutable")
	}
	in := ApplicationInput{ID: a.ID, Name: LocalizedText{typed.Name.En, typed.Name.ZhCN}, Description: LocalizedText{typed.Description.En, typed.Description.ZhCN}, Icon: typed.Icon, Provider: typed.Provider, BaseURL: typed.BaseURL, BaseURLs: typed.BaseURLs, SourceStrategy: typed.SourceStrategy, CacheTTLSeconds: typed.CacheTTLSeconds, Enabled: a.Enabled}
	if err = validateApplication(&in); err != nil {
		return err
	}
	instructions := LocalizedText{typed.Instructions.En, typed.Instructions.ZhCN}
	if err = validateInstructions(instructions); err != nil {
		return err
	}
	if err = w.materializeInstructions(a, instructions); err != nil {
		return err
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
		a.policy = &policy
	}
	if a.proxy, err = decodeProxy(spec["proxy"]); err != nil {
		return ErrInvalidDirectory
	}
	if a.before != nil && (a.BaseURL != in.BaseURL || !reflect.DeepEqual(a.BaseURLs, in.BaseURLs) || a.SourceStrategy != in.SourceStrategy) {
		a.SourceEpoch++
	}
	a.Name, a.Description, a.Icon, a.BaseURL, a.BaseURLs, a.SourceStrategy, a.CacheTTLSeconds = in.Name, in.Description, in.Icon, in.BaseURL, in.BaseURLs, in.SourceStrategy, in.CacheTTLSeconds
	template, err := w.template("App", a.Key)
	if err != nil {
		return err
	}
	_, builtin := BuiltinApplicationTemplate(a.Key)
	a.BuiltinTemplate = template != nil || builtin
	key := distributionKey(a)
	d, err := w.distribution(key)
	if err != nil {
		return err
	}
	a.RuntimeRevision = 1
	if prev := a.before; prev != nil {
		a.RuntimeRevision = prev.RuntimeRevision
		changed := prev.Enabled != a.Enabled || !reflect.DeepEqual(prev.DeletedAt, a.DeletedAt) || prev.BaseURL != a.BaseURL || !reflect.DeepEqual(prev.BaseURLs, a.BaseURLs) || prev.SourceStrategy != a.SourceStrategy || prev.CacheTTLSeconds != a.CacheTTLSeconds || !reflect.DeepEqual(policyValue(a.policyBefore), policyValue(a.policy))
		if key != "" && distributionDigestOf(w.distributionsBefore[key]) != distributionDigestOf(d) {
			changed = true
		}
		if changed {
			a.RuntimeRevision++
		}
	}
	// Retained historical sources keep their namespaces; a new epoch needs one.
	if a.Provider != "info" && a.Provider != "hosted" {
		exists := false
		if a.before != nil {
			var one int
			err = w.q.QueryRow(`SELECT 1 FROM application_sources WHERE app_uid=? AND epoch=?`, a.UID, a.SourceEpoch).Scan(&one)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			exists = err == nil
		}
		if !exists {
			a.newSource = &SourceRecord{AppUID: a.UID, Epoch: a.SourceEpoch, Provider: a.Provider, BaseURL: a.BaseURL, BaseURLs: a.BaseURLs, SourceStrategy: a.SourceStrategy, CreatedAt: time.Now().UTC()}
		}
	}
	return nil
}
func policyValue(p *cachepolicy.Config) cachepolicy.Config {
	if p == nil {
		return cachepolicy.Config{}
	}
	return *p
}

// materializeInstructions advances the instructions revision when the text,
// the inheritance of either language, or an inherited template default changes.
func (w *configSet) materializeInstructions(a *appEntry, instructions LocalizedText) error {
	old := a.instructionsBefore
	changed := old.LocalizedText != instructions
	prev, cur := a.configBefore, a.config
	for _, p := range []string{"instructions.en", "instructions.zh-CN"} {
		_, was := leaf(prev.Overrides, p)
		_, now := leaf(cur.Overrides, p)
		if a.before != nil && was != now {
			changed = true
		}
		if a.before != nil && prev.Ref != nil && cur.Ref != nil && !was && !now {
			before := w.templatesBefore[templateKey("App", *prev.Ref)]
			after, err := w.template("App", *cur.Ref)
			if err != nil {
				return err
			}
			var oldDefault, newDefault any
			if before != nil {
				oldDefault, _ = leaf(before.Spec, p)
			}
			if after != nil {
				newDefault, _ = leaf(after.Spec, p)
			}
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
	if cur.Ref == nil && a.instructionsBefore.Revision == 0 && instructions == (LocalizedText{}) {
		a.instructions = Instructions{}
	} else {
		a.instructions = old
	}
	return nil
}

// checkContracts verifies the trusted release contracts this set has loaded:
// each is bound to an application template of the same provider, and each
// loaded release application runs with a contract of its own provider.
func (w *configSet) checkContracts() error {
	for key, d := range w.distributions {
		if d == nil {
			continue
		}
		t, err := w.template("App", key)
		if err != nil {
			return err
		}
		if t == nil || t.Spec["provider"] != d.Provider {
			return fmt.Errorf("invalid trusted Provider binding for %s", key)
		}
	}
	for _, a := range w.apps {
		if !presets.VersionsProvider(a.Provider) {
			continue
		}
		d, err := w.distribution(distributionKey(a))
		if err != nil {
			return err
		}
		if d != nil && d.Provider != a.Provider {
			return fmt.Errorf("missing trusted distribution for %s", a.Key)
		}
	}
	return nil
}

// reviewedDescriptors lists every trusted release contract, sorted by key.
func (w *configSet) reviewedDescriptors() ([]presets.Descriptor, error) {
	if err := w.loadAllDistributions(); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(w.distributions))
	for key, d := range w.distributions {
		if d != nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]presets.Descriptor, len(keys))
	for i, key := range keys {
		out[i] = w.distributions[key].Descriptor
	}
	return out, nil
}

// write stores every changed row. Each existing vendor, application, category,
// note and the global proxy is updated only if its stored revision is still
// the one this set read; otherwise the operation fails with ErrConflict.
func (w *configSet) write(tx *sql.Tx) error {
	if g := w.global; g != nil && g.revision != g.stored {
		if err := casExec(tx, g.stored == 0,
			`INSERT INTO settings(key,revision,payload) VALUES('upstream_proxy',?,?) ON CONFLICT(key) DO NOTHING`, []any{g.revision, encode(g.config)},
			`UPDATE settings SET revision=?,payload=? WHERE key='upstream_proxy' AND revision=?`, []any{g.revision, encode(g.config), g.stored}); err != nil {
			return err
		}
	}
	// Categories precede the template and application references to them.
	taxonomyChanged, prune := false, false
	for _, id := range sortedKeys(w.categories) {
		item, before := w.categories[id], w.categoriesBefore[id]
		if item == nil || reflect.DeepEqual(item, before) {
			continue
		}
		var stored int64
		if before != nil {
			stored = before.Revision
		}
		if err := writeCategory(tx, *item, stored); err != nil {
			return err
		}
		taxonomyChanged, prune = true, true
	}
	for _, key := range sortedKeys(w.templates) {
		t := w.templates[key]
		if t == nil || reflect.DeepEqual(t, w.templatesBefore[key]) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO template_snapshots(kind,canonical_key,schema_version,metadata_json,spec_json,semantic_hash,present) VALUES(?,?,?,?,?,?,?) ON CONFLICT(kind,canonical_key) DO UPDATE SET schema_version=excluded.schema_version,metadata_json=excluded.metadata_json,spec_json=excluded.spec_json,semantic_hash=excluded.semantic_hash,present=excluded.present`, t.Kind, t.Key, t.Schema, encode(t.Metadata), encode(t.Spec), t.Hash, t.Present); err != nil {
			return err
		}
		if t.Kind == "App" {
			if err := projectTemplateTaxonomy(tx, *t); err != nil {
				return err
			}
			prune = true
		}
	}
	for _, key := range sortedKeys(w.distributions) {
		d := w.distributions[key]
		if d == nil || reflect.DeepEqual(d, w.distributionsBefore[key]) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO trusted_distribution_snapshots(kind,canonical_key,provider,descriptor_json,distribution_digest) VALUES('App',?,?,?,?) ON CONFLICT(canonical_key) DO UPDATE SET provider=excluded.provider,descriptor_json=excluded.descriptor_json,distribution_digest=excluded.distribution_digest`, key, d.Provider, encode(d.Descriptor), d.Digest); err != nil {
			return err
		}
	}
	for _, v := range w.sortedVendors() {
		if v.changed {
			if err := v.write(tx); err != nil {
				return err
			}
		}
	}
	for _, a := range w.sortedApps() {
		if !a.changed {
			continue
		}
		if err := a.write(tx); err != nil {
			return err
		}
		if a.taxonomyChanged() {
			taxonomyChanged = true
		}
		prune = true
	}
	for _, key := range sortedKeys(w.notes) {
		n := w.notes[key]
		if !n.dirty() {
			continue
		}
		kind, uid, _ := strings.Cut(key, ":")
		var stored int64
		if n.stored != nil {
			stored = n.stored.Revision
		}
		if err := writeNote(tx, notesTable(kind), uid, stored, n.AdminNotes); err != nil {
			return err
		}
	}
	if !prune {
		return nil
	}
	// Every association change, including deletes, copies and imports, prunes unused categories.
	removed, err := cleanupCategories(tx)
	if err != nil {
		return err
	}
	if taxonomyChanged || removed > 0 {
		return bumpCategoryRevision(tx)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// casExec inserts a row that must not exist yet, or updates one whose
// revision must be unchanged. Either affecting no row means another writer
// got there first.
func casExec(tx *sql.Tx, insert bool, insertQuery string, insertArgs []any, updateQuery string, updateArgs []any) error {
	query, args := updateQuery, updateArgs
	if insert {
		query, args = insertQuery, insertArgs
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrConflict
	}
	return nil
}

func writeConfig(tx *sql.Tx, table, uid string, c ownedConfig) error {
	var spec any
	if c.Spec != nil {
		spec = encode(c.Spec)
	}
	_, err := tx.Exec(`INSERT INTO `+table+`(entity_uid,template_ref,overrides_json,spec_json) VALUES(?,?,?,?) ON CONFLICT(entity_uid) DO UPDATE SET template_ref=excluded.template_ref,overrides_json=excluded.overrides_json,spec_json=excluded.spec_json`, uid, c.Ref, encode(c.Overrides), spec)
	return err
}

func (v *vendorEntry) write(tx *sql.Tx) error {
	if v.before == nil {
		if _, err := tx.Exec(`INSERT INTO vendors(`+vendorColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.UID, v.ID, v.Name.En, v.Name.ZhCN, v.Description.En, v.Description.ZhCN, v.Icon, v.LocalizedIcons.En, v.LocalizedIcons.ZhCN, v.Enabled, v.Revision, v.RuntimeRevision, unixPointer(v.DeletedAt)); err != nil {
			return err
		}
	} else if err := casExec(tx, false, "", nil, `UPDATE vendors SET name_en=?,name_zh_cn=?,description_en=?,description_zh_cn=?,icon=?,enabled=?,revision=?,deleted_at_s=?,icon_en=?,icon_zh_cn=?,runtime_revision=? WHERE uid=? AND revision=?`, []any{v.Name.En, v.Name.ZhCN, v.Description.En, v.Description.ZhCN, v.Icon, v.Enabled, v.Revision, unixPointer(v.DeletedAt), v.LocalizedIcons.En, v.LocalizedIcons.ZhCN, v.RuntimeRevision, v.UID, v.before.Revision}); err != nil {
		return err
	}
	if v.before == nil || !bytes.Equal(encode(v.config), encode(v.configBefore)) {
		return writeConfig(tx, "vendor_config", v.UID, v.config)
	}
	return nil
}

func (a *appEntry) write(tx *sql.Tx) error {
	if a.before == nil {
		if _, err := tx.Exec(`INSERT INTO applications(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,source_strategy,cache_ttl_seconds,enabled,revision,runtime_revision,source_epoch,deleted_at_s) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.UID, a.VendorUID, a.ID, a.Name.En, a.Name.ZhCN, a.Description.En, a.Description.ZhCN, a.Icon, a.Provider, a.BaseURL, string(encode(a.BaseURLs)), a.SourceStrategy, a.CacheTTLSeconds, a.Enabled, a.Revision, a.RuntimeRevision, a.SourceEpoch, unixPointer(a.DeletedAt)); err != nil {
			return err
		}
	} else if err := casExec(tx, false, "", nil, `UPDATE applications SET name_en=?,name_zh_cn=?,description_en=?,description_zh_cn=?,icon=?,base_url=?,base_urls_json=?,source_strategy=?,cache_ttl_seconds=?,enabled=?,revision=?,source_epoch=?,deleted_at_s=?,runtime_revision=? WHERE uid=? AND revision=?`, []any{a.Name.En, a.Name.ZhCN, a.Description.En, a.Description.ZhCN, a.Icon, a.BaseURL, string(encode(a.BaseURLs)), a.SourceStrategy, a.CacheTTLSeconds, a.Enabled, a.Revision, a.SourceEpoch, unixPointer(a.DeletedAt), a.RuntimeRevision, a.UID, a.before.Revision}); err != nil {
		return err
	}
	if a.before == nil || !bytes.Equal(encode(a.config), encode(a.configBefore)) {
		if err := writeConfig(tx, "application_config", a.UID, a.config); err != nil {
			return err
		}
	}
	if a.instructions.Revision != 0 && a.instructions != a.instructionsBefore {
		if _, err := tx.Exec(`INSERT INTO application_instructions(app_uid,revision,en,zh_cn) VALUES(?,?,?,?) ON CONFLICT(app_uid) DO UPDATE SET revision=excluded.revision,en=excluded.en,zh_cn=excluded.zh_cn`, a.UID, a.instructions.Revision, a.instructions.En, a.instructions.ZhCN); err != nil {
			return err
		}
	}
	if a.policy != nil && !reflect.DeepEqual(a.policy, a.policyBefore) {
		if _, err := tx.Exec(`INSERT INTO application_http_policies(app_uid,payload) VALUES(?,?) ON CONFLICT(app_uid) DO UPDATE SET payload=excluded.payload`, a.UID, encode(*a.policy)); err != nil {
			return err
		}
	}
	if a.pending && !a.pendingBefore {
		if _, err := tx.Exec(`INSERT INTO pending_application_deletes(app_uid,requested_revision) VALUES(?,?)`, a.UID, a.pendingRevision); err != nil {
			return err
		}
	}
	if s := a.newSource; s != nil {
		if _, err := tx.Exec(`INSERT INTO application_sources(app_uid,epoch,provider,base_url,base_urls_json,source_strategy,created_at_s) VALUES(?,?,?,?,?,?,?)`, s.AppUID, s.Epoch, s.Provider, s.BaseURL, string(encode(s.BaseURLs)), s.SourceStrategy, s.CreatedAt.Unix()); err != nil {
			return err
		}
	}
	return projectAppTaxonomy(tx, a.Application)
}

// taxonomyChanged reports whether the public category or tag projection of
// the application changed.
func (a *appEntry) taxonomyChanged() bool {
	if a.before == nil || !reflect.DeepEqual(a.before.Categories, a.Categories) || !reflect.DeepEqual(a.before.Tags, a.Tags) {
		return true
	}
	for _, path := range []string{"categories", "tags"} {
		x, xok := leaf(a.configBefore.Overrides, path)
		y, yok := leaf(a.config.Overrides, path)
		if xok != yok || !bytes.Equal(encode(x), encode(y)) {
			return true
		}
	}
	return false
}

// writeNote stores notes under CAS: stored is the revision the writer read,
// 0 when no row existed.
func writeNote(tx *sql.Tx, table, uid string, stored int64, n AdminNotes) error {
	return casExec(tx, stored == 0,
		`INSERT INTO `+table+`(entity_uid,revision,text) VALUES(?,?,?) ON CONFLICT(entity_uid) DO NOTHING`, []any{uid, n.Revision, n.Text},
		`UPDATE `+table+` SET text=?,revision=? WHERE entity_uid=? AND revision=?`, []any{n.Text, n.Revision, uid, stored})
}
