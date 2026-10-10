package store

import (
	"strings"
	"testing"
)

func TestHTTPMaintenancePagesUseSourceRowRangeIndex(t *testing.T) {
	s := openTest(t)
	for _, query := range []struct {
		name string
		sql  string
		args []any
	}{
		{"frozen page", `SELECT row_no,id,storage_id,path,sha256,size_bytes FROM http_cache_generations WHERE storage_id=? AND is_current=1 AND row_no>? AND row_no<=? ORDER BY row_no LIMIT ?`, []any{"app/test-e1", 1000, 1000000, 1000}},
		{"high water", `SELECT COALESCE(MAX(row_no),0) FROM http_cache_generations WHERE storage_id=? AND is_current=1`, []any{"app/test-e1"}},
	} {
		t.Run(query.name, func(t *testing.T) {
			rows, err := s.DB.Query("EXPLAIN QUERY PLAN "+query.sql, query.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan strings.Builder
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan.WriteString(detail + "\n")
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plan.String(), "http_cache_scan") || strings.Contains(plan.String(), "TEMP B-TREE") {
				t.Fatalf("maintenance query scans/sorts instead of paging source rows: %s", plan.String())
			}
			if query.name == "frozen page" && !strings.Contains(plan.String(), "row_no>? AND row_no<?") {
				t.Fatalf("row boundaries are not constrained by the index: %s", plan.String())
			}
		})
	}
}
