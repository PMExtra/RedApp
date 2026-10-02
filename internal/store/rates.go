package store

import (
	"sync"
	"time"
)

type rateSample struct {
	Time  time.Time
	Bytes int64
}
type rates struct {
	mu      sync.Mutex
	samples map[string][]rateSample
	started time.Time
}

func (s *Store) Rates() map[string]any { return s.ratesAt(time.Now()) }
func (s *Store) ratesAt(now time.Time) map[string]any {
	s.rates.mu.Lock()
	defer s.rates.mu.Unlock()
	observed := now.Sub(s.rates.started).Seconds()
	if observed > 5 {
		observed = 5
	}
	if observed < 0 {
		observed = 0
	}
	out := map[string]any{"sampled_at": now.UTC(), "scope": "Artifact HTTP body bytes in this process over up to five seconds: upstream identity payload consumed, downstream payload accepted before external compression; excludes HTTP/TLS overhead", "window_seconds": observed, "valid": observed == 5}
	for _, name := range []string{"upstream_bytes", "downstream_bytes"} {
		var total int64
		for _, sample := range s.rates.samples[name] {
			if sample.Time.After(now.Add(-5*time.Second)) && !sample.Time.After(now) {
				total += sample.Bytes
			}
		}
		var bps float64
		if observed > 0 {
			bps = float64(total) / observed
		}
		out[name+"_per_second"] = bps
	}
	return out
}
func (s *Store) sample(name string, n int64) { s.sampleAt(name, n, time.Now()) }
func (s *Store) sampleAt(name string, n int64, now time.Time) {
	if name != "upstream_bytes" && name != "downstream_bytes" {
		return
	}
	s.rates.mu.Lock()
	defer s.rates.mu.Unlock()
	if s.rates.samples == nil {
		s.rates.samples = map[string][]rateSample{}
	}
	samples := s.rates.samples[name]
	samples = append(samples, rateSample{now, n})
	for len(samples) > 0 && !samples[0].Time.After(now.Add(-5*time.Second)) {
		samples = samples[1:]
	}
	s.rates.samples[name] = samples
}
