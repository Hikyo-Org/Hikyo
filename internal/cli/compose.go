package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// The Compose delivery verbs (compose-integration ADR; #63): `hikyo run --`
// (path 1, exec wrapper) and `hikyo compose render|sync|doctor` (path 2,
// rendered env_file). Both are MACHINE-ONLY — the stored human session is never
// used — and both are thin wiring over internal/compose, which owns all the
// pure logic and filesystem primitives. Every use of a compose primitive sits
// behind a small helper here so the snapshot/generation format rework can be
// reconciled in one place per primitive.
//
// Test seam: HIKYO_COMPOSE_DOCKER overrides the resolved `docker` executable for
// `compose sync|doctor`. It is deliberately kept out of the help text — not part
// of the CLI's stable surface, only a test/override hook — documented here and
// in the api-cli-spellings "Compose delivery" section.

const (
	composeConfigName = "hikyo-compose.yaml"
	// runGenerationKey names run's single snapshot "generation" in the
	// GenerationStamps map. It is outside the target-name grammar
	// (^[a-z][a-z0-9-]*$) so it can never collide with a real target named
	// "run".
	runGenerationKey = "__run__"

	// credentialFingerprintDomain domain-separates the LOCAL credential
	// fingerprint that binds BOTH the cursor and the offline snapshot to the
	// presented token (compose ADR § Cursor rules; R1-3). It is a purely local
	// identity: swapping tokens changes the fingerprint and invalidates the cursor
	// before it is presented and refuses the old snapshot by name at load. The
	// bytes are frozen — changing them would pointlessly invalidate live cursors.
	credentialFingerprintDomain = "hikyo-cursor-cred-v1\x00"

	machineRevealOptIn = "secret plaintext requires the project's machine-reveal opt-in and then a `reveal` grant on this principal: " +
		"`hikyo project-settings machine-reveal set --enabled true` (project-settings and reveal, second factor), " +
		"then `hikyo access grant add --principal <mch_...> --capability reveal --env <env>`; or run with --config-only"
)

// credentialFingerprint is the local, offline-derivable identity of the
// presented credential: hex(sha256(domain ‖ token))[:32] (compose ADR § Cursor
// rules — "the stored cursor is bound to credential identity"). ONE helper for
// every save-site and compare-site so they cannot drift — it binds BOTH the
// cursor AND the offline snapshot's credential (R1-3), so a rotated token
// refuses the old snapshot by name even fully offline. The server-asserted
// credential_id (a different value) remains authenticated metadata in the AAD.
func credentialFingerprint(token string) string {
	sum := sha256.Sum256([]byte(credentialFingerprintDomain + token))
	return hex.EncodeToString(sum[:])[:32]
}

// composeStack owns the values that identify one machine-authenticated Compose
// stack. Keeping them together prevents snapshot, cursor, offline-flush, and
// render operations from accidentally using different projections or paths.
type composeStack struct {
	cfg             *compose.Config
	cfgDir          string
	client          *Client
	entry           TrustEntry
	org             string
	project         string
	env             string
	token           string
	configOnly      bool
	slug            string
	stateDir        string
	runtimeDir      string
	explicitRuntime bool
	runtimeErr      error
}

type composeStackOptions struct {
	projectDir    string
	configOnly    bool
	requireConfig bool
}

// openComposeStack resolves stack identity and paths but deliberately performs
// no filesystem writes, snapshot access, offline flush, or network fetch.
func openComposeStack(st *State, ios IO, flags commonFlags, opts composeStackOptions) (*composeStack, error) {
	start := startDir(ios, opts.projectDir)
	cfg, cfgDir, err := findComposeConfig(start)
	if err != nil {
		return nil, err
	}
	if cfg == nil && opts.requireConfig {
		if flags.operation == "compose doctor" || flags.operation == "compose sync" {
			return nil, failf(ExitUsage, "hikyo compose doctor requires a %s (searched up from %s)", composeConfigName, start)
		}
		return nil, failf(ExitUsage, "hikyo compose render requires a %s (searched up from %s); the .hikyo.json pin file is not enough — the config carries the render targets",
			composeConfigName, start)
	}

	client, entry, resolved, token, err := resolveMachineTarget(st, ios, flags, cfg, cfgDir, topLevelOperation(flags.operation))
	if err != nil {
		return nil, err
	}
	s := &composeStack{
		cfg: cfg, cfgDir: cfgDir, client: client, entry: entry,
		org: resolved.Get(DimOrg), project: resolved.Get(DimProject), env: resolved.Get(DimEnv),
		token: token, configOnly: opts.configOnly,
	}
	if cfg == nil {
		return s, nil
	}
	s.slug, err = composeSlug(cfg, s.org, s.project, s.env)
	if err != nil {
		return nil, err
	}
	s.stateDir = composeStateDir(st, s.slug)
	s.runtimeDir, s.explicitRuntime, s.runtimeErr = composeRuntimeDir(ios, cfg, s.slug)
	return s, nil
}

