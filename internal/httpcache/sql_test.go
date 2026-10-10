package httpcache

import (
	"database/sql"
	"testing"

	"github.com/PMExtra/RedApp/internal/store/storetest"
)

// sql opens a second connection to the fixture database. Tests use it only to
// seed many rows in one transaction, to inject faults with triggers and to
// inspect rows; the service under test reaches the database only through the
// store. Triggers created here persist and fire on the store's connection.
func (f *fixture) sql(t *testing.T) *sql.DB {
	t.Helper()
	return storetest.Open(t, f.dir)
}
