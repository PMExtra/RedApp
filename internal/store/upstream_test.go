package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mirrorFixture(t *testing.T) (*Store, Upstream) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUpstream("DMZ", "https://dmz.example", strings.Repeat("a", 32), time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	return s, u
}

func effectiveSpec(t *testing.T, s *Store, key string) Object {
	t.Helper()
	c, err := s.ApplicationConfiguration(key)
	if err != nil {
		t.Fatal(err)
	}
	return c.Effective
}

func leafString(spec Object, path string) string {
	value, _ := leaf(spec, path)
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestMirroredApplicationFollowsItsUpstream(t *testing.T) {
	s, u := mirrorFixture(t)
	at := time.Unix(1_800_000_100, 0)
	snapshot := MirrorSnapshot{Name: LocalizedText{"Tools", "工具"}, Description: LocalizedText{"Shared tools", "共享工具"}, Instructions: LocalizedText{"Get {{app_url}}/", "下载"}, AutoIndex: true}
	a, err := s.BindApplication(BindRequest{UpstreamUID: u.UID, RemoteKey: "acme/tools", Vendor: "acme", ID: "tools", Provider: "http-cache", NewVendor: &VendorInput{Name: LocalizedText{"Acme", "示例"}}, Snapshot: snapshot, RemoteRevision: "r1", Chain: []string{strings.Repeat("b", 32)}, At: at})
	if err != nil {
		t.Fatal(err)
	}
	if a.Provider != "http-cache" || len(a.BaseURLs) != 1 || a.BaseURLs[0] != "https://dmz.example/acme/tools" || !a.AutoIndex || a.Name.ZhCN != "工具" {
		t.Fatalf("%+v", a)
	}
	if _, err = s.BindApplication(BindRequest{UpstreamUID: u.UID, RemoteKey: "acme/tools", Vendor: "acme", ID: "copy", Provider: "http-cache", Snapshot: snapshot, At: at}); !errors.Is(err, ErrMirrorExists) {
		t.Fatal("bound twice", err)
	}
	b, err := s.Binding(a.Key)
	if err != nil || b.UpstreamUID != u.UID || b.RemoteKey != "acme/tools" || !b.Available || b.Chain[0] != strings.Repeat("b", 32) || b.ErrorCode != "" {
		t.Fatalf("%+v %v", b, err)
	}
	if instance, chain, ok, err := s.MirroredFrom(a.UID); err != nil || !ok || instance != u.InstanceID || len(chain) != 1 {
		t.Fatal("mirror chain", instance, chain, ok, err)
	}
	// The source belongs to the binding; display fields may be overridden per language.
	for _, path := range []string{"base_urls", "source_strategy"} {
		if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{path: json.RawMessage(`["https://elsewhere.example"]`)}}); err == nil {
			t.Fatal("source of a mirror overridden", path)
		}
	}
	c, err := s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Set: map[string]json.RawMessage{"name.en": json.RawMessage(`"Local tools"`), "cache_ttl_seconds": json.RawMessage(`60`)}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Name, snapshot.Instructions.ZhCN = LocalizedText{"Tools 2", "工具 2"}, "下载 2"
	if err = s.UpdateMirror(a.UID, MirrorUpdate{Snapshot: &snapshot, RemoteRevision: "r2", At: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	spec := effectiveSpec(t, s, a.Key)
	if leafString(spec, "name.en") != `"Local tools"` || leafString(spec, "name.zh-CN") != `"工具 2"` || leafString(spec, "instructions.zh-CN") != `"下载 2"` || leafString(spec, "cache_ttl_seconds") != "60" {
		t.Fatalf("local overrides and followed fields mixed up: %v", spec)
	}
	after, _ := s.Application(a.Key)
	if after.Revision <= c.Revision || after.SourceEpoch != a.SourceEpoch {
		t.Fatal("snapshot update must change the revision but not the source", after.Revision, after.SourceEpoch)
	}
	if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: after.Revision, Unset: []string{"name.en"}}); err != nil {
		t.Fatal(err)
	}
	if spec = effectiveSpec(t, s, a.Key); leafString(spec, "name.en") != `"Tools 2"` {
		t.Fatal("reset did not follow the upstream again", spec)
	}
	// A failed check keeps every value; a gone application marks the template missing.
	if err = s.UpdateMirror(a.UID, MirrorUpdate{ErrorCode: "UPSTREAM_UNAVAILABLE", At: at.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if b, _ = s.Binding(a.Key); b.ErrorCode != "UPSTREAM_UNAVAILABLE" || !b.Available || b.RemoteRevision != "r2" {
		t.Fatalf("failed check %+v", b)
	}
	if err = s.UpdateMirror(a.UID, MirrorUpdate{Gone: true, ErrorCode: "APPLICATION_NOT_FOUND", At: at.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.ApplicationConfiguration(a.Key); !cfg.TemplateMissing {
		t.Fatal("gone upstream application not shown as a missing template")
	}
	if b, _ = s.Binding(a.Key); b.Available {
		t.Fatal("gone binding still available")
	}
	// Mirrors leave as independent copies.
	if _, err = s.UpdateUpstream(u.UID, u.Revision, "DMZ", "https://dmz2.example", u.InstanceID); err != nil {
		t.Fatal(err)
	}
	moved, _ := s.Application(a.Key)
	if moved.BaseURLs[0] != "https://dmz2.example/acme/tools" || moved.SourceEpoch != a.SourceEpoch+1 {
		t.Fatalf("moved upstream %+v", moved)
	}
	u, _ = s.Upstream(u.UID)
	if err = s.DeleteUpstream(u.UID, u.Revision); !errors.Is(err, ErrUpstreamInUse) {
		t.Fatal("deleted an upstream in use", err)
	}
	if err = s.PermanentlyDeleteApplication(a.Key, moved.Revision); err != nil {
		t.Fatal(err)
	}
	var templates int
	if err = s.read.QueryRow(`SELECT count(*) FROM template_snapshots WHERE canonical_key=?`, UpstreamTemplateRef(a.UID)).Scan(&templates); err != nil || templates != 0 {
		t.Fatal("template of a deleted mirror kept", templates, err)
	}
	if err = s.DeleteUpstream(u.UID, u.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptedBuiltinApplicationMirrorsAndUnbinds(t *testing.T) {
	s, u := mirrorFixture(t)
	codex, err := s.Application("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchApplicationConfiguration(codex.Key, ConfigurationPatch{Revision: codex.Revision, Set: map[string]json.RawMessage{"name.zh-CN": json.RawMessage(`"本地名称"`), "cache_ttl_seconds": json.RawMessage(`120`)}}); err != nil {
		t.Fatal(err)
	}
	codex, _ = s.Application(codex.Key)
	snapshot := MirrorSnapshot{Name: LocalizedText{"Codex CLI", "Codex CLI 中文"}, Description: LocalizedText{"Agent", "代理"}}
	request := BindRequest{UpstreamUID: u.UID, RemoteKey: "openai/codex", Vendor: "openai", ID: "codex", Provider: "codex", Snapshot: snapshot, At: time.Unix(1_800_000_200, 0)}
	if _, err = s.BindApplication(request); !errors.Is(err, ErrDirectoryExists) {
		t.Fatal("an existing application was taken over without adopting it", err)
	}
	request.Adopt, request.Revision = true, codex.Revision-1
	if _, err = s.BindApplication(request); !errors.Is(err, ErrConflict) {
		t.Fatal("stale adoption", err)
	}
	request.Revision = codex.Revision
	adopted, err := s.BindApplication(request)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.BaseURL != "https://dmz.example/openai/codex" || adopted.SourceEpoch != codex.SourceEpoch+1 || adopted.CacheTTLSeconds != 120 || adopted.Name.ZhCN != "Codex CLI 中文" {
		t.Fatalf("adoption must follow the upstream and keep local settings: %+v", adopted)
	}
	snapshotView, err := s.DirectoryConfigurationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if ref, ok := snapshotView.TemplateBindings[adopted.UID]; ok && ref != "openai/codex" {
		t.Fatal("a mirror must keep the reviewed release contract", ref)
	}
	unbound, err := s.UnbindApplication(adopted.Key, adopted.Revision)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.ApplicationConfiguration(unbound.Key)
	if cfg.TemplateRef == nil || *cfg.TemplateRef != "openai/codex" || unbound.BaseURL != "https://dmz.example/openai/codex" || unbound.Name.ZhCN != "Codex CLI 中文" {
		t.Fatalf("unbound application must keep its values under its built-in template: %+v %+v", cfg.TemplateRef, unbound)
	}
	if _, err = s.Binding(unbound.Key); !errors.Is(err, ErrMirrorNotFound) {
		t.Fatal("binding kept", err)
	}
}

// A mirror's template belongs to its binding: copies are independent, a
// linked export never references it, and re-importing a package that contains
// a mirrored key only fails when it would update the mirror.
func TestMirroredApplicationCopiesAndImports(t *testing.T) {
	s, u := mirrorFixture(t)
	snapshot := MirrorSnapshot{Name: LocalizedText{"Tools", "工具"}}
	a, err := s.BindApplication(BindRequest{UpstreamUID: u.UID, RemoteKey: "acme/tools", Vendor: "acme", ID: "tools", Provider: "http-cache", NewVendor: &VendorInput{Name: LocalizedText{"Acme", "示例"}}, Snapshot: snapshot, At: time.Unix(1_800_000_000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CopyApplication(a.Key, CopyApplicationInput{SourceUID: a.UID, SourceRevision: a.Revision, TargetVendor: "acme", TargetID: "linked", Mode: "linked"}); err == nil {
		t.Fatal("a linked copy shares the mirror's template")
	}
	copied, err := s.CopyApplication(a.Key, CopyApplicationInput{SourceUID: a.UID, SourceRevision: a.Revision, TargetVendor: "acme", TargetID: "copy", Mode: "independent"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ExportConfiguration(ExportOptions{Selection: []ExportSelection{{Kind: "App", Key: a.Key}}, Mode: "linked"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Documents {
		if d.Template != nil && IsUpstreamTemplateRef(*d.Template) {
			t.Fatal("export references a mirror template")
		}
	}
	if _, err = s.PreviewConfigurationImport(p.Documents, nil); err != nil {
		t.Fatal("re-importing a mirrored key that is skipped must work", err)
	}
	if _, err = s.PreviewConfigurationImport(p.Documents, []ImportChoice{{Kind: "App", Key: a.Key, Action: "update"}}); err == nil {
		t.Fatal("an import updated a mirror")
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplicationConfiguration(copied.Key); err != nil {
		t.Fatal("the copy depends on the deleted mirror", err)
	}
	if _, err = s.DirectoryConfigurationSnapshot(); err != nil {
		t.Fatal(err)
	}
}
