package store

import (
	"testing"
	"time"
)

// A write transaction holds the only writer connection and the database write
// lock; reads use the separate query_only pool and must neither wait for it
// nor observe its uncommitted changes.
func TestReadsProceedWhileWriteTransactionIsOpen(t *testing.T) {
	s := openTest(t)
	before, err := s.HomepagePins()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE catalog_state SET revision=revision+1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		pins HomepagePins
		err  error
	}
	read := make(chan result, 1)
	go func() {
		pins, err := s.HomepagePins()
		read <- result{pins, err}
	}()
	select {
	case r := <-read:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.pins.Revision != before.Revision {
			t.Fatal("read observed an uncommitted write", r.pins.Revision, before.Revision)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("read waited for the open write transaction")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := s.HomepagePins()
	if err != nil || after.Revision != before.Revision+1 {
		t.Fatal("read after commit missed the write", after.Revision, err)
	}
}

func TestReadPoolCannotWrite(t *testing.T) {
	s := openTest(t)
	if _, err := s.read.Exec(`UPDATE catalog_state SET revision=revision+1 WHERE id=1`); err == nil {
		t.Fatal("read pool accepted a write")
	}
}
