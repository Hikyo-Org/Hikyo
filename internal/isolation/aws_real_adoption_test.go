package isolation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func (s *realAWSSandbox) seedSynthetic(t *testing.T, name, value string) string {
	t.Helper()
	var result struct {
		ARN string `json:"ARN"`
	}
	if err := s.call(t.Context(), false, "CreateSecret", name, map[string]any{"SecretString": value, "KmsKeyId": s.manifest.KMSKeyARN, "ClientRequestToken": strings.Repeat("d", 64)}, &result); err != nil {
		t.Fatal("synthetic adoption resource creation failed")
	}
	if err := s.remember(name, result.ARN); err != nil {
		t.Fatal(err)
	}
	return result.ARN
}

// Replacement is explicitly destructive to a recorded synthetic ARN only.
// The full fixture scope must authorize this before any test can be launched.
func (s *realAWSSandbox) replaceSynthetic(t *testing.T, name string) {
	t.Helper()
	old := s.arn(t, name)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := s.call(ctx, false, "DeleteSecret", old, map[string]any{"ForceDeleteWithoutRecovery": true}, nil); err != nil {
		t.Fatal("recorded synthetic replacement deletion failed")
	}
	for {
		err := s.call(ctx, false, "DescribeSecret", old, map[string]any{}, nil)
		if realAWSCode(err, "ResourceNotFoundException") {
			break
		}
		if err != nil {
			t.Fatal("cannot verify old synthetic ARN absence")
		}
		select {
		case <-ctx.Done():
			t.Fatal("old synthetic ARN deletion did not finish in time")
		case <-time.After(time.Second):
		}
	}
	// AWS name reuse can lag ARN absence. Retry only a known name-occupied
	// response, at most twice; every attempt consumes the shared 32 ceiling.
	for attempt := 0; attempt < 3; attempt++ {
		var result struct {
			ARN string `json:"ARN"`
		}
		err := s.call(ctx, false, "CreateSecret", name, map[string]any{"SecretString": "unreviewed replacement synthetic", "KmsKeyId": s.manifest.KMSKeyARN, "ClientRequestToken": strings.Repeat("f", 64)}, &result)
		if err == nil {
			if result.ARN == old {
				t.Fatal("AWS replacement did not change the complete ARN")
			}
			if err := s.remember(name, result.ARN); err != nil {
				t.Fatal(err)
			}
			return
		}
		if !realAWSCode(err, "ResourceExistsException") && !realAWSCode(err, "InvalidRequestException") {
			t.Fatal("synthetic replacement creation failed; reconcile reservation")
		}
		select {
		case <-ctx.Done():
			t.Fatal("synthetic name reuse deadline reached")
		case <-time.After(2 * time.Second):
		}
	}
	t.Fatal("synthetic name remained unavailable within the bounded replacement attempts")
}

func (h *realAWSDurable) planAdoption(target store.AdapterTarget) service.AdapterPlanResult {
	h.t.Helper()
	plan, err := h.svc.Plan(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, target.ID)
	if err != nil || plan.ArtifactID == "" {
		h.t.Fatal("AWS consent plan unavailable")
	}
	return plan
}

func (h *realAWSDurable) adoptPlan(target store.AdapterTarget, artifactID, name string) {
	h.t.Helper()
	if _, err := h.svc.Adopt(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: artifactID, ExpectedGeneration: target.Generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
		h.t.Fatal("durable AWS adoption refused")
	}
}

