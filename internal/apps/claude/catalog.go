package claude

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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ID = "claude-code"
const BaseURL = "https://downloads.claude.ai/claude-code-releases"

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
var platforms = map[string]string{"darwin-arm64": "claude", "darwin-x64": "claude", "linux-arm64": "claude", "linux-x64": "claude", "linux-arm64-musl": "claude", "linux-x64-musl": "claude", "win32-x64": "claude.exe", "win32-arm64": "claude.exe"}

func ValidVersion(v string) bool {
	if len(v) > 128 || !versionPattern.MatchString(v) {
		return false
	}
	for _, n := range strings.Split(strings.SplitN(v, "-", 2)[0], ".") {
		if _, err := strconv.ParseUint(n, 10, 64); err != nil {
			return false
		}
	}
	return true
}

type Platform struct {
	Binary   string `json:"binary"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}
type Manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]Platform `json:"platforms"`
}
type Metadata struct {
	// []byte preserves the exact signed bytes through JSON persistence.
	Raw       []byte
	Signature []byte
	Manifest  Manifest `json:"-"`
}
type channel struct {
	Version string
	Fetched time.Time
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
	ttl      time.Duration
}

func New(db *store.Store, upstream *distributor.Client) *Catalog {
	c := &Catalog{db: db, upstream: upstream, flights: map[string]*flight{}, ttl: time.Minute}
	var ttl int
	if db.Get("setting", "claude-code:channel_ttl", &ttl) == nil && ttl >= 1 && ttl <= 86400 {
		c.ttl = time.Duration(ttl) * time.Second
	}
	return c
}
func (c *Catalog) LatestTTLSeconds() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int(c.ttl / time.Second)
}
func (c *Catalog) SetTTL(seconds int) error {
	if seconds < 1 || seconds > 86400 {
		return errors.New("TTL must be between 1 and 86400 seconds")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.db.Put("setting", "claude-code:channel_ttl", seconds); err != nil {
		return err
	}
	c.ttl = time.Duration(seconds) * time.Second
	return nil
}
func parse(raw []byte, requested string) (Manifest, error) {
	if err := jsoncheck.Unique(raw); err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if json.Unmarshal(raw, &m) != nil || !ValidVersion(m.Version) || m.Version != requested || len(m.Platforms) == 0 || len(m.Platforms) > len(platforms) {
		return m, errors.New("Invalid manifest version or platform count")
	}
	for platform, p := range m.Platforms {
		if platforms[platform] == "" || platforms[platform] != p.Binary || len(p.Checksum) != 64 || strings.ToLower(p.Checksum) != p.Checksum || p.Size <= 0 || p.Size > 4<<30 {
			return m, errors.New("Invalid manifest platform")
		}
		if _, err := hex.DecodeString(p.Checksum); err != nil {
			return m, errors.New("Invalid manifest digest")
		}
	}
	return m, nil
}
func (m *Metadata) validate(version string) error {
	if err := Verify(m.Raw, m.Signature); err != nil {
		return err
	}
	var err error
	m.Manifest, err = parse(m.Raw, version)
	return err
}
func (c *Catalog) Get(ctx context.Context, target string) (Metadata, error) {
	alias := target == "latest" || target == "stable"
	if !alias && !ValidVersion(target) {
		return Metadata{}, errors.New("Invalid Claude version")
	}
	c.mu.Lock()
	if alias {
		var cached channel
		if c.db.Get("claude-channel", target, &cached) == nil && ValidVersion(cached.Version) && time.Since(cached.Fetched) >= 0 && time.Since(cached.Fetched) < c.ttl {
			c.mu.Unlock()
			return c.Get(ctx, cached.Version)
		}
	} else {
		var cached Metadata
		if c.db.Get("claude-metadata", target, &cached) == nil {
			if cached.validate(target) == nil {
				c.mu.Unlock()
				return cached, nil
			}
			// Never authorize resources from corrupted or unverifiable persisted metadata.
			if err := c.db.Delete("claude-metadata", target); err != nil {
				c.mu.Unlock()
				return Metadata{}, err
			}
		}
	}
	f := c.flights[target]
	if f == nil {
		if len(c.flights) >= 32 {
			c.mu.Unlock()
			return Metadata{}, errors.New("Metadata concurrency limit exceeded")
		}
		f = &flight{done: make(chan struct{})}
		c.flights[target] = f
		go c.fetch(target, alias, f)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return Metadata{}, ctx.Err()
	case <-f.done:
		return f.result, f.err
	}
}
func (c *Catalog) body(ctx context.Context, path string, limit int64) ([]byte, error) {
	r, err := c.upstream.Get(ctx, c.upstream.URL(path), http.Header{})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("Metadata HTTP %d", r.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || int64(len(b)) > limit {
		return nil, errors.New("Metadata size exceeds limit")
	}
	return b, nil
}
func (c *Catalog) fetch(target string, alias bool, f *flight) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var m Metadata
	var err error
	if alias {
		var b []byte
		b, err = c.body(ctx, target, 256)
		version := strings.TrimSpace(string(b))
		if err == nil && !ValidVersion(version) {
			err = errors.New("Invalid channel version")
		}
		if err == nil {
			m, err = c.Get(ctx, version)
		}
		if err == nil {
			err = c.db.Put("claude-channel", target, channel{version, time.Now().UTC()})
		}
	} else {
		m.Raw, err = c.body(ctx, target+"/manifest.json", 1<<20)
		if err == nil {
			m.Signature, err = c.body(ctx, target+"/manifest.json.sig", 16<<10)
		}
		if err == nil {
			err = m.validate(target)
		}
		if err == nil {
			err = c.db.PutVersion("claude-metadata", target, m, ID, target)
		}
	}
	if err != nil {
		c.db.Event("claude-metadata:"+target, "metadata", err.Error())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f.result = m
	f.err = err
	delete(c.flights, target)
	close(f.done)
}
func (c *Catalog) Authorize(ctx context.Context, version, platform, name string) (download.Resource, error) {
	if !ValidVersion(version) || platforms[platform] == "" || name != platforms[platform] {
		return download.Resource{}, errors.New("Invalid Claude resource path")
	}
	m, err := c.Get(ctx, version)
	if err != nil {
		return download.Resource{}, err
	}
	p, ok := m.Manifest.Platforms[platform]
	if !ok {
		return download.Resource{}, errors.New("Resource is not in signed manifest")
	}
	source := c.upstream.URL(version + "/" + platform + "/" + p.Binary)
	return download.Resource{ID: download.Identity(source, p.Checksum), Source: source, Hash: p.Checksum, Size: &p.Size, Labels: map[string]string{"app": ID, "version": version, "platform": platform, "name": platform + "/" + p.Binary}}, nil
}
func Compare(a, b string) (int, error) {
	if !ValidVersion(a) || !ValidVersion(b) {
		return 0, errors.New("Invalid Claude version")
	}
	x, y := strings.SplitN(a, "-", 2), strings.SplitN(b, "-", 2)
	xs, ys := strings.Split(x[0], "."), strings.Split(y[0], ".")
	for i := range xs {
		u, _ := strconv.ParseUint(xs[i], 10, 64)
		v, _ := strconv.ParseUint(ys[i], 10, 64)
		if u < v {
			return -1, nil
		}
		if u > v {
			return 1, nil
		}
	}
	if len(x) == 1 && len(y) == 1 {
		return 0, nil
	}
	if len(x) == 1 {
		return 1, nil
	}
	if len(y) == 1 {
		return -1, nil
	}
	// Conservatively retain prereleases when their ordering is not an ordinary stable threshold.
	if x[1] == y[1] {
		return 0, nil
	}
	return 0, errors.New("Unordered prerelease identifiers")
}
func (c *Catalog) Candidates(min string, views []download.View) (map[string]bool, []string, error) {
	if !ValidVersion(min) {
		return nil, nil, errors.New("Invalid minimum version")
	}
	ids := map[string]bool{}
	unknown := []string{}
	for _, v := range views {
		if v.Resource.Labels["app"] != ID {
			continue
		}
		n, err := Compare(v.Resource.Labels["version"], min)
		if err != nil {
			unknown = append(unknown, v.Resource.Labels["version"])
		} else if n < 0 {
			ids[v.Resource.ID] = true
		}
	}
	return ids, unknown, nil
}
