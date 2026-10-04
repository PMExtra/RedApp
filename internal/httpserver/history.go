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
	for _, count := range status["application_version_counts"].(map[string]int64) {
		values["versions.total"] += float64(count)
	}
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
			observations := []history.Observation{{Scope: "global", Metrics: status["metrics"].([]history.Metric)}}
			for _, entry := range s.Registry.Entries() {
				appStatus, e := s.appStatus(entry.Descriptor.ID, "")
				if e != nil {
					err = e
					break
				}
				observations = append(observations, history.Observation{Scope: "app", AppID: entry.MetricsID(), Metrics: appStatus["metrics"].([]history.Metric)})
			}
			if err == nil {
				err = s.History.RecordScoped(at, observations)
			}
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

func applicationMetrics(status map[string]any) []history.Metric {
	values := map[string]float64{}
	for key, value := range status["counters"].(map[string]int64) {
		values["counters."+key] = float64(value)
	}
	for _, v := range status["resources"].([]download.View) {
		values["resources.total"]++
		values["resources.readers"] += float64(v.Readers)
		if v.Current {
			values["resources.current"]++
		}
		if v.Retired {
			values["resources.retired"]++
		}
		if v.ActiveWriter {
			values["resources.active_writers"]++
		}
		values["resources."+v.State]++
	}
	versionCount, hasVersions := status["version_count"].(int64)
	if hasVersions {
		values["versions.total"] = float64(versionCount)
	}
	out := []history.Metric{}
	for _, d := range history.AppDefinitions() {
		value := values[d.Key]
		metric := history.Metric{Definition: d, Value: &value}
		if d.Key == "versions.total" && !hasVersions {
			metric.Value = nil
		}
		out = append(out, metric)
	}
	return out
}
