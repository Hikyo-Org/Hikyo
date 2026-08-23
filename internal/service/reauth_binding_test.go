package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

func TestWindowBindingKind(t *testing.T) {
	tests := []struct {
		name               string
		window             authz.ReauthWindow
		wantKind           reauthWindowBindingKind
		wantPurpose        ReauthPurpose
		wantOperation      authz.Operation
		wantKeySet         string
		wantEnvironmentSet string
		wantError          bool
	}{
		{name: "unbound", wantKind: reauthWindowUnbound},
		{
			name: "operation bound",
			window: authz.ReauthWindow{
				BoundOperation: string(authz.OpValueReveal),
				BoundKeySet:    "API_KEY\nDATABASE_URL",
			},
			wantKind:      reauthWindowOperationBound,
			wantOperation: authz.OpValueReveal,
			wantKeySet:    "API_KEY\nDATABASE_URL",
		},
		{
			name: "adapter bound",
			window: authz.ReauthWindow{
				BoundPurpose:        string(PurposeAdapter),
				BoundOperation:      string(authz.OpAdapterSync),
				BoundEnvironmentSet: "env_prod\nenv_stage",
			},
			wantKind:           reauthWindowAdapterBound,
			wantPurpose:        PurposeAdapter,
			wantOperation:      authz.OpAdapterSync,
			wantEnvironmentSet: "env_prod\nenv_stage",
		},
		{
			name:      "key set without operation",
			window:    authz.ReauthWindow{BoundKeySet: "DATABASE_URL"},
			wantError: true,
		},
		{
			name: "purpose without environment set",
			window: authz.ReauthWindow{
				BoundPurpose:   string(PurposeAdapter),
				BoundOperation: string(authz.OpAdapterSync),
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, binding, err := windowBindingKind(test.window)
			if test.wantError {
				if err == nil {
					t.Fatal("windowBindingKind() error = nil, want refusal")
				}
				return
			}
			if err != nil {
				t.Fatalf("windowBindingKind() error = %v", err)
			}
			if kind != test.wantKind {
				t.Errorf("kind = %v, want %v", kind, test.wantKind)
			}
			if binding.purpose != test.wantPurpose || binding.operation != test.wantOperation ||
				binding.keySet != test.wantKeySet || binding.environmentSet != test.wantEnvironmentSet {
				t.Errorf("binding = %+v, want purpose=%q operation=%q keySet=%q environmentSet=%q",
					binding, test.wantPurpose, test.wantOperation, test.wantKeySet, test.wantEnvironmentSet)
			}
		})
	}
}
