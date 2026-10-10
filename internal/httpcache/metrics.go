package httpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/fsutil"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// Snapshot maps complete HTTP bodies into the shared resource/disk view without
// assigning release versions or inventing trusted expected digests.
func (s *Service) Snapshot() ([]download.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.db.AllHTTPCacheEntries()
	if err != nil {
		return nil, err
	}
	out := []download.View{}
	remaining := map[string]int{}
	for key, count := range s.readers {
		remaining[key] = count
	}
	for _, e := range entries {
		r, err := rowFromEntry(e)
		if err != nil {
			return nil, err
		}
		metric := r.storageID
		if uid, _, ok := identity.ParseStorageID(r.storageID); ok {
			metric = identity.MetricsID(uid)
		}
		logical := sha256.Sum256([]byte(r.storageID + "\x00" + r.Path))
		size := r.SizeBytes
		resource := download.Resource{Application: r.storageID, MetricsID: metric, Key: r.Path, ID: hex.EncodeToString(logical[:]), Size: &size, Labels: map[string]string{"name": r.Path}}
		key := r.storageID + "\x00" + r.Path
		readers := s.pins[r.GenerationID]
		if readers > remaining[key] {
			readers = remaining[key]
		}
		remaining[key] -= readers
		out = append(out, download.View{Generation: download.Generation{ID: r.GenerationID, Resource: resource, State: "complete", Path: s.bodyPath(r.GenerationID), Bytes: r.SizeBytes, Total: r.SizeBytes, Started: r.FetchedAt, Received: r.FetchedAt, Finished: r.FetchedAt, Retired: !r.current}, Readers: readers, Current: r.current, SampledAt: s.now()})
	}
	for _, t := range s.transfers {
		logical := sha256.Sum256([]byte(t.entry.StorageID() + "\x00" + t.path))
		duration := s.now().Sub(t.started)
		average := float64(0)
		if duration > 0 {
			average = float64(t.bytes) / duration.Seconds()
		}
		key := t.entry.StorageID() + "\x00" + t.path
		readers := remaining[key]
		remaining[key] = 0
		out = append(out, download.View{Generation: download.Generation{ID: t.id, Resource: download.Resource{Application: t.entry.StorageID(), MetricsID: t.entry.MetricsID(), Key: t.path, ID: hex.EncodeToString(logical[:]), Labels: map[string]string{"name": t.path}}, State: "downloading", Path: t.filePath, Bytes: t.diskBytes, Total: -1, SourceBytes: t.bytes, Started: t.started}, ActiveWriter: true, Readers: readers, Current: true, AverageBPS: average, DownloadNS: int64(duration), SampledAt: s.now()})
	}
	return out, nil
}

type metricBody struct {
	transferID string
	io.ReadCloser
	s   *Service
	app string
}

func (b *metricBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.s.mu.Lock()
		if t := b.s.transfers[b.transferID]; t != nil {
			t.bytes += int64(n)
		}
		b.s.mu.Unlock()
		_ = b.s.db.AddFor(b.app, "upstream_bytes", int64(n)) // Buffered; accounting never fails a transfer.
	}
	return n, err
}

type metricWriter struct {
	http.ResponseWriter
	s      *Service
	app    string
	status int
	failed bool
}

func (w *metricWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *metricWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		w.failed = true
	}
	if n > 0 && (w.status == http.StatusOK || w.status == http.StatusPartialContent) {
		_ = w.s.db.AddFor(w.app, "downstream_bytes", int64(n)) // Buffered; accounting never fails a transfer.
	}
	return n, err
}
func (s *Service) upstreamFailure(entry application.Entry, path string, status int) error {
	if err := s.db.AddFor(entry.MetricsID(), "upstream_errors", 1); err != nil {
		return err
	}
	event := store.Event{AppID: entry.MetricsID(), ResourceKey: path, Category: "http", Code: "upstream_http", Message: "HTTP upstream failed to provide a complete representation"}
	if status >= 100 && status <= 599 {
		event.UpstreamStatus = &status
	} else {
		event.Code = "upstream_connection"
	}
	return s.db.RecordEvent(event)
}
func (s *Service) finishMetrics(w *metricWriter, entry application.Entry, path string, err error) {
	key := "download_success"
	if err != nil || w.failed || w.status >= 400 || w.status == 0 {
		key = "download_errors"
	}
	_ = s.db.AddFor(entry.MetricsID(), key, 1)
	if key == "download_errors" {
		_ = s.db.RecordEvent(store.Event{AppID: entry.MetricsID(), ResourceKey: path, Category: "http", Code: "download_failed", Message: "HTTP download did not complete"})
	}
	s.db.SettleCounters()
}

func (s *Service) startTransfer(entry application.Entry, path string) (string, func(), error) {
	id, err := fsutil.RandomID()
	if err != nil {
		return "", nil, err
	}
	s.mu.Lock()
	s.transfers[id] = &transfer{id: id, path: path, entry: entry, started: time.Now()}
	s.mu.Unlock()
	var once sync.Once
	return id, func() { once.Do(func() { s.mu.Lock(); delete(s.transfers, id); s.mu.Unlock() }) }, nil
}

type spoolMeter struct {
	s  *Service
	id string
}

func (m *spoolMeter) Write(p []byte) (int, error) {
	m.s.mu.Lock()
	if t := m.s.transfers[m.id]; t != nil {
		t.diskBytes += int64(len(p))
	}
	m.s.mu.Unlock()
	return len(p), nil
}
