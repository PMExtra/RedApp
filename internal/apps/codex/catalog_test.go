package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(base, v string) []byte {
	hash := sha256.Sum256([]byte("artifact"))
	b, _ := json.Marshal(Release{Tag: "rust-v" + v, Assets: []Asset{{Name: "asset.tar.gz", Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: base + "/releases/" + v + "/asset.tar.gz"}, {Name: "other.tgz", Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: base + "/releases/" + v + "/other.tgz"}}})
	return b
}
func TestMetadataLazyTTLFailureAndHistory(t *testing.T) {
	var count atomic.Int32
	var fail atomic.Bool
	var base string
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "latest") && !strings.HasSuffix(r.URL.Path, "release.json") {
			t.Errorf("variant 被预下载: %s", r.URL.Path)
		}
		count.Add(1)
		if fail.Load() {
			http.Error(w, "down", 503)
			return
		}
		w.Write(fixture(base, "0.159.2"))
	}))
	base = c.Base.String()
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	cat := New(db, c)
	if _, e = cat.Get(context.Background(), "latest"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := cat.Get(context.Background(), "latest"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("TTL 内未复用")
	}
	history, e := db.Versions()
	if e != nil {
		t.Fatal(e)
	}
	first := history["0.159.2"]
	// Expire the persisted successful timestamp deterministically, without waiting a minute.
	var m Metadata
	if e = db.Get("metadata", "latest", &m); e != nil {
		t.Fatal(e)
	}
	m.Fetched = time.Now().Add(-2 * time.Minute)
	if e = db.Put("metadata", "latest", m); e != nil {
		t.Fatal(e)
	}
	fail.Store(true)
	if _, e = cat.Get(context.Background(), "latest"); e == nil {
		t.Fatal("过期 latest 刷新失败仍返回旧值")
	}
	var after Metadata
	db.Get("metadata", "latest", &after)
	if !after.Fetched.Equal(m.Fetched) {
		t.Fatal("失败延长了有效期")
	}
	fail.Store(false)
	if _, e = cat.Get(context.Background(), "0.159.2"); e != nil {
		t.Fatal(e)
	}
	if count.Load() != 2 {
		t.Fatal("release 永久缓存重新回源")
	}
	if e = cat.SetTTL(3600); e != nil {
		t.Fatal(e)
	}
	if _, e = cat.Get(context.Background(), "latest"); e != nil {
		t.Fatal("设置 TTL 未生效", e)
	}
	history, _ = db.Versions()
	if history["0.159.2"] != first {
		t.Fatal("first_seen 改变")
	}
	if _, e = cat.Authorize(context.Background(), "0.159.2", "nonexistent"); e == nil {
		t.Fatal("未授权资产被接受")
	}
}
func TestExpiredMetadataSingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var base string
	var count atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			close(started)
		}
		<-release
		w.Write(fixture(base, "0.159.2"))
	}))
	base = c.Base.String()
	db, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	cat := New(db, c)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := cat.Get(context.Background(), "latest"); e != nil {
				t.Error(e)
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("元数据刷新 %d 次", count.Load())
	}
}
func TestMalformedMetadataRejected(t *testing.T) {
	c, _ := testutil.Upstream(t, http.NotFoundHandler())
	cat := &Catalog{upstream: c}
	good := string(fixture(c.Base.String(), "0.159.2"))
	cases := []string{`{"tag_name":"rust-v0.159.2","tag_name":"rust-v0.159.2","assets":[]}`, strings.Replace(good, "sha256:", "sha512:", 1), strings.Replace(good, "asset.tar.gz", "../asset.tar.gz", 1), strings.Replace(good, c.Base.String(), "https://evil.example", 1), strings.Replace(good, "rust-v0.159.2", "rust-v0.159.3", 1), strings.Replace(good, "other.tgz", "asset.tar.gz", -1), good + " trailing"}
	for i, b := range cases {
		if _, e := cat.Parse([]byte(b), "0.159.2"); e == nil {
			t.Errorf("bad case %d accepted", i)
		}
	}
	m, e := cat.Parse([]byte(good), "0.159.2")
	if e != nil {
		t.Fatal(e)
	}
	rewritten := m.Public("https://enterprise.example")
	if !strings.HasPrefix(rewritten.Assets[0].URL, "https://enterprise.example/") || rewritten.Assets[0].Digest != m.Release.Assets[0].Digest {
		t.Fatal("改写破坏哈希或原始清单")
	}
	if rewritten.Assets[0].Size != nil {
		t.Fatal("缺 size 被当作0")
	}
}
func TestVersionOrderingAndCleanupUnknown(t *testing.T) {
	cat := &Catalog{}
	for _, pair := range [][2]string{{"v0.9.0", "rust-v0.10.0"}, {"0.150.0-alpha.2", "0.150.0-alpha.10"}, {"0.150.0-beta.1", "0.150.0"}, {"0.150.0-alpha", "0.150.0-beta"}} {
		n, e := Compare(pair[0], pair[1])
		if e != nil || n >= 0 {
			t.Fatal(pair, n, e)
		}
	}
	for _, v := range []string{"../1.0.0", "0.1", "0.01.0", "1.0.0-unknown", "99999999999999999999999.0.0"} {
		if _, e := Normalize(v); e == nil {
			t.Fatal(v)
		}
	}
	ids, unknown, e := cat.Candidates("0.150.0", []download.View{{Generation: download.Generation{Resource: download.Resource{ID: "old", Labels: map[string]string{"app": "codex", "version": "0.149.9"}}}}, {Generation: download.Generation{Resource: download.Resource{ID: "unknown", Labels: map[string]string{"app": "codex", "version": "nonsense"}}}}})
	if e != nil || !ids["old"] || ids["unknown"] || len(unknown) != 1 {
		t.Fatal(fmt.Sprint(ids, unknown, e))
	}
}
