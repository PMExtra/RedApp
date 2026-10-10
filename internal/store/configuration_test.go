package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
)

func TestConfigurationWholeLeavesSurviveTemplateUpdateAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	set := presets.Embedded()
	for i := range set.Apps {
		if set.Apps[i].Metadata.Vendor == "openai" && set.Apps[i].Metadata.ID == "codex" {
			set.Apps[i].Spec.Proxy = networkproxy.Config{Mode: "url", URL: "http://127.0.0.1:3128"}
			set.Apps[i].Spec.Prewarm = &presets.Prewarm{Enabled: true, Channels: []string{"latest"}, Platforms: []string{"linux-x64"}}
			set.Apps[i].Spec.Retention = &presets.Retention{Enabled: true, KeepLatest: 7}
		}
	}
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"proxy": networkproxy.Config{Mode: "inherit"}, "prewarm": presets.DefaultPrewarm(), "retention": presets.DefaultRetention()}
	patch(t, s, "openai/codex", want)
	check := func() {
		t.Helper()
		cfg, err := s.ApplicationConfiguration("openai/codex")
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range want {
			if !bytes.Equal(encode(cfg.Effective[key]), encode(object(value))) {
				t.Fatalf("%s retained template fields: %s", key, encode(cfg.Effective[key]))
			}
		}
	}
	check()
	set.Apps[0].Spec.Name.En += " changed"
	if err = s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	check()
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	check()
}

func presetFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	root := os.DirFS("../../presets")
	if err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := fs.ReadFile(root, p)
		if e == nil {
			out[p] = &fstest.MapFile{Data: b}
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func loadFixture(t *testing.T, f fstest.MapFS) presets.Set {
	t.Helper()
	s, e := presets.LoadFS(f)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func patch(t *testing.T, s *Store, key string, set map[string]any, unset ...string) Configuration {
	t.Helper()
	c, e := s.ApplicationConfiguration(key)
	if e != nil {
		t.Fatal(e)
	}
	p := ConfigurationPatch{Revision: c.Revision, Set: map[string]json.RawMessage{}, Unset: unset}
	for k, v := range set {
		p.Set[k] = encode(v)
	}
	c, e = s.PatchApplicationConfiguration(key, p)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestConfigurationTemplateUpgradeSparseOverridesAndEpoch(t *testing.T) {
	s := openTest(t)
	initial := presets.Embedded()
	if err := s.ReconcileTemplates(initial); err != nil {
		t.Fatal(err)
	}
	key := "openai/codex"
	a, _ := s.Application(key)
	i, _ := s.Instructions(a.UID)
	c := patch(t, s, key, map[string]any{"name.en": a.Name.En, "description.zh-CN": "", "instructions.en": "", "cache_ttl_seconds": 123})
	if c.Fields["name.en"].Source != "custom" || *c.Fields["name.en"].Differs {
		t.Fatal("equal-value override lost", c)
	}
	f := presetFixture(t)
	raw := string(f["openai/codex.yaml"].Data)
	raw = strings.Replace(raw, `en: "Codex CLI"`, `en: "New Codex"`, 1)
	raw = strings.Replace(raw, `"zh-CN": "Codex CLI"`, `"zh-CN": "新 Codex"`, 1)
	raw = strings.Replace(raw, `base_url: "https://releases.openai.com/codex"`, `base_url: "https://releases.openai.com/codex/new"`, 1)
	f["openai/codex.yaml"].Data = []byte(raw)
	if err := s.ReconcileTemplates(loadFixture(t, f)); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Application(key)
	c, _ = s.ApplicationConfiguration(key)
	saved, _ := s.Instructions(a.UID)
	if current.Revision != a.Revision+2 || current.SourceEpoch != a.SourceEpoch+1 || current.Name.En != a.Name.En || current.Name.ZhCN != "新 Codex" || current.CacheTTLSeconds != 123 || saved.En != "" || saved.ZhCN != i.ZhCN {
		t.Fatal("incorrect overlay upgrade", current, saved, c)
	}
	before := current
	f["openai/codex.yaml"].Data = append([]byte("# Comment-only change\n"), f["openai/codex.yaml"].Data...)
	if err := s.ReconcileTemplates(loadFixture(t, f)); err != nil {
		t.Fatal(err)
	}
	current, _ = s.Application(key)
	if !reflect.DeepEqual(before, current) {
		t.Fatal("comment changed revision or epoch", current)
	}
	c = patch(t, s, key, nil, "name.en", "instructions.en", "cache_ttl_seconds")
	current, _ = s.Application(key)
	saved, _ = s.Instructions(a.UID)
	if c.Fields["name.en"].Source != "inherited" || current.Name.En != "New Codex" || saved.En != i.En || current.CacheTTLSeconds != 60 || current.SourceEpoch != before.SourceEpoch {
		t.Fatal("unset failed", c, current)
	}
}

func TestConfigurationMissingTemplateFrozenAndReappearance(t *testing.T) {
	s := openTest(t)
	set := presets.Embedded()
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Application("openai/codex")
	missing := presets.Embedded()
	codex := slices.IndexFunc(missing.Apps, func(a presets.App) bool { return a.Key() == before.Key })
	missing.Apps = slices.Delete(missing.Apps, codex, codex+1)
	if err := s.ReconcileTemplates(missing); err != nil {
		t.Fatal(err)
	}
	c, _ := s.ApplicationConfiguration(before.Key)
	after, _ := s.Application(before.Key)
	if !c.TemplateMissing || after.UID != before.UID || after.Enabled != before.Enabled || after.SourceEpoch != before.SourceEpoch {
		t.Fatal("missing template destroyed entity", c, after)
	}
	if _, err := s.CreateConfiguredApplication("openai", ApplicationInput{ID: "copy", Name: before.Name, Provider: before.Provider, BaseURL: before.BaseURL, CacheTTLSeconds: 60}, c.TemplateRef); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("missing template admitted new binding", err)
	}
	bad := presets.Embedded()
	bad.Apps[codex].Spec.Provider = "http-cache"
	bad.Apps[codex].Distribution = nil
	state, _ := s.configurationState()
	if err := s.ReconcileTemplates(bad); err == nil {
		t.Fatal("Provider changed")
	}
	retained, _ := s.configurationState()
	if !bytes.Equal(encode(state), encode(retained)) {
		t.Fatal("partial template transaction")
	}
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	c, _ = s.ApplicationConfiguration(before.Key)
	if c.TemplateMissing {
		t.Fatal("template did not reappear")
	}
}

func TestConfigurationPolicyArraysEmptyValuesAndRejectOperations(t *testing.T) {
	s := openTest(t)
	set := presets.Embedded()
	community := presets.App{SchemaVersion: 1, Kind: "App", Metadata: presets.Metadata{ID: "files", Vendor: "openai"}, Spec: presets.AppSpec{Name: presets.Text{En: "Files", ZhCN: "文件"}, Provider: "http-cache", BaseURLs: []string{"https://first.example", "https://second.example"}, CacheTTLSeconds: 60, HTTPPolicy: ptrPolicy(cachepolicy.Empty())}}
	set.Apps = append(set.Apps, community)
	if err := s.ReconcileTemplates(set); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Application("openai/files")
	c := patch(t, s, a.Key, map[string]any{"base_urls": []string{"https://second.example", "https://first.example"}, "cache_ttl_seconds": 0, "http_policy.rules": []any{}, "http_policy.auto_cleanup": []any{}, "http_policy.stale_fallback": false, "instructions.zh-CN": ""})
	current, _ := s.Application(a.Key)
	policy, _, _ := s.ReadHTTPPolicy(a.Key)
	if current.SourceEpoch != a.SourceEpoch+1 || current.CacheTTLSeconds != 0 || policy.StaleFallback || len(policy.Rules) != 0 || c.Fields["instructions.en"].Source != "inherited" || c.Fields["instructions.zh-CN"].Source != "custom" {
		t.Fatal(current, policy, c)
	}
	for _, path := range []string{"enabled", "notes", "provider", "id", "vendor", "uid", "distribution", "trust", "name", "http_policy.rules.0.ttl_seconds"} {
		if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: current.Revision, Set: map[string]json.RawMessage{path: encode(false)}}); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatal(path, err)
		}
	}
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: current.Revision, Set: map[string]json.RawMessage{"instructions.en": []byte("null")}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("null accepted", err)
	}
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: current.Revision, Set: map[string]json.RawMessage{"icon": encode("")}, Unset: []string{"icon"}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("conflicting operations accepted", err)
	}
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: current.Revision, Set: map[string]json.RawMessage{"base_urls": encode([]string{})}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("invalid empty source list accepted", err)
	}
	c, _ = s.ApplicationConfiguration(a.Key)
	if c.Revision != current.Revision {
		t.Fatal("invalid patch partly saved")
	}
}
func ptrPolicy(c cachepolicy.Config) *cachepolicy.Config { return &c }

