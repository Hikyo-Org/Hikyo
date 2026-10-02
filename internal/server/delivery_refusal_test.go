package server_test

import (
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestOnlyMatchedDeliveryRefusalCarriesAuthority(t *testing.T) {
	srv := federationServer(t, nil, stubDelivery{err: domain.ErrNotFound})
	matched, matchedBody := call(t, srv, http.MethodGet, deliveryPath, "hik_1_wl_abc", nil)
	unmatched, unmatchedBody := call(t, srv, http.MethodGet, "/api/v1/unknown-delivery-route", "hik_1_wl_abc", nil)
	if matched.StatusCode != http.StatusNotFound || unmatched.StatusCode != http.StatusNotFound || string(matchedBody) != string(unmatchedBody) {
		t.Fatalf("404 bodies differ: matched %d %q, unmatched %d %q", matched.StatusCode, matchedBody, unmatched.StatusCode, unmatchedBody)
	}
	if matched.Header.Get(delivery.RefusalHeader) != delivery.RefusalVersion || unmatched.Header.Get(delivery.RefusalHeader) != "" {
		t.Fatalf("markers: matched %q, unmatched %q", matched.Header.Get(delivery.RefusalHeader), unmatched.Header.Get(delivery.RefusalHeader))
	}
	srv = federationServer(t, nil, stubDelivery{err: domain.ErrUnauthenticated})
	unauth, _ := call(t, srv, http.MethodGet, deliveryPath, "not-live", nil)
	if unauth.StatusCode != http.StatusUnauthorized || unauth.Header.Get(delivery.RefusalHeader) != "" {
		t.Fatalf("unauthenticated response %d carries marker %q", unauth.StatusCode, unauth.Header.Get(delivery.RefusalHeader))
	}
}
