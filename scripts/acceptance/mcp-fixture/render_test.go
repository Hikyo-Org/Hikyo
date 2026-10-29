//go:build darwin || linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestRenderedResourcesAreIsolatedAndStartServersOnlyAfterSeeding(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory failed")
	}
	if os.MkdirAll(filepath.Join(dir, "public", "bundle"), 0700) != nil {
		t.Fatal("public directory failed")
	}
	files := map[string][]byte{"operator.pub": []byte("synthetic-render-only-key"), "bundle/index.json": []byte(`{"synthetic":"syntax only, no authority"}`)}
	inventory := map[string]string{}
	for path, b := range files {
		if os.WriteFile(filepath.Join(dir, "public", path), b, 0600) != nil {
			t.Fatal("public input failed")
		}
		hash := sha256.Sum256(b)
		inventory[path] = fmt.Sprintf("%x", hash)
	}
	b, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(b)
	digest := strings.Repeat("a", 64)
	binding := deploymentBinding{SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), Image: "ghcr.io/hikyo-org/hikyo@sha256:" + digest, PlatformManifest: digest, ImageConfig: digest, Binary: digest, HelperBinary: digest, Manifest: digest, Bundle: fmt.Sprintf("%x", hash), OperatorPin: fileHash(filepath.Join(dir, "public", "operator.pub")), VerificationReceipt: digest, SelectionRef: "SYNTHETIC render unit only", Channel: "stable/v1", Version: "1.0.0-synthetic-render-only", Origin: "https://fixture.example", NodeA: "node-a", NodeB: "node-b", DNSIP: "10.43.0.10", ProxyCIDR: "10.42.0.0/16"}
	if writePrivate(filepath.Join(dir, "binding.json"), binding) != nil {
		t.Fatal("binding write failed")
	}
	if err := render(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "resources.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	objects := 0
	serverSeen, gatewaySeen, defaultDenySeen := false, false, false
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if object.GetKind() == "" {
			continue
		}
		objects++
		if object.GetKind() != "Namespace" && object.GetNamespace() != "hikyo-mcp-651" {
			t.Fatal("resource escaped isolated namespace")
		}
		if object.GetKind() == "Namespace" && object.GetName() != "hikyo-mcp-651" {
			t.Fatal("unexpected cluster-scoped resource")
		}
		if object.GetKind() == "StatefulSet" && object.GetName() == "hikyo" {
			replicas, ok, err := unstructured.NestedInt64(object.Object, "spec", "replicas")
			if err != nil || !ok || replicas != 0 {
				t.Fatal("candidate servers start before supported seeding")
			}
			serverSeen = true
			if claims, _, _ := unstructured.NestedSlice(object.Object, "spec", "volumeClaimTemplates"); len(claims) != 0 {
				t.Fatal("replica-local installation custody cannot admit shared database")
			}
			assertSharedCustody(t, object.Object, "spec", "template", "spec")
		}
		if object.GetKind() == "Pod" && object.GetName() == "tooling" {
			assertSharedCustody(t, object.Object, "spec")
		}
		if object.GetKind() == "Deployment" && object.GetName() == "gateway" {
			replicas, ok, err := unstructured.NestedInt64(object.Object, "spec", "replicas")
			if err != nil || !ok || replicas != 0 {
				t.Fatal("gateway starts before selected helper is staged")
			}
			gatewaySeen = true
		}
		if object.GetKind() == "NetworkPolicy" && object.GetName() == "default-deny" {
			policies, _, _ := unstructured.NestedStringSlice(object.Object, "spec", "policyTypes")
			if len(policies) != 2 || !slices.Contains(policies, "Ingress") || !slices.Contains(policies, "Egress") {
				t.Fatal("missing ingress/egress default deny")
			}
			defaultDenySeen = true
		}
		if object.GetKind() == "Service" {
			kind, _, _ := unstructured.NestedString(object.Object, "spec", "type")
			if kind == "LoadBalancer" || kind == "NodePort" {
				t.Fatal("fixture creates unauthorized public listener")
			}
		}
	}
	if objects < 15 || !serverSeen || !gatewaySeen || !defaultDenySeen {
		t.Fatal("incomplete deployable resource inventory")
	}
	var reservation struct {
		APIVersion string                      `json:"apiVersion"`
		Kind       string                      `json:"kind"`
		Items      []unstructured.Unstructured `json:"items"`
	}
	if err := readPrivate(filepath.Join(dir, "reserve.json"), &reservation); err != nil {
		t.Fatal(err)
	}
	if len(reservation.Items) != 5 {
		t.Fatal("reservation includes workloads before storage proof")
	}
	for _, item := range reservation.Items {
		if item.GetKind() == "Pod" || item.GetKind() == "StatefulSet" || item.GetKind() == "Deployment" {
			t.Fatal("reservation starts runtime before storage review")
		}
	}
	// Rendering must not accept changed bundle bytes under a prior receipt.
	if os.Remove(filepath.Join(dir, "resources.yaml")) != nil {
		t.Fatal("remove output failed")
	}
	if os.WriteFile(filepath.Join(dir, "public", "bundle", "index.json"), []byte("changed"), 0600) != nil {
		t.Fatal("change input failed")
	}
	if render(dir) == nil {
		t.Fatal("changed public bundle accepted under selected inventory hash")
	}
}

func assertSharedCustody(t *testing.T, object map[string]interface{}, fields ...string) {
	t.Helper()
	policyPath := append(append([]string{}, fields...), "securityContext", "fsGroupChangePolicy")
	policy, _, _ := unstructured.NestedString(object, policyPath...)
	if policy != "OnRootMismatch" {
		t.Fatal("remount may rewrite private operator custody permissions")
	}
	volumePath := append(append([]string{}, fields...), "volumes")
	volumes, _, _ := unstructured.NestedSlice(object, volumePath...)
	found := false
	for _, volume := range volumes {
		v := volume.(map[string]interface{})
		if v["name"] == "installation" {
			claim, _, _ := unstructured.NestedString(v, "persistentVolumeClaim", "claimName")
			if claim != "installation-authority" {
				t.Fatal("tooling and replicas must share installation authority")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("shared installation volume missing")
	}
}