func runRealAWSAdoptionWitness(t *testing.T, s *realAWSSandbox) {
	for _, scenario := range []string{"version-before-adopt", "version-after-adopt", "replacement-after-adopt"} {
		t.Run(scenario, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h := newRealAWSDurable(t, db, s, "static")
				h.publish("witness source", "witness alpha")
				name := h.prefix + scenario
				s.seedSynthetic(t, name, "reviewed legacy synthetic")
				target := h.create(name, adapter.JSONObject)
				plan := h.planAdoption(target)
				mutate := func() {
					if scenario == "replacement-after-adopt" {
						s.replaceSynthetic(t, name)
						return
					}
					if err := s.call(t.Context(), false, "PutSecretValue", s.arn(t, name), map[string]any{"SecretString": "unreviewed version synthetic", "ClientRequestToken": strings.Repeat("e", 64)}, nil); err != nil {
						t.Fatal("external synthetic edit failed")
					}
				}
				if scenario == "version-before-adopt" {
					mutate()
				}
				h.adoptPlan(target, plan.ArtifactID, name)
				if scenario != "version-before-adopt" {
					mutate()
				}
				beforeMetadata := h.metadata(name)
				before := s.value(t, name)
				putsBefore := len(s.recordedPutTokens(beforeMetadata.ARN))
				h.drain()
				state := h.inspect(target.ID)
				refused := h.metadata(name)
				if state.Health() == adapter.HealthConverged || state.LastErrorClass != adapter.ErrorClassConflict || s.value(t, name) != before || !realAWSSameCurrent(beforeMetadata, refused) || len(s.recordedPutTokens(beforeMetadata.ARN)) != putsBefore {
					t.Fatal("stale adoption consent overwrote an unreviewed AWS resource/version")
				}
				if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
					t.Fatal("stale-consent refusal lost custody")
				}
				fresh := h.planAdoption(state)
				h.adoptPlan(state, fresh.ArtifactID, name)
				h.drain()
				h.converged(target.ID)
				puts := s.recordedPutTokens(beforeMetadata.ARN)
				if !realAWSAddedVersion(beforeMetadata, h.metadata(name), target.ID, puts[putsBefore:]) || s.value(t, name) != `{"ALPHA":"witness alpha","SHARED_KEY":"witness source"}` {
					t.Fatal("fresh real adoption did not recover exactly once")
				}
				h.remove(target.ID, false)
				h.deleted(name, true)
				h.audit()
			})
		})
	}
}

// DescribeSecret omits unlabelled versions. Observe the exact write token and
// current/previous identities instead of treating labelled entries as a count
// of all versions. Repeating one token is idempotent; a second distinct token
// proves an additional version and must fail.
func realAWSPromotedVersion(before, after awssm.SecretMetadata, targetID string, puts []string) bool {
	if len(puts) == 0 || len(puts[0]) != 64 || before.ARN != after.ARN || before.Name != after.Name || after.Tags[adapter.SentinelName] != targetID || targetID == "" {
		return false
	}
	token, predecessor := puts[0], realAWSCurrentVersion(before)
	for _, attempt := range puts {
		if attempt != token {
			return false
		}
	}
	return predecessor != "" && predecessor != token &&
		realAWSCurrentVersion(after) == token &&
		slices.Contains(after.Stages[predecessor], "AWSPREVIOUS")
}

func realAWSAddedVersion(before, after awssm.SecretMetadata, targetID string, puts []string) bool {
	marker, unique := realAWSStageVersion(after, awssm.CurrentStage)
	return realAWSPromotedVersion(before, after, targetID, puts) && unique && marker == puts[0] && after.Tags[awssm.VersionTag] == puts[0]
}

func realAWSReplayCompleted(loads int, before, after awssm.SecretMetadata, targetID string, puts []string) bool {
	// The observer checks the same job and consent witness on every load.
	// A scheduled transient-read retry can load again without another value
	// version. Both drain budgets and the distinct-token proof still apply.
	return loads >= 2 && realAWSAddedVersion(before, after, targetID, puts)
}

func realAWSMarkerRefused(before, after awssm.SecretMetadata, targetID string, puts []string) bool {
	oldMarker, oldUnique := realAWSStageVersion(before, awssm.CurrentStage)
	marker, unique := realAWSStageVersion(after, awssm.CurrentStage)
	return realAWSPromotedVersion(before, after, targetID, puts) && oldUnique && unique && marker == oldMarker && marker != puts[0] && after.Tags[awssm.VersionTag] == realAWSCurrentVersion(before)
}

