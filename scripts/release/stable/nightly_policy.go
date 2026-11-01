package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// prepareNightlyPolicyRotation produces unsigned public documents for the
// existing recovery rotation ceremony. It neither holds a signer nor installs
// the proposal. The caller must review and recovery-sign all three changed
// authority documents before asking VerifySnapshot to accept the successor.
func prepareNightlyPolicyRotation(t publicTrust, currentTrustDir, newPolicyPath, outputDir string) error {
	if !t.snapshot.Valid() {
		return errors.New("nightly policy rotation requires authenticated current trust")
	}
	verified, err := releasetrust.VerifySnapshot(t.pinned, t.material, t.snapshot.Floor())
	if err != nil || verified.Digest() != t.snapshot.Digest() {
		return errors.New("nightly policy rotation refuses changed cached trust authority")
	}
	current, err := loadTrust(currentTrustDir)
	if err != nil {
		return fmt.Errorf("nightly policy rotation current trust: %w", err)
	}
	if current.snapshot.Digest() != t.snapshot.Digest() || !reflect.DeepEqual(current.pinned, t.pinned) || !reflect.DeepEqual(current.material, t.material) ||
		!reflect.DeepEqual(current.metadata, t.metadata) || !reflect.DeepEqual(current.catalog, t.catalog) {
		return errors.New("nightly policy rotation current trust changed after authentication")
	}
	t = current
	policy, enabled := t.snapshot.StablePolicy()
	if !enabled {
		return errors.New("nightly policy rotation requires the existing recovery-authorized stable delegation; use its explicit setup ceremony first")
	}
	previous, err := releasetrust.ParseNightlyPolicy(t.material.NightlyPolicy)
	if err != nil || previous.Schema != "hikyo.dev/nightly-policy/v1" {
		return errors.New("nightly policy rotation requires the currently authorized v1 main-source policy")
	}
	oldDigest := releaseidentity.Hash(t.material.NightlyPolicy)
	if !slices.Equal(t.catalog.NightlyPolicies, policy.NightlyPolicies) || !slices.Contains(t.catalog.NightlyPolicies, oldDigest) {
		return errors.New("nightly policy rotation requires matching current catalog and stable-policy nightly authorization; reconcile through an explicit recovery ceremony")
	}
	trustedRoot, err := readDocument(filepath.Join(currentTrustDir, "nightly"), "trusted-root.json")
	if err != nil || releaseidentity.Hash(trustedRoot) != previous.TrustedRootSHA256 {
		return errors.New("nightly policy rotation requires the exact current nightly Sigstore root")
	}
	var raw []byte
	if newPolicyPath == "" {
		next := previous
		next.Schema, next.SourceRef, next.SourceWorkflowPath = "hikyo.dev/nightly-policy/v2", "refs/heads/dev", ".github/workflows/ci-dev.yml"
		raw, err = marshal(next)
	} else {
		raw, err = readDocument(filepath.Dir(newPolicyPath), filepath.Base(newPolicyPath))
	}
	if err != nil {
		return err
	}
	next, err := releasetrust.ParseNightlyPolicy(raw)
	if err != nil || next.Schema != "hikyo.dev/nightly-policy/v2" {
		return errors.New("nightly policy proposal requires the explicit v2 dev-source policy")
	}
	identity := next
	identity.Schema, identity.SourceRef, identity.SourceWorkflowPath = previous.Schema, "", ""
	if !reflect.DeepEqual(identity, previous) {
		return errors.New("nightly v2 proposal changes existing issuer, repository, root, runner, workflow, main ref or revocation authority; use a separate reviewed recovery ceremony")
	}
	newDigest := releaseidentity.Hash(raw)
	if slices.Contains(t.catalog.NightlyPolicies, newDigest) {
		return errors.New("nightly v2 policy is already authorized; no new rotation proposal is needed")
	}
	if len(t.catalog.NightlyPolicies) >= 256 || t.metadata.Sequence >= 9007199254740991 || t.catalog.Sequence >= 9007199254740991 {
		return errors.New("nightly policy rotation authorization or sequence bound is exhausted")
	}

	var root releasetrust.Root
	if err := definitions.DecodeStrict(t.pinned.Root, &root); err != nil {
		return err
	}
	metadata := t.metadata
	metadata.Sequence++
	metadata.Event.Type, metadata.Event.SignedBy = "rotation", root.Recovery.ID
	// These identify a keyless release event. The existing recovery branch of
	// VerifySnapshot forbids them, so retain the previous signed identity in the
	// proposal summary and use the schema's existing recovery rotation event.
	metadata.SourceCommit, metadata.ReleaseTag = "", ""
	if err := releasetrust.ValidateMetadata(root, metadata); err != nil {
		return fmt.Errorf("current metadata cannot represent recovery policy rotation; use an explicit supported recovery ceremony: %w", err)
	}
	metadataRaw, err := marshal(metadata)
	if err != nil {
		return err
	}
	policies := append(slices.Clone(t.catalog.NightlyPolicies), newDigest)
	policy.NightlyPolicies = slices.Clone(policies)
	if err := policy.Validate(); err != nil {
		return err
	}
	policyRaw, err := marshal(policy)
	if err != nil {
		return err
	}
	catalog := t.catalog
	catalog.Sequence++
	catalog.NightlyPolicies = slices.Clone(policies)
	catalog.StableMetadataSHA256 = releaseidentity.Hash(metadataRaw)
	catalog.StablePolicySHA256 = releaseidentity.Hash(policyRaw)
	catalogRaw, err := marshal(catalog)
	if err != nil {
		return err
	}

	files := map[string][]byte{
		"nightly-policy.json": raw,
		"stable-policy.json":  policyRaw,
		"metadata.json":       metadataRaw,
		"catalog.json":        catalogRaw,
	}
	summary := struct {
		Schema                  string                            `json:"schema"`
		Status                  string                            `json:"status"`
		SignatureAuthority      string                            `json:"signature_authority"`
		RequiredSignatures      []string                          `json:"required_signatures"`
		SourceSnapshotSHA256    releaseidentity.Digest            `json:"source_snapshot_sha256"`
		SourceFloor             releaseidentity.SnapshotFloor     `json:"source_floor"`
		PreviousDelegatedCommit string                            `json:"previous_delegated_commit,omitempty"`
		PreviousDelegatedTag    string                            `json:"previous_delegated_tag,omitempty"`
		Previous                map[string]releaseidentity.Digest `json:"previous"`
		Proposed                map[string]releaseidentity.Digest `json:"proposed"`
		Install                 map[string]string                 `json:"install_after_signature_verification"`
	}{
		Schema: "hikyo.dev/nightly-policy-rotation-proposal/v1", Status: "UNSIGNED", SignatureAuthority: root.Recovery.ID,
		RequiredSignatures:   []string{"stable-policy.json", "metadata.json", "catalog.json"},
		SourceSnapshotSHA256: t.snapshot.Digest(), SourceFloor: t.snapshot.Floor(),
		PreviousDelegatedCommit: t.metadata.SourceCommit, PreviousDelegatedTag: t.metadata.ReleaseTag,
		Previous: map[string]releaseidentity.Digest{
			"root.json": releaseidentity.Hash(t.pinned.Root), "nightly-policy.json": oldDigest,
			"stable-policy.json": releaseidentity.Hash(t.material.StablePolicy), "metadata.json": releaseidentity.Hash(t.material.Metadata),
			"catalog.json": releaseidentity.Hash(t.material.Catalog), "nightly-trusted-root.json": releaseidentity.Hash(trustedRoot),
			"stable-trusted-root.json": releaseidentity.Hash(t.material.StableTrustedRoot),
		},
		Proposed: map[string]releaseidentity.Digest{},
		Install: map[string]string{
			"nightly-policy.json": "nightly/policy.json", "stable-policy.json": "stable-policy.json",
			"metadata.json": "metadata.json", "catalog.json": "catalog.json",
		},
	}
	for name, contents := range files {
		summary.Proposed[name] = releaseidentity.Hash(contents)
	}
	files["proposal.json"], err = marshal(summary)
	if err != nil {
		return err
	}
	return writeNightlyRotationProposal(currentTrustDir, outputDir, files)
}

