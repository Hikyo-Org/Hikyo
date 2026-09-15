package storagehealth

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSummary(at time.Time, capacity, available string) string {
	return fmt.Sprintf(`{"node":{"nodeName":"database-node"},"pods":[{"podRef":{"namespace":"database"},"volume":[{"time":%q,"capacityBytes":%s,"availableBytes":%s,"pvcRef":{"name":"pg-data","namespace":"database"}}]}]}`, at.Format(time.RFC3339Nano), capacity, available)
}

func testOptions() KubernetesOptions {
	return KubernetesOptions{KubeletURL: "https://kubelet.example:10250", Namespace: "database", PVC: "pg-data", Node: "database-node"}
}

func TestCapacityFromKubeletSummary(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	good := testSummary(now, "100", "70")
	for _, tt := range []struct {
		name, body string
		known      bool
		available  uint64
	}{
		{"fresh", good, true, 70},
		{"full volume", testSummary(now, "100", "0"), true, 0},
		{"boundary age", testSummary(now.Add(-maxVolumeAge), "100", "70"), true, 70},
		{"clock skew", testSummary(now.Add(maxFutureSkew), "100", "70"), true, 70},
		{"stale", testSummary(now.Add(-maxVolumeAge-time.Nanosecond), "100", "70"), false, 0},
		{"future", testSummary(now.Add(maxFutureSkew+time.Nanosecond), "100", "70"), false, 0},
		{"no timestamp", testSummary(time.Time{}, "100", "70"), false, 0},
		{"no capacity", testSummary(now, "null", "70"), false, 0},
		{"no available", testSummary(now, "100", "null"), false, 0},
		{"zero capacity", testSummary(now, "0", "0"), false, 0},
		{"excess available", testSummary(now, "100", "101"), false, 0},
		{"negative", testSummary(now, "100", "-1"), false, 0},
		{"overflow", testSummary(now, "18446744073709551616", "1"), false, 0},
		{"wrong node", strings.ReplaceAll(good, "database-node", "other-node"), false, 0},
		{"wrong namespace", strings.ReplaceAll(good, `"namespace":"database"`, `"namespace":"other"`), false, 0},
		{"wrong PVC namespace", strings.Replace(good, `"name":"pg-data","namespace":"database"`, `"name":"pg-data","namespace":"other"`, 1), false, 0},
		{"wrong PVC", strings.ReplaceAll(good, "pg-data", "other"), false, 0},
		{"malformed", `{`, false, 0},
		{"trailing document", good + `{}`, false, 0},
		{"empty", `{}`, false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := capacityFromSummary([]byte(tt.body), testOptions(), now)
			if (err == nil) != tt.known {
				t.Fatalf("capacity=%+v err=%v, want known=%v", capacity, err, tt.known)
			}
			if tt.known && capacity != (Capacity{TotalBytes: 100, AvailableBytes: tt.available}) {
				t.Fatalf("capacity=%+v", capacity)
			}
		})
	}
}

func testKubelet(t *testing.T, handler http.Handler) (*Kubernetes, *httptest.Server) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	dir := t.TempDir()
	caFile, tokenFile := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "token")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("test-projected-token"), 0600); err != nil {
		t.Fatal(err)
	}
	options := testOptions()
	options.KubeletURL = server.URL
	reader, err := newKubernetes(options, caFile, tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseIdleConnections)
	return reader, server
}

func TestKubernetesReadAuthenticatedTLSAndFreshSamples(t *testing.T) {
	calls := 0
	reader, _ := testKubelet(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/stats/summary" || r.Header.Get("Authorization") != "Bearer test-projected-token" {
			t.Errorf("unexpected request %s, auth present=%v", r.URL.Path, r.Header.Get("Authorization") != "")
		}
		if calls > 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, testSummary(time.Now(), "100", "70"))
	}))
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	capacity, err := reader.Read()
	if err != nil || capacity.AvailableBytes != 70 {
		t.Fatalf("capacity=%+v err=%v", capacity, err)
	}
	if _, err := reader.Read(); err == nil {
		t.Fatal("must not reuse last successful sample")
	}
}

