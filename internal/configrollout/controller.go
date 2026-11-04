package configrollout

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// controllerJournal is executor-owned durable state. It carries independently
// signed authorization and the prepared plan, not merely an editable self-hash.
// Keeping the accepted command permits exact recovery after its transport TTL.
type controllerJournal struct {
	Accepted SignedCommand `json:"accepted"`
	Plan     *record       `json:"plan,omitempty"`
	Response *Response     `json:"response,omitempty"`
}

type Controller struct {
	client     kubernetes.Interface
	enrollment Enrollment
	keys       PublicKeys
	executor   *Kubernetes
	now        func() time.Time
}

// NewController builds a compatibility-profile controller from one Ed25519 key.
// An enrollment carrying an Authority needs NewControllerKeys.
func NewController(client kubernetes.Interface, e Enrollment, public ed25519.PublicKey) (*Controller, error) {
	return NewControllerKeys(client, e, PublicKeys{Ed25519: public})
}

// NewControllerKeys refuses keys that are not exactly the enrolled ones, so a
// swapped or extra key never becomes acceptable.
func NewControllerKeys(client kubernetes.Interface, e Enrollment, keys PublicKeys) (*Controller, error) {
	if !validEnrollment(e) {
		return nil, ErrInvalid
	}
	if err := CheckKeys(e, keys); err != nil {
		return nil, err
	}
	executor, err := NewKubernetes(client, e.Target)
	if err != nil {
		return nil, err
	}
	return &Controller{client: client, enrollment: cloneEnrollment(e), keys: PublicKeys{Ed25519: append(ed25519.PublicKey(nil), keys.Ed25519...), MLDSA65: keys.MLDSA65}, executor: executor, now: time.Now}, nil
}

// trustError is the closed refusal for an unanchorable journal: strict
// enrollments report trust, compatibility keeps the historical class.
func (c *Controller) trustError() error {
	if c.enrollment.Authority != nil {
		return ErrTrust
	}
	return ErrConflict
}

// reconcileTrust runs at takeover and restart, before any command. It refuses a
// stale or substituted enrollment against the persisted generation floor, refuses
// a retained journal entry the installed keys cannot anchor (so the failure is
// visible at start, not a silent per-command refusal), and raises the floor.
//
// The compatibility profile keeps its historical start: a journal that cannot
// be read or does not verify never stops the process (reconcile retries and
// refuses per tick, as before). Only a persisted strict floor refuses it, which
// is the rollback case. Compatibility writes nothing, so existing journals stay
// byte-identical.
func (c *Controller) reconcileTrust(ctx context.Context) error {
	e := c.enrollment
	journalSecret, err := fixedSecret(ctx, c.client, e.Target.Namespace, e.JournalSecret, e.JournalSecretUID)
	if err != nil {
		if e.Authority == nil {
			return nil
		}
		return err
	}
	recorded, err := parseTrustRecord(journalSecret.Data[trustKey])
	if err != nil {
		return err
	}
	current := trustFor(e)
	if err := checkFloor(recorded, current); err != nil {
		return err
	}
	if e.Authority != nil && len(journalSecret.Data[journalKey]) > 0 {
		var journal controllerJournal
		if decode(journalSecret.Data[journalKey], &journal) != nil || !anchored(journal.Accepted, e, c.keys) {
			return ErrTrust
		}
	}
	if current == nil || recorded != nil && recorded.Generation == current.Generation {
		return nil
	}
	putRecord(journalSecret, trustKey, current)
	if _, err = c.client.CoreV1().Secrets(e.Target.Namespace).Update(ctx, journalSecret, metav1.UpdateOptions{}); err != nil {
		return apiError(err)
	}
	return nil
}

