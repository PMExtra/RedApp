package store

import (
	"testing"
)

func TestAdminNotesRestartPreservesDataAndStableIdentity(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.CreateVendor(VendorInput{ID: "acme", Name: LocalizedText{"Custom", "自定义"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateApplication(v.ID, ApplicationInput{ID: "private", Name: LocalizedText{"Private", "私有"}, Provider: "info", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	i, _ := s.Instructions(a.UID)
	if _, err = s.SaveInstructions(a.Key, i.Revision, LocalizedText{En: "Keep public instructions"}); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Application(a.Key)

	for _, item := range []struct{ kind, key string }{{"vendor", v.ID}, {"app", a.Key}} {
		empty, err := s.AdminNotes(item.kind, item.key)
		if err != nil || empty != (AdminNotes{}) {
			t.Fatal(empty, err)
		}
		if _, err = s.SaveAdminNotes(item.kind, item.key, 0, item.kind+" private\n\ttext"); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	for _, item := range []struct{ kind, key string }{{"vendor", v.ID}, {"app", a.Key}} {
		got, err := s.AdminNotes(item.kind, item.key)
		if err != nil || got.Text != item.kind+" private\n\ttext" || got.Revision != 1 {
			t.Fatal(got, err)
		}
	}
	if err = s.PermanentlyDeleteApplication(a.Key, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.PermanentlyDeleteVendor(v.ID, v.Revision); err != nil {
		t.Fatal(err)
	}
	v2, err := s.CreateVendor(VendorInput{ID: v.ID, Name: v.Name})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.CreateApplication(v2.ID, ApplicationInput{ID: a.ID, Name: a.Name, Provider: "info"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.UID == v.UID || a2.UID == a.UID {
		t.Fatal("identity reused")
	}
	for _, item := range []struct{ kind, key string }{{"vendor", v2.ID}, {"app", a2.Key}} {
		got, err := s.AdminNotes(item.kind, item.key)
		if err != nil || got != (AdminNotes{}) {
			t.Fatal("notes leaked to recreated identity", got, err)
		}
	}
}
