package configrollout

import (
	"bytes"
	"encoding/json"
	"path"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// UpgradeCustodySource is an operator-enrolled tuple on the installation's
// existing public-artifact and persistent-state volumes. Commands carry an
// alias and proof, never arbitrary paths, key material or a new image.
type UpgradeCustodySource struct {
	BundleDirectory       string `json:"bundle_directory"`
	StateDirectory        string `json:"state_directory"`
	EvidenceDirectory     string `json:"evidence_directory"`
	CiphertextPath        string `json:"ciphertext_path"`
	OperatorPublicKeyFile string `json:"operator_public_key_file"`
	TargetManifestSHA256  string `json:"target_manifest_sha256"`
	LegacyWritersStopped  bool   `json:"legacy_writers_stopped"`
}

func (s *UpgradeCustodySource) UnmarshalJSON(raw []byte) error {
	type plain UpgradeCustodySource
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
		return ErrInvalid
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	var value plain
	if decode(raw, &value) != nil {
		return ErrInvalid
	}
	*s = UpgradeCustodySource(value)
	if !s.Valid() {
		return ErrInvalid
	}
	return nil
}

func upgradePath(value, root string) bool {
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return path.Clean(value) == value && strings.HasPrefix(value, root+"/") && len(value) <= 1024 && !strings.ContainsAny(value, "\x00\r\n")
}

func (s UpgradeCustodySource) Valid() bool {
	if !upgradePath(s.BundleDirectory, "/run/hikyo-upgrade") || !upgradePath(s.StateDirectory, "/var/lib/hikyo-upgrade") || !upgradePath(s.OperatorPublicKeyFile, "/run/hikyo-upgrade") {
		return false
	}
	if s.LegacyWritersStopped && s.EvidenceDirectory == "" {
		return false
	}
	if (s.EvidenceDirectory == "") != (s.CiphertextPath == "") {
		return false
	}
	if s.EvidenceDirectory != "" && (!upgradePath(s.EvidenceDirectory, "/run/hikyo-upgrade") || !upgradePath(s.CiphertextPath, "/run/hikyo-upgrade")) {
		return false
	}
	return s.TargetManifestSHA256 == "" || validDigest(s.TargetManifestSHA256)
}

func validUpgradeSources(sources map[string]UpgradeCustodySource) bool {
	if len(sources) > 32 {
		return false
	}
	for alias, source := range sources {
		if len(alias) > 31 || len(validation.IsDNS1123Label(alias)) != 0 || !source.Valid() {
			return false
		}
	}
	return true
}

func UpgradeSourceDigest(source UpgradeCustodySource) string { return digest(source) }

// Environment returns only the seven source-owned bootstrap inputs, in stable
// order. Empty optional settings explicitly clear the previous selection.
func (s UpgradeCustodySource) Environment() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "HIKYO_UPGRADE_BUNDLE", Value: s.BundleDirectory},
		{Name: "HIKYO_UPGRADE_STATE_DIR", Value: s.StateDirectory},
		{Name: "HIKYO_UPGRADE_EVIDENCE", Value: s.EvidenceDirectory},
		{Name: "HIKYO_UPGRADE_BACKUP", Value: s.CiphertextPath},
		{Name: "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY", Value: s.OperatorPublicKeyFile},
		{Name: "HIKYO_UPGRADE_TARGET_MANIFEST", Value: s.TargetManifestSHA256},
		{Name: "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED", Value: strconv.FormatBool(s.LegacyWritersStopped)},
	}
}

const upgradeProofAnnotation = "hikyo.dev/configuration-upgrade-proof"

const upgradeAliasAnnotation = "hikyo.dev/configuration-upgrade-source"

func (k *Kubernetes) validUpgradeProof(p *SourceProof) bool {
	if p == nil {
		return true
	}
	source, ok := k.target.UpgradeSources[p.Alias]
	return ok && source.Valid() && validDigest(p.ProofDigest) && p.RootEpoch == 0 && p.SourceDigest == UpgradeSourceDigest(source)
}

func (k *Kubernetes) prepareUpgradeDelta(d *appsv1.Deployment, p *planData) error {
	if p.Bootstrap == nil || p.Bootstrap.Upgrade == nil {
		return nil
	}
	proof := p.Bootstrap.Upgrade
	beforeAlias := annotationFor(d.Spec.Template.Annotations, upgradeAliasAnnotation)
	if beforeAlias == nil {
		return ErrUnsupported
	}
	before, ok := k.target.UpgradeSources[*beforeAlias]
	if !ok {
		return ErrUnsupported
	}
	c := container(d, k.target.Container)
	previous := before.Environment()
	for i, next := range k.target.UpgradeSources[proof.Alias].Environment() {
		delta := envDelta{Name: next.Name, After: next}
		for _, env := range c.Env {
			if env.Name == next.Name {
				if delta.Before != nil || digest(env) != digest(previous[i]) {
					return ErrConflict
				}
				delta.Before = env.DeepCopy()
			}
		}
		if delta.Before == nil {
			return ErrUnsupported
		}
		p.Delta.Environment = append(p.Delta.Environment, delta)
	}
	p.Delta.SourceAliases = append(p.Delta.SourceAliases, sourceAliasDelta{Name: upgradeAliasAnnotation, Before: beforeAlias, After: proof.Alias}, sourceAliasDelta{Name: upgradeProofAnnotation, Before: annotationFor(d.Spec.Template.Annotations, upgradeProofAnnotation), After: proof.ProofDigest})
	return nil
}

func (k *Kubernetes) validUpgradeDelta(p planData, offset int) bool {
	if p.Bootstrap.Upgrade == nil {
		return len(p.Delta.Environment) == offset
	}
	proof := p.Bootstrap.Upgrade
	expected := k.target.UpgradeSources[proof.Alias].Environment()
	if len(p.Delta.Environment) != offset+len(expected) {
		return false
	}
	var prior *UpgradeCustodySource
	for _, alias := range p.Delta.SourceAliases {
		if alias.Name == upgradeAliasAnnotation && alias.Before != nil {
			source, ok := k.target.UpgradeSources[*alias.Before]
			if ok {
				prior = &source
			}
		}
	}
	if prior == nil {
		return false
	}
	before := prior.Environment()
	for i, env := range expected {
		delta := p.Delta.Environment[offset+i]
		if delta.Name != env.Name || delta.Before == nil || digest(*delta.Before) != digest(before[i]) || digest(delta.After) != digest(env) {
			return false
		}
	}
	return true
}
