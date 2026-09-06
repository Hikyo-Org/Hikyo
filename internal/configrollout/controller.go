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
	public     ed25519.PublicKey
	executor   *Kubernetes
	now        func() time.Time
}

func NewController(client kubernetes.Interface, e Enrollment, public ed25519.PublicKey) (*Controller, error) {
	if !validEnrollment(e) || len(public) != ed25519.PublicKeySize {
		return nil, ErrInvalid
	}
	executor, err := NewKubernetes(client, e.Target)
	if err != nil {
		return nil, err
	}
	return &Controller{client: client, enrollment: cloneEnrollment(e), public: append(ed25519.PublicKey(nil), public...), executor: executor, now: time.Now}, nil
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
	if decode(commandSecret.Data[commandKey], &signed) != nil || !verifyCommand(signed, e, c.public) {
		return ErrInvalid
	}
	journalSecret, err := fixedSecret(ctx, c.client, e.Target.Namespace, e.JournalSecret, e.JournalSecretUID)
	if err != nil {
		return err
	}
	var journal controllerJournal
	if len(journalSecret.Data[journalKey]) > 0 {
		if decode(journalSecret.Data[journalKey], &journal) != nil || !verifyCommand(journal.Accepted, e, c.public) {
			return ErrConflict
		}
	}
	command := signed.Command
	accepted := journal.Accepted.Command.Sequence
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
