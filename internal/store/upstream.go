package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
)

// A mirrored application is linked to the template upstream:<app uid>, which
// holds what it follows from its upstream application: the display fields
// and the source. Local overrides of the display fields and every local
// setting work as for any template; the source cannot be overridden.
const upstreamTemplatePrefix = "upstream:"

// UpstreamTemplateRef is the template reference of a mirrored application.
func UpstreamTemplateRef(appUID string) string { return upstreamTemplatePrefix + appUID }

// IsUpstreamTemplateRef reports whether ref is the template of a mirrored application.
func IsUpstreamTemplateRef(ref string) bool { return strings.HasPrefix(ref, upstreamTemplatePrefix) }

func isUpstreamRef(ref *string) bool { return ref != nil && IsUpstreamTemplateRef(*ref) }

// upstreamSourcePaths belong to the binding of a mirrored application.
var upstreamSourcePaths = map[string]bool{"json_release": true, "base_url": true, "base_urls": true, "source_strategy": true}

// mirroredDisplayPaths follow the upstream unless overridden locally.
var mirroredDisplayPaths = []string{"name.en", "name.zh-CN", "description.en", "description.zh-CN", "icon", "instructions.en", "instructions.zh-CN", "auto_index"}

var (
	ErrUpstreamNotFound = fmt.Errorf("upstream not found: %w", sql.ErrNoRows)
	ErrUpstreamInUse    = errors.New("the upstream has mirrored applications")
	ErrMirrorNotFound   = fmt.Errorf("application is not mirrored: %w", sql.ErrNoRows)
	ErrMirrorExists     = errors.New("the upstream application or the local application is already mirrored")
)

// Upstream is another RedApp instance whose applications this one mirrors.
type Upstream struct {
	UID        string
	Name       string
	Origin     string // scheme://host[:port]
	InstanceID string // identity it reported when last verified
	Revision   int64
	CreatedAt  time.Time
	Bindings   int
}

const upstreamColumns = `u.uid,u.name,u.origin,u.instance_id,u.revision,u.created_at_s,(SELECT count(*) FROM application_upstreams b WHERE b.upstream_uid=u.uid)`

func scanUpstream(row directoryScanner) (Upstream, error) {
	var u Upstream
	var at int64
	err := row.Scan(&u.UID, &u.Name, &u.Origin, &u.InstanceID, &u.Revision, &at, &u.Bindings)
	u.CreatedAt = time.Unix(at, 0).UTC()
	return u, err
}

func validateUpstream(name, origin, instanceID string) error {
	if strings.TrimSpace(name) != name || name == "" || len(name) > 200 || !identity.ValidUID(instanceID) {
		return ErrInvalidDirectory
	}
	if origin == "" || len(origin) > 2048 || strings.Count(origin, "/") != 2 || !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return invalidf("an upstream address is scheme://host[:port]")
	}
	return nil
}

