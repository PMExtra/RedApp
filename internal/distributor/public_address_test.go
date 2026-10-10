package distributor

import (
	"net"
	"testing"
)

func TestUpstreamRejectsNonPublicSpecialAddresses(t *testing.T) {
	for _, s := range []string{"255.255.255.255", "100.64.0.1", "100.127.255.254", "198.18.0.1", "198.19.255.254", "::ffff:100.64.0.1"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatal("special-purpose address treated as public", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(s)) {
			t.Fatal("public address rejected", s)
		}
	}
}
