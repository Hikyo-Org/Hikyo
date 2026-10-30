package server

import (
	"net"
	"net/http/httptest"
	"testing"
)

// These are source checks for audit/admission attribution. A deployment still
// needs to prove its actual proxy chain and trusted CIDRs through real ingress.
func TestSourceIPTrustedProxyBoundary(t *testing.T) {
	cases := []struct {
		name      string
		remote    string
		forwarded string
		proxies   []string
		want      string
	}{
		{name: "no configured trust", remote: "192.0.2.10:4321", forwarded: "198.51.100.7", want: "192.0.2.10"},
		{name: "untrusted peer cannot spoof", remote: "198.51.100.7:4321", forwarded: "203.0.113.8", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "trusted peer without header", remote: "192.0.2.10:4321", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "single client", remote: "192.0.2.10:4321", forwarded: "198.51.100.7", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "right to left skips trusted proxies", remote: "192.0.2.10:4321", forwarded: "203.0.113.8, 198.51.100.7, 192.0.2.11", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "all hops trusted returns peer", remote: "192.0.2.10:4321", forwarded: "192.0.2.12, 192.0.2.11", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "trim whitespace", remote: "192.0.2.10:4321", forwarded: " 198.51.100.7 ,\t192.0.2.11 ", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "malformed rightmost falls back to peer", remote: "192.0.2.10:4321", forwarded: "198.51.100.7, invalid", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "malformed interior falls back to peer", remote: "192.0.2.10:4321", forwarded: "198.51.100.7, invalid, 192.0.2.11", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "empty hop falls back to peer", remote: "192.0.2.10:4321", forwarded: "198.51.100.7, ,192.0.2.11", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "forwarded port is not an address", remote: "192.0.2.10:4321", forwarded: "198.51.100.7:1234", proxies: []string{"192.0.2.0/24"}, want: "192.0.2.10"},
		{name: "untrusted hop bounds client controlled prefix", remote: "192.0.2.10:4321", forwarded: "invalid, 198.51.100.7, 192.0.2.11", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "peer without port", remote: "192.0.2.10", forwarded: "198.51.100.7", proxies: []string{"192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "malformed peer never trusts header", remote: "invalid", forwarded: "198.51.100.7", proxies: []string{"192.0.2.0/24"}, want: "invalid"},
		{name: "untrusted IPv6 peer", remote: "[2001:db8:2::7]:4321", forwarded: "2001:db8:3::8", proxies: []string{"2001:db8:1::/48"}, want: "2001:db8:2::7"},
		{name: "trusted IPv6 chain", remote: "[2001:db8:1::10]:4321", forwarded: "2001:db8:3::8, 2001:db8:2::7, 2001:db8:1::11", proxies: []string{"2001:db8:1::/48"}, want: "2001:db8:2::7"},
		{name: "mixed address families", remote: "[2001:db8:1::10]:4321", forwarded: "198.51.100.7, 192.0.2.11", proxies: []string{"2001:db8:1::/48", "192.0.2.0/24"}, want: "198.51.100.7"},
		{name: "IPv6 all trusted", remote: "[2001:db8:1::10]:4321", forwarded: "2001:db8:1::11", proxies: []string{"2001:db8:1::/48"}, want: "2001:db8:1::10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &API{TrustedProxies: sourceIPTestNetworks(t, tc.proxies)}
			r := httptest.NewRequest("GET", "/api/v1/auth/whoami", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			if got := a.sourceIP(r); got != tc.want {
				t.Fatalf("sourceIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrustedProxyCIDRs(t *testing.T) {
	a := &API{TrustedProxies: sourceIPTestNetworks(t, []string{"192.0.2.0/24", "2001:db8:1::/48"})}
	for _, tc := range []struct {
		address string
		want    bool
	}{
		{"192.0.2.10", true},
		{"::ffff:192.0.2.10", true},
		{"198.51.100.7", false},
		{"2001:db8:1::10", true},
		{"2001:db8:2::7", false},
		{"invalid", false},
		{"192.0.2.10:4321", false},
		{"", false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			if got := a.trusted(tc.address); got != tc.want {
				t.Fatalf("trusted(%q) = %v, want %v", tc.address, got, tc.want)
			}
		})
	}
}

func sourceIPTestNetworks(t *testing.T, cidrs []string) []*net.IPNet {
	t.Helper()
	var networks []*net.IPNet
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		networks = append(networks, network)
	}
	return networks
}
