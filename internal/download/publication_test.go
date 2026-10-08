package download

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func publicationDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication lock order deadlocked")
	}
}
func TestPublicationTransactionInterleavesAcquireWithoutDeadlock(t *testing.T) {
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("fixture")) }))
	m, db, _ := setup(t, client)
	r := authorizedResource(t, m, client, []byte("fixture"))
	plan, err := m.PrepareUpstreams(map[string]*distributor.Client{testApp: client})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		plan.Abort()
		t.Fatal(err)
	}
	entered := make(chan struct{})
	continueAcquire := make(chan struct{})
	m.testFault = func(point string, _ *Generation) {
		if point == "acquire_before_db_validation" {
			close(entered)
			<-continueAcquire
		}
	}
	acquired := make(chan error, 1)
	go func() {
		rd, _, err := m.Acquire(context.Background(), r)
		if err == nil {
			rd.Close()
		}
		acquired <- err
	}()
	<-entered // Acquire owns mu; the configuration transaction owns the only DB connection.
	close(continueAcquire)
	// Commit releases the connection before publication needs mu. There is no
	// transaction -> mu edge, even if Acquire is waiting on SQLite right now.
	if err = tx.Commit(); err != nil {
		plan.Abort()
		t.Fatal(err)
	}
	published := make(chan error, 1)
	go func() { plan.Publish(); published <- nil }()
	publicationDone(t, acquired)
	publicationDone(t, published)
}
func TestCloseWaitsForPreparedPublicationAndAbortDoesNotRegister(t *testing.T) {
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	m, _, _ := setup(t, client)
	plan, err := m.PrepareUpstreams(map[string]*distributor.Client{"acme/new": client})
	if err != nil {
		t.Fatal(err)
	}
	plan.Abort()
	m.mu.Lock()
	_, exists := m.upstreams["acme/new"]
	m.mu.Unlock()
	if exists {
		t.Fatal("aborted plan leaked registration")
	}
	plan, err = m.PrepareUpstreams(map[string]*distributor.Client{"acme/new": client})
	if err != nil {
		t.Fatal(err)
	}
	closing := make(chan struct{})
	m.testFault = func(point string, _ *Generation) {
		if point == "close_before_publication_gate" {
			select {
			case <-closing:
			default:
				close(closing)
			}
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	<-closing
	m.mu.Lock()
	unavailable := m.closed
	m.mu.Unlock()
	if unavailable {
		t.Fatal("Close crossed the publication gate")
	}
	plan.PublishWith(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed || m.upstreams["acme/new"] == nil {
			t.Error("publication not installed before Close")
		}
	})
	publicationDone(t, closed)
	if _, err = m.PrepareUpstreams(nil); err == nil {
		t.Fatal("prepared while closed")
	}
}
