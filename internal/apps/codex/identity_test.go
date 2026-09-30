package codex

import (
	"context"
	"encoding/json"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreshLatestChangedDigestGetsNewResourceIdentity(t *testing.T) {
	var changed atomic.Bool
	var base string
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := fixture(base, "0.159.2")
		if changed.Load() {
			var release Release
			json.Unmarshal(body, &release)
			release.Assets[0].Digest = "sha256:" + strings.Repeat("a", 64)
			body, _ = json.Marshal(release)
		}
		w.Write(body)
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
	old, e := cat.Authorize(context.Background(), "0.159.2", "asset.tar.gz")
	if e != nil {
		t.Fatal(e)
	}
	history, _ := db.Versions()
	var expired Metadata
	db.Get("metadata", "latest", &expired)
	expired.Fetched = time.Now().Add(-2 * time.Minute)
	db.Put("metadata", "latest", expired)
	changed.Store(true)
	if _, e = cat.Get(context.Background(), "latest"); e != nil {
		t.Fatal(e)
	}
	fresh, e := cat.Authorize(context.Background(), "0.159.2", "asset.tar.gz")
	if e != nil || fresh.ID == old.ID || fresh.Hash == old.Hash {
		t.Fatal("新可信哈希复用了旧身份", e)
	}
	after, _ := db.Versions()
	if after["0.159.2"] != history["0.159.2"] {
		t.Fatal("身份变化重写了 first_seen")
	}
}
