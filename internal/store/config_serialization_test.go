package store

import (
	"database/sql"
	"errors"
	"github.com/PMExtra/RedApp/presets"
	"reflect"
	"sync"
	"testing"
)

func TestConcurrentImportWithSameIDReturnsExistingReceipt(t *testing.T) {
	s := openTest(t)
	s.ReconcileTemplates(presets.Embedded())
	p, _ := s.ExportConfiguration(exchangeOptions("independent"))
	for i := range p.Documents {
		if p.Documents[i].Kind == "Vendor" {
			p.Documents[i].Metadata.ID = "new"
		} else {
			p.Documents[i].Metadata.Vendor = "new"
			p.Documents[i].Metadata.ID = "copy"
		}
	}
	plan, e := s.PreviewConfigurationImport(p.Documents, nil)
	if e != nil || !plan.Ready {
		t.Fatal(plan, e)
	}
	const n = 4
	results, errs := make([]ImportResult, n), make([]error, n)
	var wg, checked sync.WaitGroup
	checked.Add(n)
	importReceiptChecked = checked.Done
	t.Cleanup(func() { importReceiptChecked = nil })
	run := func(i int) {
		defer wg.Done()
		results[i], errs[i] = s.ExecuteConfigurationImport(plan, "same-id", true, func() bool { return true })
	}
	// The first import holds writeMu until every other import has found no
	// receipt outside it; they must then find the first one's receipt inside.
	var once sync.Once
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		once.Do(func() {
			for i := 1; i < n; i++ {
				wg.Add(1)
				go run(i)
			}
			checked.Wait()
		})
		return nil, nil
	})
	wg.Add(1)
	run(0)
	wg.Wait()
	for i := range n {
		if errs[i] != nil || !results[i].Applied || !reflect.DeepEqual(results[i], results[0]) {
			t.Fatal(i, results[i], errs[i])
		}
	}
	if receipt, found, err := s.ImportReceipt("same-id"); err != nil || !found || !reflect.DeepEqual(receipt, results[0]) {
		t.Fatal(receipt, found, err)
	}
}

func TestAdminNotesAndPermanentDeleteDuringConfigurationWrite(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "Acme"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "notes", Name: LocalizedText{"Notes", "Notes"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.CreateApplication(v.ID, ApplicationInput{ID: "gone", Name: LocalizedText{"Gone", "Gone"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var once sync.Once
	var notesErr, deleteErr error
	s.SetConfigurationPrepare(func(DirectorySnapshot) (ConfigurationPublication, error) {
		once.Do(func() {
			// Notes are not published, so they commit while the write is open.
			_, notesErr = s.SaveAdminNotes("app", a.Key, 1, "private")
			// Permanent deletion is published and waits for the write.
			wg.Add(1)
			go func() { defer wg.Done(); deleteErr = s.PermanentlyDeleteApplication(gone.Key, gone.Revision) }()
		})
		return nil, nil
	})
	if _, err = s.UpdateApplication(a.Key, a.Revision, ApplicationChanges{Name: LocalizedText{"Renamed", "Renamed"}, Enabled: true}); err != nil {
		t.Fatal("notes or a deletion made the write conflict", err)
	}
	wg.Wait()
	if notesErr != nil || deleteErr != nil {
		t.Fatal(notesErr, deleteErr)
	}
	if notes, _ := s.AdminNotes("app", a.Key); notes.Text != "private" {
		t.Fatal(notes)
	}
	if _, err = s.Application(gone.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("application not purged", err)
	}
}

func TestPermanentDeleteOfLiveEntitiesIsAtomic(t *testing.T) {
	fault := injectCommitFault(t)
	s := openTest(t)
	v, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "Acme"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "app", Name: LocalizedText{"App", "App"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fault.armed.Store(true)
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err == nil {
		t.Fatal("fault ignored")
	}
	if got, err := s.Application(a.Key); err != nil || got.DeletedAt != nil || got.Revision != a.Revision {
		t.Fatal("failed permanent delete left a soft delete", got, err)
	}
	fault.armed.Store(false)
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	if v, err = s.Vendor(v.ID); err != nil {
		t.Fatal(err)
	}
	fault.armed.Store(true)
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); err == nil {
		t.Fatal("fault ignored")
	}
	if got, err := s.Vendor(v.ID); err != nil || got.DeletedAt != nil || got.Revision != v.Revision {
		t.Fatal("failed permanent delete left a soft delete", got, err)
	}
	fault.armed.Store(false)
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Vendor(v.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("vendor not purged", err)
	}
}
