package claude

import "github.com/PMExtra/RedApp/internal/apps"

func PublicApplication(origin string) apps.PublicInfo {
	return apps.PublicInfo{ID: ID, Name: "Claude Code", Summary: "Anthropic’s coding agent for your terminal.", Origin: origin, Icon: ""}
}
