package distributor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestReleaseSourcesDoNotReplaceAddressPolicy(t *testing.T) {
	fake := newFakeNetwork(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "public.test" {
			http.Redirect(w, r, "https://internal.test/api?x=1", http.StatusFound)
			return
		}
		t.Errorf("unexpected private request %s", r.Host)
	}))
	base, err := newPool(fake.network()).NewClient("http://public.test/api", ConfiguredRelease)
	if err != nil {
		t.Fatal(err)
	}
	client, err := base.WithReleaseSources(func(s string) bool {
		return s == "http://public.test/api?product=app" || s == "https://internal.test/api?x=1"
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(context.Background(), "http://public.test/api?product=app", nil)
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrAddressNotAllowed) || fake.wasDialed("10.0.0.5") {
		t.Fatal("explicit URL policy bypassed address policy", err)
	}
	u, _ := url.Parse("http://public.test/api?product=app")
	if base.Validate(u) == nil {
		t.Fatal("original client was broadened")
	}
}

func TestReleaseSourcesAuthorizeEveryHopAndKeepStreamRules(t *testing.T) {
	fake := newFakeNetwork(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/forbidden?key=secret", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/compressed":
			w.Header().Set("Content-Encoding", "gzip")
			io.WriteString(w, "not identity")
		case "/allowed":
			if r.URL.Query().Get("platform") != "win-x64" {
				t.Error("query lost")
			}
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Error("credentials forwarded")
			}
			io.WriteString(w, "ok")
		default:
			t.Error("unauthorized hop reached", r.URL)
		}
	}))
	base, err := newPool(fake.network()).NewClient("http://public.test", ConfiguredRelease)
	if err != nil {
		t.Fatal(err)
	}
	client, err := base.WithReleaseSources(func(s string) bool {
		return strings.HasPrefix(s, "http://public.test/") && !strings.Contains(s, "forbidden")
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"start", "loop", "compressed"} {
		resp, err := client.Get(context.Background(), "http://public.test/"+path, nil)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("unsafe response accepted", path)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("query leaked", err)
		}
	}
	resp, err := client.Get(context.Background(), "http://public.test/allowed?platform=win-x64", http.Header{"Cookie": {"secret"}, "Authorization": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "ok" {
		t.Fatal(string(body), err)
	}
}
