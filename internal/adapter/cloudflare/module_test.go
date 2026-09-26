package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

const (
	testAccount = "0123456789abcdef0123456789abcdef"
	testToken   = "cf_fixture_token_0123456789abcdefghijklmnopq"
)

type fakeAPI struct {
	accounts    []string
	token       TokenStatus
	tokenErr    error
	scriptTag   string
	projectID   string
	names       map[string]string // destination name -> type
	writeErr    map[string]error
	deleteErr   map[string]error
	calls       []string
	pagesWrites map[string]*string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{accounts: []string{testAccount}, token: TokenStatus{Status: "active"}, scriptTag: "tag-1", projectID: "proj-1", names: map[string]string{}, writeErr: map[string]error{}, deleteErr: map[string]error{}, pagesWrites: map[string]*string{}}
}

func (f *fakeAPI) VerifyToken(context.Context, string) (TokenStatus, error) {
	f.calls = append(f.calls, "verify")
	return f.token, f.tokenErr
}
func (f *fakeAPI) ListAccountIDs(context.Context) ([]string, error) { return f.accounts, nil }
func (f *fakeAPI) ResolveScript(context.Context, adapter.Destination) (string, error) {
	if f.scriptTag == "" {
		return "", &ResponseError{Status: http.StatusNotFound}
	}
	return f.scriptTag, nil
}
func (f *fakeAPI) ResolveProject(_ context.Context, d adapter.Destination) (ProjectShape, error) {
	shape := ProjectShape{ID: f.projectID, Names: map[string]string{}}
	for name, kind := range f.names {
		shape.Names[name] = kind
	}
	return shape, nil
}
func (f *fakeAPI) ListSecretNames(context.Context, adapter.Destination) ([]string, error) {
	var out []string
	for name := range f.names {
		out = append(out, name)
	}
	return out, nil
}
func (f *fakeAPI) PutSecret(_ context.Context, _ adapter.Destination, name, _ string) error {
	f.calls = append(f.calls, "put:"+name)
	if err := f.writeErr[name]; err != nil {
		return err
	}
	f.names[name] = SecretType
	return nil
}
func (f *fakeAPI) DeleteSecret(_ context.Context, _ adapter.Destination, name string) error {
	f.calls = append(f.calls, "delete:"+name)
	if err := f.deleteErr[name]; err != nil {
		return err
	}
	delete(f.names, name)
	return nil
}
func (f *fakeAPI) PatchPagesSecret(_ context.Context, d adapter.Destination, name string, value *string) error {
	f.calls = append(f.calls, "patch:"+d.Environment+":"+name)
	f.pagesWrites[name] = value
	if value == nil {
		delete(f.names, name)
		return nil
	}
	if err := f.writeErr[name]; err != nil {
		return err
	}
	f.names[name] = SecretType
	return nil
}

type fakeJournal struct {
	states      map[string]adapter.LedgerState
	completions map[string]adapter.Completion
	refused     []string
	intents     []string
}

func newFakeJournal() *fakeJournal {
	return &fakeJournal{states: map[string]adapter.LedgerState{}, completions: map[string]adapter.Completion{}}
}

func key(effect adapter.Effect) string { return string(effect.Surface) + ":" + effect.EffectiveName }

func (j *fakeJournal) Gate(context.Context, adapter.Effect) error { return nil }
func (j *fakeJournal) Reserve(_ context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	j.states[key(effect)] = adapter.Reserved
	return adapter.Reserved, nil
}
func (j *fakeJournal) Prepare(_ context.Context, effect adapter.Effect, prior adapter.LedgerState) error {
	j.intents = append(j.intents, key(effect))
	if prior == adapter.Reserved {
		j.states[key(effect)] = adapter.Dispatched
	}
	return nil
}
func (j *fakeJournal) Finish(_ context.Context, effect adapter.Effect, completion adapter.Completion) error {
	if err := adapter.ValidateCompletion(completion); err != nil {
		return err
	}
	j.completions[key(effect)] = completion
	if completion.ReleaseLedger {
		delete(j.states, key(effect))
	} else {
		j.states[key(effect)] = completion.State
	}
	return nil
}
func (j *fakeJournal) Refuse(_ context.Context, effect adapter.Effect) error {
	j.refused = append(j.refused, effect.EffectiveName)
	delete(j.states, key(effect))
	return nil
}
func (j *fakeJournal) ReleaseReservation(_ context.Context, effect adapter.Effect) error {
	delete(j.states, key(effect))
	return nil
}
func (j *fakeJournal) ledger() []adapter.LedgerEntry {
	var out []adapter.LedgerEntry
	for k, state := range j.states {
		surface, name, _ := strings.Cut(k, ":")
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(surface), EffectiveName: name, State: state})
	}
	return out
}

