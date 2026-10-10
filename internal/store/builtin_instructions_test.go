package store

import (
	"strings"
	"testing"
)

// Built-in instructions read install, getting started, update, then the
// optional pinned-version commands, and use template variables instead of
// sample version numbers.
func TestBuiltinInstructionsFollowReadingOrder(t *testing.T) {
	for _, template := range EntityTemplates() {
		for _, locale := range []struct {
			text     string
			headings []string
		}{
			{template.Instructions.En, []string{"## Install", "### Getting started", "### Update", "<summary>Install a specific version</summary>"}},
			{template.Instructions.ZhCN, []string{"## 安装", "### 开始使用", "### 更新", "<summary>安装指定版本</summary>"}},
		} {
			for _, sample := range []string{"1.2.3", "--release latest", "-s -- latest", "'latest'"} {
				if strings.Contains(locale.text, sample) {
					t.Errorf("%s instructions contain the sample version %q", template.Application.ID, sample)
				}
			}
			last := -1
			for _, heading := range locale.headings {
				index := strings.Index(locale.text, heading)
				if index <= last {
					t.Errorf("%s instructions place %q out of order", template.Application.ID, heading)
				}
				last = index
			}
		}
	}
}

// Claude Code downloads silently for a while, so its instructions warn once,
// right after the basic installation commands.
func TestBuiltinInstructionsPlaceWaitHint(t *testing.T) {
	for _, template := range EntityTemplates() {
		for _, locale := range []struct{ text, heading, wait string }{
			{template.Instructions.En, "### Getting started", "Download progress may not be displayed. Please wait 1–2 minutes after running the command."},
			{template.Instructions.ZhCN, "### 开始使用", "下载过程中可能无进度显示，执行后请等待1~2分钟。"},
		} {
			want := 0
			if template.Application.Provider == "claude-code" {
				want = 1
			}
			if got := strings.Count(locale.text, locale.wait); got != want {
				t.Fatalf("%s has %d wait hints; want %d", template.Application.ID, got, want)
			}
			if want == 1 {
				position := strings.Index(locale.text, locale.wait)
				if position < strings.Index(locale.text, "| iex\n```") || position > strings.Index(locale.text, locale.heading) {
					t.Fatalf("%s wait hint must follow the basic installation commands", template.Application.ID)
				}
			}
		}
	}
}
