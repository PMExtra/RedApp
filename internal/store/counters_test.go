package store

import (
	"strings"
	"sync"
	"testing"
)

func persistedCounter(t *testing.T, s *Store, scope, app, name string) int64 {
	t.Helper()
	var n int64
	s.DB.QueryRow(`SELECT coalesce(sum(value),0) FROM metric_counters WHERE scope=? AND app_id=? AND metric=?`, scope, app, "counters."+name).Scan(&n)
	return n
}

func TestCountersCoalesceInMemoryAndSampleRatesImmediately(t *testing.T) {
	s := openTest(t)
	releaseFixture(t, s, "openai/codex", "1.0.0")
	for i := 0; i < 100; i++ {
		if err := s.AddFor("openai/codex", "downstream_bytes", 10); err != nil {
			t.Fatal(err)
		}
		if err := s.AddVersion("openai/codex", "1.0.0", 1, 10); err != nil {
			t.Fatal(err)
		}
	}
	if persistedCounter(t, s, "global", "", "downstream_bytes") != 0 {
		t.Fatal("hot-path add wrote synchronously")
	}
	if rate := s.Rates()["downstream_bytes_per_second"].(float64); rate <= 0 {
		t.Fatal("live rate did not observe buffered bytes", rate)
	}
	if err := s.FlushCounters(); err != nil {
		t.Fatal(err)
	}
	if persistedCounter(t, s, "global", "", "downstream_bytes") != 1000 || persistedCounter(t, s, "app", "openai/codex", "downstream_bytes") != 1000 {
		t.Fatal("coalesced counters not persisted")
	}
	stats, _ := s.VersionStats("openai/codex")
	if v := stats["1.0.0"]; v.ArtifactRequests != 100 || v.DownstreamBytes != 1000 {
		t.Fatal("version counters", v)
	}
	if err := s.FlushCounters(); err != nil {
		t.Fatal(err)
	}
	if persistedCounter(t, s, "global", "", "downstream_bytes") != 1000 {
		t.Fatal("flush applied increments twice")
	}
}

func TestCounterReadsObserveBufferedIncrements(t *testing.T) {
	s := openTest(t)
	releaseFixture(t, s, "openai/codex", "1.0.0")
	s.AddFor("openai/codex", "upstream_bytes", 5)
	s.AddVersion("openai/codex", "1.0.0", 2, 3)
	global, _ := s.Counters()
	app, _ := s.CountersFor("openai/codex")
	page, _ := s.VersionPage("openai/codex", "", 10)
	if global["upstream_bytes"] != 5 || app["upstream_bytes"] != 5 || len(page) != 1 || page[0].ArtifactRequests != 2 || page[0].DownstreamBytes != 3 {
		t.Fatal(global, app, page)
	}
}

func TestCounterValidationHappensAtAddTime(t *testing.T) {
	s := openTest(t)
	if s.Add("reuse_requests", 1) == nil || s.Add("requests", -1) == nil || s.AddFor("", "requests", 1) == nil || s.AddVersion("openai/codex", "1.0.0", -1, 0) == nil {
		t.Fatal("invalid counter accepted")
	}
	if err := s.FlushCounters(); err != nil {
		t.Fatal(err)
	}
	if counters, _ := s.Counters(); len(counters) != 0 {
		t.Fatal("rejected increments buffered", counters)
	}
}

func TestCountersForMissingApplicationsOrVersionsAreDiscarded(t *testing.T) {
	s := openTest(t)
	missing := "app/" + strings.Repeat("c", 32)
	s.AddFor(missing, "download_success", 3)
	s.AddVersion("openai/codex", "9.9.9", 1, 1)
	s.Add("requests", 1)
	if err := s.FlushCounters(); err != nil {
		t.Fatal("missing owner blocked the flush", err)
	}
	if persistedCounter(t, s, "global", "", "download_success") != 0 || persistedCounter(t, s, "app", missing, "download_success") != 0 {
		t.Fatal("counter for missing application recorded")
	}
	if persistedCounter(t, s, "global", "", "requests") != 1 {
		t.Fatal("unrelated counter lost")
	}
}

func TestFailedCounterFlushRetainsVersionIncrements(t *testing.T) {
	s := openTest(t)
	releaseFixture(t, s, "openai/codex", "1.0.0")
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_version_counter BEFORE UPDATE ON app_versions BEGIN SELECT RAISE(FAIL,'version fault'); END`); err != nil {
		t.Fatal(err)
	}
	var reported []error
	stop := s.StartCounterFlush(1<<62, func(err error) { reported = append(reported, err) })
	s.AddFor("openai/codex", "downstream_bytes", 4)
	s.AddVersion("openai/codex", "1.0.0", 0, 4)
	s.SettleCounters()
	if len(reported) != 1 || persistedCounter(t, s, "global", "", "downstream_bytes") != 0 {
		t.Fatal("failed flush not reported or partially committed", reported)
	}
	s.AddVersion("openai/codex", "1.0.0", 0, 6)
	s.DB.Exec("DROP TRIGGER fail_version_counter")
	stop()
	stats, _ := s.VersionStats("openai/codex")
	if stats["1.0.0"].DownstreamBytes != 10 || persistedCounter(t, s, "app", "openai/codex", "downstream_bytes") != 4 {
		t.Fatal("retained increments not retried", stats)
	}
}

func TestCloseFlushesBufferedCounters(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.AddFor("openai/codex", "upstream_bytes", 21)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if counters, _ := s.CountersFor("openai/codex"); counters["upstream_bytes"] != 21 {
		t.Fatal("buffered counters lost on close", counters)
	}
}

func TestConcurrentCounterAddsAndFlushesAreExact(t *testing.T) {
	s := openTest(t)
	releaseFixture(t, s, "openai/codex", "1.0.0")
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				s.AddFor("openai/codex", "upstream_bytes", 1)
				s.AddVersion("openai/codex", "1.0.0", 1, 0)
				if i%50 == 0 {
					s.SettleCounters()
				}
			}
		}()
	}
	wg.Wait()
	counters, _ := s.CountersFor("openai/codex")
	stats, _ := s.VersionStats("openai/codex")
	if counters["upstream_bytes"] != 1600 || stats["1.0.0"].ArtifactRequests != 1600 {
		t.Fatal(counters, stats)
	}
}
