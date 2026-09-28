package gitlab

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

var testNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

type fakeAPI struct {
	version      string
	token        TokenInfo
	tokenErr     error
	id           int64
	path         string
	createStatus map[string]int
	createErr    map[string]error
	updateStatus map[string]int
	deleteErr    map[string]error
	calls        []string
	writes       []Variable
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		version: "17.5.0-ee", id: 42, path: "platform/api",
		token: TokenInfo{Scopes: []string{"api"}, Active: true, Bot: true},
	}
}

func (f *fakeAPI) Version(context.Context) (string, error) {
	f.calls = append(f.calls, "version")
	return f.version, nil
}

func (f *fakeAPI) Token(context.Context) (TokenInfo, error) {
	f.calls = append(f.calls, "token")
	return f.token, f.tokenErr
}

func (f *fakeAPI) ResolveDestination(_ context.Context, d adapter.Destination) (DestinationIdentity, error) {
	f.calls = append(f.calls, "resolve")
	return DestinationIdentity{ID: f.id, Path: f.path}, nil
}

func (f *fakeAPI) CreateVariable(_ context.Context, _ adapter.Destination, v Variable) (WriteResult, error) {
	f.calls = append(f.calls, "create:"+v.Key)
	f.writes = append(f.writes, v)
	if err := f.createErr[v.Key]; err != nil {
		return WriteResult{Status: http.StatusBadRequest}, err
	}
	if status, ok := f.createStatus[v.Key]; ok {
		return WriteResult{Status: status}, statusError(status)
	}
	return WriteResult{Status: http.StatusCreated}, nil
}

func (f *fakeAPI) UpdateVariable(_ context.Context, _ adapter.Destination, v Variable) (WriteResult, error) {
	f.calls = append(f.calls, "update:"+v.Key)
	f.writes = append(f.writes, v)
	if status, ok := f.updateStatus[v.Key]; ok {
		return WriteResult{Status: status}, statusError(status)
	}
	return WriteResult{Status: http.StatusOK}, nil
}

func (f *fakeAPI) DeleteVariable(_ context.Context, _ adapter.Destination, key, scope string) error {
	f.calls = append(f.calls, "delete:"+key+"@"+scope)
	return f.deleteErr[key]
}

func statusError(status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return &ResponseError{Status: status}
}

type fakeJournal struct {
	states      map[string]adapter.LedgerState
	missing     map[string]bool
	completions map[string]adapter.Completion
	refused     []string
	conflicts   []string
	gateErr     error
}

func newFakeJournal() *fakeJournal {
	return &fakeJournal{states: map[string]adapter.LedgerState{}, missing: map[string]bool{}, completions: map[string]adapter.Completion{}}
}

func journalKey(e adapter.Effect) string { return string(e.Surface) + ":" + e.EffectiveName }

func (j *fakeJournal) Gate(context.Context, adapter.Effect) error { return j.gateErr }

func (j *fakeJournal) Reserve(_ context.Context, e adapter.Effect) (adapter.LedgerState, error) {
	j.states[journalKey(e)] = adapter.Reserved
	return adapter.Reserved, nil
}

func (j *fakeJournal) Prepare(_ context.Context, e adapter.Effect, prior adapter.LedgerState) error {
	if prior == adapter.Reserved {
		j.states[journalKey(e)] = adapter.Dispatched
	}
	return nil
}

func (j *fakeJournal) Finish(_ context.Context, e adapter.Effect, c adapter.Completion) error {
	if err := adapter.ValidateCompletion(c); err != nil {
		return err
	}
	j.completions[journalKey(e)] = c
	if c.Conflict {
		j.conflicts = append(j.conflicts, journalKey(e))
	}
	if c.ReleaseLedger {
		delete(j.states, journalKey(e))
		return nil
	}
	j.states[journalKey(e)] = c.State
	j.missing[journalKey(e)] = c.Missing
	return nil
}

