package warmplan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

type endingReader struct {
	data            string
	end             error
	together        bool
	calls, consumed int
}

func (r *endingReader) Read(p []byte) (int, error) {
	r.calls++
	n := copy(p, r.data)
	r.data = r.data[n:]
	r.consumed += n
	if len(r.data) == 0 && (n == 0 || r.together) {
		return n, r.end
	}
	return n, nil
}

func TestBudgetReaderBoundary(t *testing.T) {
	for _, known := range []bool{false, true} {
		for _, body := range []string{"ab", "abc", "abcd", "abcdefgh"} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/known=%t/together=%t", body, known, together), func(t *testing.T) {
					budget := &Budget{Max: 3}
					src := &endingReader{data: body, end: io.EOF, together: together}
					if known && len(body) > 3 {
						if !errors.Is(budget.CheckLength(int64(len(body))), ErrLimited) || budget.Used() != 0 || src.calls != 0 {
							t.Fatal("known oversize not rejected before reading")
						}
						return
					}
					data, err := io.ReadAll(budget.Reader(context.Background(), src))
					if len(body) <= 3 {
						if err != nil || string(data) != body || budget.Limited() || budget.Used() != int64(len(body)) {
							t.Fatal(string(data), err, budget.Used())
						}
					} else {
						if !errors.Is(err, ErrLimited) || string(data) != "abc" || !budget.Limited() || budget.Used() != 4 || src.consumed != 4 {
							t.Fatal(string(data), err, budget.Used(), src.consumed)
						}
						// A new reader sharing an already exceeded budget must not probe again.
						next := &endingReader{data: "another", end: io.EOF}
						_, err = budget.Reader(context.Background(), next).Read(make([]byte, 1))
						if !errors.Is(err, ErrLimited) || next.calls != 0 {
							t.Fatal("repeated overflow probe")
						}
					}
				})
			}
		}
	}
}
func TestBudgetReaderTerminalErrors(t *testing.T) {
	failure := errors.New("upstream failed")
	for _, tc := range []struct {
		name, data string
		end        error
		want       error
		used       int64
		limited    bool
	}{
		{"exact-eof", "abc", io.EOF, io.EOF, 3, false},
		{"exact-error", "abc", failure, failure, 3, false},
		{"overflow-error", "abcd", failure, ErrLimited, 4, true},
		{"stalled-boundary", "abc", nil, io.ErrNoProgress, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &endingReader{data: tc.data, end: tc.end, together: tc.name == "overflow-error"}
			budget := &Budget{Max: 3}
			r := budget.Reader(context.Background(), src)
			buf := make([]byte, 3)
			if n, err := r.Read(buf); n != 3 || err != nil {
				t.Fatal(n, err)
			}
			if n, err := r.Read(nil); n != 0 || err != nil || src.calls != 1 {
				t.Fatal("empty read probed source")
			}
			n, err := r.Read(buf)
			if n != 0 || !errors.Is(err, tc.want) || budget.Used() != tc.used || budget.Limited() != tc.limited {
				t.Fatal(n, err, budget.Used())
			}
			if tc.name == "overflow-error" && !errors.Is(err, failure) {
				t.Fatal("lost source error", err)
			}
			calls := src.calls
			for i := 0; i < 3; i++ {
				r.Read(buf)
			}
			if src.calls != calls {
				t.Fatal("terminal source read again")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := &endingReader{data: "x", end: io.EOF}
	_, err := (&Budget{Max: 0}).Reader(ctx, src).Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) || src.calls != 0 {
		t.Fatal(err, src.calls)
	}
}

func TestBudgetReaderPreservesDataAndEarlyError(t *testing.T) {
	failure := errors.New("read failed before budget")
	src := &endingReader{data: "ab", end: failure, together: true}
	budget := &Budget{Max: 3}
	r := budget.Reader(context.Background(), src)
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if n != 2 || string(buf[:n]) != "ab" || !errors.Is(err, failure) || budget.Used() != 2 || budget.Limited() {
		t.Fatal(n, err, budget.Used())
	}
	if _, err = r.Read(buf); !errors.Is(err, failure) || src.calls != 1 {
		t.Fatal("terminal error lost or reread", err, src.calls)
	}
}
