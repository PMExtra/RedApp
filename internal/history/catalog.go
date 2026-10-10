// Package history stores bounded global/application observations and UTC aggregates.
package history

// Definition describes one catalog metric. Group is a stable identifier
// (disk, traffic, speed, runtime, resources); clients localize its title.
type Definition struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Unit  string `json:"unit"`
	Group string `json:"group"`
}

type Metric struct {
	Definition
	Value           *float64 `json:"value"`
	ObservedSeconds float64  `json:"observed_seconds"`
}

// Definitions is a fixed, bounded catalog. Versions, resources and event strings
// cannot create metric dimensions.
func Definitions() []Definition {
	out := []Definition{}
	add := func(key, label, kind, unit, group string) {
		out = append(out, Definition{Key: key, Label: label, Kind: kind, Unit: unit, Group: group})
	}
	for _, item := range [][2]string{{"used_bytes", "Disk used"}, {"allocated_cache_bytes", "Cached files"}, {"allocated_temporary_bytes", "Partial downloads"}, {"allocated_pending_bytes", "Pending deletion"}, {"other_bytes", "Other data"}, {"free_bytes", "Free space"}} {
		add("disk."+item[0], item[1], "gauge", "bytes", "disk")
	}
	for _, item := range [][3]string{{"requests", "Public requests", "count"}, {"artifact_requests", "File requests", "count"}, {"cache_hit_requests", "Cache hits", "count"}, {"shared_follower_requests", "Shared downloads", "count"}, {"miss_requests", "Cache misses", "count"}, {"download_success", "Downloads served", "count"}, {"download_errors", "Downloads failed", "count"}, {"upstream_errors", "Upstream errors", "count"}, {"upstream_bytes", "Traffic in", "bytes"}, {"downstream_bytes", "Traffic out", "bytes"}, {"cleanup_freed_bytes", "Space reclaimed", "bytes"}} {
		add("counters."+item[0], item[1], "counter", item[2], "traffic")
	}
	add("rates.upstream_bytes_per_second", "Speed in", "rate", "bytes_per_second", "speed")
	add("rates.downstream_bytes_per_second", "Speed out", "rate", "bytes_per_second", "speed")
	add("runtime.memory_bytes", "Memory", "gauge", "bytes", "runtime")
	for _, item := range [][2]string{{"current", "Current files"}, {"retired", "Retired files"}, {"readers", "Active downloads"}, {"active_writers", "Active fetches"}, {"failed", "Failed files"}, {"invalid", "Invalid files"}} {
		add("resources."+item[0], item[1], "gauge", "count", "resources")
	}
	add("versions.total", "Versions", "gauge", "count", "resources")
	return out
}

func Find(key string) (Definition, bool) {
	for _, d := range Definitions() {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}
