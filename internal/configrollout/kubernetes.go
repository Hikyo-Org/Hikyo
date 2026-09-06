// Package configrollout executes a committed configuration decision against one
// installation-owned deployment. It never obtains Hikyo project credentials.
// The installed coordinator must serialize Submit, Observe and Restore for a
// target, including across executor replicas. Kubernetes resource-version CAS
// detects concurrent writes but cannot transact across Secrets and Deployments.
package configrollout

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

var (
	ErrInvalid      = errors.New("config rollout: invalid decision")
	ErrConflict     = errors.New("config rollout: target changed or another decision is active")
	ErrUnavailable  = errors.New("config rollout: deployment API unavailable")
	ErrUnsupported  = errors.New("config rollout: target requires a dedicated deployment protocol")
	ErrNotSubmitted = errors.New("config rollout: decision has not been submitted")
)

const (
	stampAnnotation = "hikyo.dev/configuration-rollout"
	requestKey      = "request.json"
	rollbackKey     = "rollback.json"
	receiptKey      = "receipt.json"
	maxRecordBytes  = 512 << 10
)

// Variable is the closed deployment-safe vocabulary. Root keys, database
// locators, upgrade controls and development-mode escape hatches are absent.
type Variable string

const (
	AdmissionBudget    Variable = "HIKYO_ADMISSION_BUDGET_MIB"
	PostgresPoolMax    Variable = "HIKYO_PG_POOL_MAX"
	Listen             Variable = "HIKYO_LISTEN"
	OperationalListen  Variable = "HIKYO_OPERATIONAL_LISTEN"
	HA                 Variable = "HIKYO_HA"
	NodeID             Variable = "HIKYO_NODE_ID"
	BackupDirectory    Variable = "HIKYO_BACKUP_DIR"
	AdapterPolicyFile  Variable = "HIKYO_ADAPTER_EGRESS_POLICY_FILE"
	DynamicPolicyFile  Variable = "HIKYO_DYNAMIC_EGRESS_POLICY_FILE"
	OIDCPolicyFile     Variable = "HIKYO_OIDC_EGRESS_POLICY_FILE"
	TLSCertificateFile Variable = "HIKYO_TLS_CERT_FILE"
	TLSKeyFile         Variable = "HIKYO_TLS_KEY_FILE"
)

// Change.Value is a scalar, except file/directory variables whose value is an
// installed source alias. Source contents never enter this protocol.
type Change struct {
	Variable Variable `json:"variable"`
	Value    string   `json:"value"`
}

// Target is installation custody, never request input. Every Secret is
// precreated and dedicated to this executor. ResourceNames RBAC must limit its
// client, and admission policy must restrict allowed Deployment field changes.
type Target struct {
	Namespace       string                         `json:"namespace"`
	Deployment      string                         `json:"deployment"`
	DeploymentUID   types.UID                      `json:"deployment_uid"`
	Container       string                         `json:"container"`
	ConfigSecret    string                         `json:"config_secret"`
	RollbackSecret  string                         `json:"rollback_secret"`
	RequestSecret   string                         `json:"request_secret"`
	ReceiptSecret   string                         `json:"receipt_secret"`
	Sources         map[Variable]map[string]string `json:"sources"`
	DatabaseSources map[string]SecretSource        `json:"database_sources"`
	RootSources     map[string]SecretSource        `json:"root_sources"`
	StableNodeID    string                         `json:"stable_node_id"`
}

// Intent is copied from the durably committed, exact-MFA SelfConfigJob.
// The executor does not establish human authorization; only the app-owned
// coordinator may submit the committed job through its installed control path.
type Intent struct {
	JobID              string `json:"job_id"`
	OwnerInstanceID    string `json:"owner_instance_id"`
	Incarnation        string `json:"incarnation"`
	SnapshotID         string `json:"snapshot_id"`
	Revision           int64  `json:"revision"`
	CatalogueVersion   int    `json:"catalogue_version"`
	ExpectedGeneration int64  `json:"expected_generation"`
	Generation         int64  `json:"generation"`
}

type ResourceVersion struct {
	UID     types.UID `json:"uid"`
	Version string    `json:"version"`
}
type ResourceVersions struct {
	Deployment ResourceVersion `json:"deployment"`
	Config     ResourceVersion `json:"config"`
	Rollback   ResourceVersion `json:"rollback"`
	Request    ResourceVersion `json:"request"`
	Receipt    ResourceVersion `json:"receipt"`
}

