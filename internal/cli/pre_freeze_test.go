package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

// The Go client uses the same generated models as the server. Exercise its
// real response decoder as well: a forward-compatible model alone is not enough.
func TestClientPreFreezeOpenEnums(t *testing.T) {
	checkClientOpenEnum(t, apigen.IdentityProviderKind("future-kind"), `"future-kind"`)
	checkClientOpenEnum(t, apigen.OidcStartRequest{Purpose: "future-purpose"}, `{"purpose":"future-purpose"}`)
	checkClientOpenEnum(t, apigen.SamlStartRequest{Purpose: "future-purpose"}, `{"purpose":"future-purpose"}`)
	checkClientOpenEnum(t, apigen.GrantOrigin{Kind: "future-origin", Subject: "holder"}, `{"kind":"future-origin","subject":"holder"}`)
}

func checkClientOpenEnum[T comparable](t *testing.T, want T, wire string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, wire)
	}))
	defer srv.Close()
	client := &cli.Client{Entry: cli.TrustEntry{Origin: srv.URL}, HTTP: srv.Client()}
	var got T
	if err := client.Do(t.Context(), http.MethodGet, "/fixture", nil, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unknown value lost: got %+v, want %+v", got, want)
	}
}
