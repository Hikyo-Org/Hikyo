package service

import "testing"

func TestCanonicalOriginRequiresHTTPSOutsideLoopback(t *testing.T) {
	t.Parallel()

	accepted := map[string]string{
		"https://remote.example": "https://remote.example",
		"http://localhost:8080":  "http://localhost:8080",
		"http://127.0.0.42":      "http://127.0.0.42",
		"http://[::1]:8080":      "http://[::1]:8080",
	}
	for raw, want := range accepted {
		if got, err := CanonicalOrigin(raw); err != nil || got != want {
			t.Errorf("CanonicalOrigin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	for _, raw := range []string{
		"http://remote.example",
		"http://192.168.1.10:8080",
		"http://[2001:db8::1]",
	} {
		if got, err := CanonicalOrigin(raw); err == nil {
			t.Errorf("CanonicalOrigin(%q) = %q; want refusal", raw, got)
		}
	}
}
