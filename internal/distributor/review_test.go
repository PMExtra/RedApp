package distributor

import (
	"net"
	"testing"
)

func TestUpstreamRejectsNonPublicSpecialAddresses(t *testing.T) {
	for _, s := range []string{"255.255.255.255", "100.64.0.1", "100.127.255.254", "198.18.0.1", "198.19.255.254", "::ffff:100.64.0.1"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatal("特殊地址被当作公开上游", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(s)) {
			t.Fatal("公开地址被拒绝", s)
		}
	}
}
