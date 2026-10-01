package history

import (
	"errors"
	"github.com/PMExtra/RedApp/internal/store"
	"testing"
	"time"
)

func setup(t *testing.T) (*History, *store.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	h, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}
func observation(key string, value float64) Metric {
	d, _ := Find(key)
	m := Metric{Definition: d, Value: &value}
	if d.Kind == "rate" {
		m.ObservedSeconds = 5
	}
	return m
}
func point(t *testing.T, h *History, key, window string, now, at time.Time) Point {
	t.Helper()
	s, err := h.Query(key, window, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range s.Points {
		if p.Time == at.Unix() {
			return p
		}
	}
	t.Fatalf("point missing %s", at)
	return Point{}
}
func value(t *testing.T, actual *float64, expected float64) {
	t.Helper()
	if actual == nil || *actual != expected {
		t.Fatalf("value=%v expected=%v", actual, expected)
	}
}
func TestUTCAndGaugeAggregationPartialResolution(t *testing.T) {
	h, _ := setup(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= 60; i++ {
		if err := h.Record(base.Add(time.Duration(i)*time.Minute), []Metric{observation("disk.cache_bytes", float64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	now := base.Add(time.Hour + 30*time.Second)
	p := point(t, h, "disk.cache_bytes", "7d", now, base)
	value(t, p.Min, 0)
	value(t, p.Max, 59)
	value(t, p.Avg, 29.5)
	value(t, p.Last, 59)
	if p.Count != 60 || p.Partial || p.Incomplete {
		t.Fatalf("closed hour %+v", p)
	}
	open := point(t, h, "disk.cache_bytes", "7d", now, base.Add(time.Hour))
	value(t, open.Value, 60)
	if !open.Partial || !open.Incomplete || open.Count != 1 {
		t.Fatalf("partial %+v", open)
	}
	for _, window := range []string{"24h", "7d", "30d"} {
		series, err := h.Query("disk.cache_bytes", window, now)
		if err != nil {
			t.Fatal(err)
		}
		expected := int64(3600)
		if window == "24h" {
			expected = 60
		}
		if series.ResolutionSeconds != expected {
			t.Fatal("wrong resolution")
		}
		for _, p := range series.Points {
			if p.Time%expected != 0 {
				t.Fatal("non-UTC bucket")
			}
		}
	}
	zone := time.FixedZone("offset", 9*3600)
	local := time.Date(2026, 9, 2, 8, 59, 0, 0, zone)
	if err := h.Record(local, []Metric{observation("disk.free_bytes", 77)}); err != nil {
		t.Fatal(err)
	}
	if err := h.Record(local.Add(time.Minute), []Metric{observation("disk.free_bytes", 88)}); err != nil {
		t.Fatal(err)
	}
	p = point(t, h, "disk.free_bytes", "7d", local.Add(time.Minute), local.UTC().Truncate(time.Hour))
	value(t, p.Last, 77)
}
func TestCountersRestartResetGapAndNoAveraging(t *testing.T) {
	h, db := setup(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	key := "counters.upstream_bytes"
	for i := 0; i <= 60; i++ {
		if err := h.Record(base.Add(time.Duration(i)*time.Minute), []Metric{observation(key, float64(100+20*i))}); err != nil {
			t.Fatal(err)
		}
	}
	p := point(t, h, key, "7d", base.Add(time.Hour), base)
	if p.Avg != nil || p.Min != nil || p.Max != nil || p.DeltaCount != 59 {
		t.Fatalf("counter averaged %+v", p)
	}
	value(t, p.Last, 1280)
	value(t, p.Delta, 1180)
	if p.ObservedSeconds != 59*60 {
		t.Fatal("counter duration")
	}
	record := func(min int, n float64) {
		t.Helper()
		if err := h.Record(base.Add(time.Duration(min)*time.Minute), []Metric{observation(key, n)}); err != nil {
			t.Fatal(err)
		}
	}
	record(61, 5)
	record(62, 9)
	restarted, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	h = restarted
	record(63, 15)
	record(66, 20)
	record(67, 21)
	now := base.Add(67 * time.Minute)
	for _, minute := range []int{0, 61, 63, 66} {
		p := point(t, h, key, "24h", now, base.Add(time.Duration(minute)*time.Minute))
		if p.Delta != nil {
			t.Fatalf("invented reset/restart/gap delta at %d", minute)
		}
	}
	value(t, point(t, h, key, "24h", now, base.Add(62*time.Minute)).Delta, 4)
	value(t, point(t, h, key, "24h", now, base.Add(67*time.Minute)).Delta, 1)
	missing := point(t, h, key, "24h", now, base.Add(64*time.Minute))
	if missing.Value != nil || missing.Count != 0 || !missing.Incomplete {
		t.Fatal("gap filled with zero")
	}
}
func TestRetentionAggregatesBeforeDeletionAndIsIdempotent(t *testing.T) {
	h, db := setup(t)
	now := time.Date(2026, 9, 30, 12, 35, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour).Truncate(time.Hour)
	// A stopped process may leave unaggregated raw observations beyond 24h.
	if _, err := db.DB.Exec("INSERT INTO metric_samples VALUES(?,?,?,?,?,?,?)", "disk.cache_bytes", old.Unix(), old.Unix(), "old", 123, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.Maintain(now); err != nil {
		t.Fatal(err)
	}
	var raw int
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_samples").Scan(&raw)
	if raw != 0 {
		t.Fatal("raw retention failed")
	}
	value(t, point(t, h, "disk.cache_bytes", "7d", now, old).Last, 123)
	for i := 0; i < 2; i++ {
		if err := h.Maintain(now); err != nil {
			t.Fatal(err)
		}
	}
	value(t, point(t, h, "disk.cache_bytes", "7d", now, old).Last, 123)
	if err := h.Maintain(now.Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	var hours int
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_hours").Scan(&hours)
	if hours != 0 {
		t.Fatal("hour retention failed")
	}
}
func TestAtomicFailureCannotLoseUnaggregatedRaw(t *testing.T) {
	h, db := setup(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	db.DB.Exec("INSERT INTO metric_samples VALUES(?,?,?,?,?,?,?)", "disk.cache_bytes", old.Unix(), old.Unix(), "old", 42, nil, 0)
	db.DB.Exec(`CREATE TRIGGER fail_cleanup BEFORE DELETE ON metric_samples BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if err := h.Maintain(now); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	var raw, hours int
	var watermark int64
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_samples").Scan(&raw)
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_hours").Scan(&hours)
	db.DB.QueryRow("SELECT aggregated_before FROM metric_history_state").Scan(&watermark)
	if raw != 1 || hours != 0 || watermark != 0 {
		t.Fatalf("failed transaction changed state raw=%d hours=%d watermark=%d", raw, hours, watermark)
	}
	db.DB.Exec("DROP TRIGGER fail_cleanup")
	if err := h.Maintain(now); err != nil {
		t.Fatal(err)
	}
	value(t, point(t, h, "disk.cache_bytes", "7d", now, old).Last, 42)
}
func TestRatesAndCatalogValidation(t *testing.T) {
	h, _ := setup(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	key := "rates.upstream_bytes_per_second"
	h.Record(base, []Metric{observation(key, 2)})
	h.Record(base.Add(time.Minute), []Metric{observation(key, 4)})
	h.Maintain(base.Add(time.Hour))
	p := point(t, h, key, "7d", base.Add(time.Hour), base)
	value(t, p.Avg, 3)
	if p.Count != 2 || p.ObservedSeconds != 10 || !p.Incomplete {
		t.Fatal("rate coverage lost")
	}
	if len(Definitions()) != 43 {
		t.Fatal("catalog changed unexpectedly")
	}
	seen := map[string]bool{}
	for _, d := range Definitions() {
		if seen[d.Key] {
			t.Fatal("duplicate catalog key")
		}
		seen[d.Key] = true
	}
	if _, err := h.Query("version:any", "7d", base); err == nil {
		t.Fatal("high cardinality accepted")
	}
	if _, err := h.Query(key, "bad", base); err == nil {
		t.Fatal("invalid range accepted")
	}
	if err := h.Record(base.Add(2*time.Hour), []Metric{{Definition: Definition{Key: "resource:any"}, Value: new(float64)}}); err == nil {
		t.Fatal("unknown metric accepted")
	}
	bad := observation(key, 10)
	bad.ObservedSeconds = 1
	if err := h.Record(base.Add(2*time.Hour), []Metric{bad}); err == nil {
		t.Fatal("partial startup rate treated as full window")
	}
	if err := h.Record(base, []Metric{observation("disk.cache_bytes", 1)}); err == nil {
		t.Fatal("clock rollback ignored")
	}
}
func TestDuplicateMinuteIsIdempotentAndPersistenceSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := Open(db)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err = h.Record(base, []Metric{observation("disk.cache_bytes", 5)}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_samples").Scan(&n)
	if n != 1 {
		t.Fatal("duplicate sampling")
	}
	db.DB.Close()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	h, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	value(t, point(t, h, "disk.cache_bytes", "24h", base, base).Last, 5)
	if err = h.Record(base.Add(time.Minute), []Metric{observation("disk.cache_bytes", -1)}); err == nil {
		t.Fatal("negative metric accepted")
	}
	if err = h.Record(base.Add(time.Minute), []Metric{{Definition: Definition{Key: "disk.cache_bytes"}}}); err != nil && !errors.Is(err, nil) {
		t.Fatal(err)
	}
}

func TestQueryIncludesJustClosedUncommittedHourWithoutWriting(t *testing.T) {
	h, db := setup(t)
	base := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		if err := h.Record(base.Add(time.Duration(i)*time.Minute), []Metric{observation("disk.cache_bytes", float64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	now := base.Add(time.Hour + 10*time.Second)
	p := point(t, h, "disk.cache_bytes", "7d", now, base)
	value(t, p.Avg, 29.5)
	value(t, p.Last, 59)
	if p.Partial || p.Count != 60 {
		t.Fatal("just-closed hour lost")
	}
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM metric_hours").Scan(&count)
	if count != 0 {
		t.Fatal("read query mutated aggregates")
	}
}

func TestCounterDoesNotBridgeMissingUTCMinute(t *testing.T) {
	h, _ := setup(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	key := "counters.upstream_bytes"
	h.Record(base.Add(59*time.Second), []Metric{observation(key, 100)})
	h.Record(base.Add(2*time.Minute+time.Second), []Metric{observation(key, 110)})
	now := base.Add(3*time.Minute + time.Second)
	h.Record(now, []Metric{observation(key, 120)})
	if point(t, h, key, "24h", now, base.Add(2*time.Minute)).Delta != nil {
		t.Fatal("delta crossed a missing UTC minute")
	}
	value(t, point(t, h, key, "24h", now, base.Add(3*time.Minute)).Delta, 10)
}
