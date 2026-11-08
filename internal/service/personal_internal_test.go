package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Only a human's own ordinary session reaches the personal surface; every
// other caller gets the uniform nonexistent answer (personal-values ADR, Who
// can do what).
func TestRefusePersonalCallerAdmitsOnlyAnOrdinaryHumanSession(t *testing.T) {
	for name, tc := range map[string]struct {
		caller authz.Identity
		admit  bool
	}{
		"human session":        {authz.Identity{Class: domain.ClassHuman, SessionID: "ses_1", Artifact: "cli"}, true},
		"workspace session":    {authz.Identity{Class: domain.ClassHuman, SessionID: "ses_1", Artifact: authz.WorkspaceArtifact}, false},
		"developer credential": {authz.Identity{Class: domain.ClassDeveloper, SessionID: "ses_1"}, false},
		"workload":             {authz.Identity{Class: domain.ClassWorkload}, false},
		"automation":           {authz.Identity{Class: domain.ClassAutomation}, false},
		"sessionless human":    {authz.Identity{Class: domain.ClassHuman}, false},
	} {
		err := refusePersonalCaller(tc.caller)
		if tc.admit != (err == nil) || (err != nil && !errors.Is(err, domain.ErrNotFound)) {
			t.Errorf("%s: refusal = %v", name, err)
		}
	}
}
