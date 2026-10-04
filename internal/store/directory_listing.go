package store

import (
	"database/sql"
	"fmt"
)

type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func NewPage[T any](page, limit int, total int64) Page[T] {
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	return Page[T]{Items: []T{}, Page: page, Limit: limit, Total: total, TotalPages: pages}
}

type VendorCard struct {
	Vendor
	Apps     []Application `json:"apps"`
	AppTotal int64         `json:"app_total"`
}

const vendorMatch = `(instr(lower(v.id),lower(?))>0 OR instr(lower(v.name_en),lower(?))>0 OR instr(lower(v.name_zh_cn),lower(?))>0)`
const appMatch = `(instr(lower(a.id),lower(?))>0 OR instr(lower(v.id||'/'||a.id),lower(?))>0 OR instr(lower(a.name_en),lower(?))>0 OR instr(lower(a.name_zh_cn),lower(?))>0)`

func appState(state string) string {
	switch state {
	case "deleted":
		return `(a.deleted_at_s IS NOT NULL OR v.deleted_at_s IS NOT NULL)`
	case "disabled":
		return `(a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL AND (a.enabled=0 OR v.enabled=0))`
	default:
		return `(a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL)`
	}
}
func validDirectoryPage(page, limit int, state string) bool {
	return page >= 1 && page <= 1000000000 && limit >= 1 && limit <= 100 && (state == "current" || state == "disabled" || state == "deleted")
}
func (s *Store) DirectoryPage(page, limit int, q, state string) (Page[VendorCard], error) {
	if !validDirectoryPage(page, limit, state) {
		return Page[VendorCard]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[VendorCard]{}, err
	}
	defer tx.Rollback()
	condition := "v.deleted_at_s IS NULL"
	if state == "disabled" {
		condition = `v.deleted_at_s IS NULL AND (v.enabled=0 OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND a.deleted_at_s IS NULL AND a.enabled=0))`
	}
	if state == "deleted" {
		condition = `(v.deleted_at_s IS NOT NULL OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND a.deleted_at_s IS NOT NULL))`
	}
	args := []any{}
	if q != "" {
		condition += ` AND (` + vendorMatch + ` OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND ` + appState(state) + ` AND ` + appMatch + `))`
		args = append(args, q, q, q, q, q, q, q)
	}
	var total int64
	if err = tx.QueryRow(`SELECT COUNT(*) FROM vendors v WHERE `+condition, args...).Scan(&total); err != nil {
		return Page[VendorCard]{}, err
	}
	result := NewPage[VendorCard](page, limit, total)
	queryArgs := append(append([]any{}, args...), limit, (result.Page-1)*limit)
	rows, err := tx.Query(`SELECT `+vendorColumns+` FROM vendors v WHERE `+condition+` ORDER BY v.id LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scanVendor(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, VendorCard{Vendor: v})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for i := range result.Items {
		// Search filters before this five-item preview, so an original sixth item
		// can never disappear behind the unfiltered preview.
		apps, e := applicationPage(tx, result.Items[i].ID, 1, 5, q, state)
		if e != nil {
			return result, e
		}
		result.Items[i].Apps = apps.Items
		result.Items[i].AppTotal = apps.Total
	}
	return result, tx.Commit()
}
func (s *Store) ApplicationPage(vendor string, page, limit int, q, state string) (Page[Application], error) {
	if !validDirectoryPage(page, limit, state) {
		return Page[Application]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[Application]{}, err
	}
	defer tx.Rollback()
	result, err := applicationPage(tx, vendor, page, limit, q, state)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func applicationPage(tx *sql.Tx, vendor string, page, limit int, q, state string) (Page[Application], error) {
	where := appState(state)
	args := []any{}
	if vendor != "" {
		where += ` AND v.id=?`
		args = append(args, vendor)
	}
	order := `v.id,a.id`
	if q != "" {
		where += ` AND (` + vendorMatch + ` OR ` + appMatch + `)`
		args = append(args, q, q, q, q, q, q, q)
		// Matching applications precede the ordinary preview when the vendor also matches.
		order = fmt.Sprintf(`CASE WHEN %s THEN 0 ELSE 1 END,v.id,a.id`, appMatch)
	}
	join := ` FROM applications a JOIN vendors v ON v.uid=a.vendor_uid WHERE ` + where
	var total int64
	if err := tx.QueryRow(`SELECT COUNT(*)`+join, args...).Scan(&total); err != nil {
		return Page[Application]{}, err
	}
	result := NewPage[Application](page, limit, total)
	if q != "" {
		args = append(args, q, q, q, q)
	}
	args = append(args, limit, (result.Page-1)*limit)
	rows, err := tx.Query(`SELECT `+applicationColumns+join+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		a, e := scanApplication(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, a)
	}
	return result, rows.Err()
}
