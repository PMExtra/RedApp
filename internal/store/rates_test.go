package store

import (
	"testing"
	"time"
)

func TestRateWindowValidityIdleAndRestart(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	s := &Store{rates: rates{started: base}}
	if s.ratesAt(base.Add(time.Second))["valid"] != false {
		t.Fatal("startup window considered valid")
	}
	s.sampleAt("upstream_bytes", 100, base.Add(4*time.Second))
	s.sampleAt("upstream_bytes", 50, base.Add(4100*time.Millisecond))
	r := s.ratesAt(base.Add(5 * time.Second))
	if r["valid"] != true || r["window_seconds"] != float64(5) || r["upstream_bytes_per_second"] != float64(30) {
		t.Fatalf("rate %v", r)
	}
	r = s.ratesAt(base.Add(10 * time.Second))
	if r["upstream_bytes_per_second"] != float64(0) || r["valid"] != true {
		t.Fatal("idle window must be observed zero")
	}
	restarted := &Store{rates: rates{started: base.Add(time.Hour)}}
	if restarted.ratesAt(base.Add(time.Hour))["valid"] != false {
		t.Fatal("restart reused window")
	}
}
