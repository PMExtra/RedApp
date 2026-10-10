package store

import (
	"database/sql"
	"maps"
	"sort"

	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/presets"
)

// writeConfiguration runs one configuration write:
//
//  1. Read: change loads the rows it needs into a working set and edits them;
//     materialize validates the changed entities and derives their stored and
//     runtime data. No row is written yet.
//  2. Prepare: the runtime publication is built from the current runtime view
//     plus the changed entities, with no transaction open.
//  3. Commit: one transaction stores the changed rows, each conditioned on the
//     revision it was read with (ErrConflict otherwise), then runs finalize.
//  4. Publish: only after the commit succeeded; a failed prepare or commit
//     aborts the publication and leaves the runtime view unchanged.
//
// writeMu is held from step 1 to step 4 so that publications are applied in
// commit order and each prepare starts from the view of the previous commit.
// The longest operation under it is one write transaction over the touched
// rows plus the in-memory runtime prepare; SQLite admits one writer at a time
// anyway. Admin notes and other per-entity rows that are not published are
// written without it, under CAS alone.
func (s *Store) writeConfiguration(change func(*configSet) error, finalize func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeConfigurationLocked(change, finalize)
}

// writeConfigurationLocked requires writeMu.
func (s *Store) writeConfigurationLocked(change func(*configSet) error, finalize func(*sql.Tx) error) error {
	w, err := s.readChange(change)
	if err != nil {
		return err
	}
	if finalize == nil && !w.changed() {
		return nil
	}
	var next *directoryView
	if s.prepareConfiguration != nil || s.view != nil {
		current, err := s.currentView()
		if err != nil {
			return err
		}
		next = current.clone()
		w.applyTo(next)
	}
	var publication ConfigurationPublication
	if s.prepareConfiguration != nil {
		if publication, err = s.prepareConfiguration(next.snapshot()); err != nil {
			return err
		}
		defer func() {
			if publication != nil {
				publication.Abort()
			}
		}()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = w.write(tx); err != nil {
		return err
	}
	if finalize != nil {
		if err = finalize(tx); err != nil {
			return err
		}
	}
	if s.beforeCommit != nil {
		if err = s.beforeCommit(tx); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if publication != nil {
		publication.Publish()
		publication = nil
	}
	if next != nil {
		next.remove(w.removedApps, w.removedVendors)
		s.view = next
	}
	return nil
}

// readChange runs the read phase in a read transaction. change must read
// through the working set only: the transaction holds the connection.
func (s *Store) readChange(change func(*configSet) error) (*configSet, error) {
	tx, err := s.read.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	w := newConfigSet(tx)
	if err = change(w); err != nil {
		return nil, err
	}
	if err = w.materialize(); err != nil {
		return nil, err
	}
	if s.validateDistributions != nil && (w.allDistributions || w.distributionsChanged()) {
		descriptors, err := w.reviewedDescriptors()
		if err != nil {
			return nil, err
		}
		if err = s.validateDistributions(descriptors); err != nil {
			return nil, err
		}
	}
	w.q = nil
	return w, nil
}

// dropFromView removes purged entities from the runtime view without a
// publication: they were already published as deleted. Requires writeMu.
func (s *Store) dropFromView(apps, vendors []string) {
	if s.view != nil {
		next := s.view.clone()
		next.remove(apps, vendors)
		s.view = next
	}
}

// directoryView is the runtime projection of the committed configuration:
// everything a DirectorySnapshot is built from. A write derives the next view
// from the current one and the entities it changed, so preparing a
// publication never rereads the configuration. A view is never modified
// after it is installed.
type directoryView struct {
	globalProxy    networkproxy.Config
	globalRevision int64
	vendors        map[string]Vendor // by UID
	vendorProxy    map[string]networkproxy.Config
	apps           map[string]Application // by UID
	appProxy       map[string]networkproxy.Config
	appRef         map[string]string
	sources        map[string]SourceRecord // by storage ID
	distributions  map[string]trustedDistribution
}

// currentView returns the runtime view, loading it on first use. Requires writeMu.
func (s *Store) currentView() (*directoryView, error) {
	if s.view != nil {
		return s.view, nil
	}
	return s.reloadView()
}

// reloadView rebuilds the runtime view from the database. Requires writeMu.
func (s *Store) reloadView() (*directoryView, error) {
	tx, err := s.read.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	view, err := loadDirectoryView(newConfigSet(tx))
	if err != nil {
		return nil, err
	}
	s.view = view
	return view, nil
}

func loadDirectoryView(w *configSet) (*directoryView, error) {
	if err := w.loadEverything(); err != nil {
		return nil, err
	}
	global, err := w.globalProxy()
	if err != nil {
		return nil, err
	}
	v := &directoryView{globalProxy: global.config, globalRevision: global.revision, vendors: map[string]Vendor{}, vendorProxy: map[string]networkproxy.Config{}, apps: map[string]Application{}, appProxy: map[string]networkproxy.Config{}, appRef: map[string]string{}, sources: map[string]SourceRecord{}, distributions: map[string]trustedDistribution{}}
	for uid, e := range w.vendors {
		if v.vendorProxy[uid], err = w.ownProxy("Vendor", e.config); err != nil {
			return nil, err
		}
		v.vendors[uid] = e.Vendor
	}
	for uid, e := range w.apps {
		if _, ok := w.vendors[e.VendorUID]; !ok {
			return nil, ErrInvalidDirectory
		}
		if v.appProxy[uid], err = w.ownProxy("App", e.config); err != nil {
			return nil, err
		}
		v.apps[uid] = e.Application
		if e.config.Ref != nil {
			v.appRef[uid] = *e.config.Ref
		}
	}
	for key, d := range w.distributions {
		if d != nil {
			v.distributions[key] = *d
		}
	}
	rows, err := w.q.Query(`SELECT ` + sourceColumns + sourceJoin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		v.sources[source.StorageID()] = source
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = w.checkContracts(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *directoryView) clone() *directoryView {
	out := *v
	out.vendors, out.vendorProxy = maps.Clone(v.vendors), maps.Clone(v.vendorProxy)
	out.apps, out.appProxy, out.appRef = maps.Clone(v.apps), maps.Clone(v.appProxy), maps.Clone(v.appRef)
	out.sources, out.distributions = maps.Clone(v.sources), maps.Clone(v.distributions)
	return &out
}

// applyTo adds the entities this set changed to a view.
func (w *configSet) applyTo(v *directoryView) {
	if g := w.global; g != nil && g.revision != g.stored {
		v.globalProxy, v.globalRevision = g.config, g.revision
	}
	for uid, e := range w.vendors {
		if e.changed {
			v.vendors[uid], v.vendorProxy[uid] = e.Vendor, e.proxy
		}
	}
	for uid, e := range w.apps {
		if !e.changed {
			continue
		}
		v.apps[uid], v.appProxy[uid] = e.Application, e.proxy
		if e.config.Ref != nil {
			v.appRef[uid] = *e.config.Ref
		} else {
			delete(v.appRef, uid)
		}
		if e.newSource != nil {
			v.sources[e.newSource.StorageID()] = *e.newSource
		}
	}
	for key, d := range w.distributions {
		if d != nil {
			v.distributions[key] = *d
		}
	}
}

func (v *directoryView) remove(apps, vendors []string) {
	for _, uid := range apps {
		delete(v.apps, uid)
		delete(v.appProxy, uid)
		delete(v.appRef, uid)
		for id, source := range v.sources {
			if source.AppUID == uid {
				delete(v.sources, id)
			}
		}
	}
	for _, uid := range vendors {
		delete(v.vendors, uid)
		delete(v.vendorProxy, uid)
	}
}

// snapshot resolves the view into the runtime input: proxy scopes through the
// three-level inheritance, template bindings, reviewed contracts and the
// current eligibility of every source.
func (v *directoryView) snapshot() DirectorySnapshot {
	out := DirectorySnapshot{GlobalProxy: v.globalProxy, GlobalProxyRevision: v.globalRevision, ProxyScopes: map[string]networkproxy.Scope{}, TemplateBindings: map[string]string{}, ProviderDefaults: map[string]string{}}
	for _, vendor := range v.vendors {
		out.Vendors = append(out.Vendors, vendor)
	}
	sort.Slice(out.Vendors, func(i, j int) bool { return out.Vendors[i].ID < out.Vendors[j].ID })
	for _, app := range v.apps {
		out.Applications = append(out.Applications, app)
	}
	sort.Slice(out.Applications, func(i, j int) bool {
		a, b := out.Applications[i], out.Applications[j]
		if a.VendorID != b.VendorID {
			return a.VendorID < b.VendorID
		}
		return a.ID < b.ID
	})
	for _, app := range out.Applications {
		vendor := v.vendors[app.VendorUID]
		proxy := networkproxy.Resolve(v.appProxy[app.UID], app.Key, v.vendorProxy[app.VendorUID], vendor.ID, v.globalProxy)
		out.ProxyScopes[app.UID] = networkproxy.Scope{VendorUID: app.VendorUID, Proxy: proxy, Allowed: app.DeletedAt == nil && vendor.DeletedAt == nil}
		if ref, ok := v.appRef[app.UID]; ok {
			out.TemplateBindings[app.UID] = ref
		}
	}
	keys := make([]string, 0, len(v.distributions))
	for key := range v.distributions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		d := v.distributions[key]
		out.ReviewedDescriptors = append(out.ReviewedDescriptors, d.Descriptor)
		if builtin, _ := presets.ReleaseTemplateKey(d.Provider); builtin == key {
			out.ProviderDefaults[d.Provider] = d.Descriptor.Upstream
		}
	}
	for _, source := range v.sources {
		app := v.apps[source.AppUID]
		vendor := v.vendors[app.VendorUID]
		source.AppRuntimeRevision, source.VendorRuntimeRevision = app.RuntimeRevision, vendor.RuntimeRevision
		source.Active = app.Enabled && vendor.Enabled && app.DeletedAt == nil && vendor.DeletedAt == nil && source.Epoch == app.SourceEpoch
		out.Sources = append(out.Sources, source)
	}
	sort.Slice(out.Sources, func(i, j int) bool {
		a, b := out.Sources[i], out.Sources[j]
		if a.AppUID != b.AppUID {
			return a.AppUID < b.AppUID
		}
		return a.Epoch < b.Epoch
	})
	return out
}

// RepublishConfiguration rebuilds the runtime view from the database and
// publishes it through the installed coordinator, in order with configuration
// writes. It picks up rows changed outside the configuration write path.
func (s *Store) RepublishConfiguration() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	view, err := s.reloadView()
	if err != nil || s.prepareConfiguration == nil {
		return err
	}
	publication, err := s.prepareConfiguration(view.snapshot())
	if err != nil {
		return err
	}
	publication.Publish()
	return nil
}
