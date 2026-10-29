//go:build darwin || linux

package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
)

//go:embed resources.yaml.tmpl
var resourceTemplate string

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
}

type publicFile struct{ Key, Path string }

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
		b.PublicFiles = append(b.PublicFiles, publicFile{Key: key, Path: filepath.ToSlash(rel)})
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
	f, err := os.OpenFile(filepath.Join(dir, "resources.yaml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("refusing existing rendered resources")
	}
	_, writeErr := f.Write(output.Bytes())
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
