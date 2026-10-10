package store

import (
	"database/sql"
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"strings"
)

type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// NewPage starts a page-style result. A page beyond the last one stays as
// requested and returns no items.
func NewPage[T any](page, limit int, total int64) Page[T] {
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		pages = 1
	}
	return Page[T]{Items: []T{}, Page: page, Limit: limit, Total: total, TotalPages: pages}
}

type VendorCard struct {
	Vendor
	Apps     []Application `json:"apps"`
	AppTotal int64         `json:"app_total"`
}

const vendorMatch = `(instr(lower(v.id),lower(?))>0 OR instr(lower(v.name_en),lower(?))>0 OR instr(lower(v.name_zh_cn),lower(?))>0)`

// Tags match any one tag by literal substring of the folded (NFC, case-folded) form.
const appMatch = `(instr(lower(a.id),lower(?))>0 OR instr(lower(v.id||'/'||a.id),lower(?))>0 OR instr(lower(a.name_en),lower(?))>0 OR instr(lower(a.name_zh_cn),lower(?))>0 OR EXISTS(SELECT 1 FROM application_tags t WHERE t.app_uid=a.uid AND ?<>'' AND instr(t.folded,?)>0))`

func vendorMatchArgs(q string) []any { return []any{q, q, q} }

// appMatchArgs accepts "#tag" input: the display prefix is ignored for tag matching only.
func appMatchArgs(q string) []any {
	tag := presets.FoldText(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(q), "#")))
	return []any{q, q, q, q, tag, tag}
}

func appState(state string) string {
	switch state {
	case "deleted":
		return `(a.deleted_at_s IS NOT NULL OR v.deleted_at_s IS NOT NULL)`
	case "enabled":
		return `(a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL AND a.enabled=1 AND v.enabled=1)`
	case "disabled":
		return `(a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL AND (a.enabled=0 OR v.enabled=0))`
	default:
		return `(a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL)`
	}
}
func validDirectoryPage(page, limit int, state string) bool {
	return page >= 1 && page <= 1000000000 && limit >= 1 && limit <= 100 && (state == "enabled" || state == "current" || state == "disabled" || state == "deleted")
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
	if state == "enabled" {
		condition = `v.deleted_at_s IS NULL AND v.enabled=1 AND EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND a.deleted_at_s IS NULL AND a.enabled=1)`
	}
	if state == "disabled" {
		condition = `v.deleted_at_s IS NULL AND (v.enabled=0 OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND a.deleted_at_s IS NULL AND a.enabled=0))`
	}
	if state == "deleted" {
		condition = `(v.deleted_at_s IS NOT NULL OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND a.deleted_at_s IS NOT NULL))`
	}
	args := []any{}
	if q != "" {
		condition += ` AND (` + vendorMatch + ` OR EXISTS(SELECT 1 FROM applications a WHERE a.vendor_uid=v.uid AND ` + appState(state) + ` AND ` + appMatch + `))`
		args = append(append(args, vendorMatchArgs(q)...), appMatchArgs(q)...)
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
	return applicationCategoryPage(tx, vendor, page, limit, q, state, "")
}

func applicationCategoryPage(tx *sql.Tx, vendor string, page, limit int, q, state, category string) (Page[Application], error) {
	where := appState(state)
	args := []any{}
	if category != "" {
		where += ` AND EXISTS(SELECT 1 FROM application_categories c WHERE c.app_uid=a.uid AND c.category_id=?)`
		args = append(args, category)
	}
	if vendor != "" {
		where += ` AND v.id=?`
		args = append(args, vendor)
	}
	order := `v.id,a.id`
	if q != "" {
		where += ` AND (` + vendorMatch + ` OR ` + appMatch + `)`
		args = append(append(args, vendorMatchArgs(q)...), appMatchArgs(q)...)
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
		args = append(args, appMatchArgs(q)...)
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

// ApplicationsMatching reads the complete filtered set in one snapshot. The
// caller can order provider-derived metadata before applying page boundaries.
func (s *Store) ApplicationsMatching(vendor, q, state string) ([]Application, error) {
	if !validDirectoryPage(1, 1, state) {
		return nil, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	first, err := applicationPage(tx, vendor, 1, 1, q, state)
	if err != nil {
		return nil, err
	}
	if first.Total <= 1 {
		return first.Items, tx.Commit()
	}
	all, err := applicationPage(tx, vendor, 1, int(first.Total), q, state)
	if err != nil {
		return nil, err
	}
	return all.Items, tx.Commit()
}

func (s *Store) ApplicationCategoryPage(vendor string, page, limit int, q, category string) (Page[Application], error) {
	if !validDirectoryPage(page, limit, "enabled") {
		return Page[Application]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[Application]{}, err
	}
	defer tx.Rollback()
	result, err := applicationCategoryPage(tx, vendor, page, limit, q, "enabled", category)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// SearchVendors returns at most limit published vendors whose ID or a
// localized name contains q (case-insensitive), ordered by ID.
func (s *Store) SearchVendors(q string, limit int) ([]Vendor, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidDirectory
	}
	rows, err := s.DB.Query(`SELECT `+vendorColumns+` FROM vendors v WHERE v.enabled=1 AND v.deleted_at_s IS NULL AND `+vendorMatch+` ORDER BY v.id LIMIT ?`, append(vendorMatchArgs(q), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Vendor{}
	for rows.Next() {
		v, err := scanVendor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
