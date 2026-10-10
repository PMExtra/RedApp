package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
	"net"
	"sort"
	"time"
)

const sketchSize = 1024
const rankingHours = 168

// Only fixed-size registers are persisted, never IP addresses or their hashes.
// Hourly buckets use a conservative rolling window, excluding the oldest partial hour.
func (s *Store) RecordDownload(uid, client string, now time.Time) error {
	ip := net.ParseIP(client)
	if ip == nil || uid == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var live bool
	if err = tx.QueryRow(`SELECT a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL FROM applications a JOIN vendors v ON v.uid=a.vendor_uid WHERE a.uid=?`, uid).Scan(&live); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if !live {
		return nil
	}
	var salt []byte
	if err = tx.QueryRow(`SELECT ranking_salt FROM catalog_state WHERE id=1`).Scan(&salt); err != nil {
		return err
	}
	h := hmac.New(sha256.New, salt)
	h.Write([]byte(ip.String()))
	sum := h.Sum(nil)
	hash := binary.BigEndian.Uint64(sum)
	index := hash >> (64 - 10)
	rank := byte(bits.LeadingZeros64((hash<<10)|(1<<9)) + 1)
	hour := now.UTC().Unix() / 3600 * 3600
	registers := make([]byte, sketchSize)
	var saved []byte
	if err = tx.QueryRow(`SELECT registers FROM download_sketches WHERE app_uid=? AND hour_s=?`, uid, hour).Scan(&saved); err == nil {
		copy(registers, saved)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if rank > registers[index] {
		registers[index] = rank
	}
	if _, err = tx.Exec(`INSERT INTO download_sketches(app_uid,hour_s,registers) VALUES(?,?,?) ON CONFLICT(app_uid,hour_s) DO UPDATE SET registers=excluded.registers`, uid, hour, registers); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM download_sketches WHERE hour_s<?`, hour-(rankingHours-1)*3600); err != nil {
		return err
	}
	return tx.Commit()
}
func estimateSketch(registers []byte) int64 {
	total, zeros := 0.0, 0
	for _, v := range registers {
		total += math.Ldexp(1, -int(v))
		if v == 0 {
			zeros++
		}
	}
	m := float64(sketchSize)
	estimate := 0.7213 / (1 + 1.079/m) * m * m / total
	if estimate <= 2.5*m && zeros > 0 {
		estimate = m * math.Log(m/float64(zeros))
	}
	return int64(math.Round(estimate))
}

type RankedApplication struct {
	UID     string `json:"uid"`
	Clients int64  `json:"clients"`
}

func (s *Store) DownloadRanking(now time.Time, limit int) ([]RankedApplication, error) {
	hour := now.UTC().Unix() / 3600 * 3600
	rows, err := s.read.Query(`SELECT d.app_uid,d.registers FROM download_sketches d JOIN applications a ON a.uid=d.app_uid JOIN vendors v ON v.uid=a.vendor_uid WHERE d.hour_s BETWEEN ? AND ? AND a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL ORDER BY d.app_uid`, hour-(rankingHours-1)*3600, hour)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RankedApplication{}
	uid := ""
	merged := make([]byte, sketchSize)
	for rows.Next() {
		var id string
		var registers []byte
		if err = rows.Scan(&id, &registers); err != nil {
			return nil, err
		}
		if id != uid {
			if uid != "" {
				result = append(result, RankedApplication{uid, estimateSketch(merged)})
			}
			uid = id
			clear(merged)
		}
		for i, v := range registers {
			if v > merged[i] {
				merged[i] = v
			}
		}
	}
	if uid != "" {
		result = append(result, RankedApplication{uid, estimateSketch(merged)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Clients == result[j].Clients {
			return result[i].UID < result[j].UID
		}
		return result[i].Clients > result[j].Clients
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type HomepagePins struct {
	Keys     []string `json:"keys"`
	Revision int64    `json:"revision"`
}

func (s *Store) HomepagePins() (HomepagePins, error) {
	out := HomepagePins{Keys: []string{}}
	tx, err := s.read.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT revision FROM catalog_state WHERE id=1`).Scan(&out.Revision); err != nil {
		return out, err
	}
	rows, err := tx.Query(`SELECT v.id||'/'||a.id FROM homepage_pins p JOIN applications a ON a.uid=p.app_uid JOIN vendors v ON v.uid=a.vendor_uid ORDER BY p.position`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return out, err
		}
		out.Keys = append(out.Keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Store) SaveHomepagePins(value HomepagePins) (HomepagePins, error) {
	if len(value.Keys) > 100 {
		return value, ErrInvalidDirectory
	}
	tx, err := s.db.Begin()
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE catalog_state SET revision=revision+1 WHERE id=1 AND revision=?`, value.Revision)
	if err != nil {
		return value, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return value, ErrConflict
	}
	if _, err = tx.Exec(`DELETE FROM homepage_pins`); err != nil {
		return value, err
	}
	seen := map[string]bool{}
	for i, key := range value.Keys {
		if seen[key] {
			return value, ErrInvalidDirectory
		}
		seen[key] = true
		a, e := readApplication(tx, key)
		if e != nil {
			return value, e
		}
		if a.DeletedAt != nil {
			return value, ErrDirectoryDeleted
		}
		if _, err = tx.Exec(`INSERT INTO homepage_pins(app_uid,position) VALUES(?,?)`, a.UID, i); err != nil {
			return value, err
		}
	}
	value.Revision++
	return value, tx.Commit()
}
