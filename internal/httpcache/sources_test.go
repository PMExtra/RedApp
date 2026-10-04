package httpcache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
)

func sourceEntry(t *testing.T) application.Entry {
	t.Helper()
	pool := distributor.NewPool()
	clients := []*distributor.Client{}
	for i := 0; i < 3; i++ {
		client, err := pool.NewClient(fmt.Sprintf("http://source%d.internal/files", i), distributor.GeneralHTTP)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	return application.Entry{UID: "00000000000000000000000000000001", SourceEpoch: 1, Provider: application.HttpCache, Upstream: clients[0], Upstreams: clients, SourceStrategy: "ordered"}
}
func checkPermutation(t *testing.T, attempts []sourceAttempt) {
	t.Helper()
	seen := map[int]bool{}
	for _, attempt := range attempts {
		if attempt.Index < 0 || attempt.Index >= 3 || seen[attempt.Index] || attempt.SourceURL != attempt.Client.Base.String() {
			t.Errorf("invalid attempt: %+v", attempt)
		}
		seen[attempt.Index] = true
	}
	if len(seen) != 3 {
		t.Errorf("missing source in attempt plan: %v", seen)
	}
}
func TestSourceOrderingRotationAndEpochFences(t *testing.T) {
	service := &Service{}
	entry := sourceEntry(t)
	for i := 0; i < 2; i++ {
		attempts, err := service.sourceAttempts(entry)
		if err != nil {
			t.Fatal(err)
		}
		for j, a := range attempts {
			if a.Index != j {
				t.Fatal("ordered source order changed")
			}
		}
	}
	entry.SourceStrategy = "round_robin"
	for i := 0; i < 5; i++ {
		attempts, err := service.sourceAttempts(entry)
		if err != nil {
			t.Fatal(err)
		}
		checkPermutation(t, attempts)
		for j, a := range attempts {
			if a.Index != (i+j)%3 {
				t.Fatal("round robin did not rotate remaining sources", i, attempts)
			}
		}
	}
	old := entry
	entry.SourceEpoch++
	attempts, err := service.sourceAttempts(entry)
	if err != nil || attempts[0].Index != 0 {
		t.Fatal("new source epoch did not reset rotation", attempts, err)
	}
	if _, err = service.sourceAttempts(old); err != nil {
		t.Fatal(err)
	}
	attempts, err = service.sourceAttempts(entry)
	if err != nil || attempts[0].Index != 1 {
		t.Fatal("old epoch changed new rotation", attempts, err)
	}
	entry.SourceStrategy = "random"
	for i := 0; i < 20; i++ {
		attempts, err = service.sourceAttempts(entry)
		if err != nil {
			t.Fatal(err)
		}
		checkPermutation(t, attempts)
	}
	entry.Provider = application.Codex
	if _, err = service.sourceAttempts(entry); err == nil {
		t.Fatal("release provider accepted GeneralHttp source strategy")
	}
}
func TestConcurrentSourceRotationAndBoundedState(t *testing.T) {
	service := &Service{}
	entry := sourceEntry(t)
	entry.SourceStrategy = "round_robin"
	var counts [3]atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < ninety; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempts, err := service.sourceAttempts(entry)
			if err != nil {
				t.Error(err)
				return
			}
			counts[attempts[0].Index].Add(1)
		}()
	}
	wg.Wait()
	for i := range counts {
		if counts[i].Load() != ninety/3 {
			t.Fatal("concurrent rotation lost an advance", i, counts[i].Load())
		}
	}
	for i := 0; i < sourceCursorLimit+1; i++ {
		copy := entry
		copy.UID = fmt.Sprintf("%032x", i+2)
		if _, err := service.sourceAttempts(copy); err != nil {
			t.Fatal(err)
		}
	}
	if len(service.sourceCursors) != sourceCursorLimit || service.sourceOrder.Len() != sourceCursorLimit {
		t.Fatal("source cursor state exceeded bound")
	}
	attempts, err := service.sourceAttempts(entry)
	if err != nil || attempts[0].Index != 0 {
		t.Fatal("evicted cursor did not restart", attempts, err)
	}
}

const ninety = 90
