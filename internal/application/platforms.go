package application

// PlatformProtocol is the small optional immutable-release prewarm capability.
// Selection only returns keys from an already verified Release.
type Platform struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type PlatformProtocol interface {
	Platforms() []Platform
	SelectArtifacts(Release, []string) ([]string, error)
}

// SnapshotProtocol discovers a complete immutable snapshot for a channel.
// Its channel and resource bindings are committed atomically; refresh failure
// may serve the last valid snapshot without changing the existing head.
type SnapshotProtocol interface {
	Protocol
	CanonicalChannel(string) (string, error)
	PrewarmKeys(string) []string
}

// ArtifactKeyProtocol selects precise normalized package variants, independently
// of the older installer platform grouping capability.
type ArtifactKeyProtocol interface {
	SelectArtifactKeys(Release, []string) ([]string, error)
}
