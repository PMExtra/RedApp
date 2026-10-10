package httpserver

import (
	"net/http"
	"strconv"
	"time"
)

// Used only around authorized public file handlers. Metadata, icons, installers,
// administrative warmups, unsuccessful writes, HEAD and 304 never enter ranking.
type downloadReceipt struct {
	http.ResponseWriter
	status int
	bytes  int64
	failed bool
}

func (w *downloadReceipt) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *downloadReceipt) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *downloadReceipt) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	if err != nil || n != len(b) {
		w.failed = true
	}
	return n, err
}
func (w *downloadReceipt) Flush() {
	if w.status == 0 {
		w.status = 200
	}
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		w.failed = true
	}
}
func (s *Server) finishDownload(w *downloadReceipt, r *http.Request, uid string) {
	if r.Method != http.MethodGet || r.Context().Err() != nil || w.failed || (w.status != 200 && w.status != 206) {
		return
	}
	if raw := w.Header().Get("Content-Length"); raw != "" {
		expected, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || expected != w.bytes {
			return
		}
	}
	// Accounting failure cannot change a file response already delivered.
	_ = s.store.RecordDownload(uid, s.proxies.ClientIP(r), time.Now())
}
