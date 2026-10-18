package auditrow

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func TestResolveActorClassRefusalOrder(t *testing.T) {
	for _, allow := range []bool{false, true} {
		for _, class := range []audit.ActorClass{audit.ActorHuman, audit.ActorMachine, audit.ActorSystem, audit.ActorBreakGlass, audit.ActorUnauthenticated} {
			t.Run(string(class)+map[bool]string{false: "/principal", true: "/system"}[allow], func(t *testing.T) {
				calls := 0
				lookup := func(context.Context, string) (string, error) { calls++; return "human", nil }
				actor := audit.Actor{Class: class, ID: "actor", CredentialID: "credential"}
				got, err := ResolveActorClass(t.Context(), lookup, actor, allow)
				assertedAllowed := allow && class != audit.ActorHuman && class != audit.ActorMachine
				if (err == nil) != assertedAllowed {
					t.Fatalf("allowed=%v: %v", assertedAllowed, err)
				}
				if calls != 0 {
					t.Fatal("assertion reached authoritative lookup")
				}
				if assertedAllowed && got != actor {
					t.Fatal("system-proof attribution changed")
				}
			})
		}
	}
}

func TestResolveActorClassAuthoritativeFailures(t *testing.T) {
	sentinel := errors.New("lookup failed")
	actor := audit.Actor{ID: "actor", CredentialID: "credential"}
	for _, tc := range []struct {
		name, kind string
		err        error
		want       audit.ActorClass
		fail       bool
	}{
		{name: "human", kind: "human", want: audit.ActorHuman},
		{name: "machine", kind: "machine", want: audit.ActorMachine},
		{name: "deleted", err: sql.ErrNoRows, want: audit.ActorUnauthenticated},
		{name: "unknown", kind: "other", fail: true},
		{name: "failure", err: sentinel, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveActorClass(t.Context(), func(context.Context, string) (string, error) { return tc.kind, tc.err }, actor, false)
			if (err != nil) != tc.fail {
				t.Fatalf("want failure=%v: %v", tc.fail, err)
			}
			if !tc.fail && got.Class != tc.want {
				t.Fatalf("class %q != %q", got.Class, tc.want)
			}
			if tc.err == nil && !tc.fail && (got.ID != actor.ID || got.CredentialID != actor.CredentialID) {
				t.Fatal("principal attribution changed")
			}
			if tc.err == sql.ErrNoRows && (got.ID != "" || got.CredentialID != "") {
				t.Fatal("deleted principal kept attribution")
			}
			if tc.err == sentinel && !errors.Is(err, sentinel) {
				t.Fatal("lookup error was hidden")
			}
		})
	}
}
