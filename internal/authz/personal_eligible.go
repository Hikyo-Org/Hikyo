package authz

import (
	"context"
	"fmt"
)

// PersonalEligible counts the humans who currently hold read and reveal over
// the whole of the counts proof's environment: the matrix denominator
// (personal-values ADR, One complete surface). Each candidate is evaluated
// with the same grant and member-access-rule formula that decides developer
// delivery, so the count can never disagree with who could receive a value.
func (a *TxAuthorizer) PersonalEligible(ctx context.Context, p Proof) (int, error) {
	// Only a counts proof reaches the counts store door.
	c, err := verifyProof(p, StorePersonalReportCounts, a.tok)
	if err != nil {
		return 0, err
	}
	if c.kind != kindTenant || c.chain.Env == "" {
		return 0, fmt.Errorf("authz: personal eligibility needs an environment tenant proof")
	}
	candidates, err := a.r.PersonalEligibleCandidates(ctx, c.chain.Org)
	if err != nil {
		return 0, err
	}
	// shortcut: two resolution reads per candidate in the org, bounded by the
	// org's reveal holders; batch them if a matrix read grows slow.
	n := 0
	for _, id := range candidates {
		grants, err := a.r.Grants(ctx, id)
		if err != nil {
			return 0, err
		}
		rules, err := a.r.Rules(ctx, id)
		if err != nil {
			return 0, err
		}
		if developerEnvironmentAuthority(c.chain, grants, rules) {
			n++
		}
	}
	return n, nil
}
