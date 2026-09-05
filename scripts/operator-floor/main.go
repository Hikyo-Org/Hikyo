// operator-floor drives the real, separately resource-limited operator pod.
// The delivery endpoint is a synthetic TLS fixture; Kubernetes is never mocked.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	opclient "github.com/Hikyo-Org/hikyo/internal/operator/client"
)

const (
	namespace  = "operator-floor"
	objects    = 50
	keys       = 64
	valueBytes = 1024
	// The complete fleet must settle within the shipped periodic fetch cadence.
	phaseLimit = 5 * time.Minute
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "operator-floor:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 4 && os.Args[1] == "verify-resources" {
		return verifyResources(os.Args[2], os.Args[3])
	}
	if len(os.Args) == 2 && os.Args[1] == "serve" {
		return serve()
	}
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: operator-floor serve | KUBECONFIG CA_FILE OUTPUT_JSON")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Args[1])
	if err != nil {
		return err
	}
	cfg.QPS, cfg.Burst = 50, 100 // Load driver only; production client defaults stay intact.
	sch := runtime.NewScheme()
	if err := scheme.AddToScheme(sch); err != nil {
		return err
	}
	if err := hikyov1.AddToScheme(sch); err != nil {
		return err
	}
	cl, err := client.New(cfg, client.Options{Scheme: sch})
	if err != nil {
		return err
	}
	ca, err := os.ReadFile(os.Args[2])
	if err != nil {
		return err
	}
	inst := &hikyov1.HikyoInstance{ObjectMeta: metav1.ObjectMeta{Name: "floor"}, Spec: hikyov1.HikyoInstanceSpec{
		URL: "https://127.0.0.1:9443", CABundle: base64.StdEncoding.EncodeToString(ca),
	}}
	if err := cl.Create(ctx, inst); err != nil {
		return err
	}
	for i := range objects {
		name := fmt.Sprintf("delivery-%03d", i)
		cred := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace,
			Labels: map[string]string{hikyov1.LabelDelivery: hikyov1.LabelDeliveryValue, hikyov1.LabelInstance: "floor"}},
			Data: map[string][]byte{hikyov1.BootstrapTokenKey: []byte(name)}}
		if err := cl.Create(ctx, cred); err != nil {
			return err
		}
		cr := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: hikyov1.HikyoSecretSpec{
			InstanceRef: hikyov1.InstanceRef{Name: "floor"}, Auth: hikyov1.AuthRef{SecretRef: &hikyov1.LocalObjectRef{Name: name}},
			Scope:  hikyov1.Scope{Org: "org_0192f000-0000-7000-8000-00000000000a", Project: "prj_0192f000-0000-7000-8000-00000000000b", Environment: "env_0192f000-0000-7000-8000-00000000000c"},
			Target: hikyov1.Target{Name: "managed-" + name, CreationPolicy: hikyov1.CreationPolicyOwner}, Projection: hikyov1.ProjectionFull,
		}}
		for k := range keys {
			cr.Spec.Mapping = append(cr.Spec.Mapping, hikyov1.Mapping{Key: hikyov1.KeyName(fmt.Sprintf("KEY_%03d", k))})
		}
		if err := cl.Create(ctx, cr); err != nil {
			return err
		}
	}
	type phaseResult struct {
		Name    string  `json:"name"`
		Seconds float64 `json:"seconds"`
		Objects int     `json:"objects"`
	}
	results := []phaseResult{}
	versions := map[string]string{}
	for phase, name := range []string{"initial", "updated", "conditional-current", "unavailable-retained", "recovered"} {
		// This command addresses only the kubeconfig for the owned scratch cluster.
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", os.Args[1], "-n", namespace, "exec", "deployment/floor-hikyo-operator", "-c", "fixture", "--", "sh", "-c", fmt.Sprintf("printf %%s %d > /tmp/next-phase && mv /tmp/next-phase /tmp/phase", phase))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fixture phase: %w: %s", err, out)
		}
		// Kubernetes timestamps have second precision. Set the barrier to the next
		// second before inducing the watch events, so old status cannot pass.
		barrier := time.Now().UTC().Truncate(time.Second).Add(time.Second)
		time.Sleep(time.Until(barrier))
		start := time.Now()
		var list hikyov1.HikyoSecretList
		if err := cl.List(ctx, &list, client.InNamespace(namespace)); err != nil {
			return err
		}
		for i := range list.Items {
			cr := &list.Items[i]
			before := cr.DeepCopy()
			cr.Annotations = map[string]string{"floor.hikyo.dev/phase": fmt.Sprint(phase)}
			if err := cl.Patch(ctx, cr, client.MergeFrom(before)); err != nil {
				return err
			}
		}
		deadline := start.Add(phaseLimit)
		for {
			ready, err := settled(ctx, cl, phase, barrier, versions)
			if err != nil {
				return err
			}
			if ready {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("phase %s did not converge all %d CRs within %s", name, objects, phaseLimit)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		results = append(results, phaseResult{Name: name, Seconds: time.Since(start).Seconds(), Objects: objects})
		fmt.Fprintf(os.Stderr, "operator-floor: %s, %d CRs converged in %.2fs\n", name, objects, time.Since(start).Seconds())
		var secrets corev1.SecretList
		if err := cl.List(ctx, &secrets, client.InNamespace(namespace)); err != nil {
			return err
		}
		for _, secret := range secrets.Items {
			versions[secret.Name] = secret.ResourceVersion
		}
	}
	report := struct {
		Schema            string        `json:"schema"`
		CRs               int           `json:"crs"`
		KeysPerCR         int           `json:"keys_per_cr"`
		ValueBytes        int           `json:"value_bytes"`
		PhaseLimitSeconds int           `json:"phase_limit_seconds"`
		Phases            []phaseResult `json:"phases"`
	}{"hikyo.dev/operator-floor/v1", objects, keys, valueBytes, int(phaseLimit.Seconds()), results}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(os.Args[3], append(raw, '\n'), 0o600)
}

