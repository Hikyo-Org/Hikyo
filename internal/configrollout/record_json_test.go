package configrollout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// v1DeploymentSpec is what encoding/json (v1) wrote for the spec below. Digests
// persisted by earlier binaries hash these bytes; v2 alone would add
// "minReadySeconds":0, "paused":false, "generation":0 and drop "strategy":{}.
const v1DeploymentSpec = `{"replicas":1,"selector":{"matchLabels":{"app":"hikyo"}},"template":{"metadata":{"labels":{"app":"hikyo"}},"spec":{"containers":[{"name":"hikyo","image":"hikyo:1","env":[{"name":"HIKYO_A","value":"\u003ca\u0026b\u003e"}],"resources":{}}]}},"strategy":{}}`

func recordSpec() appsv1.DeploymentSpec {
	one := int32(1)
	return appsv1.DeploymentSpec{
		Replicas: &one,
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hikyo"}},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "hikyo"}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "hikyo", Image: "hikyo:1", Env: []corev1.EnvVar{{Name: "HIKYO_A", Value: "<a&b>"}}}}},
		},
	}
}

func TestRecordJSONKeepsKubernetesV1Bytes(t *testing.T) {
	spec := recordSpec()
	got, err := json.Marshal(spec, recordJSON)
	if err != nil || string(got) != v1DeploymentSpec {
		t.Fatalf("spec bytes changed (%v):\n got %s\nwant %s", err, got, v1DeploymentSpec)
	}
	sum := sha256.Sum256([]byte(v1DeploymentSpec))
	if d := digest(spec); d != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest(spec) = %s, not the hash of the v1 bytes", d)
	}
	// Kubernetes values nested in our own records keep the same bytes.
	nested, err := json.Marshal(struct {
		Spec   *appsv1.DeploymentSpec `json:"spec"`
		Absent *corev1.EnvVar         `json:"absent"`
	}{Spec: &spec}, recordJSON)
	if want := `{"spec":` + v1DeploymentSpec + `,"absent":null}`; err != nil || string(nested) != want {
		t.Fatalf("nested bytes changed (%v):\n got %s\nwant %s", err, nested, want)
	}
	secret := &corev1.Secret{}
	if err := putRecord(secret, "k", spec); err != nil || string(secret.Data["k"]) != v1DeploymentSpec {
		t.Fatalf("putRecord stored %s (%v)", secret.Data["k"], err)
	}
}

// The library serializer replaces invalid UTF-8; digest must still refuse it.
func TestDigestOfUnencodableKubernetesValueNeverMatches(t *testing.T) {
	bad := corev1.EnvVar{Name: "HIKYO_X", Value: "\xff"}
	first, second := digest(bad), digest(bad)
	if first == second {
		t.Fatal("unencodable Kubernetes values digest equal; a digest check would pass")
	}
	if err := putRecord(&corev1.Secret{}, "k", envDelta{Name: "HIKYO_X", After: bad}); err == nil {
		t.Fatal("putRecord must refuse an unencodable Kubernetes value")
	}
}
