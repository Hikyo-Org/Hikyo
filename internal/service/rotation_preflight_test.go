package service

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	storekeyring "github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

type rotationSourceProbe struct {
	reads   atomic.Int64
	next    func(context.Context) ([]byte, error)
	current func(context.Context) ([]byte, error)
}

func (p *rotationSourceProbe) Current(ctx context.Context) ([]byte, error) {
	p.reads.Add(1)
	if p.current != nil {
		return p.current(ctx)
	}
	return nil, errors.New("primary root source must not be read")
}

func (p *rotationSourceProbe) Next(ctx context.Context) ([]byte, error) {
	p.reads.Add(1)
	if p.next != nil {
		return p.next(ctx)
	}
	return nil, errors.New("new root source must not be read")
}

func rootRotationWhileLockHeld(t *testing.T, rotation *Rotation, actor Actor, phase RootKeyRotationPhase) error {
	t.Helper()
	rotation.Keyring.LockHierarchyRotation()
	defer rotation.Keyring.UnlockHierarchyRotation()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := rotation.RotateRootKey(ctx, actor, phase)
		result <- err
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal("refused root rotation waited for the hierarchy lock before admission")
		return ctx.Err()
	}
}

func TestRootRotationPreflightRefusesBeforeLockOrRootSource(t *testing.T) {
	s, _ := selfConfigFixture(t)
	if _, err := s.DB.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO principals (id,kind,created_at) VALUES ('usr_no_rotation','human','2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var budget *Budget
		if enabled {
			budget = NewBudget()
		}
		for _, phase := range []RootKeyRotationPhase{RootRotatePrepare, RootRotateVerify, RootRotateFinalize} {
			for _, caller := range []struct {
				name  string
				actor Actor
				want  error
			}{
				{"anonymous", Bearer(""), domain.ErrUnauthenticated},
				{"no-capability", LocalPrincipal("usr_no_rotation"), domain.ErrUnauthorized},
			} {
				t.Run(caller.name+"/"+string(phase)+map[bool]string{false: "/no-budget", true: "/budget"}[enabled], func(t *testing.T) {
					source := &rotationSourceProbe{}
					rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: source, Budget: budget}
					if err := rootRotationWhileLockHeld(t, rotation, caller.actor, phase); !errors.Is(err, caller.want) {
						t.Fatalf("root rotation = %v, want %v", err, caller.want)
					}
					if source.reads.Load() != 0 {
						t.Fatal("refused caller read a privileged root source")
					}
				})
			}
		}
		if budget != nil {
			budget.mu.Lock()
			charged := len(budget.rate) != 0 || len(budget.inflight) != 0
			budget.mu.Unlock()
			if charged {
				t.Fatal("refused callers consumed an authenticated operation budget")
			}
		}
	}
}

func TestRootRotationAnonymousOutcomeDoesNotExposeConfigurationOrPhase(t *testing.T) {
	s, _ := selfConfigFixture(t)
	for _, phase := range []RootKeyRotationPhase{RootRotatePrepare, RootRotateVerify, RootRotateFinalize} {
		for _, configured := range []string{"missing-keyring", "missing-source", "source-failure", "not-dual-wrapped"} {
			t.Run(string(phase)+"/"+configured, func(t *testing.T) {
				rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: &rotationSourceProbe{}}
				switch configured {
				case "missing-keyring":
					rotation.Keyring = nil
				case "missing-source":
					rotation.RootKey = nil
				case "not-dual-wrapped":
					rotation.RootKey = &rotationSourceProbe{next: func(context.Context) ([]byte, error) {
						return bytes.Repeat([]byte{0x42}, crypto.KeySize), nil
					}}
				}
				if _, err := rotation.RotateRootKey(t.Context(), Bearer(""), phase); !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("anonymous phase/configuration oracle = %v, want uniform unauthenticated", err)
				}
			})
		}
	}
}

