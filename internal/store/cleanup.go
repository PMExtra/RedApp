package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// CleanupSelection is one frozen release generation.
type CleanupSelection struct {
	GenerationID  string `json:"generation_id"`
	Version       string `json:"version"`
	ResourceKey   string `json:"resource_key"`
	SnapshotBytes int64  `json:"snapshot_bytes"`
}

// ReleaseItemDetail is the frozen detail of one version of a version cleanup
// or retention preview: the generations execution retires together, and for
// retention why the version is kept or selected.
type ReleaseItemDetail struct {
	Reasons     []string           `json:"reasons,omitempty"`
	Generations []CleanupSelection `json:"generations"`
}

// ReleasePreviewSummary holds the frozen estimates of a release preview.
type ReleasePreviewSummary struct {
	// ReclaimableBytes are the blob bytes freed once no other generation shares them.
	ReclaimableBytes int64    `json:"reclaimable_bytes"`
	UnknownVersions  []string `json:"unknown_versions"`
}

// ReleaseReceipt is the result of executing a release preview.
type ReleaseReceipt struct {
	Selection       []CleanupSelection `json:"selection"`
	Skipped         map[string]string  `json:"skipped"`
	RetiredVersions int                `json:"retired_versions"`
	LogicalBytes    int64              `json:"logical_bytes"`
}

// ReleaseDetail decodes the detail of a release preview item.
func ReleaseDetail(item PreviewItem) (ReleaseItemDetail, error) {
	var detail ReleaseItemDetail
	err := json.Unmarshal(item.Detail, &detail)
	return detail, err
}

// ReleaseSummary decodes the summary of a release preview.
func ReleaseSummary(p Preview) (ReleasePreviewSummary, error) {
	var summary ReleasePreviewSummary
	err := json.Unmarshal(p.Summary, &summary)
	if summary.UnknownVersions == nil {
		summary.UnknownVersions = []string{}
	}
	return summary, err
}

// ReleaseResult decodes the receipt of an executed release preview.
func ReleaseResult(p Preview) (ReleaseReceipt, error) {
	receipt := ReleaseReceipt{Selection: []CleanupSelection{}, Skipped: map[string]string{}}
	if p.State != PreviewDone {
		return receipt, errors.New("release preview has no receipt")
	}
	err := json.Unmarshal(p.Result, &receipt)
	if receipt.Selection == nil {
		receipt.Selection = []CleanupSelection{}
	}
	if receipt.Skipped == nil {
		receipt.Skipped = map[string]string{}
	}
	return receipt, err
}

// releaseGenerationCurrent reports whether a frozen generation is still the
// current generation of the same application, version and resource.
func releaseGenerationCurrent(tx *sql.Tx, storageID string, g CleanupSelection) (bool, error) {
	var app, version, key string
	var current bool
	err := tx.QueryRow(`SELECT app_id,version,resource_key,is_current FROM generations WHERE id=?`, g.GenerationID).Scan(&app, &version, &key, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return app == storageID && version == g.Version && key == g.ResourceKey && current, nil
}

// admitReleaseItems requires every selected generation to be current at creation.
func admitReleaseItems(tx *sql.Tx, p Preview, items []PreviewItem) error {
	seen := map[string]bool{}
	for _, item := range items {
		detail, err := ReleaseDetail(item)
		if err != nil {
			return err
		}
		if item.Selected && len(detail.Generations) == 0 {
			return errors.New("selected release version without generations")
		}
		for _, g := range detail.Generations {
			if g.GenerationID == "" || seen[g.GenerationID] || g.Version != item.Label || g.SnapshotBytes < 0 {
				return errors.New("invalid release preview item")
			}
			seen[g.GenerationID] = true
			current, err := releaseGenerationCurrent(tx, p.StorageID(), g)
			if err != nil {
				return err
			}
			if !current {
				return ErrPreviewStale
			}
		}
	}
	return nil
}

// ExecuteReleasePreview retires the frozen generations of a version cleanup
// or retention preview of storageID's source epoch in one transaction and
// stores the receipt. Only generations that are still current are retired;
// a version without any is skipped as generation_changed. Retention retires
// a version whole or not at all: blocked maps a version to the reason it must
// be kept, and a version any of whose generations changed is skipped. An
// executed preview returns its receipt again without changing anything.
func (s *Store) ExecuteReleasePreview(ctx context.Context, storageID string, kind PreviewKind, id string, blocked map[string]string, at time.Time) (Preview, ReleaseReceipt, error) {
	var receipt ReleaseReceipt
	if kind != PreviewVersionCleanup && kind != PreviewRetention {
		return Preview{}, receipt, errors.New("not a release preview kind")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Preview{}, receipt, err
	}
	defer tx.Rollback()
	p, start, err := beginExecution(ctx, tx, storageID, kind, id, at)
	if err != nil {
		return p, receipt, err
	}
	if !start {
		receipt, err = ReleaseResult(p)
		return p, receipt, err
	}
	items, err := queryPreviewItems(ctx, tx, p.ID, 0, -1, true)
	if err != nil {
		return p, receipt, err
	}
	receipt = ReleaseReceipt{Selection: []CleanupSelection{}, Skipped: map[string]string{}}
	for _, item := range items {
		detail, err := ReleaseDetail(item)
		if err != nil {
			return p, receipt, err
		}
		reason := blocked[item.Label]
		retire := []CleanupSelection{}
		for _, g := range detail.Generations {
			current, err := releaseGenerationCurrent(tx, p.StorageID(), g)
			if err != nil {
				return p, receipt, err
			}
			if current {
				retire = append(retire, g)
			} else if kind == PreviewRetention && reason == "" {
				reason = "generation_changed"
			}
		}
		if len(retire) == 0 && reason == "" {
			reason = "generation_changed"
		}
		outcome := PreviewOutcome{Ordinal: item.Ordinal, Status: "retired"}
		if reason != "" {
			receipt.Skipped[item.Label] = reason
			outcome = PreviewOutcome{Ordinal: item.Ordinal, Status: "skipped", ErrorCode: reason}
		} else {
			for _, g := range retire {
				if _, err = tx.ExecContext(ctx, `UPDATE generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=? AND app_id=?`, at.Unix(), g.GenerationID, p.StorageID()); err != nil {
					return p, receipt, err
				}
				receipt.Selection = append(receipt.Selection, g)
				receipt.LogicalBytes += g.SnapshotBytes
			}
			receipt.RetiredVersions++
		}
		if err = recordPreviewItem(ctx, tx, p.ID, outcome); err != nil {
			return p, receipt, err
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return p, receipt, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE previews SET state='done',executed_at_s=?,result_json=? WHERE id=?`, at.Unix(), raw, p.ID); err != nil {
		return p, receipt, err
	}
	if err = tx.Commit(); err != nil {
		return p, receipt, err
	}
	executed := at.UTC().Truncate(time.Second)
	p.State, p.ExecutedAt, p.Result = PreviewDone, &executed, raw
	p.CompletedItems = len(items)
	return p, receipt, nil
}
