package operator

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	opclient "github.com/Hikyo-Org/hikyo/internal/operator/client"
)

// deliveryClient is the fetch and status-report surface the reconciler depends
// on. The concrete *opclient.Client satisfies it; tests inject a stub against an
// httptest server so the reconciler is exercised without a real Hikyo server.
type deliveryClient interface {
	Fetch(ctx context.Context, r opclient.FetchRequest) (*opclient.DeliveryResponse, opclient.Outcome, error)
	Capabilities(ctx context.Context) ([]string, error)
	Report(ctx context.Context, org, project, environment, bearer string, body apigen.DeliveryTargetReportRequest) (int, error)
	Tombstone(ctx context.Context, org, project, environment, bearer string, body apigen.DeliveryTargetTombstoneRequest) (int, error)
}

// clientFactory builds a deliveryClient for one instance's URL and CA bundle.
// The reconciler's NewClientForURL hook defaults to this.
func defaultClientFactory(rawURL string, caBundlePEM []byte) (deliveryClient, error) {
	return opclient.NewClient(rawURL, caBundlePEM, "hikyo-operator/"+Version)
}
