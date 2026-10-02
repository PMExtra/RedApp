package codex

import "github.com/PMExtra/RedApp/internal/apps"

func PublicApplication(origin string) apps.PublicInfo {
	return apps.PublicInfo{ID: "codex", Name: "Codex CLI", Summary: "OpenAI's coding agent for your terminal.", Origin: origin, Icon: "/apps/codex/icon.svg"}
}
