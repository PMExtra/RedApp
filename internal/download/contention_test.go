package download

import (
	"bytes"
	"context"
	"fmt"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func Test100SimultaneousFirstAcquisitionsCreateOneWriter(t *testing.T) {
	data := bytes.Repeat([]byte("contention"), 1000)
	release := make(chan struct{})
	var upstream atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { upstream.Add(1); <-release; w.Write(data) }))
	m, _, _ := setup(t, c)
	resource := resource(c, data)
	start := make(chan struct{})
	ready := make(chan struct{}, 100)
	failures := make(chan error, 100)
	var done sync.WaitGroup
	for i := 0; i < 100; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			<-start
			rd, _, e := m.Acquire(context.Background(), resource)
			ready <- struct{}{}
			if e != nil {
				failures <- e
				return
			}
			defer rd.Close()
			b, e := io.ReadAll(rd)
			if e != nil || !bytes.Equal(b, data) {
				failures <- fmt.Errorf("并发请求数据错误: %v", e)
			}
		}()
	}
	close(start)
	for i := 0; i < 100; i++ {
		<-ready
	}
	close(release)
	done.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	if upstream.Load() != 1 {
		t.Fatalf("首次请求竞争创建了 %d 个上游任务", upstream.Load())
	}
}
