// Package upstream mirrors applications of other RedApp instances: it verifies
// upstream instances, browses their catalogs, binds their applications to
// local ones and keeps the bound applications' templates current.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/media"
)

// maxDocument bounds every JSON document read from an upstream.
const maxDocument = 4 << 20

var (
	// ErrUnavailable reports an upstream that could not be reached or failed.
	ErrUnavailable = errors.New("the upstream is unavailable")
	// ErrIncompatible reports an answer that is not what a compatible RedApp
	// instance returns, an upstream application this instance cannot mirror,
	// or an address that now serves another instance.
	ErrIncompatible = errors.New("the upstream is not a compatible RedApp instance or application")
	// ErrRemoteNotFound reports an application the upstream does not publish.
	ErrRemoteNotFound = errors.New("the upstream does not publish this application")
	// ErrLoop reports an upstream whose chain leads back to this instance.
	ErrLoop = errors.New("the upstream chain leads back to this instance")
)

// NormalizeOrigin accepts http(s)://host[:port] with an optional trailing "/"
// and returns it without one. RedApp serves from the root of its host, so an
// upstream has no path.
func NormalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Opaque != "" {
		return "", errors.New("an upstream address is http(s)://host[:port]")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

type client struct {
	http *distributor.Client
}

func (s *Service) client(origin string) (client, error) {
	c, err := s.pool.NewClient(origin, distributor.GeneralHTTP)
	if err != nil {
		return client{}, fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	return client{http: c}, nil
}

// getJSON reads an upstream API document. A 404 with an error document is
// returned as notFound, anything else unexpected as ErrUnavailable or
// ErrIncompatible.
func (c client) getJSON(ctx context.Context, path string, out any) error {
	source, err := c.http.RelativeURL(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	resp, err := c.http.Get(ctx, source, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrRemoteNotFound
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: HTTP %d", ErrIncompatible, resp.StatusCode)
	case len(body) > maxDocument:
		return fmt.Errorf("%w: document too large", ErrIncompatible)
	}
	if err = json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	return nil
}

// Instance is the identity an upstream reports.
type Instance struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
}

func (c client) instance(ctx context.Context) (Instance, error) {
	var out Instance
	if err := c.getJSON(ctx, "api/instance", &out); err != nil {
		if errors.Is(err, ErrRemoteNotFound) {
			return out, fmt.Errorf("%w: no instance identity", ErrIncompatible)
		}
		return out, err
	}
	if !identity.ValidUID(out.InstanceID) {
		return out, fmt.Errorf("%w: invalid instance identity", ErrIncompatible)
	}
	return out, nil
}

type localized struct {
	En   string `json:"en"`
	ZhCN string `json:"zh-CN"`
}

// mirrorApp is the upstream's getMirrorApp document.
type mirrorApp struct {
	InstanceID string `json:"instance_id"`
	Key        string `json:"key"`
	Provider   string `json:"provider"`
	Vendor     struct {
		ID          string    `json:"id"`
		Name        localized `json:"name"`
		Description localized `json:"description"`
		Icon        string    `json:"icon"`
	} `json:"vendor"`
	Name           localized `json:"name"`
	Description    localized `json:"description"`
	Icon           string    `json:"icon"`
	Instructions   localized `json:"instructions"`
	AutoIndex      bool      `json:"auto_index"`
	Channels       []string  `json:"channels"`
	DefaultChannel *string   `json:"default_channel"`
	// Download choices of a JSON release; absent before RedApp 0.11.1.
	DefaultArtifact string   `json:"default_artifact"`
	HiddenArtifacts []string `json:"hidden_artifacts"`
	UpstreamChain   []string `json:"upstream_chain"`
	SourceEpoch     int64    `json:"source_epoch"`
	Revision        string   `json:"revision"`
}

func (c client) mirror(ctx context.Context, key string) (mirrorApp, error) {
	var out mirrorApp
	if !identity.ValidKey(key) {
		return out, ErrRemoteNotFound
	}
	if err := c.getJSON(ctx, "api/apps/"+key+"/mirror", &out); err != nil {
		return out, err
	}
	if out.Key != key || !identity.ValidUID(out.InstanceID) || out.Revision == "" || out.Name.En == "" {
		return out, fmt.Errorf("%w: invalid mirror description", ErrIncompatible)
	}
	for _, id := range out.UpstreamChain {
		if !identity.ValidUID(id) {
			return out, fmt.Errorf("%w: invalid upstream chain", ErrIncompatible)
		}
	}
	if out.UpstreamChain == nil {
		out.UpstreamChain = []string{}
	}
	return out, nil
}

// RemoteApp is one application of an upstream catalog.
type RemoteApp struct {
	Key         string
	Provider    string
	Name        localized
	Description localized
	Icon        string // path on the upstream
}

// catalogPage is the upstream's listCatalog document.
type catalogPage struct {
	Items []struct {
		Key         string    `json:"key"`
		Provider    string    `json:"provider"`
		Name        localized `json:"name"`
		Description localized `json:"description"`
		Icon        string    `json:"icon"`
	} `json:"items"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func (c client) catalog(ctx context.Context, query string, page, limit int) (catalogPage, error) {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	values.Set("page", strconv.Itoa(page))
	values.Set("limit", strconv.Itoa(limit))
	var out catalogPage
	if err := c.getAPI(ctx, "api/catalog", values, &out); err != nil {
		return out, err
	}
	for _, item := range out.Items {
		if !identity.ValidKey(item.Key) {
			return out, fmt.Errorf("%w: invalid catalog entry", ErrIncompatible)
		}
	}
	return out, nil
}

func (c client) getAPI(ctx context.Context, relative string, query url.Values, out any) error {
	resp, err := c.http.GetAPI(ctx, relative, query)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || len(body) > maxDocument {
		return fmt.Errorf("%w: HTTP %d", ErrIncompatible, resp.StatusCode)
	}
	if err = json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	return nil
}

// icon copies an upstream icon into the local media store. Icons are
// optional: a missing or invalid icon leaves the application without one.
func (s *Service) icon(ctx context.Context, c client, remote string) (string, error) {
	if remote == "" {
		return "", nil
	}
	if !strings.HasPrefix(remote, "/assets/") || strings.Contains(remote, "..") {
		return "", fmt.Errorf("%w: icon path", ErrIncompatible)
	}
	source, err := c.http.RelativeURL(strings.TrimPrefix(remote, "/"))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrIncompatible, err)
	}
	resp, err := c.http.Get(ctx, source, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: icon HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	return s.icons.Put(io.LimitReader(resp.Body, media.MaxBytes+1))
}
