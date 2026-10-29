//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

func TestCanonicalRootStageIsAcceptedWithoutRelaxingJSONCustody(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "root")
	if err := os.WriteFile(source, []byte(strings.Repeat("07", 32)), 0440); err != nil {
		t.Fatal(err)
	}
	if err := securefile.StageRootKey(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := privatePathModes(destination, false, 0400, 0600); err != nil {
		t.Fatal(err)
	}
	var cfg settings
	if readPrivate(destination, &cfg) == nil {
		t.Fatal("root staging mode broadened JSON input custody")
	}
}

func TestProjectedPublicFilesBecomeRegularProductionReadableFiles(t *testing.T) {
	dir := t.TempDir()
	projected, staged := filepath.Join(dir, "projected"), filepath.Join(dir, "staged")
	if err := os.MkdirAll(filepath.Join(projected, "revision", "bundle"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("revision", filepath.Join(projected, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..data/bundle", filepath.Join(projected, "bundle")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projected, "revision", "bundle", "index.json")
	want := []byte(`{"fixture":"public staging consumer only"}`)
	if err := os.WriteFile(path, want, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..data/operator.pub", filepath.Join(projected, "operator.pub")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projected, "revision", "operator.pub"), want, 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := backupreceipt.ReadPublicArtifact(filepath.Join(projected, "operator.pub"), 1024); err == nil {
		t.Fatal("positive control did not reject projected symlink")
	}
	args := []string{"stage-public.sh", projected, staged, "bundle/index.json:" + fileHash(path), "operator.pub:" + fileHash(path)}
	if output, err := exec.Command("sh", args...).CombinedOutput(); err != nil {
		t.Fatalf("staging: %v: %s", err, output)
	}
	for _, relative := range []string{"bundle/index.json", "operator.pub"} {
		got, err := backupreceipt.ReadPublicArtifact(filepath.Join(staged, relative), 1024)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("staged file rejected by production no-follow reader", err)
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed projected content"), 0600); err != nil {
		t.Fatal(err)
	}
	if exec.Command("sh", args...).Run() == nil {
		t.Fatal("changed projection passed selected hash")
	}
}

func TestAmbiguousGrantWriteAlwaysAttemptsRecovery(t *testing.T) {
	ackLost, recoveryFailed := errors.New("writer reply lost after possible commit"), errors.New("recovery failed")
	for _, recoveryErr := range []error{nil, recoveryFailed} {
		called := false
		err := withGrantRecovery(func() error { return ackLost }, func() error { called = true; return recoveryErr })
		if !called || !errors.Is(err, ackLost) || recoveryErr != nil && !errors.Is(err, recoveryErr) {
			t.Fatal("ambiguous commit skipped compensation or hid failure")
		}
	}
}

func TestWaitCannotPassAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if wait(ctx, time.Second, func(context.Context) bool { called = true; return true }) == nil || called {
		t.Fatal("expired wait invoked readiness")
	}
	if wait(context.Background(), 10*time.Millisecond, func(ctx context.Context) bool { <-ctx.Done(); return true }) == nil {
		t.Fatal("late positive observation passed")
	}
}

func TestCodexPublicFixtureBypassesDeadProxyOnlyForItsHost(t *testing.T) {
	value := codexNoProxy("https://mcp-651.example/mcp/codex")
	if value != "NO_PROXY=localhost,127.0.0.1,mcp-651.example" || strings.Contains(value, "*") {
		t.Fatal("public fixture would use dead proxy or unrestricted bypass", value)
	}
}

func TestWireReceiptCannotPersistCallerControlledSecrets(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory failed")
	}
	wire := &wireRecorder{directory: dir}
	h := wire.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	secret := "SYNTHETIC-CANARY-MUST-NOT-ENTER-WIRE-RECEIPT"
	req := httptest.NewRequest(http.MethodPost, "https://fixture.example/mcp/codex", strings.NewReader(`{"method":"`+secret+`","params":{"protocolVersion":"`+secret+`"}}`))
	req.Header.Set("Mcp-Protocol-Version", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(httptest.NewRecorder(), req)
	raw, err := os.ReadFile(filepath.Join(dir, "wire-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("Authorization")) || !bytes.Contains(raw, []byte(`"method":"other"`)) {
		t.Fatal("wire-only receipt retained caller material")
	}
}

func TestPublicPreflightFailsBeforeBearerOnEmptyOrWrongMetadata(t *testing.T) {
	for _, body := range []string{"", `{"server_version":"different","api_revision":12}`} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			var calls atomic.Int32
			var bearer atomic.Bool
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "" {
					bearer.Store(true)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Fixture-Process", "a")
				io.WriteString(w, body)
			}))
			defer s.Close()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if os.Chmod(dir, 0700) != nil {
				t.Fatal("private directory failed")
			}
			ca := filepath.Join(dir, "ca.pem")
			if os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600) != nil {
				t.Fatal("CA write failed")
			}
			p, err := newProbe(target{URL: s.URL + "/mcp", CAFile: ca, Processes: []string{"a", "b"}}, fixture{}, newReceipt())
			if err != nil {
				t.Fatal(err)
			}
			defer p.client.CloseIdleConnections()
			b := deploymentBinding{Origin: s.URL, SourceCommit: strings.Repeat("a", 40), Image: "synthetic-preflight-unit-only", Version: "1.0.0"}
			pods := make([]observedPod, 2)
			pods[0].Metadata.UID = "a"
			pods[1].Metadata.UID = "b"
			proof := ingressProof{Origin: b.Origin, Source: b.SourceCommit, Image: b.Image, PodUIDs: []string{"a", "b"}, EdgeConfig: strings.Repeat("b", 64), TLSVerified: true, NoRedirects: true, CNIEnforced: true, Reference: "synthetic unit receipt only"}
			if writePrivate(filepath.Join(dir, "ingress-verification.json"), proof) != nil {
				t.Fatal("proof write failed")
			}
			a := executionAuthorization{IngressSHA256: fileHash(filepath.Join(dir, "ingress-verification.json"))}
			if p.publicPreflight(context.Background(), dir, a, b, pods) == nil || calls.Load() != 1 || bearer.Load() {
				t.Fatal("public metadata mismatch passed or bearer was sent")
			}
		})
	}
}

