package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/identity"
)

// resourceViews is the shared status/listing boundary. The GeneralHttp cache can
// append its owned snapshots here without changing metric ownership or callers.
func (s *Server) resourceViews() ([]download.View, error) {
	views := []download.View{}
	if s.downloads != nil {
		views = s.downloads.Snapshot()
	}
	if s.httpCache != nil {
		httpViews, err := s.httpCache.Snapshot()
		if err != nil {
			return nil, err
		}
		views = append(views, httpViews...)
	}
	return views, nil
}

func (s *Server) publicScopes() map[string]string {
	scopes := map[string]string{}
	for _, entry := range s.registry.AllEntries() {
		scopes[entry.MetricsID()] = entry.Descriptor.ID
	}
	return scopes
}

func publicScope(scope string, scopes map[string]string) string {
	if uid, _, ok := identity.ParseStorageID(scope); ok {
		return scopes[identity.MetricsID(uid)]
	}
	if public, ok := scopes[scope]; ok {
		return public
	}
	if uid, ok := strings.CutPrefix(scope, "app/"); ok && identity.ValidUID(uid) {
		return "" // A private identity must never become a public fallback label.
	}
	return scope
}

// metricDTO is one Metric of the specification.
type metricDTO struct {
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	Kind            string   `json:"kind"`
	Unit            string   `json:"unit"`
	Group           string   `json:"group"`
	Value           *float64 `json:"value"`
	ObservedSeconds float64  `json:"observed_seconds"`
}

type globalStatusDTO struct {
	SampledAt time.Time   `json:"sampled_at"`
	StartedAt time.Time   `json:"started_at"`
	Metrics   []metricDTO `json:"metrics"`
}

type appStatusDTO struct {
	SampledAt time.Time   `json:"sampled_at"`
	Metrics   []metricDTO `json:"metrics"`
}

func metricDocuments(metrics []history.Metric) []metricDTO {
	out := make([]metricDTO, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, metricDTO{Key: m.Key, Label: m.Label, Kind: m.Kind, Unit: m.Unit, Group: m.Group, Value: m.Value, ObservedSeconds: m.ObservedSeconds})
	}
	return out
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	at, metrics, err := s.globalMetrics()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeOK(w, globalStatusDTO{SampledAt: at, StartedAt: s.started.UTC(), Metrics: metricDocuments(metrics)})
}

// metricsApp resolves an application that records metrics: HTTP cache and
// release applications, including disabled and deleted ones.
func (s *Server) metricsApp(w http.ResponseWriter, r *http.Request) (application.Entry, bool) {
	return s.managedAppWith(w, r, application.HttpCache, application.Codex, application.ClaudeCode)
}

func (s *Server) getAppStatus(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.metricsApp(w, r)
	if !ok {
		return
	}
	at := time.Now().UTC()
	metrics, err := s.appMetrics(entry)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	writeOK(w, appStatusDTO{SampledAt: at, Metrics: metricDocuments(metrics)})
}

// globalMetrics computes all global catalog metrics now. It walks the data
// directory for disk usage.
func (s *Server) globalMetrics() (time.Time, []history.Metric, error) {
	views, err := s.resourceViews()
	if err != nil {
		return time.Time{}, nil, err
	}
	values := map[string]float64{}
	if err = s.diskMetrics(views, values); err != nil {
		return time.Time{}, nil, err
	}
	counters, err := s.store.Counters()
	if err != nil {
		return time.Time{}, nil, err
	}
	for key, value := range counters {
		if _, ok := history.Find("counters." + key); ok {
			values["counters."+key] = float64(value)
		}
	}
	rates := s.store.Rates()
	for _, key := range []string{"upstream_bytes_per_second", "downstream_bytes_per_second"} {
		values["rates."+key] = number(rates[key])
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	values["runtime.memory_bytes"] = float64(mem.Alloc)
	values["runtime.goroutines"] = float64(runtime.NumGoroutine())
	if !s.started.IsZero() {
		values["runtime.uptime_seconds"] = time.Since(s.started).Seconds()
	}
	resourceMetrics(views, values)
	for _, entry := range s.registry.Entries() {
		if entry.Protocol == nil {
			continue
		}
		count, err := s.store.VersionCount(entry.StorageID())
		if err != nil {
			return time.Time{}, nil, err
		}
		values["versions.total"] += float64(count)
	}
	validRate, _ := rates["valid"].(bool)
	metrics := []history.Metric{}
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
	return time.Now().UTC(), metrics, nil
}

func number(value any) float64 {
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

// diskMetrics fills the disk.* metrics: allocated blocks of the data directory
// classified by resource state, logical sizes and free file system space.
func (s *Server) diskMetrics(views []download.View, values map[string]float64) error {
	var complete, temp, pending, total, logical, allocatedCache, allocatedTemp, allocatedPending int64
	classes := map[string]string{}
	fileBytes := map[string]int64{}
	for _, v := range views {
		fileBytes[v.Path] = v.Bytes
		if v.Retired {
			if classes[v.Path] == "" {
				classes[v.Path] = "pending"
			}
		} else if v.State == "complete" {
			classes[v.Path] = "cache"
		} else if classes[v.Path] != "cache" {
			classes[v.Path] = "temporary"
		}
	}
	for path, size := range fileBytes {
		switch classes[path] {
		case "pending":
			pending += size
		case "cache":
			complete += size
		case "temporary":
			temp += size
		}
	}
	err := filepath.WalkDir(s.dataDir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		allocated := info.Size()
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			allocated = st.Blocks * 512
		}
		total += allocated
		if !d.IsDir() {
			logical += info.Size()
		}
		switch classes[path] {
		case "cache":
			allocatedCache += allocated
		case "temporary":
			allocatedTemp += allocated
		case "pending":
			allocatedPending += allocated
		}
		return nil
	})
	if err != nil {
		return err
	}
	var disk syscall.Statfs_t
	if err = syscall.Statfs(s.dataDir, &disk); err != nil {
		return err
	}
	for key, value := range map[string]int64{"used_bytes": total, "logical_bytes": logical, "allocated_cache_bytes": allocatedCache, "allocated_temporary_bytes": allocatedTemp, "allocated_pending_bytes": allocatedPending, "cache_bytes": complete, "temporary_bytes": temp, "pending_bytes": pending, "other_bytes": total - allocatedCache - allocatedTemp - allocatedPending} {
		values["disk."+key] = float64(value)
	}
	values["disk.free_bytes"] = float64(disk.Bavail * uint64(disk.Bsize))
	return nil
}

// resourceMetrics adds the resources.* metrics of views.
func resourceMetrics(views []download.View, values map[string]float64) {
	for _, view := range views {
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
}

// appMetrics computes the application catalog metrics of entry. versions.total
// is unknown (null) for applications without release versions.
func (s *Server) appMetrics(entry application.Entry) ([]history.Metric, error) {
	all, err := s.resourceViews()
	if err != nil {
		return nil, err
	}
	views := make([]download.View, 0)
	for _, v := range all {
		if v.Resource.MetricScope() == entry.MetricsID() {
			views = append(views, v)
		}
	}
	values := map[string]float64{}
	resourceMetrics(views, values)
	hasVersions := entry.Protocol != nil
	if hasVersions {
		count, err := s.store.VersionCount(entry.StorageID())
		if err != nil {
			return nil, err
		}
		values["versions.total"] = float64(count)
	}
	counters, err := s.store.CountersFor(entry.MetricsID())
	if err != nil {
		return nil, err
	}
	for key, value := range counters {
		values["counters."+key] = float64(value)
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
	return out, nil
}
