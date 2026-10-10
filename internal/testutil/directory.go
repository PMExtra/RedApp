package testutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/store"
)

// App returns the directory application key ("vendor/app") of db, creating
// it (and its vendor) enabled when it does not exist yet. input supplies the
// provider settings; its ID, Name and Enabled fields are filled in.
func App(t testing.TB, db *store.Store, key string, input store.ApplicationInput) store.Application {
	t.Helper()
	if app, err := db.Application(key); err == nil {
		return app
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	vendorID, appID, ok := strings.Cut(key, "/")
	if !ok {
		t.Fatalf("invalid application key %q", key)
	}
	name := store.LocalizedText{En: key, ZhCN: key}
	if _, err := db.Vendor(vendorID); errors.Is(err, store.ErrNotFound) {
		if _, err = db.CreateVendor(store.VendorInput{ID: vendorID, Name: name, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	input.ID, input.Name, input.Enabled = appID, name, true
	app, err := db.CreateApplication(vendorID, input)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// Entry returns runtime with the identity of the persisted application key:
// UID, vendor, revisions, source epoch, provider, effective enabled state and
// Descriptor.ID. runtime supplies the provider parts (descriptor fields,
// protocol and upstreams), as the registry builder does in production.
func Entry(t testing.TB, db *store.Store, key string, runtime application.Entry) application.Entry {
	t.Helper()
	app, err := db.Application(key)
	if err != nil {
		t.Fatal(err)
	}
	vendor, err := db.Vendor(app.VendorID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Descriptor.ID = app.Key
	runtime.Provider = app.Provider
	runtime.UID, runtime.SourceEpoch = app.UID, app.SourceEpoch
	runtime.VendorUID, runtime.VendorID = vendor.UID, vendor.ID
	runtime.Revision, runtime.RuntimeRevision = app.Revision, app.RuntimeRevision
	runtime.VendorRevision, runtime.VendorRuntimeRevision = vendor.Revision, vendor.RuntimeRevision
	runtime.Enabled = app.Enabled && vendor.Enabled
	runtime.DeletedAt = app.DeletedAt
	return runtime
}
