package isolation

import (
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/jwkssource"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestFederationCABundleRoundTrip(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newFedRig(t, db)
		bundle := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: r.idp.Server.Certificate().Raw}))
		actor := service.LocalPrincipal(root)
		refused := []string{"https://kubernetes.default.svc"}
		req := service.IssuerRequest{Issuer: r.idp.Issuer(), Type: domain.IssuerKubernetes, KeySource: jwkssource.RemoteDiscovery(), RefusedAudiences: refused, CABundlePEM: bundle}
		iss, err := r.fed.CreateIssuer(t.Context(), actor, req)
		if err != nil {
			t.Fatal(err)
		}
		assertBundle := func(want string) {
			t.Helper()
			rows, err := r.fed.ListIssuers(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.ID == iss.ID {
					if row.CABundlePEM != want {
						t.Fatal("stored CA bundle differs")
					}
					return
				}
			}
			t.Fatal("issuer missing")
		}
		assertBundle(bundle)
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, jwkssource.RemoteDiscovery(), refused, nil); err != nil {
			t.Fatal(err)
		}
		assertBundle(bundle)
		malformed := "not a certificate"
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, jwkssource.RemoteDiscovery(), refused, &malformed); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("malformed update: %v", err)
		}
		assertBundle(bundle)
		empty := ""
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, jwkssource.RemoteDiscovery(), refused, &empty); err != nil {
			t.Fatal(err)
		}
		assertBundle("")
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, jwkssource.RemoteDiscovery(), refused, &bundle); err != nil {
			t.Fatal(err)
		}
		document, err := r.idp.JWKSDocument()
		if err != nil {
			t.Fatal(err)
		}
		source := staticKeySource(t, document)
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, source, refused, &bundle); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("static plus CA: %v", err)
		}
		assertBundle(bundle)
		if _, err := r.fed.UpdateIssuer(t.Context(), actor, iss.ID, source, refused, nil); err != nil {
			t.Fatal(err)
		}
		assertBundle("")
		req.Issuer = "https://invalid.example.test"
		req.KeySource = source
		if _, err := r.fed.CreateIssuer(t.Context(), actor, req); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("static create plus CA: %v", err)
		}
	})
}

func TestFederationCABundleCannotChangeMidFlight(t *testing.T) {
	db := seededDB(t, openSQLite)
	r := newFedRig(t, db)
	shape := oidctest.KubernetesShape("prod", "ca-racer", "uid-ca", "https://kubernetes.default.svc")
	iss := r.configureIssuer(t, domain.IssuerKubernetes, []string{shape.DefaultAudience})
	r.bindShape(t, "wl-ca-race", shape, hikyoAudience)
	token, err := r.idp.MintShape(shape, hikyoAudience, r.clk.Now(), 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.fed.OnValidated = func() {
		bundle := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: r.idp.Server.Certificate().Raw}))
		if _, err := r.fed.UpdateIssuer(t.Context(), service.LocalPrincipal(root), iss.ID, jwkssource.RemoteDiscovery(), []string{shape.DefaultAudience}, &bundle); err != nil {
			t.Fatal(err)
		}
		r.fed.OnValidated = nil
	}
	if _, err := r.del.Fetch(t.Context(), token, scopeEnv(orgA, prjA1, envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("CA changed mid-flight: %v", err)
	}
}
