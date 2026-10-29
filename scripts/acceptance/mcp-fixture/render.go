//go:build darwin || linux

package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed resources.yaml.tmpl
var resourceTemplate string

//go:embed stage-public.sh
var publicStageScript string

// DeploymentBinding is a selection record, not a signature verifier. The
// genuine runtime gate still authenticates the supplied bundle and installed
// operator pin. No field below can mint release or deployment authority.
type deploymentBinding struct {
	SourceCommit        string       `json:"source_commit"`
	SourceTree          string       `json:"source_tree"`
	Image               string       `json:"image"`
	PlatformManifest    string       `json:"platform_manifest_sha256"`
	ImageConfig         string       `json:"image_config_sha256"`
	Binary              string       `json:"server_binary_sha256"`
	HelperBinary        string       `json:"helper_binary_sha256"`
	Manifest            string       `json:"release_manifest_sha256"`
	Bundle              string       `json:"bundle_inventory_sha256"`
	OperatorPin         string       `json:"operator_public_key_sha256"`
	VerificationReceipt string       `json:"authenticated_verification_receipt_sha256"`
	SelectionRef        string       `json:"owner_final_candidate_selection_ref"`
	Channel             string       `json:"channel"`
	Version             string       `json:"server_version"`
	Origin              string       `json:"origin"`
	NodeA               string       `json:"node_a"`
	NodeB               string       `json:"node_b"`
	DNSIP               string       `json:"cluster_dns_ip"`
	ProxyCIDR           string       `json:"proxy_cidr"`
	OriginHost          string       `json:"-"`
	PublicFiles         []publicFile `json:"-"`
	StageLines          []string     `json:"-"`
}

type publicFile struct{ Key, Path, SHA256 string }

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourcePattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var imagePattern = regexp.MustCompile(`^ghcr\.io/hikyo-org/hikyo@sha256:[a-f0-9]{64}$`)
var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

