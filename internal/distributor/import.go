package distributor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrImport = errors.New("import source could not be downloaded")

// importTimeout preserves the administrator import transfer window that the
// shared client deadline used to provide.
const importTimeout = 5 * time.Minute

// ValidImportURL reports whether raw is acceptable as an import source: an
// absolute http(s) URL without credentials or fragment. The query is kept.
func ValidImportURL(raw string) bool {
	_, err := importURL(raw)
	return err == nil
}

func importURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u == nil || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t") {
		return nil, ErrImport
	}
	base := *u
	base.RawQuery = ""
	base.ForceQuery = false
	if _, err = NormalizeBase(base.String(), GeneralHTTP); err != nil {
		return nil, ErrImport
	}
	return u, nil
}

// FetchImport grants one administrator-requested download, with the existing
// configured-origin transport limits. Its URL (including signed query strings)
// is never persisted or returned in errors. It creates no upstream rule.
func (p *Pool) FetchImport(ctx context.Context, appUID, vendorUID, raw string) (*http.Response, error) {
	u, err := importURL(raw)
	if err != nil {
		return nil, err
	}
	c, err := p.NewScopedClient(u.Scheme+"://"+u.Host, GeneralHTTP, appUID, vendorUID)
	if err != nil {
		return nil, ErrImport
	}
	client := *c.HTTP
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return ErrImport
		}
		if _, err := importURL(r.URL.String()); err != nil {
			return ErrImport
		}
		if via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme != "https" {
			return ErrImport
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		r.Header.Del("Referer")
		r.Header.Set("Accept-Encoding", "identity")
		return nil
	}
	// An import keeps its overall transfer window; the body also fails early
	// when the source stops sending bytes.
	importCtx, cancel := context.WithTimeout(ctx, importTimeout)
	request, err := http.NewRequestWithContext(importCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		cancel()
		return nil, ErrImport
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrImport
	}
	if response.StatusCode != 200 || response.Uncompressed || (response.Header.Get("Content-Encoding") != "" && !strings.EqualFold(response.Header.Get("Content-Encoding"), "identity")) {
		response.Body.Close()
		cancel()
		return nil, ErrImport
	}
	watchBody(response, DefaultIdleTimeout, cancel)
	return response, nil
}
