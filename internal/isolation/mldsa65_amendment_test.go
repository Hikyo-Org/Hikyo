package isolation

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// The api-cli-surface amendment of 2026-11-04 lets the /api/v1 contract name
// mldsa65 (Transit algorithm, PKI key algorithms). It enables nothing: the
// Transit mechanism is not in this tree and PKI stays behind
// service.PKI.MLDSAEnabled, which production wiring leaves false. Because the
// contract now lets the value past request validation, the service is the line
// that holds, so each create must refuse it as a validation error (never a 500)
// and leave no row behind. TestMLDSA65OverHTTP covers the contract and the other
// paths.
func TestMLDSA65IsRefusedByTheService(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx, actor := tctx(t), service.LocalPrincipal(alice)
		rows := func(table string) int64 { return queryInt(t, db, `SELECT COUNT(*) FROM `+table) }

		// shortcut: the Transit mechanism lands with #1172; this case flips to a
		// positive lifecycle there.
		tsvc, _ := transitEnv(t, db, "mldsa")
		_, err := tsvc.CreateKey(ctx, actor, transitScope, service.CreateTransitKeyRequest{Name: "pq", Algorithm: "mldsa65", AllowedOperations: []string{"sign", "verify"}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("transit create with mldsa65: %v, want a validation error", err)
		}
		if n := rows("transit_keys") + rows("transit_key_versions"); n != 0 {
			t.Errorf("a refused transit key left %d rows", n)
		}

		pkisvc := pkiSvc(t, db, &pkiClock{now: time.Now().UTC().Truncate(time.Second)}) // production wiring: MLDSAEnabled false
		op := service.LocalPrincipal(root)
		pkiGrant(t, db, "g_mldsa65_alice_issue", alice, domain.CapIssueCertificate, envA1)
		refusals := map[string]func() error{
			"issuer": func() error {
				_, err := pkisvc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "pq-root", CommonName: "x", KeyAlgorithm: "mldsa65"})
				return err
			},
			"profile": func() error {
				_, err := pkisvc.CreateProfile(ctx, op, "pq-profile", pki.Policy{
					AllowedIssuers: []string{"whatever"}, DNSPatterns: []string{"*.svc.example.com"},
					KeyAlgorithms: []pki.KeyAlgorithm{pki.MLDSA65}, KeyUsages: []pki.KeyUsage{pki.UsageDigitalSignature},
					ExtKeyUsages: []pki.ExtKeyUsage{pki.ExtUsageServerAuth}, MaxTTL: 72 * time.Hour, DefaultTTL: 24 * time.Hour, RenewWindow: 8 * time.Hour,
					AllowCSR: true, AllowGeneratedKey: true,
				})
				return err
			},
			"generated leaf key": func() error {
				_, err := pkisvc.IssueCertificate(ctx, service.LocalPrincipal(alice), transitScope, service.CertificateIssueRequest{Profile: "none", GenerateKey: true, KeyAlgorithm: "mldsa65", DNSNames: []string{"pq.svc.example.com"}})
				return err
			},
		}
		for name, run := range refusals {
			if err := run(); !errors.Is(err, service.ErrPKIAlgorithmHeld) || !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("pki %s with mldsa65: %v, want ErrPKIAlgorithmHeld wrapping a validation error", name, err)
			}
		}
		if n := rows("pki_issuers") + rows("pki_profiles") + rows("pki_certificates"); n != 0 {
			t.Errorf("a refused pki request left %d rows", n)
		}
	})
}

