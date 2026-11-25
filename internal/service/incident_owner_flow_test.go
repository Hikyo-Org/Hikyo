package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// The lost-grant outcomes of the lease and target owners are pinned on the flow
// logic itself: each step is a stub, so the result does not depend on timing.

var errStore = errors.New("store failed")

func lost() error { return fmt.Errorf("%w: authority over the lease was lost", domain.ErrUnauthorized) }

type leaseStep struct {
	state  string
	absent bool
	err    error
}

// leaseScript replays reads in order and counts revokes.
func leaseScript(reads []leaseStep, revokeErr error) (read func() (string, bool, error), revoke func() error, revokes *int) {
	i, n := 0, 0
	read = func() (string, bool, error) {
		r := reads[i]
		if i < len(reads)-1 {
			i++
		}
		return r.state, r.absent, r.err
	}
	revoke = func() error { n++; return revokeErr }
	return read, revoke, &n
}

func TestRevokeLeaseFlow(t *testing.T) {
	masked := domain.ErrNotFound
	for _, c := range []struct {
		name      string
		reads     []leaseStep
		revokeErr error
		want      IncidentOwnerResult
		wantErr   error
		revokes   int
	}{
		{"absent is confirmed", []leaseStep{{absent: true}}, nil, OwnerConfirmed, nil, 0},
		{"a revoked lease is confirmed", []leaseStep{{state: "revoked"}}, nil, OwnerConfirmed, nil, 0},
		{"an active lease is queued and stays unconfirmed", []leaseStep{{state: "active"}, {state: "revoking"}}, nil, OwnerUnconfirmed, nil, 1},
		{"a revoking lease queues nothing more", []leaseStep{{state: "revoking"}}, nil, OwnerUnconfirmed, nil, 0},
		{"a lease gone after the revoke is confirmed", []leaseStep{{state: "active"}, {absent: true}}, nil, OwnerConfirmed, nil, 1},
		{"a grant lost before the first read is blocked", []leaseStep{{err: lost()}}, nil, OwnerBlocked, nil, 0},
		{"a grant lost before the confirming read is blocked", []leaseStep{{state: "active"}, {err: lost()}}, nil, OwnerBlocked, nil, 1},
		{"a revoke refused for authority is blocked", []leaseStep{{state: "active"}}, lost(), OwnerBlocked, nil, 1},
		{"a revoke that is masked not found is settled by the confirming read: lost grant", []leaseStep{{state: "active"}, {err: lost()}}, masked, OwnerBlocked, nil, 1},
		{"a revoke that is masked not found is settled by the confirming read: lease gone", []leaseStep{{state: "active"}, {absent: true}}, masked, OwnerConfirmed, nil, 1},
		{"a store error on the first read is not absence", []leaseStep{{err: errStore}}, nil, OwnerUnconfirmed, errStore, 0},
		{"a store error on the confirming read is not absence", []leaseStep{{state: "active"}, {err: errStore}}, nil, OwnerUnconfirmed, errStore, 1},
		{"a store error on the revoke is unconfirmed", []leaseStep{{state: "active"}}, errStore, OwnerUnconfirmed, errStore, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			read, revoke, revokes := leaseScript(c.reads, c.revokeErr)
			got, err := revokeLeaseFlow(read, revoke)
			if got != c.want || !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("= %v, %v; want %v, %v", got, err, c.want, c.wantErr)
			}
			if *revokes != c.revokes {
				t.Fatalf("revokes = %d, want %d", *revokes, c.revokes)
			}
		})
	}
}

func TestPauseTargetFlow(t *testing.T) {
	pause := func(paused bool, err error) func() (bool, error) { return func() (bool, error) { return paused, err } }
	absent := func(gone bool, err error) func() (bool, error) { return func() (bool, error) { return gone, err } }
	for _, c := range []struct {
		name    string
		pause   func() (bool, error)
		absent  func() (bool, error)
		want    IncidentOwnerResult
		wantErr error
	}{
		{"paused is confirmed", pause(true, nil), nil, OwnerConfirmed, nil},
		{"a pause that did not stick is unconfirmed", pause(false, nil), nil, OwnerUnconfirmed, nil},
		{"a target that is gone is confirmed", pause(false, domain.ErrNotFound), absent(true, nil), OwnerConfirmed, nil},
		{"a lost grant behind the masked not found is blocked", pause(false, domain.ErrNotFound), absent(false, lost()), OwnerBlocked, nil},
		{"a target that exists but could not be paused is a conflict", pause(false, domain.ErrNotFound), absent(false, nil), OwnerUnconfirmed, domain.ErrConflict},
		{"a store error behind the masked not found is not absence", pause(false, domain.ErrNotFound), absent(false, errStore), OwnerUnconfirmed, errStore},
		{"a pause refused for authority is blocked", pause(false, lost()), nil, OwnerBlocked, nil},
		{"a store error on the pause is unconfirmed", pause(false, errStore), nil, OwnerUnconfirmed, errStore},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := pauseTargetFlow(c.pause, c.absent)
			if got != c.want || !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("= %v, %v; want %v, %v", got, err, c.want, c.wantErr)
			}
		})
	}
}
