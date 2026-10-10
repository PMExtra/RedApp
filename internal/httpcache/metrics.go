package httpcache

import (
	"io"
	"net/http"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/store"
)

// Files reports stored entries and the part files of running fills for the
// shared capacity and disk metrics.
func (s *Service) Files() ([]spool.FileStatus, error) {
	entries, err := s.db.AllHTTPCacheEntries()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Pins also include short maintenance holds; only as many as there are
	// public responses for the path count as readers.
	remaining := map[string]int{}
	for key, count := range s.readers {
		remaining[key] = count
	}
	out := make([]spool.FileStatus, 0, len(entries)+len(s.streams))
	for _, e := range entries {
		key := e.StorageID + "\x00" + e.Path
		readers := min(s.pins[e.ID], remaining[key])
		remaining[key] -= readers
		if st := s.streams[e.ID]; st != nil {
			readers += st.readers
		}
		out = append(out, spool.FileStatus{Scope: metricScope(e.StorageID), Path: s.bodyPath(e.ID), Bytes: e.SizeBytes, State: "complete", Current: e.Current, Retired: !e.Current, Readers: readers})
	}
	for _, st := range s.streams {
		if st.finished {
			continue
		}
		out = append(out, spool.FileStatus{Scope: st.fill.entry.MetricsID(), Path: s.partPath(st.id), Bytes: st.body.Size(), State: "downloading", Current: true, ActiveWriter: true, Readers: st.readers})
	}
	return out, nil
}

// upstreamBody counts the bytes of an upstream body that is not streamed into
// the cache, such as an uncacheable response or a directory listing.
type upstreamBody struct {
	io.ReadCloser
	s   *Service
	app string
}

func (b *upstreamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
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