func (s *Store) Upstreams() ([]Upstream, error) {
	rows, err := s.read.Query(`SELECT ` + upstreamColumns + ` FROM upstreams u ORDER BY u.name,u.uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upstream{}
	for rows.Next() {
		u, err := scanUpstream(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) Upstream(uid string) (Upstream, error) {
	u, err := scanUpstream(s.read.QueryRow(`SELECT `+upstreamColumns+` FROM upstreams u WHERE u.uid=?`, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrUpstreamNotFound
	}
	return u, err
}

// CreateUpstream records an upstream whose identity the caller verified.
func (s *Store) CreateUpstream(name, origin, instanceID string, at time.Time) (Upstream, error) {
	if err := validateUpstream(name, origin, instanceID); err != nil {
		return Upstream{}, err
	}
	uid, err := identity.NewUID()
	if err != nil {
		return Upstream{}, err
	}
	result, err := s.db.Exec(`INSERT INTO upstreams(uid,name,origin,instance_id,revision,created_at_s) VALUES(?,?,?,?,1,?) ON CONFLICT(origin) DO NOTHING`, uid, name, origin, instanceID, at.Unix())
	if err != nil {
		return Upstream{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return Upstream{}, ErrDirectoryExists
	}
	return s.Upstream(uid)
}

// UpdateUpstream renames an upstream or moves it to another address. A new
// address becomes the source of every application mirrored from it, which
// starts a new source epoch for each of them.
func (s *Store) UpdateUpstream(uid string, revision int64, name, origin, instanceID string) (Upstream, error) {
	if err := validateUpstream(name, origin, instanceID); err != nil {
		return Upstream{}, err
	}
	update := func(tx *sql.Tx) error {
		result, err := tx.Exec(`UPDATE upstreams SET name=?,origin=?,instance_id=?,revision=revision+1 WHERE uid=? AND revision=? AND NOT EXISTS(SELECT 1 FROM upstreams o WHERE o.origin=? AND o.uid<>?)`, name, origin, instanceID, uid, revision, origin, uid)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return s.upstreamWriteFailure(tx, uid, origin)
		}
		return nil
	}
	err := s.writeConfiguration(func(w *configSet) error {
		var current string
		if err := w.q.QueryRow(`SELECT origin FROM upstreams WHERE uid=?`, uid).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUpstreamNotFound
			}
			return err
		}
		if current == origin {
			return nil
		}
		bindings, err := w.upstreamBindings(`upstream_uid=?`, uid)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			a, t, err := w.mirroredApp(b.AppUID)
			if err != nil {
				return err
			}
			next := *t
			next.Spec = cloneObject(t.Spec)
			setMirrorSource(next.Spec, fmt.Sprint(next.Spec["provider"]), origin+"/"+b.RemoteKey)
			next.Hash = semanticHash(next.Schema, next.Kind, next.Metadata, next.Spec)
			w.putTemplate(next)
			a.Revision++
			a.changed = true
		}
		return nil
	}, update)
	if err != nil {
		return Upstream{}, err
	}
	return s.Upstream(uid)
}

func (s *Store) upstreamWriteFailure(tx *sql.Tx, uid, origin string) error {
	var taken int
	if err := tx.QueryRow(`SELECT count(*) FROM upstreams WHERE origin=? AND uid<>?`, origin, uid).Scan(&taken); err != nil {
		return err
	}
	if taken != 0 {
		return ErrDirectoryExists
	}
	var exists int
	if err := tx.QueryRow(`SELECT count(*) FROM upstreams WHERE uid=?`, uid).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrUpstreamNotFound
	}
	return ErrConflict
}

// DeleteUpstream removes an upstream that no application mirrors.
func (s *Store) DeleteUpstream(uid string, revision int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var stored int64
	var bindings int
	err = tx.QueryRow(`SELECT revision,(SELECT count(*) FROM application_upstreams WHERE upstream_uid=uid) FROM upstreams WHERE uid=?`, uid).Scan(&stored, &bindings)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUpstreamNotFound
	}
	if err != nil {
		return err
	}
	if stored != revision {
		return ErrConflict
	}
	if bindings != 0 {
		return ErrUpstreamInUse
	}
	if _, err = tx.Exec(`DELETE FROM upstreams WHERE uid=?`, uid); err != nil {
		return err
	}
	return tx.Commit()
}

// MirrorSnapshot is what a mirrored application follows from its upstream
// application, with the icon already stored locally.
type MirrorSnapshot struct {
	Name         LocalizedText
	Description  LocalizedText
	Instructions LocalizedText
	Icon         string
	AutoIndex    bool
}

// Binding is the state of a mirrored application.
type Binding struct {
	AppUID         string
	AppKey         string
	Provider       string // of the local application
	Icon           string // local copy of the upstream icon, as followed
	UpstreamUID    string
	RemoteKey      string
	RemoteRevision string
	RemoteIcon     string
	Chain          []string // upstream chain of the remote application
	SyncedAt       time.Time
	CheckedAt      time.Time
	ErrorCode      string // "" after a successful check
	// Available is false once the upstream reported the application gone.
	Available bool
}

const bindingColumns = `b.app_uid,v.id||'/'||a.id,a.provider,COALESCE((SELECT json_extract(t.spec_json,'$.icon') FROM template_snapshots t WHERE t.kind='App' AND t.canonical_key='` + upstreamTemplatePrefix + `'||b.app_uid),''),b.upstream_uid,b.remote_key,b.remote_revision,b.remote_icon,b.upstream_chain_json,b.synced_at_s,b.checked_at_s,b.error_code,COALESCE((SELECT t.present FROM template_snapshots t WHERE t.kind='App' AND t.canonical_key='` + upstreamTemplatePrefix + `'||b.app_uid),0)`

func scanBinding(row directoryScanner) (Binding, error) {
	var b Binding
	var chain []byte
	var synced, checked int64
	err := row.Scan(&b.AppUID, &b.AppKey, &b.Provider, &b.Icon, &b.UpstreamUID, &b.RemoteKey, &b.RemoteRevision, &b.RemoteIcon, &chain, &synced, &checked, &b.ErrorCode, &b.Available)
	if err == nil {
		err = json.Unmarshal(chain, &b.Chain)
	}
	b.SyncedAt, b.CheckedAt = time.Unix(synced, 0).UTC(), time.Unix(checked, 0).UTC()
	return b, err
}

type rowsQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func queryBindings(q rowsQuerier, where string, args ...any) ([]Binding, error) {
	rows, err := q.Query(`SELECT `+bindingColumns+` FROM application_upstreams b JOIN applications a ON a.uid=b.app_uid JOIN vendors v ON v.uid=a.vendor_uid WHERE `+where+` ORDER BY v.id,a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Binding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (w *configSet) upstreamBindings(where string, args ...any) ([]Binding, error) {
	return queryBindings(w.q, where, args...)
}

// Bindings lists the mirrored applications of an upstream, or of every
// upstream when upstreamUID is empty.
func (s *Store) Bindings(upstreamUID string) ([]Binding, error) {
	if upstreamUID == "" {
		return queryBindings(s.read, `1`)
	}
	return queryBindings(s.read, `b.upstream_uid=?`, upstreamUID)
}

// Binding returns the binding of a mirrored application, or ErrMirrorNotFound.
func (s *Store) Binding(appKey string) (Binding, error) {
	out, err := queryBindings(s.read, `v.id||'/'||a.id=?`, appKey)
	if err != nil {
		return Binding{}, err
	}
	if len(out) == 0 {
		return Binding{}, ErrMirrorNotFound
	}
	return out[0], nil
}

// MirroredFrom returns the remote instance and chain an application mirrors,
// so that its own mirror description can extend the chain; ok is false for
// an application that is not mirrored.
func (s *Store) MirroredFrom(appUID string) (instanceID string, chain []string, ok bool, err error) {
	var raw []byte
	err = s.read.QueryRow(`SELECT u.instance_id,b.upstream_chain_json FROM application_upstreams b JOIN upstreams u ON u.uid=b.upstream_uid WHERE b.app_uid=?`, appUID).Scan(&instanceID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &chain)
	}
	return instanceID, chain, err == nil, err
}

func setMirrorSource(spec Object, provider, source string) {
	switch provider {
	case "http-cache":
		spec["base_urls"], spec["source_strategy"] = []any{source}, "ordered"
	case "codex", "claude-code":
		spec["base_url"] = source
	}
}

// mirrorTemplate is the template a mirrored application follows. The upstream
// application's root is its source.
func mirrorTemplate(appUID, upstreamUID, remoteKey, provider, origin string, m MirrorSnapshot) templateSnapshot {
	spec := Object{
		"provider":     provider,
		"name":         map[string]any{"en": m.Name.En, "zh-CN": m.Name.ZhCN},
		"description":  map[string]any{"en": m.Description.En, "zh-CN": m.Description.ZhCN},
		"instructions": map[string]any{"en": m.Instructions.En, "zh-CN": m.Instructions.ZhCN},
		"icon":         m.Icon,
		// Local settings are not followed; the proxy, like every template's,
		// defaults to inheritance.
		"proxy": map[string]any{"mode": "inherit"},
	}
	if presets.AutoIndexProvider(provider) {
		spec["auto_index"] = m.AutoIndex
	}
	setMirrorSource(spec, provider, origin+"/"+remoteKey)
	spec = object(spec)
	metadata := object(map[string]any{"upstream": upstreamUID, "remote_key": remoteKey})
	return templateSnapshot{Kind: "App", Key: UpstreamTemplateRef(appUID), Schema: 1, Metadata: metadata, Spec: spec, Hash: semanticHash(1, "App", metadata, spec), Present: true}
}

// mirroredApp loads a live mirrored application and its template.
func (w *configSet) mirroredApp(appUID string) (*appEntry, *templateSnapshot, error) {
	a, err := w.app(appUID)
	if err != nil {
		return nil, nil, err
	}
	if a == nil || !isUpstreamRef(a.config.Ref) {
		return nil, nil, ErrMirrorNotFound
	}
	t, err := w.template("App", *a.config.Ref)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return nil, nil, ErrMirrorNotFound
	}
	return a, t, nil
}