func TestKubernetesReadRefusesRedirectStatusAndOversize(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	for _, tt := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }},
		{"forbidden", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) }},
		{"oversize", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat(" ", maxSummaryBytes+1)) }},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{`) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader, _ := testKubelet(t, tt.handler)
			if _, err := reader.Read(); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
	if targetCalls != 0 {
		t.Fatal("redirect target received request")
	}
}

func TestKubernetesReadTimeoutAndTLSVerification(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		reader, _ := testKubelet(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		reader.client.Timeout = 20 * time.Millisecond
		if _, err := reader.Read(); err == nil {
			t.Fatal("expected timeout")
		}
	})
	t.Run("wrong certificate", func(t *testing.T) {
		reader, server := testKubelet(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, testSummary(time.Now(), "100", "70")) }))
		reader.options.KubeletURL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
		if _, err := reader.Read(); err == nil || !strings.Contains(err.Error(), "certificate") {
			t.Fatalf("expected TLS verification failure, got %v", err)
		}
	})
}

func TestNewKubernetesRequiresCredentialsAndOrigin(t *testing.T) {
	for _, origin := range []string{"http://host:10250", "https://host/", "https://host/stats/summary", "https://u:p@host", "https://host?x=y", "https://host#fragment", "https://HOST", "https://host."} {
		options := testOptions()
		options.KubeletURL = origin
		if _, err := newKubernetes(options, "missing", "missing"); err == nil || !strings.Contains(err.Error(), "origin") {
			t.Errorf("origin %q: %v", origin, err)
		}
	}
	if _, err := newKubernetes(testOptions(), filepath.Join(t.TempDir(), "missing"), "missing"); err == nil {
		t.Fatal("missing credentials must refuse startup")
	}
}

func TestNewKubernetesRejectsMalformedMapping(t *testing.T) {
	for _, edit := range []func(*KubernetesOptions){
		func(o *KubernetesOptions) { o.KubeletURL = "https://host:70000" },
		func(o *KubernetesOptions) { o.KubeletURL = "https://host:0" },
		func(o *KubernetesOptions) { o.KubeletURL = "https://host:" },
		func(o *KubernetesOptions) { o.KubeletURL = "https://bad_host" },
		func(o *KubernetesOptions) { o.Namespace = "a.b" },
		func(o *KubernetesOptions) { o.PVC = "invalid_name" },
		func(o *KubernetesOptions) { o.Node = "to kyo" },
	} {
		options := testOptions()
		edit(&options)
		if _, err := newKubernetes(options, "missing", "missing"); err == nil || strings.Contains(err.Error(), "credentials") {
			t.Fatalf("mapping must fail before credentials: %+v: %v", options, err)
		}
	}
}

func TestCapacityFromKubeletSummaryMultiplePods(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name      string
		available uint64
		namespace string
		known     bool
	}{
		{"same PVC same capacity", 70, "database", true},
		{"same PVC disagreement", 60, "database", false},
		{"unrelated pod", 60, "unrelated", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var summary kubeletSummary
			if err := json.Unmarshal([]byte(testSummary(now, "100", "70")), &summary); err != nil {
				t.Fatal(err)
			}
			other := summary.Pods[0]
			other.Volumes = append(other.Volumes[:0:0], other.Volumes...)
			other.PodRef.Namespace = tt.namespace
			other.Volumes[0].AvailableBytes = &tt.available
			summary.Pods = append(summary.Pods, other)
			body, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			result, err := capacityFromSummary(body, testOptions(), now)
			if (err == nil) != tt.known {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestNewKubernetesRefusesInvalidCAAndToken(t *testing.T) {
	_, server := testKubelet(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, tt := range []struct{ name, ca, token string }{
		{"empty CA", "", "token"},
		{"invalid CA", "not-a-certificate", "token"},
		{"empty token", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			caFile, tokenFile := filepath.Join(dir, "ca"), filepath.Join(dir, "token")
			if err := os.WriteFile(caFile, []byte(tt.ca), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tokenFile, []byte(tt.token), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := newKubernetes(testOptions(), caFile, tokenFile); err == nil {
				t.Fatal("invalid local credentials accepted")
			}
		})
	}
}
