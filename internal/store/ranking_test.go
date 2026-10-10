package store

import (
	"fmt"
	"testing"
	"time"
)

func TestRankingMergesClientsAndExpiresBuckets(t *testing.T) {
	s := openTest(t)
	s.CreateVendor(directoryVendor("acme"))
	a, err := s.CreateApplication("acme", directoryApplication("tool"))
	if err != nil {
		t.Fatal(err)
	}
	// Fixed fixture salt makes approximation bounds reproducible; production salts remain random.
	if _, err = s.DB.Exec(`UPDATE catalog_state SET ranking_salt=zeroblob(32)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	var firstDay int64
	for day := 6; day >= 0; day-- {
		for ip := 1; ip <= 10; ip++ {
			if err = s.RecordDownload(a.UID, fmt.Sprintf("192.0.2.%d", ip), now.Add(-time.Duration(day)*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		if day == 6 {
			first, e := s.DownloadRanking(now, 20)
			if e != nil || len(first) != 1 {
				t.Fatal(first, e)
			}
			firstDay = first[0].Clients
		}
	}
	s.RecordDownload(a.UID, "::ffff:192.0.2.1", now)
	scores, err := s.DownloadRanking(now, 20)
	if err != nil || len(scores) != 1 || scores[0].Clients != firstDay || firstDay < 8 || firstDay > 12 {
		t.Fatal("daily cardinalities were added instead of merged", scores, err)
	}
	var bytesStored int
	if err = s.DB.QueryRow(`SELECT sum(length(registers)) FROM download_sketches`).Scan(&bytesStored); err != nil || bytesStored != 7*1024 {
		t.Fatal("unbounded sketches", bytesStored, err)
	}
	scores, err = s.DownloadRanking(now.Add(7*24*time.Hour), 20)
	if err != nil || len(scores) != 0 {
		t.Fatal("expired clients remain ranked", scores, err)
	}
	for i := 0; i < 5000; i++ {
		if err = s.RecordDownload(a.UID, fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256), now); err != nil {
			t.Fatal(err)
		}
	}
	scores, _ = s.DownloadRanking(now, 20)
	if scores[0].Clients < 4500 || scores[0].Clients > 5500 {
		t.Fatal("invalid estimate", scores)
	}
	if _, err = s.DB.Exec(`UPDATE vendors SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	scores, _ = s.DownloadRanking(now, 20)
	if len(scores) != 0 {
		t.Fatal("disabled vendor ranked")
	}
}