func TestDatastoreGuardRejectsProductionAndDSNOverrides(t *testing.T) {
	if err := validateDSN("postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable", true); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{
		"postgres://hikyo:synthetic@production.example:15432/hikyo_mcp_651?sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo?sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable&host=production.example",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable&dbname=production",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable&sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:543/hikyo_mcp_651?sslmode=disable",
		"host=127.0.0.1 dbname=hikyo_mcp_651",
	} {
		if validateDSN(dsn, true) == nil {
			t.Fatal("accepted datastore outside local fixture")
		}
	}
	if validateDSN("postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable", false) == nil {
		t.Fatal("production accepted local development datastore")
	}
	if validateDSN("postgres://hikyo:synthetic@db.hikyo-mcp-651.svc.cluster.local:5432/hikyo_mcp_651?sslmode=verify-full", false) != nil {
		t.Fatal("refused dedicated verified production fixture datastore")
	}
}

func TestProtectedInputsRejectSymlinksFIFOsAndTrailingDocuments(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte(`{"dsn":"synthetic"} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	var cfg settings
	if readPrivate(path, &cfg) == nil {
		t.Fatal("accepted trailing protected document")
	}
	link := filepath.Join(dir, "link")
	if os.Symlink(path, link) != nil {
		t.Fatal("symlink creation failed")
	}
	if readPrivate(link, &cfg) == nil {
		t.Fatal("accepted symlinked protected input")
	}
	fifo := filepath.Join(dir, "fifo")
	if syscall.Mkfifo(fifo, 0600) != nil {
		t.Fatal("FIFO creation failed")
	}
	if privatePath(fifo, false) == nil {
		t.Fatal("accepted a blocking nonregular credential input")
	}
	if os.Chmod(path, 0640) != nil {
		t.Fatal("chmod failed")
	}
	if readPrivate(path, &cfg) == nil {
		t.Fatal("accepted group-readable protected input")
	}
}

func TestBearerNeverFollowsHTTPSRedirect(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Store(true); w.WriteHeader(200) }))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	der := source.Certificate().Raw
	if _, err := x509.ParseCertificate(der); err != nil {
		t.Fatal(err)
	}
	if os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600) != nil {
		t.Fatal("CA write failed")
	}
	p, err := newProbe(target{URL: source.URL + "/mcp", CAFile: ca, Processes: []string{"a", "b"}}, fixture{}, newReceipt())
	if err != nil {
		t.Fatal(err)
	}
	defer p.client.CloseIdleConnections()
	r, err := p.call(context.Background(), "tools/call", "hikyo_list_definitions", map[string]any{}, "synthetic-bearer")
	if err != nil || r.Status != 307 || forwarded.Load() {
		t.Fatal("bearer redirect guard failed")
	}
	// The system trust store must refuse the same private-CA origin unless
	// its trust root was explicitly supplied. No InsecureSkipVerify fallback.
	p, err = newProbe(target{URL: source.URL + "/mcp", Processes: []string{"a", "b"}}, fixture{}, newReceipt())
	if err != nil {
		t.Fatal(err)
	}
	defer p.client.CloseIdleConnections()
	if _, err := p.call(context.Background(), "tools/list", "", nil, ""); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}

func TestCandidateCannotUseMutableImageOrMissingAuthority(t *testing.T) {
	var b deploymentBinding
	if validateBinding(&b) == nil {
		t.Fatal("accepted unselected final artifact")
	}
	b.SourceCommit = strings.Repeat("a", 40)
	b.SourceTree = strings.Repeat("b", 40)
	b.SelectionRef = "synthetic unit selection only"
	b.Image = "ghcr.io/hikyo-org/hikyo:latest"
	if validateBinding(&b) == nil {
		t.Fatal("accepted mutable image")
	}
	b.Image = "ghcr.io/hikyo-org/hikyo@sha256:" + strings.Repeat("c", 64)
	if validateBinding(&b) == nil {
		t.Fatal("accepted missing artifact and operator identities")
	}
}

func TestNamespaceCreateFailureCannotMutateExistingNamespace(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"items":[{"kind":"Namespace","metadata":{"name":"hikyo-mcp-651"}},{"kind":"ResourceQuota"},{"kind":"ServiceAccount"},{"kind":"NetworkPolicy"},{"kind":"PersistentVolumeClaim"}]}`)
	calls := 0
	err = createReservationObjects(dir, executionAuthorization{}, raw, func(input []byte) ([]byte, error) {
		calls++
		var object struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(input, &object) != nil || object.Kind != "Namespace" {
			t.Fatal("namespaced resources dispatched before exclusive namespace creation")
		}
		return nil, errors.New("already exists, denied, transport failure or lost acknowledgement")
	})
	if err == nil || calls != 1 {
		t.Fatal("failed namespace creation reached later mutations")
	}
	if _, err := os.Lstat(filepath.Join(dir, "reservation.json")); !os.IsNotExist(err) {
		t.Fatal("failed creation recorded ownership")
	}
	calls = 0
	err = createReservationObjects(dir, executionAuthorization{}, raw, func(input []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{"metadata":{"name":"hikyo-mcp-651","uid":"unit-created-namespace","labels":{"hikyo.dev/fixture":"mcp-651"}}}`), nil
		}
		var objects struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		}
		if json.Unmarshal(input, &objects) != nil || len(objects.Items) != 4 {
			t.Fatal("remaining create inventory incorrect")
		}
		for _, object := range objects.Items {
			if object.Kind == "Namespace" {
				t.Fatal("namespace was updated instead of exclusively created")
			}
		}
		return nil, errors.New("partial child creation failure")
	})
	if err == nil || calls != 2 {
		t.Fatal("partial creation failure hidden")
	}
	var recorded reservationReceipt
	if err := readPrivate(filepath.Join(dir, "reservation.json"), &recorded); err != nil || recorded.NamespaceUID != "unit-created-namespace" {
		t.Fatal("partial reservation lost original ownership identity", err)
	}
}

func TestShellAuthorizationFailureCannotReachKubectlInConditionalCleanup(t *testing.T) {
	for _, name := range []string{"deploy.sh", "check-installation.sh"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			wrapper := ""
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "kube() ") {
					wrapper = line
				}
			}
			if wrapper == "" {
				t.Fatal("guarded wrapper not found")
			}
			dir := t.TempDir()
			fixture := filepath.Join(dir, "fixture")
			kube := filepath.Join(dir, "kubectl")
			marker := filepath.Join(dir, "called")
			if err := os.WriteFile(kube, []byte("#!/bin/sh\nprintf called > \"$TEST_MARKER\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, authorized := range []bool{false, true} {
				status := "42"
				if authorized {
					status = "0"
				}
				if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit "+status+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				command := exec.Command("sh", "-eu", "-c", "fixture=$TEST_FIXTURE; runtime=$TEST_RUNTIME\n"+wrapper+"\nkube delete only-unit-fixture || true")
				command.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "TEST_FIXTURE="+fixture, "TEST_RUNTIME="+dir, "TEST_MARKER="+marker)
				if out, err := command.CombinedOutput(); err != nil {
					t.Fatalf("wrapper execution: %v %s", err, out)
				}
				_, err := os.Stat(marker)
				if authorized && err != nil || !authorized && !os.IsNotExist(err) {
					t.Fatal("kubectl dispatch did not respect authorization in conditional context")
				}
			}
		})
	}
}