// The same line over the real server: the contract lets each request through,
// and a request naming mldsa65 never succeeds with a classical key, which would
// read as the algorithm the caller asked for.
func TestMLDSA65OverHTTP(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{username: "factor-admin", displayName: "A", password: "correct horse battery staple mldsa65"})
		operator := service.LocalPrincipal(root)
		keyring := probeKeyring(t, db)
		org, err := (&service.Orgs{DB: db}).Create(t.Context(), operator, "amendment-org", true, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		project, err := (&service.Projects{DB: db}).Create(t.Context(), operator, domain.OrgID(org.ID), "amendment-project")
		if err != nil {
			t.Fatal(err)
		}
		env, err := (&service.Environments{DB: db, Keyring: keyring}).Create(t.Context(), operator, domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}, "amendment-env", nil)
		if err != nil {
			t.Fatal(err)
		}
		principal := string(admin.boot.PrincipalID)
		for i, grant := range [][2]string{{"read", "'" + env.ID + "'"}, {"crypto-manage", "NULL"}, {string(domain.CapIssueCertificate), "'" + env.ID + "'"}} {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_amendment_%d','%s','%s','%s','%s',%s,%s)`, i, principal, grant[0], org.ID, project.ID, grant[1], ts))
		}
		clk := time.Now().UTC()
		admin.auth.Now = func() time.Time { return clk }
		initial, err := admin.auth.LocalLogin(t.Context(), "factor-admin", admin.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := admin.auth.EnrolTOTPStart(t.Context(), initial.SessionToken, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		confirmed, err := admin.auth.EnrolTOTPConfirm(t.Context(), initial.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		clk = clk.Add(30 * time.Second)
		elevated, err := admin.auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		// The clock stops changing before HTTP workers start.
		srv := httptest.NewServer(server.New(&server.API{
			Auth: admin.auth, Orgs: &service.Orgs{DB: db}, Version: "mldsa65-amendment",
			Transit: &service.Transit{DB: db, Keyring: keyring, Custody: transit.NewRegistry(&transit.Software{Keyring: keyring})},
			PKI:     pkiSvc(t, db, &pkiClock{now: time.Now().UTC().Truncate(time.Second)}), // production wiring: MLDSAEnabled false
		}, nil))
		defer srv.Close()
		admin.auth.ExternalOrigin = srv.URL

		post := func(path string, body map[string]any, want int) string {
			t.Helper()
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+elevated.SessionToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", srv.URL)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			out, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != want {
				t.Fatalf("POST %s = %d, want %d: %.300s", path, res.StatusCode, want, out)
			}
			return string(out)
		}
		classicalCA := func(prefix string) map[string]any {
			t.Helper()
			now := time.Now().UTC()
			rootKey, err := pki.GenerateKey(pki.ECDSAP256)
			if err != nil {
				t.Fatal(err)
			}
			rootDER, err := pki.CreateRoot(rootKey, pki.Subject{CommonName: prefix + " Root"}, now, 30*24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			rootCert, err := x509.ParseCertificate(rootDER)
			if err != nil {
				t.Fatal(err)
			}
			key, err := pki.GenerateKey(pki.ECDSAP256)
			if err != nil {
				t.Fatal(err)
			}
			der, err := pki.SignIntermediate(pki.Parent{Certificate: rootCert, Signer: rootKey}, key.Public(), pki.Subject{CommonName: prefix + " Issuing"}, now, 10*24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			pkcs8, err := pki.MarshalPrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			return map[string]any{"private_key_pem": string(pki.PrivateKeyPEM(pkcs8)), "certificate_pem": string(pki.CertificatePEM(der)), "chain_pem": string(pki.CertificatePEM(rootDER))}
		}
		with := func(base map[string]any, extra map[string]any) map[string]any {
			out := map[string]any{}
			maps.Copy(out, base)
			maps.Copy(out, extra)
			return out
		}
		const instance = "/api/v1/instance/pki"
		envPath := "/api/v1/orgs/" + org.ID + "/projects/" + project.ID + "/environments/" + env.ID
		rows := func(table string) int64 { return queryInt(t, db, `SELECT COUNT(*) FROM `+table) }

		post(envPath+"/transit-keys", map[string]any{"name": "pq", "algorithm": "mldsa65", "allowed_operations": []string{"sign", "verify"}}, http.StatusBadRequest)
		if n := rows("transit_keys"); n != 0 {
			t.Errorf("a refused transit key left %d rows", n)
		}
		// The contract itself still refuses any other value.
		post(envPath+"/transit-keys", map[string]any{"name": "pq", "algorithm": "mldsa44"}, http.StatusBadRequest)

		post(instance+"/issuers", map[string]any{"mode": "root", "name": "web-root", "common_name": "Web Root"}, http.StatusCreated)
		post(instance+"/issuers", map[string]any{"mode": "root", "name": "pq-root", "common_name": "x", "key_algorithm": "mldsa65"}, http.StatusBadRequest)
		post(instance+"/issuers/web-root/rotate", map[string]any{"key_algorithm": "mldsa65"}, http.StatusBadRequest)

		// Each named case has a control: the same request without the name works.
		ca := classicalCA("named")
		post(instance+"/issuers", with(ca, map[string]any{"mode": "import", "name": "named", "key_algorithm": "mldsa65"}), http.StatusBadRequest)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pki_issuers WHERE name='named'`); n != 0 {
			t.Errorf("import naming mldsa65 with a classical key stored %d issuers", n)
		}
		post(instance+"/issuers", with(ca, map[string]any{"mode": "import", "name": "named"}), http.StatusCreated)

		next := classicalCA("next")
		post(instance+"/issuers/named/rotate", with(next, map[string]any{"key_algorithm": "mldsa65"}), http.StatusBadRequest)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pki_issuers WHERE name='named'`); n != 1 {
			t.Errorf("rotation naming mldsa65 with a classical key left %d versions", n)
		}
		post(instance+"/issuers/named/rotate", next, http.StatusOK)

		policy := map[string]any{
			"allowed_issuers": []string{"web-root"}, "dns_patterns": []string{"*.svc.example.com"}, "ip_ranges": []string{}, "uri_patterns": []string{}, "allow_wildcard_names": false,
			"key_algorithms": []string{"ecdsa-p256", "ed25519"}, "key_usages": []string{"digital-signature"}, "ext_key_usages": []string{"server-auth"},
			"max_ttl_seconds": 259200, "default_ttl_seconds": 86400, "renew_window_seconds": 28800, "allow_csr": true, "allow_generated_key": true, "machine_issuance": false, "organization": "",
		}
		post(instance+"/profiles", map[string]any{"name": "web", "policy": policy}, http.StatusCreated)
		post(instance+"/profiles/web/bindings", map[string]any{"org_id": org.ID, "project_id": project.ID}, http.StatusOK)
		pqPolicy := with(policy, map[string]any{"key_algorithms": []string{"mldsa65"}})
		post(instance+"/profiles", map[string]any{"name": "pq-web", "policy": pqPolicy}, http.StatusBadRequest)

		csr, _ := pkiCSR(t)
		issue := map[string]any{"profile": "web", "csr_pem": csr, "dns_names": []string{"a.svc.example.com"}}
		post(envPath+"/certificates", with(issue, map[string]any{"key_algorithm": "mldsa65"}), http.StatusBadRequest)
		if n := rows("pki_certificates"); n != 0 {
			t.Errorf("issue naming mldsa65 with a classical CSR left %d certificates", n)
		}
		post(envPath+"/certificates", issue, http.StatusOK)
		post(envPath+"/certificates", map[string]any{"profile": "web", "generate_key": true, "key_algorithm": "mldsa65", "dns_names": []string{"b.svc.example.com"}}, http.StatusBadRequest)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM pki_issuers WHERE key_algorithm='mldsa65'`) + queryInt(t, db, `SELECT COUNT(*) FROM pki_certificates WHERE key_algorithm='mldsa65'`) + queryInt(t, db, `SELECT COUNT(*) FROM pki_profiles WHERE name='pq-web'`); n != 0 {
			t.Errorf("the real server stored %d ML-DSA rows", n)
		}
	})
}
