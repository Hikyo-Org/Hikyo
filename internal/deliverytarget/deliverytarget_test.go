package deliverytarget

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

// The server vocabulary must be the operator's closed condition set, no more
// and no less. A new operator condition fails here until a new vocabulary
// version carries it.
func TestVocabularyV1MatchesOperatorConditions(t *testing.T) {
	operator := map[string][]string{
		hikyov1.ConditionReady:            {hikyov1.ReasonReconciled, hikyov1.ReasonBlocked},
		hikyov1.ConditionSynced:           {hikyov1.ReasonDelivered, hikyov1.ReasonCurrent, hikyov1.ReasonFetchFailed, hikyov1.ReasonNotMaterialized},
		hikyov1.ConditionDesignation:      {hikyov1.ReasonSecretNotDesignated, hikyov1.ReasonServiceAccountNotDesignated, hikyov1.ReasonInstanceMismatch, hikyov1.ReasonAudienceMissing},
		hikyov1.ConditionConflict:         {hikyov1.ReasonManagedSecretNotOwned, hikyov1.ReasonTargetClaimed, hikyov1.ReasonTargetTypeImmutable},
		hikyov1.ConditionDelivery:         {hikyov1.ReasonUndeliveredSecrets, hikyov1.ReasonKeysMissing, hikyov1.ReasonInvalidSecretData, hikyov1.ReasonLoaderControlUnacknowledged, hikyov1.ReasonEnvFromSkip},
		hikyov1.ConditionScrubbed:         {hikyov1.ReasonAuthorizationWithdrawn},
		hikyov1.ConditionRollout:          {hikyov1.ReasonStalled},
		hikyov1.ConditionCredentialExpiry: {hikyov1.ReasonExpiresSoon, hikyov1.ReasonExpired},
		hikyov1.ConditionPinExpired:       {hikyov1.ReasonPinExpired},
		hikyov1.ConditionUnreconciled:     {hikyov1.ReasonNamespaceNotBound},
	}
	types := ConditionTypes(1)
	if len(types) != len(operator) {
		t.Fatalf("vocabulary 1 has %d types, operator has %d", len(types), len(operator))
	}
	for condType, reasons := range operator {
		want := slices.Clone(reasons)
		slices.Sort(want)
		if got := Reasons(1, condType); !slices.Equal(got, want) {
			t.Errorf("%s reasons = %v, want %v", condType, got, want)
		}
	}
	lifecycles := []string{
		string(hikyov1.LifecycleRefused), string(hikyov1.LifecycleRetained), string(hikyov1.LifecycleScrubbed),
		string(hikyov1.LifecycleSynced), string(hikyov1.LifecycleUnreconciled),
	}
	if got := Lifecycles(); !slices.Equal(got, lifecycles) {
		t.Errorf("lifecycles = %v, want %v", got, lifecycles)
	}
}

func validReport() Report {
	return Report{
		Vocabulary: 1,
		Target: Target{
			ClusterID:   "0b7f3c9e-1d2a-4e5f-8a9b-0c1d2e3f4a5b",
			InstanceUID: "1b7f3c9e-1d2a-4e5f-8a9b-0c1d2e3f4a5b",
			Namespace:   "payments",
			Name:        "api-secrets",
			UID:         "2b7f3c9e-1d2a-4e5f-8a9b-0c1d2e3f4a5b",
		},
		Generation: 3, ObservedGeneration: 3,
		ReportedAt:            time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		ReportIntervalSeconds: 300,
		Lifecycle:             "Synced",
		Conditions: []Condition{
			{Type: "Ready", Status: "True", Reason: "Reconciled", ObservedGeneration: 3},
			{Type: "Conflict", Status: "True", Reason: "ManagedSecretNotOwned", ObservedGeneration: 3},
		},
		Reporter: ReporterKubernetesOperator, ReporterVersion: "1.4.0-rc.1+build.7",
	}
}

func TestCheckShape(t *testing.T) {
	if err := CheckShape(validReport()); err != nil {
		t.Fatalf("valid report: %v", err)
	}
	cases := map[string]func(*Report){
		"namespace uppercase":     func(r *Report) { r.Target.Namespace = "Payments" },
		"namespace too long":      func(r *Report) { r.Target.Namespace = strings.Repeat("a", 64) },
		"name too long":           func(r *Report) { r.Target.Name = strings.Repeat("a", 254) },
		"name free text":          func(r *Report) { r.Target.Name = "api secrets" },
		"cluster not uid":         func(r *Report) { r.Target.ClusterID = "kube-system" },
		"instance uid uppercase":  func(r *Report) { r.Target.InstanceUID = strings.ToUpper(r.Target.InstanceUID) },
		"cr uid empty":            func(r *Report) { r.Target.UID = "" },
		"generation zero":         func(r *Report) { r.Generation = 0 },
		"observed above":          func(r *Report) { r.ObservedGeneration = 4 },
		"no reported_at":          func(r *Report) { r.ReportedAt = time.Time{} },
		"interval zero":           func(r *Report) { r.ReportIntervalSeconds = 0 },
		"version free text":       func(r *Report) { r.ReporterVersion = "latest" },
		"version too long":        func(r *Report) { r.ReporterVersion = "1.0.0-" + strings.Repeat("a", 64) },
		"condition status":        func(r *Report) { r.Conditions[0].Status = "Maybe" },
		"lifecycle outside enum":  func(r *Report) { r.Lifecycle = "Healthy" },
		"reporter outside enum":   func(r *Report) { r.Reporter = "compose" },
		"condition observed":      func(r *Report) { r.Conditions[0].ObservedGeneration = 9 },
		"too many conditions":     func(r *Report) { r.Conditions = make([]Condition, 11) },
		"name leading hyphen":     func(r *Report) { r.Target.Name = "-api" },
		"namespace with dot":      func(r *Report) { r.Target.Namespace = "a.b" },
		"observed generation neg": func(r *Report) { r.ObservedGeneration = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validReport()
			mutate(&r)
			if err := CheckShape(r); !errors.Is(err, ErrShape) {
				t.Fatalf("got %v, want ErrShape", err)
			}
		})
	}
}

