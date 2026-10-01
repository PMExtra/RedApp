package httpserver

import (
	"context"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"time"
)

func globalMetrics(status map[string]any, started time.Time) []history.Metric {
	values := map[string]float64{}
	switchNumber := func(value any) float64 {
		switch n := value.(type) {
		case int:
			return float64(n)
		case int64:
			return float64(n)
		case uint64:
			return float64(n)
		case float64:
			return n
		}
		return 0
	}
	for key, value := range status["disk"].(map[string]any) {
		values["disk."+key] = switchNumber(value)
	}
	for key, value := range status["counters"].(map[string]int64) {
		if _, ok := history.Find("counters." + key); ok {
			values["counters."+key] = float64(value)
		}
	}
	rates := status["rates"].(map[string]any)
	for _, key := range []string{"upstream_bytes_per_second", "downstream_bytes_per_second"} {
		values["rates."+key] = switchNumber(rates[key])
	}
	values["runtime.memory_bytes"] = switchNumber(status["memory_bytes"])
	values["runtime.goroutines"] = switchNumber(status["goroutines"])
	if !started.IsZero() {
		values["runtime.uptime_seconds"] = time.Since(started).Seconds()
	}
	for _, view := range status["resources"].([]download.View) {
		values["resources.total"]++
		values["resources.readers"] += float64(view.Readers)
		if view.Current {
			values["resources.current"]++
		}
		if view.Retired {
			values["resources.retired"]++
		}
		if view.ActiveWriter {
			values["resources.active_writers"]++
		}
		if _, ok := history.Find("resources." + view.State); ok {
			values["resources."+view.State]++
		}
	}
	values["versions.total"] = float64(len(status["versions"].(map[string]string)))
	values["events.recent_total"] = float64(len(status["events"].([]map[string]any)))
	metrics := []history.Metric{}
	validRate, _ := rates["valid"].(bool)
	for _, definition := range history.Definitions() {
		value := values[definition.Key]
		metric := history.Metric{Definition: definition, Value: &value}
		if definition.Kind == "rate" {
			if validRate {
				metric.ObservedSeconds = 5
			} else {
				metric.Value = nil
			}
		}
		metrics = append(metrics, metric)
	}
	return metrics
}

// SampleHistory records immediately, then once per minute. The same owner
// performs closed-hour aggregation and retention, independent of admin traffic.
func (s *Server) SampleHistory(ctx context.Context, onError func(error)) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	sample := func() {
		at := time.Now().UTC()
		status, err := s.status("")
		if err == nil {
			at = status["sampled_at"].(time.Time)
			err = s.History.Record(at, status["metrics"].([]history.Metric))
		} else if maintenanceErr := s.History.Maintain(at); maintenanceErr != nil {
			onError(maintenanceErr)
		}
		if err != nil {
			onError(err)
		}
	}
	sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			sample()
		}
	}
}
