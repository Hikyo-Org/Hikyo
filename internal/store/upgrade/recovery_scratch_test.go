package upgrade

import "testing"

func TestAutomaticDrillRefusesLiveRecoveryAuthority(t *testing.T) {
	for _, admission := range []RecoveryAdmission{{}, {state: &recoveryState{kind: restoredData, epoch: 1}}} {
		if err := admission.CheckScratch(); err == nil {
			t.Fatal("automatic drill admitted non-scratch recovery")
		}
	}
}