func TestCheckVocabulary(t *testing.T) {
	if err := CheckVocabulary(validReport()); err != nil {
		t.Fatalf("valid report: %v", err)
	}
	cases := map[string]struct {
		mutate func(*Report)
		field  string
	}{
		"version":        {func(r *Report) { r.Vocabulary = 2 }, "vocabulary"},
		"type":           {func(r *Report) { r.Conditions[1].Type = "Available" }, "conditions[1].type"},
		"reason":         {func(r *Report) { r.Conditions[0].Reason = "Delivered" }, "conditions[0].reason"},
		"duplicate type": {func(r *Report) { r.Conditions[1] = r.Conditions[0] }, "conditions[1].type"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := validReport()
			tc.mutate(&r)
			var verr *VocabularyError
			if err := CheckVocabulary(r); !errors.As(err, &verr) || verr.Field != tc.field {
				t.Fatalf("got %v, want vocabulary refusal naming %s", err, tc.field)
			}
		})
	}
}

func TestOrderAndSkew(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if Order(3, at, 2, at.Add(time.Hour)) != OlderGeneration {
		t.Error("lower observed generation must be refused")
	}
	if Order(3, at, 3, at) != NotLater {
		t.Error("equal reported_at within a generation must be refused")
	}
	if Order(3, at, 3, at.Add(-time.Second)) != NotLater {
		t.Error("earlier reported_at within a generation must be refused")
	}
	if Order(3, at, 3, at.Add(time.Second)) != InOrder {
		t.Error("later reported_at within a generation is in order")
	}
	if Order(3, at, 4, at.Add(-time.Hour)) != InOrder {
		t.Error("a newer generation is in order regardless of the clock")
	}
	if InFuture(at.Add(5*time.Minute), at) {
		t.Error("exactly 5 min of skew is allowed")
	}
	if !InFuture(at.Add(5*time.Minute+time.Second), at) {
		t.Error("more than 5 min of skew is refused")
	}
}

func TestDerive(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	fresh := Row{ReceivedAt: now.Add(-time.Minute), ReportInterval: 5 * time.Minute}
	if got := Derive(fresh, true, now); got != StateReported {
		t.Errorf("fresh row = %s", got)
	}
	// 2 * 5 min + 5 min = 15 min threshold.
	edge := Row{ReceivedAt: now.Add(-15 * time.Minute), ReportInterval: 5 * time.Minute}
	if got := Derive(edge, true, now); got != StateReported {
		t.Errorf("row at the threshold = %s", got)
	}
	stale := Row{ReceivedAt: now.Add(-15*time.Minute - time.Second), ReportInterval: 5 * time.Minute}
	if got := Derive(stale, true, now); got != StateStale {
		t.Errorf("stale row = %s", got)
	}
	// A 30 s interval is clamped up to 5 min, so it is not stale at 2 min.
	clamped := Row{ReceivedAt: now.Add(-2 * time.Minute), ReportInterval: 30 * time.Second}
	if got := Derive(clamped, true, now); got != StateReported {
		t.Errorf("clamped row = %s", got)
	}
	// A 7-day interval is clamped down to 24 h.
	long := Row{ReceivedAt: now.Add(-49*time.Hour - 6*time.Minute), ReportInterval: 7 * 24 * time.Hour}
	if got := Derive(long, true, now); got != StateStale {
		t.Errorf("long-interval row = %s", got)
	}
	refused := Row{ReceivedAt: now.Add(-time.Hour), ReportInterval: 5 * time.Minute, RefusedAt: now.Add(-time.Minute)}
	if got := Derive(refused, true, now); got != StateRefused {
		t.Errorf("refused row = %s, want refused before stale", got)
	}
	if got := Derive(refused, false, now); got != StateReporterRevoked {
		t.Errorf("revoked reporter = %s, want reporter-revoked first", got)
	}
	superseded := Row{ReceivedAt: now.Add(-time.Minute), ReportInterval: 5 * time.Minute, RefusedAt: now.Add(-time.Hour)}
	if got := Derive(superseded, true, now); got != StateReported {
		t.Errorf("refusal before the last accepted report = %s", got)
	}
}

func TestCapabilityTokens(t *testing.T) {
	if got := CapabilityTokens(); !slices.Equal(got, []string{"delivery-target-report/1"}) {
		t.Fatalf("tokens = %v", got)
	}
}
