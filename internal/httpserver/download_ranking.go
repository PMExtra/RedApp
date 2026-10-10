package httpserver

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// downloadWriteIdle bounds one write of a file response that makes no
// progress, for example to a stalled client. File responses have no total
// deadline: large files on slow links may stream for as long as they progress,
// so the server's WriteTimeout is replaced before every write.
const downloadWriteIdle = 60 * time.Second

// Used only around authorized public file handlers. Metadata, icons, installers,
// administrative warmups, unsuccessful writes, HEAD and 304 never enter ranking.
type downloadReceipt struct {
	http.ResponseWriter
	// ctx is the response's application work; once it ends, the deadline its
	// cancellation set is never extended again.
	ctx    context.Context
	status int
	bytes  int64
	failed bool
}

func newDownloadReceipt(w http.ResponseWriter, r *http.Request) *downloadReceipt {
	return &downloadReceipt{ResponseWriter: w, ctx: r.Context()}
}

// extend moves the write deadline to downloadWriteIdle from now. The check
// after setting it keeps an abort that application work cancellation set
// concurrently (applicationResponse), whichever deadline was set last.
func (w *downloadReceipt) extend() {
	if w.ctx == nil || w.ctx.Err() != nil {
		return
	}
	controller := http.NewResponseController(w.ResponseWriter)
	_ = controller.SetWriteDeadline(time.Now().Add(downloadWriteIdle))
	if w.ctx.Err() != nil {
		_ = controller.SetWriteDeadline(time.Now())
	}
}

func (w *downloadReceipt) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *downloadReceipt) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.extend()
	w.ResponseWriter.WriteHeader(status)
}
func (w *downloadReceipt) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.extend()
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
	w.extend()
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
