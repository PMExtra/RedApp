package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestV075InstructionHintsPreserveCustomAndEmptyLocales(t *testing.T) {
	var previous []EntityTemplate
	if err := json.Unmarshal(instructionsV075, &previous); err != nil {
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
		for _, custom := range []string{"", "## Installation instructions\n\nMy custom heading and commands"} {
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
