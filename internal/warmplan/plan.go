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
	ArtifactKeys []string        `json:"artifact_keys,omitempty"`
	RetrySkip    []string        `json:"retry_skip,omitempty"`
	RequestID    string          `json:"request_id"`
	Target       string          `json:"target,omitempty"`
	Platforms    []string        `json:"platforms,omitempty"`
	Paths        []string        `json:"paths,omitempty"`
	Indexes      []string        `json:"indexes,omitempty"`
	Manifest     string          `json:"manifest,omitempty"`
	Match        *pathmatch.Spec `json:"match,omitempty"`
	Limits       Limits          `json:"limits"`
}
type Item struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Bytes  int64  `json:"bytes"`
}

// Budget counts bytes consumed by the single prewarm worker, not upstream
// traffic. An exhausted unknown-length stream may consume one extra byte to
// distinguish EOF from overflow; Used includes that byte (at most Max+1).
// Reader calls are serialized by the worker; atomic fields allow progress reads.
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
	ctx      context.Context
	r        io.Reader
	b        *Budget
	terminal error
}

func (b *Budget) Reader(ctx context.Context, r io.Reader) io.Reader {
	return &reader{ctx: ctx, r: r, b: b}
}
func (r *reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.terminal != nil {
		return 0, r.terminal
	}
	if r.b.Limited() {
		return 0, ErrLimited
	}
	remaining := r.b.Remaining()
	if remaining == 0 {
		// Do not fabricate EOF: one bounded probe distinguishes an exact fit
		// from a truncated stream. Never expose the overflow byte to the caller.
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			r.b.used.Add(int64(n))
			r.b.limited.Store(true)
			r.terminal = ErrLimited
			if err != nil && err != io.EOF {
				r.terminal = errors.Join(ErrLimited, err)
			}
		} else if err != nil {
			r.terminal = err
		} else {
			// A reader making no progress must not trigger unbounded EOF probes.
			r.terminal = io.ErrNoProgress
		}
		return 0, r.terminal
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.r.Read(p)
	if e := r.b.Consume(int64(n)); e != nil {
		if err != nil && err != io.EOF {
			e = errors.Join(e, err)
		}
		r.terminal = e
		return n, e
	}
	if err != nil {
		r.terminal = err
	}
	return n, err
}
