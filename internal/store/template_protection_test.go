package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestTemplateInsertionProtectionAndSelectiveCAS(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(VendorInput{ID: "openai", Name: LocalizedText{"Custom OpenAI", "自定义"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "codex", Name: LocalizedText{"My info", "我的介绍"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveInstructions(a.Key, 0, LocalizedText{}); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Application(a.Key)
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	same, _ := s.Application(a.Key)
	if same.UID != a.UID || same.Provider != "info" || same.Name != a.Name || same.Revision != a.Revision || !same.Enabled {
		t.Fatal("existing template key overwritten", same)
	}
	blank, _ := s.Instructions(a.UID)
	if blank.Revision != 0 || blank.En != "" {
		t.Fatal("explicit blank replaced", blank)
	}
	claude, _ := s.Application("anthropic/claude-code")
	vendor, _ := s.Vendor("anthropic")
	if claude.Enabled || vendor.Enabled {
		t.Fatal("new templates must start disabled")
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); !errors.Is(err, ErrBuiltinTemplate) {
		t.Fatal("provider mismatch bypassed protection", err)
	}
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); !errors.Is(err, ErrBuiltinTemplate) {
		t.Fatal("vendor bypassed protection", err)
	}
	// Field-level reset is a configuration unset; an independent entity has nothing to inherit.
	if _, err = s.PatchApplicationConfiguration(a.Key, ConfigurationPatch{Revision: a.Revision, Unset: []string{"name.en", "instructions.en"}}); !errors.Is(err, ErrInvalidDirectory) {
		t.Fatal("independent entity must not acquire inheritance through reset", err)
	}
	after, _ := s.Application(a.Key)
	if after.Provider != a.Provider || after.SourceEpoch != a.SourceEpoch || after.Enabled != a.Enabled || after.UID != a.UID {
		t.Fatal("unselected fields changed", after)
	}
	blank, _ = s.Instructions(a.UID)
	if blank.En != "" || blank.Revision != 0 {
		t.Fatal("unselected instructions changed")
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Application(a.Key)
	if !reflect.DeepEqual(again, after) {
		t.Fatal("restart rewrote existing template")
	}
}
