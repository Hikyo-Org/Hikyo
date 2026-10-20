package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Synthetic, disk-backed, authorized bearer polls. Boot, declaration, import,
// publication and credential minting are untimed. Each "changed" request uses a
// genuine cursor from before a publication; each "unchanged" request uses the
// post-publication cursor. The immutable snapshot stays fixed during sampling,
// but production fetch/disclosure audit rows are committed on every iteration.
// Nil budgets exclude admission throttling, not authentication/authorization.
type deliveryPollFixture struct {
	svc       *Delivery
	admin     Actor
	mintActor Actor
	sessionID string
	scope     domain.Scope
	values    *Values
	revisions *Revisions
	keys      map[string]string
	entries   []ImportEntry
}

func newDeliveryPollFixture(t testing.TB, cfg store.Config, count, renderBytes int) deliveryPollFixture {
	t.Helper()
	self, admin := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
	ctx := t.Context()
	org, err := (&Orgs{DB: self.DB}).Create(ctx, admin, "delivery-profile", true, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	project, err := (&Projects{DB: self.DB}).Create(ctx, admin, domain.OrgID(org.ID), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}
	// Admin does not imply audit-read. Grant the fixture's observer that
	// separate capability through the normal grant service before its session.
	if _, err := (&Grants{DB: self.DB, Auth: self.Auth}).Create(ctx, admin, GrantSpec{
		Target: admin.principal, Capability: domain.CapAuditRead, Scope: scope,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&ProjectSettings{DB: self.DB}).SetMachineReveal(ctx, admin, scope, true); err != nil {
		t.Fatal(err)
	}
	keys := &Keys{DB: self.DB, Keyring: self.Keyring}
	f := deliveryPollFixture{svc: &Delivery{DB: self.DB, Keyring: self.Keyring}, admin: admin, keys: map[string]string{}}
	f.mintActor, f.sessionID = selfConfigSession(t, self, admin)
	for i := range count {
		name, class := fmt.Sprintf("SETTING_%04d", i), "config"
		if i%5 == 0 {
			class = "secret"
		}
		key, err := keys.Create(ctx, admin, scope, KeySpec{Name: name, Classification: class,
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.keys[name] = key.ID
		size := renderBytes / count
		if i < renderBytes%count {
			size++
		}
		f.entries = append(f.entries, ImportEntry{Key: name, Value: strings.Repeat("x", size)})
	}
	envs := &Environments{DB: self.DB, Keyring: self.Keyring}
	env, err := envs.Create(ctx, admin, scope, "development", nil)
	if err != nil {
		t.Fatal(err)
	}
	scope.Env = domain.EnvID(env.ID)
	f.scope = scope
	f.values = &Values{DB: self.DB, Keyring: self.Keyring, Auth: self.Auth}
	f.revisions = &Revisions{DB: self.DB, Keyring: self.Keyring, Auth: self.Auth}
	result, err := f.values.Import(ctx, admin, scope, ImportRequest{Entries: f.entries})
	if err != nil || len(result.Imported) != count || len(result.Skipped) != 0 {
		t.Fatalf("fixture import: imported=%d skipped=%d err=%v", len(result.Imported), len(result.Skipped), err)
	}
	return f
}

type deliveryPollCase struct {
	name, bearer     string
	options          FetchOptions
	included, opened int
	expiresAt        time.Time
}

func deliveryPollCases(t testing.TB, f deliveryPollFixture) []deliveryPollCase {
	t.Helper()
	ctx := t.Context()
	project := f.scope
	project.Env = ""
	identities := &Identities{DB: f.svc.DB, Auth: f.values.Auth}
	grants := &Grants{DB: f.svc.DB, Auth: f.values.Auth}
	var cases []deliveryPollCase
	for _, name := range []string{"full", "config-only", "presence-only", "file-target"} {
		sa, err := identities.CreateServiceAccount(ctx, f.admin, project, name, domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		caps := []domain.Capability{domain.CapRead}
		if name != "presence-only" {
			caps = append(caps, domain.CapReveal)
		}
		for _, cap := range caps {
			openDeliveryPollMintWindow(t, f)
			if _, err := grants.Create(ctx, f.mintActor, GrantSpec{Target: sa.Principal, Capability: cap, Scope: f.scope}); err != nil {
				t.Fatal(err)
			}
		}
		openDeliveryPollMintWindow(t, f)
		mint, err := identities.MintCredential(ctx, f.mintActor, project, sa.ID, MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		c := deliveryPollCase{name: name, bearer: mint.Value, included: len(f.entries), opened: len(f.entries), expiresAt: mint.Credential.ExpiresAt}
		switch name {
		case "config-only":
			c.options.Projection = delivery.ModeConfigOnly
			c.included, c.opened = len(f.entries)*4/5, len(f.entries)*4/5
		case "presence-only":
			c.opened = len(f.entries) * 4 / 5
		case "file-target":
			var selected []string
			for i := range len(f.entries) / 10 {
				selected = append(selected, f.keys[fmt.Sprintf("SETTING_%04d", i)])
			}
			target, err := (&FileTargets{DB: f.svc.DB}).Create(ctx, f.admin, project, FileTargetInput{
				EnvironmentID: string(f.scope.Env), Name: "profile-target", ServiceAccountID: sa.ID, KeyIDs: selected})
			if err != nil {
				t.Fatal(err)
			}
			c.options.Target, c.included, c.opened = target.ID, len(selected), len(selected)
		}
		cases = append(cases, c)
	}
	return cases
}

func openDeliveryPollMintWindow(t testing.TB, f deliveryPollFixture) {
	t.Helper()
	intent, err := NewMintReauthIntent(string(f.scope.Env), nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID("raw")
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Write(t.Context(), f.svc.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		// Fixture starts after factor verification. Persist the real mint
		// ceremony binding and consume it through production grant/mint gates.
		ceremonyID := "wac_" + id
		if err := az.CreateWebAuthnCeremony(ctx, authz.NewWebAuthnCeremony{
			ID: ceremonyID, ChallengeVerifier: []byte("synthetic-verified-profile-factor-" + id), SessionData: []byte("{}"),
			SessionID: f.sessionID, Purpose: "reauth", OperationBinding: binding.challengeBinding,
			EnvironmentID: string(f.scope.Env), CredentialEpoch: epoch, ExpiresAt: now.Add(time.Minute), CreatedAt: now,
		}); err != nil {
			return err
		}
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: id, SessionID: f.sessionID,
			EnvironmentID: string(f.scope.Env), CeremonyID: ceremonyID, FactorClass: "webauthn", SingleDecision: true,
			AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute),
			CredentialEpoch: epoch, CreatedAt: now, BoundOperation: string(binding.operation), BoundKeySet: binding.keySet})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func deliveryPollCursors(t testing.TB, f deliveryPollFixture, cases []deliveryPollCase) (before, after []string) {
	t.Helper()
	for _, c := range cases {
		out, err := f.svc.Fetch(t.Context(), c.bearer, f.scope, "", c.options)
		checkDeliveryPoll(t, out, err, c, false)
		checkDeliveryPollContents(t, out, f, c, false)
		before = append(before, out.Cursor)
	}
	// A selected config changes for every projection, without changing render size.
	entry := f.entries[1]
	change, err := f.values.Set(t.Context(), f.admin, f.scope, entry.Key, "y"+entry.Value[1:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.revisions.PublishPlanned(t.Context(), f.admin, f.scope, PublishRequest{VersionIDs: []string{change.VersionID}}); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		out, err := f.svc.Fetch(t.Context(), c.bearer, f.scope, before[i], c.options)
		checkDeliveryPoll(t, out, err, c, false)
		checkDeliveryPollContents(t, out, f, c, true)
		if out.Cursor == before[i] {
			t.Fatal("publication did not invalidate cursor")
		}
		after = append(after, out.Cursor)
	}
	return before, after
}

func checkDeliveryPollContents(t testing.TB, out FetchResult, f deliveryPollFixture, c deliveryPollCase, changed bool) {
	t.Helper()
	want := map[string]ImportEntry{}
	for i, entry := range f.entries {
		if c.name == "file-target" && i >= len(f.entries)/10 || c.name == "config-only" && i%5 == 0 {
			continue
		}
		if changed && i == 1 {
			entry.Value = "y" + entry.Value[1:]
		}
		want[entry.Key] = entry
	}
	for _, key := range out.Keys {
		entry, ok := want[key.Name]
		if !ok || key.KeyID != f.keys[key.Name] || key.Presence != delivery.PresenceSet {
			t.Fatal("unexpected delivered key")
		}
		if key.Value != nil && *key.Value != entry.Value {
			t.Fatal("synthetic delivered value changed unexpectedly")
		}
		delete(want, key.Name)
	}
	if len(want) != 0 {
		t.Fatal("missing delivered keys")
	}
}

func checkDeliveryPoll(t testing.TB, out FetchResult, err error, c deliveryPollCase, current bool) {
	t.Helper()
	wantKeys, wantValues := c.included, c.opened
	if current {
		wantKeys, wantValues = 0, 0
	}
	if err != nil || out.Current != current || out.Cursor == "" || len(out.Keys) != wantKeys {
		t.Fatalf("%s poll: current=%v keys=%d err=%v", c.name, out.Current, len(out.Keys), err)
	}
	values := 0
	for _, key := range out.Keys {
		if key.Value != nil {
			values++
			if key.SnapshotReceipt == nil {
				t.Fatal("delivered value has no receipt")
			}
		} else if key.Classification != "secret" || key.SnapshotReceipt != nil {
			t.Fatal("invalid presence-only key")
		}
	}
	if values != wantValues {
		t.Fatalf("%s delivered values=%d, want %d", c.name, values, wantValues)
	}
}

func BenchmarkDeliveryPoll(b *testing.B) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		b.Run(string(engine), func(b *testing.B) {
			// CI benchmark smoke and CodSpeed do not provision PostgreSQL.
			// The work-count test keeps the normal CI-fatal database requirement.
			if engine == store.EnginePostgres && os.Getenv("HIKYO_TEST_POSTGRES_DSN") == "" {
				b.Skip("optional PostgreSQL benchmark: HIKYO_TEST_POSTGRES_DSN not set")
			}
			for _, size := range []struct {
				name        string
				keys, bytes int
			}{
				{"typical", 100, 100 * 64}, {"key-limit", config.DefaultKeysPerProject, config.DefaultKeysPerProject * 64},
				{"render-limit", config.DefaultKeysPerProject, MaxRenderBytesPerTarget},
			} {
				b.Run(size.name, func(b *testing.B) {
					cfg := store.Config{Engine: engine, Path: filepath.Join(b.TempDir(), "poll.db")}
					if engine == store.EnginePostgres {
						cfg = selfConfigPostgres(b)
					}
					f := newDeliveryPollFixture(b, cfg, size.keys, size.bytes)
					cases := deliveryPollCases(b, f)
					before, after := deliveryPollCursors(b, f, cases)
					for i, c := range cases {
						b.Run(c.name, func(b *testing.B) {
							opens := countDeliveryRows(b, f, c)
							if opens != c.opened {
								b.Fatalf("snapshot opens=%d, want %d", opens, c.opened)
							}
							for _, current := range []bool{true, false} {
								name, cursor := "changed", before[i]
								if current {
									name, cursor = "unchanged", after[i]
								}
								b.Run(name, func(b *testing.B) {
									fetches, disclosures := deliveryPollAuditCounts(b, f)
									b.ReportAllocs()
									pprof.Do(b.Context(), pprof.Labels("delivery-poll", b.Name()), func(ctx context.Context) {
										for b.Loop() {
											out, err := f.svc.Fetch(ctx, c.bearer, f.scope, cursor, c.options)
											checkDeliveryPoll(b, out, err, c, current)
											if out.Cursor != after[i] {
												b.Fatal("poll returned a different post-publication cursor")
											}
										}
									})
									gotFetches, gotDisclosures := deliveryPollAuditCounts(b, f)
									wantDisclosures := c.opened * b.N
									if current {
										wantDisclosures = 0
									}
									if gotFetches-fetches != b.N || gotDisclosures-disclosures != wantDisclosures {
										b.Fatal("poll audit cardinality changed")
									}
									b.ReportMetric(float64(c.included), "included-entries/op")
									b.ReportMetric(float64(opens), "snapshot-opens/op")
									b.ReportMetric(float64(gotFetches-fetches+gotDisclosures-disclosures)/float64(b.N), "audit-rows/op")
								})
							}
						})
					}
				})
			}
		})
	}
}

// Counts actual successful snapshot OpenField calls, not inferred delivery
// cardinality. The same row builder runs before either cursor disposition.
type countingDeliveryOpener struct {
	sealer *crypto.ProjectSealer
	opens  int
}

func (s *countingDeliveryOpener) OpenField(aad crypto.ProjectFieldAAD, ciphertext []byte) ([]byte, error) {
	plain, err := s.sealer.OpenField(aad, ciphertext)
	if err == nil {
		s.opens++
	}
	return plain, err
}

func countDeliveryRows(t testing.TB, f deliveryPollFixture, c deliveryPollCase) int {
	t.Helper()
	sealer, err := sealerFor(t.Context(), f.svc.DB, f.svc.Keyring, Bearer(c.bearer), authz.OpDeliveryFetch, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	opener := &countingDeliveryOpener{sealer: sealer}
	err = tx.Write(t.Context(), f.svc.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, Bearer(c.bearer), authz.OpDeliveryFetch, f.scope, f.svc.now())
		if err != nil {
			return err
		}
		grants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
		if err != nil {
			return err
		}
		only, _, err := fileTargetSelection(ctx, r, p, caller.Principal, f.scope, c.options.Target)
		if err != nil {
			return err
		}
		keys, manifest, _, _, err := deliveryRows(ctx, r, p, opener, f.scope, nil, grants, delivery.NormalizeMode(c.options.Projection), false, nil, only)
		if err == nil && (len(keys) != c.included || len(manifest) != c.included) {
			return fmt.Errorf("included keys=%d manifest=%d, want %d", len(keys), len(manifest), c.included)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return opener.opens
}
