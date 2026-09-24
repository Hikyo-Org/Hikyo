// Package deliverytarget owns the closed vocabulary, bounds and derived state
// of delivery-target condition reporting (k8s-condition-reporting ADR).
//
// It holds vocabulary and pure rules only. Authorization lives at the
// chokepoint, rows live in the store, and the service decides which rule a
// report meets. Every string a report may carry is decided here: a closed
// enum, the Kubernetes name grammar, a UID, or a SemVer version. The server
// never stores a string that this package did not accept.
package deliverytarget

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Vocabulary is the vocabulary version this server advertises (ADR D10). The
// server keeps accepting every version in Vocabularies.
const Vocabulary = 1

// Capability is the `/meta` protocol capability token. The vocabulary version
// follows the slash, one token per accepted version.
const Capability = "delivery-target-report"

// Vocabularies returns every vocabulary version this server accepts.
func Vocabularies() []int { return []int{1} }

// CapabilityTokens returns the `/meta` tokens this server advertises.
func CapabilityTokens() []string {
	out := make([]string, 0, len(Vocabularies()))
	for _, v := range Vocabularies() {
		out = append(out, fmt.Sprintf("%s/%d", Capability, v))
	}
	return out
}

// Bounds fixed by the ADR (D5, D6, D8) and the ops catalogue.
const (
	MaxReportBytes      = 8 * 1024
	MaxRowsPerPrincipal = 100
	MinReportInterval   = 5 * time.Minute
	MaxReportInterval   = 24 * time.Hour
	StaleGrace          = 5 * time.Minute
	MaxFutureSkew       = 5 * time.Minute
	PurgeAfter          = 30 * 24 * time.Hour
	PrincipalBudget     = 60
	OrgBudget           = 300
)

// Reporter is the closed integration enum.
const ReporterKubernetesOperator = "kubernetes-operator"

var reporters = map[string]bool{ReporterKubernetesOperator: true}

// IsReporter reports whether r is in the closed integration enum.
func IsReporter(r string) bool { return reporters[r] }

// Reporters returns the closed integration enum, sorted.
func Reporters() []string { return sortedKeys(reporters) }

var lifecycles = map[string]bool{
	"Synced": true, "Retained": true, "Scrubbed": true, "Refused": true, "Unreconciled": true,
}

// IsLifecycle reports whether l is in the closed `status.lifecycle` enum.
func IsLifecycle(l string) bool { return lifecycles[l] }

// Lifecycles returns the closed lifecycle enum, sorted.
func Lifecycles() []string { return sortedKeys(lifecycles) }

var conditionStatuses = map[string]bool{"True": true, "False": true, "Unknown": true}

// IsConditionStatus reports whether s is a Kubernetes condition status.
func IsConditionStatus(s string) bool { return conditionStatuses[s] }

// ConditionStatuses returns the condition status enum, sorted.
func ConditionStatuses() []string { return sortedKeys(conditionStatuses) }

// vocabularyV1 is the operator's closed condition vocabulary
// (internal/operator/api/v1alpha1/conditions.go): each type with its closed
// reason set. A new type or reason needs a new vocabulary version.
var vocabularyV1 = map[string][]string{
	"Ready":            {"Reconciled", "Blocked"},
	"Synced":           {"Delivered", "Current", "FetchFailed", "NotMaterialized"},
	"Designation":      {"SecretNotDesignated", "ServiceAccountNotDesignated", "InstanceMismatch", "AudienceMissing"},
	"Conflict":         {"ManagedSecretNotOwned", "TargetClaimed", "TargetTypeImmutable"},
	"Delivery":         {"UndeliveredSecrets", "KeysMissing", "InvalidSecretData", "LoaderControlUnacknowledged", "EnvFromSkip"},
	"Scrubbed":         {"AuthorizationWithdrawn"},
	"Rollout":          {"Stalled"},
	"CredentialExpiry": {"ExpiresSoon", "Expired"},
	"PinExpired":       {"PinExpired"},
	"Unreconciled":     {"NamespaceNotBound"},
}

