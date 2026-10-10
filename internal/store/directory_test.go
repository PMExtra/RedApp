package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/PMExtra/RedApp/internal/identity"
)

func directoryVendor(id string) VendorInput {
	return VendorInput{ID: id, Name: LocalizedText{En: "Publisher", ZhCN: "发布者"}, Enabled: true}
}
func directoryApplication(id string) ApplicationInput {
	return ApplicationInput{ID: id, Name: LocalizedText{En: "Download", ZhCN: "下载"}, Provider: "http-cache", BaseURL: "http://files.internal:8080/packages/", CacheTTLSeconds: 300, Enabled: true}
}
func vendorChanges(v Vendor) VendorChanges {
	return VendorChanges{Name: v.Name, Description: v.Description, Icon: v.Icon, LocalizedIcons: v.LocalizedIcons, Enabled: v.Enabled}
}
func applicationChanges(a Application) ApplicationChanges {
	return ApplicationChanges{Name: a.Name, Description: a.Description, Icon: a.Icon, BaseURL: a.BaseURL, CacheTTLSeconds: a.CacheTTLSeconds, Enabled: a.Enabled}
}

func TestDirectoryIdentityEpochAndEligibilityFences(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, directoryApplication("app"))
	if err != nil {
		t.Fatal(err)
	}
	if !identity.ValidUID(v.UID) || !identity.ValidUID(a.UID) || !ValidAppID(a.StorageID()) || !ValidAppID(a.MetricsID()) || a.Key != "vendor/app" || a.SourceEpoch != 1 {
		t.Fatal(v, a)
	}
	original, err := s.Source(a.StorageID())
	if err != nil || !original.Active {
		t.Fatal(original, err)
	}
	if original.BaseURL != "http://files.internal:8080/packages" {
		t.Fatal(original.BaseURL)
	}
	if err = s.CheckSourceActive(original.StorageID(), original.Fence()); err != nil {
		t.Fatal(err)
	}

	// Vendor state affects eligibility without rewriting the app's own enable bit.
	disabled := vendorChanges(v)
	disabled.Enabled = false
	v, err = s.UpdateVendor(v.ID, v.Revision, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if active, err := s.SourceActive(a.StorageID()); err != nil || active {
		t.Fatal(active, err)
	}
	unchanged, err := s.Application(a.Key)
	if err != nil || !unchanged.Enabled || unchanged.Revision != a.Revision {
		t.Fatal(unchanged, err)
	}
	disabled.Enabled = true
	v, err = s.UpdateVendor(v.ID, v.Revision, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if active, err := s.SourceActive(a.StorageID()); err != nil || !active {
		t.Fatal(active, err)
	}
	if err = s.CheckSourceActive(a.StorageID(), original.Fence()); !errors.Is(err, ErrSourceInactive) {
		t.Fatal("work admitted before vendor disable became eligible", err)
	}

	// Cosmetic/TTL edits preserve identity and source epoch. Changing the origin
	// creates an immutable snapshot and leaves all data under the old namespace.
	change := applicationChanges(a)
	change.Name.En = "Renamed label"
	change.CacheTTLSeconds = 60
	edited, err := s.UpdateApplication(a.Key, a.Revision, change)
	if err != nil {
		t.Fatal(err)
	}
	if edited.UID != a.UID || edited.Key != a.Key || edited.Provider != a.Provider || edited.VendorUID != v.UID || edited.SourceEpoch != a.SourceEpoch {
		t.Fatal(edited)
	}
	if _, err = s.UpdateApplication(a.Key, a.Revision, change); !errors.Is(err, ErrConflict) {
		t.Fatal("stale application edit accepted", err)
	}
	releaseFixture(t, s, a.StorageID(), "1.0.0")
	change.BaseURL = "https://replacement.internal/files"
	replaced, err := s.UpdateApplication(edited.Key, edited.Revision, change)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.SourceEpoch != 2 || replaced.StorageID() == a.StorageID() || replaced.MetricsID() != a.MetricsID() {
		t.Fatal(replaced)
	}
	old, err := s.Source(a.StorageID())
	if err != nil || old.Active || old.BaseURL != original.BaseURL || old.Provider != original.Provider || !old.CreatedAt.Equal(original.CreatedAt) {
		t.Fatal(old, err)
	}
	if _, err = s.Release(a.StorageID(), "1.0.0"); err != nil {
		t.Fatal("source change removed cached metadata", err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = s.RequireSourceActive(tx, old.StorageID(), original.Fence())
	tx.Rollback()
	if !errors.Is(err, ErrSourceInactive) {
		t.Fatal("old source can publish", err)
	}

	// App disable/re-enable also keeps existing storage, but fences old writers.
	beforeDisable, err := s.Source(replaced.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	change = applicationChanges(replaced)
	change.Enabled = false
	replaced, err = s.UpdateApplication(replaced.Key, replaced.Revision, change)
	if err != nil {
		t.Fatal(err)
	}
	change.Enabled = true
	replaced, err = s.UpdateApplication(replaced.Key, replaced.Revision, change)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.SourceEpoch != 2 {
		t.Fatal("enable state unexpectedly changed source namespace")
	}
	if err = s.CheckSourceActive(replaced.StorageID(), beforeDisable.Fence()); !errors.Is(err, ErrSourceInactive) {
		t.Fatal("old app admission became eligible", err)
	}
	if err = s.DeleteVendor(v.ID, v.Revision); !errors.Is(err, ErrVendorHasApplications) {
		t.Fatal(err)
	}
	if err = s.DeleteApplication(replaced.Key, replaced.Revision); err != nil {
		t.Fatal(err)
	}
	if active, err := s.SourceActive(replaced.StorageID()); err != nil || active {
		t.Fatal(active, err)
	}
	if _, err = s.CreateApplication(v.ID, directoryApplication("app")); !errors.Is(err, ErrDirectoryExists) {
		t.Fatal("deleted slug was reused", err)
	}
	if err = s.DeleteVendor(v.ID, v.Revision); err != nil {
		t.Fatal(err)
	}
	sources, err := s.Sources()
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	if _, err = s.Release(a.StorageID(), "1.0.0"); err != nil {
		t.Fatal("tombstone removed data", err)
	}
}

func TestDirectoryCASHasOnlyOneWinner(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	changes := vendorChanges(v)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { <-start; _, err := s.UpdateVendor(v.ID, v.Revision, changes); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}

func TestDirectoryInputBoundaries(t *testing.T) {
	s := openTest(t)
	for _, id := range []string{"Admin", "admin", "api", "assets", "health", "../vendor", "a/b", "-vendor"} {
		if _, err := s.CreateVendor(directoryVendor(id)); !errors.Is(err, ErrInvalidDirectory) {
			t.Fatal(id, err)
		}
	}
	v, err := s.CreateVendor(directoryVendor("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*ApplicationInput){
		"missing translation":        func(a *ApplicationInput) { a.Name.ZhCN = "" },
		"unknown provider":           func(a *ApplicationInput) { a.Provider = "plugin" },
		"icon external":              func(a *ApplicationInput) { a.Icon = "https://example.org/icon.svg" },
		"icon unreviewed executable": func(a *ApplicationInput) { a.Icon = "/assets/icons/unreviewed.svg" },
		"base credential":            func(a *ApplicationInput) { a.BaseURL = "https://user:password@example.org/files" },
		"base query":                 func(a *ApplicationInput) { a.BaseURL = "https://example.org/files?path=other" },
		"base dot segment":           func(a *ApplicationInput) { a.BaseURL = "https://example.org/files/%2e%2e/private" },
		"base encoded slash":         func(a *ApplicationInput) { a.BaseURL = "https://example.org/files%2fprivate" },
		"negative TTL":               func(a *ApplicationInput) { a.CacheTTLSeconds = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			in := directoryApplication("app")
			edit(&in)
			if _, err := s.CreateApplication(v.ID, in); !errors.Is(err, ErrInvalidDirectory) {
				t.Fatal(err)
			}
		})
	}
	if apps, err := s.Applications(true); err != nil || len(apps) != 0 {
		t.Fatal("invalid record persisted", apps, err)
	}
}

// Legacy fixtures use published columns, never current writers against old DDL.
func legacyVendor(t *testing.T, db *sql.DB, in VendorInput) (Vendor, error) {
	t.Helper()
	uid, err := identity.NewUID()
	if err != nil {
		return Vendor{}, err
	}
	_, err = db.Exec(`INSERT INTO vendors(uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,enabled,revision,deleted_at_s) VALUES(?,?,?,?,?,?,?,?,1,NULL)`, uid, in.ID, in.Name.En, in.Name.ZhCN, in.Description.En, in.Description.ZhCN, in.Icon, in.Enabled)
	if err != nil {
		return Vendor{}, err
	}
	return readLegacyVendor(db, in.ID)
}
func readLegacyVendor(db *sql.DB, id string) (Vendor, error) {
	var v Vendor
	err := db.QueryRow(`SELECT uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,enabled,revision FROM vendors WHERE id=?`, id).Scan(&v.UID, &v.ID, &v.Name.En, &v.Name.ZhCN, &v.Description.En, &v.Description.ZhCN, &v.Icon, &v.Enabled, &v.Revision)
	_, v.HasTemplate = BuiltinVendorTemplate(v.ID)
	return v, err
}
