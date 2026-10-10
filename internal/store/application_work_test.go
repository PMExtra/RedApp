package store

import (
	"context"
	"errors"
	"testing"
)

func TestApplicationDeletionValidationPrecedesCancellation(t *testing.T) {
	fault := injectCommitFault(t)
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
	fault.armed.Store(true)
	if _, _, err = s.PrepareApplicationDeletion(a.Key, a.Revision); err == nil {
		t.Fatal("intent failure ignored")
	}
	if ctx2.Err() != nil {
		t.Fatal("failed intent interrupted task")
	}
	fault.armed.Store(false)
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
