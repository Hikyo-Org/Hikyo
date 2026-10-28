// networkpolicyfixture is a synthetic HTTP destination for the chart's isolated
// CNI exercise. It holds no credentials and never calls an external provider.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	mux := http.NewServeMux()
	provider := &gitlabFixture{values: make(map[string]variable)}
	cert, ca := certificate("adapter." + os.Getenv("POD_NAMESPACE") + ".svc")
	mux.HandleFunc("/fixture/ca", func(w http.ResponseWriter, _ *http.Request) { w.Write(ca) })
	mux.Handle("/api/v4/", provider)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s\n", r.Method, r.URL.Path)
	})
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		tlsServer := &http.Server{Addr: ":8443", Handler: mux, ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}}
		log.Fatal(tlsServer.ListenAndServeTLS("", ""))
	}()
	log.Fatal(server.ListenAndServe())
}

// A stateful subset of the GitLab wire protocol. Readback belongs to the test
// harness; the production adapter's interface still cannot read variables.
type variable struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Scope     string `json:"environment_scope"`
	Protected bool   `json:"protected"`
	Masked    bool   `json:"masked"`
	Raw       bool   `json:"raw"`
}
type gitlabFixture struct {
	mu     sync.Mutex
	values map[string]variable
}

func (g *gitlabFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("PRIVATE-TOKEN") != "synthetic-network-policy" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/v4")
	switch path {
	case "/version":
		json.NewEncoder(w).Encode(map[string]string{"version": "18.0.0"})
		return
	case "/personal_access_tokens/self":
		json.NewEncoder(w).Encode(map[string]any{"scopes": []string{"api"}, "active": true, "revoked": false})
		return
	case "/user":
		json.NewEncoder(w).Encode(map[string]bool{"bot": true})
		return
	case "/projects/fixture/project", "/projects/42":
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "path_with_namespace": "fixture/project"})
		return
	}
	const collection = "/projects/42/variables"
	if path != collection && !strings.HasPrefix(path, collection+"/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path, collection+"/")
	scope := r.URL.Query().Get("filter[environment_scope]")
	if scope == "" {
		scope = "*"
	}
	key := scope + ":" + name
	if r.Method == http.MethodGet {
		value, ok := g.values[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(value)
		return
	}
	if r.Method == http.MethodDelete {
		if _, ok := g.values[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(g.values, key)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var v variable
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&v); err != nil {
		http.Error(w, "invalid variable", http.StatusBadRequest)
		return
	}
	if v.Scope == "" {
		v.Scope = "*"
	}
	if r.Method == http.MethodPost {
		key = v.Scope + ":" + v.Key
		if _, ok := g.values[key]; ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		g.values[key] = v
		w.WriteHeader(http.StatusCreated)
	} else if r.Method == http.MethodPut {
		if _, ok := g.values[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		v.Key = name
		g.values[key] = v
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func certificate(host string) (tls.Certificate, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic network policy fixture"}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