func workersTarget() adapter.Target {
	d := adapter.Destination{Kind: adapter.WorkersScript, Owner: testAccount, Name: "api-worker"}
	d.NumericID = Fingerprint(d, "tag-1")
	return adapter.Target{ID: "tgt_1", Destination: d, Generation: 1}
}

func pagesTarget(environment string) adapter.Target {
	d := adapter.Destination{Kind: adapter.PagesProject, Owner: testAccount, Name: "site", Environment: environment}
	d.NumericID = Fingerprint(d, "proj-1")
	return adapter.Target{ID: "tgt_" + environment, Destination: d, Generation: 1}
}

func allow(context.Context) error { return nil }

func manifest() []adapter.ManifestEntry {
	return []adapter.ManifestEntry{
		{KeyID: "key_1", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "s3cret"},
		{KeyID: "key_2", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug"},
	}
}

func TestSyncWritesEverythingAsSecretSentinelFirst(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	result, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest()}, journal)
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	for _, call := range api.calls {
		if strings.HasPrefix(call, "put:") {
			writes = append(writes, call)
		}
	}
	want := []string{"put:" + adapter.SentinelName, "put:LOG_LEVEL", "put:TOKEN"}
	if !slices.Equal(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
	for _, name := range []string{adapter.SentinelName, "LOG_LEVEL", "TOKEN"} {
		if journal.states["secret:"+name] != adapter.Owned {
			t.Fatalf("ledger %s = %q, want owned on the secret surface", name, journal.states["secret:"+name])
		}
	}
	for k := range journal.states {
		if strings.HasPrefix(k, "variable:") {
			t.Fatalf("Cloudflare claimed a plaintext surface: %s", k)
		}
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "deploys a new version") {
		t.Fatalf("deployment side effect not surfaced: %v", result.Warnings)
	}
}

func TestSyncRefusesUnownedNameWithoutWriting(t *testing.T) {
	for _, target := range []adapter.Target{workersTarget(), pagesTarget("production")} {
		t.Run(string(target.Destination.Kind), func(t *testing.T) {
			api := newFakeAPI()
			api.names["TOKEN"] = "plain_text"
			journal := newFakeJournal()
			result, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest()}, journal)
			if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || result.Conflicts[0].EffectiveName != "TOKEN" {
				t.Fatalf("Sync() = %+v, %v; want TOKEN conflict", result, err)
			}
			if slices.ContainsFunc(api.calls, func(call string) bool { return strings.HasSuffix(call, ":TOKEN") }) {
				t.Fatalf("unowned name was overwritten: %v", api.calls)
			}
			if !slices.Equal(journal.refused, []string{"TOKEN"}) {
				t.Fatalf("refused = %v", journal.refused)
			}
		})
	}
}

func TestSyncPagesIsolatesEnvironmentsAndOnlySendsSecretText(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: pagesTarget("preview"), Manifest: manifest()}, journal); err != nil {
		t.Fatal(err)
	}
	for _, call := range api.calls {
		if strings.HasPrefix(call, "patch:") && !strings.HasPrefix(call, "patch:preview:") {
			t.Fatalf("preview target wrote another environment: %s", call)
		}
	}
	if pagesTarget("preview").Destination.NumericID == pagesTarget("production").Destination.NumericID {
		t.Fatal("preview and production share one ownership destination id")
	}
}

