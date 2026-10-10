# Dependency stack repair: PRs 873 through 877

Stack order: #873 (Go patch group), #874 (x/tools), #875 (etree), #876 (sigstore), #877 (goose). Preserve this order and leave merge decisions with the maintainer.

The go-jose 4.1.5 update rejects low-order Ed25519 public keys. The canonicalization tests and browser setup used an all-zero key, so their fixture failed admission before testing canonicalization. Replace the fixture with RFC 8032 section 7.1 test vector 1's public key. Keep production admission unchanged and explicitly test rejection of the original low-order key.

Local verification: the JWKS parsing, admission, and federation route tests pass with the updated dependency. The full exact-head CI remains the delivery check for each layer.

Repair overlapping go.mod edits by retaining the dependency increases from both sides, then regenerate go.sum with go mod tidy. Merge repaired lower branches into their dependants with signed, DCO-signed commits so existing signed Dependabot commits retain their identity. Verify the full range before pushing and confirm GitHub signature verification afterward.
