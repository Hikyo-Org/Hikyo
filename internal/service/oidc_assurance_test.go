package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestEvaluateAssuranceRejectsEmptyOrMalformedPolicyValues(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"empty ACR":        `{"acr_values":[""]}`,
		"whitespace ACR":   `{"acr_values":[" mfa"]}`,
		"empty AMR":        `{"amr_sets":[[""]]}`,
		"empty AMR set":    `{"amr_sets":[[]]}`,
		"unknown field":    `{"other":["mfa"]}`,
		"trailing content": `{"acr_values":["mfa"]}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			policy := raw
			matched, err := evaluateAssurance(&policy, "", nil)
			if matched || !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("evaluateAssurance() = %v, %v; want false, ErrInvalid", matched, err)
			}
		})
	}
}

func TestValidateAssurancePolicyCanonicalizesAndBoundsCollections(t *testing.T) {
	t.Parallel()

	policy := " { \"amr_sets\" : [[\"otp\",\"pwd\"]], \"acr_values\" : [\"strong\"] } "
	canonical, err := validateAssurancePolicy(&policy)
	if err != nil {
		t.Fatal(err)
	}
	if *canonical != `{"acr_values":["strong"],"amr_sets":[["otp","pwd"]]}` {
		t.Fatalf("canonical policy = %q", *canonical)
	}

	tooMany := `{"acr_values":["` + strings.Join(make([]string, maxAssurancePolicyItems+1), `","`) + `"]}`
	if _, err := validateAssurancePolicy(&tooMany); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized policy error = %v; want ErrInvalid", err)
	}
}

func TestEvaluateAssuranceRequiresPresentedEvidence(t *testing.T) {
	t.Parallel()

	policy := `{"acr_values":["strong"],"amr_sets":[["otp","pwd"]]}`
	if matched, err := evaluateAssurance(&policy, "", nil); err != nil || matched {
		t.Fatalf("missing evidence = %v, %v; want false, nil", matched, err)
	}
	if matched, err := evaluateAssurance(&policy, "strong", nil); err != nil || !matched {
		t.Fatalf("matching ACR = %v, %v; want true, nil", matched, err)
	}
}
