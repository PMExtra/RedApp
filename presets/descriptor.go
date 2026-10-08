package presets

type Localized map[string]string
type Installer struct {
	File   string `json:"file"`
	Source string `json:"source"`
	Shell  string `json:"shell"`
}
type Asset struct {
	File string `json:"file"`
	Kind string `json:"kind"`
}
type Descriptor struct {
	ID                       string      `json:"id"`
	Name                     Localized   `json:"name"`
	Publisher                string      `json:"publisher"`
	Summary                  Localized   `json:"summary"`
	Protocol                 string      `json:"protocol"`
	TrustRevision            int64       `json:"trust_revision"`
	Upstream                 string      `json:"upstream"`
	Channels                 []string    `json:"channels"`
	DefaultChannelTTLSeconds int         `json:"default_channel_ttl_seconds"`
	InstallerValidator       string      `json:"installer_validator"`
	Installers               []Installer `json:"installers"`
	Assets                   []Asset     `json:"assets"`
	Icon                     string      `json:"icon"`
	UpdatePolicy             Localized   `json:"update_policy"`
}
