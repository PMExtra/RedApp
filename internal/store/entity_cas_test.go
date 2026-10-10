package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/networkproxy"
)

// infoApps creates a vendor with one content application per id.
func infoApps(t *testing.T, s *Store, vendor string, ids ...string) []Application {
	t.Helper()
	if _, err := s.Vendor(vendor); errors.Is(err, sql.ErrNoRows) {
		if _, err = s.CreateVendor(VendorInput{ID: vendor, Name: LocalizedText{"Vendor", "厂商"}, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	var out []Application
	for _, id := range ids {
		a, err := s.CreateApplication(vendor, ApplicationInput{ID: id, Name: LocalizedText{id, id}, Provider: "info", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}
func describe(s *Store, key string, revision int64, text string) error {
	_, err := s.PatchApplicationConfiguration(key, ConfigurationPatch{Revision: revision, Set: map[string]json.RawMessage{"description.en": encode(text)}})
	return err
}

// concurrently runs every function at once and returns their errors.
func concurrently(fns ...func() error) []error {
	errs := make([]error, len(fns))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// publications records every snapshot published, and whether each matched
// the committed rows at the moment it was published.
type publications struct {
	mu        sync.Mutex
	store     *Store
	published []DirectorySnapshot
	aborted   int
	stale     []string
	fail      atomic.Bool
	// unchecked skips comparing published applications with their rows.
	unchecked bool
}

func (p *publications) install(s *Store) *publications {
	p.store = s
	s.SetConfigurationPrepare(func(snapshot DirectorySnapshot) (ConfigurationPublication, error) {
		if p.fail.Load() {
			return nil, errors.New("injected prepare failure")
		}
		return &recordedPublication{p, snapshot}, nil
	})
	return p
}
func (p *publications) last() DirectorySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.published[len(p.published)-1]
}

type recordedPublication struct {
	owner    *publications
	snapshot DirectorySnapshot
}

// Publish checks that every published application matches its committed row.
func (r *recordedPublication) Publish() {
	p := r.owner
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range r.snapshot.Applications {
		if p.unchecked {
			break
		}
		stored, err := p.store.Application(a.Key)
		if err != nil || stored.Revision != a.Revision || stored.Description != a.Description {
			p.stale = append(p.stale, a.Key)
		}
	}
	p.published = append(p.published, r.snapshot)
}
func (r *recordedPublication) Abort() {
	r.owner.mu.Lock()
	r.owner.aborted++
	r.owner.mu.Unlock()
}

func snapshotApp(snapshot DirectorySnapshot, uid string) Application {
	for _, a := range snapshot.Applications {
		if a.UID == uid {
			return a
		}
	}
	return Application{}
}

func TestConcurrentEditsOfDifferentApplicationsBothSucceed(t *testing.T) {
	s := openTest(t)
	apps := infoApps(t, s, "acme", "one", "two")
	p := (&publications{}).install(s)
	errs := concurrently(
		func() error { return describe(s, apps[0].Key, apps[0].Revision, "first") },
		func() error { return describe(s, apps[1].Key, apps[1].Revision, "second") },
	)
	if errs[0] != nil || errs[1] != nil {
		t.Fatal("unrelated edits conflicted", errs)
	}
	for i, want := range []string{"first", "second"} {
		if a, _ := s.Application(apps[i].Key); a.Description.En != want || a.Revision != apps[i].Revision+1 {
			t.Fatal("edit lost", a)
		}
		if a := snapshotApp(p.last(), apps[i].UID); a.Description.En != want {
			t.Fatal("last publication lacks an edit", a)
		}
	}
	if len(p.stale) != 0 {
		t.Fatal("publication saw uncommitted rows", p.stale)
	}
}

func TestEditIgnoresOtherEntitiesCommittedDuringIt(t *testing.T) {
	s := openTest(t)
	apps := infoApps(t, s, "acme", "one", "two")
	var once sync.Once
	// Another writer commits a change to a different application between this
	// write's read and its commit.
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		var err error
		once.Do(func() {
			_, err = s.db.Exec(`UPDATE applications SET revision=revision+1,description_en='other' WHERE uid=?`, apps[1].UID)
		})
		return nil, err
	})
	if err := describe(s, apps[0].Key, apps[0].Revision, "mine"); err != nil {
		t.Fatal("change to another application conflicted", err)
	}
	if a, _ := s.Application(apps[0].Key); a.Description.En != "mine" {
		t.Fatal(a)
	}
}

func TestConcurrentEditsOfTheSameApplicationExactlyOneWins(t *testing.T) {
	s := openTest(t)
	a := infoApps(t, s, "acme", "one")[0]
	p := (&publications{}).install(s)
	const writers = 8
	fns := make([]func() error, writers)
	for i := range fns {
		fns[i] = func() error { return describe(s, a.Key, a.Revision, fmt.Sprint("writer ", i)) }
	}
	won := ""
	for i, err := range concurrently(fns...) {
		switch {
		case err == nil && won == "":
			won = fmt.Sprint("writer ", i)
		case err == nil:
			t.Fatal("two writers won with the same revision")
		case !errors.Is(err, ErrConflict):
			t.Fatal(err)
		}
	}
	stored, _ := s.Application(a.Key)
	if won == "" || stored.Revision != a.Revision+1 || stored.Description.En != won {
		t.Fatal("winner not stored", won, stored)
	}
	if len(p.published) != 1 || len(p.stale) != 0 {
		t.Fatal("losing writers published", len(p.published), p.stale)
	}
}

func TestVendorProxyChangeDuringApplicationEdit(t *testing.T) {
	s := openTest(t)
	a := infoApps(t, s, "acme", "one")[0]
	v, _ := s.Vendor("acme")
	p := (&publications{}).install(s)
	vendorProxy := networkproxy.Config{Mode: "url", URL: "http://vendor.example:3128"}
	errs := concurrently(
		func() error {
			_, err := s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision, Set: map[string]json.RawMessage{"proxy": encode(vendorProxy)}})
			return err
		},
		func() error { return describe(s, a.Key, a.Revision, "edited") },
	)
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	c, _ := s.ApplicationConfiguration(a.Key)
	if _, overridden := c.Overrides["proxy"]; overridden || c.Revision != a.Revision+1 {
		t.Fatal("vendor change rewrote the application", c.Overrides, c.Revision)
	}
	if c.ProxyEffective.Config != vendorProxy || c.ProxyEffective.SourceScope != "vendor" {
		t.Fatal("effective view does not inherit the vendor proxy", c.ProxyEffective)
	}
	last := p.last()
	if scope := last.ProxyScopes[a.UID]; scope.Proxy.Config != vendorProxy || snapshotApp(last, a.UID).Description.En != "edited" {
		t.Fatal("publication lacks one of the writes", scope, snapshotApp(last, a.UID))
	}
}

func TestImportWithOneStaleEntityRollsBackEntirely(t *testing.T) {
	s := openTest(t)
	ids := []string{"a1", "a2", "a3", "a4", "a5"}
	apps := infoApps(t, s, "batch", ids...)
	unrelated := infoApps(t, s, "other", "x")[0]
	preview := func() ImportPlan {
		t.Helper()
		p, err := s.ExportConfiguration(ExportOptions{Selection: []ExportSelection{{Kind: "Vendor", Key: "batch"}}, Mode: "independent"})
		if err != nil {
			t.Fatal(err)
		}
		var choices []ImportChoice
		for _, d := range p.Documents {
			if d.Kind == "App" {
				d.Spec["description"].(map[string]any)["en"] = "imported"
				choices = append(choices, ImportChoice{Kind: "App", Key: d.Key(), Action: "update"})
			}
		}
		plan, err := s.PreviewConfigurationImport(p.Documents, choices)
		if err != nil || !plan.Ready {
			t.Fatal(plan, err)
		}
		return plan
	}
	plan := preview()
	before := configurationRows(t, s)
	// One application the import updates changes after the preview.
	if err := describe(s, apps[2].Key, apps[2].Revision, "concurrent"); err != nil {
		t.Fatal(err)
	}
	changed := configurationRows(t, s)
	if _, err := s.ExecuteConfigurationImport(plan, "stale", false); !errors.Is(err, ErrConflict) {
		t.Fatal("stale entity not detected", err)
	}
	if after := configurationRows(t, s); !reflect.DeepEqual(changed, after) || reflect.DeepEqual(before, after) {
		t.Fatal("rejected import changed rows")
	}
	if _, found, _ := s.ImportReceipt("stale"); found {
		t.Fatal("rejected import left a receipt")
	}
	// A change to an entity the import does not touch never conflicts.
	plan = preview()
	if err := describe(s, unrelated.Key, unrelated.Revision, "unrelated"); err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteConfigurationImport(plan, "fresh", false)
	if err != nil || len(result.Items) != len(ids) {
		t.Fatal(result, err)
	}
	for _, a := range apps {
		if stored, _ := s.Application(a.Key); stored.Description.En != "imported" {
			t.Fatal("import not applied to", a.Key)
		}
	}
}

func TestPublicationNeverObservesAHalfAppliedWrite(t *testing.T) {
	fault := &commitFault{}
	s := openTest(t, fault.option())
	a := infoApps(t, s, "acme", "one")[0]
	p := (&publications{}).install(s)
	if err := describe(s, a.Key, a.Revision, "committed"); err != nil {
		t.Fatal(err)
	}
	rows := configurationRows(t, s)
	p.fail.Store(true)
	if err := describe(s, a.Key, a.Revision+1, "failed prepare"); err == nil {
		t.Fatal("prepare failure ignored")
	}
	p.fail.Store(false)
	fault.armed.Store(true)
	if err := describe(s, a.Key, a.Revision+1, "failed commit"); err == nil {
		t.Fatal("commit failure ignored")
	}
	fault.armed.Store(false)
	if after := configurationRows(t, s); !reflect.DeepEqual(rows, after) {
		t.Fatal("failed write changed rows")
	}
	if len(p.published) != 1 || p.aborted != 1 {
		t.Fatal("failed write published", len(p.published), p.aborted)
	}
	// The next write starts from the committed state, not from the failed ones.
	b := infoApps(t, s, "acme", "two")[0]
	last := p.last()
	if got := snapshotApp(last, a.UID); got.Description.En != "committed" || got.Revision != a.Revision+1 || snapshotApp(last, b.UID).UID != b.UID {
		t.Fatal("publication carried a failed write", got)
	}
	if len(p.stale) != 0 {
		t.Fatal("publication saw uncommitted rows", p.stale)
	}
}

func TestCategoryPruningUnderConcurrentSaves(t *testing.T) {
	s := openTest(t)
	apps := infoApps(t, s, "acme", "keeper", "taker")
	for round := range 5 {
		keeper, _ := s.Application(apps[0].Key)
		saved, err := s.PatchApplicationConfiguration(keeper.Key, ConfigurationPatch{Revision: keeper.Revision, Set: map[string]json.RawMessage{"categories": encode([]string{})}, NewCategories: []string{fmt.Sprint("Temporary ", round)}})
		if err != nil {
			t.Fatal(err)
		}
		id := saved.Effective["categories"].([]string)[0]
		taker, _ := s.Application(apps[1].Key)
		// The keeper drops the category while the taker adopts it.
		errs := concurrently(
			func() error { return setCategories(s, keeper.Key, saved.Revision, nil) },
			func() error { return setCategories(s, taker.Key, taker.Revision, []string{id}) },
		)
		if errs[0] != nil {
			t.Fatal(errs[0])
		}
		_, err = s.Category(id)
		adopted, _ := s.Application(taker.Key)
		switch {
		case errs[1] == nil && (err != nil || !reflect.DeepEqual(adopted.Categories, []string{id})):
			t.Fatal("adopted category was pruned", err, adopted.Categories)
		case errs[1] != nil && (!errors.Is(errs[1], ErrInvalidDirectory) || !errors.Is(err, sql.ErrNoRows)):
			t.Fatal("rejected adoption left the category or failed differently", errs[1], err)
		}
		if err == nil {
			if err = setCategories(s, taker.Key, adopted.Revision, nil); err != nil {
				t.Fatal(err)
			}
		}
		var orphans int
		if err = s.db.QueryRow(`SELECT count(*) FROM categories WHERE builtin=0 AND id NOT IN (SELECT category_id FROM application_categories)`).Scan(&orphans); err != nil || orphans != 0 {
			t.Fatal("unused category kept", orphans, err)
		}
	}
}
func setCategories(s *Store, key string, revision int64, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	_, err := s.PatchApplicationConfiguration(key, ConfigurationPatch{Revision: revision, Set: map[string]json.RawMessage{"categories": encode(ids)}})
	return err
}

func TestConcurrentNewCategoryNamesResolveToOneCategory(t *testing.T) {
	s := openTest(t)
	apps := infoApps(t, s, "acme", "one", "two")
	fns := make([]func() error, len(apps))
	for i, a := range apps {
		fns[i] = func() error {
			_, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"categories": encode([]string{})}, NewCategories: []string{"Shared Tools"}})
			return err
		}
	}
	if errs := concurrently(fns...); errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	one, _ := s.Application(apps[0].Key)
	two, _ := s.Application(apps[1].Key)
	if len(one.Categories) != 1 || !reflect.DeepEqual(one.Categories, two.Categories) {
		t.Fatal("typed name created two categories", one.Categories, two.Categories)
	}
}

