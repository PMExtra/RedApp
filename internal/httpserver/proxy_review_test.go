package httpserver

import (
	"net/http/httptest"
	"testing"
)

func TestMalformedForwardedNeverChangesTrustedPeer(t *testing.T) {
	p, _ := NewProxy("10.0.0.0/8")
	for _, h := range []string{`for="";for=8.8.8.8`, `for="\\x38.8.8.8"`, `for=8.8.8.8;proto="";proto=https`, `=x;for=8.8.8.8`, `for="8.8.8.8"garbage`} {
		r := httptest.NewRequest("GET", "http://redapp.local/", nil)
		r.RemoteAddr = "10.0.0.1:4321"
		r.Header.Set("Forwarded", h)
		r.Header.Set("X-Forwarded-For", "9.9.9.9")
		if got := p.ClientIP(r); got != "10.0.0.1" {
			t.Fatalf("畸形头被信任: %s -> %s", h, got)
		}
	}
	v, e := unquoteForwarded(`"a\nb"`)
	if e != nil || v != "anb" {
		t.Fatalf("错误的 quoted-pair: %q %v", v, e)
	}
}
