package upstream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/logging"
	"github.com/PMExtra/RedApp/internal/media"
	"github.com/PMExtra/RedApp/internal/store"
)

// SyncInterval separates two automatic checks of every mirrored application.
const SyncInterval = 15 * time.Minute

// Service mirrors applications of upstream RedApp instances.
type Service struct {
	db          *store.Store
	pool        *distributor.Pool
	icons       *media.Store
	instanceUID string
	now         func() time.Time
	log         *slog.Logger
	// syncMu serializes checks, so the loop and a manual check of the same
	// application never record results out of order.
	syncMu sync.Mutex
}

type Option func(*Service)

// WithClock replaces the clock of check times (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger; records carry component=upstream.
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

func New(db *store.Store, pool *distributor.Pool, icons *media.Store, options ...Option) (*Service, error) {
	if db == nil || pool == nil || icons == nil {
		return nil, errors.New("upstream: store, pool and icons are required")
	}
	uid, err := db.InstanceUID()
	if err != nil {
		return nil, err
	}
	s := &Service{db: db, pool: pool, icons: icons, instanceUID: uid, now: time.Now}
	for _, option := range options {
		option(s)
	}
	s.log = logging.For(s.log, "upstream")
	return s, nil
}

// verify reads the identity of the instance at origin, which must not be
// this instance.
func (s *Service) verify(ctx context.Context, origin string) (client, Instance, error) {
	c, err := s.client(origin)
	if err != nil {
		return c, Instance{}, err
	}
	instance, err := c.instance(ctx)
	if err != nil {
		return c, instance, err
	}
	if instance.InstanceID == s.instanceUID {
		return c, instance, ErrLoop
	}
	return c, instance, nil
}

// Create verifies and records an upstream.
func (s *Service) Create(ctx context.Context, name, origin string) (store.Upstream, error) {
	origin, err := NormalizeOrigin(origin)
	if err != nil {
		return store.Upstream{}, fmt.Errorf("%w: %w", store.ErrInvalidDirectory, err)
	}
	_, instance, err := s.verify(ctx, origin)
	if err != nil {
		return store.Upstream{}, err
	}
	return s.db.CreateUpstream(strings.TrimSpace(name), origin, instance.InstanceID, s.now())
}

// Update renames an upstream or moves it, verifying the address again. An
// address of another instance is accepted: the next checks of its mirrored
// applications report what that instance publishes.
func (s *Service) Update(ctx context.Context, uid string, revision int64, name, origin string) (store.Upstream, error) {
	current, err := s.db.Upstream(uid)
	if err != nil {
		return current, err
	}
	origin, err = NormalizeOrigin(origin)
	if err != nil {
		return store.Upstream{}, fmt.Errorf("%w: %w", store.ErrInvalidDirectory, err)
	}
	instanceID := current.InstanceID
	if origin != current.Origin {
		_, instance, err := s.verify(ctx, origin)
		if err != nil {
			return store.Upstream{}, err
		}
		instanceID = instance.InstanceID
	}
	return s.db.UpdateUpstream(uid, revision, strings.TrimSpace(name), origin, instanceID)
}

// localProvider is the provider a mirror of an upstream application uses:
// hosted files are mirrored through the HTTP cache. JSON releases are not
// mirrored yet.
func localProvider(remote string) (string, bool) {
	switch remote {
	case "info", "http-cache", "codex", "claude-code":
		return remote, true
	case "hosted":
		return "http-cache", true
	}
	return "", false
}

// CatalogEntry is a remote application with its local mirror, if any.
type CatalogEntry struct {
	RemoteApp
	// Supported is false for applications this instance cannot mirror.
	Supported bool
	// Mirror is the local application mirroring it, or "".
	Mirror string
}

// CatalogPage is one page of an upstream catalog.
type CatalogPage struct {
	Items      []CatalogEntry
	Page       int
	Limit      int
	Total      int64
	TotalPages int
}

