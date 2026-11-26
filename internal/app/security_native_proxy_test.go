package app

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

type unreachableFetcher struct{}

func (unreachableFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	return nil, nil, bridgerevocation.ErrUnreachable
}

// The native security service has no operator environment: the proxy the revocation fetcher
// uses comes from the host's own file, like the policy, and never from the service's process
// environment.
func TestNativeSecurityRevocationFetcherUsesTheHostProxyNotTheProcessEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://process-proxy.invalid:3128")
	var resolved *url.URL
	previous := newRevocationFetcher
	newRevocationFetcher = func(_ string, proxy func(*http.Request) (*url.URL, error)) (bridgerevocation.Fetcher, error) {
		request, err := http.NewRequest(http.MethodGet, "https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations.json", nil)
		if err != nil {
			return nil, err
		}
		resolved, err = proxy(request)
		return unreachableFetcher{}, err
	}
	t.Cleanup(func() { newRevocationFetcher = previous })

	for name, tc := range map[string]struct {
		policy map[string]string
		want   string
	}{
		"host proxy":                {map[string]string{"https_proxy": "http://host-proxy.invalid:3128"}, "http://host-proxy.invalid:3128"},
		"host no_proxy":             {map[string]string{"HTTPS_PROXY": "http://host-proxy.invalid:3128", "NO_PROXY": "github.com"}, ""},
		"host sets no proxy at all": {map[string]string{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			resolved = nil
			host := &nativeDriverHost{configuration: hostupgrade.Config{StateDirectory: t.TempDir()}, policy: tc.policy}
			guard := nativeSecurityRevocationGuard(host)
			if _, err := bridgeRevocation(guard)(t.Context(), trustfixture.New(t).Snapshot(t)); err != nil {
				t.Fatal(err)
			}
			got := ""
			if resolved != nil {
				got = resolved.String()
			}
			if got != tc.want {
				t.Fatalf("proxy = %q, want %q", got, tc.want)
			}
		})
	}
}
