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
	totals  map[string]int64
}

func (s *Store) Rates() map[string]any {
	s.rates.mu.Lock()
	defer s.rates.mu.Unlock()
	now := time.Now()
	out := map[string]any{"sampled_at": now.UTC(), "scope": "当前进程最近五秒实际传输字节"}
	for _, name := range []string{"upstream_bytes", "downstream_bytes"} {
		samples := s.rates.samples[name]
		var bps float64
		if len(samples) > 0 {
			first := samples[0]
			if now.Sub(first.Time) > 0 && now.Sub(samples[len(samples)-1].Time) < 5*time.Second {
				bps = float64(s.rates.totals[name]-first.Bytes) / now.Sub(first.Time).Seconds()
			}
		}
		out[name+"_per_second"] = bps
	}
	return out
}
func (s *Store) sample(name string, n int64) {
	if name != "upstream_bytes" && name != "downstream_bytes" {
		return
	}
	s.rates.mu.Lock()
	defer s.rates.mu.Unlock()
	if s.rates.samples == nil {
		s.rates.samples = map[string][]rateSample{}
		s.rates.totals = map[string]int64{}
	}
	now := time.Now()
	samples := s.rates.samples[name]
	if len(samples) == 0 {
		samples = append(samples, rateSample{now, 0})
	}
	s.rates.totals[name] += n
	if len(samples) < 2 || now.Sub(samples[len(samples)-1].Time) > 200*time.Millisecond {
		samples = append(samples, rateSample{now, s.rates.totals[name]})
	} else {
		samples[len(samples)-1] = rateSample{now, s.rates.totals[name]}
	}
	for len(samples) > 2 && now.Sub(samples[1].Time) > 5*time.Second {
		samples = samples[1:]
	}
	s.rates.samples[name] = samples
}
