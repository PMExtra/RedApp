package codex

// PublicInfo contains only information intended for anonymous client installation.
type PublicInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Origin  string `json:"origin"`
	Icon    string `json:"icon"`
}

func PublicApplication(origin string) PublicInfo {
	return PublicInfo{ID: "codex", Name: "Codex CLI", Summary: "OpenAI's coding agent for your terminal.", Origin: origin, Icon: "/apps/codex/icon.svg"}
}
