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
