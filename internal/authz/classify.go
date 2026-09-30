package authz

import (
	"maps"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

// The wire registry: the probe classification for every non-operation entry
// point (tenant-isolation ADR, invariant 1). Service operations carry their
// class in the operation registry; everything else that can be reached from
// outside — HTTP routes, CLI verbs, background job types, SSE emit sites —
// is classified here. The classification-totality invariant enumerates the
// actual router, the actual CLI verb table and the (currently empty) job and
// SSE registries against this table: an unclassified entry point fails the
// build, and a stale entry here fails it too.

// ClassStub marks a CLI verb that is not an operation yet: it refuses (exit
// 2), reaches no server and no store, and has no entry in the operation
// registry — the totality check enforces exactly that. It is deliberately
// not one of the ADR's four probe classes: classifying an unimplemented
// verb as tenant-scoped or unauthenticated now would let the eventual
// implementation ride in on a stale class without ever meeting its probe
// contract. When a verb's ticket lands, its class here changes and the
// matching probes must exist.
const ClassStub Class = -1

// wireEntry is the single owner of one externally reachable entry point's
// classification, operation linkage, and direct audit-event linkage. Keeping
// all three facts in one row makes contradictions local and reviewable instead
// of spreading them across parallel maps.
type wireEntry struct {
	Class  Class
	Ops    []Operation
	Events []audit.EventType
}

// Events records audit event types a wire entry emits DIRECTLY, without an
// operation registry row behind it.
//
// It exists because authentication is the one surface that cannot be modelled
// as an operation: `authorize()` needs a principal, and these endpoints are
// what produce one. Their audit obligation is real all the same — the
// human-auth ADR requires login success and failure, logout, session
// creation, and credential-establishment mint, consumption and refusal — so
// the completeness invariant reads each row beside the operation registry
// rather than letting an unaudited pre-auth path hide behind "no operation".
//
// Most wire entries are either operation-backed (Ops) or declare their events
// directly (the authentication surface). The credential-reset route (#54) is
// deliberately BOTH: its Ops field names the two operations it dispatches
// between at runtime — so the operation linkage records
// that it reaches CapCredentialReset (MFA-mandatory) — AND declares its events
// here, because its writes and audit ride the resolution surface (like the
// account-security mutations) rather than a single operation row. It names no
// single x-hikyo-operation in the contract, since two ops of different classes
// cannot be carried by one row; the completeness invariant unions both sources.

// Ops maps an HTTP entry point to the registered operation(s) it reaches. The
// audit-completeness invariant follows it so a domain route inherits its
// operation's audit mapping instead of needing a second declaration that
// could drift from the first. Most routes reach exactly one operation; a route
// that dispatches at runtime between operations (credential reset) lists them
// all, so the linkage records every operation the route can reach.

// WireRoutes returns the route→operation(s) mapping for the invariant tests and
// the contract cross-check.
func (RegistryFacts) WireRoutes() map[string][]Operation {
	routes := make(map[string][]Operation)
	for name, entry := range wireRegistry {
		if len(entry.Ops) > 0 {
			routes[name] = append([]Operation(nil), entry.Ops...)
		}
	}
	return routes
}

// WireEvents returns the direct wire→event mapping for the invariant tests.
func (RegistryFacts) WireEvents() map[string][]audit.EventType {
	events := make(map[string][]audit.EventType)
	for name, entry := range wireRegistry {
		if len(entry.Events) > 0 {
			events[name] = append([]audit.EventType(nil), entry.Events...)
		}
	}
	return events
}

// Cache is one registered cache holding derived tenant material
// (tenant-isolation ADR invariant 12). Registration is mandatory: the
// invariant test fails on any cache-shaped declaration in the module that
// is not listed here, so a new cache cannot appear without stating how it
// is keyed and who may reach it.
type Cache struct {
	// KeyConstructor is the single function that builds its keys. The ADR's
	// keying rule: the full id chain to the owning scope, structured and
	// injectively encoded (length-prefixed — bare concatenation is how
	// (org "a", project "bc") and (org "ab", project "c") collide).
	KeyConstructor string
	// ProofGatedAt names the layer that supplies the proof for reads and
	// writes. For the DEK LRU this is deliberately NOT inside the cache:
	// internal/crypto is a locked leaf package (encryption-model ADR; enforced by
	// the boundary test) and may not import the authorization package, so
	// its accessors cannot take an authz.Proof. The access rule is therefore
	// discharged one layer up, at the service seam that resolves a scope
	// before asking crypto to seal for it.
	ProofGatedAt string
}

// caches is the closed cache registry.
var caches = map[string]Cache{
	"app.unattended-image": {
		// One public release descriptor per private installation directory.
		// Every read reauthenticates the bundle and native archive against the
		// running build and the database's durable trust floor; descriptor
		// paths and hashes never independently authorize executable bytes.
		KeyConstructor: "internal/app.cachedUnattendedImage: installation StateDirectory/unattended/image.json; exact embedded release claim",
		ProofGatedAt:   "not tenant proof-gated: public signed release assets; RunUnattendedUpgrade holds installation exclusion, VerifyCachedRelease reauthenticates archives, and the database gate controls migration/admission",
	},
	"crypto.dek-lru": {
		KeyConstructor: "internal/crypto.dekScope",
		// No tenant-facing caller exists yet: the DEK LRU is reachable only
		// from Keyring.ForProject, whose only callers today are crypto's own
		// tests and the boot path. The first tenant consumer is #50 (flat
		// encrypted values), which MUST resolve the scope through
		// authorize() and pass the proof's chain — a cache hit must not be a
		// proof-free path to tenant material.
		ProofGatedAt: "service seam (#50); no tenant caller today",
	},
	"oidcfed.jwks": {
		// The byte-exact issuer identifies the authority. The CA digest binds
		// cached signing keys to the roots under which they were fetched.
		KeyConstructor: "internal/oidcfed.Issuer.Issuer + SHA-256(CABundlePEM)",
		// Not proof-gated, and here that is the right answer rather than a
		// deferral. The contents are the PUBLIC signing keys an issuer publishes
		// at a well-known URL — no tenant material, nothing a proof could
		// protect. What the cache governs is the FRESHNESS of the answer, which
		// is the staleness bound. And it is read pre-authentication by
		// construction: validating the presented token is what produces a
		// principal, so no proof can exist yet.
		ProofGatedAt: "not proof-gated: public issuer signing keys, read pre-authentication (#62)",
	},
	"updatecheck.releases": {
		// One process-wide list from the compile-time fixed Hikyo GitHub
		// repository. Channel selection happens after the list is read.
		KeyConstructor: "singleton: github.com/Hikyo-Org/hikyo releases",
		ProofGatedAt:   "not proof-gated: public release metadata; endpoint authorization precedes access",
	},
	"operator.delivery-capabilities": {
		// One probe result per HikyoInstance, held for 10 minutes: the highest
		// delivery-target-report vocabulary the instance advertises in /meta.
		// The CR UID keys nothing here; only the instance does.
		KeyConstructor: "internal/operator.statusReporter.capabilities: HikyoInstance UID",
		ProofGatedAt:   "not proof-gated: public tenant-free /meta capability list; every report is authorized by the server under the CR's own credential",
	},
	"parameters.patterns": {
		// Compiled RE2 programs are pure functions of bounded public pattern
		// text. Entries contain no values, tenant identifiers or authorization
		// decisions; reuse cannot disclose another environment's contract.
		KeyConstructor: "internal/parameters.compiledPattern: byte-exact pattern string",
		ProofGatedAt:   "stored declarations require service authorization; caller-supplied syntax may compile before authorization; pure compiled programs contain no tenant results and are bounded to 128 entries",
	},
	"selfupdate.nightly-downloads": {
		// On-disk directories under the operator CLI state directory, one per
		// verified nightly, named by the signed release manifest digest
		// (nightly-<sha256>) and, for assembled bundles, by the route digest
		// plus the recovery-signed snapshot digest. Reuse never trusts the
		// disk: every cached asset is rechecked against the immutable GitHub
		// release inventory and the current signed trust before use.
		KeyConstructor: "internal/selfupdate: release manifest SHA-256; bundle route digest + trust snapshot digest",
		ProofGatedAt:   "not proof-gated: public signed release assets held by the operator process; no tenant material",
	},
}

// Caches returns the cache registry for the invariant test.
func (RegistryFacts) Caches() map[string]Cache {
	return maps.Clone(caches)
}

// Wire returns the wire registry for the invariant tests.
func (RegistryFacts) Wire() map[string]Class {
	classes := make(map[string]Class, len(wireRegistry))
	for name, entry := range wireRegistry {
		classes[name] = entry.Class
	}
	return classes
}
