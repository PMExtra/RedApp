package store

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMigration16PreservesRowsAndRollsBackFailures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			path := loadSchemaFixture(t, dir, fixturePath(16))
			queries, before := oldRows(t, path, nil)
			if fail {
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				plan := productionPlan()
				plan.steps = append([]migrationStep{}, plan.steps...)
				plan.steps[len(plan.steps)-1].Up = func(tx *sql.Tx) error {
					if err := migrate16To17(tx); err != nil {
						return err
					}
					return errors.New("injected after provider migration")
				}
				if err = plan.migrate(dir, path, discardLog); err == nil {
					t.Fatal("expected rollback")
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(after, original) {
					t.Fatal("rollback changed original")
				}
				if _, version := headerOf(t, path); version != 16 {
					t.Fatal(version)
				}
				return
			}
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			var mismatches int
			if err = s.read.QueryRow(`SELECT count(*) FROM application_config c JOIN applications a ON a.uid=c.entity_uid WHERE c.provider_key!=a.provider`).Scan(&mismatches); err != nil || mismatches != 0 {
				t.Fatal(mismatches, err)
			}
			s.Close()
			_, after := oldRows(t, path, queries)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("old configuration/cache rows changed")
			}
			if len(backups(t, dir)) != 1 {
				t.Fatal("missing backup")
			}
			s, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			if len(backups(t, dir)) != 1 {
				t.Fatal("migration repeated")
			}
		})
	}
}

func TestMigration16MaterializesLegacyJSONOverlay(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Application("microsoft/vscode")
	source, _ := s.Source(before.StorageID())
	s.Close()
	db, err := sql.Open("sqlite3", sqliteURL(filepath.Join(dir, databaseName), "mode=rw"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE application_config_v16(
 entity_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
 template_ref TEXT, overrides_json BLOB NOT NULL,spec_json BLOB,
 CHECK((template_ref IS NOT NULL AND spec_json IS NULL) OR (template_ref IS NULL AND spec_json IS NOT NULL)));
 INSERT INTO application_config_v16 SELECT entity_uid,template_ref,overrides_json,spec_json FROM application_config;
 DROP TABLE application_config;ALTER TABLE application_config_v16 RENAME TO application_config;
 UPDATE application_config SET overrides_json='{"json_release":{"refresh_seconds":777}}' WHERE template_ref='microsoft/vscode';
 DROP TABLE http_cache_indexes;
 PRAGMA user_version=16;`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.ApplicationConfiguration(before.Key)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		JSONRelease map[string]any `json:"json_release"`
	}
	if err = strict(Object{"json_release": cfg.Effective["json_release"]}, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.JSONRelease["refresh_seconds"] != float64(777) || spec.JSONRelease["sources"] == nil || spec.JSONRelease["channels"] == nil {
		t.Fatal(spec)
	}
	after, err := s.Source(before.StorageID())
	if err != nil || !reflect.DeepEqual(source, after) {
		t.Fatal("migration changed source/cache identity", after, err)
	}
}
