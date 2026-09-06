package configrollout

import (
	"context"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// SecretSource names an operator-installed immutable source. The executor
// neither reads its contents nor accepts a source name/key from a command.
type SecretSource struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// SourceProof is the digest of the coordinator's durable application proof.
// Database proof must demonstrate a fresh challenge written through the current
// connection and read through the candidate, not merely a cloneable instance ID.
// Root proof must record successful existing dual-wrapper prepare at RootEpoch.
// The signed command binds this proof to Intent and the installed source alias.
type SourceProof struct {
	Alias        string `json:"alias"`
	SourceDigest string `json:"source_digest"`
	ProofDigest  string `json:"proof_digest"`
	RootEpoch    int64  `json:"root_epoch,omitempty"`
}

type BootstrapChanges struct {
	Database *SourceProof `json:"database,omitempty"`
	Root     *SourceProof `json:"root,omitempty"`
}

type rootSourceDelta struct {
	Index  int                        `json:"index"`
	Before *corev1.SecretVolumeSource `json:"before"`
	After  *corev1.SecretVolumeSource `json:"after"`
}

func SourceDigest(source SecretSource) string { return digest(source) }

const databaseAliasAnnotation = "hikyo.dev/configuration-database-source"
const rootAliasAnnotation = "hikyo.dev/configuration-root-source"

type sourceAliasDelta struct {
	Name   string  `json:"name"`
	Before *string `json:"before,omitempty"`
	After  string  `json:"after"`
}

func annotationFor(values map[string]string, key string) *string {
	value, ok := values[key]
	if !ok {
		return nil
	}
	return &value
}

func validSecretSources(sources map[string]SecretSource) bool {
	for alias, source := range sources {
		if !safeName(alias) || len(validation.IsDNS1123Subdomain(source.Name)) != 0 || len(validation.IsConfigMapKey(source.Key)) != 0 {
			return false
		}
	}
	return true
}

func (k *Kubernetes) validBootstrap(b *BootstrapChanges) bool {
	if b == nil {
		return true
	}
	if b.Database == nil && b.Root == nil {
		return false
	}
	for _, item := range []struct {
		proof   *SourceProof
		sources map[string]SecretSource
		root    bool
	}{{b.Database, k.target.DatabaseSources, false}, {b.Root, k.target.RootSources, true}} {
		if item.proof == nil {
			continue
		}
		p := item.proof
		source, ok := item.sources[p.Alias]
		if !ok || !validDigest(p.ProofDigest) || p.SourceDigest != SourceDigest(source) || item.root && p.RootEpoch < 2 || !item.root && p.RootEpoch != 0 {
			return false
		}
	}
	return true
}

// PrepareBootstrap accepts only application-proven installed source aliases.
// Callers must persist and authorize the actual proof before signing submit;
// a nonempty digest by itself is not verification of a datastore or root key.
func (k *Kubernetes) PrepareBootstrap(ctx context.Context, intent Intent, changes []Change, bootstrap BootstrapChanges) (*Plan, error) {
	if !k.validBootstrap(&bootstrap) {
		return nil, ErrInvalid
	}
	copy := bootstrap
	if copy.Database != nil {
		value := *copy.Database
		copy.Database = &value
	}
	if copy.Root != nil {
		value := *copy.Root
		copy.Root = &value
	}
	return k.prepare(ctx, intent, changes, &copy)
}

func databaseEnv(source SecretSource) corev1.EnvVar {
	return corev1.EnvVar{Name: "HIKYO_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: source.Name}, Key: source.Key}}}
}

func (k *Kubernetes) prepareBootstrapDelta(d *appsv1.Deployment, p *planData) error {
	b := p.Bootstrap
	if b == nil {
		return nil
	}
	if !k.validBootstrap(b) {
		return ErrInvalid
	}
	c := container(d, k.target.Container)
	if b.Database != nil {
		delta := envDelta{Name: "HIKYO_DB", After: databaseEnv(k.target.DatabaseSources[b.Database.Alias])}
		p.Delta.SourceAliases = append(p.Delta.SourceAliases, sourceAliasDelta{Name: databaseAliasAnnotation, Before: annotationFor(d.Spec.Template.Annotations, databaseAliasAnnotation), After: b.Database.Alias})
		for _, env := range c.Env {
			if env.Name == delta.Name {
				if delta.Before != nil {
					return ErrUnsupported
				}
				delta.Before = env.DeepCopy()
			}
		}
		if delta.Before == nil || delta.Before.ValueFrom == nil || delta.Before.ValueFrom.SecretKeyRef == nil {
			return ErrUnsupported
		}
		p.Delta.Environment = append(p.Delta.Environment, delta)
	}
	if b.Root != nil {
		source := k.target.RootSources[b.Root.Alias]
		p.Delta.SourceAliases = append(p.Delta.SourceAliases, sourceAliasDelta{Name: rootAliasAnnotation, Before: annotationFor(d.Spec.Template.Annotations, rootAliasAnnotation), After: b.Root.Alias})
		for i, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name != "root-key-source" {
				continue
			}
			if p.Delta.RootSource != nil || volume.Secret == nil || len(volume.Secret.Items) != 1 || volume.Secret.Items[0].Path != "root-key" {
				return ErrUnsupported
			}
			after := volume.Secret.DeepCopy()
			after.SecretName = source.Name
			after.Items[0].Key = source.Key
			p.Delta.RootSource = &rootSourceDelta{Index: i, Before: volume.Secret.DeepCopy(), After: after}
		}
		if p.Delta.RootSource == nil {
			return ErrUnsupported
		}
		stager := false
		for _, init := range d.Spec.Template.Spec.InitContainers {
			if init.Name == "root-key-stage" && len(init.Args) == 1 && init.Args[0] == "__hikyo-stage-root-key" {
				for _, mount := range init.VolumeMounts {
					if mount.Name == "root-key-source" && mount.MountPath == "/run/hikyo-root-key-source" && mount.ReadOnly {
						stager = true
					}
				}
			}
		}
		if !stager {
			return ErrUnsupported
		}
	}
	return nil
}

func (k *Kubernetes) validBootstrapDelta(p planData) bool {
	if !k.validBootstrap(p.Bootstrap) {
		return false
	}
	if p.Bootstrap == nil {
		return p.Delta.RootSource == nil && len(p.Delta.Environment) == len(p.Changes) && len(p.Delta.SourceAliases) == 0
	}
	b := p.Bootstrap
	aliases := map[string]string{}
	if b.Database != nil {
		aliases[databaseAliasAnnotation] = b.Database.Alias
	}
	if b.Root != nil {
		aliases[rootAliasAnnotation] = b.Root.Alias
	}
	if len(p.Delta.SourceAliases) != len(aliases) {
		return false
	}
	for _, alias := range p.Delta.SourceAliases {
		if aliases[alias.Name] != alias.After || alias.After == "" {
			return false
		}
		delete(aliases, alias.Name)
	}
	want := len(p.Changes)
	if b.Database != nil {
		want++
		if len(p.Delta.Environment) != want {
			return false
		}
		e := p.Delta.Environment[want-1]
		if e.Name != "HIKYO_DB" || e.Before == nil || e.Before.Name != e.Name || digest(e.After) != digest(databaseEnv(k.target.DatabaseSources[b.Database.Alias])) {
			return false
		}
	}
	if len(p.Delta.Environment) != want {
		return false
	}
	if b.Root == nil {
		return p.Delta.RootSource == nil
	}
	r := p.Delta.RootSource
	if r == nil || r.Index < 0 || r.Before == nil || r.After == nil || len(r.Before.Items) != 1 || r.Before.Items[0].Path != "root-key" {
		return false
	}
	source := k.target.RootSources[b.Root.Alias]
	after := r.Before.DeepCopy()
	after.SecretName = source.Name
	after.Items[0].Key = source.Key
	return digest(after) == digest(r.After)
}

// cloneEnrollment detaches installation maps from caller mutation.
func cloneEnrollment(e Enrollment) Enrollment {
	e.Target.DatabaseSources = maps.Clone(e.Target.DatabaseSources)
	e.Target.RootSources = maps.Clone(e.Target.RootSources)
	e.Target.Sources = maps.Clone(e.Target.Sources)
	for key, aliases := range e.Target.Sources {
		e.Target.Sources[key] = maps.Clone(aliases)
	}
	return e
}
