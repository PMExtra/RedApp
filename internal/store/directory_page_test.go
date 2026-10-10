package store

import (
	"fmt"
	"testing"
)

func TestDirectoryPagesFindSixthAppAndReturnEmptyPagesBeyondTheEnd(t *testing.T) {
	s := openTest(t)
	v, err := s.CreateVendor(directoryVendor("acme"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		input := directoryApplication(fmt.Sprintf("tool-%d", i))
		input.Name = LocalizedText{En: fmt.Sprintf("Tool %d", i), ZhCN: fmt.Sprintf("工具%d", i)}
		if _, err = s.CreateApplication(v.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.DirectoryPage(1, 1, "", "current")
	if err != nil || first.Total != 1 || len(first.Items) != 1 || len(first.Items[0].Apps) != 5 || first.Items[0].AppTotal != 8 {
		t.Fatal(first, err)
	}
	for _, q := range []string{"tool-6", "工具6", "TOOL 6", "acme/tool-6"} {
		page, err := s.DirectoryPage(1, 1, q, "current")
		if err != nil || page.Page != 1 || len(page.Items) != 1 || page.Items[0].Apps[0].ID != "tool-6" {
			t.Fatal(q, page, err)
		}
	}
	beyond, err := s.DirectoryPage(99, 1, "tool-6", "current")
	if err != nil || beyond.Page != 99 || beyond.Total != 1 || len(beyond.Items) != 0 {
		t.Fatal(beyond, err)
	}
	apps, err := s.ApplicationPage("acme", 3, 3, "", "current")
	if err != nil || apps.Page != 3 || apps.Total != 8 || len(apps.Items) != 2 {
		t.Fatal(apps, err)
	}
	if apps, err = s.ApplicationPage("acme", 99, 3, "", "current"); err != nil || apps.Page != 99 || apps.TotalPages != 3 || len(apps.Items) != 0 {
		t.Fatal(apps, err)
	}
	empty, err := s.ApplicationPage("acme", 1, 3, "nonexistent", "current")
	if err != nil || empty.Page != 1 || empty.TotalPages != 1 || len(empty.Items) != 0 {
		t.Fatal(empty, err)
	}
	a, _ := s.Application("acme/tool-6")
	change := applicationChanges(a)
	change.Enabled = false
	if _, err = s.UpdateApplication(a.Key, a.Revision, change); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.ApplicationPage("acme", 1, 3, "", "disabled")
	if err != nil || disabled.Total != 1 || disabled.Items[0].ID != "tool-6" {
		t.Fatal(disabled, err)
	}
}

func TestSearchVendorsMatchesPublishedVendorsByIDOrName(t *testing.T) {
	s := openTest(t)
	for _, id := range []string{"zeta-tools", "alpha-tools", "hidden-tools", "other"} {
		if _, err := s.CreateVendor(directoryVendor(id)); err != nil {
			t.Fatal(err)
		}
	}
	hidden, _ := s.Vendor("hidden-tools")
	change := vendorChanges(hidden)
	change.Enabled = false
	if _, err := s.UpdateVendor(hidden.ID, hidden.Revision, change); err != nil {
		t.Fatal(err)
	}
	found, err := s.SearchVendors("TOOLS", 4)
	if err != nil || len(found) != 2 || found[0].ID != "alpha-tools" || found[1].ID != "zeta-tools" {
		t.Fatal(found, err)
	}
	if byName, err := s.SearchVendors("发布", 1); err != nil || len(byName) != 1 || byName[0].ID != "alpha-tools" {
		t.Fatal(byName, err)
	}
}