func (j *fakeJournal) Refuse(_ context.Context, e adapter.Effect) error {
	j.refused = append(j.refused, journalKey(e))
	delete(j.states, journalKey(e))
	return nil
}

func (j *fakeJournal) ReleaseReservation(_ context.Context, e adapter.Effect) error {
	delete(j.states, journalKey(e))
	return nil
}

func (j *fakeJournal) ledger() []adapter.LedgerEntry {
	var out []adapter.LedgerEntry
	for key, state := range j.states {
		surface, name, _ := strings.Cut(key, ":")
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(surface), EffectiveName: name, State: state, Missing: j.missing[key]})
	}
	return out
}

func allow(context.Context) error { return nil }

func testTarget() adapter.Target {
	return adapter.Target{
		ID: "tgt_1", Generation: 1,
		Destination: adapter.Destination{Kind: adapter.Repository, Owner: "platform", Name: "api", NumericID: 42, Scope: "production"},
	}
}

func testModule(api *fakeAPI) *Module {
	return &Module{API: api, Now: func() time.Time { return testNow }}
}

const maskable = "s3cr3t-value-0123"

func TestAPIHasNoVariableReadAndRoutesCannotExpressOne(t *testing.T) {
	got := apiMethods()
	want := []string{"CreateVariable", "DeleteVariable", "ResolveDestination", "Token", "UpdateVariable", "Version"}
	if !slices.Equal(got, want) {
		t.Fatalf("linked GitLab API operations = %v, want closed value-free set %v", got, want)
	}
	for name, op := range operationRegistry {
		if op.Method == http.MethodGet && strings.Contains(op.Path, "variables") {
			t.Errorf("operation %s links forbidden variable read %s %s", name, op.Method, op.Path)
		}
		if strings.Contains(op.Path, "variables") && !op.Mutation {
			t.Errorf("operation %s addresses variables without being a mutation", name)
		}
	}
}

func TestPlanIsLedgerOnlyAndReadFree(t *testing.T) {
	api := newFakeAPI()
	plan, err := testModule(api).Plan(t.Context(), adapter.PlanRequest{
		Target: testTarget(), Gate: allow,
		Manifest: []adapter.ManifestEntry{
			{CanonicalName: "TOKEN", Classification: adapter.SecretClassification},
			{CanonicalName: "MODE", Classification: adapter.ConfigClassification},
		},
		Ledger: []adapter.LedgerEntry{{Surface: adapter.Variable, EffectiveName: "MODE", State: adapter.Owned}, {Surface: adapter.Variable, EffectiveName: "OLD", State: adapter.Owned}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Change{
		{Surface: adapter.Variable, EffectiveName: adapter.SentinelName, Disposition: adapter.Unknown},
		{Surface: adapter.Variable, EffectiveName: "MODE", Disposition: adapter.Update},
		{Surface: adapter.Variable, EffectiveName: "OLD", Disposition: adapter.Delete},
		{Surface: adapter.Variable, EffectiveName: "TOKEN", Disposition: adapter.Unknown},
	}
	if !slices.Equal(plan.Changes, want) {
		t.Fatalf("Plan() = %+v, want %+v", plan.Changes, want)
	}
	if !slices.Equal(api.calls, []string{"resolve"}) {
		t.Fatalf("Plan called %v, want destination resolution only", api.calls)
	}
}

func TestSyncWritesSentinelFirstAndAppliesFlags(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	target := testTarget()
	target.Options = adapter.VariableOptions{Protected: true, Hidden: true}
	result, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: target,
		Manifest: []adapter.ManifestEntry{
			{KeyID: "k1", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: maskable},
			{KeyID: "k2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug $HOME"},
		},
	}, journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 3 || api.writes[0].Key != adapter.SentinelName {
		t.Fatalf("writes = %+v", api.writes)
	}
	byKey := map[string]Variable{}
	for _, write := range api.writes {
		byKey[write.Key] = write
	}
	sentinel, mode, token := byKey[adapter.SentinelName], byKey["MODE"], byKey["TOKEN"]
	if sentinel.Protected || sentinel.Masked || !sentinel.Raw || sentinel.Scope != "production" {
		t.Fatalf("sentinel = %+v", sentinel)
	}
	if !mode.Protected || mode.Masked || mode.Hidden || !mode.Raw || mode.Value != "debug $HOME" {
		t.Fatalf("config variable = %+v", mode)
	}
	if !token.Protected || !token.Masked || !token.Hidden || !token.Raw {
		t.Fatalf("secret variable = %+v", token)
	}
	for _, key := range []string{"variable:" + adapter.SentinelName, "variable:MODE", "variable:TOKEN"} {
		if journal.states[key] != adapter.Owned {
			t.Fatalf("state[%s] = %q", key, journal.states[key])
		}
	}
	if _, ok := journal.states["secret:"+adapter.SentinelName]; ok {
		t.Fatal("GitLab has one variable namespace; no secret-surface sentinel may exist")
	}
}

func TestSyncTakenCreateIsConflictAndReleasesReservation(t *testing.T) {
	api := newFakeAPI()
	api.createErr = map[string]error{"TOKEN": &ResponseError{Status: http.StatusBadRequest, Taken: true}}
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	result, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: testTarget(), Ledger: journal.ledger(),
		Manifest: []adapter.ManifestEntry{{KeyID: "k1", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: maskable}},
	}, journal)
	if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 {
		t.Fatalf("Sync() = %+v, %v; want exists-unowned conflict", result, err)
	}
	if _, ok := journal.states["variable:TOKEN"]; ok || !slices.Contains(journal.conflicts, "variable:TOKEN") {
		t.Fatalf("states=%v conflicts=%v", journal.states, journal.conflicts)
	}
	if slices.Contains(api.calls, "update:TOKEN") {
		t.Fatal("an unowned variable was overwritten")
	}
	if strings.Contains(err.Error(), maskable) {
		t.Fatal("conflict error leaked plaintext")
	}
}