// Catalog reads one page of an upstream's published applications.
func (s *Service) Catalog(ctx context.Context, uid, query string, page, limit int) (CatalogPage, error) {
	u, err := s.db.Upstream(uid)
	if err != nil {
		return CatalogPage{}, err
	}
	c, err := s.client(u.Origin)
	if err != nil {
		return CatalogPage{}, err
	}
	remote, err := c.catalog(ctx, query, page, limit)
	if err != nil {
		return CatalogPage{}, err
	}
	bindings, err := s.db.Bindings(uid)
	if err != nil {
		return CatalogPage{}, err
	}
	mirrors := map[string]string{}
	for _, b := range bindings {
		mirrors[b.RemoteKey] = b.AppKey
	}
	out := CatalogPage{Items: []CatalogEntry{}, Page: remote.Page, Limit: remote.Limit, Total: remote.Total, TotalPages: remote.TotalPages}
	for _, item := range remote.Items {
		_, supported := localProvider(item.Provider)
		out.Items = append(out.Items, CatalogEntry{RemoteApp: RemoteApp{Key: item.Key, Provider: item.Provider, Name: item.Name, Description: item.Description, Icon: item.Icon}, Supported: supported, Mirror: mirrors[item.Key]})
	}
	return out, nil
}

// checkChain refuses an upstream application whose chain contains this
// instance: mirroring it would make the chain a loop.
func (s *Service) checkChain(m mirrorApp) error {
	if m.InstanceID == s.instanceUID || slices.Contains(m.UpstreamChain, s.instanceUID) {
		return ErrLoop
	}
	return nil
}

func snapshotOf(m mirrorApp, icon string) store.MirrorSnapshot {
	return store.MirrorSnapshot{
		Name:         store.LocalizedText{En: m.Name.En, ZhCN: m.Name.ZhCN},
		Description:  store.LocalizedText{En: m.Description.En, ZhCN: m.Description.ZhCN},
		Instructions: store.LocalizedText{En: m.Instructions.En, ZhCN: m.Instructions.ZhCN},
		Icon:         icon,
		AutoIndex:    m.AutoIndex,
	}
}

// BindInput selects an upstream application and the local application that
// mirrors it: a new one, or with Adopt an existing one of the same provider.
type BindInput struct {
	RemoteKey string
	Vendor    string
	ID        string
	Adopt     bool
	Revision  int64
}

// Bind mirrors an upstream application. The local vendor is created from the
// upstream's vendor when it does not exist.
func (s *Service) Bind(ctx context.Context, uid string, in BindInput) (store.Application, error) {
	u, err := s.db.Upstream(uid)
	if err != nil {
		return store.Application{}, err
	}
	c, err := s.client(u.Origin)
	if err != nil {
		return store.Application{}, err
	}
	m, err := c.mirror(ctx, in.RemoteKey)
	if err != nil {
		return store.Application{}, err
	}
	if m.InstanceID != u.InstanceID {
		return store.Application{}, fmt.Errorf("%w: the address now serves another instance; save the upstream again to verify it", ErrIncompatible)
	}
	if err = s.checkChain(m); err != nil {
		return store.Application{}, err
	}
	provider, ok := localProvider(m.Provider)
	if !ok {
		return store.Application{}, fmt.Errorf("%w: %s applications cannot be mirrored", ErrIncompatible, m.Provider)
	}
	icon, err := s.icon(ctx, c, m.Icon)
	if err != nil {
		s.log.Warn("upstream icon not copied", slog.String("app", in.Vendor+"/"+in.ID), logging.Error(err))
		icon = ""
	}
	request := store.BindRequest{UpstreamUID: uid, RemoteKey: in.RemoteKey, Vendor: in.Vendor, ID: in.ID, Provider: provider, Adopt: in.Adopt, Revision: in.Revision,
		Snapshot: snapshotOf(m, icon), RemoteRevision: m.Revision, RemoteIcon: m.Icon, Chain: m.UpstreamChain, At: s.now()}
	if _, err := s.db.Vendor(in.Vendor); errors.Is(err, store.ErrNotFound) {
		vendorIcon, err := s.icon(ctx, c, m.Vendor.Icon)
		if err != nil {
			s.log.Warn("upstream vendor icon not copied", slog.String("vendor", in.Vendor), logging.Error(err))
			vendorIcon = ""
		}
		request.NewVendor = &store.VendorInput{Name: store.LocalizedText{En: m.Vendor.Name.En, ZhCN: m.Vendor.Name.ZhCN}, Description: store.LocalizedText{En: m.Vendor.Description.En, ZhCN: m.Vendor.Description.ZhCN}, Icon: vendorIcon}
		if request.NewVendor.Name.En == "" {
			request.NewVendor.Name = store.LocalizedText{En: in.Vendor, ZhCN: in.Vendor}
		}
	} else if err != nil {
		return store.Application{}, err
	}
	a, err := s.db.BindApplication(request)
	if err != nil {
		return a, err
	}
	s.log.Info("application mirrored", slog.String("app", a.Key), slog.String("upstream", u.Origin), slog.String("remote", in.RemoteKey))
	return a, nil
}

