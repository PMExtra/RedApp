package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestV075DefaultTitlesMigrateWithoutTouchingCustomHeadings(t *testing.T) {
	var previous []EntityTemplate
	if err := json.Unmarshal(instructionsV074, &previous); err != nil {
		t.Fatal(err)
	}
	for _, template := range previous {
		for _, custom := range []string{"", template.Instructions.ZhCN + "\nCustom", "### Shell\n\n```sh\necho custom\n```"} {
			s := openTest(t)
			if err := s.EnsureEntityTemplates(); err != nil {
				t.Fatal(err)
			}
			key := template.Vendor.ID + "/" + template.Application.ID
			app, err := s.Application(key)
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.Instructions(app.UID)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := s.SaveInstructions(key, before.Revision, LocalizedText{En: template.Instructions.En, ZhCN: custom})
			if err != nil {
				t.Fatal(err)
			}
			if err = upgradeBuiltinInstructions(s.DB); err != nil {
				t.Fatal(err)
			}
			after, err := s.Instructions(app.UID)
			if err != nil {
				t.Fatal(err)
			}
			current, _ := BuiltinApplicationTemplate(key)
			if after.En != current.Instructions.En || strings.Contains(after.En, "### Shell") || after.ZhCN != custom || after.Revision != saved.Revision+1 {
				t.Fatal(after)
			}
			if err = upgradeBuiltinInstructions(s.DB); err != nil {
				t.Fatal(err)
			}
			again, _ := s.Instructions(app.UID)
			if again != after {
				t.Fatal("migration repeated", again)
			}
		}
	}
}
