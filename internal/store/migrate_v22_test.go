package store

import (
	"testing"

	"github.com/PMExtra/RedApp/internal/networkproxy"
)

// Existing data directories start with strict global redirect protection.
func TestMigration22AddsStrictRedirectProtection(t *testing.T) {
	dir := t.TempDir()
	loadSchemaFixture(t, dir, fixturePath(21))
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mode, revision, err := s.GlobalRedirectProtection()
	if err != nil || mode != networkproxy.RedirectStrict || revision != 1 {
		t.Fatal(mode, revision, err)
	}
	if _, err = s.PatchGlobalRedirectProtection(1, networkproxy.RedirectBasic); err != nil {
		t.Fatal(err)
	}
}
