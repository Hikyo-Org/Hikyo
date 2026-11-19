package clientauth

import (
	"net/url"
	"testing"
)

func TestCallbackQueryIsClosedAndExact(t *testing.T) {
	state := "hik_1_hs_opaque"
	if !exactCallbackQuery(url.Values{"code": {"one"}, "state": {state}}, state) {
		t.Fatal("exact code and state were refused")
	}
	for _, invalid := range []url.Values{
		{"code": {"one"}, "state": {"wrong"}},
		{"code": {"one"}, "state": {state}, "next": {"evil"}},
		{"code": {"one", "two"}, "state": {state}},
		{"code": {""}, "state": {state}},
	} {
		if exactCallbackQuery(invalid, state) {
			t.Fatalf("invalid callback accepted: %v", invalid)
		}
	}
}