func TestSyncRefusesMovedDestination(t *testing.T) {
	api := newFakeAPI()
	api.scriptTag = "tag-recreated"
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest()}, newFakeJournal())
	if !errors.Is(err, adapter.ErrDestinationID) {
		t.Fatalf("Sync() = %v, want destination identity refusal", err)
	}
	if slices.ContainsFunc(api.calls, func(call string) bool { return strings.HasPrefix(call, "put:") }) {
		t.Fatal("wrote into a recreated script")
	}
}

func TestSyncRefusesExpiredOrInactiveToken(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for name, token := range map[string]TokenStatus{
		"expired":  {Status: "active", ExpiresAt: now.Add(-time.Minute)},
		"disabled": {Status: "disabled"},
	} {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI()
			api.token = token
			_, err := (&Module{API: api, Now: func() time.Time { return now }}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest()}, newFakeJournal())
			if !errors.Is(err, adapter.ErrProviderAuth) {
				t.Fatalf("Sync() = %v, want provider auth refusal", err)
			}
		})
	}
}

func TestAmbiguousWriteStaysDispatchedAndReplaysAsUpdate(t *testing.T) {
	api := newFakeAPI()
	api.writeErr["TOKEN"] = ErrAmbiguousResponse
	journal := newFakeJournal()
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest()}, journal)
	if !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("Sync() = %v, want indeterminate", err)
	}
	if journal.states["secret:TOKEN"] != adapter.Dispatched || journal.completions["secret:TOKEN"].Outcome != adapter.OutcomeUnknown {
		t.Fatalf("state=%q completion=%+v", journal.states["secret:TOKEN"], journal.completions["secret:TOKEN"])
	}
	// The ambiguous write may have landed: the replay must not treat the
	// now-present name as an unowned conflict.
	api.names["TOKEN"] = SecretType
	delete(api.writeErr, "TOKEN")
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest(), Ledger: journal.ledger()}, journal); err != nil {
		t.Fatalf("replay = %v", err)
	}
	if journal.states["secret:TOKEN"] != adapter.Owned {
		t.Fatalf("replay state = %q", journal.states["secret:TOKEN"])
	}
}

func TestDefiniteRefusalReleasesFreshReservation(t *testing.T) {
	api := newFakeAPI()
	api.writeErr["TOKEN"] = errors.Join(adapter.ErrProviderAuth, &ResponseError{Status: http.StatusForbidden})
	journal := newFakeJournal()
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Manifest: manifest()}, journal)
	if err == nil || !strings.Contains(err.Error(), "Workers Scripts: Edit") {
		t.Fatalf("Sync() = %v, want permission guidance", err)
	}
	if _, ok := journal.states["secret:TOKEN"]; ok || journal.completions["secret:TOKEN"].Outcome != adapter.OutcomeFailure {
		t.Fatalf("state=%v completion=%+v", journal.states, journal.completions["secret:TOKEN"])
	}
}

func TestPruneDeletesOnlyOwnedNamesSentinelLast(t *testing.T) {
	api := newFakeAPI()
	api.names = map[string]string{adapter.SentinelName: SecretType, "OLD": SecretType, "FOREIGN": SecretType}
	journal := newFakeJournal()
	journal.states["secret:"+adapter.SentinelName] = adapter.Owned
	journal.states["secret:OLD"] = adapter.Owned
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Ledger: journal.ledger(), Teardown: true}, journal)
	if err != nil {
		t.Fatal(err)
	}
	var deletes []string
	for _, call := range api.calls {
		if strings.HasPrefix(call, "delete:") {
			deletes = append(deletes, call)
		}
	}
	if !slices.Equal(deletes, []string{"delete:OLD", "delete:" + adapter.SentinelName}) {
		t.Fatalf("deletes = %v", deletes)
	}
	if _, ok := api.names["FOREIGN"]; !ok {
		t.Fatal("prune removed a name Hikyo never owned")
	}
}

func TestPruneTreatsAlreadyGoneAsDeleted(t *testing.T) {
	api := newFakeAPI()
	api.deleteErr["OLD"] = &ResponseError{Status: http.StatusNotFound}
	journal := newFakeJournal()
	journal.states["secret:OLD"] = adapter.Owned
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: workersTarget(), Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
		t.Fatal(err)
	}
	if journal.states["secret:OLD"] != adapter.Released {
		t.Fatalf("state = %q", journal.states["secret:OLD"])
	}
}

