package store

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestInstructionHintsPreserveCustomAndEmptyLocales(t *testing.T) {
	for _, snapshot := range [][]byte{instructionsV075, instructionsV076, instructionsV077, instructionsV079} {
		testInstructionHintsMigration(t, snapshot)
	}
}
func testInstructionHintsMigration(t *testing.T, snapshot []byte) {
	var previous []EntityTemplate
	if err := json.Unmarshal(snapshot, &previous); err != nil {
		t.Fatal(err)
	}
	for _, template := range previous {
		key := template.Vendor.ID + "/" + template.Application.ID
		current, _ := BuiltinApplicationTemplate(key)
		for _, locale := range []string{current.Instructions.En, current.Instructions.ZhCN} {
			for _, hint := range []string{"Linux", "macOS", "Shell", "Windows", "PowerShell"} {
				if !strings.Contains(locale, hint) {
					t.Fatalf("%s lacks %s", key, hint)
				}
			}
		}
		for _, custom := range []string{"", "## Installation instructions\n\nMy custom heading and commands", "Custom <!-- redapp:known-version --> {{latest_version}}", "Custom: This command downloads and runs an installer. Use a service you trust. 安装脚本与自动更新通过此分发服务下载。"} {
			cases := []struct{ input, want LocalizedText }{
				{template.Instructions, current.Instructions},
				{LocalizedText{En: template.Instructions.En, ZhCN: custom}, LocalizedText{En: current.Instructions.En, ZhCN: custom}},
				{LocalizedText{En: custom, ZhCN: template.Instructions.ZhCN}, LocalizedText{En: custom, ZhCN: current.Instructions.ZhCN}},
				{LocalizedText{En: custom, ZhCN: custom}, LocalizedText{En: custom, ZhCN: custom}},
			}
			for _, tc := range cases {
				s := openTest(t)
				if err := s.EnsureEntityTemplates(); err != nil {
					t.Fatal(err)
				}
				app, err := s.Application(key)
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.Instructions(app.UID)
				if err != nil {
					t.Fatal(err)
				}
				saved, err := s.SaveInstructions(key, before.Revision, tc.input)
				if err != nil {
					t.Fatal(err)
				}
				if err := upgradeBuiltinInstructions(s.DB); err != nil {
					t.Fatal(err)
				}
				after, err := s.Instructions(app.UID)
				if err != nil {
					t.Fatal(err)
				}
				revision := saved.Revision
				if tc.input != tc.want {
					revision++
				}
				if after.LocalizedText != tc.want || after.Revision != revision {
					t.Fatalf("%s: got %#v, want %#v revision %d", key, after, tc.want, revision)
				}
				if err := upgradeBuiltinInstructions(s.DB); err != nil {
					t.Fatal(err)
				}
				again, err := s.Instructions(app.UID)
				if err != nil {
					t.Fatal(err)
				}
				if again != after {
					t.Fatal("migration is not idempotent")
				}
			}
		}
	}
}

func TestBuiltinInstructionReadingOrderAndCommands(t *testing.T) {
	var previous []EntityTemplate
	if err := json.Unmarshal(instructionsV077, &previous); err != nil {
		t.Fatal(err)
	}
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
