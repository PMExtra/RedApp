// Package history stores bounded global/application observations and UTC aggregates.
package history

type Definition struct {
	Retired bool   `json:"retired,omitempty"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Unit    string `json:"unit"`
	Group   string `json:"group"`
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
	for _, item := range [][2]string{{"used_bytes", "Service disk usage"}, {"logical_bytes", "Total logical file bytes"}, {"allocated_cache_bytes", "Allocated cache"}, {"allocated_temporary_bytes", "Allocated temporary files"}, {"allocated_pending_bytes", "Allocated pending deletion"}, {"cache_bytes", "Cache"}, {"temporary_bytes", "Temporary"}, {"pending_bytes", "Pending deletion"}, {"other_bytes", "Other allocated disk usage"}, {"free_bytes", "Filesystem available"}} {
		add("disk."+item[0], item[1], "gauge", "bytes", "Disk")
	}
	for _, item := range [][3]string{{"requests", "Public requests", "count"}, {"artifact_requests", "Artifact requests", "count"}, {"cache_hit_requests", "Cache-hit requests", "count"}, {"shared_follower_requests", "Shared-follower requests", "count"}, {"miss_requests", "Miss requests", "count"}, {"download_success", "Successful downloads", "count"}, {"download_errors", "Download errors", "count"}, {"upstream_errors", "Artifact upstream errors", "count"}, {"upstream_bytes", "Upstream traffic", "bytes"}, {"downstream_bytes", "Downstream traffic", "bytes"}, {"cleanup_freed_bytes", "Logical bytes reclaimed", "bytes"}} {
		add("counters."+item[0], item[1], "counter", item[2], "Traffic and requests")
	}
	add("rates.upstream_bytes_per_second", "Upstream rate", "rate", "bytes_per_second", "Speed")
	add("rates.downstream_bytes_per_second", "Downstream rate", "rate", "bytes_per_second", "Speed")
	add("runtime.memory_bytes", "Memory allocated", "gauge", "bytes", "Runtime")
	add("runtime.goroutines", "Goroutines", "gauge", "count", "Runtime")
	add("runtime.uptime_seconds", "Process uptime", "gauge", "seconds", "Runtime")
	for _, item := range [][2]string{{"total", "Resource generations"}, {"current", "Current generations"}, {"retired", "Retired generations"}, {"readers", "Active readers"}, {"active_writers", "Active writers"}, {"queued", "Queued generations"}, {"downloading", "Downloading generations"}, {"resuming", "Resuming generations"}, {"retry_wait", "Retry-wait generations"}, {"verifying", "Verifying generations"}, {"complete", "Complete generations"}, {"failed", "Failed generations"}, {"invalid", "Invalid generations"}, {"interrupted", "Interrupted generations"}} {
		add("resources."+item[0], item[1], "gauge", "count", "Resources and tasks")
	}
	add("versions.total", "Discovered versions", "gauge", "count", "Resources and tasks")
	return out
}

// Catalog retains retired definitions solely for reading and aggregating their
// existing observations until ordinary retention expires them.
func Catalog() []Definition {
	return append(Definitions(),
		Definition{Key: "counters.reuse_requests", Label: "Reused requests", Kind: "counter", Unit: "count", Group: "Traffic and requests", Retired: true},
		Definition{Key: "events.recent_total", Label: "Recent failures (up to 100)", Kind: "gauge", Unit: "count", Group: "Resources and tasks", Retired: true})
}
func Find(key string) (Definition, bool) {
	for _, d := range Catalog() {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}
