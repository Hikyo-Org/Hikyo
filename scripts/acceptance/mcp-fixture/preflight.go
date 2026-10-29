//go:build darwin || linux

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func validateDeploymentCustody(dir string, b deploymentBinding) error {
	custody := filepath.Join(dir, "custody")
	if err := privatePath(custody, true); err != nil {
		return err
	}
	for _, name := range []string{"root-key", "password", "dsn", "backend.crt", "backend.key", "backend-ca.crt", "postgres.crt", "postgres.key", "postgres-ca.crt", "public.crt", "public.key", "settings.json"} {
		if err := privatePath(filepath.Join(custody, name), false); err != nil {
			return err
		}
	}
	root, err := crypto.ReadRootKey(filepath.Join(custody, "root-key"), "")
	if err != nil {
		return errors.New("fixture root-key format refused")
	}
	crypto.Zero(root)
	var cfg settings
	if err := readPrivate(filepath.Join(custody, "settings.json"), &cfg); err != nil {
		return err
	}
	dsn, err := os.ReadFile(filepath.Join(custody, "dsn"))
	if err != nil {
		return errors.New("fixture DSN custody unavailable")
	}
	if cfg.Development || cfg.DSN != strings.TrimSpace(string(dsn)) || validateDSN(cfg.DSN, false) != nil || cfg.Root != "/run/hikyo-root-key/root-key" || cfg.State != "/installation/operator-state" || cfg.Bundle != "/public/bundle" || cfg.Operator != "/public/operator.pub" {
		return errors.New("fixture custody descriptor differs from reviewed deployment")
	}
	u, _ := url.Parse(cfg.DSN)
	password, _ := u.User.Password()
	protected, err := os.ReadFile(filepath.Join(custody, "password"))
	if err != nil || password == "" || password != strings.TrimSpace(string(protected)) {
		return errors.New("dedicated database credential custody differs")
	}
	origin, _ := url.Parse(b.Origin)
	for _, pair := range []struct{ name, ca, host string }{{"postgres", "postgres-ca.crt", "db.hikyo-mcp-651.svc.cluster.local"}, {"backend", "backend-ca.crt", origin.Hostname()}, {"public", "", origin.Hostname()}} {
		certificate, err := tls.LoadX509KeyPair(filepath.Join(custody, pair.name+".crt"), filepath.Join(custody, pair.name+".key"))
		if err != nil || len(certificate.Certificate) == 0 {
			return errors.New("fixture TLS keypair invalid")
		}
		leaf, err := x509.ParseCertificate(certificate.Certificate[0])
		if err != nil || leaf.NotAfter.Before(time.Now().Add(4*time.Hour)) {
			return errors.New("fixture TLS certificate validity insufficient")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return errors.New("system certificate roots unavailable")
		}
		if pair.ca != "" {
			raw, err := os.ReadFile(filepath.Join(custody, pair.ca))
			if err != nil {
				return errors.New("fixture CA unavailable")
			}
			roots = x509.NewCertPool()
			if !roots.AppendCertsFromPEM(raw) {
				return errors.New("fixture CA invalid")
			}
		}
		intermediates := x509.NewCertPool()
		for _, der := range certificate.Certificate[1:] {
			c, err := x509.ParseCertificate(der)
			if err != nil {
				return errors.New("fixture intermediate invalid")
			}
			intermediates.AddCert(c)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: pair.host, Roots: roots, Intermediates: intermediates}); err != nil {
			return errors.New("fixture TLS hostname/chain verification failed")
		}
	}
	return nil
}

type ingressProof struct {
	Origin      string   `json:"origin"`
	Source      string   `json:"source_commit"`
	Image       string   `json:"image"`
	PodUIDs     []string `json:"pod_uids"`
	EdgeConfig  string   `json:"edge_configuration_sha256"`
	TLSVerified bool     `json:"public_TLS_and_upstream_TLS_verified"`
	NoRedirects bool     `json:"no_redirects"`
	CNIEnforced bool     `json:"effective_CNI_positive_negative_controls_passed"`
	Reference   string   `json:"operator_receipt_ref"`
}

func (p *probe) publicPreflight(ctx context.Context, dir string, a executionAuthorization, b deploymentBinding, pods []observedPod) error {
	path := filepath.Join(dir, "ingress-verification.json")
	if !sha256Pattern.MatchString(a.IngressSHA256) || fileHash(path) != a.IngressSHA256 {
		return errors.New("HOLD: reviewed ingress/CNI verification receipt required before bearer calls")
	}
	var proof ingressProof
	if err := readPrivate(path, &proof); err != nil {
		return err
	}
	if proof.Origin != b.Origin || proof.Source != b.SourceCommit || proof.Image != b.Image || len(proof.PodUIDs) != 2 || proof.PodUIDs[0] != pods[0].Metadata.UID || proof.PodUIDs[1] != pods[1].Metadata.UID || !sha256Pattern.MatchString(proof.EdgeConfig) || !proof.TLSVerified || !proof.NoRedirects || !proof.CNIEnforced || proof.Reference == "" {
		return errors.New("HOLD: ingress proof does not bind this exact fixture")
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.Origin+"/api/v1/meta", nil)
		if err != nil {
			return errors.New("metadata request failed")
		}
		// No bearer has been placed on any request before this control.
		h, err := p.client.Do(req)
		if err != nil {
			return errors.New("public metadata TLS failed before bearer")
		}
		body, readErr := io.ReadAll(io.LimitReader(h.Body, (64<<10)+1))
		h.Body.Close()
		var meta struct {
			Version  string `json:"server_version"`
			Revision int    `json:"api_revision"`
		}
		identity := h.Header.Get("X-Fixture-Process")
		if readErr != nil || len(body) > 64<<10 || h.StatusCode != 200 || !strings.HasPrefix(h.Header.Get("Content-Type"), "application/json") || json.Unmarshal(body, &meta) != nil || meta.Version != b.Version || meta.Revision <= 0 || !contains(p.target.Processes, identity) {
			return errors.New("public metadata/revision/route mismatch before bearer")
		}
		seen[identity] = true
	}
	if len(seen) != 2 {
		return errors.New("anonymous public route did not reach both observed processes")
	}
	p.receipt.Checks["public_TLS_metadata_revision_ingress_CNI_before_bearer"] = "PASS; separate operator ingress/CNI receipt retained"
	return nil
}
