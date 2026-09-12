package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Machine exports share delivery's live authority and durable pin selection.
// The human historical-export endpoint must remain unavailable to machines.
func machineExport(ctx context.Context, client *Client, base string, reveal bool, revision int64, parameters map[string]string) (apigen.ExportedValues, error) {
	out := apigen.ExportedValues{Items: []apigen.ExportedValue{}}
	if revision != 0 {
		return out, failf(ExitRefused, "machine values export does not accept --revision; delivery selects the authorized current or pinned snapshot")
	}
	meta, err := client.Meta(ctx)
	if err != nil {
		return out, err
	}
	// Revision 3 introduced delivery's selected snapshot revision. An older
	// response would otherwise silently become an export of revision zero.
	if meta.ApiRevision < 3 {
		return out, failf(ExitRefused, "this instance is running %s (API revision %d); machine values export needs revision 3. Upgrade the server.", meta.ServerVersion, meta.ApiRevision)
	}
	query := url.Values{}
	if !reveal {
		query.Set("projection", "config-only")
	}
	if len(parameters) > 0 {
		encoded, err := json.Marshal(parameters)
		if err != nil {
			return out, err
		}
		query.Set("parameters", string(encoded))
	}
	path := base + "/delivery"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var fetched apigen.DeliveryResponse
	if err := client.Do(ctx, http.MethodGet, path, nil, &fetched); err != nil {
		return out, err
	}
	if fetched.Current {
		return out, failf(ExitInternal, "server returned a conditional response to an unconditional machine export")
	}
	for _, key := range fetched.Keys {
		if reveal && key.Classification == apigen.KeyClassificationSecret && key.Presence == apigen.DeliveredKeyPresenceSet && key.Value == nil {
			return apigen.ExportedValues{}, failf(ExitRefused, "machine export cannot reveal all delivered secrets; check reveal grants and project machine-reveal settings")
		}
		out.Items = append(out.Items, apigen.ExportedValue{Name: key.Name, Classification: key.Classification, Value: key.Value, Revealed: key.Value != nil})
	}
	out.Count = len(out.Items)
	out.Revision = fetched.Revision
	return out, nil
}
