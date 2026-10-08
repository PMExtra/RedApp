package store

import (
	"sort"
	"time"
)

// Candidates use association indexes; popularity reuses the existing 168-hour sketch.
func (s *Store) RelatedApplications(uid string, now time.Time) ([]string, error) {
	rows, err := s.DB.Query(`SELECT a.uid,v.id||'/'||a.id,count(*) FROM application_tags peer JOIN application_tags own ON own.tag_id=peer.tag_id AND own.app_uid=? JOIN applications a ON a.uid=peer.app_uid JOIN vendors v ON v.uid=a.vendor_uid WHERE peer.app_uid<>? AND a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL GROUP BY a.uid,v.id,a.id`, uid, uid)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		uid, key   string
		common     int
		popularity int64
	}
	items := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.uid, &item.key, &item.common); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []string{}
	if len(items) == 0 {
		return result, nil
	}
	ranking, err := s.DownloadRanking(now, 0)
	if err != nil {
		return nil, err
	}
	scores := map[string]int64{}
	for _, item := range ranking {
		scores[item.UID] = item.Clients
	}
	for i := range items {
		items[i].popularity = scores[items[i].uid]
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.common != b.common {
			return a.common > b.common
		}
		if a.popularity != b.popularity {
			return a.popularity > b.popularity
		}
		return a.key < b.key
	})
	for _, item := range items[:min(6, len(items))] {
		result = append(result, item.key)
	}
	return result, nil
}