// ---------------------------------------------------------------------------
// hikyo compose render|sync|doctor
// ---------------------------------------------------------------------------

func runCompose(ctx context.Context, ios IO, args []string) error {
	if len(args) == 0 {
		return failf(ExitUsage, "usage: hikyo compose render|sync|doctor")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "render":
		_, err := runComposeRender(ctx, ios, rest)
		return err
	case "sync":
		return runComposeSync(ctx, ios, rest)
	case "doctor":
		return runComposeDoctor(ctx, ios, rest)
	default:
		return failf(ExitUsage, "unknown compose verb %q: use render, sync or doctor", sub)
	}
}

// ---------------------------------------------------------------------------
// machine-only target resolution
// ---------------------------------------------------------------------------

// resolveMachineTarget resolves the target, folds any hikyo-compose.yaml
// dimensions in (a disagreement with an already-resolved dimension is a hard
// error), and REQUIRES a machine credential. It never falls back to the stored
// human session — that path is a refusal in this build.
func resolveMachineTarget(st *State, ios IO, flags commonFlags, cfg *compose.Config, cfgPath, verb string) (*Client, TrustEntry, Resolved, string, error) {
	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return nil, TrustEntry{}, Resolved{}, "", err
	}
	if cfg != nil {
		for _, d := range []struct {
			dim Dimension
			val string
		}{{DimOrg, cfg.Org}, {DimProject, cfg.Project}, {DimEnv, cfg.Environment}} {
			if err := foldConfigDim(&resolved, d.dim, d.val, cfgPath); err != nil {
				return nil, TrustEntry{}, Resolved{}, "", err
			}
		}
	}
	if _, err := NewTenantScope(resolved); err != nil {
		return nil, TrustEntry{}, Resolved{}, "", err
	}
	if !flags.kinds.Allows(AuthKindMachineCredential) {
		return nil, TrustEntry{}, Resolved{}, "", failf(ExitRefused, "hikyo %s does not accept machine credentials", flags.operation)
	}
	if flags.Auth == "human" {
		hint := "this operation requires a machine credential"
		if flags.operation == "run" {
			hint = "pass --use-human-session for run's gated human-session exception"
		}
		return nil, TrustEntry{}, Resolved{}, "", failf(ExitRefused, "hikyo %s cannot use --auth=human: %s", flags.operation, hint)
	}

	entry, err := machineEntry(st, resolved, cfg)
	if err != nil {
		return nil, TrustEntry{}, Resolved{}, "", err
	}
	for _, d := range []Dimension{DimOrg, DimProject, DimEnv} {
		if _, err := resolved.Require(d); err != nil {
			return nil, TrustEntry{}, Resolved{}, "", err
		}
	}

	token, err := machineToken(ios, flags.TokenFile)
	if err != nil {
		return nil, TrustEntry{}, Resolved{}, "", err
	}
	if token == "" {
		// `run` has the single locked human-session exception; `render`/`sync` have
		// no human path at all (api-cli-surface ADR line 96).
		hint := "render and sync have no human path"
		if verb == "run" {
			hint = "pass --use-human-session to run under the stored human session (a TTY, an enumerated confirmation, and a live disclosure window are required)"
		}
		return nil, TrustEntry{}, Resolved{}, "", failf(ExitAuth,
			"hikyo %s accepts only a machine credential (--token-file or HIKYO_TOKEN); %s", verb, hint)
	}
	client, err := NewClient(entry, token)
	if err != nil {
		return nil, TrustEntry{}, Resolved{}, "", err
	}
	if echo := resolved.Echo(); echo != "" {
		fmt.Fprintf(ios.Stderr, "target: %s [origin %s, artifact machine-credential]\n", echo, entry.Origin)
	}
	return client, entry, resolved, token, nil
}

