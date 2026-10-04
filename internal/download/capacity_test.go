package download

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/testutil"
)

func TestHTTPAndReleaseShareGlobalCapacity(t *testing.T) {
	payload := []byte("release transfer cannot bypass HTTP cache capacity")
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(payload) }))
	m, _, _ := setup(t, client)
	if err := m.ConfigureLimits(1, 1, 1024); err != nil {
		t.Fatal(err)
	}
	resource := authorizedResource(t, m, client, payload)
	releaseReader, err := m.AcquireHTTPReader()
	if err != nil {
		t.Fatal(err)
	}
	if reader, _, err := m.Acquire(context.Background(), resource); !errors.Is(err, ErrReaderLimit) {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("release escaped HTTP reader occupancy: %v", err)
	}
	releaseReader()
	releaseReader()
	releaseWriter, err := m.AcquireHTTPWriter()
	if err != nil {
		t.Fatal(err)
	}
	if reader, _, err := m.Acquire(context.Background(), resource); !errors.Is(err, ErrWriterLimit) {
		if reader != nil {
			reader.Close()
		}
		t.Fatalf("release escaped HTTP writer occupancy: %v", err)
	}
	releaseWriter()
	releaseWriter()
	reader, _, err := m.Acquire(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if release, err := m.AcquireHTTPReader(); !errors.Is(err, ErrReaderLimit) {
		if release != nil {
			release()
		}
		t.Fatalf("HTTP escaped release reader occupancy: %v", err)
	}
	if got := m.MaxArtifactBytes(); got != 1024 {
		t.Fatalf("HTTP byte limit differs: %d", got)
	}
}

func TestCloseWaitsForSharedHTTPWriterAndRejectsAdmission(t *testing.T) {
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	m, _, _ := setup(t, client)
	release, err := m.AcquireHTTPWriter()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	<-m.ctx.Done() // Close has atomically stopped admissions before canceling.
	select {
	case err := <-closed:
		t.Fatalf("Close returned before external writer release: %v", err)
	default:
	}
	if lease, err := m.AcquireHTTPWriter(); err == nil {
		lease()
		t.Fatal("writer admitted during Close")
	}
	if lease, err := m.AcquireHTTPReader(); err == nil {
		lease()
		t.Fatal("reader admitted during Close")
	}
	release()
	release()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after external writer released")
	}
}
