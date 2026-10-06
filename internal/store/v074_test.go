package store

import (
	"encoding/json"
	"testing"
)

func TestV074InstructionsUpgradePreservesCustomAndEmpty(t *testing.T) {
	for _, custom := range []string{"", "custom {{install_commands}}", "custom {{base_url}}"} {
		t.Run(custom, func(t *testing.T) {
			s := openTest(t)
			if err := s.EnsureEntityTemplates(); err != nil {
				t.Fatal(err)
			}
			var old []EntityTemplate
			if err := json.Unmarshal(instructionsV073, &old); err != nil {
				t.Fatal(err)
			}
			for _, previous := range old {
				key := previous.Vendor.ID + "/" + previous.Application.ID
				a, err := s.Application(key)
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.Instructions(a.UID)
				if err != nil {
					t.Fatal(err)
				}
				saved, err := s.SaveInstructions(key, before.Revision, LocalizedText{En: previous.Instructions.En, ZhCN: custom})
				if err != nil {
					t.Fatal(err)
				}
				if err = upgradeInstructionsV074(s.DB); err != nil {
					t.Fatal(err)
				}
				after, err := s.Instructions(a.UID)
				if err != nil {
					t.Fatal(err)
				}
				current, _ := BuiltinApplicationTemplate(key)
				if after.En != current.Instructions.En || after.ZhCN != custom || after.Revision != saved.Revision+1 {
					t.Fatal(after)
				}
				if err = upgradeInstructionsV074(s.DB); err != nil {
					t.Fatal(err)
				}
				again, _ := s.Instructions(a.UID)
				if again != after {
					t.Fatal("repeat migration changed instructions", again)
				}
			}
		})
	}
}

func TestV074InstructionsUpgradeOnReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	var old []EntityTemplate
	if err = json.Unmarshal(instructionsV073, &old); err != nil {
		t.Fatal(err)
	}
	previous := old[0]
	key := previous.Vendor.ID + "/" + previous.Application.ID
	app, err := s.Application(key)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Instructions(app.UID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveInstructions(key, before.Revision, LocalizedText{En: "", ZhCN: previous.Instructions.ZhCN})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	after, err := s.Instructions(app.UID)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := BuiltinApplicationTemplate(key)
	if after.En != "" || after.ZhCN != current.Instructions.ZhCN || after.Revision != saved.Revision+1 {
		t.Fatal(after)
	}
}