func TestPagesPruneSendsNullDelete(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	journal.states["secret:OLD"] = adapter.Owned
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: pagesTarget("production"), Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
		t.Fatal(err)
	}
	value, ok := api.pagesWrites["OLD"]
	if !ok || value != nil {
		t.Fatalf("pages delete body = %v, %v; want explicit null", value, ok)
	}
}

func TestPlanIsValueBlindAndReportsConflicts(t *testing.T) {
	api := newFakeAPI()
	api.names["TOKEN"] = "plain_text"
	plan, err := (&Module{API: api}).Plan(t.Context(), adapter.PlanRequest{Target: pagesTarget("production"), Gate: allow, Manifest: []adapter.ManifestEntry{
		{CanonicalName: "TOKEN", Classification: adapter.SecretClassification},
		{CanonicalName: "MODE", Classification: adapter.ConfigClassification},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Change{
		{Surface: adapter.Secret, EffectiveName: adapter.SentinelName, Disposition: adapter.Create},
		{Surface: adapter.Secret, EffectiveName: "MODE", Disposition: adapter.Create},
		{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Conflict},
	}
	if !slices.Equal(plan.Changes, want) {
		t.Fatalf("Plan() = %+v, want %+v", plan.Changes, want)
	}
	if slices.ContainsFunc(api.calls, func(call string) bool { return strings.Contains(call, ":") }) {
		t.Fatalf("Plan wrote to the provider: %v", api.calls)
	}
}

func TestConnectionRefusesOverbroadTokens(t *testing.T) {
	api := newFakeAPI()
	api.accounts = []string{testAccount, "fedcba9876543210fedcba9876543210"}
	_, err := (&Module{API: api}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: workersTarget().Destination, Access: adapter.Access{Credential: testToken}, Gate: allow})
	if err == nil || !strings.Contains(err.Error(), "overbroad") {
		t.Fatalf("TestConnection() = %v, want overbroad refusal", err)
	}
}

func TestConnectionRefusesGlobalAPIKey(t *testing.T) {
	_, err := (&Module{API: newFakeAPI()}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: workersTarget().Destination, Access: adapter.Access{Credential: "0123456789abcdef0123456789abcdef01234"}, Gate: allow})
	if err == nil || !strings.Contains(err.Error(), "Global API Key") {
		t.Fatalf("TestConnection() = %v, want Global API Key refusal", err)
	}
}

func TestConnectionResolvesFingerprintAndExpiry(t *testing.T) {
	api := newFakeAPI()
	expires := time.Now().Add(24 * time.Hour).UTC()
	api.token.ExpiresAt = expires
	got, err := (&Module{API: api}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.PagesProject, Owner: testAccount, Name: "site", Environment: "preview"}, Access: adapter.Access{Credential: testToken}, Gate: allow})
	if err != nil {
		t.Fatal(err)
	}
	if got.DestinationID != pagesTarget("preview").Destination.NumericID || !got.CredentialExpiresAt.Equal(expires) {
		t.Fatalf("connection = %+v", got)
	}
}

func TestDestinationShapeRules(t *testing.T) {
	for name, d := range map[string]adapter.Destination{
		"github kind":       {Kind: adapter.Repository, Owner: testAccount, Name: "x"},
		"bad account":       {Kind: adapter.WorkersScript, Owner: "acme", Name: "api"},
		"worker env":        {Kind: adapter.WorkersScript, Owner: testAccount, Name: "api", Environment: "production"},
		"pages no env":      {Kind: adapter.PagesProject, Owner: testAccount, Name: "site"},
		"pages bad env":     {Kind: adapter.PagesProject, Owner: testAccount, Name: "site", Environment: "staging"},
		"uppercase project": {Kind: adapter.PagesProject, Owner: testAccount, Name: "Site", Environment: "preview"},
	} {
		if err := ValidateDestination(d); err == nil {
			t.Errorf("%s: ValidateDestination accepted %+v", name, d)
		}
	}
}