type publicationProbe struct{ published, aborted *int }

func (p publicationProbe) Publish() { *p.published++ }
func (p publicationProbe) Abort()   { *p.aborted++ }
func TestConfigurationPrepareCASAndDatabaseFailuresAreAtomic(t *testing.T) {
	fault := &commitFault{}
	s := openTest(t, fault.option())
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Application("openai/codex")
	before, _ := s.ApplicationConfiguration(a.Key)
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		return nil, errors.New("injected prepare failure")
	})
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"description.en": encode("new")}}); err == nil {
		t.Fatal("prepare failure ignored")
	}
	published, aborted := 0, 0
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		return publicationProbe{&published, &aborted}, nil
	})
	fault.armed.Store(true)
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"description.en": encode("new")}}); err == nil {
		t.Fatal("DB failure ignored")
	}
	after, _ := s.ApplicationConfiguration(a.Key)
	if !reflect.DeepEqual(before, after) || published != 0 || aborted != 1 {
		t.Fatal("partial save/publication", after, published, aborted)
	}
	fault.armed.Store(false)
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		_, err := s.db.Exec(`UPDATE vendors SET revision=revision+1 WHERE id='anthropic'`)
		if err != nil {
			return nil, err
		}
		return publicationProbe{&published, &aborted}, nil
	})
	if _, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"description.en": encode("new")}}); !errors.Is(err, ErrConflict) {
		t.Fatal("candidate baseline not rechecked", err)
	}
	after, _ = s.ApplicationConfiguration(a.Key)
	if !reflect.DeepEqual(before, after) || published != 0 || aborted != 2 {
		t.Fatal("CAS conflict published or saved")
	}
}

func TestRestartWithSameSchemaKeepsConfiguration(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.configurationState()
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	after, _ := s.configurationState()
	if !bytes.Equal(encode(before), encode(after)) {
		t.Fatal("same-schema restart changed configuration")
	}
}