// ErrorCode is the error code a failed check records.
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrRemoteNotFound):
		return "APPLICATION_NOT_FOUND"
	case errors.Is(err, ErrLoop):
		return "LOOP_DETECTED"
	case errors.Is(err, ErrIncompatible), errors.Is(err, store.ErrInvalidDirectory):
		return "UPSTREAM_INCOMPATIBLE"
	default:
		return "UPSTREAM_UNAVAILABLE"
	}
}

// Sync checks one mirrored application now and returns its binding.
func (s *Service) Sync(ctx context.Context, appKey string) (store.Binding, error) {
	b, err := s.db.Binding(appKey)
	if err != nil {
		return b, err
	}
	s.syncMu.Lock()
	err = s.check(ctx, b)
	s.syncMu.Unlock()
	if err != nil {
		return b, err
	}
	return s.db.Binding(appKey)
}

// check refreshes one binding and records the outcome. It returns only
// failures to record it; an upstream failure is part of the outcome.
func (s *Service) check(ctx context.Context, b store.Binding) error {
	at := s.now()
	u, err := s.db.Upstream(b.UpstreamUID)
	if err != nil {
		return err
	}
	c, err := s.client(u.Origin)
	if err != nil {
		return s.db.UpdateMirror(b.AppUID, store.MirrorUpdate{ErrorCode: ErrorCode(err), At: at})
	}
	m, err := c.mirror(ctx, b.RemoteKey)
	if err == nil && m.InstanceID != u.InstanceID {
		err = fmt.Errorf("%w: the address now serves another instance", ErrIncompatible)
	}
	if err == nil {
		err = s.checkChain(m)
	}
	if err == nil {
		if provider, ok := localProvider(m.Provider); !ok || provider != b.Provider {
			err = fmt.Errorf("%w: the upstream application changed its provider", ErrIncompatible)
		}
	}
	if errors.Is(err, ErrRemoteNotFound) {
		s.log.Warn("upstream application gone", slog.String("app", b.AppKey), slog.String("remote", b.RemoteKey))
		return s.db.UpdateMirror(b.AppUID, store.MirrorUpdate{Gone: true, ErrorCode: ErrorCode(err), At: at})
	}
	if err != nil {
		s.log.Warn("upstream check failed", slog.String("app", b.AppKey), logging.Error(err))
		return s.db.UpdateMirror(b.AppUID, store.MirrorUpdate{ErrorCode: ErrorCode(err), At: at})
	}
	if m.Revision == b.RemoteRevision && b.Available {
		return s.db.UpdateMirror(b.AppUID, store.MirrorUpdate{At: at})
	}
	icon := b.Icon
	if m.Icon != b.RemoteIcon || !b.Available {
		if icon, err = s.icon(ctx, c, m.Icon); err != nil {
			s.log.Warn("upstream icon not copied", slog.String("app", b.AppKey), logging.Error(err))
			icon = ""
		}
	}
	snapshot := snapshotOf(m, icon)
	return s.db.UpdateMirror(b.AppUID, store.MirrorUpdate{Snapshot: &snapshot, RemoteRevision: m.Revision, RemoteIcon: m.Icon, Chain: m.UpstreamChain, At: at})
}

// SyncAll checks every mirrored application once.
func (s *Service) SyncAll(ctx context.Context) {
	bindings, err := s.db.Bindings("")
	if err != nil {
		s.log.Error("mirror check failed", logging.Error(err))
		return
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	for _, b := range bindings {
		if ctx.Err() != nil {
			return
		}
		if err := s.check(ctx, b); err != nil && !errors.Is(err, store.ErrDirectoryDeleted) && !errors.Is(err, store.ErrMirrorNotFound) {
			s.log.Error("mirror check not recorded", slog.String("app", b.AppKey), logging.Error(err))
		}
	}
}

// Run checks every mirrored application every interval until ctx ends. The
// first check runs one interval after the start, like the other maintenance.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncAll(ctx)
		}
	}
}
