package codex

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/jsoncheck"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta)(\.(0|[1-9][0-9]*)){0,2})?$`)
var assetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

func Normalize(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "rust-v"), "v")
	if !versionPattern.MatchString(v) {
		return "", errors.New("Invalid version format")
	}
	for _, n := range regexp.MustCompile(`[0-9]+`).FindAllString(v, -1) {
		if _, e := strconv.ParseUint(n, 10, 64); e != nil {
			return "", errors.New("Version number exceeds limit")
		}
	}
	return v, nil
}
func Compare(a, b string) (int, error) {
	var e error
	a, e = Normalize(a)
	if e != nil {
		return 0, e
	}
	b, e = Normalize(b)
	if e != nil {
		return 0, e
	}
	aa := strings.SplitN(a, "-", 2)
	bb := strings.SplitN(b, "-", 2)
	an := strings.Split(aa[0], ".")
	bn := strings.Split(bb[0], ".")
	for i := range an {
		x, _ := strconv.ParseUint(an[i], 10, 64)
		y, _ := strconv.ParseUint(bn[i], 10, 64)
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	if len(aa) == 1 && len(bb) == 1 {
		return 0, nil
	}
	if len(aa) == 1 {
		return 1, nil
	}
	if len(bb) == 1 {
		return -1, nil
	}
	ap := strings.Split(aa[1], ".")
	bp := strings.Split(bb[1], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if i == 0 {
			if ap[i] < bp[i] {
				return -1, nil
			}
			if ap[i] > bp[i] {
				return 1, nil
			}
		} else {
			x, _ := strconv.ParseUint(ap[i], 10, 64)
			y, _ := strconv.ParseUint(bp[i], 10, 64)
			if x < y {
				return -1, nil
			}
			if x > y {
				return 1, nil
			}
		}
	}
	if len(ap) < len(bp) {
		return -1, nil
	}
	if len(ap) > len(bp) {
		return 1, nil
	}
	return 0, nil
}

type Asset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
	Size   *int64 `json:"size,omitempty"`
}
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}
type Metadata struct {
	Raw     json.RawMessage
	Fetched time.Time
	Release Release
}
type flight struct {
	done   chan struct{}
	result Metadata
	err    error
}
type Catalog struct {
	mu       sync.Mutex
	db       *store.Store
	upstream *distributor.Client
	flights  map[string]*flight
	TTL      time.Duration
}

func New(db *store.Store, c *distributor.Client) *Catalog {
	cat := &Catalog{db: db, upstream: c, flights: map[string]*flight{}, TTL: time.Minute}
	var ttl int
	if db.Get("setting", "latest_ttl", &ttl) == nil && ttl >= 1 && ttl <= 86400 {
		cat.TTL = time.Duration(ttl) * time.Second
	}
	return cat
}
func (c *Catalog) LatestTTLSeconds() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int(c.TTL / time.Second)
}