func TestConfigurationCommitFailureDoesNotPublish(t *testing.T) {
	var armed atomic.Bool
	// A deferred foreign-key violation makes the commit itself fail after every write.
	s := openTest(t, withBeforeCommit(func(tx *sql.Tx) error {
		if !armed.Load() {
			return nil
		}
		if _, err := tx.Exec(`PRAGMA defer_foreign_keys=ON`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO vendor_config(entity_uid,template_ref,overrides_json,spec_json) VALUES('ffffffffffffffffffffffffffffffff',NULL,'{}','{}')`)
		return err
	}))
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ApplicationConfiguration("openai/codex")
	published, aborted := 0, 0
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		return publicationProbe{&published, &aborted}, nil
	})
	armed.Store(true)
	_, err := s.PatchApplicationConfiguration("openai/codex", ConfigurationPatch{Revision: before.Revision, Set: map[string]json.RawMessage{"description.en": encode("failed commit")}})
	if err == nil {
		t.Fatal("deferred foreign-key commit failure ignored")
	}
	after, err := s.ApplicationConfiguration("openai/codex")
	if err != nil || !reflect.DeepEqual(before, after) || published != 0 || aborted != 1 {
		t.Fatal("failed commit saved or published", after, published, aborted, err)
	}
}

func TestConfigurationBoundCopiesAreDeletableAndIdentityIsSeparate(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Application("openai/codex")
	ref := a.Key
	copied, err := s.CreateConfiguredApplication("openai", ApplicationInput{ID: "copy", Name: a.Name, Provider: a.Provider, BaseURL: a.BaseURL, CacheTTLSeconds: a.CacheTTLSeconds}, &ref)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.ApplicationConfiguration(copied.Key)
	if c.TemplateRef == nil || *c.TemplateRef != ref || copied.BuiltinTemplate {
		t.Fatal("template binding conflated with public identity", c, copied)
	}
	if err = s.PermanentlyDeleteApplication(copied.Key, copied.Revision); err != nil {
		t.Fatal("bound copy protected from deletion", err)
	}
	missing := presets.Embedded()
	missing.Apps = missing.Apps[1:]
	if err = s.ReconcileTemplates(missing); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Application(a.Key)
	if err = s.DeleteApplication(a.Key, a.Revision); !errors.Is(err, ErrBuiltinTemplate) {
		t.Fatal("removed canonical template lost protection", err)
	}
}

func TestDistributionDigestRevisionSeparateFromSpec(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ApplicationConfiguration("openai/codex")
	a, _ := s.Application("openai/codex")
	instructions, _ := s.Instructions(a.UID)
	changed := presets.Embedded()
	for i := range changed.Apps {
		if changed.Apps[i].Key() == "openai/codex" {
			changed.Apps[i].Distribution.UpdatePolicy.En += " Reviewed policy revision."
		}
	}
	if err := s.ReconcileTemplates(changed); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ApplicationConfiguration("openai/codex")
	current, _ := s.Application(a.Key)
	saved, _ := s.Instructions(a.UID)
	if after.Revision != before.Revision+1 || !reflect.DeepEqual(after.TemplateHash, before.TemplateHash) || !reflect.DeepEqual(before.Overrides, after.Overrides) || current.SourceEpoch != a.SourceEpoch || saved.Revision != instructions.Revision {
		t.Fatalf("distribution revision/hash/epoch/doc: %d/%d %v %d/%d %d/%d", before.Revision, after.Revision, reflect.DeepEqual(before.TemplateHash, after.TemplateHash), a.SourceEpoch, current.SourceEpoch, instructions.Revision, saved.Revision)
	}
	if err := s.ReconcileTemplates(changed); err != nil {
		t.Fatal(err)
	}
	again, _ := s.ApplicationConfiguration(a.Key)
	if again.Revision != after.Revision {
		t.Fatal("unchanged distribution incremented revision")
	}
	// A simultaneous user-spec and distribution change still advances only once.
	for i := range changed.Apps {
		if changed.Apps[i].Key() == a.Key {
			changed.Apps[i].Spec.Name.En += " Updated"
			changed.Apps[i].Distribution.UpdatePolicy.En += " Another revision."
		}
	}
	if err := s.ReconcileTemplates(changed); err != nil {
		t.Fatal(err)
	}
	final, _ := s.ApplicationConfiguration(a.Key)
	if final.Revision != after.Revision+1 {
		t.Fatal("double revision increment", final.Revision)
	}
}

// Configuration writes only insert and update rows, so a change that drops a
// persisted entry must fail instead of leaving the row behind.
func TestConfigurationChangeCannotDropPersistedEntries(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	a, err := s.Application("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAdminNotes("app", a.Key, 1, "private"); err != nil {
		t.Fatal(err)
	}
	for name, drop := range map[string]func(*configurationState){
		"application": func(st *configurationState) {
			for i := range st.Applications {
				if st.Applications[i].UID == a.UID {
					st.Applications = append(st.Applications[:i], st.Applications[i+1:]...)
					return
				}
			}
		},
		"admin note": func(st *configurationState) { delete(st.Notes, configKey("App", a.UID)) },
	} {
		t.Run(name, func(t *testing.T) {
			before, _ := s.configurationState()
			err := s.changeConfiguration(func(st *configurationState) error { drop(st); return nil })
			if err == nil {
				t.Fatal("dropping a persisted entry was accepted")
			}
			after, _ := s.configurationState()
			if !bytes.Equal(encode(before), encode(after)) {
				t.Fatal("rejected change modified the configuration")
			}
		})
	}
}
