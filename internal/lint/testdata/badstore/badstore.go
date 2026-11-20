// Package badstore is a negative fixture for the proof-signature analyzer:
// a repository surface that omits proofs and smuggles tenant-typed
// identifiers, both directly and through a struct field.
package badstore

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

type Filter struct {
	Org domain.OrgID // transitive smuggle
}

type WidgetRepo interface {
	// Missing proof parameter entirely.
	Get(ctx context.Context, id string) (string, error) // want "store\\.WidgetRepo\\.Get: second parameter must be authz\\.Proof"
	// Proof present but a tenant-typed parameter beside it.
	List(ctx context.Context, p authz.Proof, org domain.OrgID) ([]string, error) // want "store\\.WidgetRepo\\.List: tenant-identifier-typed parameter \\(.*internal/domain\\.OrgID\\)"
	// Tenant type hidden in a struct field.
	Search(ctx context.Context, p authz.Proof, f Filter) ([]string, error) // want "store\\.WidgetRepo\\.Search: tenant-identifier-typed parameter \\(.*internal/domain\\.OrgID\\)"
}

type Repos interface {
	Widgets() WidgetRepo
}

type ReadRepos interface {
	Widgets() WidgetRepo
}
