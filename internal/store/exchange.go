package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ExportSelection struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	IncludeApps *bool  `json:"include_apps,omitempty"`
}
type ExportOptions struct {
	Selection               []ExportSelection `json:"selection"`
	Mode                    string            `json:"mode"`
	IncludeNotes            bool              `json:"include_notes"`
	IncludeProxyCredentials bool              `json:"include_proxy_credentials"`
}
type ImportChoice struct {
	Kind               string               `json:"kind"`
	Key                string               `json:"key"`
	Action             string               `json:"action"`
	TargetVendor       string               `json:"target_vendor,omitempty"`
	TargetID           string               `json:"target_id,omitempty"`
	DictionaryUpdate   bool                 `json:"dictionary_update,omitempty"`
	Proxy              *networkproxy.Config `json:"proxy,omitempty"`
	KeepEffectiveProxy bool                 `json:"keep_effective_proxy,omitempty"`
	DetachTemplate     bool                 `json:"detach_template,omitempty"`
	UpdateNotes        bool                 `json:"update_notes,omitempty"`
}
type ImportDifference struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type ImportItem struct {
	Kind                 string             `json:"kind"`
	Key                  string             `json:"key"`
	Target               string             `json:"target"`
	Action               string             `json:"action"`
	UID                  string             `json:"uid,omitempty"`
	Revision             int64              `json:"revision"`
	NotesRevision        int64              `json:"notes_revision"`
	TemplateHashMismatch bool               `json:"template_hash_mismatch"`
	TemplateMissing      bool               `json:"template_missing"`
	DetachTemplate       bool               `json:"detach_template"`
	Omitted              []string           `json:"omitted_fields"`
	Differences          []ImportDifference `json:"differences"`
	Requirements         []string           `json:"requirements"`
}

// ImportPlan is a reviewed import. Executing it recomputes the plan against the
// current rows of the entities it touches and fails with ErrConflict unless the
// result is the one that was reviewed, so the import is checked per entity.
type ImportPlan struct {
	Items      []ImportItem              `json:"items"`
	NeedsTrust bool                      `json:"needs_instructions_trust"`
	Ready      bool                      `json:"ready"`
	Documents  []configexchange.Document `json:"-"`
	Choices    []ImportChoice            `json:"-"`
	icons      map[string]bool
}
type ImportApplied struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	UID      string `json:"uid,omitempty"`
	Revision int64  `json:"revision"`
}
type ImportResult struct {
	Items   []ImportApplied `json:"items"`
	Applied bool            `json:"applied"`
}