func TestRootRotationAnonymousDoesNotExposeWrapperState(t *testing.T) {
	s, actor := selfConfigFixture(t)
	readNew := func(context.Context) ([]byte, error) {
		return bytes.Repeat([]byte{0x42}, crypto.KeySize), nil
	}
	source := &rotationSourceProbe{next: readNew, current: readNew}
	rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: source, Budget: NewBudget()}
	assertAnonymous := func() {
		t.Helper()
		reads := source.reads.Load()
		for _, phase := range []RootKeyRotationPhase{RootRotatePrepare, RootRotateVerify, RootRotateFinalize} {
			if _, err := rotation.RotateRootKey(t.Context(), Bearer(""), phase); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("anonymous %s exposed wrapper state: %v", phase, err)
			}
		}
		if source.reads.Load() != reads {
			t.Fatal("anonymous phase oracle read a root source")
		}
	}
	if _, err := rotation.RotateRootKey(t.Context(), actor, RootRotateVerify); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("authorized single-wrapper verify = %v, want phase conflict", err)
	}
	assertAnonymous()
	if _, err := rotation.RotateRootKey(t.Context(), actor, RootRotatePrepare); err != nil {
		t.Fatal(err)
	}
	if _, err := rotation.RotateRootKey(t.Context(), actor, RootRotateVerify); err != nil {
		t.Fatalf("authorized dual-wrapped verify refused installed new root: %v", err)
	}
	assertAnonymous()
}

func TestRootRotationBudgetRefusesBeforeLockAndRootSource(t *testing.T) {
	s, actor := selfConfigFixture(t)
	budget := NewBudget()
	for range BudgetDefaultRatePerMin {
		release, err := budget.acquire(budgetDefault, budgetKeys{Principal: actor.principal})
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	source := &rotationSourceProbe{}
	rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: source, Budget: budget}
	for _, phase := range []RootKeyRotationPhase{RootRotatePrepare, RootRotateVerify, RootRotateFinalize} {
		if err := rootRotationWhileLockHeld(t, rotation, actor, phase); !errors.Is(err, admission.ErrOverloaded) {
			t.Fatalf("exhausted root rotation budget = %v, want overload before lock", err)
		}
	}
	if source.reads.Load() != 0 {
		t.Fatal("budget-refused rotation read root sources")
	}
}

func TestRootRotationReauthorizesAfterRootSourceRead(t *testing.T) {
	s, actor := selfConfigFixture(t)
	ks := &storekeyring.Store{DB: s.DB}
	before, err := ks.ActiveMasterWrappers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source := &rotationSourceProbe{next: func(ctx context.Context) ([]byte, error) {
		write, err := s.DB.SQLiteWrite().BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer write.Rollback()
		if _, err := write.ExecContext(ctx, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id=? AND capability=?)`, actor.principal, domain.CapRotateRootKey); err != nil {
			return nil, err
		}
		if _, err := write.ExecContext(ctx, `DELETE FROM grants WHERE principal_id=? AND capability=?`, actor.principal, domain.CapRotateRootKey); err != nil {
			return nil, err
		}
		if err := write.Commit(); err != nil {
			return nil, err
		}
		return bytes.Repeat([]byte{0x42}, crypto.KeySize), nil
	}}
	rotation := &Rotation{DB: s.DB, Keyring: s.Keyring, RootKey: source, Budget: NewBudget()}
	if _, err := rotation.RotateRootKey(t.Context(), actor, RootRotatePrepare); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("revoked committing authority = %v, want unauthorized", err)
	}
	if source.reads.Load() != 1 {
		t.Fatal("authorized preflight did not reach the revocation barrier")
	}
	after, err := ks.ActiveMasterWrappers(t.Context())
	if err != nil || len(after) != len(before) {
		t.Fatalf("revoked rotation changed master wrappers: before=%d after=%d, %v", len(before), len(after), err)
	}
}
