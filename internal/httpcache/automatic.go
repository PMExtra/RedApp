package httpcache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/store"
)

const CleanupInterval = 15 * time.Minute
const CleanupScanLimit = 1000
const CleanupRetireLimit = 100

type AutomaticCleanupStatus struct {
	Running           bool       `json:"running"`
	IntervalSeconds   int64      `json:"interval_seconds"`
	ScanLimitPerApp   int        `json:"scan_limit_per_app"`
	RetireLimitPerApp int        `json:"retire_limit_per_app"`
	LastAttemptAt     *time.Time `json:"last_attempt_at"`
	LastSuccessAt     *time.Time `json:"last_success_at"`
	LastErrorAt       *time.Time `json:"last_error_at"`
	LastError         string     `json:"last_error"`
	PassesTotal       int64      `json:"passes_total"`
	FailuresTotal     int64      `json:"failures_total"`
	ConfiguredApps    int        `json:"configured_apps"`
	ScannedFiles      int        `json:"scanned_files"`
	RetiredFiles      int        `json:"retired_files"`
	SkippedAccessed   int        `json:"skipped_accessed"`
	SkippedChanged    int        `json:"skipped_changed"`
	RetiredBytes      int64      `json:"retired_bytes"`
}

type cleanupCursor struct {
	Revision int64
	After    int64
}

func (s *Service) CleanupStatus() AutomaticCleanupStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.cleanupStatus
	status.IntervalSeconds = int64(CleanupInterval / time.Second)
	status.ScanLimitPerApp = CleanupScanLimit
	status.RetireLimitPerApp = CleanupRetireLimit
	// Return independent timestamps so API callers cannot mutate shared state.
	if status.LastAttemptAt != nil {
		value := *status.LastAttemptAt
		status.LastAttemptAt = &value
	}
	if status.LastSuccessAt != nil {
		value := *status.LastSuccessAt
		status.LastSuccessAt = &value
	}
	if status.LastErrorAt != nil {
		value := *status.LastErrorAt
		status.LastErrorAt = &value
	}
	return status
}

// RunCleanup runs one serial scheduler. It intentionally waits for the first
// interval, and an empty policy never selects cached files for deletion.
func (s *Service) RunCleanup(ctx context.Context, registry *application.Registry, onError func(error)) {
	s.mu.Lock()
	if s.closed || s.cleanupStatus.Running || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.cleanupStatus.Running = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.cleanupStatus.Running = false; s.mu.Unlock(); s.wg.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	tick := time.NewTicker(CleanupInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.cleanupPass(ctx, registry, onError)
		}
	}
}

func (s *Service) cleanupPass(ctx context.Context, registry *application.Registry, onError func(error)) {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	now := s.now().UTC()
	s.mu.Lock()
	status := s.cleanupStatus
	status.LastAttemptAt = &now
	status.PassesTotal++
	status.ConfiguredApps = 0
	status.ScannedFiles = 0
	status.RetiredFiles = 0
	status.SkippedAccessed = 0
	status.SkippedChanged = 0
	status.RetiredBytes = 0
	s.mu.Unlock()
	failed := false
	if err := s.prunePreviews(ctx, 4); err != nil && !errors.Is(err, context.Canceled) {
		failed = true
		status.FailuresTotal++
		at := s.now().UTC()
		status.LastErrorAt = &at
		status.LastError = "HTTP cache preview metadata cleanup failed"
		if onError != nil {
			onError(err)
		}
	}
	alive := map[string]bool{}
	for _, entry := range registry.Entries() {
		if ctx.Err() != nil {
			break
		}
		if entry.Provider != application.HttpCache || !entry.Active() {
			continue
		}
		alive[entry.StorageID()] = true
		config, revision, err := s.db.ReadHTTPPolicy(entry.Descriptor.ID)
		if err == nil && revision != entry.Revision {
			err = store.ErrSourceInactive
		}
		if err == nil && len(config.AutoCleanup) == 0 {
			delete(s.cleanupCursors, entry.StorageID())
			continue
		}
		var policy *cachepolicy.Policy
		if err == nil {
			policy, err = cachepolicy.Compile(config)
		}
		if err == nil {
			status.ConfiguredApps++
			cursor := s.cleanupCursors[entry.StorageID()]
			if cursor.Revision != entry.Revision {
				cursor = cleanupCursor{Revision: entry.Revision}
			}
			var scanned int
			var next int64
			var jobID string
			jobID, scanned, next, err = s.previewAutomatic(ctx, entry, policy, cursor.After)
			status.ScannedFiles += scanned
			if err == nil && jobID != "" {
				var result CleanupResult
				result, err = s.ExecuteCleanup(ctx, entry, jobID)
				status.RetiredFiles += result.RetiredFiles
				status.RetiredBytes += result.RetiredBytes
				status.SkippedAccessed += result.SkippedAccessed
				status.SkippedChanged += result.SkippedChanged
			}
			if err == nil {
				s.cleanupCursors[entry.StorageID()] = cleanupCursor{Revision: entry.Revision, After: next}
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			failed = true
			status.FailuresTotal++
			at := s.now().UTC()
			status.LastErrorAt = &at
			status.LastError = "Automatic HTTP cache cleanup failed"
			_ = s.db.RecordEvent(store.Event{AppID: entry.MetricsID(), Category: "cleanup", Code: "automatic_cleanup_failed", Message: "Automatic HTTP cache cleanup failed; it will retry on the next scheduled pass"})
			if onError != nil {
				onError(fmt.Errorf("automatic cleanup for %s: %w", entry.Descriptor.ID, err))
			}
		}
	}
	for id := range s.cleanupCursors {
		if !alive[id] {
			delete(s.cleanupCursors, id)
		}
	}
	if ctx.Err() == nil && !failed {
		at := s.now().UTC()
		status.LastSuccessAt = &at
		status.LastError = ""
	}
	s.mu.Lock()
	status.Running = s.cleanupStatus.Running
	s.cleanupStatus = status
	s.mu.Unlock()
}

// previewAutomatic uses the same persisted, paged frozen items as manual
// cleanup while bounding one scheduled app pass to 1000 scans / 100 selections.
func (s *Service) previewAutomatic(ctx context.Context, entry application.Entry, policy *cachepolicy.Policy, after int64) (string, int, int64, error) {
	preview, err := s.buildPreview(ctx, entry, "cleanup", PreviewCriteria{Match: pathmatch.Spec{Type: "glob", Pattern: "/"}, Automatic: true}, buildOptions{after: after, scanLimit: CleanupScanLimit, selectLimit: CleanupRetireLimit, policy: policy})
	if err != nil {
		return "", preview.ScannedFiles, after, err
	}
	next := preview.lastScanned
	if preview.ScannedFiles < CleanupScanLimit && preview.SelectedFiles < CleanupRetireLimit {
		next = 0
	}
	if preview.SelectedFiles == 0 {
		// Empty automatic passes do not accumulate durable empty jobs.
		_, err = s.db.DB.ExecContext(ctx, `DELETE FROM http_cleanup_previews WHERE id=? AND selected_count=0 AND state='ready'`, preview.ID)
		return "", preview.ScannedFiles, next, err
	}
	return preview.ID, preview.ScannedFiles, next, nil
}
