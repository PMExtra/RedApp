package download

import (
	"bytes"
	"context"
	"fmt"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestBoundsAndMissingCompleteFile(t *testing.T) {
	data := []byte("bounded")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, _, _ := setup(t, c)
	r := resource(c, data)
	collect(t, m, r)
	m.mu.Lock()
	g := m.current[r.ID]
	if g.file != nil {
		t.Error("空闲完成缓存仍持有描述符")
	}
	path := g.Path
	m.maxReaders = 1
	m.mu.Unlock()
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = m.Acquire(context.Background(), r); e == nil {
		t.Fatal("读者上限不生效")
	}
	rd.Close()
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("缺失 complete 未重新获取")
	}
	m.mu.Lock()
	m.maxWriters = 0
	m.mu.Unlock()
	r.ID = Identity(r.Source, "another-identity")
	if _, _, e = m.Acquire(context.Background(), r); e == nil {
		t.Fatal("writer 上限未生效")
	}
}
func TestSafe416AndWeakValidatorResume(t *testing.T) {
	data := bytes.Repeat([]byte("range"), 1000)
	for _, mode := range []string{"416", "weak"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-Range") != "" {
					t.Error("弱 validator 不应发 If-Range")
				}
				if mode == "416" {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
					w.WriteHeader(416)
				} else {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 1000-%d/%d", len(data)-1, len(data)))
					w.WriteHeader(206)
					w.Write(data[1000:])
				}
			}))
			m, _, _ := setup(t, c)
			r := resource(c, data)
			m.mu.Lock()
			g, e := m.createLocked(r, false)
			if e != nil {
				t.Fatal(e)
			}
			prefix := data
			if mode == "weak" {
				prefix = data[:1000]
			}
			g.file.WriteAt(prefix, 0)
			g.Bytes = int64(len(prefix))
			g.Total = int64(len(data))
			g.ETag = "W/\"weak\""
			m.save(g)
			m.mu.Unlock()
			rd, _, e := m.Acquire(context.Background(), r)
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(rd)
			rd.Close()
			if e != nil || !bytes.Equal(b, data) {
				t.Fatal(mode, e)
			}
		})
	}
}
