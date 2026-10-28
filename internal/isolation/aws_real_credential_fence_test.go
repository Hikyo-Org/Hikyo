package isolation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Reuse a converged synthetic resource: these fences consume no new creates.
// Expiry is a genuine AWS refusal; local revoke is a Hikyo authority change,
// not a claim that AWS has revoked the already-issued temporary session.
func (h *realAWSDurable) credentialFences(targetID, name string) {
	t := h.t
	scope := domain.Scope{Org: orgA, Project: prjA1}
	actor := service.LocalPrincipal(alice)
	originalValue := h.s.value(t, name)
	originalCreates, originalOps := h.s.creationAndMutationCounts()
	a := h.s.manifest.Writers["static"]
	expired := h.s.expiredStaticDescriptor(t, a.ExpiredCredentialFile, a.CredentialFile)
	parsed, err := awssm.ParseDescriptor(expired)
	if err != nil {
		t.Fatal("invalid protected expired descriptor")
	}
	h.secrets = append(h.secrets, parsed.SecretKey, parsed.SessionToken)
	if _, err := h.svc.ReplaceCredential(t.Context(), actor, scope, h.adapterID, []byte(expired)); err != nil {
		t.Fatal("could not install authentic expired fixture credential")
	}
	expiredJob := h.resync(targetID)
	h.drain()
	state := h.inspect(targetID)
	if state.LastErrorClass != adapter.ErrorClassAuth || state.Health() == adapter.HealthConverged || queryString(t, h.db, fmt.Sprintf(`SELECT state FROM adapter_outbox WHERE id='%s'`, expiredJob)) != "failed" {
		t.Fatal("expired credential did not fail the actual durable job loudly")
	}
	creates, ops := h.s.creationAndMutationCounts()
	if creates != originalCreates || ops != originalOps || h.s.value(t, name) != originalValue {
		t.Fatal("expired credential mutated the synthetic destination")
	}
	h.restoreFreshCredential(targetID)

	// Revoke after AWS has accepted a staged Put, before its response returns
	// to the module's next request boundary. The actual DB generation and
	// outbox retirement must prevent promotion and all following mutations.
	before := h.metadata(name)
	arn, current := before.ARN, h.currentVersion(name)
	ledgerID := queryString(t, h.db, fmt.Sprintf(`SELECT id FROM adapter_ledger WHERE org_id='%s' AND project_id='%s' AND environment_id='%s' AND target_id='%s' AND surface='secret' AND effective_name='%s' AND state='owned'`, orgA, prjA1, envA1, targetID, name))
	if ledgerID == "" {
		t.Fatal("credential revocation requires exact existing owned custody")
	}
	opsAtRevocation := -1
	h.afterPut = func(ctx context.Context, id string) error {
		if id != arn {
			return nil
		}
		h.afterPut = nil
		_, opsAtRevocation = h.s.creationAndMutationCounts()
		_, err := h.svc.RevokeCredential(ctx, actor, scope, h.adapterID)
		return err
	}
	t.Cleanup(func() { h.afterPut = nil })
	revokedJob := h.resync(targetID)
	// Revoke retires this running job and clears its lease owner. The effect
	// still settles, but the worker's terminal Fail cannot own that retired
	// row. Capture this exact expected supersession; generic drain stays strict.
	worked, err := h.worker.RunOnce(t.Context())
	if !worked || !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatal("revoked worker did not return the expected job supersession")
	}
	creates, ops = h.s.creationAndMutationCounts()
	if opsAtRevocation < 0 || ops != opsAtRevocation || creates != originalCreates {
		t.Fatal("provider mutations continued after actual credential revocation")
	}
	if queryString(t, h.db, fmt.Sprintf(`SELECT state FROM adapter_outbox WHERE id='%s'`, revokedJob)) != "superseded" || queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapters WHERE id='%s' AND credential_ciphertext IS NULL`, h.adapterID)) != 1 {
		t.Fatal("revocation did not retire the exact running job and destroy credential custody")
	}
	if h.s.value(t, name) != originalValue || h.metadata(name).ARN != arn || h.currentVersion(name) != current || queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE id='%s' AND org_id='%s' AND project_id='%s' AND environment_id='%s' AND target_id='%s' AND surface='secret' AND effective_name='%s' AND state='owned'`, ledgerID, orgA, prjA1, envA1, targetID, name)) != 1 {
		t.Fatal("revoked staged write changed current identity/version/value or exact owned custody")
	}
	if queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE job_id='%s' AND target_id='%s' AND surface='secret' AND effective_name='%s' AND outcome='failure' AND finished_at IS NOT NULL`, revokedJob, targetID, name)) != 1 || queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_targets WHERE id='%s' AND provider_lease_job_id IS NULL AND provider_lease_effect_id IS NULL AND provider_lease_expires_at IS NULL`, targetID)) != 1 {
		t.Fatal("revoked write did not settle its exact effect and release the provider fence")
	}
	h.restoreFreshCredential(targetID)
	h.audit()
}

func (h *realAWSDurable) restoreFreshCredential(targetID string) {
	h.t.Helper()
	if _, err := h.svc.ReplaceCredential(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, h.adapterID, []byte(h.descriptor)); err != nil {
		h.t.Fatal("fresh fixture credential replacement failed")
	}
	h.resync(targetID)
	h.drain()
	h.converged(targetID)
}

func (s *realAWSSandbox) creationAndMutationCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.creations), len(s.ops)
}


func (h *realAWSDurable) currentVersion(name string) string {
	h.t.Helper()
	current := ""
	for version, stages := range h.metadata(name).Stages {
		if slices.Contains(stages, "AWSCURRENT") {
			if current != "" || version == "" {
				h.t.Fatal("synthetic secret has ambiguous current version metadata")
			}
			current = version
		}
	}
	if current == "" {
		h.t.Fatal("synthetic secret has no current version")
	}
	return current
}
