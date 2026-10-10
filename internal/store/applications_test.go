package store

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T, options ...Option) *Store {
	t.Helper()
	s, e := Open(t.TempDir(), options...)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = b
	}
	return out
}
func releaseFixture(t *testing.T, s *Store, app, version string) Resource {
	t.Helper()
	r := Resource{AppID: app, Version: version, Key: "linux/binary", SourceURL: "https://example.test/file", SHA256: strings.Repeat("a", 64)}
	if err := s.PutRelease(ReleaseMetadata{AppID: app, Version: version, Raw: []byte(`{"release":1}`), TrustRevision: 1, FetchedAt: time.Now()}, []Resource{r}); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMetadataAtomicityImmutabilityAndApplicationIsolation(t *testing.T) {
	s := openTest(t)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_resources BEFORE INSERT ON resources BEGIN SELECT RAISE(FAIL,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	m := ReleaseMetadata{AppID: "openai/codex", Version: "1.0.0", Raw: []byte(`{}`), TrustRevision: 1, FetchedAt: time.Now()}
	r := Resource{AppID: m.AppID, Version: m.Version, Key: "file", SourceURL: "https://example.test/file", SHA256: strings.Repeat("a", 64)}
	if err := s.PutRelease(m, []Resource{r}); err == nil {
		t.Fatal("injected fault ignored")
	}
	if _, err := s.Release(m.AppID, m.Version); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial metadata", err)
	}
	versions, _ := s.VersionsFor(m.AppID)
	if len(versions) != 0 {
		t.Fatal("partial version history")
	}
	s.db.Exec("DROP TRIGGER reject_resources")
	r = releaseFixture(t, s, m.AppID, m.Version)
	releaseFixture(t, s, "anthropic/claude-code", m.Version)
	r.SHA256 = strings.Repeat("b", 64)
	if err := s.PutRelease(m, []Resource{r}); !errors.Is(err, ErrImmutableRelease) {
		t.Fatal("mutable resource authorized", err)
	}
	before, _ := s.Release(m.AppID, m.Version)
	if string(before.Raw) != `{"release":1}` {
		t.Fatal("failed update changed envelope")
	}
	got, _ := s.VersionsFor("anthropic/claude-code")
	if len(got) != 1 {
		t.Fatal("application history not isolated")
	}
}
func TestGlobalSettingCASRejectsStaleAndMalformedWrites(t *testing.T) {
	s := openTest(t)
	var value map[string]string
	// Every setting exists from creation at revision 1 with an empty document.
	if rev, err := s.ReadSiteSettings(&value); err != nil || rev != 1 || len(value) != 0 {
		t.Fatalf("initial setting = %v, %d, %v; want empty at revision 1", value, rev, err)
	}
	for _, expected := range []int64{0, -1} {
		if _, err := s.SaveSiteSettings(expected, map[string]string{"title": "invalid"}); err == nil || errors.Is(err, ErrConflict) {
			t.Fatalf("revision %d accepted: %v", expected, err)
		}
	}
	if _, err := s.SaveSiteSettings(1, []string{"not an object"}); err == nil {
		t.Fatal("non-object setting accepted")
	}
	rev, err := s.SaveSiteSettings(1, map[string]string{"title": "first"})
	if err != nil || rev != 2 {
		t.Fatalf("first save = %d, %v; want 2", rev, err)
	}
	if _, err = s.SaveSiteSettings(1, map[string]string{"title": "stale"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update = %v; want ErrConflict", err)
	}
	var publicURL map[string]any
	if rev, err = s.ReadPublicURLSetting(&publicURL); err != nil || rev != 1 {
		t.Fatalf("site setting leaked into public URL: %d, %v", rev, err)
	}
	if rev, err = s.ReadSiteSettings(&value); err != nil || rev != 2 || value["title"] != "first" {
		t.Fatalf("stored setting = %v at %d, %v", value, rev, err)
	}
}
func TestGenerationCleanupScopeAndCurrentCannotBeResurrected(t *testing.T) {
	s := openTest(t)
	r := releaseFixture(t, s, "openai/codex", "1.0.0")
	releaseFixture(t, s, "anthropic/claude-code", "1.0.0")
	now := time.Now().UTC().Truncate(time.Second)
	g := Generation{ID: "first", AppID: r.AppID, Version: r.Version, ResourceKey: r.Key, ExpectedSHA256: r.SHA256, Phase: "incomplete", IsCurrent: true, StartedAt: now}
	if err := s.CreateGeneration(g); err != nil {
		t.Fatal(err)
	}
	p := CleanupPreview{ID: "preview", AppID: r.AppID, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute), Selection: []CleanupSelection{{GenerationID: g.ID, Version: r.Version, ResourceKey: r.Key}}}
	if err := s.SaveCleanupPreview(p); err != nil {
		t.Fatal(err)
	}
	g2 := g
	g2.ID = "second"
	if err := s.CreateGeneration(g2); err != nil {
		t.Fatal(err)
	}
	g.Bytes = 10
	if err := s.SaveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetireCleanupPreview("anthropic/claude-code", p.ID, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross app cleanup accepted", err)
	}
	if _, err := s.RetireCleanupPreview(r.AppID, p.ID, now); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Generations()
	for _, v := range all {
		if v.ID == g.ID && v.IsCurrent || v.ID == g2.ID && !v.IsCurrent {
			t.Fatal("cleanup/current generation leaked", all)
		}
	}
	b := Blob{AppID: r.AppID, SHA256: r.SHA256, VerifiedAt: now}
	if err := s.CompleteGeneration(r.AppID, g.ID, b, now, 0); err == nil {
		t.Fatal("retired writer published")
	}
	if err := s.CompleteGeneration(r.AppID, g2.ID, b, now, 0); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteUnreferencedBlob(r.AppID, r.SHA256); err != nil || deleted {
		t.Fatal("referenced blob deleted", err)
	}
	if _, err := s.Blob("anthropic/claude-code", r.SHA256); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("blob ownership shared", err)
	}
}

func TestWALDataSurvivesAbruptProcessExit(t *testing.T) {
	const helper = "REDAPP_STORE_CRASH_DIR"
	if dir := os.Getenv(helper); dir != "" {
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.SaveSiteSettings(1, map[string]string{"title": "kept"}); err != nil {
			t.Fatal(err)
		}
		if err = s.AddFor("openai/codex", "upstream_bytes", 13); err != nil {
			t.Fatal(err)
		}
		if err = s.FlushCounters(); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestWALDataSurvivesAbruptProcessExit$")
	command.Env = append(os.Environ(), helper+"="+dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture: %v %s", err, output)
	}
	info, err := os.Stat(filepath.Join(dir, "state.sqlite-wal"))
	if err != nil || info.Size() == 0 {
		t.Fatal("fixture did not leave committed data in WAL", err)
	}
	before := snapshotFiles(t, dir)
	if err = Preflight(dir); err != nil {
		t.Fatal("valid schema rejected after crash", err)
	}
	after := snapshotFiles(t, dir)
	for name, b := range before {
		if !bytes.Equal(b, after[name]) {
			t.Fatal("immutable preflight touched source", name)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var site map[string]string
	if rev, err := s.ReadSiteSettings(&site); err != nil || rev != 2 || site["title"] != "kept" {
		t.Fatal("committed WAL setting lost", site, rev, err)
	}
	counts, err := s.CountersFor("openai/codex")
	if err != nil || counts["upstream_bytes"] != 13 {
		t.Fatal("committed WAL counter lost", counts, err)
	}
}
