package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
	"golang.org/x/text/unicode/norm"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// TaxonomyItem is one category. Built-in categories keep template defaults and sparse name overrides.
type TaxonomyItem struct {
	Kind            string                 `json:"kind"`
	ID              string                 `json:"id"`
	Name            LocalizedText          `json:"name"`
	Revision        int64                  `json:"revision"`
	Builtin         bool                   `json:"builtin"`
	Present         bool                   `json:"-"`
	Default         *LocalizedText         `json:"-"`
	Override        Object                 `json:"-"`
	Defaults        Object                 `json:"defaults"`
	Overrides       Object                 `json:"overrides"`
	Effective       Object                 `json:"effective"`
	Fields          map[string]FieldOrigin `json:"fields"`
	TemplateRef     *string                `json:"template_ref"`
	TemplateHash    *string                `json:"template_hash"`
	TemplateMissing bool                   `json:"template_missing"`
}

// ErrCategoryAmbiguous means a typed category name matches more than one existing category.
var ErrCategoryAmbiguous = errors.New("Category name matches more than one existing category; choose one from the list")

const categoryKind = "categories"
const taxonomyColumns = `id,name_en,name_zh_cn,revision,builtin,present,default_en,default_zh_cn,override_en,override_zh_cn`

func taxonomyKey(kind, id string) string { return kind + ":" + id }
func scanTaxonomy(row scanner) (item TaxonomyItem, err error) {
	var en, zh, oe, oz sql.NullString
	err = row.Scan(&item.ID, &item.Name.En, &item.Name.ZhCN, &item.Revision, &item.Builtin, &item.Present, &en, &zh, &oe, &oz)
	if err != nil {
		return
	}
	item.Kind = categoryKind
	item.Override = Object{}
	if en.Valid && zh.Valid {
		item.Default = &LocalizedText{en.String, zh.String}
	}
	if oe.Valid {
		setLeaf(item.Override, "name.en", oe.String)
	}
	if oz.Valid {
		setLeaf(item.Override, "name.zh-CN", oz.String)
	}
	item.decorate()
	return
}
func (item *TaxonomyItem) decorate() {
	item.Overrides = cloneObject(item.Override)
	item.Defaults = nil
	item.Effective = object(map[string]any{"name": item.Name})
	item.Fields = map[string]FieldOrigin{}
	item.TemplateMissing = item.Builtin && !item.Present
	item.TemplateRef = nil
	if item.Builtin {
		ref := taxonomyKey(item.Kind, item.ID)
		item.TemplateRef = &ref
	}
	if item.Default != nil {
		item.Defaults = object(map[string]any{"name": item.Default})
	}
	for _, p := range []string{"name.en", "name.zh-CN"} {
		source := "custom"
		var differs *bool
		if item.Builtin {
			source = "inherited"
			if _, ok := leaf(item.Override, p); ok {
				source = "custom"
			}
			a, _ := leaf(item.Effective, p)
			b, _ := leaf(item.Defaults, p)
			diff := !reflect.DeepEqual(a, b)
			differs = &diff
		}
		item.Fields[p] = FieldOrigin{Source: source, Differs: differs}
	}
}

// writeCategory stores a category under CAS: stored is the revision the writer
// read, 0 for a category it created.
func writeCategory(tx *sql.Tx, item TaxonomyItem, stored int64) error {
	var en, zh, oe, oz any
	if item.Default != nil {
		en, zh = item.Default.En, item.Default.ZhCN
	}
	if v, ok := leaf(item.Override, "name.en"); ok {
		oe = v
	}
	if v, ok := leaf(item.Override, "name.zh-CN"); ok {
		oz = v
	}
	return casExec(tx, stored == 0,
		`INSERT INTO categories(`+taxonomyColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, []any{item.ID, item.Name.En, item.Name.ZhCN, item.Revision, item.Builtin, item.Present, en, zh, oe, oz},
		`UPDATE categories SET name_en=?,name_zh_cn=?,revision=?,builtin=?,present=?,default_en=?,default_zh_cn=?,override_en=?,override_zh_cn=? WHERE id=? AND revision=?`, []any{item.Name.En, item.Name.ZhCN, item.Revision, item.Builtin, item.Present, en, zh, oe, oz, item.ID, stored})
}
func (w *configSet) reconcileTaxonomy(spec presets.TaxonomySpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := w.loadAllCategories(); err != nil {
		return err
	}
	previous := map[string]TaxonomyItem{}
	for _, id := range w.categoryIDs() {
		item, _ := w.category(id)
		previous[id] = *item
		if item.Builtin {
			if item.Present {
				item.Revision++
			}
			item.Present = false
			item.decorate()
			w.putCategory(*item)
		}
	}
	for _, entry := range spec.Categories {
		item, _ := w.category(entry.ID)
		exists := item != nil
		before := previous[entry.ID]
		if !exists {
			item = &TaxonomyItem{Kind: categoryKind, ID: entry.ID, Revision: 1, Override: Object{}}
		} else if !item.Builtin {
			// Preset additions must not silently bind an administrator-owned identity.
			continue
		}
		if exists {
			item.Revision = before.Revision
		}
		item.Builtin = true
		item.Present = true
		item.Default = &LocalizedText{entry.Name.En, entry.Name.ZhCN}
		var effective struct {
			Name LocalizedText `json:"name"`
		}
		if err := strict(merge(object(map[string]any{"name": item.Default}), item.Override), &effective); err != nil {
			return err
		}
		item.Name = effective.Name
		if exists && (before.Default == nil || *before.Default != *item.Default || !before.Present || !before.Builtin) {
			item.Revision++
		}
		item.decorate()
		w.putCategory(*item)
	}
	return nil
}

