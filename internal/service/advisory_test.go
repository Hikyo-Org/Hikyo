package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestAdvisoryConnectionCaps(t *testing.T) {
	t.Run("principal", func(t *testing.T) {
		a := NewAdvisory()
		var cancels []func()
		for i := 0; i < advisoryPrincipalLimit; i++ {
			_, cancel, err := a.subscribe("org", domain.ProjectID(fmt.Sprintf("project-%d", i)), "principal")
			if err != nil {
				t.Fatal(err)
			}
			cancels = append(cancels, cancel)
		}
		if _, _, err := a.subscribe("org", "overflow", "principal"); !errors.Is(err, ErrAdvisoryPrincipalLimit) {
			t.Fatalf("principal cap refusal = %v", err)
		}
		cancels[0]()
		if _, cancel, err := a.subscribe("org", "replacement", "principal"); err != nil {
			t.Fatalf("released principal slot stayed occupied: %v", err)
		} else {
			cancel()
		}
	})

	t.Run("organization", func(t *testing.T) {
		a := NewAdvisory()
		for i := 0; i < advisoryOrgLimit; i++ {
			if _, _, err := a.subscribe("org", "project", domain.PrincipalID(fmt.Sprintf("principal-%d", i))); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := a.subscribe("org", "project", "overflow"); !errors.Is(err, ErrAdvisoryOrgLimit) {
			t.Fatalf("organization cap refusal = %v", err)
		}
	})

	t.Run("instance", func(t *testing.T) {
		a := NewAdvisory()
		for i := 0; i < advisoryInstanceWideLimit; i++ {
			org := domain.OrgID(fmt.Sprintf("org-%d", i/advisoryOrgLimit))
			principal := domain.PrincipalID(fmt.Sprintf("principal-%d", i))
			if _, _, err := a.subscribe(org, "project", principal); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := a.subscribe("another-org", "project", "overflow"); !errors.Is(err, ErrAdvisoryInstanceLimit) {
			t.Fatalf("instance cap refusal = %v", err)
		}
	})
}

// A burst may disconnect a slow stream after 32 queued events. Reconnection
// starts empty: authorized refetch remains the recovery mechanism.
func TestAdvisoryBurstDropsAndReconnectDoesNotReplay(t *testing.T) {
	a := NewAdvisory()
	scope := domain.Scope{Org: "org", Project: "project"}
	raw, cancel, err := a.subscribe(scope.Org, scope.Project, "principal")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	changes := make([]ChangedKey, 300)
	for i := range changes {
		changes[i] = ChangedKey{KeyID: fmt.Sprintf("key_%d", i), Name: fmt.Sprintf("KEY_%d", i)}
	}
	a.published(scope, []PublishedEnvironment{{EnvironmentID: "env", Revision: 1, ChangedKeys: changes}})
	n := 0
	for ev := range raw {
		if n == 0 && ev.Type != AdvisoryPublished {
			t.Fatalf("first event=%+v", ev)
		}
		if n > 0 && ev.Type != AdvisoryChanged {
			t.Fatalf("changed event=%+v", ev)
		}
		n++
	}
	if n != advisoryBuffer || len(a.subs) != 0 {
		t.Fatalf("queued=%d subscribers=%d", n, len(a.subs))
	}
	replacement, release, err := a.subscribe(scope.Org, scope.Project, "principal")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case ev := <-replacement:
		t.Fatalf("reconnected stream replayed %+v", ev)
	default:
	}
}
