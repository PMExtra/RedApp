package apps

// PublicInfo contains only information intended for anonymous client installation.
type PublicInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Origin  string `json:"origin"`
	Icon    string `json:"icon"`
}
