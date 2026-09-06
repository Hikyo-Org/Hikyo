package isolation

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"k8s.io/apimachinery/pkg/types"
)

// auditDeployment models an enrolled executor that prepares a replacement but
// cannot complete it. The service must independently authorize and audit Restore.
func newAuditDeployment(owner, incarnation string) *auditDeployment {
	return &auditDeployment{identity: service.DeploymentIdentity{EnrollmentID: "audit-enrolled", OwnerInstanceID: owner, Incarnation: incarnation, DeploymentUID: "audit-deployment", TemplateStamp: "original-template"}, sent: make(map[uint64]bool)}
}

type auditDeployment struct {
	mu          sync.Mutex
	identity    service.DeploymentIdentity
	installed   bool
	sent        map[uint64]bool
	submissions int
}

func (p *auditDeployment) RootPreparation(context.Context, configrollout.SignedCommand) (*crypto.WrappedKey, error) {
	return nil, nil
}
func (p *auditDeployment) SeedSources(context.Context) (config.ManagedBootstrapSources, error) {
	return config.ManagedBootstrapSources{}, nil
}
func (p *auditDeployment) Identity() service.DeploymentIdentity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.identity
}
func (p *auditDeployment) PrepareCommand(_ context.Context, intent configrollout.Intent, _ *runtimeconfig.Bundle, sequence uint64) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{Command: configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: configrollout.ActionPrepare, Intent: intent}, Signature: make([]byte, 64)}, nil
}
func (p *auditDeployment) DecisionCommand(_ context.Context, prepared configrollout.SignedCommand, action configrollout.Action, sequence uint64, digest string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	return configrollout.SignedCommand{Command: configrollout.Command{EnrollmentID: p.Identity().EnrollmentID, Sequence: sequence, Action: action, Intent: prepared.Command.Intent, PlanDigest: digest, Acknowledgement: ack}, Signature: make([]byte, 64)}, nil
}
func (p *auditDeployment) RenewCommand(_ context.Context, committed configrollout.SignedCommand, sequence uint64) (configrollout.SignedCommand, error) {
	committed.Command.Sequence = sequence
	committed.Command.IssuedAt = time.Now().UTC()
	committed.Command.ExpiresAt = committed.Command.IssuedAt.Add(5 * time.Minute)
	return committed, nil
}
func (p *auditDeployment) Send(_ context.Context, c configrollout.SignedCommand) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[c.Command.Sequence] && c.Command.Action == configrollout.ActionSubmit {
		p.submissions++
	}
	p.sent[c.Command.Sequence] = true
	return nil
}
func (p *auditDeployment) Response(_ context.Context, c configrollout.SignedCommand) (configrollout.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sent[c.Command.Sequence] {
		return configrollout.Response{}, configrollout.ErrNotSubmitted
	}
	response := configrollout.Response{EnrollmentID: p.identity.EnrollmentID, Sequence: c.Command.Sequence, Outcome: "complete", PlanDigest: strings.Repeat("a", 64), TemplateStamp: "replacement-template"}
	if c.Command.Action != configrollout.ActionPrepare {
		phase := configrollout.RolloutRequested
		if p.installed {
			phase = configrollout.RolloutReady
		}
		if c.Command.Acknowledgement != nil && p.installed {
			phase = configrollout.Applied
		}
		response.Receipt = &configrollout.Receipt{Intent: c.Command.Intent, PlanDigest: c.Command.PlanDigest, DeploymentUID: types.UID(p.identity.DeploymentUID), Phase: phase, ApplicationAcknowledged: phase == configrollout.Applied}
	}
	return response, nil
}
func (p *auditDeployment) VerifyInstalled(_ context.Context, bundle *runtimeconfig.Bundle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if bundle.BootstrapSources() == (config.ManagedBootstrapSources{}) || p.installed {
		return nil
	}
	return service.ErrDeploymentSourcesPending
}
