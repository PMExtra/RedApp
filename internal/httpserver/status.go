package httpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
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

// publicViews owns its response copies. Internal source namespaces and metric
// scopes stay intact in the managers, including retained historical epochs.
func (s *Server) publicViews(views []download.View) []download.View {
	scopes := s.publicScopes()
	out := make([]download.View, len(views))
	for i, view := range views {
		view.Resource.Application = publicScope(view.Resource.Application, scopes)
		view.Resource.MetricsID = publicScope(view.Resource.MetricsID, scopes)
		out[i] = view
	}
	return out
}

func (s *Server) status(public string) (map[string]any, error) {
	views, err := s.resourceViews()
	if err != nil {
		return nil, err
	}
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
	e := filepath.WalkDir(s.dataDir, func(path string, d os.DirEntry, e error) error {
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
	if e != nil {
		return nil, e
	}
	var disk syscall.Statfs_t
	if e = syscall.Statfs(s.dataDir, &disk); e != nil {
		return nil, e
	}
	applicationVersionCounts := map[string]int64{}
	for _, entry := range s.registry.Entries() {
		if entry.Protocol == nil {
			continue
		}
		count, err := s.store.VersionCount(entry.StorageID())
		if err != nil {
			return nil, err
		}
		applicationVersionCounts[entry.Descriptor.ID] = count
	}
	counters, e := s.store.Counters()
	if e != nil {
		return nil, e
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	status := map[string]any{"name": "RedApp", "started": s.started, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "goroutines": runtime.NumGoroutine(), "memory_bytes": mem.Alloc, "sampled_at": time.Now().UTC(), "resources": views, "counters": counters, "disk": map[string]any{"used_bytes": total, "logical_bytes": logical, "allocated_cache_bytes": allocatedCache, "allocated_temporary_bytes": allocatedTemp, "allocated_pending_bytes": allocatedPending, "cache_bytes": complete, "temporary_bytes": temp, "pending_bytes": pending, "other_bytes": total - allocatedCache - allocatedTemp - allocatedPending, "free_bytes": disk.Bavail * uint64(disk.Bsize)}, "public_base_url": public, "rates": s.store.Rates(), "application_version_counts": applicationVersionCounts}
	status["metrics"] = globalMetrics(status, s.started)
	status["resources"] = s.publicViews(views)
	return status, nil
}

func (s *Server) appStatus(app, public string) (map[string]any, error) {
	entry, ok := s.registry.LookupAny(app)
	if !ok {
		return nil, application.ErrNotFound
	}
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
	var versionCount any
	if entry.Protocol != nil {
		count, err := s.store.VersionCount(entry.StorageID())
		if err != nil {
			return nil, err
		}
		versionCount = count
	}
	counters, err := s.store.CountersFor(entry.MetricsID())
	if err != nil {
		return nil, err
	}
	status := map[string]any{"application": app, "sampled_at": time.Now().UTC(), "resources": views, "version_count": versionCount, "counters": counters, "public_base_url": public + "/" + app}
	status["metrics"] = applicationMetrics(status)
	status["resources"] = s.publicViews(views)
	return status, nil
}