func (c *Controller) reconcile(ctx context.Context) error {
	e := c.enrollment
	commandSecret, err := fixedSecret(ctx, c.client, e.Target.Namespace, e.CommandSecret, e.CommandSecretUID)
	if err != nil {
		return err
	}
	if len(commandSecret.Data[commandKey]) == 0 {
		return nil
	}
	var signed SignedCommand
	if decode(commandSecret.Data[commandKey], &signed) != nil {
		return ErrInvalid
	}
	// Only the verifier's own trust state authorizes a new command. A command
	// from an older trust state is honored only as the already accepted one.
	fresh := verifyCommand(signed, e, c.keys)
	journalSecret, err := fixedSecret(ctx, c.client, e.Target.Namespace, e.JournalSecret, e.JournalSecretUID)
	if err != nil {
		return err
	}
	recorded, err := parseTrustRecord(journalSecret.Data[trustKey])
	if err != nil {
		return err
	}
	if err := checkFloor(recorded, trustFor(e)); err != nil {
		return err
	}
	var journal controllerJournal
	if len(journalSecret.Data[journalKey]) > 0 {
		if decode(journalSecret.Data[journalKey], &journal) != nil || !anchored(journal.Accepted, e, c.keys) {
			return c.trustError()
		}
	}
	command := signed.Command
	accepted := journal.Accepted.Command.Sequence
	if !fresh && (accepted == 0 || digest(signed) != digest(journal.Accepted)) {
		return ErrInvalid
	}
	if command.Sequence > accepted && accepted != 0 && journal.Response == nil && command.Action != ActionRestore {
		// A later mailbox entry must not strand an already accepted operation.
		// Finish its durable journal first, then accept the queued command.
		signed = journal.Accepted
		command = signed.Command
	}
	if command.Sequence < accepted || command.Sequence == accepted && digest(signed) != digest(journal.Accepted) {
		return ErrConflict
	}
	if command.Sequence > accepted {
		now := c.now()
		if command.IssuedAt.After(now) || !now.Before(command.ExpiresAt) {
			return ErrInvalid
		}
		// A new request cannot displace an accepted operation before its durable
		// response. Retries continue the accepted command after process restart.
		if command.Action != ActionPrepare && (journal.Plan == nil || journal.Plan.Digest != command.PlanDigest || journal.Plan.Plan.Intent != command.Intent) {
			return ErrConflict
		}
		if command.Action == ActionPrepare && journal.Plan != nil {
			staged := journal.Accepted.Command.Action == ActionPrepare && journal.Response != nil && journal.Response.Outcome == "complete"
			terminal := journal.Response != nil && journal.Response.Receipt != nil && (journal.Response.Receipt.Phase == Applied || journal.Response.Receipt.Phase == Restored)
			if !staged && !terminal {
				return ErrConflict
			}
			journal.Plan = nil
		}
		journal.Accepted = signed
		journal.Response = nil
		putRecord(journalSecret, journalKey, journal)
		if current := trustFor(e); current != nil && (recorded == nil || recorded.Generation < current.Generation) {
			putRecord(journalSecret, trustKey, current)
		}
		journalSecret, err = c.client.CoreV1().Secrets(e.Target.Namespace).Update(ctx, journalSecret, metav1.UpdateOptions{})
		if err != nil {
			return apiError(err)
		}
	}
	if journal.Response != nil {
		return c.writeResponse(ctx, *journal.Response)
	}
	if command.Action != ActionPrepare && (journal.Plan == nil || digest(command.Topology) != digest(journal.Plan.Plan.Topology) || command.PreviousTemplateStamp != previousTemplateStamp(journal.Plan.Plan)) {
		return ErrConflict
	}
	response := Response{EnrollmentID: e.ID, Sequence: command.Sequence, CommandDigest: digest(signed), PlanDigest: command.PlanDigest}
	switch command.Action {
	case ActionPrepare:
		var plan *Plan
		if command.Topology != nil && command.Bootstrap != nil {
			plan, err = c.executor.PrepareBootstrapWithTopology(ctx, command.Intent, *command.Bootstrap, *command.Topology)
		} else if command.Topology != nil {
			plan, err = c.executor.PrepareTopology(ctx, command.Intent, *command.Topology)
		} else if command.Bootstrap != nil {
			plan, err = c.executor.PrepareBootstrap(ctx, command.Intent, command.Changes, *command.Bootstrap)
		} else {
			plan, err = c.executor.Prepare(ctx, command.Intent, command.Changes)
		}
		if err == nil && command.PreviousTemplateStamp != previousTemplateStamp(plan.data) {
			return ErrConflict
		}
		if err == nil {
			journal.Plan = &record{Digest: plan.Digest(), Plan: plan.data}
			response.PlanDigest = plan.Digest()
			response.TemplateStamp = plan.TemplateStamp()
			response.Resources = plan.Resources()
		}
	case ActionSubmit:
		if journal.Plan == nil || journal.Plan.Digest != command.PlanDigest {
			return ErrConflict
		}
		plan := &Plan{data: journal.Plan.Plan, digest: journal.Plan.Digest}
		var receipt Receipt
		receipt, err = c.executor.Submit(ctx, command.Intent, command.PlanDigest, plan)
		if err == nil {
			response.Receipt = &receipt
			response.Resources = receipt.Resources
		}
	case ActionObserve:
		var receipt Receipt
		receipt, err = c.executor.Observe(ctx, command.Intent, command.PlanDigest, command.Acknowledgement)
		if err == nil {
			response.Receipt = &receipt
			response.Resources = receipt.Resources
		}
	case ActionRestore:
		if journal.Plan == nil {
			return ErrConflict
		}
		var receipt Receipt
		plan := &Plan{data: journal.Plan.Plan, digest: journal.Plan.Digest}
		receipt, err = c.executor.cancelPrepared(ctx, command.Intent, command.PlanDigest, plan)
		if errors.Is(err, ErrConflict) {
			receipt, err = c.executor.Restore(ctx, command.Intent, command.PlanDigest)
		}
		if err == nil {
			response.Receipt = &receipt
			response.Resources = receipt.Resources
		}
	default:
		return ErrInvalid
	}
	if err != nil {
		// API outages and optimistic conflicts are retried from the same signed
		// command. They must not become a terminal response that skips recovery.
		if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrConflict) || ctx.Err() != nil {
			return err
		}
		response.Outcome = "unsupported"
	} else {
		response.Outcome = "complete"
	}
	journal.Response = &response
	putRecord(journalSecret, journalKey, journal)
	if _, err := c.client.CoreV1().Secrets(e.Target.Namespace).Update(ctx, journalSecret, metav1.UpdateOptions{}); err != nil {
		return apiError(err)
	}
	return c.writeResponse(ctx, response)
}

func (c *Controller) writeResponse(ctx context.Context, response Response) error {
	e := c.enrollment
	s, err := fixedSecret(ctx, c.client, e.Target.Namespace, e.ResponseSecret, e.ResponseSecretUID)
	if err != nil {
		return err
	}
	var current Response
	if decode(s.Data[responseKey], &current) == nil && digest(current) == digest(response) {
		return nil
	}
	putRecord(s, responseKey, response)
	if _, err := c.client.CoreV1().Secrets(e.Target.Namespace).Update(ctx, s, metav1.UpdateOptions{}); err != nil {
		return apiError(err)
	}
	return nil
}

func previousTemplateStamp(plan planData) string {
	if plan.Delta.BeforeStamp == nil {
		return ""
	}
	return *plan.Delta.BeforeStamp
}