func (s *Store) ExportConfiguration(options ExportOptions) (configexchange.Package, error) {
	p := configexchange.Package{Assets: map[string][]byte{}}
	if len(options.Selection) == 0 || len(options.Selection) > 1000 || options.Mode != "linked" && options.Mode != "independent" {
		return p, ErrInvalidDirectory
	}
	err := s.readConfiguration(func(w *configSet) error {
		var err error
		p, err = exportConfiguration(w, options)
		return err
	})
	return p, err
}
func exportConfiguration(w *configSet, options ExportOptions) (configexchange.Package, error) {
	p := configexchange.Package{Assets: map[string][]byte{}}
	type entry struct{ kind, key, mode string }
	entries := map[string]entry{}
	forcedParents := map[string]bool{}
	for _, sel := range options.Selection {
		if sel.Kind != "Vendor" && sel.Kind != "App" {
			return p, ErrInvalidDirectory
		}
		uid, _, deleted, err := w.entity(sel.Kind, sel.Key)
		if err != nil {
			return p, err
		}
		if uid == "" && sel.Kind == "Vendor" {
			return p, ErrVendorNotFound
		}
		if uid == "" {
			return p, ErrApplicationNotFound
		}
		if deleted {
			return p, invalidf("%s is deleted", sel.Key)
		}
		entries[templateKey(sel.Kind, sel.Key)] = entry{sel.Kind, sel.Key, options.Mode}
		if sel.Kind == "App" {
			vendor, _, _ := strings.Cut(sel.Key, "/")
			forcedParents[vendor] = true
			entries[templateKey("Vendor", vendor)] = entry{"Vendor", vendor, "independent"}
		} else if sel.IncludeApps == nil || *sel.IncludeApps {
			apps, err := w.vendorApps(uid)
			if err != nil {
				return p, err
			}
			for _, app := range apps {
				if app.DeletedAt == nil {
					entries[templateKey("App", app.Key)] = entry{"App", app.Key, options.Mode}
				}
			}
		}
	}
	for vendor := range forcedParents {
		key := templateKey("Vendor", vendor)
		entry := entries[key]
		entry.mode = "independent"
		entries[key] = entry
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	categoryIDs := map[string]bool{}
	for _, key := range keys {
		entry := entries[key]
		uid, _, _, err := w.entity(entry.kind, entry.key)
		if err != nil {
			return p, err
		}
		cfg := w.config(entry.kind, uid)
		effective, err := w.effective(entry.kind, cfg)
		if err != nil {
			return p, err
		}
		meta := &presets.Metadata{ID: entry.key}
		if entry.kind == "App" {
			meta.Vendor, meta.ID, _ = strings.Cut(entry.key, "/")
			var app presets.AppSpec
			if err = strict(effective, &app); err != nil {
				return p, err
			}
			for _, id := range app.Categories {
				categoryIDs[id] = true
			}
		}
		d := configexchange.Document{SchemaVersion: 1, Kind: entry.kind, Metadata: meta}
		if entry.mode == "linked" && cfg.Ref != nil {
			ref := *cfg.Ref
			t, err := w.template(entry.kind, ref)
			if err != nil {
				return p, err
			}
			d.Template = &ref
			d.TemplateHash = templateHash(t)
			d.Overrides = map[string]any(cloneObject(cfg.Overrides))
			if d.Overrides == nil {
				d.Overrides = map[string]any{}
			}
		} else {
			d.Spec = map[string]any(cloneObject(effective))
		}
		ownProxy, err := decodeProxy(effective["proxy"])
		if err != nil {
			return p, err
		}
		if ownProxy.Mode == "url" && !options.IncludeProxyCredentials {
			parsed, _ := url.Parse(ownProxy.URL)
			if parsed.User != nil {
				delete(d.Spec, "proxy")
				delete(d.Overrides, "proxy")
				d.OmittedFields = []string{"proxy"}
			}
		}
		if options.IncludeNotes {
			note, err := w.note(entry.kind, uid)
			if err != nil {
				return p, err
			}
			text := note.Text
			d.AdminNotes = &text
		}
		p.Documents = append(p.Documents, d)
	}
	// Tags travel inside App specs/overrides; only referenced category names need a dictionary document.
	tax := presets.TaxonomySpec{Categories: []presets.TaxonomyEntry{}}
	for id := range categoryIDs {
		item, err := w.category(id)
		if err != nil {
			return p, err
		}
		if item == nil {
			return p, invalidf("unknown category %s", id)
		}
		tax.Categories = append(tax.Categories, presets.TaxonomyEntry{ID: id, Name: presets.Text{En: item.Name.En, ZhCN: item.Name.ZhCN}})
	}
	sort.Slice(tax.Categories, func(i, j int) bool { return tax.Categories[i].ID < tax.Categories[j].ID })
	if len(tax.Categories) > 0 {
		p.Documents = append(p.Documents, configexchange.Document{SchemaVersion: 1, Kind: "Taxonomy", Spec: map[string]any(object(tax))})
	}
	if len(p.Documents) > configexchange.MaxEntities {
		return p, ErrInvalidDirectory
	}
	return p, nil
}
func choiceKey(kind, key string) string { return kind + ":" + key }
func (s *Store) PreviewConfigurationImport(documents []configexchange.Document, choices []ImportChoice) (ImportPlan, error) {
	var plan ImportPlan
	err := s.readConfiguration(func(w *configSet) error {
		var err error
		plan, err = makeImportPlan(newConfigSet(w.q), w, documents, choices)
		return err
	})
	return plan, err
}

// makeImportPlan applies the documents to w and reports what changes. base
// reads the same transaction unchanged and supplies the values before the import.
func makeImportPlan(base, w *configSet, documents []configexchange.Document, choices []ImportChoice) (ImportPlan, error) {
	plan := ImportPlan{Items: []ImportItem{}, Ready: true, Documents: documents, Choices: choices}
	byChoice := map[string]ImportChoice{}
	for _, c := range choices {
		k := choiceKey(c.Kind, c.Key)
		if _, ok := byChoice[k]; ok {
			return plan, ErrInvalidDirectory
		}
		byChoice[k] = c
	}
	seen := map[string]bool{}
	usedChoices := map[string]bool{}
	// Dictionaries precede entities, regardless of archive order.
	ordered := append([]configexchange.Document{}, documents...)
	sort.SliceStable(ordered, func(i, j int) bool { return kindOrder(ordered[i].Kind) < kindOrder(ordered[j].Kind) })
	for _, d := range ordered {
		if d.Validate() != nil {
			return plan, ErrInvalidDirectory
		}
		key := choiceKey(d.Kind, d.Key())
		if seen[key] {
			return plan, ErrInvalidDirectory
		}
		seen[key] = true
		if d.Kind == "Taxonomy" {
			var tax presets.TaxonomySpec
			if strict(Object(d.Spec), &tax) != nil {
				return plan, ErrInvalidDirectory
			}
			for _, entry := range tax.Categories {
				cKey := choiceKey(categoryKind, entry.ID)
				choice, chosen := byChoice[cKey]
				if chosen {
					usedChoices[cKey] = true
				}
				item, err := w.category(entry.ID)
				if err != nil {
					return plan, err
				}
				row := ImportItem{Kind: categoryKind, Key: entry.ID, Target: entry.ID, Action: "create", Differences: []ImportDifference{}, Requirements: []string{}, Omitted: []string{}}
				incoming := LocalizedText{entry.Name.En, entry.Name.ZhCN}
				if item != nil {
					row.Revision = item.Revision
					row.Action = "keep"
					if item.Name != incoming {
						row.Differences = append(row.Differences, ImportDifference{"name", item.Name, incoming})
						if choice.DictionaryUpdate {
							row.Action = "update"
							item.Revision++
							if item.Builtin {
								setLeaf(item.Override, "name.en", incoming.En)
								setLeaf(item.Override, "name.zh-CN", incoming.ZhCN)
							}
							item.Name = incoming
							item.decorate()
							w.putCategory(*item)
						}
					}
				} else {
					created := TaxonomyItem{Kind: categoryKind, ID: entry.ID, Name: incoming, Revision: 1, Present: true, Override: Object{}}
					created.decorate()
					w.putCategory(created)
				}
				plan.Items = append(plan.Items, row)
			}
			continue
		}
		choice, chosen := byChoice[key]
		if chosen {
			usedChoices[key] = true
		}
		target := d.Key()
		meta := *d.Metadata
		if choice.TargetID != "" || choice.TargetVendor != "" {
			if d.Kind != "App" || countAppDocuments(documents) != 1 {
				return plan, ErrInvalidDirectory
			}
			if choice.TargetID != "" {
				meta.ID = choice.TargetID
			}
			if choice.TargetVendor != "" {
				meta.Vendor = choice.TargetVendor
			}
			if !identity.ValidSlug(meta.ID) || !identity.ValidVendor(meta.Vendor) {
				return plan, ErrInvalidDirectory
			}
			target = meta.Vendor + "/" + meta.ID
		}
		uid, revision, deleted, err := w.entity(d.Kind, target)
		if err != nil {
			return plan, err
		}
		if deleted {
			return plan, ErrDirectoryDeleted
		}
		row := ImportItem{Kind: d.Kind, Key: d.Key(), Target: target, UID: uid, Revision: revision, Action: "create", Differences: []ImportDifference{}, Requirements: []string{}, Omitted: append([]string{}, d.OmittedFields...)}
		var note *noteEntry
		if uid != "" {
			row.Action = "skip"
			if note, err = w.note(d.Kind, uid); err != nil {
				return plan, err
			}
			row.NotesRevision = note.current()
		}
		if choice.Action != "" {
			row.Action = choice.Action
		}
		if uid != "" && row.Action == "create" {
			return plan, ErrDirectoryExists
		}
		if row.Action != "create" && row.Action != "skip" && row.Action != "update" || uid == "" && row.Action == "update" {
			return plan, invalidf("%s cannot be imported with action %q", target, row.Action)
		}
		oldCfg := w.config(d.Kind, uid)
		var before Object
		if uid != "" {
			if _, _, _, err = base.entity(d.Kind, target); err != nil {
				return plan, err
			}
			before, _ = base.effective(d.Kind, base.config(d.Kind, uid))
		}
		cfg := ownedConfig{Ref: d.Template, Spec: cloneObject(Object(d.Spec)), Overrides: cloneObject(Object(d.Overrides))}
		if cfg.Overrides == nil {
			cfg.Overrides = Object{}
		}
		var proposed Object
		if cfg.Ref != nil {
			t, err := w.template(d.Kind, *cfg.Ref)
			if err != nil {
				return plan, err
			}
			row.TemplateMissing = t == nil || !t.Present
			row.TemplateHashMismatch = t != nil && t.Hash != d.TemplateHash
			if t == nil || !t.Present && (uid == "" || oldCfg.Ref == nil || *oldCfg.Ref != *cfg.Ref) {
				return plan, invalidf("template unavailable; use an independent copy")
			}
			if err := validateOverrides(d.Kind, cfg.Overrides); err != nil {
				return plan, ErrInvalidDirectory
			}
			proposed = merge(t.Spec, cfg.Overrides)
		} else {
			check := cloneObject(cfg.Spec)
			if len(d.OmittedFields) > 0 {
				check["proxy"] = object(networkproxy.Inherit())
			}
			if err := validateExchangeSpec(d.Kind, check); err != nil {
				return plan, err
			}
			proposed = cloneObject(cfg.Spec)
		}
		if d.AdminNotes != nil && !validNotes(*d.AdminNotes) {
			return plan, ErrInvalidDirectory
		}
		validationCopy := cloneObject(proposed)
		if len(d.OmittedFields) > 0 {
			validationCopy["proxy"] = object(networkproxy.Inherit())
		}
		if validateExchangeSpec(d.Kind, validationCopy) != nil {
			return plan, ErrInvalidDirectory
		}
		if d.Kind == "App" {
			var app presets.AppSpec
			if strict(proposed, &app) != nil {
				return plan, ErrInvalidDirectory
			}
			categories, catErr := presets.NormalizeCategories(app.Categories)
			_, tagErr := presets.NormalizeTags(app.Tags)
			if catErr != nil || tagErr != nil || w.requireCategories(categories) != nil {
				return plan, ErrInvalidDirectory
			}
		}
		if row.Action == "skip" {
			row.DetachTemplate = d.Template == nil && oldCfg.Ref != nil
			if len(d.OmittedFields) > 0 {
				proposed["proxy"] = before["proxy"]
			}
			for _, field := range paths(d.Kind) {
				x, _ := leaf(before, field)
				y, _ := leaf(proposed, field)
				if !bytes.Equal(encode(x), encode(y)) {
					row.Differences = append(row.Differences, ImportDifference{field, x, y})
				}
			}
			if d.AdminNotes != nil && note.Text != *d.AdminNotes {
				row.Differences = append(row.Differences, ImportDifference{"admin_notes", note.Text, *d.AdminNotes})
			}
			plan.Items = append(plan.Items, row)
			continue
		}
		if uid == "" {
			if uid, err = identity.NewUID(); err != nil {
				return plan, err
			}
			if d.Kind == "Vendor" {
				w.addVendor(Vendor{UID: uid, ID: meta.ID, Revision: 1}, cfg)
			} else {
				parent, err := w.vendorByID(meta.Vendor)
				if err != nil {
					return plan, err
				}
				if parent == nil || parent.DeletedAt != nil {
					return plan, invalidf("target vendor required")
				}
				w.addApp(Application{UID: uid, ID: meta.ID, Key: target, VendorID: meta.Vendor, VendorUID: parent.UID, Provider: fmt.Sprint(proposed["provider"]), Revision: 1, SourceEpoch: 1}, cfg)
			}
			if note, err = w.note(d.Kind, uid); err != nil {
				return plan, err
			}
		}
		if d.Template == nil && oldCfg.Ref != nil {
			row.DetachTemplate = true
			if !choice.DetachTemplate {
				row.Requirements = append(row.Requirements, "confirm_detach_template")
			}
		}
		if len(d.OmittedFields) > 0 {
			var proxy networkproxy.Config
			if choice.Proxy != nil {
				// No saved password is bound to an import choice; use keep_effective_proxy.
				if _, err := networkproxy.KeepRedactedPassword(*choice.Proxy, networkproxy.Config{}); err != nil {
					return plan, invalidf("%s", err)
				}
				proxy = *choice.Proxy
			} else if choice.KeepEffectiveProxy && before != nil {
				if proxy, err = base.objectProxy(d.Kind, row.UID, nil); err != nil {
					return plan, err
				}
			} else if before != nil {
				proxy, _ = decodeProxy(before["proxy"])
			} else {
				proxy = networkproxy.Inherit()
				row.Requirements = append(row.Requirements, "resolve_omitted_proxy")
			}
			if proxy.Validate(true) != nil {
				return plan, ErrInvalidDirectory
			}
			if choice.Proxy == nil && !choice.KeepEffectiveProxy && before != nil && cfg.Ref != nil && reflect.DeepEqual(cfg.Ref, oldCfg.Ref) {
				if value, exists := oldCfg.Overrides["proxy"]; exists {
					cfg.Overrides["proxy"] = cloneValue(value)
				} else {
					delete(cfg.Overrides, "proxy")
				}
			} else {
				putConfigProxy(&cfg, proxy)
			}
		} else if choice.Proxy != nil || choice.KeepEffectiveProxy {
			return plan, ErrInvalidDirectory
		}
		w.setConfig(d.Kind, uid, cfg)
		if row.Action == "update" {
			bumpEntity(w, d.Kind, uid)
		}
		if d.AdminNotes != nil && note.Text != *d.AdminNotes {
			row.Differences = append(row.Differences, ImportDifference{"admin_notes", note.Text, *d.AdminNotes})
			if row.Action == "update" && !choice.UpdateNotes {
				row.Requirements = append(row.Requirements, "confirm_notes_update")
			} else {
				note.AdminNotes = AdminNotes{Text: *d.AdminNotes, Revision: note.current() + 1}
			}
		}
		after, err := w.effective(d.Kind, cfg)
		if err != nil {
			return plan, ErrInvalidDirectory
		}
		for _, field := range paths(d.Kind) {
			x, _ := leaf(before, field)
			y, _ := leaf(after, field)
			if !bytes.Equal(encode(x), encode(y)) {
				row.Differences = append(row.Differences, ImportDifference{field, x, y})
				if strings.HasPrefix(field, "instructions.") && (x != nil || y != "") {
					plan.NeedsTrust = true
				}
			}
		}
		if len(row.Requirements) > 0 {
			plan.Ready = false
		}
		plan.Items = append(plan.Items, row)
	}
	for k := range byChoice {
		if !usedChoices[k] {
			return plan, ErrInvalidDirectory
		}
	}
	if err := w.materialize(); err != nil {
		return plan, ErrInvalidDirectory
	}
	for i := range plan.Items {
		row := &plan.Items[i]
		choice := byChoice[choiceKey(row.Kind, row.Key)]
		if row.Action != "update" || len(row.Omitted) == 0 || choice.Proxy != nil || choice.KeepEffectiveProxy {
			continue
		}
		oldCfg := base.config(row.Kind, row.UID)
		newCfg := w.config(row.Kind, row.UID)
		if reflect.DeepEqual(oldCfg.Ref, newCfg.Ref) {
			continue
		}
		// Review the route the new binding would select without silently
		// choosing an effective-value override on the administrator's behalf.
		alternate := ownedConfig{Ref: newCfg.Ref, Overrides: cloneObject(newCfg.Overrides), Spec: cloneObject(newCfg.Spec)}
		if alternate.Ref != nil {
			delete(alternate.Overrides, "proxy")
		} else {
			alternate.Spec["proxy"] = object(networkproxy.Inherit())
		}
		own, err := w.ownProxy(row.Kind, alternate)
		if err != nil {
			return plan, ErrInvalidDirectory
		}
		was, err := base.objectProxy(row.Kind, row.UID, nil)
		if err != nil {
			return plan, err
		}
		would, err := w.objectProxy(row.Kind, row.UID, &own)
		if err != nil {
			return plan, ErrInvalidDirectory
		}
		if !reflect.DeepEqual(was, would) {
			row.Requirements = append(row.Requirements, "resolve_changed_proxy")
			plan.Ready = false
		}
	}
	// A created category that no resulting App uses would be pruned on commit;
	// preview it as skipped. Only Apps of this import can use a new category.
	referenced := map[string]bool{}
	plan.icons = map[string]bool{}
	for _, a := range w.apps {
		if a.changed {
			plan.icons[a.Icon] = true
			if a.DeletedAt == nil {
				for _, id := range a.Categories {
					referenced[id] = true
				}
			}
		}
	}
	for _, v := range w.vendors {
		if v.changed {
			plan.icons[v.Icon], plan.icons[v.LocalizedIcons.En], plan.icons[v.LocalizedIcons.ZhCN] = true, true, true
		}
	}
	for i, row := range plan.Items {
		if row.Kind == categoryKind && row.Action == "create" && !referenced[row.Key] {
			plan.Items[i].Action = "skip"
			w.dropCategory(row.Key)
		}
	}
	if len(plan.Items) > configexchange.MaxEntities {
		return plan, ErrInvalidDirectory
	}
	sort.Slice(plan.Items, func(i, j int) bool {
		a, b := plan.Items[i], plan.Items[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Key < b.Key
	})
	return plan, nil
}
func kindOrder(k string) int {
	if k == "Taxonomy" {
		return 0
	}
	if k == "Vendor" {
		return 1
	}
	return 2
}
func countAppDocuments(ds []configexchange.Document) int {
	n := 0
	for _, d := range ds {
		if d.Kind == "App" {
			n++
		}
	}
	return n
}
func validateExchangeSpec(kind string, value Object) error {
	if value == nil || !noNull(value) {
		return ErrInvalidDirectory
	}
	required := []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "proxy"}
	if kind == "Vendor" {
		var v presets.VendorSpec
		if strict(value, &v) != nil || validateVendor(VendorInput{ID: "exchange", Name: LocalizedText{v.Name.En, v.Name.ZhCN}, Description: LocalizedText{v.Description.En, v.Description.ZhCN}, Icon: v.Icon, LocalizedIcons: LocalizedText{v.LocalizedIcons.En, v.LocalizedIcons.ZhCN}}) != nil || v.Proxy.Validate(true) != nil {
			return ErrInvalidDirectory
		}
		required = append(required, "localized_icons.en", "localized_icons.zh-CN")
	} else {
		var v presets.AppSpec
		if strict(value, &v) != nil || v.Proxy.Validate(true) != nil {
			return ErrInvalidDirectory
		}
		input := ApplicationInput{ID: "exchange", Name: LocalizedText{v.Name.En, v.Name.ZhCN}, Description: LocalizedText{v.Description.En, v.Description.ZhCN}, Icon: v.Icon, Provider: v.Provider, BaseURL: v.BaseURL, BaseURLs: v.BaseURLs, SourceStrategy: v.SourceStrategy, CacheTTLSeconds: v.CacheTTLSeconds}
		if validateApplication(&input) != nil || validateInstructions(LocalizedText{v.Instructions.En, v.Instructions.ZhCN}) != nil {
			return ErrInvalidDirectory
		}
		if v.Prewarm != nil && v.Prewarm.Validate(v.Provider) != nil || v.Retention != nil && (!presets.VersionsProvider(v.Provider) || v.Retention.Validate() != nil) || v.HTTPPolicy != nil && (v.Provider != "http-cache" || v.HTTPPolicy.Validate() != nil) {
			return ErrInvalidDirectory
		}
		if _, e := presets.NormalizeCategories(v.Categories); e != nil {
			return ErrInvalidDirectory
		}
		if _, e := presets.NormalizeTags(v.Tags); e != nil {
			return ErrInvalidDirectory
		}
		required = append(required, "provider", "instructions.en", "instructions.zh-CN", "categories", "tags")
	}
	for _, p := range required {
		if _, ok := leaf(value, p); !ok {
			return ErrInvalidDirectory
		}
	}
	return nil
}
func putConfigProxy(c *ownedConfig, p networkproxy.Config) {
	if c.Ref == nil {
		c.Spec["proxy"] = object(p)
	} else {
		c.Overrides["proxy"] = object(p)
	}
}
func bumpEntity(w *configSet, kind, uid string) {
	if kind == "Vendor" {
		w.vendors[uid].Revision++
	} else {
		w.apps[uid].Revision++
	}
}
func importReceipt(q querier, id string) (ImportResult, bool, error) {
	var result ImportResult
	var raw []byte
	var created int64
	e := q.QueryRow(`SELECT result_json,created_s FROM configuration_import_receipts WHERE id=?`, id).Scan(&raw, &created)
	if errors.Is(e, sql.ErrNoRows) {
		return result, false, nil
	}
	if e != nil {
		return result, false, e
	}
	if created < time.Now().Add(-24*time.Hour).Unix() {
		return result, false, ErrExpired
	}
	e = json.Unmarshal(raw, &result)
	return result, e == nil, e
}
func (s *Store) ImportReceipt(id string) (ImportResult, bool, error) { return importReceipt(s.read, id) }

// ExecuteConfigurationImport applies a reviewed plan atomically: every entity
// it creates or updates, its categories and notes commit in one transaction or
// not at all. Each touched entity is checked against what the preview saw;
// changes to entities the import does not touch never conflict.
func (s *Store) ExecuteConfigurationImport(plan ImportPlan, id string, trust bool, guard ...func() bool) (ImportResult, error) {
	if result, found, e := s.ImportReceipt(id); e != nil || found {
		return result, e
	}
	result := ImportResult{Items: []ImportApplied{}, Applied: true}
	if !plan.Ready || plan.NeedsTrust && !trust {
		return result, ErrInvalidDirectory
	}
	var existing *ImportResult
	err := s.writeConfiguration(func(w *configSet) error {
		// Receipts are written under writeMu, so a concurrent request with the
		// same id that committed first is visible here.
		if previous, found, e := importReceipt(w.q, id); e != nil {
			return e
		} else if found {
			existing = &previous
			return errImportReceipt
		}
		fresh, e := makeImportPlan(newConfigSet(w.q), w, plan.Documents, plan.Choices)
		if errors.Is(e, ErrInvalidDirectory) || errors.Is(e, ErrDirectoryExists) || errors.Is(e, ErrDirectoryDeleted) || errors.Is(e, sql.ErrNoRows) {
			// The documents were valid when previewed; only a changed entity can fail them now.
			return ErrConflict
		}
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(fresh.Items, plan.Items) || fresh.Ready != plan.Ready || fresh.NeedsTrust != plan.NeedsTrust {
			return ErrConflict
		}
		for _, row := range fresh.Items {
			if row.Action == "skip" || row.Action == "keep" {
				continue
			}
			applied := ImportApplied{Kind: row.Kind, Key: row.Target}
			if row.Kind == categoryKind {
				item, e := w.category(row.Key)
				if e != nil {
					return e
				}
				applied.Revision = item.Revision
			} else if applied.UID, applied.Revision, _, e = w.entity(row.Kind, row.Target); e != nil {
				return e
			}
			result.Items = append(result.Items, applied)
		}
		return nil
	}, func(tx *sql.Tx) error {
		if len(guard) > 0 && !guard[0]() {
			return ErrImportGuard
		}
		if _, e := tx.Exec(`DELETE FROM configuration_import_receipts WHERE created_s<?`, time.Now().Add(-24*time.Hour).Unix()); e != nil {
			return e
		}
		_, e := tx.Exec(`INSERT INTO configuration_import_receipts(id,result_json,created_s) VALUES(?,?,?)`, id, encode(result), time.Now().Unix())
		return e
	})
	if errors.Is(err, errImportReceipt) {
		return *existing, nil
	}
	return result, err
}

var errImportReceipt = errors.New("import receipt already exists")

// ErrImportGuard means the caller's guard (the preview's session) rejected
// the import just before commit; nothing was changed.
var ErrImportGuard = errors.New("import guard rejected the commit")

type CopyApplicationInput struct {
	SourceUID      string `json:"source_uid"`
	SourceRevision int64  `json:"source_revision"`
	TargetVendor   string `json:"target_vendor"`
	TargetID       string `json:"target_id"`
	Mode           string `json:"mode"`
	IncludeNotes   bool   `json:"include_notes"`
	NotesRevision  int64  `json:"notes_revision"`
}

// CopyApplication creates a new application from another one's configuration.
// It reads the source, its template and notes, the target vendor and key only.
func (s *Store) CopyApplication(source string, input CopyApplicationInput) (Application, error) {
	if !identity.ValidVendor(input.TargetVendor) || !identity.ValidSlug(input.TargetID) || input.Mode != "linked" && input.Mode != "independent" {
		return Application{}, ErrInvalidDirectory
	}
	target := input.TargetVendor + "/" + input.TargetID
	err := s.writeConfiguration(func(w *configSet) error {
		src, err := w.appByKey(source)
		switch {
		case err != nil:
			return err
		case src == nil:
			return ErrApplicationNotFound
		case src.DeletedAt != nil:
			return ErrDirectoryDeleted
		case src.UID != input.SourceUID || src.Revision != input.SourceRevision:
			return ErrConflict
		}
		if existing, err := w.appByKey(target); err != nil {
			return err
		} else if existing != nil {
			return ErrDirectoryExists
		}
		parent, err := w.vendorByID(input.TargetVendor)
		if err != nil {
			return err
		}
		if parent == nil {
			return ErrVendorNotFound
		}
		if parent.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		old := src.config
		cfg := ownedConfig{Overrides: Object{}}
		if input.Mode == "linked" {
			if old.Ref == nil {
				return invalidf("linked copies need a source linked to a template")
			}
			t, err := w.template("App", *old.Ref)
			if err != nil {
				return err
			}
			if t == nil || !t.Present {
				return invalidf("the source template is unavailable; use an independent copy")
			}
			ref := *old.Ref
			cfg.Ref = &ref
			cfg.Overrides = cloneObject(old.Overrides)
		} else {
			effective, e := w.effective("App", old)
			if e != nil {
				return e
			}
			cfg.Spec = effective
		}
		fresh, e := identity.NewUID()
		if e != nil {
			return e
		}
		w.addApp(Application{UID: fresh, ID: input.TargetID, Key: target, VendorID: input.TargetVendor, VendorUID: parent.UID, Provider: src.Provider, Revision: 1, SourceEpoch: 1}, cfg)
		if input.IncludeNotes {
			note, err := w.note("App", src.UID)
			if err != nil {
				return err
			}
			if note.current() != input.NotesRevision {
				return ErrConflict
			}
			copied, err := w.note("App", fresh)
			if err != nil {
				return err
			}
			copied.AdminNotes = AdminNotes{Text: note.Text, Revision: 1}
		}
		return nil
	}, nil)
	if err != nil {
		return Application{}, err
	}
	return s.Application(target)
}
func (s *Store) IconReferenced(path string) bool {
	var count int
	e := s.read.QueryRow(`SELECT (SELECT count(*) FROM applications WHERE icon=?)+(SELECT count(*) FROM vendors WHERE icon=? OR icon_en=? OR icon_zh_cn=?)`, path, path, path, path).Scan(&count)
	return e != nil || count > 0
}

// ReferencesIcon reports whether an entity the plan creates or updates uses the icon.
func (plan ImportPlan) ReferencesIcon(path string) bool { return plan.icons[path] }