func TestSyncOwnedMissingRecreates(t *testing.T) {
	api := newFakeAPI()
	api.updateStatus = map[string]int{"MODE": http.StatusNotFound}
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	journal.states["variable:MODE"] = adapter.Owned
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: testTarget(), Ledger: journal.ledger(),
		Manifest: []adapter.ManifestEntry{{KeyID: "k2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug"}},
	}, journal)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(api.calls, "update:MODE") || !slices.Contains(api.calls, "create:MODE") || journal.states["variable:MODE"] != adapter.Owned || journal.missing["variable:MODE"] {
		t.Fatalf("calls=%v state=%v missing=%v", api.calls, journal.states, journal.missing)
	}
}

func TestSyncOwnedMissingThenTakenKeepsOwnershipAndFlagsConflict(t *testing.T) {
	api := newFakeAPI()
	api.updateStatus = map[string]int{"MODE": http.StatusNotFound}
	api.createErr = map[string]error{"MODE": &ResponseError{Status: http.StatusBadRequest, Taken: true}}
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	journal.states["variable:MODE"] = adapter.Owned
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: testTarget(), Ledger: journal.ledger(),
		Manifest: []adapter.ManifestEntry{{KeyID: "k2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug"}},
	}, journal)
	if !errors.Is(err, adapter.ErrConflict) || journal.states["variable:MODE"] != adapter.Owned || !journal.missing["variable:MODE"] {
		t.Fatalf("err=%v states=%v missing=%v", err, journal.states, journal.missing)
	}
}

func TestSyncPrunesOnlyLedgerOwnedAndSentinelLastOnTeardown(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	journal.states["variable:OLD"] = adapter.Owned
	journal.states["variable:MODE"] = adapter.Owned
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Ledger: journal.ledger(), Teardown: true}, journal)
	if err != nil {
		t.Fatal(err)
	}
	var deletes []string
	for _, call := range api.calls {
		if strings.HasPrefix(call, "delete:") {
			deletes = append(deletes, call)
		}
	}
	want := []string{"delete:MODE@production", "delete:OLD@production", "delete:" + adapter.SentinelName + "@production"}
	if !slices.Equal(deletes, want) {
		t.Fatalf("deletes = %v, want %v", deletes, want)
	}
	if len(journal.states) != 3 || journal.states["variable:OLD"] != adapter.Released {
		t.Fatalf("states = %v", journal.states)
	}
}

