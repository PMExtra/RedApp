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
		{"frozen page", `SELECT ` + httpEntryColumns + ` FROM http_cache_generations ` + httpPageCondition, []any{"app/test-e1", 1000, 1000000, 1000}},
		{"high water", httpHighWaterQuery, []any{"app/test-e1"}},
	} {
		t.Run(query.name, func(t *testing.T) {
			rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query.sql, query.args...)
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