type envDelta struct {
	Name   string         `json:"name"`
	Before *corev1.EnvVar `json:"before,omitempty"`
	After  corev1.EnvVar  `json:"after"`
}
type argDelta struct {
	Flag   string `json:"flag"`
	Index  int    `json:"index"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type portDelta struct {
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Before int32  `json:"before"`
	After  int32  `json:"after"`
}
type deploymentDelta struct {
	Environment   []envDelta         `json:"environment"`
	Arguments     []argDelta         `json:"arguments"`
	Ports         []portDelta        `json:"ports"`
	BeforeStamp   *string            `json:"before_stamp,omitempty"`
	RootSource    *rootSourceDelta   `json:"root_source,omitempty"`
	SourceAliases []sourceAliasDelta `json:"source_aliases,omitempty"`
}
type planData struct {
	Intent               Intent            `json:"intent"`
	TargetDigest         string            `json:"target_digest"`
	Resources            ResourceVersions  `json:"resources"`
	Changes              []Change          `json:"changes"`
	ConfigBefore         map[string][]byte `json:"config_before"`
	ConfigAfter          map[string][]byte `json:"config_after"`
	ConfigBeforeStamp    *string           `json:"config_before_stamp,omitempty"`
	Delta                deploymentDelta   `json:"delta"`
	BeforeMetadataDigest string            `json:"before_metadata_digest"`
	BeforeSpecDigest     string            `json:"before_spec_digest"`
	AfterSpecDigest      string            `json:"after_spec_digest"`
	Replicas             int32             `json:"replicas"`
	Bootstrap            *BootstrapChanges `json:"bootstrap,omitempty"`
}

// Plan is opaque to callers. Its digest binds the decision, target, prior
// resources and exact limited mutation. Prepare performs no writes.
type Plan struct {
	data   planData
	digest string
}

func (p *Plan) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}
func (p *Plan) Resources() ResourceVersions {
	if p == nil {
		return ResourceVersions{}
	}
	return p.data.Resources
}

type Phase string

func (p *Plan) TemplateStamp() string {
	if p == nil {
		return ""
	}
	return stampFor(p.data)
}

const (
	Prepared         Phase = "prepared"
	ConfigWritten    Phase = "config-written"
	RolloutRequested Phase = "rollout-requested"
	RolloutReady     Phase = "rollout-ready"
	Applied          Phase = "applied"
	Restoring        Phase = "restoring"
	Restored         Phase = "restored"
)

// Receipt contains metadata only. RolloutReady does not imply application
// acknowledgement, and Restored refers only to external deployment inputs.
type Receipt struct {
	Intent                  Intent           `json:"intent"`
	PlanDigest              string           `json:"plan_digest"`
	DeploymentUID           types.UID        `json:"deployment_uid"`
	Phase                   Phase            `json:"phase"`
	Resources               ResourceVersions `json:"resources"`
	ApplicationAcknowledged bool             `json:"application_acknowledged"`
}

// ApplicationAcknowledgement must come from the coordinator's verified node
// acknowledgements, not an HTTP 200 or Deployment availability alone.
type ApplicationAcknowledgement struct {
	Intent        Intent
	PlanDigest    string
	DeploymentUID types.UID
	ReadyReplicas int32
}

type record struct {
	Digest string   `json:"digest"`
	Plan   planData `json:"plan"`
}
type rollbackRecord struct {
	Digest string            `json:"digest"`
	Intent Intent            `json:"intent"`
	Config map[string][]byte `json:"config"`
	Delta  deploymentDelta   `json:"delta"`
}

type Kubernetes struct {
	client       kubernetes.Interface
	target       Target
	targetDigest string
}

func NewKubernetes(client kubernetes.Interface, target Target) (*Kubernetes, error) {
	if client == nil || target.DeploymentUID == "" || !safeName(target.StableNodeID) || len(validation.IsDNS1123Label(target.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(target.Deployment)) != 0 || len(validation.IsDNS1123Label(target.Container)) != 0 {
		return nil, ErrInvalid
	}
	names := []string{target.ConfigSecret, target.RollbackSecret, target.RequestSecret, target.ReceiptSecret}
	seen := map[string]bool{}
	for _, name := range names {
		if len(validation.IsDNS1123Subdomain(name)) != 0 || seen[name] {
			return nil, ErrInvalid
		}
		seen[name] = true
	}
	sources := make(map[Variable]map[string]string, len(target.Sources))
	for key, aliases := range target.Sources {
		if !sourceVariable(key) {
			return nil, ErrInvalid
		}
		sources[key] = maps.Clone(aliases)
		for alias, path := range aliases {
			if !safeName(alias) || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
				return nil, ErrInvalid
			}
		}
	}
	target.Sources = sources
	if !validSecretSources(target.DatabaseSources) || !validSecretSources(target.RootSources) {
		return nil, ErrInvalid
	}
	target.DatabaseSources = maps.Clone(target.DatabaseSources)
	target.RootSources = maps.Clone(target.RootSources)
	return &Kubernetes{client: client, target: target, targetDigest: digest(target)}, nil
}

func validIntent(i Intent) bool {
	return safeName(i.JobID) && safeName(i.OwnerInstanceID) && safeName(i.Incarnation) && safeName(i.SnapshotID) && i.Revision > 0 && i.CatalogueVersion > 0 && i.ExpectedGeneration > 0 && i.Generation > i.ExpectedGeneration && i.Generation-i.ExpectedGeneration == 1
}
func safeName(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func sourceVariable(v Variable) bool {
	switch v {
	case BackupDirectory, AdapterPolicyFile, DynamicPolicyFile, OIDCPolicyFile, TLSCertificateFile, TLSKeyFile:
		return true
	}
	return false
}
func allowed(v Variable) bool {
	switch v {
	case AdmissionBudget, PostgresPoolMax, Listen, OperationalListen, HA, NodeID:
		return true
	}
	return sourceVariable(v)
}
func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func version(obj metav1.Object) ResourceVersion {
	return ResourceVersion{UID: obj.GetUID(), Version: obj.GetResourceVersion()}
}
func annotation(a map[string]string) *string {
	v, ok := a[stampAnnotation]
	if !ok {
		return nil
	}
	return &v
}
func setStamp(a map[string]string, value *string) map[string]string {
	if a == nil {
		a = map[string]string{}
	}
	if value == nil {
		delete(a, stampAnnotation)
	} else {
		a[stampAnnotation] = *value
	}
	return a
}
func apiError(err error) error {
	if apierrors.IsConflict(err) || apierrors.IsNotFound(err) || apierrors.IsAlreadyExists(err) {
		return ErrConflict
	}
	return ErrUnavailable
}

func (k *Kubernetes) get(ctx context.Context) (*appsv1.Deployment, map[string]*corev1.Secret, error) {
	d, err := k.client.AppsV1().Deployments(k.target.Namespace).Get(ctx, k.target.Deployment, metav1.GetOptions{})
	if err != nil {
		return nil, nil, apiError(err)
	}
	if d.UID != k.target.DeploymentUID {
		return nil, nil, ErrConflict
	}
	if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		return nil, nil, ErrUnsupported
	}
	if container(d, k.target.Container) == nil {
		return nil, nil, ErrUnsupported
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas != 1 {
		return nil, nil, ErrUnsupported
	}
	stable := false
	for _, env := range container(d, k.target.Container).Env {
		if env.Name == string(NodeID) {
			if stable || env.Value != k.target.StableNodeID || env.ValueFrom != nil {
				return nil, nil, ErrUnsupported
			}
			stable = true
		}
	}
	if !stable {
		return nil, nil, ErrUnsupported
	}
	secrets := map[string]*corev1.Secret{}
	for _, name := range []string{k.target.ConfigSecret, k.target.RollbackSecret, k.target.RequestSecret, k.target.ReceiptSecret} {
		s, err := k.client.CoreV1().Secrets(k.target.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, apiError(err)
		}
		if s.UID == "" || s.ResourceVersion == "" || s.Immutable != nil && *s.Immutable {
			return nil, nil, ErrUnsupported
		}
		secrets[name] = s
	}
	return d, secrets, nil
}
func container(d *appsv1.Deployment, name string) *corev1.Container {
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == name {
			return &d.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}
func (k *Kubernetes) resources(d *appsv1.Deployment, s map[string]*corev1.Secret) ResourceVersions {
	return ResourceVersions{Deployment: version(d), Config: version(s[k.target.ConfigSecret]), Rollback: version(s[k.target.RollbackSecret]), Request: version(s[k.target.RequestSecret]), Receipt: version(s[k.target.ReceiptSecret])}
}

func (k *Kubernetes) Prepare(ctx context.Context, intent Intent, changes []Change) (*Plan, error) {
	return k.prepare(ctx, intent, changes, nil)
}

func (k *Kubernetes) prepare(ctx context.Context, intent Intent, changes []Change, bootstrap *BootstrapChanges) (*Plan, error) {
	if !validIntent(intent) || len(changes) == 0 && bootstrap == nil || len(changes) > 32 {
		return nil, ErrInvalid
	}
	d, secrets, err := k.get(ctx)
	if err != nil {
		return nil, err
	}
	for key := range secrets[k.target.ConfigSecret].Data {
		if !allowed(Variable(key)) {
			return nil, ErrUnsupported
		}
	}
	changes = slices.Clone(changes)
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(string(a.Variable), string(b.Variable)) })
	p := planData{Intent: intent, TargetDigest: k.targetDigest, Resources: k.resources(d, secrets), Changes: changes, ConfigBefore: cloneData(secrets[k.target.ConfigSecret].Data), ConfigAfter: cloneData(secrets[k.target.ConfigSecret].Data), ConfigBeforeStamp: annotation(secrets[k.target.ConfigSecret].Annotations), BeforeMetadataDigest: deploymentMetadataDigest(d), BeforeSpecDigest: digest(d.Spec), Replicas: 1}
	if d.Spec.Replicas != nil {
		p.Replicas = *d.Spec.Replicas
	}
	if p.Replicas < 1 {
		return nil, ErrUnsupported
	}
	p.Delta.BeforeStamp = annotation(d.Spec.Template.Annotations)
	p.Bootstrap = bootstrap
	c := container(d, k.target.Container)
	for index, change := range changes {
		if index > 0 && changes[index-1].Variable == change.Variable {
			return nil, ErrInvalid
		}
		value, err := k.resolve(change, p.Replicas)
		if err != nil {
			return nil, err
		}
		p.ConfigAfter[string(change.Variable)] = []byte(value)
		delta := envDelta{Name: string(change.Variable), After: corev1.EnvVar{Name: string(change.Variable), ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: k.target.ConfigSecret}, Key: string(change.Variable)}}}}
		for _, env := range c.Env {
			if env.Name == delta.Name {
				if delta.Before != nil {
					return nil, ErrUnsupported
				}
				delta.Before = env.DeepCopy()
			}
		}
		p.Delta.Environment = append(p.Delta.Environment, delta)
		if flag := variableFlag(change.Variable); flag != "" {
			if err := argumentDelta(c, flag, value, &p.Delta); err != nil {
				return nil, err
			}
		}
		if change.Variable == Listen || change.Variable == OperationalListen {
			if err := listenerDelta(c, change.Variable, value, &p.Delta); err != nil {
				return nil, err
			}
		}
	}
	if err := k.prepareBootstrapDelta(d, &p); err != nil {
		return nil, err
	}
	// Derive a stable stamp from the exact decision and changes, then include
	// that target spec in the complete plan digest (no circular hash).
	stamp := stampFor(p)
	if err := applyDelta(d, k.target.Container, p.Delta, false, &stamp); err != nil {
		return nil, err
	}
	p.AfterSpecDigest = digest(d.Spec)
	plan := &Plan{data: p, digest: digest(p)}
	if len(mustJSON(record{Digest: plan.digest, Plan: p})) > maxRecordBytes {
		return nil, ErrInvalid
	}
	return plan, nil
}

func (k *Kubernetes) resolve(change Change, replicas int32) (string, error) {
	if !allowed(change.Variable) || len(change.Value) > 4096 || strings.ContainsAny(change.Value, "\x00\r\n") {
		return "", ErrInvalid
	}
	if sourceVariable(change.Variable) {
		path, ok := k.target.Sources[change.Variable][change.Value]
		if !ok {
			return "", ErrInvalid
		}
		return path, nil
	}
	switch change.Variable {
	case AdmissionBudget, PostgresPoolMax:
		n, err := strconv.ParseUint(change.Value, 10, 31)
		if err != nil || n == 0 || change.Variable == AdmissionBudget && n <= 16 {
			return "", ErrInvalid
		}
	case Listen, OperationalListen:
		host, port, err := net.SplitHostPort(change.Value)
		if err != nil {
			return "", ErrInvalid
		}
		if _, err := netip.ParseAddr(host); err != nil {
			return "", ErrInvalid
		}
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", ErrInvalid
		}
	case HA:
		if change.Value != "true" && change.Value != "false" {
			return "", ErrInvalid
		}
		if change.Value == "false" && replicas > 1 {
			return "", ErrUnsupported
		}
	case NodeID:
		return "", ErrUnsupported
	}
	return change.Value, nil
}

func variableFlag(v Variable) string {
	switch v {
	case Listen:
		return "--listen"
	case OperationalListen:
		return "--operational-listen"
	case TLSCertificateFile:
		return "--tls-cert-file"
	case TLSKeyFile:
		return "--tls-key-file"
	}
	return ""
}

func argumentDelta(c *corev1.Container, flag, value string, delta *deploymentDelta) error {
	found := false
	for i, arg := range c.Args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			if found {
				return ErrUnsupported
			}
			found = true
			if arg == flag {
				if i+1 >= len(c.Args) || strings.HasPrefix(c.Args[i+1], "-") {
					return ErrUnsupported
				}
				delta.Arguments = append(delta.Arguments, argDelta{Flag: flag, Index: i + 1, Before: c.Args[i+1], After: value})
			} else {
				delta.Arguments = append(delta.Arguments, argDelta{Flag: flag, Index: i, Before: arg, After: flag + "=" + value})
			}
		}
	}
	return nil
}

func listenerDelta(c *corev1.Container, v Variable, value string, delta *deploymentDelta) error {
	name := "http"
	if v == OperationalListen {
		name = "ops"
	}
	_, port, _ := net.SplitHostPort(value)
	n, _ := strconv.ParseInt(port, 10, 32)
	portFound := false
	for i, p := range c.Ports {
		if p.Name == name {
			if portFound {
				return ErrUnsupported
			}
			portFound = true
			delta.Ports = append(delta.Ports, portDelta{Name: name, Index: i, Before: p.ContainerPort, After: int32(n)})
		}
	}
	if !portFound {
		return ErrUnsupported
	}
	if v == OperationalListen {
		for _, probe := range []*corev1.Probe{c.ReadinessProbe, c.LivenessProbe, c.StartupProbe} {
			if probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Port.StrVal != "ops" {
				return ErrUnsupported
			}
		}
	}
	return nil
}

func applyDelta(d *appsv1.Deployment, name string, delta deploymentDelta, reverse bool, stamp *string) error {
	c := container(d, name)
	if c == nil {
		return ErrConflict
	}
	for _, e := range delta.Environment {
		found := -1
		for i := range c.Env {
			if c.Env[i].Name == e.Name {
				found = i
				break
			}
		}
		if reverse {
			if found < 0 || digest(c.Env[found]) != digest(e.After) {
				return ErrConflict
			}
			if e.Before == nil {
				if found >= 0 {
					c.Env = slices.Delete(c.Env, found, found+1)
				}
			} else {
				if found < 0 {
					return ErrConflict
				}
				c.Env[found] = *e.Before.DeepCopy()
			}
		} else {
			if found < 0 && e.Before != nil || found >= 0 && (e.Before == nil || digest(c.Env[found]) != digest(e.Before)) {
				return ErrConflict
			}
			if found < 0 {
				c.Env = append(c.Env, *e.After.DeepCopy())
			} else {
				c.Env[found] = *e.After.DeepCopy()
			}
		}
	}
	for _, a := range delta.Arguments {
		if a.Index < 0 || a.Index >= len(c.Args) {
			return ErrConflict
		}
		if a.Flag != "--listen" && a.Flag != "--operational-listen" && a.Flag != "--tls-cert-file" && a.Flag != "--tls-key-file" {
			return ErrConflict
		}
		if !strings.HasPrefix(c.Args[a.Index], a.Flag+"=") && (a.Index == 0 || c.Args[a.Index-1] != a.Flag) {
			return ErrConflict
		}
		if reverse {
			if c.Args[a.Index] != a.After {
				return ErrConflict
			}
			c.Args[a.Index] = a.Before
		} else {
			if c.Args[a.Index] != a.Before {
				return ErrConflict
			}
			c.Args[a.Index] = a.After
		}
	}
	for _, p := range delta.Ports {
		if p.Index < 0 || p.Index >= len(c.Ports) {
			return ErrConflict
		}
		if p.Name != "http" && p.Name != "ops" || c.Ports[p.Index].Name != p.Name {
			return ErrConflict
		}
		if reverse {
			if c.Ports[p.Index].ContainerPort != p.After {
				return ErrConflict
			}
			c.Ports[p.Index].ContainerPort = p.Before
		} else {
			if c.Ports[p.Index].ContainerPort != p.Before {
				return ErrConflict
			}
			c.Ports[p.Index].ContainerPort = p.After
		}
	}
	if delta.RootSource != nil {
		r := delta.RootSource
		if r.Index < 0 || r.Index >= len(d.Spec.Template.Spec.Volumes) || d.Spec.Template.Spec.Volumes[r.Index].Name != "root-key-source" {
			return ErrConflict
		}
		volume := &d.Spec.Template.Spec.Volumes[r.Index]
		before, after := r.Before, r.After
		if reverse {
			before, after = after, before
		}
		if digest(volume.Secret) != digest(before) {
			return ErrConflict
		}
		volume.Secret = after.DeepCopy()
	}
	if reverse {
		d.Spec.Template.Annotations = setStamp(d.Spec.Template.Annotations, delta.BeforeStamp)
	} else {
		d.Spec.Template.Annotations = setStamp(d.Spec.Template.Annotations, stamp)
	}
	for _, alias := range delta.SourceAliases {
		if alias.Name != databaseAliasAnnotation && alias.Name != rootAliasAnnotation {
			return ErrConflict
		}
		before, after := alias.Before, &alias.After
		if reverse {
			before, after = after, before
		}
		if digest(annotationFor(d.Spec.Template.Annotations, alias.Name)) != digest(before) {
			return ErrConflict
		}
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = map[string]string{}
		}
		if after == nil {
			delete(d.Spec.Template.Annotations, alias.Name)
		} else {
			d.Spec.Template.Annotations[alias.Name] = *after
		}
	}
	return nil
}

func cloneData(data map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for key, value := range data {
		out[key] = slices.Clone(value)
	}
	return out
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxRecordBytes {
		return ErrConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ErrConflict
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return ErrConflict
	}
	return nil
}
func sameData(a, b map[string][]byte) bool { return maps.EqualFunc(a, b, bytes.Equal) }
func stampFor(p planData) string {
	return digest(struct {
		Intent    Intent
		Changes   []Change
		Target    string
		Bootstrap *BootstrapChanges
	}{p.Intent, p.Changes, p.TargetDigest, p.Bootstrap})
}

func (k *Kubernetes) updateSecret(ctx context.Context, s *corev1.Secret) (*corev1.Secret, error) {
	out, err := k.client.CoreV1().Secrets(k.target.Namespace).Update(ctx, s, metav1.UpdateOptions{})
	if err != nil {
		return nil, apiError(err)
	}
	return out, nil
}
func putRecord(s *corev1.Secret, key string, v any) {
	if s.Data == nil {
		s.Data = map[string][]byte{}
	}
	s.Data[key] = mustJSON(v)
}
func (k *Kubernetes) saveReceipt(ctx context.Context, s *corev1.Secret, receipt Receipt) (Receipt, error) {
	putRecord(s, receiptKey, receipt)
	if _, err := k.updateSecret(ctx, s); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (k *Kubernetes) loadRecord(s map[string]*corev1.Secret, intent Intent) (record, error) {
	var r record
	if err := decode(s[k.target.RequestSecret].Data[requestKey], &r); err != nil {
		return r, ErrNotSubmitted
	}
	if r.Plan.Intent != intent || r.Plan.TargetDigest != k.targetDigest || r.Digest != digest(r.Plan) || !k.validPlan(r.Plan) {
		return record{}, ErrConflict
	}
	return r, nil
}
func compatibleUIDs(p planData, d *appsv1.Deployment, s map[string]*corev1.Secret, t Target) bool {
	return d.UID == p.Resources.Deployment.UID && s[t.ConfigSecret].UID == p.Resources.Config.UID && s[t.RollbackSecret].UID == p.Resources.Rollback.UID && s[t.RequestSecret].UID == p.Resources.Request.UID && s[t.ReceiptSecret].UID == p.Resources.Receipt.UID
}

// deploymentMetadataDigest pins ownership, labels, annotations (including the
// observed custody stamp), finalizers and deletion state. ResourceVersion and
// managed-field bookkeeping can advance when Kubernetes updates Status; neither
// changes the deployment inputs reviewed by the administrator. The guarded
// client stamps its verified current custody only on the eventual atomic PUT.
func deploymentMetadataDigest(deployment *appsv1.Deployment) string {
	metadata := deployment.ObjectMeta.DeepCopy()
	metadata.ResourceVersion = ""
	metadata.ManagedFields = nil
	return digest(metadata)
}

func (k *Kubernetes) preparedResourcesMatch(plan planData, deployment *appsv1.Deployment, secrets map[string]*corev1.Secret) bool {
	current := k.resources(deployment, secrets)
	if current.Deployment.UID != plan.Resources.Deployment.UID {
		return false
	}
	current.Deployment.Version = plan.Resources.Deployment.Version
	return current == plan.Resources && digest(deployment.Spec) == plan.BeforeSpecDigest && deploymentMetadataDigest(deployment) == plan.BeforeMetadataDigest
}

// Submit persists the request, rollback material and initial receipt before
// changing any deployment input. A nil plan resumes an already durable exact
// intent after a process restart or lost API reply. expectedPlanDigest must come
// from the coordinator's durable authorized decision, never the request Secret.
func (k *Kubernetes) Submit(ctx context.Context, intent Intent, expectedPlanDigest string, plan *Plan) (Receipt, error) {
	if !validIntent(intent) || !validDigest(expectedPlanDigest) {
		return Receipt{}, ErrInvalid
	}
	d, s, err := k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	r, existingErr := k.loadRecord(s, intent)
	if existingErr == nil && r.Digest != expectedPlanDigest {
		return Receipt{}, ErrConflict
	}
	if existingErr != nil {
		if plan == nil {
			return Receipt{}, existingErr
		}
		if plan.data.Intent != intent || plan.data.TargetDigest != k.targetDigest || plan.digest != digest(plan.data) || plan.digest != expectedPlanDigest {
			return Receipt{}, ErrInvalid
		}
		var previous Receipt
		if raw := s[k.target.ReceiptSecret].Data[receiptKey]; len(raw) > 0 {
			if decode(raw, &previous) != nil || previous.Phase != Applied && previous.Phase != Restored {
				return Receipt{}, ErrConflict
			}
		}
		if raw := s[k.target.RequestSecret].Data[requestKey]; len(raw) > 0 {
			var prior record
			if decode(raw, &prior) != nil || previous.Intent != prior.Plan.Intent || previous.PlanDigest != prior.Digest || previous.Phase != Applied && previous.Phase != Restored {
				return Receipt{}, ErrConflict
			}
		}
		if !k.preparedResourcesMatch(plan.data, d, s) {
			return Receipt{}, ErrConflict
		}
		r = record{Digest: plan.digest, Plan: plan.data}
		putRecord(s[k.target.RequestSecret], requestKey, r)
		updated, err := k.updateSecret(ctx, s[k.target.RequestSecret])
		if err != nil {
			return Receipt{}, err
		}
		s[k.target.RequestSecret] = updated
	} else if plan != nil && plan.digest != r.Digest {
		return Receipt{}, ErrConflict
	}
	if !compatibleUIDs(r.Plan, d, s, k.target) {
		return Receipt{}, ErrConflict
	}
	var receipt Receipt
	if raw := s[k.target.ReceiptSecret].Data[receiptKey]; len(raw) > 0 && decode(raw, &receipt) == nil && receipt.Intent == intent {
		if receipt.PlanDigest != r.Digest {
			return Receipt{}, ErrConflict
		}
		if receipt.Phase == Restoring || receipt.Phase == Restored {
			return receipt, ErrConflict
		}
		if receipt.Phase == Applied {
			return receipt, nil
		}
	} else {
		receipt = Receipt{Intent: intent, PlanDigest: r.Digest, DeploymentUID: d.UID, Phase: Prepared, Resources: r.Plan.Resources}
	}
	rollback := rollbackRecord{Digest: r.Digest, Intent: intent, Config: r.Plan.ConfigBefore, Delta: r.Plan.Delta}
	var saved rollbackRecord
	if decode(s[k.target.RollbackSecret].Data[rollbackKey], &saved) != nil || saved.Intent != intent {
		if version(s[k.target.RollbackSecret]) != r.Plan.Resources.Rollback {
			return Receipt{}, ErrConflict
		}
		putRecord(s[k.target.RollbackSecret], rollbackKey, rollback)
		updated, err := k.updateSecret(ctx, s[k.target.RollbackSecret])
		if err != nil {
			return Receipt{}, err
		}
		s[k.target.RollbackSecret] = updated
	} else if digest(saved) != digest(rollback) {
		return Receipt{}, ErrConflict
	}
	if receipt.Phase == Prepared {
		if _, err := k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt); err != nil {
			return Receipt{}, err
		}
		_, s, err = k.get(ctx)
		if err != nil {
			return Receipt{}, err
		}
	}
	if err := k.checkPending(s, intent, r.Digest); err != nil {
		return Receipt{}, err
	}
	stamp := stampFor(r.Plan)
	config := s[k.target.ConfigSecret]
	if !sameData(config.Data, r.Plan.ConfigAfter) || config.Annotations[stampAnnotation] != stamp {
		if !sameData(config.Data, r.Plan.ConfigBefore) || version(config) != r.Plan.Resources.Config {
			return Receipt{}, ErrConflict
		}
		config.Data = cloneData(r.Plan.ConfigAfter)
		config.Annotations = setStamp(config.Annotations, &stamp)
		updated, err := k.updateSecret(ctx, config)
		if err != nil {
			return Receipt{}, err
		}
		s[k.target.ConfigSecret] = updated
	}
	receipt.Phase = ConfigWritten
	receipt.Resources.Config = version(s[k.target.ConfigSecret])
	if _, err := k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt); err != nil {
		return Receipt{}, err
	}
	d, s, err = k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if !compatibleUIDs(r.Plan, d, s, k.target) {
		return Receipt{}, ErrConflict
	}
	if err := k.checkPending(s, intent, r.Digest); err != nil {
		return Receipt{}, err
	}
	switch digest(d.Spec) {
	case r.Plan.AfterSpecDigest:
	case r.Plan.BeforeSpecDigest:
		if deploymentMetadataDigest(d) != r.Plan.BeforeMetadataDigest {
			return Receipt{}, ErrConflict
		}
		if err := applyDelta(d, k.target.Container, r.Plan.Delta, false, &stamp); err != nil {
			return Receipt{}, err
		}
		if digest(d.Spec) != r.Plan.AfterSpecDigest {
			return Receipt{}, ErrConflict
		}
		updated, err := k.client.AppsV1().Deployments(k.target.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		if err != nil {
			return Receipt{}, apiError(err)
		}
		d = updated
	default:
		return Receipt{}, ErrConflict
	}
	receipt.Phase = RolloutRequested
	receipt.Resources.Deployment = version(d)
	return k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt)
}

func rolloutReady(d *appsv1.Deployment, replicas int32) bool {
	return d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == replicas && d.Status.UpdatedReplicas == replicas && d.Status.AvailableReplicas == replicas && d.Status.ReadyReplicas == replicas && d.Status.UnavailableReplicas == 0 && (d.Status.TerminatingReplicas == nil || *d.Status.TerminatingReplicas == 0)
}

func (k *Kubernetes) checkPending(secrets map[string]*corev1.Secret, intent Intent, wantDigest string) error {
	r, err := k.loadRecord(secrets, intent)
	if err != nil || r.Digest != wantDigest {
		return ErrConflict
	}
	var receipt Receipt
	if decode(secrets[k.target.ReceiptSecret].Data[receiptKey], &receipt) != nil || receipt.Intent != intent || receipt.PlanDigest != wantDigest {
		return ErrConflict
	}
	if receipt.Phase == Restoring || receipt.Phase == Restored || receipt.Phase == Applied {
		return ErrConflict
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

// Observe requires the plan digest pinned in the durable authorized decision.
func (k *Kubernetes) Observe(ctx context.Context, intent Intent, expectedPlanDigest string, ack *ApplicationAcknowledgement) (Receipt, error) {
	if !validDigest(expectedPlanDigest) {
		return Receipt{}, ErrInvalid
	}
	d, s, err := k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	r, err := k.loadRecord(s, intent)
	if err != nil {
		return Receipt{}, err
	}
	if r.Digest != expectedPlanDigest {
		return Receipt{}, ErrConflict
	}
	if !compatibleUIDs(r.Plan, d, s, k.target) {
		return Receipt{}, ErrConflict
	}
	var receipt Receipt
	if decode(s[k.target.ReceiptSecret].Data[receiptKey], &receipt) != nil || receipt.Intent != intent || receipt.PlanDigest != r.Digest {
		return Receipt{}, ErrNotSubmitted
	}
	if receipt.Phase == Restoring || receipt.Phase == Restored {
		return receipt, nil
	}
	if (receipt.Phase == Prepared || receipt.Phase == ConfigWritten) && digest(d.Spec) == r.Plan.BeforeSpecDigest && (sameData(s[k.target.ConfigSecret].Data, r.Plan.ConfigBefore) || sameData(s[k.target.ConfigSecret].Data, r.Plan.ConfigAfter)) {
		if ack != nil {
			return Receipt{}, ErrConflict
		}
		return receipt, nil
	}
	if digest(d.Spec) != r.Plan.AfterSpecDigest || !sameData(s[k.target.ConfigSecret].Data, r.Plan.ConfigAfter) || s[k.target.ConfigSecret].Annotations[stampAnnotation] != stampFor(r.Plan) {
		return Receipt{}, ErrConflict
	}
	if !rolloutReady(d, r.Plan.Replicas) {
		return receipt, nil
	}
	if receipt.Phase == Applied {
		return receipt, nil
	}
	receipt.Phase = RolloutReady
	if ack != nil {
		if ack.Intent != intent || ack.PlanDigest != r.Digest || ack.DeploymentUID != d.UID || ack.ReadyReplicas != r.Plan.Replicas {
			return Receipt{}, ErrConflict
		}
		receipt.Phase = Applied
		receipt.ApplicationAcknowledged = true
	}
	return k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt)
}

// Restore compensates only this decision's owned configuration changes. It
// does not change images/schema, retarget a database, or acknowledge a restored
// application generation. The coordinator owns that separate decision.
func (k *Kubernetes) Restore(ctx context.Context, intent Intent, expectedPlanDigest string) (Receipt, error) {
	if !validDigest(expectedPlanDigest) {
		return Receipt{}, ErrInvalid
	}
	d, s, err := k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	r, err := k.loadRecord(s, intent)
	if err != nil {
		return Receipt{}, err
	}
	if r.Digest != expectedPlanDigest {
		return Receipt{}, ErrConflict
	}
	if !compatibleUIDs(r.Plan, d, s, k.target) {
		return Receipt{}, ErrConflict
	}
	var rollback rollbackRecord
	if decode(s[k.target.RollbackSecret].Data[rollbackKey], &rollback) != nil || rollback.Digest != r.Digest || rollback.Intent != intent || !sameData(rollback.Config, r.Plan.ConfigBefore) || digest(rollback.Delta) != digest(r.Plan.Delta) {
		return Receipt{}, ErrConflict
	}
	var receipt Receipt
	if decode(s[k.target.ReceiptSecret].Data[receiptKey], &receipt) != nil || receipt.Intent != intent || receipt.PlanDigest != r.Digest {
		return Receipt{}, ErrNotSubmitted
	}
	if digest(d.Spec) != r.Plan.AfterSpecDigest && digest(d.Spec) != r.Plan.BeforeSpecDigest {
		return Receipt{}, ErrConflict
	}
	config := s[k.target.ConfigSecret]
	if !sameData(config.Data, r.Plan.ConfigAfter) && !sameData(config.Data, r.Plan.ConfigBefore) {
		return Receipt{}, ErrConflict
	}
	receipt.Phase = Restoring
	receipt.ApplicationAcknowledged = false
	if _, err := k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt); err != nil {
		return Receipt{}, err
	}
	if !sameData(config.Data, r.Plan.ConfigBefore) || digest(annotation(config.Annotations)) != digest(r.Plan.ConfigBeforeStamp) {
		config.Data = cloneData(r.Plan.ConfigBefore)
		config.Annotations = setStamp(config.Annotations, r.Plan.ConfigBeforeStamp)
		updated, err := k.updateSecret(ctx, config)
		if err != nil {
			return Receipt{}, err
		}
		s[k.target.ConfigSecret] = updated
	}
	if digest(d.Spec) != r.Plan.BeforeSpecDigest {
		if err := applyDelta(d, k.target.Container, r.Plan.Delta, true, nil); err != nil {
			return Receipt{}, err
		}
		if digest(d.Spec) != r.Plan.BeforeSpecDigest {
			return Receipt{}, ErrConflict
		}
		updated, err := k.client.AppsV1().Deployments(k.target.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		if err != nil {
			return Receipt{}, apiError(err)
		}
		d = updated
	}
	_, s, err = k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	receipt.Phase = Restored
	receipt.Resources.Config = version(s[k.target.ConfigSecret])
	receipt.Resources.Deployment = version(d)
	return k.saveReceipt(ctx, s[k.target.ReceiptSecret], receipt)
}