func TestSyncPruneFailureKeepsOwnership(t *testing.T) {
	api := newFakeAPI()
	api.deleteErr = map[string]error{"OLD": &ResponseError{Status: http.StatusForbidden}}
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	journal.states["variable:OLD"] = adapter.Owned
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Ledger: journal.ledger()}, journal)
	if err == nil || journal.states["variable:OLD"] != adapter.Owned {
		t.Fatalf("err=%v states=%v", err, journal.states)
	}
}

func TestSyncAmbiguousWriteIsIndeterminateAndDispatched(t *testing.T) {
	api := newFakeAPI()
	api.createStatus = map[string]int{"MODE": http.StatusBadGateway}
	journal := newFakeJournal()
	journal.states["variable:"+adapter.SentinelName] = adapter.Owned
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: testTarget(), Ledger: journal.ledger(),
		Manifest: []adapter.ManifestEntry{{KeyID: "k2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug"}},
	}, journal)
	if !errors.Is(err, adapter.ErrIndeterminate) || journal.states["variable:MODE"] != adapter.Dispatched {
		t.Fatalf("err=%v states=%v", err, journal.states)
	}
	// Replay: the dispatched row is updated, never re-created blindly.
	api.createStatus = nil
	api.calls = nil
	if _, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
		Target: testTarget(), Ledger: journal.ledger(),
		Manifest: []adapter.ManifestEntry{{KeyID: "k2", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug"}},
	}, journal); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(api.calls, "update:MODE") || journal.states["variable:MODE"] != adapter.Owned {
		t.Fatalf("replay calls=%v states=%v", api.calls, journal.states)
	}
}

func TestSyncRefusesUnmaskableSecretByNameBeforeAnyWrite(t *testing.T) {
	for _, value := range []string{"short", "has space in it", "multi\nline-value", "quote\"value!"} {
		api := newFakeAPI()
		_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{
			Target:   testTarget(),
			Manifest: []adapter.ManifestEntry{{KeyID: "k1", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: value}},
		}, newFakeJournal())
		if err == nil || !strings.Contains(err.Error(), "TOKEN") || strings.Contains(err.Error(), value) {
			t.Fatalf("value %q: err = %v; want refusal naming key without plaintext", value, err)
		}
		if len(api.writes) != 0 {
			t.Fatalf("value %q: wrote %+v before refusing", value, api.writes)
		}
	}
}

func TestSyncRefusesMovedProject(t *testing.T) {
	api := newFakeAPI()
	api.path = "someone-else/api"
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: testTarget()}, newFakeJournal())
	if !errors.Is(err, adapter.ErrDestinationID) || len(api.writes) != 0 {
		t.Fatalf("err=%v writes=%v", err, api.writes)
	}
}