func writeNightlyRotationProposal(currentTrustDir, outputDir string, files map[string][]byte) (err error) {
	if outputDir == "" {
		return errors.New("new proposal output directory is required")
	}
	trust, err := filepath.EvalSymlinks(currentTrustDir)
	if err != nil {
		return err
	}
	trust, err = filepath.Abs(trust)
	if err != nil {
		return err
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return err
	}
	output = filepath.Join(parent, filepath.Base(output))
	relative, err := filepath.Rel(trust, output)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("nightly policy proposal must be outside the current production trust directory")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return fmt.Errorf("nightly policy proposal requires a new directory: %w", err)
	}
	owned, err := os.Stat(output)
	if err != nil {
		return err
	}
	directory, err := os.OpenRoot(output)
	if err != nil {
		return errors.Join(err, os.Remove(output))
	}
	created := []string{}
	defer func() {
		if err != nil {
			for _, name := range created {
				err = errors.Join(err, directory.Remove(name))
			}
		}
		err = errors.Join(err, directory.Close())
		if err != nil {
			current, statErr := os.Stat(output)
			if statErr != nil || !os.SameFile(owned, current) {
				err = errors.Join(err, errors.New("proposal directory identity changed; cleanup refused"))
				return
			}
			err = errors.Join(err, os.Remove(output))
		}
	}()
	for _, name := range []string{"nightly-policy.json", "stable-policy.json", "metadata.json", "catalog.json", "proposal.json"} {
		file, openErr := directory.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if openErr != nil {
			return openErr
		}
		created = append(created, name)
		_, writeErr := io.Copy(file, bytes.NewReader(files[name]))
		if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	return nil
}