// foldConfigDim fills an unresolved dimension from the config, or refuses when
// the config disagrees with an already-resolved one, naming both sources.
func foldConfigDim(r *Resolved, dim Dimension, cfgVal, cfgPath string) error {
	cfgVal = strings.TrimSpace(cfgVal)
	if cfgVal == "" {
		return nil
	}
	if cur := r.Values[dim]; cur != "" {
		if cur != cfgVal {
			return failf(ExitUsage, "hikyo compose: %s is %q (from %s) but %q (from %s) — refusing rather than picking one",
				dim, cur, r.Sources[dim], cfgVal, cfgPath)
		}
		return nil
	}
	r.Values[dim] = cfgVal
	r.Sources[dim] = SourceConfig
	return nil
}

// machineEntry resolves the trust entry the credential is presented to. The
// machine path NEVER establishes trust interactively: an origin the config
// names must already be provisioned in the local store.
func machineEntry(st *State, resolved Resolved, cfg *compose.Config) (TrustEntry, error) {
	var cfgOrigin string
	if cfg != nil && strings.TrimSpace(cfg.Instance) != "" {
		o, err := CanonicalOrigin(cfg.Instance)
		if err != nil {
			return TrustEntry{}, err
		}
		cfgOrigin = o
	}

	instance := resolved.Get(DimInstance)
	if instance == "" {
		if cfgOrigin != "" {
			entry, err := lookupByOrigin(st, cfgOrigin)
			if err != nil {
				return TrustEntry{}, err
			}
			resolved.Values[DimInstance] = entry.Name
			resolved.Sources[DimInstance] = SourceConfig
			return entry, nil
		}
		// Exactly one established instance is the only reading; two or more is an
		// ambiguity, never a default.
		entries, serr := st.Trust().Load()
		if serr != nil {
			return TrustEntry{}, serr
		}
		if len(entries) != 1 {
			_, err := resolved.Require(DimInstance)
			return TrustEntry{}, err
		}
		for k := range entries {
			instance = k
		}
		resolved.Values[DimInstance] = instance
		resolved.Sources[DimInstance] = SourceContext
	}

	entry, err := st.Trust().Lookup(instance)
	if err != nil {
		return TrustEntry{}, err
	}
	if cfgOrigin != "" && entry.Origin != cfgOrigin {
		return TrustEntry{}, failf(ExitUsage,
			"instance %q resolves to origin %s but %s names %s — refusing rather than picking one",
			instance, entry.Origin, composeConfigName, cfgOrigin)
	}
	return entry, nil
}

func lookupByOrigin(st *State, origin string) (TrustEntry, error) {
	entries, err := st.Trust().Load()
	if err != nil {
		return TrustEntry{}, err
	}
	for _, e := range entries {
		if e.Origin == origin {
			return e, nil
		}
	}
	return TrustEntry{}, failf(ExitRefused,
		"%s names instance %s, which is not in the local trust store; provision it with `hikyo context create --instance %s` or --trust-file (the machine path never establishes trust interactively)",
		composeConfigName, origin, origin)
}

// ---------------------------------------------------------------------------
// delivery transport
// ---------------------------------------------------------------------------

func deliveryPath(org, project, env string) string {
	return api.PathPrefix + "/orgs/" + url.PathEscape(org) +
		"/projects/" + url.PathEscape(project) +
		"/environments/" + url.PathEscape(env) + "/delivery"
}

