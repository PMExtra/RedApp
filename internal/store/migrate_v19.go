package store

import "database/sql"

// migrate18To19 gives the existing instance its random identity. Keep this
// SQL independent of future schema.
func migrate18To19(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE instance(
  id INTEGER PRIMARY KEY CHECK(id=1),
  uid TEXT NOT NULL CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*')
);
INSERT INTO instance(id,uid) VALUES(1,lower(hex(randomblob(16))));
`)
	return err
}
