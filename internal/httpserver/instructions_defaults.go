package httpserver

import (
	"strings"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

const knownVersionMarker = "<!-- redapp:known-version -->"

var versionInstructionText = map[string]struct{ fallback, intro string }{
	"en":    {"No version is known yet. Installation commands for a specific version will appear here after a version is discovered.", "Install the latest known version:"},
	"zh-CN": {"尚无已知版本。发现版本后，此处将显示指定版本的安装命令。", "安装当前已知最新版本："},
}

func versionInstructionsFallback(lang string) string { return versionInstructionText[lang].fallback }

// Dynamic examples belong only to an exact current built-in default. Custom and
// explicitly empty documents are never rewritten, even if they contain the marker.
func (s *Server) defaultVersionInstructions(entry application.Entry, content, lang string) (string, error) {
	template, ok := store.BuiltinApplicationTemplate(entry.Descriptor.ID)
	if !ok || template.Application.Provider != string(entry.Provider) {
		return content, nil
	}
	expected := template.Instructions.En
	if lang == "zh-CN" {
		expected = template.Instructions.ZhCN
	}
	if content != expected {
		return content, nil
	}
	version, _, err := s.latestKnownVersion(entry)
	if err != nil {
		return "", err
	}
	if version == "" {
		return content, nil
	}
	// The provider validated the version above. Single-quote each shell argument as
	// well, so interpolated observations never become shell/PowerShell expressions.
	shellVersion := "'" + strings.ReplaceAll(version, "'", "'\"'\"'") + "'"
	psVersion := "'" + strings.ReplaceAll(version, "'", "''") + "'"
	runner, shellFlag, psFlag := "sh", "--release ", "-Release "
	if entry.Provider == application.ClaudeCode {
		runner, shellFlag, psFlag = "bash", "", "-Target "
	}
	shell := "curl -fsSL '{{base_url}}{{app_path}}/install.sh' | " + runner + " -s -- " + shellFlag + shellVersion
	ps := "& ([scriptblock]::Create((irm '{{base_url}}{{app_path}}/install.ps1'))) " + psFlag + psVersion
	intro := versionInstructionText[lang].intro
	replacement := intro + "\n\n```" + runner + "\n" + shell + "\n```\n\n```powershell\n" + ps + "\n```"
	return strings.Replace(content, knownVersionMarker+"\n\n"+versionInstructionsFallback(lang), replacement, 1), nil
}
