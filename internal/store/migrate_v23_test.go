package store

import "testing"

// Existing applications keep the padded icon tile.
func TestMigration23AddsIconBackgroundFlag(t *testing.T) {
	dir := t.TempDir()
	loadSchemaFixture(t, dir, fixturePath(22))
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var apps, flagged int
	if err = s.read.QueryRow(`SELECT count(*),COALESCE(sum(icon_has_background),0) FROM applications`).Scan(&apps, &flagged); err != nil || apps == 0 || flagged != 0 {
		t.Fatal(apps, flagged, err)
	}
}