// BindRequest mirrors an upstream application as the local application
// Vendor/ID, created now or, with Adopt, an existing one of the same
// provider at Revision.
type BindRequest struct {
	UpstreamUID string
	RemoteKey   string
	Vendor      string
	ID          string
	Provider    string // local provider
	// NewVendor creates the vendor when it does not exist yet.
	NewVendor      *VendorInput
	Adopt          bool
	Revision       int64
	Snapshot       MirrorSnapshot
	RemoteRevision string
	RemoteIcon     string
	Chain          []string
	At             time.Time
}

// BindApplication creates or adopts a mirrored application. An adopted
// application keeps its local settings, but its display follows the upstream
// and its source becomes the upstream application.
func (s *Store) BindApplication(in BindRequest) (Application, error) {
	key := in.Vendor + "/" + in.ID
	if _, err := identityKey(key); err != nil {
		return Application{}, err
	}
	if in.Chain == nil {
		in.Chain = []string{}
	}
	var appUID string
	err := s.writeConfiguration(func(w *configSet) error {
		var origin string
		if err := w.q.QueryRow(`SELECT origin FROM upstreams WHERE uid=?`, in.UpstreamUID).Scan(&origin); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUpstreamNotFound
			}
			return err
		}
		var bound int
		if err := w.q.QueryRow(`SELECT count(*) FROM application_upstreams WHERE upstream_uid=? AND remote_key=?`, in.UpstreamUID, in.RemoteKey).Scan(&bound); err != nil {
			return err
		}
		if bound != 0 {
			return ErrMirrorExists
		}
		vendor, err := w.vendorByID(in.Vendor)
		if err != nil {
			return err
		}
		if vendor == nil {
			if in.NewVendor == nil {
				return ErrVendorNotFound
			}
			v := *in.NewVendor
			v.ID, v.Enabled = in.Vendor, true
			if err = validateVendor(v); err != nil {
				return err
			}
			uid, err := identity.NewUID()
			if err != nil {
				return err
			}
			created := Vendor{UID: uid, ID: v.ID, Name: v.Name, Description: v.Description, Icon: v.Icon, LocalizedIcons: v.LocalizedIcons, Enabled: true, Revision: 1}
			vendor = w.addVendor(created, ownedConfig{Overrides: Object{}, Spec: vendorSpec(created)})
		} else if vendor.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		existing, err := w.appByKey(key)
		if err != nil {
			return err
		}
		if existing != nil {
			if !in.Adopt {
				return ErrDirectoryExists
			}
			if existing.DeletedAt != nil {
				return ErrDirectoryDeleted
			}
			if existing.Revision != in.Revision {
				return ErrConflict
			}
			if existing.Provider != in.Provider {
				return invalidf("the upstream application uses another provider")
			}
			if isUpstreamRef(existing.config.Ref) {
				return ErrMirrorExists
			}
			// Local settings keep their current values, including those a
			// built-in template supplied; display and source follow the upstream.
			effective, err := w.effective("App", existing.config)
			if err != nil {
				return err
			}
			over := independentOverrides(effective, existing.Provider)
			for _, path := range mirroredDisplayPaths {
				unsetLeaf(over, path)
			}
			for path := range upstreamSourcePaths {
				unsetLeaf(over, path)
			}
			appUID = existing.UID
			ref := UpstreamTemplateRef(appUID)
			existing.config = ownedConfig{ProviderKey: existing.Provider, Ref: &ref, Overrides: over}
			existing.Revision++
			existing.changed = true
		} else {
			if in.Adopt {
				return ErrApplicationNotFound
			}
			if appUID, err = identity.NewUID(); err != nil {
				return err
			}
			ref := UpstreamTemplateRef(appUID)
			a := Application{Categories: []string{}, Tags: []string{}, UID: appUID, ID: in.ID, Key: key, VendorID: in.Vendor, VendorUID: vendor.UID, Name: in.Snapshot.Name, Description: in.Snapshot.Description, Provider: in.Provider, Enabled: true, Revision: 1, SourceEpoch: 1}
			w.addApp(a, ownedConfig{ProviderKey: in.Provider, Ref: &ref, Overrides: Object{}})
		}
		w.putTemplate(mirrorTemplate(appUID, in.UpstreamUID, in.RemoteKey, in.Provider, origin, in.Snapshot))
		return nil
	}, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO application_upstreams(app_uid,upstream_uid,remote_key,remote_revision,remote_icon,upstream_chain_json,synced_at_s,checked_at_s,error_code) VALUES(?,?,?,?,?,?,?,?,'')`, appUID, in.UpstreamUID, in.RemoteKey, in.RemoteRevision, in.RemoteIcon, string(encode(in.Chain)), in.At.Unix(), in.At.Unix())
		return err
	})
	if err != nil {
		return Application{}, err
	}
	return s.Application(key)
}

func identityKey(key string) (string, error) {
	if !identity.ValidKey(key) {
		return "", invalidf("invalid application identity")
	}
	return key, nil
}

// MirrorUpdate is the result of one check of a mirrored application.
type MirrorUpdate struct {
	// Snapshot is the refreshed upstream state; nil when the check failed.
	Snapshot *MirrorSnapshot
	// Gone marks the upstream application as no longer published there.
	Gone           bool
	RemoteRevision string
	RemoteIcon     string
	Chain          []string
	ErrorCode      string
	At             time.Time
}

// UpdateMirror records a check. A changed snapshot replaces the template; a
// gone upstream application marks it missing and keeps the last values.
// Either changes the application revision, as a template update does.
func (s *Store) UpdateMirror(appUID string, u MirrorUpdate) error {
	if u.Snapshot == nil && !u.Gone {
		result, err := s.db.Exec(`UPDATE application_upstreams SET checked_at_s=?,error_code=? WHERE app_uid=?`, u.At.Unix(), u.ErrorCode, appUID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ErrMirrorNotFound
		}
		return nil
	}
	if u.Chain == nil {
		u.Chain = []string{}
	}
	return s.writeConfiguration(func(w *configSet) error {
		a, t, err := w.mirroredApp(appUID)
		if err != nil {
			return err
		}
		if a.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		next := *t
		if u.Gone {
			next.Present = false
		} else {
			var origin, upstreamUID, remoteKey string
			if err = w.q.QueryRow(`SELECT u.origin,u.uid,b.remote_key FROM application_upstreams b JOIN upstreams u ON u.uid=b.upstream_uid WHERE b.app_uid=?`, appUID).Scan(&origin, &upstreamUID, &remoteKey); err != nil {
				return err
			}
			next = mirrorTemplate(appUID, upstreamUID, remoteKey, a.Provider, origin, *u.Snapshot)
		}
		if next.Hash != t.Hash || next.Present != t.Present {
			w.putTemplate(next)
			a.Revision++
			a.changed = true
		}
		return nil
	}, func(tx *sql.Tx) error {
		if u.Gone {
			_, err := tx.Exec(`UPDATE application_upstreams SET checked_at_s=?,error_code=? WHERE app_uid=?`, u.At.Unix(), u.ErrorCode, appUID)
			return err
		}
		_, err := tx.Exec(`UPDATE application_upstreams SET remote_revision=?,remote_icon=?,upstream_chain_json=?,synced_at_s=?,checked_at_s=?,error_code='' WHERE app_uid=?`, u.RemoteRevision, u.RemoteIcon, string(encode(u.Chain)), u.At.Unix(), u.At.Unix(), appUID)
		return err
	})
}

// UnbindApplication turns a mirrored application into an ordinary one that
// keeps its current values, including its source. An application with a
// built-in template is linked to it again, as startup does for any
// independent application of that key, with every current value kept as an
// override.
func (s *Store) UnbindApplication(key string, revision int64) (Application, error) {
	var appUID string
	err := s.writeConfiguration(func(w *configSet) error {
		a, err := w.appByKey(key)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrApplicationNotFound
		}
		if !isUpstreamRef(a.config.Ref) {
			return ErrMirrorNotFound
		}
		if a.DeletedAt != nil {
			return ErrDirectoryDeleted
		}
		if a.Revision != revision {
			return ErrConflict
		}
		effective, err := w.effective("App", a.config)
		if err != nil {
			return err
		}
		appUID = a.UID
		a.config = ownedConfig{ProviderKey: a.Provider, Overrides: Object{}, Spec: effective}
		if t, err := w.template("App", a.Key); err != nil {
			return err
		} else if t != nil && t.Present && fmt.Sprint(t.Spec["provider"]) == a.Provider {
			ref := a.Key
			a.config = ownedConfig{ProviderKey: a.Provider, Ref: &ref, Overrides: independentOverrides(effective, a.Provider)}
		}
		a.Revision++
		a.changed = true
		return nil
	}, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM application_upstreams WHERE app_uid=?`, appUID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM template_snapshots WHERE kind='App' AND canonical_key=?`, UpstreamTemplateRef(appUID))
		return err
	})
	if err != nil {
		return Application{}, err
	}
	return s.Application(key)
}
