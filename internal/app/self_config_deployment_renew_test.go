package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"k8s.io/apimachinery/pkg/types"
)

func TestBootstrapDeploymentRenewalPreservesCommittedAuthorityAfterRestart(t *testing.T) {
	d, _, client := deploymentAdapterFixture(t, false)
	intent := deploymentIntent(d)
	prepare, err := configrollout.SignCommand(t.Context(), d.signer, configrollout.Command{EnrollmentID: d.enrollment.ID, Sequence: 1, Action: configrollout.ActionPrepare, Intent: intent, Changes: []configrollout.Change{{Variable: configrollout.AdmissionBudget, Value: "384"}}, IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-55 * time.Minute)})
	if err != nil || !d.validSigned(prepare) {
		t.Fatalf("prepare fixture: %v", err)
	}
	if _, err := d.RenewCommand(t.Context(), prepare, 2); err == nil {
		t.Fatal("pre-MFA preparation renewed without fresh source proof")
	}
	for _, action := range []configrollout.Action{configrollout.ActionSubmit, configrollout.ActionRestore, configrollout.ActionObserve} {
		t.Run(string(action), func(t *testing.T) {
			command := configrollout.Command{EnrollmentID: d.enrollment.ID, Sequence: 2, Action: action, Intent: intent, PlanDigest: strings.Repeat("a", 64), IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-55 * time.Minute)}
			if action == configrollout.ActionObserve {
				command.Acknowledgement = &configrollout.ApplicationAcknowledgement{Intent: intent, PlanDigest: command.PlanDigest, DeploymentUID: types.UID(d.identity.DeploymentUID), ReadyReplicas: 1}
			}
			committed, err := configrollout.SignCommand(t.Context(), d.signer, command)
			if err != nil {
				t.Fatal(err)
			}
			// Recovered signer/enrollment has no private preparation cache or files.
			d.proofs = nil
			d.sourcesDirectory = "/must-not-read"
			renewed, err := d.RenewCommand(t.Context(), committed, 3)
			if err != nil || !d.validSigned(renewed) || !time.Now().Before(renewed.Command.ExpiresAt) {
				t.Fatalf("renewal failed: %v", err)
			}
			normalized := renewed.Command
			normalized.Sequence, normalized.IssuedAt, normalized.ExpiresAt = command.Sequence, command.IssuedAt, command.ExpiresAt
			before, _ := json.Marshal(command)
			after, _ := json.Marshal(normalized)
			if string(before) != string(after) {
				t.Fatal("renewal altered durable decision")
			}
			if len(client.Actions()) != 0 {
				t.Fatal("renewal sent before service commit")
			}
			for _, mutation := range []string{"intent", "digest", "action", "sequence", "cancelled"} {
				t.Run(mutation, func(t *testing.T) {
					bad := committed
					sequence := uint64(4)
					ctx := t.Context()
					switch mutation {
					case "intent":
						bad.Command.Intent.Incarnation = "another"
					case "digest":
						bad.Command.PlanDigest = strings.Repeat("b", 64)
					case "action":
						bad.Command.Action = configrollout.ActionPrepare
					case "sequence":
						sequence = bad.Command.Sequence
					case "cancelled":
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(ctx)
						cancel()
					}
					if _, err := d.RenewCommand(ctx, bad, sequence); err == nil {
						t.Fatal("invalid renewal accepted")
					}
				})
			}
		})
	}
}
