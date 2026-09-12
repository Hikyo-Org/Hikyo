//go:build k8se2e

package isolation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func testNativeSecretTypes(t *testing.T, cfg *rest.Config, sch *runtime.Scheme) {
	registry, image := os.Getenv("HIKYO_K8S_E2E_REGISTRY"), os.Getenv("HIKYO_K8S_E2E_PRIVATE_IMAGE")
	if registry == "" || image == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("native registry fixture is mandatory in CI; use scripts/ci/k8s-e2e.sh")
		}
		t.Skip("native registry fixture not configured; use scripts/ci/k8s-e2e.sh")
	}
	passwordFile := os.Getenv("HIKYO_K8S_E2E_REGISTRY_PASSWORD_FILE")
	password, err := os.ReadFile(passwordFile)
	must(t, err)
	registryJSON, err := json.Marshal(map[string]any{"auths": map[string]any{registry: map[string]string{"auth": base64.StdEncoding.EncodeToString(append([]byte("hikyo-test:"), password...))}}})
	must(t, err)
	certPEM, keyPEM := nativeTLSFixture(t)
	e := newOpEnv(t, cfg, sch, false)
	e.createInstance(instanceName, "")
	principal, credential := e.newWorkloadCredential("native-consumers")
	grantE2ERead(t, e.db, principal.Principal)
	seedE2EReveal(t, e.db, "g_native_reveal", principal.Principal, domain.CapReveal)
	e.createBootstrapSecret("boot", credential.Value, instanceName, true)
	publishE2EValues(t, e.db, map[string]string{secKeyOne: string(registryJSON), cfgKeyOne: string(certPEM), secKeyTwo: string(keyPEM)})
	e.createCR(crSpec{name: "registry", target: "registry-credentials", secretRef: "boot", secretType: corev1.SecretTypeDockerConfigJson, mapping: [][2]string{{secKeyOne, corev1.DockerConfigJsonKey}}})
	e.createCR(crSpec{name: "tls", target: "ingress-tls", secretRef: "boot", secretType: corev1.SecretTypeTLS, mapping: [][2]string{{cfgKeyOne, corev1.TLSCertKey}, {secKeyTwo, corev1.TLSPrivateKeyKey}}})
	reconciler := e.reconciler()
	for _, name := range []string{"registry", "tls"} {
		must(t, e.reconcile(reconciler, name))
		requireCondition(t, e.getCR(name), hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	}
	registrySecret, exists := e.getSecret("registry-credentials")
	if !exists || registrySecret.Type != corev1.SecretTypeDockerConfigJson {
		t.Fatal("registry Secret not materialized with native type")
	}
	tlsSecret, exists := e.getSecret("ingress-tls")
	if !exists || tlsSecret.Type != corev1.SecretTypeTLS {
		t.Fatal("TLS Secret not materialized with native type")
	}

	// First prove the repository is private; an image cached on the node still
	// requires registry authentication because PullAlways resolves the manifest.
	makePod := func(name string, authenticated bool) *corev1.Pod {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: e.ns}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: image, ImagePullPolicy: corev1.PullAlways}}}}
		if authenticated {
			pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: registrySecret.Name}}
		}
		must(t, e.cl.Create(t.Context(), pod))
		return pod
	}
	unauthenticated := makePod("private-without-credentials", false)
	must(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		if err := e.cl.Get(ctx, types.NamespacedName{Namespace: e.ns, Name: unauthenticated.Name}, &pod); err != nil {
			return false, err
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Running != nil {
				t.Fatal("private image pulled without delivered credentials")
			}
			if w := status.State.Waiting; w != nil && (w.Reason == "ErrImagePull" || w.Reason == "ImagePullBackOff") {
				message := strings.ToLower(w.Message)
				return strings.Contains(message, "401") || strings.Contains(message, "unauthorized") || strings.Contains(message, "no basic auth credentials") || strings.Contains(message, "authorization failed"), nil
			}
		}
		return false, nil
	}))
	authenticated := makePod("private-with-delivered-credentials", true)
	must(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		if err := e.cl.Get(ctx, types.NamespacedName{Namespace: e.ns, Name: authenticated.Name}, &pod); err != nil {
			return false, err
		}
		return pod.Status.Phase == corev1.PodRunning, nil
	}))
	t.Log("private image pull refused without credentials and succeeded with the operator-delivered dockerconfigjson Secret")

	// Persist an actual Ingress reference, and run a verified TLS handshake
	// using the exact Secret bytes read back from Kubernetes. This does not
	// claim a particular third-party Ingress controller is installed.
	ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "preview", Namespace: e.ns}, Spec: networkingv1.IngressSpec{
		TLS:            []networkingv1.IngressTLS{{Hosts: []string{"preview.example.test"}, SecretName: tlsSecret.Name}},
		DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "preview", Port: networkingv1.ServiceBackendPort{Number: 443}}},
	}}
	must(t, e.cl.Create(t.Context(), ingress))
	pair, err := tls.X509KeyPair(tlsSecret.Data[corev1.TLSCertKey], tlsSecret.Data[corev1.TLSPrivateKeyKey])
	must(t, err)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("generated TLS certificate invalid")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "preview.example.test"}}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Get(server.URL)
	must(t, err)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal("delivered TLS material did not serve verified HTTPS")
	}
	t.Log("Ingress accepted the delivered TLS Secret reference; its persisted key/certificate completed hostname-verified HTTPS")

	// A normal type change must never recreate the target on the real API.
	cr := e.getCR("registry")
	cr.Spec.Target.Type = corev1.SecretTypeOpaque
	must(t, e.cl.Update(t.Context(), cr))
	must(t, e.reconcile(reconciler, cr.Name))
	requireCondition(t, e.getCR(cr.Name), hikyov1.ConditionConflict, metav1.ConditionTrue, hikyov1.ReasonTargetTypeImmutable)
	retained, exists := e.getSecret(registrySecret.Name)
	if !exists || retained.UID != registrySecret.UID || retained.Type != registrySecret.Type {
		t.Fatal("type change recreated or modified registry credentials")
	}
	cr = e.getCR("registry")
	cr.Spec.Target.Type = corev1.SecretTypeDockerConfigJson
	must(t, e.cl.Update(t.Context(), cr))
	revokeE2ERead(t, e.db, principal.Principal)
	for _, name := range []string{"registry", "tls"} {
		must(t, e.reconcile(reconciler, name))
		requireCondition(t, e.getCR(name), hikyov1.ConditionScrubbed, metav1.ConditionTrue, hikyov1.ReasonAuthorizationWithdrawn)
	}
	for _, name := range []string{registrySecret.Name, tlsSecret.Name} {
		if _, exists := e.getSecret(name); exists {
			t.Fatal("withdrawn native credentials remain")
		}
	}
}

func nativeTLSFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	must(t, err)
	certificate := &x509.Certificate{SerialNumber: serial, DNSNames: []string{"preview.example.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	must(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	must(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
}
