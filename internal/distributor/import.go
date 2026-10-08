package distributor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

var ErrImport = errors.New("Import source could not be downloaded")

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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrImport
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrImport
	}
	if response.StatusCode != 200 || response.Uncompressed || (response.Header.Get("Content-Encoding") != "" && !strings.EqualFold(response.Header.Get("Content-Encoding"), "identity")) {
		response.Body.Close()
		return nil, ErrImport
	}
	return response, nil
}
