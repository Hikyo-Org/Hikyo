package remotefetch

import (
	"net"
	"net/url"
	"testing"
)

func TestProxyDialAddressPreservesIPv6Authority(t *testing.T) {
	for _, tc := range []struct{ raw, host, port string }{
		{"https://[2001:db8::10]", "2001:db8::10", "443"},
		{"https://[2001:db8::10]:8443", "2001:db8::10", "8443"},
		{"https://proxy.example", "proxy.example", "443"},
		{"https://proxy.example:8443", "proxy.example", "8443"},
		{"https://127.0.0.1", "127.0.0.1", "443"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			proxy, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			host, port, err := net.SplitHostPort(proxyDialAddress(proxy))
			if err != nil || host != tc.host || port != tc.port {
				t.Fatalf("dial authority host=%q port=%q err=%v", host, port, err)
			}
		})
	}
}
