package store

import (
	"bytes"
	"errors"
	"github.com/PMExtra/RedApp/internal/configexchange"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func exchangeOptions(mode string) ExportOptions {
	return ExportOptions{Selection: []ExportSelection{{Kind: "App", Key: "openai/codex"}}, Mode: mode}
}
func TestExchangeLinkedIndependentSensitiveRoundtripAndCopy(t *testing.T) {
	s := openTest(t)
	if e := s.ReconcileTemplates(taxonomySet()); e != nil {
		t.Fatal(e)
	}
	key := "openai/codex"
	a, _ := s.Application(key)
	patch(t, s, key, map[string]any{"name.en": a.Name.En, "instructions.zh-CN": "", "tags": []string{"cli"}, "categories": []string{"tools"}, "prewarm": presets.DefaultPrewarm(), "retention": presets.DefaultRetention(), "proxy": networkproxy.Config{Mode: "url", URL: "http://secret:password@127.0.0.1:3128"}})
	if _, e := s.SaveAdminNotes("app", key, 1, "private notes sentinel"); e != nil {
		t.Fatal(e)
	}
	p, e := s.ExportConfiguration(exchangeOptions("linked"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := configexchange.ZIP(p)
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal("compressed credentials")
	}
	var app configexchange.Document
	for _, d := range p.Documents {
		if d.Kind == "App" {
			app = d
			if d.Template == nil || d.Spec != nil || !reflect.DeepEqual(d.OmittedFields, []string{"proxy"}) || d.AdminNotes != nil {
				t.Fatal(d)
			}
			body, _ := configexchange.YAML(d)
			if bytes.Contains(body, []byte("password")) || bytes.Contains(body, []byte("private notes sentinel")) {
				t.Fatal("secret bytes leaked")
			}
		}
	}
	dest := openTest(t)
	if e = dest.ReconcileTemplates(taxonomySet()); e != nil {
		t.Fatal(e)
	}
	choices := []ImportChoice{{Kind: "Vendor", Key: "openai", Action: "update", DetachTemplate: true}, {Kind: "App", Key: key, Action: "update"}}
	plan, e := dest.PreviewConfigurationImport(p.Documents, choices)
	if e != nil {
		t.Fatal(e)
	}
	if !plan.Ready {
		t.Fatal(plan)
	}
	choices[1].Proxy = &networkproxy.Config{Mode: "direct"}
	plan, e = dest.PreviewConfigurationImport(p.Documents, choices)
	if e != nil {
		t.Fatal(e)
	}
	result, e := dest.ExecuteConfigurationImport(plan, "roundtrip", true)
	if e != nil || !result.Applied {
		t.Fatal(result, e)
	}
	c, _ := dest.ApplicationConfiguration(key)
	if c.Fields["name.en"].Source != "custom" || *c.Fields["name.en"].Differs || !reflect.DeepEqual(c.Effective["categories"], []string{"tools"}) || !reflect.DeepEqual(c.Effective["tags"], []string{"cli"}) {
		t.Fatal(c)
	}
	again, e := dest.ExecuteConfigurationImport(plan, "roundtrip", true)
	if e != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("idempotency", again, e)
	}
	if receipt, found, err := dest.ImportReceipt("roundtrip"); err != nil || !found || !reflect.DeepEqual(receipt, result) {
		t.Fatal("durable terminal result", receipt, err)
	}
	options := exchangeOptions("independent")
	options.IncludeNotes = true
	options.IncludeProxyCredentials = true
	p, e = s.ExportConfiguration(options)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range p.Documents {
		if d.Kind == "App" {
			app = d
			raw, _ := configexchange.YAML(d)
			if !bytes.Contains(raw, []byte("password")) || !bytes.Contains(raw, []byte("private notes sentinel")) || d.Template != nil {
				t.Fatal("explicit sensitive export", d)
			}
		}
	}
	plan, e = dest.PreviewConfigurationImport([]configexchange.Document{app}, []ImportChoice{{Kind: "App", Key: key, Action: "update", DetachTemplate: true, UpdateNotes: true}})
	if e != nil || !plan.Ready {
		t.Fatal(plan, e)
	}
	if _, e = dest.ExecuteConfigurationImport(plan, "independent", true); e != nil {
		t.Fatal(e)
	}
	c, _ = dest.ApplicationConfiguration(key)
	if c.TemplateRef != nil || c.Effective["proxy"] == nil {
		t.Fatal(c)
	}
	notes, _ := dest.AdminNotes("app", key)
	if notes.Text != "private notes sentinel" {
		t.Fatal(notes)
	}
	source, _ := s.Application(key)
	copy, e := s.CopyApplication(key, CopyApplicationInput{SourceUID: source.UID, SourceRevision: source.Revision, TargetVendor: "openai", TargetID: "clone", Mode: "linked"})
	if e != nil || copy.UID == source.UID || copy.Enabled || copy.SourceEpoch != 1 || copy.BuiltinTemplate {
		t.Fatal(copy, e)
	}
	cfg, _ := s.ApplicationConfiguration(copy.Key)
	original, _ := s.ApplicationConfiguration(key)
	if !reflect.DeepEqual(cfg.Overrides, original.Overrides) {
		t.Fatal("sparse copy changed", cfg)
	}
	note, _ := s.AdminNotes("app", copy.Key)
	if note.Text != "" {
		t.Fatal("notes copied by default")
	}
	if e = s.PermanentlyDeleteApplication(copy.Key, copy.Revision); e != nil {
		t.Fatal("built-in clone undeletable", e)
	}
	_, e = s.CopyApplication(key, CopyApplicationInput{SourceUID: "wrong", SourceRevision: source.Revision, TargetVendor: "openai", TargetID: "clone", Mode: "independent"})
	if !errors.Is(e, ErrConflict) {
		t.Fatal("copy CAS")
	}
}
func TestExchangeAtomicFailureMissingTemplateAndPreviewFences(t *testing.T) {
	s := openTest(t)
	s.ReconcileTemplates(presets.Embedded())
	p, _ := s.ExportConfiguration(exchangeOptions("independent"))
	for i := range p.Documents {
		if p.Documents[i].Kind == "Vendor" {
			p.Documents[i].Metadata.ID = "new"
		} else {
			p.Documents[i].Metadata.Vendor = "new"
			p.Documents[i].Metadata.ID = "copy"
		}
	}
	plan, e := s.PreviewConfigurationImport(p.Documents, nil)
	if e != nil || !plan.Ready {
		t.Fatal(plan, e)
	}
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) { return nil, errors.New("prepare failure") })
	if _, e = s.ExecuteConfigurationImport(plan, "fail", true); e == nil {
		t.Fatal("failure accepted")
	}
	if _, e = s.Vendor("new"); e == nil {
		t.Fatal("partial vendor")
	}
	s.SetConfigurationPrepare(nil)
	patch(t, s, "openai/codex", map[string]any{"name.en": "changed"})
	if _, e = s.ExecuteConfigurationImport(plan, "stale", true); !errors.Is(e, ErrConflict) {
		t.Fatal("preview CAS", e)
	}
	linked, _ := s.ExportConfiguration(exchangeOptions("linked"))
	for i := range linked.Documents {
		if linked.Documents[i].Kind == "App" {
			bad := linked.Documents[i]
			ref := "missing/template"
			bad.Template = &ref
			bad.Metadata = &presets.Metadata{Vendor: "openai", ID: "new"}
			if _, e = s.PreviewConfigurationImport([]configexchange.Document{bad}, nil); e == nil {
				t.Fatal("unknown template accepted")
			}
		}
	}
}
func TestExchangeNewOmittedProxyRequiresChoiceAndUnknownDeclarationsRejected(t *testing.T) {
	s := openTest(t)
	s.ReconcileTemplates(presets.Embedded())
	p, _ := s.ExportConfiguration(exchangeOptions("independent"))
	for i := range p.Documents {
		if p.Documents[i].Kind == "Vendor" {
			p.Documents[i].Metadata.ID = "new"
		} else {
			d := &p.Documents[i]
			d.Metadata = &presets.Metadata{Vendor: "new", ID: "copy"}
			delete(d.Spec, "proxy")
			d.OmittedFields = []string{"proxy"}
		}
	}
	plan, e := s.PreviewConfigurationImport(p.Documents, nil)
	if e != nil || plan.Ready {
		t.Fatal(plan, e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "unresolved", true); e == nil {
		t.Fatal("omitted proxy silently resolved")
	}
	plan, e = s.PreviewConfigurationImport(p.Documents, []ImportChoice{{Kind: "App", Key: "new/copy", Proxy: &networkproxy.Config{Mode: "inherit"}}})
	if e != nil || !plan.Ready {
		t.Fatal(plan, e)
	}
	for i := range p.Documents {
		if p.Documents[i].Kind == "App" {
			p.Documents[i].Spec["distribution"] = map[string]any{"key": "evil"}
		}
	}
	if _, e = s.PreviewConfigurationImport(p.Documents, nil); e == nil {
		t.Fatal("nested trusted declaration accepted")
	}
}

func TestExchangeMissingSnapshotHashWarningAndUIDABA(t *testing.T) {
	s := openTest(t)
	set := presets.Embedded()
	if e := s.ReconcileTemplates(set); e != nil {
		t.Fatal(e)
	}
	p, _ := s.ExportConfiguration(exchangeOptions("linked"))
	var doc configexchange.Document
	for _, d := range p.Documents {
		if d.Kind == "App" {
			doc = d
		}
	}
	doc.TemplateHash = strings.Repeat("0", 64)
	plan, e := s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: doc.Key(), Action: "update"}})
	if e != nil || !plan.Items[0].TemplateHashMismatch {
		t.Fatal("hash warning missing", plan, e)
	}
	set.Apps = nil
	if e = s.ReconcileTemplates(set); e != nil {
		t.Fatal(e)
	}
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: doc.Key(), Action: "update"}})
	if e != nil || !plan.Ready || !plan.Items[0].TemplateMissing {
		t.Fatal("accepted missing snapshot update rejected", plan, e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "missing", true); e != nil {
		t.Fatal(e)
	}
	copyDoc := doc
	copyDoc.Metadata = &presets.Metadata{Vendor: "openai", ID: "new"}
	if _, e = s.PreviewConfigurationImport([]configexchange.Document{copyDoc}, nil); e == nil {
		t.Fatal("new missing template accepted")
	}
	independent, _ := s.ExportConfiguration(exchangeOptions("independent"))
	for _, d := range independent.Documents {
		if d.Kind == "App" {
			copyDoc = d
			copyDoc.Metadata = &presets.Metadata{Vendor: "openai", ID: "aba"}
		}
	}
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{copyDoc}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "create-aba", true); e != nil {
		t.Fatal(e)
	}
	old, _ := s.Application("openai/aba")
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{copyDoc}, []ImportChoice{{Kind: "App", Key: "openai/aba", Action: "update"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PermanentlyDeleteApplication(old.Key, old.Revision); e != nil {
		t.Fatal(e)
	}
	fresh, e := s.CreateApplication("openai", ApplicationInput{ID: "aba", Provider: "info", Name: LocalizedText{En: "ABA", ZhCN: "重建"}})
	if e != nil || fresh.UID == old.UID {
		t.Fatal(fresh, e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "aba", true); !errors.Is(e, ErrConflict) {
		t.Fatal("UID ABA accepted", e)
	}
}
func TestExchangeWholeBatchDatabaseRollbackAndDictionaryOwnership(t *testing.T) {
	fault := &commitFault{}
	s := openTest(t, fault.option())
	set := taxonomySet()
	if e := s.ReconcileTemplates(set); e != nil {
		t.Fatal(e)
	}
	p, _ := s.ExportConfiguration(exchangeOptions("independent"))
	for i := range p.Documents {
		d := &p.Documents[i]
		if d.Kind == "Vendor" {
			d.Metadata.ID = "batch"
		} else if d.Kind == "App" {
			d.Metadata = &presets.Metadata{Vendor: "batch", ID: "app"}
			d.Spec["categories"] = []string{"new-category"}
			text := "batch-private-note"
			d.AdminNotes = &text
		}
	}
	p.Documents = append(p.Documents, configexchange.Document{SchemaVersion: 1, Kind: "Taxonomy", Spec: map[string]any(object(presets.TaxonomySpec{Categories: []presets.TaxonomyEntry{{ID: "new-category", Name: presets.Text{En: "New", ZhCN: "新"}}}}))})
	plan, e := s.PreviewConfigurationImport(p.Documents, nil)
	if e != nil {
		t.Fatal(e)
	}
	published, aborted := 0, 0
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		return publicationProbe{&published, &aborted}, nil
	})
	fault.armed.Store(true)
	if _, e = s.ExecuteConfigurationImport(plan, "rollback", true); e == nil {
		t.Fatal("batch DB failure ignored")
	}
	if _, e = s.Vendor("batch"); e == nil {
		t.Fatal("partial vendor")
	}
	if _, found := categoryItem(t, s, "new-category"); found {
		t.Fatal("partial dictionary")
	}
	var count int
	s.DB.QueryRow(`SELECT count(*) FROM application_admin_notes WHERE text='batch-private-note'`).Scan(&count)
	if count != 0 || published != 0 || aborted != 1 {
		t.Fatal("partial notes/publication", count, published, aborted)
	}
	fault.armed.Store(false)
	tax := configexchange.Document{SchemaVersion: 1, Kind: "Taxonomy", Spec: map[string]any(object(presets.TaxonomySpec{Categories: []presets.TaxonomyEntry{{ID: "tools", Name: presets.Text{En: "Imported tools", ZhCN: "导入工具"}}, {ID: "orphan", Name: presets.Text{En: "Orphan", ZhCN: "孤立"}}}}))}
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{tax}, nil)
	// A package category no resulting App uses would be pruned, so it is previewed as skipped.
	if e != nil || plan.Items[0].Key != "orphan" || plan.Items[0].Action != "skip" || plan.Items[1].Action != "keep" {
		t.Fatal(plan, e)
	}
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{tax}, []ImportChoice{{Kind: "categories", Key: "tools", DictionaryUpdate: true}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "dictionary", false); e != nil {
		t.Fatal(e)
	}
	item, _ := categoryItem(t, s, "tools")
	if !item.Builtin || item.Fields["name.en"].Source != "custom" || item.Name.En != "Imported tools" {
		t.Fatal("dictionary ownership replaced", item)
	}
	if _, found := categoryItem(t, s, "orphan"); found {
		t.Fatal("unused imported category kept")
	}
}
func TestExchangeOmittedProxyRebindRequiresResolutionAndKeepsNotes(t *testing.T) {
	s := openTest(t)
	set := presets.Embedded()
	s.ReconcileTemplates(set)
	s.CreateVendor(VendorInput{ID: "target", Name: LocalizedText{En: "Target", ZhCN: "目标"}})
	source, _ := s.Application("openai/codex")
	app, e := s.CopyApplication(source.Key, CopyApplicationInput{SourceUID: source.UID, SourceRevision: source.Revision, TargetVendor: "target", TargetID: "copy", Mode: "linked"})
	if e != nil {
		t.Fatal(e)
	}
	s.SaveAdminNotes("app", app.Key, 1, "preserve notes")
	// A separate valid template has a different proxy default. Omitting proxy may not switch the target's exit.
	extra := set.Apps[slices.IndexFunc(set.Apps, func(a presets.App) bool { return a.Key() == source.Key })]
	extra.Metadata.ID = "other"
	extra.Spec.Proxy = networkproxy.Config{Mode: "url", URL: "http://127.0.0.1:3128"}
	set.Apps = append(set.Apps, extra)
	if e = s.ReconcileTemplates(set); e != nil {
		t.Fatal(e)
	}
	st, _ := s.configurationState()
	ref := "openai/other"
	doc := configexchange.Document{SchemaVersion: 1, Kind: "App", Metadata: &presets.Metadata{Vendor: "target", ID: "copy"}, Template: &ref, TemplateHash: st.Templates[templateKey("App", ref)].Hash, Overrides: map[string]any{}, OmittedFields: []string{"proxy"}}
	// Current own proxy is inherited from the old template. Preserving it as an explicit inherit remains direct here.
	plan, e := s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: app.Key, Action: "update"}})
	if e != nil {
		t.Fatal(e)
	}
	if plan.Ready {
		t.Fatal("binding route decision silently selected", plan)
	}
	plan, e = s.PreviewConfigurationImport([]configexchange.Document{doc}, []ImportChoice{{Kind: "App", Key: app.Key, Action: "update", Proxy: &networkproxy.Config{Mode: "inherit"}}})
	if e != nil || !plan.Ready {
		t.Fatal(plan, e)
	}
	if _, e = s.ExecuteConfigurationImport(plan, "rebind", true); e != nil {
		t.Fatal(e)
	}
	c, e := s.ApplicationConfiguration(app.Key)
	if e != nil {
		t.Fatal("post-import configuration", e)
	}
	p, _ := decodeProxy(c.Effective["proxy"])
	if p.Mode != "inherit" {
		t.Fatal("omitted proxy became template URL", c)
	}
	n, _ := s.AdminNotes("app", app.Key)
	if n.Text != "preserve notes" {
		t.Fatal("omitted notes cleared", n)
	}
}