// bulkApplications adds n copies of source in SQL; creating them one write at
// a time would dominate the test.
func bulkApplications(t *testing.T, s *Store, source Application, n int) {
	t.Helper()
	for _, query := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO applications(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,source_strategy,cache_ttl_seconds,enabled,revision,runtime_revision,source_epoch,deleted_at_s) SELECT printf('%032x',i),vendor_uid,printf('bulk-%04d',i),name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,source_strategy,cache_ttl_seconds,enabled,revision,runtime_revision,source_epoch,deleted_at_s FROM applications,n WHERE uid=?`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?) INSERT INTO application_config(entity_uid,template_ref,overrides_json,spec_json) SELECT printf('%032x',i),template_ref,overrides_json,spec_json FROM application_config,n WHERE entity_uid=?`,
	} {
		if _, err := s.db.Exec(query, n, source.UID); err != nil {
			t.Fatal(err)
		}
	}
}

// countLoads counts the application rows the store decodes while run runs.
func countLoads(run func()) (apps int64) {
	var n atomic.Int64
	observe := func(kind string) {
		if kind == "application" {
			n.Add(1)
		}
	}
	entityLoads.Store(&observe)
	defer entityLoads.Store(nil)
	run()
	return n.Load()
}

func TestWritesReadOnlyTheEntitiesTheyTouch(t *testing.T) {
	const bulk = 1000
	s := openTest(t)
	apps := infoApps(t, s, "acme", "one", "two")
	bulkApplications(t, s, apps[1], bulk)
	p := (&publications{unchecked: true}).install(s)
	// The first publication loads the runtime view once.
	if loads := countLoads(func() {
		if err := describe(s, apps[1].Key, apps[1].Revision, "warm"); err != nil {
			t.Fatal(err)
		}
	}); loads < bulk {
		t.Fatal("runtime view not loaded", loads)
	}
	v, _ := s.Vendor("acme")
	proxy := networkproxy.Config{Mode: "url", URL: "http://vendor.example:3128"}
	for name, write := range map[string]func() error{
		"application": func() error { return describe(s, apps[0].Key, apps[0].Revision, "edited") },
		"vendor": func() error {
			_, err := s.PatchVendorConfiguration(v.ID, ConfigurationPatch{Revision: v.Revision, Set: map[string]json.RawMessage{"proxy": encode(proxy)}})
			return err
		},
		"global": func() error { _, err := s.PatchGlobalProxy(1, networkproxy.Direct()); return err },
	} {
		if loads := countLoads(func() {
			if err := write(); err != nil {
				t.Fatal(name, err)
			}
		}); loads > 5 {
			t.Fatalf("%s write read %d of %d applications", name, loads, bulk+2)
		}
	}
	last := p.last()
	if len(last.Applications) != bulk+2 || last.ProxyScopes[fmt.Sprintf("%032x", 7)].Proxy.Config != proxy {
		t.Fatal("publication is incomplete", len(last.Applications))
	}
}