func TestTokenPolicy(t *testing.T) {
	for _, tt := range []struct {
		name    string
		token   TokenInfo
		allow   bool
		wantErr error
		want    string
	}{
		{name: "personal refused", token: TokenInfo{Scopes: []string{"api"}, Active: true}, wantErr: ErrPersonalToken},
		{name: "personal allowed", token: TokenInfo{Scopes: []string{"api"}, Active: true}, allow: true},
		{name: "missing api", token: TokenInfo{Scopes: []string{"read_api"}, Active: true, Bot: true}, want: "api scope"},
		{name: "sudo", token: TokenInfo{Scopes: []string{"api", "sudo"}, Active: true, Bot: true}, want: "sudo"},
		{name: "revoked", token: TokenInfo{Scopes: []string{"api"}, Revoked: true, Bot: true}, wantErr: adapter.ErrProviderAuth},
		{name: "expired", token: TokenInfo{Scopes: []string{"api"}, Active: true, Bot: true, ExpiresAt: testNow.Add(-time.Hour)}, wantErr: adapter.ErrProviderAuth},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI()
			api.token = tt.token
			_, err := testModule(api).TestConnection(t.Context(), adapter.ConnectionRequest{
				Config: adapter.Config{Origin: "https://gitlab.example", AllowPersonalToken: tt.allow}, Destination: adapter.Destination{Kind: adapter.Repository, Owner: "platform", Name: "api", Scope: "*"}, Gate: allow,
			})
			switch {
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("err = %v, want %q", err, tt.want)
			case tt.wantErr == nil && tt.want == "" && err != nil:
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSyncWarnsBeforeTokenExpiry(t *testing.T) {
	api := newFakeAPI()
	api.token.ExpiresAt = testNow.Add(7 * 24 * time.Hour)
	journal := newFakeJournal()
	result, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: testTarget()}, journal)
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "expires") {
		t.Fatalf("Sync() = %+v, %v", result, err)
	}
}

func TestConnectionResolvesByPathAndReportsExpiry(t *testing.T) {
	api := newFakeAPI()
	api.token.ExpiresAt = testNow.Add(90 * 24 * time.Hour)
	connection, err := testModule(api).TestConnection(t.Context(), adapter.ConnectionRequest{
		Destination: adapter.Destination{Kind: adapter.Repository, Owner: "Platform", Name: "API", Scope: "*"}, Gate: allow,
	})
	if err != nil || connection.DestinationID != 42 || !connection.CredentialExpiresAt.Equal(api.token.ExpiresAt) {
		t.Fatalf("TestConnection() = %+v, %v", connection, err)
	}
	api.version = "15.11.0"
	if _, err := testModule(api).TestConnection(t.Context(), adapter.ConnectionRequest{
		Destination: adapter.Destination{Kind: adapter.Repository, Owner: "platform", Name: "api", Scope: "*"}, Gate: allow,
	}); !errors.Is(err, adapter.ErrVersionFloor) {
		t.Fatalf("old GitLab err = %v", err)
	}
}

func TestHiddenRequiresSupportingVersion(t *testing.T) {
	api := newFakeAPI()
	api.version = "17.3.2"
	target := testTarget()
	target.Options.Hidden = true
	_, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: target}, newFakeJournal())
	if !errors.Is(err, adapter.ErrVersionFloor) || len(api.writes) != 0 {
		t.Fatalf("err=%v writes=%v", err, api.writes)
	}
}

func TestGateFailureStopsBeforeProviderCalls(t *testing.T) {
	api := newFakeAPI()
	journal := newFakeJournal()
	journal.gateErr = adapter.ErrSuperseded
	if _, err := testModule(api).Sync(t.Context(), adapter.SyncRequest{Target: testTarget()}, journal); !errors.Is(err, adapter.ErrSuperseded) || len(api.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, api.calls)
	}
}

func TestValidateDestination(t *testing.T) {
	for _, d := range []adapter.Destination{
		{Kind: adapter.Repository, Owner: "g", Scope: "*"},
		{Kind: adapter.Organization, Owner: "g", Name: "p", Scope: "*"},
		{Kind: adapter.Environment, Owner: "g", Name: "p", Scope: "*"},
		{Kind: adapter.Repository, Owner: "g", Name: "p"},
		{Kind: adapter.Repository, Owner: "g", Name: "p", Scope: " prod"},
		{Kind: adapter.Repository, Owner: "g", Name: "p", Scope: "*", Visibility: "all"},
	} {
		if validateDestination(d) == nil {
			t.Errorf("validateDestination(%+v) accepted", d)
		}
	}
	if err := validateDestination(adapter.Destination{Kind: adapter.Organization, Owner: "g/sub", Scope: "review/*"}); err != nil {
		t.Fatal(err)
	}
}
