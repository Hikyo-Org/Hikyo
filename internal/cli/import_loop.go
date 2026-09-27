package cli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
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
	name, found, err := vaultImportLoop(result, adapters)
	if err != nil {
		return err
	}
	if found {
		return failf(ExitRefused,
			"refusing a loop: %s is an active vault-kv sync destination of this project. "+
				"Hikyo delivers to that tree, so importing from it would feed Hikyo's own output back as source. "+
				"Import from a tree no vault-kv target writes, or remove the target first", name)
	}
	return nil
}

// vaultImportLoop reports the first active vault-kv target whose tree
// overlaps the imported selection: the same host and combined namespace/mount, and one
// path prefix containing the other.
func vaultImportLoop(result importer.Result, adapters apigen.AdapterList) (string, bool, error) {
	source, err := url.Parse(result.Identity)
	if err != nil || !validLoopOrigin(source) {
		return "", false, failf(ExitRefused, "cannot confirm this Vault/OpenBao import is loop-safe: invalid source identity")
	}
	sourceMount := vaultNamespaceMount(result.Namespace, result.Scope.Mount)
	sourcePath := strings.Trim(result.Scope.PathPrefix, "/")
	for _, a := range adapters.Items {
		if string(a.Provider) != "vault-kv" || a.State == apigen.AdapterStateTombstoned {
			continue
		}
		origin, err := url.Parse(a.Origin)
		if err != nil || !validLoopOrigin(origin) {
			return "", false, failf(ExitRefused, "cannot confirm this Vault/OpenBao import is loop-safe: invalid active sync adapter origin")
		}
		if !sameEndpoint(origin, source) {
			continue
		}
		for _, target := range a.Targets {
			if target.State == apigen.AdapterTargetStateTombstoned || vaultNamespaceMount(origin.Path, target.DestinationOwner) != sourceMount {
				continue
			}
			if pathsOverlap(sourcePath, strings.Trim(target.DestinationName, "/")) {
				return "adapter " + string(a.Id) + " target " + string(target.Id) + " (" + target.DestinationOwner + "/" + target.DestinationName + ")", true, nil
			}
		}
	}
	return "", false, nil
}

// vaultNamespaceMount unifies a child namespace's mount with its root-namespace
// path-prefix spelling without altering the case-sensitive path segments.
func vaultNamespaceMount(namespace, mount string) string {
	return strings.Trim(strings.Trim(namespace, "/")+"/"+strings.Trim(mount, "/"), "/")
}

func validLoopOrigin(origin *url.URL) bool {
	return origin != nil && (origin.Scheme == "http" || origin.Scheme == "https") && origin.Hostname() != "" && origin.User == nil && origin.RawQuery == "" && origin.Fragment == "" && effectivePort(origin) != ""
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
	port := effectivePort(a)
	return port != "" && strings.EqualFold(a.Hostname(), b.Hostname()) && port == effectivePort(b)
}

// effectivePort returns an explicit URL port, or the default for HTTP or HTTPS.
// Invalid explicit ports and other schemes without a port return an empty string.
func effectivePort(u *url.URL) string {
	if strings.HasSuffix(u.Host, ":") {
		return ""
	}
	if port := u.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return ""
		}
		return strconv.FormatUint(number, 10)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}