func settled(ctx context.Context, cl client.Client, phase int, barrier time.Time, versions map[string]string) (bool, error) {
	var list hikyov1.HikyoSecretList
	if err := cl.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return false, err
	}
	if len(list.Items) != objects {
		return false, fmt.Errorf("expected exactly %d CRs, found %d", objects, len(list.Items))
	}
	for _, cr := range list.Items {
		cond := meta.FindStatusCondition(cr.Status.Conditions, hikyov1.ConditionSynced)
		if cr.Status.LastFetch == nil || cr.Status.LastFetch.Before(&metav1.Time{Time: barrier}) || cond == nil {
			return false, nil
		}
		if phase == 3 {
			if cond.Status != metav1.ConditionFalse || cond.Reason != hikyov1.ReasonFetchFailed {
				return false, nil
			}
		} else if cond.Status != metav1.ConditionTrue || (phase >= 2 && cond.Reason != hikyov1.ReasonCurrent) {
			return false, nil
		}
		var secret corev1.Secret
		if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.Spec.Target.Name}, &secret); err != nil {
			return false, err
		}
		if len(secret.Data) != keys || !metav1.IsControlledBy(&secret, &cr) {
			return false, fmt.Errorf("managed Secret count/ownership mismatch: %s", cr.Name)
		}
		for k := range keys {
			if string(secret.Data[fmt.Sprintf("KEY_%03d", k)]) != value(phase) {
				return false, nil
			}
		}
		if phase >= 2 && versions[secret.Name] != secret.ResourceVersion {
			return false, fmt.Errorf("phase %d rewrote unchanged managed Secret %s", phase, secret.Name)
		}
	}
	return true, nil
}

func value(phase int) string {
	if phase == 0 {
		return strings.Repeat("a", valueBytes)
	}
	return strings.Repeat("b", valueBytes)
}

func serve() error {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer delivery-") || !strings.HasSuffix(r.URL.Path, "/delivery") {
			http.Error(w, "fixture credential", 401)
			return
		}
		phaseRaw, _ := os.ReadFile("/tmp/phase")
		phase := 0
		if len(phaseRaw) > 0 {
			phase = int(phaseRaw[0] - '0')
		}
		if phase == 3 {
			http.Error(w, "fixture unavailable", 503)
			return
		}
		cursor := "floor-1"
		if phase > 0 {
			cursor = "floor-2"
		}
		out := opclient.DeliveryResponse{Cursor: cursor, ChangeToken: cursor, SchemaRevision: 1, Keys: []opclient.DeliveredKey{}}
		out.Current = r.URL.Query().Get("cursor") == cursor
		if !out.Current {
			v := value(phase)
			for k := range keys {
				out.Keys = append(out.Keys, opclient.DeliveredKey{Name: fmt.Sprintf("KEY_%03d", k), Classification: "config", Presence: "set", Value: &v})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	listener, err := net.Listen("tcp", ":9443")
	if err != nil {
		return err
	}
	srv.Listener = listener
	srv.StartTLS()
	defer srv.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile("/tmp/ca.pem", ca, 0o644); err != nil {
		return err
	}
	select {}
}
