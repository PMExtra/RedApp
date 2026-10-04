package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestApplicationDeletionValidationPrecedesCancellation(t *testing.T) {
	s := openTest(t)
	if err := s.EnsureEntityTemplates(); err != nil {
		t.Fatal(err)
	}
	builtin, err := s.Application("openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := s.ApplicationWork(context.Background(), builtin.UID)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if _, _, err = s.PrepareApplicationDeletion(builtin.Key, builtin.Revision); !errors.Is(err, ErrBuiltinTemplate) {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("protected template task interrupted")
	}
	v, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Acme", "测试"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "custom", Name: v.Name, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx2, finish2, err := s.ApplicationWork(context.Background(), a.UID)
	if err != nil {
		t.Fatal(err)
	}
	defer finish2()
	if _, err = s.DB.Exec(`CREATE TEMP TRIGGER reject_delete_intent BEFORE INSERT ON pending_application_deletes BEGIN SELECT RAISE(ABORT,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PrepareApplicationDeletion(a.Key, a.Revision); err == nil {
		t.Fatal("intent failure ignored")
	}
	if ctx2.Err() != nil {
		t.Fatal("failed intent interrupted task")
	}
	row, _ := s.Application(a.Key)
	if row.DeletedAt != nil || row.Revision != a.Revision {
		t.Fatal("failed intent changed row")
	}
	_, release, err := s.ApplicationWork(context.Background(), a.UID)
	if err != nil {
		t.Fatal("failed intent closed admission", err)
	}
	release()
}

func TestExactV6UpgradeAddsDeletionIntentWithoutChangingBusinessData(t *testing.T) {
	dir := t.TempDir()
	raw, err := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(schemaV6); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`INSERT INTO settings(scope,app_id,key,revision,payload) VALUES('global','','site',19,'{"preserved":true}')`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	var payload string
	var revision int
	if err = s.DB.QueryRow(`SELECT payload,revision FROM settings WHERE key='site'`).Scan(&payload, &revision); err != nil || payload != `{"preserved":true}` || revision != 19 {
		t.Fatal(payload, revision, err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT count(*) FROM pending_application_deletes`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