// resolveNewCategories maps typed names to existing categories by either language,
// or creates administrator-owned categories in the same configuration change.
func (w *configSet) resolveNewCategories(names []string) ([]string, error) {
	if err := w.loadAllCategories(); err != nil {
		return nil, err
	}
	var out []string
	for _, raw := range names {
		name := strings.TrimSpace(norm.NFC.String(raw))
		if !presets.ValidTaxonomyName(presets.Text{En: name, ZhCN: name}) || utf8.RuneCountInString(name) > 64 {
			return nil, invalidf("invalid category name")
		}
		folded := presets.FoldText(name)
		var matches []string
		for _, id := range w.categoryIDs() {
			item := w.categories[id]
			if presets.FoldText(item.Name.En) == folded || presets.FoldText(item.Name.ZhCN) == folded {
				matches = append(matches, id)
			}
		}
		if len(matches) > 1 {
			return nil, ErrCategoryAmbiguous
		}
		if len(matches) == 1 {
			out = append(out, matches[0])
			continue
		}
		id, err := w.newCategoryID(name)
		if err != nil {
			return nil, err
		}
		item := TaxonomyItem{Kind: categoryKind, ID: id, Name: LocalizedText{En: name, ZhCN: name}, Revision: 1, Present: true, Override: Object{}}
		item.decorate()
		w.putCategory(item)
		out = append(out, id)
	}
	return out, nil
}

// New category IDs are stable after creation: a readable ASCII slug when possible,
// otherwise random. Requires every category to be loaded.
func (w *configSet) newCategoryID(name string) (string, error) {
	taken := func(id string) bool { return w.categories[id] != nil }
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 48 {
		base = strings.Trim(base[:48], "-")
	}
	ascii := true
	for _, r := range name {
		ascii = ascii && r < utf8.RuneSelf
	}
	if ascii && identity.ValidSlug(base) {
		if !taken(base) {
			return base, nil
		}
		for i := 2; i < 100; i++ {
			if id := fmt.Sprintf("%s-%d", base, i); !taken(id) {
				return id, nil
			}
		}
	}
	for {
		uid, err := identity.NewUID()
		if err != nil {
			return "", err
		}
		if id := "category-" + uid[:10]; !taken(id) {
			return id, nil
		}
	}
}
func projectAppTaxonomy(tx *sql.Tx, a Application) error {
	if _, err := tx.Exec(`DELETE FROM application_categories WHERE app_uid=?`, a.UID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM application_tags WHERE app_uid=?`, a.UID); err != nil {
		return err
	}
	if a.DeletedAt != nil {
		return nil
	}
	for _, id := range a.Categories {
		if _, err := tx.Exec(`INSERT INTO application_categories(app_uid,category_id) VALUES(?,?)`, a.UID, id); err != nil {
			return err
		}
	}
	for i, tag := range a.Tags {
		if _, err := tx.Exec(`INSERT INTO application_tags(app_uid,ordinal,tag,folded) VALUES(?,?,?,?)`, a.UID, i, tag, presets.FoldText(tag)); err != nil {
			return err
		}
	}
	return nil
}
func projectTemplateTaxonomy(tx *sql.Tx, t templateSnapshot) error {
	if _, err := tx.Exec(`DELETE FROM template_category_refs WHERE template_key=?`, t.Key); err != nil {
		return err
	}
	if !t.Present {
		return nil
	}
	var spec presets.AppSpec
	if err := strict(t.Spec, &spec); err != nil {
		return err
	}
	for _, id := range spec.Categories {
		if _, err := tx.Exec(`INSERT INTO template_category_refs(template_key,category_id) VALUES(?,?)`, t.Key, id); err != nil {
			return err
		}
	}
	return nil
}

// cleanupCategories removes administrator-created categories no live App uses (enabled or not)
// and no present template references. Built-in categories keep their template definitions.
func cleanupCategories(tx *sql.Tx) (int64, error) {
	result, err := tx.Exec(`DELETE FROM categories WHERE builtin=0 AND NOT EXISTS(SELECT 1 FROM application_categories r WHERE r.category_id=categories.id) AND NOT EXISTS(SELECT 1 FROM template_category_refs t WHERE t.category_id=categories.id)`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func bumpCategoryRevision(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE category_state SET public_revision=public_revision+1 WHERE id=1`)
	return err
}

