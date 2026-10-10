package warmplan

import (
	"context"
	"errors"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"io"
	"sync/atomic"
)

var ErrLimited = errors.New("prewarm task read limit reached")

type Limits struct {
	MaxFiles           int   `json:"max_files"`
	MaxDepth           int   `json:"max_depth"`
	MaxDownloadBytes   int64 `json:"max_download_bytes"`
	MaxDurationSeconds int   `json:"max_duration_seconds"`
}

func DefaultLimits() Limits { return Limits{10000, 16, 10 << 30, 3600} }
func (l Limits) Valid() bool {
	return l.MaxFiles >= 1 && l.MaxFiles <= 100000 && l.MaxDepth >= 0 && l.MaxDepth <= 32 && l.MaxDownloadBytes >= 1 && l.MaxDownloadBytes <= 1<<40 && l.MaxDurationSeconds >= 1 && l.MaxDurationSeconds <= 86400
}

type Input struct {
	RetrySkip []string        `json:"retry_skip,omitempty"`
	RequestID string          `json:"request_id"`
	Target    string          `json:"target,omitempty"`
	Platforms []string        `json:"platforms,omitempty"`
	Paths     []string        `json:"paths,omitempty"`
	Indexes   []string        `json:"indexes,omitempty"`
	Manifest  string          `json:"manifest,omitempty"`
	Match     *pathmatch.Spec `json:"match,omitempty"`
	Limits    Limits          `json:"limits"`
}
type Item struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Bytes  int64  `json:"bytes"`
}
type Budget struct {
	Max     int64
	used    atomic.Int64
	limited atomic.Bool
}

func (b *Budget) Used() int64      { return b.used.Load() }
func (b *Budget) Remaining() int64 { return max(0, b.Max-b.Used()) }
func (b *Budget) Limited() bool    { return b.limited.Load() }
func (b *Budget) Consume(n int64) error {
	for {
		old := b.used.Load()
		take := min(n, max(0, b.Max-old))
		if b.used.CompareAndSwap(old, old+take) {
			if take < n {
				b.limited.Store(true)
				return ErrLimited
			}
			return nil
		}
	}
}
func (b *Budget) CheckLength(n int64) error {
	if n >= 0 && n > b.Remaining() {
		b.limited.Store(true)
		return ErrLimited
	}
	return nil
}

type reader struct {
	ctx context.Context
	r   io.Reader
	b   *Budget
}

func (b *Budget) Reader(ctx context.Context, r io.Reader) io.Reader { return &reader{ctx, r, b} }
func (r *reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	remaining := r.b.Remaining()
	if remaining == 0 {
		r.b.limited.Store(true)
		return 0, ErrLimited
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.r.Read(p)
	if e := r.b.Consume(int64(n)); e != nil {
		return n, e
	}
	return n, err
}
