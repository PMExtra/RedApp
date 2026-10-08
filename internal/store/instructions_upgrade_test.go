package store

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestBuiltinInstructionReadingOrderAndCommands(t *testing.T) {
	previous := EntityTemplates()
	commands := regexp.MustCompile("(?s)```.*?```")
	for _, old := range previous {
		current, _ := BuiltinApplicationTemplate(old.Vendor.ID + "/" + old.Application.ID)
		for _, locale := range []struct {
			before, after string
			headings      []string
		}{
			{old.Instructions.En, current.Instructions.En, []string{"## Install", "### Getting started", "### Update", "<summary>Install a specific version</summary>"}},
			{old.Instructions.ZhCN, current.Instructions.ZhCN, []string{"## 安装", "### 开始使用", "### 更新", "<summary>安装指定版本</summary>"}},
		} {
			if strings.Contains(locale.after, "1.2.3") || strings.Contains(locale.after, "--release latest") || strings.Contains(locale.after, "-s -- latest") || strings.Contains(locale.after, "'latest'") {
				t.Fatal("redundant or fictional version example")
			}
			last := -1
			for _, heading := range locale.headings {
				index := strings.Index(locale.after, heading)
				if index <= last {
					t.Fatalf("incorrect instruction order for %s: %s", old.Application.ID, heading)
				}
				last = index
			}
			if !reflect.DeepEqual(commands.FindAllString(locale.before, -1)[:2], commands.FindAllString(locale.after, -1)[:2]) {
				t.Fatalf("installation commands changed for %s", old.Application.ID)
			}
		}
	}
}

func TestBuiltinInstructionBasicCopy(t *testing.T) {
	for _, template := range EntityTemplates() {
		for _, locale := range []struct{ text, heading, wait string }{
			{template.Instructions.En, "### Getting started", "Download progress may not be displayed. Please wait 1–2 minutes after running the command."},
			{template.Instructions.ZhCN, "### 开始使用", "下载过程中可能无进度显示，执行后请等待1~2分钟。"},
		} {
			for _, removed := range []string{"This command downloads and runs an installer.", "Use a service you trust.", "Installer and self-update downloads use this distribution service.", "此命令会下载并执行安装脚本", "请使用你信任的服务", "安装脚本与自动更新通过此分发服务下载"} {
				if strings.Contains(locale.text, removed) {
					t.Fatal("obsolete default copy", template.Application.ID, removed)
				}
			}
			count := 0
			if template.Application.Provider == "claude-code" {
				count = 1
			}
			if strings.Count(locale.text, locale.wait) != count {
				t.Fatal("incorrect wait hint count", template.Application.ID)
			}
			if count == 1 {
				position := strings.Index(locale.text, locale.wait)
				if position < strings.Index(locale.text, "| iex\n```") || position > strings.Index(locale.text, locale.heading) {
					t.Fatal("wait hint must follow basic installation commands")
				}
			}
		}
	}
}
