package httpcache

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"golang.org/x/net/html"
	"io"
	"net/url"
	"strings"
)

type indexLink struct {
	Href      string
	Directory bool
}

// JSON formats match nginx autoindex and Caddy browse's official array output.
func parseIndex(body []byte, contentType string) ([]indexLink, error) {
	out := []indexLink{}
	if strings.Contains(contentType, "json") || len(body) > 0 && body[0] == '[' {
		var entries []struct {
			Name  string `json:"name"`
			URL   string `json:"url"`
			Type  string `json:"type"`
			IsDir *bool  `json:"is_dir"`
		}
		if err := json.Unmarshal(body, &entries); err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Type != "" && e.Type != "directory" && e.Type != "file" {
				continue
			}
			href := e.URL
			if href == "" {
				href = url.PathEscape(e.Name)
			}
			directory := e.Type == "directory" || e.IsDir != nil && *e.IsDir || strings.HasSuffix(e.Name, "/") || strings.HasSuffix(href, "/")
			if directory && !strings.HasSuffix(href, "/") {
				href += "/"
			}
			out = append(out, indexLink{href, directory})
		}
		return out, nil
	}
	node, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					out = append(out, indexLink{a.Val, strings.HasSuffix(a.Val, "/")})
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return out, nil
}
func discoveredPath(client *distributor.Client, directory *url.URL, link indexLink) (string, error) {
	if link.Href == "" || strings.ContainsAny(link.Href, "?#") {
		return "", errIndexLink
	}
	u, err := url.Parse(link.Href)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errIndexLink
	}
	raw := strings.ToLower(u.EscapedPath())
	if strings.ContainsAny(u.Path, "\\\x00") || strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(raw, "%25") {
		return "", errIndexLink
	}
	for _, segment := range strings.Split(strings.TrimPrefix(u.Path, "./"), "/") {
		if segment == ".." || segment == "." {
			return "", errIndexLink
		}
	}
	if strings.HasPrefix(link.Href, "./") {
		u.Path = strings.TrimPrefix(u.Path, "./")
		u.RawPath = ""
	}
	target := directory.ResolveReference(u)
	if !client.IndexBoundary(target) {
		return "", errIndexLink
	}
	path := "/" + strings.TrimPrefix(target.Path, strings.TrimSuffix(client.Base.Path, "/")+"/")
	if pathmatch.ValidatePath(strings.TrimSuffix(path, "/")) != nil && path != "/" {
		return "", errIndexLink
	}
	return path, nil
}

var errIndexLink = errors.New("Ignored unsafe directory link")

// Discover streams paths into the dedicated job; listings never authorize deletion.
func (s *Service) Discover(ctx context.Context, entry application.Entry, indexes []string, limits warmplan.Limits, budget *warmplan.Budget, emit func(string) error, summary func(string)) (err error) {
	ctx, finish, err := s.db.ApplicationWork(ctx, entry.StorageID())
	if err != nil {
		return err
	}
	defer finish()
	if err = s.begin(entry); err != nil {
		return err
	}
	defer s.wg.Done()
	release, err := s.budget.AcquireHTTPReader()
	if err != nil {
		return err
	}
	defer release()
	attempts, err := s.sourceAttempts(entry)
	if err != nil {
		return err
	}
	files := map[string]bool{}
	for _, root := range indexes {
		if !strings.HasPrefix(root, "/") || strings.Contains(root, "://") || pathmatch.ValidatePath(strings.TrimSuffix(root, "/")) != nil && root != "/" {
			return errIndexLink
		}
		type directory struct {
			path  string
			depth int
		}
		seen := map[string]bool{}
		var chosen *distributor.Client
		queue := []directory{{root, 0}}
		for len(queue) > 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d := queue[0]
			queue = queue[1:]
			if seen[d.path] {
				summary("duplicate_directory")
				continue
			}
			if len(seen) >= 100000 {
				return warmplan.ErrLimited
			}
			seen[d.path] = true
			if d.depth > limits.MaxDepth {
				return warmplan.ErrLimited
			}
			var respBody []byte
			var location *url.URL
			var contentType string
			clients := attempts
			if chosen != nil {
				clients = []sourceAttempt{{Client: chosen}}
			}
			var last error
			for _, a := range clients {
				writer, err := s.budget.AcquireHTTPWriter()
				if err != nil {
					return err
				}
				resp, err := a.Client.Index(ctx, strings.TrimPrefix(d.path, "/"))
				if err != nil {
					writer()
					_ = s.upstreamFailure(entry, d.path, 0)
					last = err
					continue
				}
				if resp.StatusCode != 200 || resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
					resp.Body.Close()
					writer()
					_ = s.upstreamFailure(entry, d.path, resp.StatusCode)
					last = ErrUpstream
					continue
				}
				if resp.ContentLength > 2<<20 || resp.ContentLength > s.budget.MaxArtifactBytes() {
					resp.Body.Close()
					writer()
					return warmplan.ErrLimited
				}
				if err = budget.CheckLength(resp.ContentLength); err != nil {
					resp.Body.Close()
					writer()
					return err
				}
				body := budget.Reader(ctx, resp.Body)
				if resp.ContentLength >= 0 {
					body = io.LimitReader(body, resp.ContentLength)
				}
				body = &upstreamBody{ReadCloser: readerCloser{Reader: body, Closer: resp.Body}, s: s, app: entry.MetricsID()}
				respBody, err = io.ReadAll(io.LimitReader(body, min(int64(2<<20), s.budget.MaxArtifactBytes())+1))
				resp.Body.Close()
				writer()
				if err != nil {
					return err
				}
				if len(respBody) > 2<<20 || int64(len(respBody)) > s.budget.MaxArtifactBytes() {
					return warmplan.ErrLimited
				}
				contentType = resp.Header.Get("Content-Type")
				location = resp.Request.URL
				chosen = a.Client
				last = nil
				break
			}
			if last != nil {
				return ErrUpstream
			}
			links, err := parseIndex(respBody, contentType)
			if err != nil {
				return ErrUpstream
			}
			for _, link := range links {
				p, err := discoveredPath(chosen, location, link)
				if err != nil {
					summary("unsafe_link")
					continue
				}
				if link.Directory {
					if len(queue)+len(seen) >= 100000 {
						return warmplan.ErrLimited
					}
					queue = append(queue, directory{p, d.depth + 1})
				} else {
					if files[p] {
						summary("duplicate_file")
						continue
					}
					files[p] = true
					if err = emit(p); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

type readerCloser struct {
	io.Reader
	io.Closer
}