func realAWSStageVersion(meta awssm.SecretMetadata, stage string) (string, bool) {
	found := ""
	for version, stages := range meta.Stages {
		if slices.Contains(stages, stage) {
			if found != "" {
				return "", false
			}
			found = version
		}
	}
	return found, true
}

func realAWSCurrentVersion(meta awssm.SecretMetadata) string {
	current, unique := realAWSStageVersion(meta, "AWSCURRENT")
	if !unique {
		return ""
	}
	return current
}

func realAWSSameCurrent(before, after awssm.SecretMetadata) bool {
	current := realAWSCurrentVersion(before)
	marker, unique := realAWSStageVersion(before, awssm.CurrentStage)
	afterMarker, afterUnique := realAWSStageVersion(after, awssm.CurrentStage)
	return unique && afterUnique && marker == afterMarker && current != "" && before.ARN == after.ARN && before.Name == after.Name && realAWSCurrentVersion(after) == current && before.Tags[adapter.SentinelName] == after.Tags[adapter.SentinelName] && before.Tags[awssm.VersionTag] == after.Tags[awssm.VersionTag]
}

func runRealAWSPromotedReplay(t *testing.T, s *realAWSSandbox) {
	for _, fault := range []string{"finish-loss", "marker-refusal"} {
		t.Run(fault, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h := newRealAWSDurable(t, db, s, "static")
				h.publish("promoted source", "promoted alpha")
				name := h.prefix + "promoted-" + fault
				s.seedSynthetic(t, name, "reviewed predecessor synthetic")
				target := h.create(name, adapter.JSONObject)
				plan := h.planAdoption(target)
				h.adoptPlan(target, plan.ArtifactID, name)
				before := h.metadata(name)
				observer := &awsAdoptionReplayLoader{Loader: h.worker.Loader, t: t, targetID: target.ID}
				h.worker.Loader = observer
				interrupted := false
				if fault == "finish-loss" {
					h.finishAdoption = func(id string) error {
						if id == target.ID && !interrupted {
							interrupted = true
							return errors.New("injected journal transaction loss after real promotion")
						}
						return nil
					}
				} else {
					h.failCurrentMarker = true
				}
				if fault == "finish-loss" {
					h.drainWithFinishLoss(&interrupted)
				} else {
					h.drain()
				}
				h.finishAdoption = nil
				if fault == "marker-refusal" {
					interrupted = !h.failCurrentMarker
				}
				promoted := h.metadata(name)
				valid := realAWSAddedVersion(before, promoted, target.ID, s.recordedPutTokens(before.ARN))
				if fault == "marker-refusal" {
					valid = realAWSMarkerRefused(before, promoted, target.ID, s.recordedPutTokens(before.ARN))
				}
				if !interrupted || !valid || s.value(t, name) != `{"ALPHA":"promoted alpha","SHARED_KEY":"promoted source"}` || h.inspect(target.ID).Health() == adapter.HealthConverged {
					t.Fatal("real promotion did not precede the injected local interruption")
				}
				if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
					t.Fatal("interruption lost pending Owned custody")
				}
				h.restartWorker()
				observer.Loader = h.worker.Loader
				h.worker.Loader = observer
				h.clock = h.clock.Add(adapter.LeaseTime + adapter.RetryCap + time.Second)
				h.drain()
				h.converged(target.ID)
				if !realAWSReplayCompleted(observer.loads, before, h.metadata(name), target.ID, s.recordedPutTokens(before.ARN)) {
					t.Fatal("real promoted replay changed job/witness or duplicated a version")
				}
				if fault == "finish-loss" && queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome='unknown'`, target.ID)) != 1 {
					t.Fatal("real promotion crash window was not durably reconciled")
				}
				h.restartWorker() // retire the pending-adoption observer before scrub
				h.remove(target.ID, false)
				h.deleted(name, true)
				h.audit()
			})
		})
	}
}