func validateBinding(b *deploymentBinding) error {
	if !sourcePattern.MatchString(b.SourceCommit) || !sourcePattern.MatchString(b.SourceTree) || !imagePattern.MatchString(b.Image) || b.SelectionRef == "" {
		return errors.New("HOLD: owner-selected clean source/tree and immutable published image required")
	}
	for _, digest := range []string{b.PlatformManifest, b.ImageConfig, b.Binary, b.HelperBinary, b.Manifest, b.Bundle, b.OperatorPin, b.VerificationReceipt} {
		if !sha256Pattern.MatchString(digest) {
			return errors.New("HOLD: complete authenticated artifact, bundle and operator identities required")
		}
	}
	if b.Channel != "stable/v1" && b.Channel != "nightly/v1" {
		return errors.New("explicit authenticated trust channel required")
	}
	if b.Version == "" || len(b.Version) > 128 {
		return errors.New("selected authenticated server version required")
	}
	u, err := url.Parse(b.Origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || net.ParseIP(u.Hostname()) != nil || !hostnamePattern.MatchString(u.Hostname()) {
		return errors.New("exact public HTTPS hostname origin required")
	}
	for _, node := range []string{b.NodeA, b.NodeB} {
		if !hostnamePattern.MatchString(node) {
			return errors.New("explicit host placement required")
		}
	}
	if b.NodeA == b.NodeB || net.ParseIP(b.DNSIP) == nil {
		return errors.New("two distinct nodes and observed cluster DNS address required")
	}
	if _, _, err := net.ParseCIDR(b.ProxyCIDR); err != nil {
		return errors.New("explicit gateway trust CIDR required")
	}
	b.OriginHost = u.Hostname()
	return nil
}

func render(dir string) error {
	var b deploymentBinding
	if err := readPrivate(filepath.Join(dir, "binding.json"), &b); err != nil {
		return err
	}
	if err := validateBinding(&b); err != nil {
		return err
	}
	public := filepath.Join(dir, "public")
	b.StageLines = strings.Split(strings.TrimSuffix(publicStageScript, "\n"), "\n")
	files := map[string][]byte{}
	inventory := map[string]string{}
	total := 0
	if err := filepath.WalkDir(public, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("authenticated public bundle unavailable")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinked public material refused")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("nonregular public material refused")
		}
		rel, err := filepath.Rel(public, path)
		if err != nil {
			return err
		}
		if rel != "operator.pub" && !bytes.HasPrefix([]byte(rel), []byte("bundle"+string(filepath.Separator))) {
			return errors.New("public mount accepts only bundle and operator.pub")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(filepath.ToSlash(rel)) {
			return errors.New("public artifact path is outside the closed portable grammar")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return errors.New("public material read failed")
		}
		total += len(raw)
		if total > 750<<10 {
			return errors.New("HOLD: bundle exceeds reviewed ConfigMap budget; new storage packet required")
		}
		key := fmt.Sprintf("file-%d", len(files))
		files[key] = raw
		hash := sha256.Sum256(raw)
		inventory[filepath.ToSlash(rel)] = fmt.Sprintf("%x", hash)
		b.PublicFiles = append(b.PublicFiles, publicFile{Key: key, Path: filepath.ToSlash(rel), SHA256: fmt.Sprintf("%x", hash)})
		return nil
	}); err != nil {
		return err
	}
	if fileHash(filepath.Join(public, "operator.pub")) != b.OperatorPin {
		return errors.New("operator pin differs from selected binding")
	}
	inventoryBytes, _ := json.Marshal(inventory)
	inventoryHash := sha256.Sum256(inventoryBytes)
	if fmt.Sprintf("%x", inventoryHash) != b.Bundle {
		return errors.New("public bundle inventory differs from selected binding")
	}
	if _, err := os.Stat(filepath.Join(public, "bundle", "index.json")); err != nil {
		return errors.New("public bundle index missing")
	}
	t, err := template.New("resources").Option("missingkey=error").Parse(resourceTemplate)
	if err != nil {
		return errors.New("fixture template invalid")
	}
	var output bytes.Buffer
	cm := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]string{"name": "authenticated-public-bundle", "namespace": "hikyo-mcp-651"}, "binaryData": files}
	if t.Execute(&output, b) != nil {
		return errors.New("fixture template rendering failed")
	}
	output.WriteString("\n---\n")
	if json.NewEncoder(&output).Encode(cm) != nil {
		return errors.New("public mount encoding failed")
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output.Bytes()), 4096)
	var reservation []unstructured.Unstructured
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("rendered Kubernetes resources invalid")
		}
		kind, name := object.GetKind(), object.GetName()
		if kind == "Namespace" || kind == "ResourceQuota" || kind == "ServiceAccount" || kind == "NetworkPolicy" && name == "default-deny" || kind == "PersistentVolumeClaim" && name == "installation-authority" {
			reservation = append(reservation, object)
		}
	}
	if len(reservation) != 5 {
		return errors.New("incomplete reservation resource set")
	}
	if err := writePrivate(filepath.Join(dir, "reserve.json"), map[string]any{"apiVersion": "v1", "kind": "List", "items": reservation}); err != nil {
		return err
	}
	if err := renderStorageChecks(dir, b); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "resources.yaml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("refusing existing rendered resources")
	}
	_, writeErr := f.Write(output.Bytes())
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func renderStorageChecks(dir string, b deploymentBinding) error {
	var pods []map[string]any
	for i, node := range []string{b.NodeA, b.NodeB} {
		pods = append(pods, map[string]any{
			"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]any{"name": fmt.Sprintf("custody-check-%d", i), "namespace": "hikyo-mcp-651", "labels": map[string]string{"hikyo.dev/fixture": "mcp-651", "app": "custody-check"}},
			"spec": map[string]any{
				"serviceAccountName": "fixture", "automountServiceAccountToken": false,
				"nodeSelector":    map[string]string{"kubernetes.io/hostname": node},
				"securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532, "fsGroup": 65532, "fsGroupChangePolicy": "OnRootMismatch", "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
				"containers":      []map[string]any{{"name": "check", "image": "alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b", "command": []string{"sleep", "3600"}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}, "resources": map[string]any{"requests": map[string]string{"cpu": "10m", "memory": "16Mi"}, "limits": map[string]string{"cpu": "100m", "memory": "32Mi"}}, "volumeMounts": []map[string]any{{"name": "installation", "mountPath": "/installation"}, {"name": "work", "mountPath": "/work"}}}},
				"volumes":         []map[string]any{{"name": "installation", "persistentVolumeClaim": map[string]string{"claimName": "installation-authority"}}, {"name": "work", "emptyDir": map[string]string{"medium": "Memory", "sizeLimit": "1Mi"}}},
			},
		})
	}
	return writePrivate(filepath.Join(dir, "storage-check.json"), map[string]any{"apiVersion": "v1", "kind": "List", "items": pods})
}
