package builtin

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/presets"
)

func TestMissingReleaseTemplateRestartsWithTrustedProtocol(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db.SetDistributionValidation(ValidateDescriptors)
	if err = db.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	// Remove both release YAML entries while keeping their compiled resources.
	missing := presets.Embedded()
	missing.Apps = nil
	if err = db.ReconcileTemplates(missing); err != nil {
		t.Fatal(err)
	}
	before, err := db.ApplicationConfiguration("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if !before.TemplateMissing {
		t.Fatal("missing flag absent")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetDistributionValidation(ValidateDescriptors)
	if err = db.ReconcileTemplates(missing); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.DirectoryConfigurationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := EntriesFromConfiguration(snapshot, distributor.NewPool())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("missing release entries", len(entries))
	}
	for _, e := range entries {
		if e.Protocol == nil || e.TemplateID != e.Descriptor.ID {
			t.Fatal("protocol lost", e.Descriptor.ID)
		}
	}
	after, _ := db.ApplicationConfiguration("openai/codex")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart changed frozen configuration")
	}
	raw, _ := json.Marshal(after)
	var public map[string]json.RawMessage
	_ = json.Unmarshal(raw, &public)
	for _, field := range []string{"distribution", "descriptor", "distribution_digest", "trusted_distribution_snapshots"} {
		if _, ok := public[field]; ok {
			t.Fatal("private contract leaked", field)
		}
	}
	// A missing template cannot bypass the current build's unavailable protocol,
	// trust material or resources. No missing flags/projections are partially saved.
	for _, unavailable := range []string{"protocol", "trust", "resources"} {
		db.SetDistributionValidation(func([]presets.Descriptor) error { return errors.New("compiled " + unavailable + " unavailable") })
		if err = db.ReconcileTemplates(missing); err == nil {
			t.Fatal("unavailable runtime accepted", unavailable)
		}
		rejected, _ := db.ApplicationConfiguration("openai/codex")
		if !reflect.DeepEqual(after, rejected) {
			t.Fatal("missing contract failure changed config", unavailable)
		}
	}
	db.SetDistributionValidation(ValidateDescriptors)
	// The trusted snapshot is not an ordinary overlay field.
	if _, err = db.PatchApplicationConfiguration("openai/codex", store.ConfigurationPatch{Revision: after.Revision, Set: map[string]json.RawMessage{"distribution.protocol": json.RawMessage(`"other"`)}}); err == nil {
		t.Fatal("distribution override accepted")
	}
}

func TestInvalidTrustedContractsRollbackReconciliation(t *testing.T) {
	for _, kind := range []string{"protocol", "trust", "installer", "asset"} {
		t.Run(kind, func(t *testing.T) {
			db, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetDistributionValidation(ValidateDescriptors)
			if err = db.EnsureEntityTemplates(); err != nil {
				t.Fatal(err)
			}
			before, err := db.DirectoryConfigurationSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a newly embedded build with an unavailable runtime contract.
			changed := presets.Embedded()
			d := changed.Apps[0].Distribution
			switch kind {
			case "protocol":
				d.Protocol = "removed-protocol"
			case "trust":
				d.TrustRevision = 2
			case "installer":
				d.Installers = nil
			case "asset":
				d.Assets = nil
			}
			changed.Apps[0].Distribution = d
			if err = db.ReconcileTemplates(changed); err == nil {
				t.Fatal("unsupported contract accepted")
			}
			after, err := db.DirectoryConfigurationSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected contract partially committed")
			}
		})
	}
}