var vocabularies = map[int]map[string][]string{1: vocabularyV1}

// ConditionTypes returns the condition types of vocabulary v, sorted, or nil
// for a version this server does not accept.
func ConditionTypes(v int) []string {
	vocab, ok := vocabularies[v]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(vocab))
	for t := range vocab {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Reasons returns the closed reason set of one condition type in vocabulary
// v, sorted.
func Reasons(v int, conditionType string) []string {
	out := slices.Clone(vocabularies[v][conditionType])
	slices.Sort(out)
	return out
}

// AllReasons returns the union of every reason in vocabulary v, sorted.
func AllReasons(v int) []string {
	set := map[string]bool{}
	for _, reasons := range vocabularies[v] {
		for _, r := range reasons {
			set[r] = true
		}
	}
	return sortedKeys(set)
}

// Refusal causes: the closed set the audit trail records (ADR D8). Only
// RefusalVocabulary is ever recorded on a row (D5). An authorization refusal
// has no cause here: it is the chokepoint's uniform nonexistent answer, and
// its record is the chokepoint's own grant.denied naming the operation, since
// no tenant proof exists to write a delivery-target event under.
const (
	RefusalVocabulary = "vocabulary"
	RefusalOrdering   = "ordering"
	RefusalQuota      = "quota"
	RefusalSize       = "size"
)

// RefusalCauses returns the closed refusal-cause enum, sorted.
func RefusalCauses() []string {
	return []string{RefusalOrdering, RefusalQuota, RefusalSize, RefusalVocabulary}
}

// Derived UI states (ADR D5). Computed at read time; nothing stores "healthy".
const (
	StateUnknown         = "unknown"
	StateStale           = "stale"
	StateRefused         = "refused"
	StateReporterRevoked = "reporter-revoked"
	StateReported        = "reported"
)

// States returns the derived state enum, sorted.
func States() []string {
	return []string{StateRefused, StateReported, StateReporterRevoked, StateStale, StateUnknown}
}

// Condition is one asserted condition, from the closed vocabulary.
type Condition struct {
	Type               string
	Status             string
	Reason             string
	ObservedGeneration int64
}

// Target names one delivery target. Namespace and Name are display labels
// (D3a); the UIDs are the identity.
type Target struct {
	ClusterID   string
	InstanceUID string
	Namespace   string
	Name        string
	UID         string
}

// Report is one decoded report body. Every string field is closed.
type Report struct {
	Vocabulary            int
	Target                Target
	Generation            int64
	ObservedGeneration    int64
	ReportedAt            time.Time
	ReportIntervalSeconds int64
	Lifecycle             string
	Conditions            []Condition
	Reporter              string
	ReporterVersion       string
}

// ErrShape is a report the server refuses as malformed (400): a string
// outside the name grammar, a UID that is not one, a missing member. It is
// decided before authorization and discloses nothing.
var ErrShape = errors.New("deliverytarget: malformed report")

// VocabularyError is the whole-report refusal for a value outside the
// advertised vocabulary (422). Field names the JSON member; it never echoes
// the refused value.
type VocabularyError struct{ Field string }

func (e *VocabularyError) Error() string {
	return fmt.Sprintf("deliverytarget: %s is outside the advertised vocabulary", e.Field)
}

// Grammar patterns, exported so the OpenAPI pin test holds the wire contract
// to the exact grammar the server applies.
const (
	// DNS-1123 label: namespace names (63 chars).
	DNS1123LabelPattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// DNS-1123 subdomain: object names (253 chars).
	DNS1123SubdomainPattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// Kubernetes object UIDs are RFC 4122 UUIDs in canonical lower-case form.
	UIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	// SemVer 2.0.0, the official grammar.
	SemVerPattern = `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	MaxLabelLength     = 63
	MaxSubdomainLength = 253
	MaxUIDLength       = 36
	MaxVersionLength   = 64
	// The Kubernetes condition grammar (k8s.io/apimachinery metav1.Condition
	// Type and Reason validation). A condition type or reason is bounded by it
	// on the wire, not enumerated, so a value outside the advertised
	// vocabulary reaches the vocabulary check and is refused 422 naming the
	// member (ADR D4, D5), rather than 400 before authorization, which the
	// operator's per-CR suppression (D9) would not recognize.
	ConditionTypePattern     = `^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	MaxConditionTypeLength   = 316
	ConditionReasonPattern   = `^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`
	MaxConditionReasonLength = 1024
)

var (
	dns1123Label     = regexp.MustCompile(DNS1123LabelPattern)
	dns1123Subdomain = regexp.MustCompile(DNS1123SubdomainPattern)
	uidPattern       = regexp.MustCompile(UIDPattern)
	semverPattern    = regexp.MustCompile(SemVerPattern)
	conditionType    = regexp.MustCompile(ConditionTypePattern)
	conditionReason  = regexp.MustCompile(ConditionReasonPattern)
)

// IsDNS1123Label reports whether s is a Kubernetes namespace name.
func IsDNS1123Label(s string) bool {
	return len(s) <= MaxLabelLength && dns1123Label.MatchString(s)
}

// IsDNS1123Subdomain reports whether s is a Kubernetes object name.
func IsDNS1123Subdomain(s string) bool {
	return len(s) <= MaxSubdomainLength && dns1123Subdomain.MatchString(s)
}

// IsUID reports whether s is a Kubernetes object UID.
func IsUID(s string) bool { return uidPattern.MatchString(s) }

// IsSemVer reports whether s is a bounded SemVer 2.0.0 version.
func IsSemVer(s string) bool { return len(s) <= MaxVersionLength && semverPattern.MatchString(s) }

// CheckTarget applies the target grammar.
func CheckTarget(t Target) error {
	switch {
	case !IsUID(t.ClusterID):
		return fmt.Errorf("%w: target.cluster_id is not a UID", ErrShape)
	case !IsUID(t.InstanceUID):
		return fmt.Errorf("%w: target.instance_uid is not a UID", ErrShape)
	case !IsUID(t.UID):
		return fmt.Errorf("%w: target.uid is not a UID", ErrShape)
	case !IsDNS1123Label(t.Namespace):
		return fmt.Errorf("%w: target.namespace is not a DNS-1123 label", ErrShape)
	case !IsDNS1123Subdomain(t.Name):
		return fmt.Errorf("%w: target.name is not a DNS-1123 subdomain", ErrShape)
	}
	return nil
}

// CheckShape applies every rule that does not depend on the vocabulary:
// grammar, bounds and required members. A failure is a 400.
func CheckShape(r Report) error {
	if err := CheckTarget(r.Target); err != nil {
		return err
	}
	switch {
	case r.Generation < 1:
		return fmt.Errorf("%w: generation must be at least 1", ErrShape)
	case r.ObservedGeneration < 0 || r.ObservedGeneration > r.Generation:
		return fmt.Errorf("%w: observed_generation must be between 0 and generation", ErrShape)
	case r.ReportedAt.IsZero():
		return fmt.Errorf("%w: reported_at is required", ErrShape)
	case r.ReportIntervalSeconds < 1:
		return fmt.Errorf("%w: report_interval_seconds must be positive", ErrShape)
	case !IsSemVer(r.ReporterVersion):
		return fmt.Errorf("%w: reporter.version is not a SemVer 2.0 version", ErrShape)
	case len(r.Conditions) > len(vocabularyV1):
		return fmt.Errorf("%w: conditions carries more entries than any vocabulary defines", ErrShape)
	}
	for i, c := range r.Conditions {
		if c.ObservedGeneration < 0 || c.ObservedGeneration > r.Generation {
			return fmt.Errorf("%w: conditions[%d].observed_generation must be between 0 and generation", ErrShape, i)
		}
		if !IsConditionStatus(c.Status) {
			return fmt.Errorf("%w: conditions[%d].status must be True, False or Unknown", ErrShape, i)
		}
		if len(c.Type) > MaxConditionTypeLength || !conditionType.MatchString(c.Type) {
			return fmt.Errorf("%w: conditions[%d].type is not a Kubernetes condition type", ErrShape, i)
		}
		if len(c.Reason) > MaxConditionReasonLength || !conditionReason.MatchString(c.Reason) {
			return fmt.Errorf("%w: conditions[%d].reason is not a Kubernetes condition reason", ErrShape, i)
		}
	}
	return nil
}

// CheckVocabulary applies the advertised vocabulary. Any value outside it
// refuses the whole report (D4). It runs after CheckShape.
func CheckVocabulary(r Report) error {
	vocab, ok := vocabularies[r.Vocabulary]
	if !ok {
		return &VocabularyError{Field: "vocabulary"}
	}
	if !IsReporter(r.Reporter) {
		return &VocabularyError{Field: "reporter.integration"}
	}
	if !IsLifecycle(r.Lifecycle) {
		return &VocabularyError{Field: "lifecycle"}
	}
	seen := map[string]bool{}
	for i, c := range r.Conditions {
		reasons, ok := vocab[c.Type]
		if !ok {
			return &VocabularyError{Field: fmt.Sprintf("conditions[%d].type", i)}
		}
		if !slices.Contains(reasons, c.Reason) {
			return &VocabularyError{Field: fmt.Sprintf("conditions[%d].reason", i)}
		}
		if seen[c.Type] {
			return &VocabularyError{Field: fmt.Sprintf("conditions[%d].type", i)}
		}
		seen[c.Type] = true
	}
	return nil
}

// ClampInterval clamps a reported heartbeat interval to [5 min, 24 h] (D5).
func ClampInterval(seconds int64) time.Duration {
	d := time.Duration(seconds) * time.Second
	if seconds > int64(MaxReportInterval/time.Second) {
		return MaxReportInterval
	}
	return max(d, MinReportInterval)
}

// Ordering is the D5(a) outcome of comparing a report to the stored row.
type Ordering int

const (
	// InOrder: newer generation, or later reported_at within one generation.
	InOrder Ordering = iota
	// OlderGeneration: observed_generation below the stored one.
	OlderGeneration
	// NotLater: equal generation, reported_at not later than stored.
	NotLater
)

// Order compares an incoming report to the stored row's accepted state.
func Order(storedObserved int64, storedReportedAt time.Time, observed int64, reportedAt time.Time) Ordering {
	switch {
	case observed < storedObserved:
		return OlderGeneration
	case observed == storedObserved && !reportedAt.After(storedReportedAt):
		return NotLater
	default:
		return InOrder
	}
}

// InFuture reports whether reported_at is beyond the allowed clock skew.
func InFuture(reportedAt, now time.Time) bool {
	return reportedAt.After(now.Add(MaxFutureSkew))
}

// Row is the stored accepted state the derived state is computed over.
type Row struct {
	ReceivedAt     time.Time
	ReportInterval time.Duration
	RefusedAt      time.Time
}

// Stale reports whether the row is past its staleness threshold:
// now - received_at > 2 * report_interval + 5 min.
func (r Row) Stale(now time.Time) bool {
	interval := ClampInterval(int64(r.ReportInterval / time.Second))
	return now.Sub(r.ReceivedAt) > 2*interval+StaleGrace
}

// Derive computes the D5 state of one row. reporterLive is false when the
// principal no longer holds `report-delivery-status` on the environment, or
// holds no live credential. Precedence: reporter-revoked, refused, stale,
// reported.
func Derive(r Row, reporterLive bool, now time.Time) string {
	switch {
	case !reporterLive:
		return StateReporterRevoked
	case !r.RefusedAt.IsZero() && r.RefusedAt.After(r.ReceivedAt):
		return StateRefused
	case r.Stale(now):
		return StateStale
	default:
		return StateReported
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
