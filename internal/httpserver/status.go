package httpserver

import (
	"github.com/PMExtra/RedApp/internal/download"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func (s *Server) status(public string) (map[string]any, error) {
	views := s.Downloads.Snapshot()
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
	e := filepath.WalkDir(s.Dir, func(path string, d os.DirEntry, e error) error {
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
	if e = syscall.Statfs(s.Dir, &disk); e != nil {
		return nil, e
	}
	applicationVersionCounts := map[string]int64{}
	for _, entry := range s.Registry.Entries() {
		count, err := s.DB.VersionCount(entry.Descriptor.ID)
		if err != nil {
			return nil, err
		}
		applicationVersionCounts[entry.Descriptor.ID] = count
	}
	counters, e := s.DB.Counters()
	if e != nil {
		return nil, e
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	status := map[string]any{"name": "RedApp", "started": s.Started, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "goroutines": runtime.NumGoroutine(), "memory_bytes": mem.Alloc, "sampled_at": time.Now().UTC(), "resources": views, "counters": counters, "disk": map[string]any{"used_bytes": total, "logical_bytes": logical, "allocated_cache_bytes": allocatedCache, "allocated_temporary_bytes": allocatedTemp, "allocated_pending_bytes": allocatedPending, "cache_bytes": complete, "temporary_bytes": temp, "pending_bytes": pending, "other_bytes": total - allocatedCache - allocatedTemp - allocatedPending, "free_bytes": disk.Bavail * uint64(disk.Bsize)}, "public_base_url": public, "rates": s.DB.Rates(), "application_version_counts": applicationVersionCounts}
	status["metrics"] = globalMetrics(status, s.Started)
	return status, nil
}

func (s *Server) appStatus(app, public string) (map[string]any, error) {
	all := s.Downloads.Snapshot()
	views := make([]download.View, 0)
	for _, v := range all {
		if v.Resource.Application == app {
			views = append(views, v)
		}
	}
	versionCount, err := s.DB.VersionCount(app)
	if err != nil {
		return nil, err
	}
	counters, err := s.DB.CountersFor(app)
	if err != nil {
		return nil, err
	}
	status := map[string]any{"application": app, "sampled_at": time.Now().UTC(), "resources": views, "version_count": versionCount, "counters": counters, "public_base_url": public + "/" + app}
	status["metrics"] = applicationMetrics(status)
	return status, nil
}