type CategoryListItem struct {
	TaxonomyItem
	Applications int64 `json:"applications"`
}

func (s *Store) TaxonomyPage(q string, page, limit int) (Page[CategoryListItem], error) {
	if page < 1 || limit < 1 || limit > 100 || !utf8.ValidString(q) || utf8.RuneCountInString(q) > 128 {
		return Page[CategoryListItem]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[CategoryListItem]{}, err
	}
	defer tx.Rollback()
	where := ` WHERE instr(lower(id),lower(?))>0 OR instr(lower(name_en),lower(?))>0 OR instr(lower(name_zh_cn),lower(?))>0`
	args := []any{q, q, q}
	var count int64
	if err = tx.QueryRow(`SELECT count(*) FROM categories`+where, args...).Scan(&count); err != nil {
		return Page[CategoryListItem]{}, err
	}
	out := NewPage[CategoryListItem](page, limit, count)
	args = append(args, limit, (out.Page-1)*limit)
	rows, err := tx.Query(`SELECT `+taxonomyColumns+`,(SELECT count(*) FROM application_categories r WHERE r.category_id=categories.id) FROM categories`+where+` ORDER BY id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item CategoryListItem
		var en, zh, oe, oz sql.NullString
		if err = rows.Scan(&item.ID, &item.Name.En, &item.Name.ZhCN, &item.Revision, &item.Builtin, &item.Present, &en, &zh, &oe, &oz, &item.Applications); err != nil {
			rows.Close()
			return out, err
		}
		item.TaxonomyItem = taxonomyItem(item.TaxonomyItem, en, zh, oe, oz)
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// PatchTaxonomy renames one category; IDs and associations never change.
func (s *Store) PatchTaxonomy(id string, patch ConfigurationPatch) (TaxonomyItem, error) {
	if !identity.ValidSlug(id) || len(patch.NewCategories) > 0 {
		return TaxonomyItem{}, ErrInvalidDirectory
	}
	// Names stay unique only if no configuration write creates a category
	// between the uniqueness check and the commit.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return TaxonomyItem{}, err
	}
	defer tx.Rollback()
	item, err := scanTaxonomy(tx.QueryRow(`SELECT `+taxonomyColumns+` FROM categories WHERE id=?`, id))
	if err != nil {
		return item, err
	}
	if item.Revision != patch.Revision {
		return item, ErrConflict
	}
	used := map[string]bool{}
	for p, raw := range patch.Set {
		if p != "name.en" && p != "name.zh-CN" {
			return item, invalidf("%s cannot be set", p)
		}
		var text string
		if json.Unmarshal(raw, &text) != nil || string(raw) == "null" {
			return item, invalidf("%s must be a string", p)
		}
		used[p] = true
		if item.Builtin {
			setLeaf(item.Override, p, text)
		} else if p == "name.en" {
			item.Name.En = text
		} else {
			item.Name.ZhCN = text
		}
	}
	for _, p := range patch.Unset {
		if !item.Builtin || used[p] || p != "name.en" && p != "name.zh-CN" {
			return item, invalidf("%s cannot be reset (only names of built-in categories, not also set)", p)
		}
		used[p] = true
		unsetLeaf(item.Override, p)
	}
	if item.Builtin {
		var effective struct {
			Name LocalizedText `json:"name"`
		}
		if err = strict(merge(item.Defaults, item.Override), &effective); err != nil {
			return item, err
		}
		item.Name = effective.Name
	}
	if !presets.ValidTaxonomyName(presets.Text{En: item.Name.En, ZhCN: item.Name.ZhCN}) {
		return item, invalidf("category names must be 1..64 characters")
	}
	if len(used) == 0 {
		return item, nil
	}
	if err = uniqueCategoryNames(tx, item); err != nil {
		return item, err
	}
	item.Revision++
	item.decorate()
	if err = writeCategory(tx, item, patch.Revision); err == nil {
		err = bumpCategoryRevision(tx)
	}
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}

// uniqueCategoryNames rejects a rename whose name (either language, case-folded)
// is already used by another category, so typed names stay unambiguous.
func uniqueCategoryNames(tx *sql.Tx, item TaxonomyItem) error {
	rows, err := tx.Query(`SELECT id,name_en,name_zh_cn FROM categories WHERE id<>?`, item.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	names := map[string]bool{presets.FoldText(item.Name.En): true, presets.FoldText(item.Name.ZhCN): true}
	for rows.Next() {
		var id, en, zh string
		if err = rows.Scan(&id, &en, &zh); err != nil {
			return err
		}
		if names[presets.FoldText(en)] || names[presets.FoldText(zh)] {
			return invalidf("category %s already uses this name", id)
		}
	}
	return rows.Err()
}

// Category reads one category with its application count.
func (s *Store) Category(id string) (CategoryListItem, error) {
	var item CategoryListItem
	var en, zh, oe, oz sql.NullString
	err := s.DB.QueryRow(`SELECT `+taxonomyColumns+`,(SELECT count(*) FROM application_categories r WHERE r.category_id=categories.id) FROM categories WHERE id=?`, id).Scan(&item.ID, &item.Name.En, &item.Name.ZhCN, &item.Revision, &item.Builtin, &item.Present, &en, &zh, &oe, &oz, &item.Applications)
	if err != nil {
		return item, err
	}
	item.TaxonomyItem = taxonomyItem(item.TaxonomyItem, en, zh, oe, oz)
	return item, nil
}

func taxonomyItem(item TaxonomyItem, en, zh, oe, oz sql.NullString) TaxonomyItem {
	item.Kind = categoryKind
	item.Override = Object{}
	if en.Valid && zh.Valid {
		item.Default = &LocalizedText{en.String, zh.String}
	}
	if oe.Valid {
		setLeaf(item.Override, "name.en", oe.String)
	}
	if oz.Valid {
		setLeaf(item.Override, "name.zh-CN", oz.String)
	}
	item.decorate()
	return item
}

// Public projection contains IDs and localized names only, with one batch query.
type TaxonomyLabel struct {
	ID   string        `json:"id"`
	Name LocalizedText `json:"name"`
}
type CategoryCount struct {
	TaxonomyLabel
	Count int64 `json:"count"`
}

const publicApplicationFilter = `a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL`

// PublicTaxonomy returns each public App's categories and site-wide counts of public Apps per category.
func (s *Store) PublicTaxonomy() (map[string][]TaxonomyLabel, []CategoryCount, error) {
	rows, err := s.DB.Query(`SELECT a.uid,c.id,c.name_en,c.name_zh_cn FROM applications a JOIN vendors v ON v.uid=a.vendor_uid JOIN application_categories r ON r.app_uid=a.uid JOIN categories c ON c.id=r.category_id WHERE ` + publicApplicationFilter + ` ORDER BY a.uid,c.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[string][]TaxonomyLabel{}
	counts := map[string]*CategoryCount{}
	for rows.Next() {
		var uid string
		var label TaxonomyLabel
		if err = rows.Scan(&uid, &label.ID, &label.Name.En, &label.Name.ZhCN); err != nil {
			return nil, nil, err
		}
		out[uid] = append(out[uid], label)
		if counts[label.ID] == nil {
			counts[label.ID] = &CategoryCount{TaxonomyLabel: label}
		}
		counts[label.ID].Count++
	}
	all := []CategoryCount{}
	for _, v := range counts {
		all = append(all, *v)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return out, all, rows.Err()
}
func (s *Store) TaxonomyPublicRevision() (int64, error) {
	var revision int64
	err := s.DB.QueryRow(`SELECT public_revision FROM category_state WHERE id=1`).Scan(&revision)
	return revision, err
}