func (c *Catalog) SetTTL(seconds int) error {
	if seconds < 1 || seconds > 86400 {
		return errors.New("TTL must be between 1 and 86400 seconds")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.db.Put("setting", "latest_ttl", seconds); e != nil {
		return e
	}
	c.TTL = time.Duration(seconds) * time.Second
	return nil
}
func (c *Catalog) Get(ctx context.Context, v string) (Metadata, error) {
	if v != "latest" {
		var e error
		v, e = Normalize(v)
		if e != nil {
			return Metadata{}, e
		}
	}
	c.mu.Lock()
	var cached Metadata
	if c.db.Get("metadata", v, &cached) == nil && (v != "latest" || time.Since(cached.Fetched) < c.TTL) {
		c.mu.Unlock()
		return cached, nil
	}
	f := c.flights[v]
	if f == nil {
		if len(c.flights) >= 32 {
			c.mu.Unlock()
			return Metadata{}, errors.New("Metadata concurrency limit exceeded")
		}
		f = &flight{done: make(chan struct{})}
		c.flights[v] = f
		go c.fetch(v, f)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return Metadata{}, ctx.Err()
	case <-f.done:
		return f.result, f.err
	}
}
func (c *Catalog) fetch(v string, f *flight) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := "channels/latest"
	if v != "latest" {
		path = "releases/" + v + "/release.json"
	}
	resp, e := c.upstream.Get(ctx, c.upstream.URL(path), http.Header{})
	var m Metadata
	if e == nil {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			e = fmt.Errorf("Metadata HTTP %d", resp.StatusCode)
		} else {
			var body []byte
			body, e = io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
			if e == nil && len(body) > 4<<20 {
				e = errors.New("Metadata size exceeds limit")
			}
			if e == nil {
				m, e = c.Parse(body, v)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e == nil {
		m.Fetched = time.Now().UTC()
		canonical, _ := Normalize(m.Release.Tag)
		var prior Metadata
		if v != "latest" && c.db.Get("metadata", canonical, &prior) == nil {
			m.Raw = prior.Raw
			m.Release = prior.Release
		}
		e = c.db.PutVersion("metadata", canonical, m, "codex", canonical)
		if e == nil && v == "latest" {
			e = c.db.Put("metadata", "latest", m)
		}
	}
	if e != nil {
		c.db.Event("metadata:"+v, "metadata", e.Error())
	}
	f.result = m
	f.err = e
	delete(c.flights, v)
	close(f.done)
}
func (c *Catalog) Parse(body []byte, requested string) (Metadata, error) {
	if e := jsoncheck.Unique(body); e != nil {
		return Metadata{}, e
	}
	var r Release
	if e := json.Unmarshal(body, &r); e != nil {
		return Metadata{}, errors.New("Invalid JSON metadata")
	}
	v, e := Normalize(r.Tag)
	if e != nil || r.Tag != "rust-v"+v {
		return Metadata{}, errors.New("Invalid release tag")
	}
	if requested != "latest" && requested != v {
		return Metadata{}, errors.New("Release tag does not match request")
	}
	if len(r.Assets) == 0 || len(r.Assets) > 1024 {
		return Metadata{}, errors.New("Asset count exceeds limit")
	}
	seen := map[string]bool{}
	for _, a := range r.Assets {
		if !assetPattern.MatchString(a.Name) || a.Name == "." || a.Name == ".." || seen[a.Name] {
			return Metadata{}, errors.New("Invalid or duplicate asset name")
		}
		seen[a.Name] = true
		if !strings.HasPrefix(a.Digest, "sha256:") || len(a.Digest) != 71 {
			return Metadata{}, errors.New("Missing trusted SHA256")
		}
		if _, e := hex.DecodeString(a.Digest[7:]); e != nil {
			return Metadata{}, errors.New("Invalid SHA256")
		}
		if a.Size != nil && (*a.Size < 0 || *a.Size > 4<<30) {
			return Metadata{}, errors.New("Invalid asset length")
		}
		u, e := url.Parse(a.URL)
		if e != nil || c.upstream.Validate(u) != nil || a.URL != c.upstream.URL("releases/"+v+"/"+a.Name) {
			return Metadata{}, errors.New("Asset URL is not authorized")
		}
	}
	return Metadata{Raw: append(json.RawMessage(nil), body...), Release: r}, nil
}

func (c *Catalog) Authorize(ctx context.Context, v, name string) (download.Resource, error) {
	if !assetPattern.MatchString(name) {
		return download.Resource{}, errors.New("Invalid asset name")
	}
	m, e := c.Get(ctx, v)
	if e != nil {
		return download.Resource{}, e
	}
	v, _ = Normalize(m.Release.Tag)
	for _, a := range m.Release.Assets {
		if a.Name == name {
			hash := strings.ToLower(a.Digest[7:])
			return download.Resource{ID: download.Identity(a.URL, hash), Source: a.URL, Hash: hash, Size: a.Size, Labels: map[string]string{"app": "codex", "version": v, "name": name}}, nil
		}
	}
	return download.Resource{}, errors.New("Resource is not in the trusted manifest")
}
func (m Metadata) Public(base string) Release {
	r := m.Release
	r.Assets = append([]Asset(nil), r.Assets...)
	v, _ := Normalize(r.Tag)
	for i := range r.Assets {
		r.Assets[i].URL = base + "/releases/" + v + "/" + r.Assets[i].Name
	}
	return r
}
func (c *Catalog) Candidates(min string, views []download.View) (map[string]bool, []string, error) {
	min, e := Normalize(min)
	if e != nil {
		return nil, nil, e
	}
	ids := map[string]bool{}
	unknown := []string{}
	for _, v := range views {
		if v.Resource.Labels["app"] != "codex" {
			continue
		}
		n, e := Compare(v.Resource.Labels["version"], min)
		if e != nil {
			unknown = append(unknown, v.Resource.Labels["version"])
			continue
		}
		if n < 0 {
			ids[v.Resource.ID] = true
		}
	}
	return ids, unknown, nil
}
