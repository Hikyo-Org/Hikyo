package isolation

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// The contract cross-check.
//
// api/openapi.yaml records, per operation, the probe class, the authz
// operation it reaches, that operation's formula, and its artifact
// eligibility. The api-cli-surface ADR says the behavioural half of the
// freeze promise "rests on review of a hand-written spec" — and it does, but
// the parts a machine CAN check must be checked, or the document quietly
// becomes a description of a system that no longer exists.
//
// Generated wire metadata is checked by generator freshness and negative
// fixtures, not by comparisons with its own OpenAPI source. Actual router
// classification is checked by TestInvariant01ClassificationTotality.
//
// What this proves: the document and the independent operation formulas
// describe the same authorization posture. What it cannot prove: that either
// is the posture the ADR intended. That remains review, stated as such.

var levelNames = map[domain.Level]string{
	domain.LevelNone: "instance", domain.LevelOrg: "org",
	domain.LevelProject: "project", domain.LevelEnv: "environment",
}

func TestContractFormulasMatchTheOperationRegistry(t *testing.T) {
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	formulas := facts.Formulas()
	for id, op := range ops {
		if op.AuthzOp == "" {
			continue
		}
		operation := authz.Operation(op.AuthzOp)
		formula, ok := formulas[operation]
		if !ok {
			t.Errorf("%s: contract names authz operation %q, which is not registered", id, op.AuthzOp)
			continue
		}
		want := make([]string, 0, len(formula))
		for _, atom := range formula {
			want = append(want, string(atom.Cap)+"@"+levelNames[atom.At])
		}
		got := op.Formula()
		slices.Sort(got)
		slices.Sort(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: contract records formula %v, the operation registry evaluates %v — the freeze promise covers behaviour, and this is where the two would silently diverge",
				id, got, want)
		}
	}
}

func TestContractSecuredOperationsTakeAnArtifact(t *testing.T) {
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	for id, op := range ops {
		// Every verb declares its eligible artifact set as a closed matrix, and
		// machine-credential eligibility is now REAL (#62's delivery surface)
		// rather than a promise nothing keeps. So the check inverts: a route may
		// declare it only if a machine principal could actually satisfy its
		// formula.
		//
		// The test is the normative allowlist itself — a machine class may hold
		// only the capabilities #55 admits for it — so a route claiming machine
		// eligibility under, say, `manage-identities` fails here rather than
		// advertising an authority no machine can ever hold. That is the property
		// the old blanket refusal was standing in for.
		artifacts := op.Artifacts()
		for _, artifact := range artifacts {
			if artifact != "machine-credential" {
				continue
			}
			if op.AuthzOp == "" {
				t.Errorf("%s declares machine-credential eligibility but reaches no registered operation", id)
				continue
			}
			if !machineSatisfiable(authz.Operation(op.AuthzOp)) {
				t.Errorf("%s declares machine-credential eligibility, but no machine class may hold its formula %v",
					id, op.Formula())
			}
		}
		if op.Secured && len(artifacts) == 1 && artifacts[0] == "none" {
			t.Errorf("%s: secured but eligible for no artifact", id)
		}
	}
}

// machineSatisfiable reports whether SOME machine class may hold every atom of
// an operation's formula, under #55's normative per-class allowlists.
//
// It asks "some class", not "every class": a workload holds `read` and an
// automation holds more, so a route reachable by one of them is legitimately
// machine-eligible. What it refuses is a route whose formula no machine class
// can ever satisfy — which is what a stale or aspirational eligibility
// declaration looks like.
func machineSatisfiable(op authz.Operation) bool {
	formula := facts.Formulas()[op]
	if len(formula) == 0 {
		return false
	}
	for _, class := range domain.MachineClasses() {
		ok := true
		for _, atom := range formula {
			// This asks whether the formula is ever satisfiable. Reveal can
			// be granted under the live project opt-in even though it is not
			// in the unconditional machine allowlist. Runtime checks still
			// require that opt-in and grants on both sides of a secret copy.
			// Pin-bound reveal-history does not admit a generic historical
			// operation; machines exercise that delegation through delivery.
			conditionalReveal := atom.Cap == domain.CapReveal && domain.MachineMayHoldRevealByOptIn(class)
			if !domain.MachineMayHold(class, atom.Cap) && !conditionalReveal {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestMachineFormulaSatisfiabilityIncludesConditionalReveal(t *testing.T) {
	for _, tc := range []struct {
		op   authz.Operation
		want bool
	}{
		{authz.OpEnvRead, true},
		{authz.OpValueCopyDestination, true},
		{authz.OpValueCopyDestinationConfig, true},
		{authz.OpSelfConfigTest, false},
		{authz.OpValueExportRevealHistory, false},
	} {
		t.Run(string(tc.op), func(t *testing.T) {
			if got := machineSatisfiable(tc.op); got != tc.want {
				t.Fatalf("machine satisfiable = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestTenantRoutesDeclareForbiddenOnlyForMFA is the registry-aware half of the
// tenant-refusal contract, and it is an IFF in both directions.
//
// Grant refusal on a tenant-scoped operation is the uniform 404 — authorize()
// returns ErrNotFound there, so a declared 403 would either be dead or a leak.
// The one exception is the assurance leg: a caller who HOLDS an MFA-mandatory
// capability but presents a single-factor session is refused with
// ErrUnauthorized, deliberately, because they can already reach the object and
// hiding it would tell a capability holder it is missing. So:
//
//   - MFA-mandatory tenant operation  => 403 MUST be declared (the code can
//     produce it, and an undeclared status is a contract the server breaks).
//   - a reviewed dynamic post-grant refusal => 403 MUST be declared (adapter
//     operations refine the static project formula with affected-environment
//     reveal and reauthentication checks after the tenant object resolves).
//   - every other tenant operation    => 403 MUST NOT be declared (unreachable,
//     and declaring it invites a handler to start answering it).
func TestTenantRoutesDeclareForbiddenOnlyForMFA(t *testing.T) {
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	for id, op := range ops {
		if op.Class != "tenant" || op.AuthzOp == "" {
			continue
		}
		item := doc.Paths.Find(op.Path)
		if item == nil {
			t.Fatalf("%s: contract path %q vanished", id, op.Path)
		}
		operation := item.GetOperation(op.Method)
		if operation == nil || operation.Responses == nil {
			t.Fatalf("%s: no operation at %s %s", id, op.Method, op.Path)
		}
		declared := operation.Responses.Status(http.StatusForbidden) != nil
		// Artifact-class mismatch is handled before grant evaluation and always
		// uses the uniform nonexistent response. A tenant-class route therefore
		// declares 403 only for the post-grant MFA assurance floor.
		operationID := authz.Operation(op.AuthzOp)
		pins := facts.FormulaPins()
		wanted := authz.FormulaDemandsMFA(operationID)
		for _, pin := range pins {
			if pin.Operation == string(operationID) {
				wanted = wanted || pin.PostGrantForbidden
				break
			}
		}
		switch {
		case wanted && !declared:
			t.Errorf("%s is tenant-class with an MFA-mandatory post-grant refusal (formula %v) but declares no 403 — the refusal it can return is undeclared",
				id, op.Formula())
		case !wanted && declared:
			t.Errorf("%s (formula %v) is tenant-class with no post-grant refusal but declares a 403 — grant refusal there is the uniform 404, so the status is unreachable or a leak",
				id, op.Formula())
		}
	}
}
