package app

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The production incident service installs the owners it resumes through: an
// uninstalled owner would leave its items open after Resume.
func TestProductionIncidentsInstallTheirOwners(t *testing.T) {
	svc := newIncidents(nil, nil, &service.Auth{}, &service.Adapters{}, &service.Dynamic{}, &service.SCIM{}, &service.EmailInvitations{})
	for _, owner := range []string{
		domain.OwnerAccountContain, domain.OwnerSessionRevoke, domain.OwnerDeveloperRevoke, domain.OwnerRotationHalt,
		domain.OwnerMachineCredential, domain.OwnerEmailInvitation, domain.OwnerSCIMCredential,
		domain.OwnerLeaseRevoke, domain.OwnerAdapterTargetPause,
	} {
		if svc.Owners[owner] == nil {
			t.Errorf("production incident service has no owner for %s", owner)
		}
	}
}
