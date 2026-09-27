package cli

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/importer"
)

// refuseVaultImportLoop keeps one Vault/OpenBao tree from being both a live
// import source and an active vault-kv sync destination of the same project.
// Importing what Hikyo itself delivered would turn one-way sync into a merge
// loop, so the overlap is refused by name with no override. The adapter list
// is read before anything is planned; a caller who cannot read it cannot prove
// the import is loop-safe and is refused too.
func refuseVaultImportLoop(ctx context.Context, client *Client, project string, result importer.Result) error {
	var adapters apigen.AdapterList
	if err := client.Do(ctx, http.MethodGet, project+"/adapters", nil, &adapters); err != nil {
		return failf(ExitRefused, "cannot confirm this Vault/OpenBao import is loop-safe: listing the project's sync adapters failed: %v", err)
	}
	if name, found := vaultImportLoop(result, adapters); found {
		return failf(ExitRefused,
			"refusing a loop: %s is an active vault-kv sync destination of this project. "+
				"Hikyo delivers to that tree, so importing from it would feed Hikyo's own output back as source. "+
				"Import from a tree no vault-kv target writes, or remove the target first", name)
	}
	return nil
}

// vaultImportLoop reports the first active vault-kv target whose tree
// overlaps the imported selection: the same host, namespace and mount, and one
// path prefix containing the other.
func vaultImportLoop(result importer.Result, adapters apigen.AdapterList) (string, bool) {
	source, err := url.Parse(result.Identity)
	if err != nil || source.Host == "" {
		return "", false
	}
	sourceMount := strings.Trim(result.Scope.Mount, "/")
	sourcePath := strings.Trim(result.Scope.PathPrefix, "/")
	for _, a := range adapters.Items {
		if string(a.Provider) != "vault-kv" || a.State == apigen.AdapterStateTombstoned {
			continue
		}
		origin, err := url.Parse(a.Origin)
		if err != nil || !sameEndpoint(origin, source) || strings.Trim(origin.Path, "/") != strings.Trim(result.Namespace, "/") {
			continue
		}
		for _, target := range a.Targets {
			if target.State == apigen.AdapterTargetStateTombstoned || strings.Trim(target.DestinationOwner, "/") != sourceMount {
				continue
			}
			if pathsOverlap(sourcePath, strings.Trim(target.DestinationName, "/")) {
				return "adapter " + string(a.Id) + " target " + string(target.Id) + " (" + target.DestinationOwner + "/" + target.DestinationName + ")", true
			}
		}
	}
	return "", false
}

// pathsOverlap reports equal paths or ancestry at a slash boundary. Inputs
// must have leading and trailing slashes removed; an empty path covers all paths.
func pathsOverlap(a, b string) bool {
	return a == "" || b == "" || a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// sameEndpoint compares hosts case-insensitively with the scheme's default
// port made explicit, so https://vault.example and https://vault.example:443
// name the same server.
func sameEndpoint(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

// effectivePort returns an explicit URL port, or the default for HTTP or HTTPS.
// Other schemes without an explicit port return an empty string.
func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}
