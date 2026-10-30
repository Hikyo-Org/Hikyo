package githubactions

import (
	"context"
	"crypto/sha256"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// This seam exists only in the external contract. It annotates owned call
// arguments, not wire bodies, and leaves the production API/error contract alone.
type observedContractClient struct {
	*Client
	observer *contractOperationObserver
}

type contractOperationKey struct{}

type contractValueFingerprint struct {
	length int
	hash   [32]byte
}

type contractOperation struct {
	name, operation string
}

type contractOperationObserver struct {
	base      http.RoundTripper
	scope     contractObserverScope
	emit      func(map[string]any)
	mu        sync.Mutex
	owned     map[string]struct{}
	sensitive map[contractValueFingerprint]struct{}
}

type contractObserverScope struct {
	owner                        string
	organizationID, repositoryID int64
}

var (
	contractObservedName = regexp.MustCompile(`^HIKYO_CONTRACT_[A-Z0-9_]{1,100}$`)
	contractRequestID    = regexp.MustCompile(`^[A-F0-9]{4,32}(:[A-F0-9]{1,32}){4}$`)
	contractPermissions  = regexp.MustCompile(`^(organization_variables=(read|write)|metadata=read)([,;] ?(organization_variables=(read|write)|metadata=read))*$`)
)

func observeContractClient(client *Client, cfg githubContractConfig, emit func(map[string]any)) *observedContractClient {
	observer := &contractOperationObserver{
		base: client.http.Transport, scope: contractObserverScope{cfg.owner, cfg.organizationID, cfg.repositoryID}, emit: emit,
		owned: make(map[string]struct{}), sensitive: map[contractValueFingerprint]struct{}{fingerprintContractValue(client.token): {}},
	}
	client.http.Transport = observer
	return &observedContractClient{Client: client, observer: observer}
}

func fingerprintContractValue(value string) contractValueFingerprint {
	return contractValueFingerprint{length: len(value), hash: sha256.Sum256([]byte(value))}
}

func (o *contractOperationObserver) inScope(d adapter.Destination) bool {
	return d.Kind == adapter.Organization && d.Owner == o.scope.owner && d.NumericID == o.scope.organizationID &&
		d.Visibility == "selected" && slices.Equal(d.SelectedRepositoryIDs, []int64{o.scope.repositoryID})
}

func (c *observedContractClient) own(destination adapter.Destination, surface adapter.Surface, name string) {
	if surface != adapter.Variable || !c.observer.inScope(destination) || !contractObservedName.MatchString(name) {
		return
	}
	c.observer.mu.Lock()
	defer c.observer.mu.Unlock()
	c.observer.owned[name] = struct{}{}
}

func (c *observedContractClient) operationContext(ctx context.Context, d adapter.Destination, surface adapter.Surface, name, operation string, value *string) context.Context {
	if surface != adapter.Variable || !c.observer.inScope(d) {
		return ctx
	}
	c.observer.mu.Lock()
	defer c.observer.mu.Unlock()
	_, owned := c.observer.owned[name]
	if !owned {
		return ctx
	}
	if value != nil {
		c.observer.sensitive[fingerprintContractValue(*value)] = struct{}{}
	}
	return context.WithValue(ctx, contractOperationKey{}, contractOperation{name: name, operation: operation})
}

func (c *observedContractClient) CreateVariable(ctx context.Context, d adapter.Destination, name, value string) (WriteResult, error) {
	return c.Client.CreateVariable(c.operationContext(ctx, d, adapter.Variable, name, "create_variable", &value), d, name, value)
}

func (c *observedContractClient) ReplaceSelectedRepositories(ctx context.Context, d adapter.Destination, surface adapter.Surface, name string) error {
	return c.Client.ReplaceSelectedRepositories(c.operationContext(ctx, d, surface, name, "replace_selected_repositories", nil), d, surface, name)
}

func (o *contractOperationObserver) RoundTrip(request *http.Request) (*http.Response, error) {
	op, observed := request.Context().Value(contractOperationKey{}).(contractOperation)
	// Never observe artifact redirects, unrelated resources, or caller-supplied
	// URLs. Neither request/response bodies nor Authorization are inspected.
	method, route := http.MethodPost, "/orgs/"+o.scope.owner+"/actions/variables"
	if op.operation == "replace_selected_repositories" {
		method, route = http.MethodPut, route+"/"+op.name+"/repositories"
	} else if op.operation != "create_variable" {
		observed = false
	}
	o.mu.Lock()
	_, owned := o.owned[op.name]
	o.mu.Unlock()
	observed = observed && owned && contractObservedName.MatchString(op.name) && request.Method == method &&
		request.URL.Scheme == "https" && request.URL.Host == "api.github.com" && request.URL.User == nil &&
		(request.Host == "" || request.Host == "api.github.com") && request.URL.Opaque == "" &&
		request.URL.RawQuery == "" && request.URL.Fragment == "" && request.URL.EscapedPath() == route
	started := time.Now()
	response, err := o.base.RoundTrip(request)
	finished := time.Now()
	if !observed {
		return response, err
	}
	record := map[string]any{
		"kind": "provider_operation", "operation": op.operation, "method": method,
		"destination": adapter.Organization, "surface": adapter.Variable, "owner": o.scope.owner,
		"organization_id": o.scope.organizationID, "name": op.name, "visibility": "selected",
		"selected_repository_ids": []int64{o.scope.repositoryID}, "metadata_source": "owned_call_arguments",
		"started_at": started.UTC().Format(time.RFC3339Nano), "finished_at": finished.UTC().Format(time.RFC3339Nano),
		"error_category": "transport_error",
	}
	if response != nil {
		record["status"] = response.StatusCode
		if err == nil {
			switch {
			case response.StatusCode == http.StatusNotFound:
				record["error_category"] = "http_404_ambiguous"
			case response.StatusCode >= 200 && response.StatusCode < 300:
				record["error_category"] = "success"
			default:
				record["error_category"] = "http_error"
			}
		}
		for _, header := range []struct {
			name, field string
			limit       int
			pattern     *regexp.Regexp
		}{
			{"X-GitHub-Request-Id", "request_id", 128, contractRequestID},
			{"X-Accepted-GitHub-Permissions", "accepted_permissions", 256, contractPermissions},
		} {
			values := response.Header.Values(header.name)
			if len(values) == 0 {
				continue
			}
			if len(values) != 1 || len(values[0]) > header.limit || !header.pattern.MatchString(values[0]) ||
				o.headerContainsSensitiveValue(values[0]) {
				record[header.field+"_redacted"] = true
				continue
			}
			record[header.field] = values[0]
		}
	}
	o.emit(record)
	return response, err
}

func (o *contractOperationObserver) headerContainsSensitiveValue(header string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for value := range o.sensitive {
		if contractHeaderContainsValue(header, value) {
			return true
		}
	}
	return false
}

// The source values remain outside the observer. Even a reflected value that
// happens to satisfy the header grammar must not enter diagnostic evidence.
func contractHeaderContainsValue(header string, value contractValueFingerprint) bool {
	if value.length == 0 || value.length > len(header) {
		return false
	}
	for i := 0; i <= len(header)-value.length; i++ {
		if sha256.Sum256([]byte(header[i:i+value.length])) == value.hash {
			return true
		}
	}
	return false
}

func (o *contractOperationObserver) CloseIdleConnections() {
	if closer, ok := o.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
