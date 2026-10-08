package store

import (
	"database/sql"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/presets"
	"reflect"
	"sort"
	"unicode/utf8"
)

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
type TaxonomyInUse struct{ References int64 }

func (e *TaxonomyInUse) Error() string { return "Taxonomy item is in use" }

const taxonomyColumns = `kind,id,name_en,name_zh_cn,revision,builtin,present,default_en,default_zh_cn,override_en,override_zh_cn`

func taxonomyKey(kind, id string) string { return kind + ":" + id }
func taxonomyKind(kind string) bool      { return kind == "categories" || kind == "tags" }
func scanTaxonomy(row scanner) (item TaxonomyItem, err error) {
	var en, zh, oe, oz sql.NullString
	err = row.Scan(&item.Kind, &item.ID, &item.Name.En, &item.Name.ZhCN, &item.Revision, &item.Builtin, &item.Present, &en, &zh, &oe, &oz)
	if err != nil {
		return
	}
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
func readTaxonomy(tx *sql.Tx) (map[string]TaxonomyItem, error) {
	rows, err := tx.Query(`SELECT ` + taxonomyColumns + ` FROM taxonomy ORDER BY kind,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TaxonomyItem{}
	for rows.Next() {
		item, err := scanTaxonomy(rows)
		if err != nil {
			return nil, err
		}
		out[taxonomyKey(item.Kind, item.ID)] = item
	}
	return out, rows.Err()
}
func writeTaxonomy(tx *sql.Tx, item TaxonomyItem) error {
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
	_, err := tx.Exec(`INSERT INTO taxonomy(`+taxonomyColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET name_en=excluded.name_en,name_zh_cn=excluded.name_zh_cn,revision=excluded.revision,builtin=excluded.builtin,present=excluded.present,default_en=excluded.default_en,default_zh_cn=excluded.default_zh_cn,override_en=excluded.override_en,override_zh_cn=excluded.override_zh_cn`, item.Kind, item.ID, item.Name.En, item.Name.ZhCN, item.Revision, item.Builtin, item.Present, en, zh, oe, oz)
	return err
}
func (st *configurationState) reconcileTaxonomy(spec presets.TaxonomySpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	previous := map[string]TaxonomyItem{}
	for key, item := range st.Taxonomy {
		previous[key] = item
	}
	for key, item := range st.Taxonomy {
		if item.Builtin {
			if item.Present {
				item.Revision++
			}
			item.Present = false
			item.decorate()
			st.Taxonomy[key] = item
		}
	}
	for kind, items := range map[string][]presets.TaxonomyEntry{"categories": spec.Categories, "tags": spec.Tags} {
		for _, entry := range items {
			key := taxonomyKey(kind, entry.ID)
			item, exists := st.Taxonomy[key]
			before := previous[key]
			if !exists {
				item = TaxonomyItem{Kind: kind, ID: entry.ID, Revision: 1, Override: Object{}}
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
			st.Taxonomy[key] = item
		}
	}
	return nil
}
func (st *configurationState) validateTaxonomy(category string, tags []string) error {
	if category != "" {
		if _, ok := st.Taxonomy[taxonomyKey("categories", category)]; !ok {
			return ErrInvalidDirectory
		}
	}
	for _, id := range tags {
		if _, ok := st.Taxonomy[taxonomyKey("tags", id)]; !ok {
			return ErrInvalidDirectory
		}
	}
	return nil
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
	if a.Category != "" {
		if _, err := tx.Exec(`INSERT INTO application_categories(app_uid,category_id) VALUES(?,?)`, a.UID, a.Category); err != nil {
			return err
		}
	}
	for _, id := range a.Tags {
		if _, err := tx.Exec(`INSERT INTO application_tags(app_uid,tag_id) VALUES(?,?)`, a.UID, id); err != nil {
			return err
		}
	}
	return nil
}
func projectTemplateTaxonomy(tx *sql.Tx, t templateSnapshot) error {
	if _, err := tx.Exec(`DELETE FROM template_taxonomy_refs WHERE template_key=?`, t.Key); err != nil {
		return err
	}
	if !t.Present {
		return nil
	}
	var spec presets.AppSpec
	if err := strict(t.Spec, &spec); err != nil {
		return err
	}
	if spec.Category != "" {
		if _, err := tx.Exec(`INSERT INTO template_taxonomy_refs VALUES(?,?,?)`, t.Key, "categories", spec.Category); err != nil {
			return err
		}
	}
	for _, id := range spec.Tags {
		if _, err := tx.Exec(`INSERT INTO template_taxonomy_refs VALUES(?,?,?)`, t.Key, "tags", id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) TaxonomyItem(kind, id string) (TaxonomyItem, error) {
	if !taxonomyKind(kind) || !identity.ValidSlug(id) {
		return TaxonomyItem{}, ErrInvalidDirectory
	}
	return scanTaxonomy(s.DB.QueryRow(`SELECT `+taxonomyColumns+` FROM taxonomy WHERE kind=? AND id=?`, kind, id))
}
func (s *Store) TaxonomyPage(kind, q string, page, limit int) (Page[TaxonomyItem], error) {
	if kind != "" && !taxonomyKind(kind) || page < 1 || limit < 1 || limit > 100 || !utf8.ValidString(q) || utf8.RuneCountInString(q) > 128 {
		return Page[TaxonomyItem]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[TaxonomyItem]{}, err
	}
	defer tx.Rollback()
	where := ` WHERE (?='' OR kind=?) AND (instr(lower(id),lower(?))>0 OR instr(lower(name_en),lower(?))>0 OR instr(lower(name_zh_cn),lower(?))>0)`
	args := []any{kind, kind, q, q, q}
	var count int64
	if err = tx.QueryRow(`SELECT count(*) FROM taxonomy`+where, args...).Scan(&count); err != nil {
		return Page[TaxonomyItem]{}, err
	}
	out := NewPage[TaxonomyItem](page, limit, count)
	args = append(args, limit, (out.Page-1)*limit)
	rows, err := tx.Query(`SELECT `+taxonomyColumns+` FROM taxonomy`+where+` ORDER BY kind,id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		item, err := scanTaxonomy(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Store) CreateTaxonomy(kind, id string, name LocalizedText) (TaxonomyItem, error) {
	if !taxonomyKind(kind) || !identity.ValidSlug(id) || !presets.ValidTaxonomyName(presets.Text{En: name.En, ZhCN: name.ZhCN}) {
		return TaxonomyItem{}, ErrInvalidDirectory
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return TaxonomyItem{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM taxonomy WHERE kind=? AND id=?`, kind, id).Scan(&count); err != nil {
		return TaxonomyItem{}, err
	}
	if count != 0 {
		return TaxonomyItem{}, ErrConflict
	}
	item := TaxonomyItem{Kind: kind, ID: id, Name: name, Revision: 1, Present: true, Override: Object{}}
	item.decorate()
	if err = writeTaxonomy(tx, item); err == nil {
		_, err = tx.Exec(`UPDATE taxonomy_state SET public_revision=public_revision+1 WHERE id=1`)
	}
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}
func (s *Store) PatchTaxonomy(kind, id string, patch ConfigurationPatch) (TaxonomyItem, error) {
	if !taxonomyKind(kind) || !identity.ValidSlug(id) {
		return TaxonomyItem{}, ErrInvalidDirectory
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return TaxonomyItem{}, err
	}
	defer tx.Rollback()
	item, err := scanTaxonomy(tx.QueryRow(`SELECT `+taxonomyColumns+` FROM taxonomy WHERE kind=? AND id=?`, kind, id))
	if err != nil {
		return item, err
	}
	if item.Revision != patch.Revision {
		return item, ErrConflict
	}
	used := map[string]bool{}
	for p, raw := range patch.Set {
		if p != "name.en" && p != "name.zh-CN" {
			return item, ErrInvalidDirectory
		}
		var text string
		if json.Unmarshal(raw, &text) != nil || string(raw) == "null" {
			return item, ErrInvalidDirectory
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
			return item, ErrInvalidDirectory
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
		return item, ErrInvalidDirectory
	}
	if len(used) == 0 {
		return item, nil
	}
	item.Revision++
	item.decorate()
	if err = writeTaxonomy(tx, item); err == nil {
		_, err = tx.Exec(`UPDATE taxonomy_state SET public_revision=public_revision+1 WHERE id=1`)
	}
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}
func (s *Store) DeleteTaxonomy(kind, id string, revision int64) error {
	if !taxonomyKind(kind) || !identity.ValidSlug(id) {
		return ErrInvalidDirectory
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := scanTaxonomy(tx.QueryRow(`SELECT `+taxonomyColumns+` FROM taxonomy WHERE kind=? AND id=?`, kind, id))
	if err != nil {
		return err
	}
	if item.Revision != revision {
		return ErrConflict
	}
	if item.Builtin {
		return ErrBuiltinTemplate
	}
	table, column := "application_tags", "tag_id"
	if kind == "categories" {
		table, column = "application_categories", "category_id"
	}
	var count int64
	if err = tx.QueryRow(`SELECT (SELECT count(*) FROM `+table+` r JOIN applications a ON a.uid=r.app_uid WHERE r.`+column+`=? AND a.deleted_at_s IS NULL)+(SELECT count(*) FROM template_taxonomy_refs WHERE kind=? AND id=?)`, id, kind, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return &TaxonomyInUse{count}
	}
	if _, err = tx.Exec(`DELETE FROM taxonomy WHERE kind=? AND id=?`, kind, id); err == nil {
		_, err = tx.Exec(`UPDATE taxonomy_state SET public_revision=public_revision+1 WHERE id=1`)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Public projection contains IDs and localized names only, with one batch query.
type TaxonomyLabel struct {
	ID   string        `json:"id"`
	Name LocalizedText `json:"name"`
}
type AppTaxonomy struct {
	Category *TaxonomyLabel  `json:"category"`
	Tags     []TaxonomyLabel `json:"tags"`
}

func (s *Store) PublicTaxonomy() (map[string]AppTaxonomy, []TaxonomyLabel, error) {
	rows, err := s.DB.Query(`SELECT a.uid,t.kind,t.id,t.name_en,t.name_zh_cn FROM applications a JOIN vendors v ON v.uid=a.vendor_uid JOIN (SELECT app_uid,kind,category_id AS id FROM application_categories UNION ALL SELECT app_uid,kind,tag_id FROM application_tags) r ON r.app_uid=a.uid JOIN taxonomy t ON t.kind=r.kind AND t.id=r.id WHERE a.enabled=1 AND v.enabled=1 AND a.deleted_at_s IS NULL AND v.deleted_at_s IS NULL ORDER BY a.uid,t.kind,t.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[string]AppTaxonomy{}
	categories := map[string]TaxonomyLabel{}
	for rows.Next() {
		var uid, kind string
		var label TaxonomyLabel
		if err = rows.Scan(&uid, &kind, &label.ID, &label.Name.En, &label.Name.ZhCN); err != nil {
			return nil, nil, err
		}
		item := out[uid]
		if item.Tags == nil {
			item.Tags = []TaxonomyLabel{}
		}
		if kind == "categories" {
			copy := label
			item.Category = &copy
			categories[label.ID] = label
		} else {
			item.Tags = append(item.Tags, label)
		}
		out[uid] = item
	}
	all := []TaxonomyLabel{}
	for _, v := range categories {
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return out, all, rows.Err()
}
func (s *Store) TaxonomyPublicRevision() (int64, error) {
	var revision int64
	err := s.DB.QueryRow(`SELECT public_revision FROM taxonomy_state WHERE id=1`).Scan(&revision)
	return revision, err
}
