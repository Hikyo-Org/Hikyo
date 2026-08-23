package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// TestWindowBindingKind pins the fail-closed classifier: the three real consent
// shapes resolve to their kind, and every partial or impossible combination is
// refused rather than read as the most permissive UNBOUND grant (the fail-open
// a key-set-only window caused before the classifier existed).
func TestWindowBindingKind(t *testing.T) {
	cases := []struct {
		name   string
		window authz.ReauthWindow
		want   windowBinding
		err    bool
	}{
		{
			name:   "unbound",
			window: authz.ReauthWindow{},
			want:   windowUnbound,
		},
		{
			name:   "operation bound over a key set",
			window: authz.ReauthWindow{BoundOperation: "key.reveal", BoundKeySet: "DATABASE_URL"},
			want:   windowOperationBound,
		},
		{
			name:   "operation bound with no key set",
			window: authz.ReauthWindow{BoundOperation: "key.reveal"},
			want:   windowOperationBound,
		},
		{
			name:   "adapter bound",
			window: authz.ReauthWindow{BoundPurpose: "adapter", BoundOperation: "adapter.sync", BoundEnvironmentSet: "env-1\nenv-2"},
			want:   windowAdapterBound,
		},
		{
			name:   "key set only",
			window: authz.ReauthWindow{BoundKeySet: "DATABASE_URL"},
			err:    true,
		},
		{
			name:   "purpose without environment set",
			window: authz.ReauthWindow{BoundPurpose: "adapter", BoundOperation: "adapter.sync"},
			err:    true,
		},
		{
			name:   "environment set without purpose",
			window: authz.ReauthWindow{BoundOperation: "adapter.sync", BoundEnvironmentSet: "env-1"},
			err:    true,
		},
		{
			name:   "adapter shape carrying a key set",
			window: authz.ReauthWindow{BoundPurpose: "adapter", BoundOperation: "adapter.sync", BoundEnvironmentSet: "env-1", BoundKeySet: "DATABASE_URL"},
			err:    true,
		},
		{
			name:   "purpose only",
			window: authz.ReauthWindow{BoundPurpose: "reveal"},
			err:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := windowBindingKind(tc.window)
			if tc.err {
				if !errors.Is(err, ErrReauthUnitMismatch) {
					t.Fatalf("want ErrReauthUnitMismatch, got kind=%v err=%v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want kind %v, got %v", tc.want, got)
			}
		})
	}
}