// renderAcknowledged is the sorted, deduped union of every target's
// acknowledge_loader_control — the loader-control acknowledgement in force for a
// render, sent on the fetch so the server's audit record carries it (#64).
func renderAcknowledged(cfg *compose.Config) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, name := range cfg.TargetNames() {
		for _, k := range cfg.Targets[name].AcknowledgeLoaderControl {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func fetchDelivery(ctx context.Context, client *Client, org, project, env string, configOnly bool, acknowledged []string, cursor string) (apigen.DeliveryResponse, error) {
	q := url.Values{}
	if configOnly {
		// The wire term is `projection=config-only` (#64's server param); the CLI
		// flag stays `--config-only`. `full` is the default and is left implicit.
		q.Set("projection", "config-only")
	}
	// acknowledged_keys is sent AS PRESENTED so the server records which
	// loader-control acknowledgement was in force for this delivery (#64 audit
	// field). The server records and otherwise ignores it — client-side refusal
	// stays authoritative. style: form, explode: false ⇒ a single CSV member.
	if len(acknowledged) > 0 {
		q.Set("acknowledged_keys", strings.Join(acknowledged, ","))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	path := deliveryPath(org, project, env)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp apigen.DeliveryResponse
	if err := client.Do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apigen.DeliveryResponse{}, err
	}
	return resp, nil
}

func (s *composeStack) fetchDelivery(ctx context.Context, acknowledged []string, cursor string) (apigen.DeliveryResponse, error) {
	return fetchDelivery(ctx, s.client, s.org, s.project, s.env, s.configOnly, acknowledged, cursor)
}

// flushOffline reconciles buffered offline records before a fetch (ops-spec § 6
// ordering rule). Records chunk to the server's 1000-per-call limit; the files
// are marked flushed only after every chunk is accepted, so a mid-run failure
// re-sends idempotently rather than dropping evidence.
func (s *composeStack) flushOffline(ctx context.Context) error {
	if s.stateDir == "" {
		return nil
	}
	records, files, err := compose.Pending(s.stateDir)
	if err != nil {
		return failf(ExitInternal, "reading pending offline records: %v", err)
	}
	if len(records) == 0 {
		return nil
	}
	path := deliveryPath(s.org, s.project, s.env) + "/offline-records"
	const batch = 1000
	for i := 0; i < len(records); i += batch {
		end := min(i+batch, len(records))
		body := apigen.ReconcileOfflineRecordsRequest{Records: toAPIRecords(records[i:end])}
		if err := s.client.Do(ctx, http.MethodPost, path, body, nil); err != nil {
			return err // refuses the fetch: ExitUnavailable or the server's mapped code
		}
	}
	if err := compose.MarkFlushed(s.stateDir, files); err != nil {
		return failf(ExitInternal, "marking offline records flushed: %v", err)
	}
	return nil
}

func toAPIRecords(recs []compose.OfflineRecord) []apigen.OfflineDeliveryRecord {
	out := make([]apigen.OfflineDeliveryRecord, 0, len(recs))
	for _, r := range recs {
		occ, _ := time.Parse(time.RFC3339, r.OccurredAt)
		served, _ := time.Parse(time.RFC3339, r.ServedFrom)
		out = append(out, apigen.OfflineDeliveryRecord{
			RecordId: r.RecordID, KeyId: r.KeyID, KeyName: r.KeyName,
			Classification: apigen.KeyClassification(r.Classification),
			OccurredAt:     occ, ServedFrom: served,
			CredentialId: r.CredentialID, Generation: r.Generation,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// snapshot / cursor helpers (thin wrappers over internal/compose)
// ---------------------------------------------------------------------------

func saveSnapshot(keys *crypto.LocalKeys, binding crypto.SnapshotBinding, payload compose.SnapshotPayload) error {
	return compose.SaveSnapshot(keys, binding, payload)
}

// loadOfflineSnapshot opens the persisted snapshot under the validated binding
// the box constructed before the fetch: origin/org/project/env, config_only,
// mode target set, and the LOCAL fingerprint of the presented token. A rotated
// token refuses the old snapshot by name before decrypt work.
func loadOfflineSnapshot(ios IO, cfg *compose.Config, binding crypto.SnapshotBinding) (compose.SnapshotPayload, crypto.SnapshotBinding, error) {
	var zeroP compose.SnapshotPayload
	var zeroB crypto.SnapshotBinding
	if err := binding.ValidateScope(); err != nil {
		return zeroP, zeroB, err
	}
	stateDir, err := binding.StorageDir()
	if err != nil {
		return zeroP, zeroB, err
	}
	keys, err := loadLocalKeys(stateDir)
	if err != nil {
		return zeroP, zeroB, err
	}
	payload, storedBinding, err := compose.LoadSnapshot(keys, binding, ios.now(), cfg.SnapshotMaxAge())
	if err != nil {
		if errors.Is(err, compose.ErrSnapshotContext) {
			return zeroP, zeroB, failf(ExitRefused, "offline snapshot belongs to a different context and will not be served: %v", err)
		}
		if errors.Is(err, os.ErrNotExist) {
			return zeroP, zeroB, failf(ExitRefused, "offline serve is enabled but no snapshot has been saved for this stack yet")
		}
		if errors.Is(err, compose.ErrSnapshotExpired) || errors.Is(err, compose.ErrSnapshotRollback) || errors.Is(err, crypto.ErrDecrypt) {
			aad, aadErr := storedBinding.AAD()
			if aadErr != nil {
				return zeroP, zeroB, failf(ExitRefused, "offline serve refused: snapshot binding is unusable (%v)", err)
			}
			return zeroP, zeroB, failf(ExitRefused,
				"offline serve refused: snapshot issued %s, expires %s — past the maximum stale age (%s) or otherwise unusable (%v)",
				aad.IssuedAt, aad.ExpiresAt, cfg.SnapshotMaxAge(), err)
		}
		return zeroP, zeroB, failf(ExitRefused, "offline serve: %v", err)
	}
	return payload, storedBinding, nil
}

// newSnapshotBinding validates and owns the offline-known scope before any
// snapshot filesystem work. The same value is completed from a live delivery
// or matched against the stored delivery fields on an offline path.

func (s *composeStack) newSnapshotBinding(targetNames []string) (crypto.SnapshotBinding, error) {
	if s.stateDir == "" {
		return crypto.SnapshotBinding{}, nil
	}
	return crypto.NewSnapshotBinding(crypto.SnapshotBindingScope{
		StorageDir:     s.stateDir,
		InstanceOrigin: s.entry.Origin,
		OrgID:          s.org, ProjectID: s.project, EnvironmentID: s.env,
		CredentialFingerprint: credentialFingerprint(s.token), ConfigOnly: s.configOnly,
		TargetNames: targetNames,
	})
}

// cursorBinding builds the eligibility binding. The credential identity is the
// LOCAL fingerprint of the PRESENTED token (finding 8) — not a stored value, so
// swapping tokens invalidates the cursor before it is presented. The env,
// config_only, and per-target key-id membership are local truth; the pinned
// revision and projection are server-asserted (unknowable pre-fetch, and the
// server re-binds anyway), so they come from the stored cursor when present.
func (s *composeStack) cursorBinding(stored *compose.CursorState) compose.CursorBinding {
	b := compose.CursorBinding{
		CredentialID: credentialFingerprint(s.token),
		Environment:  s.env,
		ConfigOnly:   s.configOnly,
		TargetKeyIDs: targetKeyIDs(s.cfg),
	}
	if stored != nil {
		b.PinnedRevision = stored.Binding.PinnedRevision
		b.Projection = stored.Binding.Projection
	}
	return b
}

// saveCursor persists the cursor with its full binding after a committed render.
func (s *composeStack) saveCursor(resp apigen.DeliveryResponse, stamps map[string]string) error {
	pinned := int64(0)
	if resp.PinnedRevision != nil {
		pinned = *resp.PinnedRevision
	}
	binding := compose.CursorBinding{
		CredentialID:   credentialFingerprint(s.token),
		Environment:    s.env,
		ConfigOnly:     s.configOnly,
		PinnedRevision: pinned,
		Projection:     deliveryProjection(resp.Keys),
		TargetKeyIDs:   targetKeyIDs(s.cfg),
	}
	return compose.SaveCursor(s.stateDir, compose.CursorState{
		Cursor: resp.Cursor, Binding: binding, GenerationStamps: stamps,
	})
}

// eligibleCursor returns the stored cursor iff the full local eligibility test
// holds against the currently presented token, env, mode, and target set.
func (s *composeStack) eligibleCursor(currentStamps map[string]string) string {
	state, err := compose.LoadCursor(s.stateDir)
	if err != nil || state == nil {
		return ""
	}
	want := s.cursorBinding(state)
	c, ok := compose.EligibleCursor(state, want, currentStamps, s.runtimeDir)
	if !ok {
		return ""
	}
	return c
}

// appendOfflineRecords writes one durable, fsynced disclosure record per served
// row BEFORE the plaintext is released (compose ADR § "Audit during offline
// serve"). KeyID travels inside the sealed payload's rows now.
func appendOfflineRecords(ios IO, stateDir string, rows []compose.SnapshotRow, binding crypto.SnapshotBinding, generation string) error {
	if len(rows) == 0 {
		return nil
	}
	aad, err := binding.AAD()
	if err != nil {
		return err
	}
	recs := make([]compose.OfflineRecord, 0, len(rows))
	for _, r := range rows {
		id, err := compose.NewRecordID()
		if err != nil {
			return err
		}
		recs = append(recs, compose.OfflineRecord{
			RecordID: id, KeyID: r.KeyID, KeyName: r.Name, Classification: r.Classification,
			OccurredAt: ios.now().UTC().Format(time.RFC3339), CredentialID: aad.CredentialID,
			Generation: generation, ServedFrom: aad.IssuedAt,
		})
	}
	return compose.Append(stateDir, recs)
}

// ---------------------------------------------------------------------------
// path / config discovery
// ---------------------------------------------------------------------------

func startDir(ios IO, projectDir string) string {
	if strings.TrimSpace(projectDir) != "" {
		return projectDir
	}
	return ios.Workdir
}

// findComposeConfig walks up from startDir looking for hikyo-compose.yaml.
func findComposeConfig(startDir string) (*compose.Config, string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, composeConfigName)
		raw, err := os.ReadFile(candidate)
		switch {
		case err == nil:
			cfg, perr := compose.ParseConfig(raw)
			if perr != nil {
				return nil, "", failf(ExitRefused, "%s: %v", candidate, perr)
			}
			return cfg, dir, nil
		case !errors.Is(err, os.ErrNotExist):
			return nil, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir || strings.TrimSpace(parent) == "" {
			return nil, "", nil
		}
		dir = parent
	}
}

// composeIDGrammar is the repo's canonical resource-id grammar
// (api/openapi.yaml:8754 `^[a-z]{2,8}_[0-9a-fA-F-]{36}$`). There is no Go
// constant for it, so it is anchored here for the slug derivation.
var composeIDGrammar = regexp.MustCompile(`^[a-z]{2,8}_[0-9a-fA-F-]{36}$`)

// composeSlug derives the project slug. An explicit config slug (already
// grammar-checked as a path segment in ParseConfig) wins. Otherwise it is
// "<org>-<project>-<env>", but ONLY after validating each id against the repo id
// grammar so an unvalidated string cannot become a path segment (finding 2), and
// asserting containment so the derived state dir cannot escape.
func composeSlug(cfg *compose.Config, org, project, env string) (string, error) {
	if cfg != nil && strings.TrimSpace(cfg.Slug) != "" {
		return cfg.Slug, nil
	}
	for _, id := range []string{org, project, env} {
		if !composeIDGrammar.MatchString(id) {
			return "", failf(ExitUsage,
				"hikyo compose: cannot derive a project slug from %q — it is not a valid id (want %s); set an explicit `slug` in %s",
				id, composeIDGrammar.String(), composeConfigName)
		}
	}
	slug := org + "-" + project + "-" + env
	// Containment: the slug is a single path segment under the state dir, so a
	// join must not climb out of it (defence in depth over the grammar).
	if rel, err := filepath.Rel(".", slug); err != nil || rel != slug || strings.ContainsRune(slug, filepath.Separator) || strings.Contains(slug, "..") {
		return "", failf(ExitUsage, "hikyo compose: derived slug %q is not a safe path segment", slug)
	}
	return slug, nil
}

func composeStateDir(st *State, slug string) string {
	return filepath.Join(st.Dir(), "compose", slug)
}

// composeRuntimeDir resolves the tmpfs runtime directory (ops-spec § 6):
// config runtime_dir, else /run/hikyo/<slug> as root, else
// $XDG_RUNTIME_DIR/hikyo/<slug>. No runtime dir and not root is a usage error
// naming runtime_dir rather than a silent guess. The bool reports whether the
// path came from an EXPLICIT config runtime_dir (the operator's call on tmpfs)
// versus a derived DEFAULT (which the renderer requires to be tmpfs).
func composeRuntimeDir(ios IO, cfg *compose.Config, slug string) (string, bool, error) {
	if cfg != nil && strings.TrimSpace(cfg.RuntimeDir) != "" {
		return cfg.RuntimeDir, true, nil
	}
	if os.Geteuid() == 0 {
		return filepath.Join("/run/hikyo", slug), false, nil
	}
	if xdg := ios.Env.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "hikyo", slug), false, nil
	}
	return "", false, failf(ExitUsage,
		"no runtime directory: not root and XDG_RUNTIME_DIR is unset. Set `runtime_dir` in %s, or run under a session with XDG_RUNTIME_DIR", composeConfigName)
}

func loadLocalKeys(stateDir string) (*crypto.LocalKeys, error) {
	keys, err := crypto.LoadOrCreateLocalKey(stateDir)
	if err != nil {
		return nil, failf(ExitRefused, "compose: local key: %v", err)
	}
	return keys, nil
}

// ---------------------------------------------------------------------------
// small pure helpers
// ---------------------------------------------------------------------------

func isUnavailable(err error) bool {
	var ce *Error
	return asCLIError(err, &ce) && ce.Code == ExitUnavailable
}

func allTargetKeyIDs(cfg *compose.Config) []string {
	set := map[string]struct{}{}
	for _, t := range cfg.Targets {
		for _, id := range t.Keys {
			set[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// targetKeyIDs is the per-target key-id membership map the cursor binds to
// (compose ADR § Cursor rules — membership is by immutable key id, per target).
func targetKeyIDs(cfg *compose.Config) map[string][]string {
	out := make(map[string][]string, len(cfg.Targets))
	for name, t := range cfg.Targets {
		out[name] = append([]string(nil), t.Keys...)
	}
	return out
}

// bindSnapshotDelivery completes the already-validated local snapshot scope
// with one live response. PinnedRevision is the resolved pin when the server
// served a pin, else 0 (unpinned "current") — it is NOT schema revision.
func bindSnapshotDelivery(binding crypto.SnapshotBinding, resp apigen.DeliveryResponse) (crypto.SnapshotBinding, error) {
	pinned := int64(0)
	if resp.PinnedRevision != nil {
		pinned = *resp.PinnedRevision
	}
	return binding.WithDelivery(crypto.SnapshotBindingDelivery{
		CredentialID:   resp.CredentialId,
		PinnedRevision: pinned,
		ChangeToken:    resp.ChangeToken,
		Projection:     deliveryProjection(resp.Keys),
		// RFC3339Nano (not RFC3339): the server issues at sub-second precision, and
		// second-truncation would make two fetches within the same wall-clock
		// second collide on the snapshot high-water mark (equal issuance, different
		// ChangeToken → refused as a rollback) even though the second is legitimate
		// forward progress — bricking a publish-then-sync inside one second. Nano
		// precision keeps distinct issuances distinct; a true rollback still has a
		// strictly-older instant and is still refused. RFC3339Nano is valid RFC3339
		// (fractional seconds are permitted), so the stale-line spelling holds.
		IssuedAt:  resp.IssuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: resp.SnapshotExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

// deliveryProjection is the authorized projection recorded in the snapshot AAD,
// derived from what was delivered: `read` always, plus `reveal` when any
// delivered secret carried a value (the values-export rule mirrored). One
// function so the derivation cannot drift between save sites.
func deliveryProjection(keys []apigen.DeliveredKey) []string {
	proj := []string{"read"}
	for _, k := range keys {
		if k.Classification == apigen.KeyClassificationSecret && k.Value != nil {
			proj = append(proj, "reveal")
			break
		}
	}
	return proj
}
