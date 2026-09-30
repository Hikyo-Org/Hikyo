package vaultkv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// fakeKV is an in-memory KV v2 mount with Vault's check-and-set, custom
// metadata, and soft-delete semantics. It records values only so tests can
// prove what reached the destination; the module can never read them back.
type fakeKV struct {
	mount        Mount
	paths        map[string]*fakePath
	calls        []string
	sealed       bool
	failOn       map[string]error
	applyOn      map[string]error // apply the write, then return this error
	beforeDelete func(string)
}

type fakePath struct {
	current int64
	custom  map[string]string
	values  map[int64]string
	deleted map[int64]bool
}

func newFakeKV() *fakeKV {
	return &fakeKV{
		mount:   Mount{Type: "kv", Version: "2", UUID: "mount-uuid-1", Accessor: "kv_1"},
		paths:   map[string]*fakePath{},
		failOn:  map[string]error{},
		applyOn: map[string]error{},
	}
}

func (f *fakeKV) record(call string) error {
	f.calls = append(f.calls, call)
	if err, ok := f.failOn[call]; ok {
		delete(f.failOn, call)
		return err
	}
	return nil
}

func (f *fakeKV) applied(call string) error {
	if err, ok := f.applyOn[call]; ok {
		delete(f.applyOn, call)
		return err
	}
	return nil
}

func (f *fakeKV) Health(context.Context) (Health, error) {
	return Health{Initialized: true, Sealed: f.sealed, Version: "2.1.1"}, f.record("health")
}

func (f *fakeKV) MountInfo(_ context.Context, mount string) (Mount, error) {
	if err := f.record("mount:" + mount); err != nil {
		return Mount{}, err
	}
	return f.mount, nil
}

func (f *fakeKV) LookupSelf(context.Context) (TokenInfo, error) {
	return TokenInfo{}, f.record("lookup-self")
}

func (f *fakeKV) ReadMetadata(_ context.Context, _, path string) (Metadata, error) {
	if err := f.record("read-metadata:" + path); err != nil {
		return Metadata{}, err
	}
	p := f.paths[path]
	if p == nil {
		return Metadata{}, &ResponseError{Status: 404}
	}
	meta := Metadata{CurrentVersion: p.current, CustomMetadata: map[string]string{}, Versions: map[int64]VersionMetadata{}}
	for k, v := range p.custom {
		meta.CustomMetadata[k] = v
	}
	for version := range p.values {
		meta.Versions[version] = VersionMetadata{Deleted: p.deleted[version]}
	}
	return meta, nil
}

func (f *fakeKV) PatchCustomMetadata(_ context.Context, _, path string, custom map[string]*string) error {
	if err := f.record("patch-metadata:" + path); err != nil {
		return err
	}
	p := f.paths[path]
	if p == nil {
		return &ResponseError{Status: 404}
	}
	if p.custom == nil {
		p.custom = map[string]string{}
	}
	for k, v := range custom {
		if v == nil {
			delete(p.custom, k)
		} else {
			p.custom[k] = *v
		}
	}
	return f.applied("patch-metadata:" + path)
}

func (f *fakeKV) WriteCAS(_ context.Context, _, path, value string, cas int64) (int64, error) {
	if err := f.record("write:" + path); err != nil {
		return 0, err
	}
	p := f.paths[path]
	if p == nil {
		p = &fakePath{values: map[int64]string{}, deleted: map[int64]bool{}}
		f.paths[path] = p
	}
	if p.current != cas {
		return 0, &ResponseError{Status: 400, CASMismatch: true}
	}
	p.current++
	p.values[p.current] = value
	return p.current, f.applied("write:" + path)
}

func (f *fakeKV) DeleteVersion(_ context.Context, _, path string, version int64) error {
	if f.beforeDelete != nil {
		f.beforeDelete(path)
	}
	if err := f.record("soft-delete:" + path); err != nil {
		return err
	}
	p := f.paths[path]
	if p == nil {
		return &ResponseError{Status: 404}
	}
	p.deleted[version] = true
	return f.applied("soft-delete:" + path)
}

// externalWrite simulates another writer, bypassing Hikyo's protocol.
func (f *fakeKV) externalWrite(path, value string) {
	p := f.paths[path]
	if p == nil {
		p = &fakePath{values: map[int64]string{}, deleted: map[int64]bool{}, custom: map[string]string{}}
		f.paths[path] = p
	}
	p.current++
	p.values[p.current] = value
}

func (f *fakeKV) value(path string) (string, bool) {
	p := f.paths[path]
	if p == nil || p.current == 0 || p.deleted[p.current] {
		return "", false
	}
	return p.values[p.current], true
}

func (f *fakeKV) mutations() []string {
	var out []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "write:") || strings.HasPrefix(call, "patch-metadata:") || strings.HasPrefix(call, "soft-delete:") {
			out = append(out, call)
		}
	}
	return out
}

type fakeJournal struct {
	states    map[string]adapter.LedgerState
	missing   map[string]bool
	conflicts []string
	outcomes  []string
	refusals  int
	releases  map[string]int
	gateErr   error
	finishErr error
}

func newFakeJournal() *fakeJournal {
	return &fakeJournal{states: map[string]adapter.LedgerState{}, missing: map[string]bool{}, releases: map[string]int{}}
}

func journalKey(effect adapter.Effect) string {
	return string(effect.Surface) + ":" + strings.ToUpper(effect.EffectiveName)
}

func (j *fakeJournal) Gate(context.Context, adapter.Effect) error { return j.gateErr }
func (j *fakeJournal) Reserve(_ context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	j.states[journalKey(effect)] = adapter.Reserved
	return adapter.Reserved, nil
}
func (j *fakeJournal) Prepare(_ context.Context, effect adapter.Effect, prior adapter.LedgerState) error {
	if prior == adapter.Reserved {
		j.states[journalKey(effect)] = adapter.Dispatched
	}
	return nil
}
func (j *fakeJournal) Finish(_ context.Context, effect adapter.Effect, completion adapter.Completion) error {
	if err := adapter.ValidateCompletion(completion); err != nil {
		return err
	}
	if j.finishErr != nil {
		return j.finishErr
	}
	key := journalKey(effect)
	j.outcomes = append(j.outcomes, key+"="+string(completion.Outcome))
	if completion.Conflict {
		j.conflicts = append(j.conflicts, key)
	}
	if completion.ReleaseLedger {
		delete(j.states, key)
		delete(j.missing, key)
		return nil
	}
	j.states[key] = completion.State
	j.missing[key] = completion.Missing
	return nil
}
func (j *fakeJournal) Refuse(_ context.Context, effect adapter.Effect) error {
	j.refusals++
	j.conflicts = append(j.conflicts, journalKey(effect))
	delete(j.states, journalKey(effect))
	return nil
}
func (j *fakeJournal) ReleaseReservation(_ context.Context, effect adapter.Effect) error {
	j.releases[journalKey(effect)]++
	delete(j.states, journalKey(effect))
	return nil
}

func (j *fakeJournal) ledger() []adapter.LedgerEntry {
	var out []adapter.LedgerEntry
	for key, state := range j.states {
		if state == adapter.Released {
			continue
		}
		surface, name, _ := strings.Cut(key, ":")
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(surface), EffectiveName: name, State: state, Missing: j.missing[key]})
	}
	return out
}

func testTarget(t *testing.T, kv *fakeKV) adapter.Target {
	t.Helper()
	id, err := DestinationID(kv.mount, "apps/pay")
	if err != nil {
		t.Fatal(err)
	}
	return adapter.Target{
		ID:          "tgt_1",
		Destination: adapter.Destination{Kind: adapter.Repository, Owner: "secret", Name: "apps/pay", NumericID: id},
		Generation:  1,
	}
}

var manifest = []adapter.ManifestEntry{
	{KeyID: "key_db", CanonicalName: "DATABASE_URL", Classification: adapter.SecretClassification, Value: "postgres://one"},
	{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug"},
}

func TestSyncCreatesWithCASAndMarksOwnershipSentinelFirst(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	result, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"write:apps/pay/MANAGED_BY_HIKYO", "patch-metadata:apps/pay/MANAGED_BY_HIKYO",
		"write:apps/pay/DATABASE_URL", "patch-metadata:apps/pay/DATABASE_URL",
		"write:apps/pay/LOG_LEVEL", "patch-metadata:apps/pay/LOG_LEVEL",
	}
	if got := kv.mutations(); !slices.Equal(got, want) {
		t.Fatalf("mutations = %v, want %v", got, want)
	}
	for path, value := range map[string]string{"apps/pay/DATABASE_URL": "postgres://one", "apps/pay/LOG_LEVEL": "debug", "apps/pay/MANAGED_BY_HIKYO": adapter.SentinelName} {
		if got, ok := kv.value(path); !ok || got != value {
			t.Errorf("%s = %q, %v; want %q", path, got, ok, value)
		}
		custom := kv.paths[path].custom
		if custom[MarkerKey] != "tgt_1" || custom[VersionKey] != "1" || custom[PendingKey] != "" {
			t.Errorf("%s custom metadata = %v, want marker, version 1, no pending", path, custom)
		}
	}
	if len(result.Changes) != 3 {
		t.Fatalf("changes = %+v, want sentinel plus two keys", result.Changes)
	}
	if _, ok := journal.states["variable:MANAGED_BY_HIKYO"]; ok {
		t.Fatal("variable-surface sentinel was claimed although both surfaces share one KV tree")
	}
	for _, key := range []string{"secret:MANAGED_BY_HIKYO", "secret:DATABASE_URL", "variable:LOG_LEVEL"} {
		if journal.states[key] != adapter.Owned {
			t.Errorf("%s = %q, want owned", key, journal.states[key])
		}
	}

	// A second revision updates with cas = recorded version.
	kv.calls = nil
	next := slices.Clone(manifest)
	next[0].Value = "postgres://two"
	if _, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: next, Ledger: journal.ledger()}, journal); err != nil {
		t.Fatal(err)
	}
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "postgres://two" {
		t.Fatalf("update delivered %q", got)
	}
	if custom := kv.paths["apps/pay/DATABASE_URL"].custom; custom[VersionKey] != "2" || custom[PendingKey] != "" {
		t.Fatalf("update custom metadata = %v", custom)
	}
}

func TestSyncRefusesUnownedPathWithoutWriting(t *testing.T) {
	kv := newFakeKV()
	kv.externalWrite("apps/pay/DATABASE_URL", "theirs")
	journal := newFakeJournal()
	target := testTarget(t, kv)
	result, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("Sync() = %v, want exists-unowned", err)
	}
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "theirs" {
		t.Fatalf("unowned value overwritten with %q", got)
	}
	for _, call := range kv.mutations() {
		if strings.Contains(call, "DATABASE_URL") {
			t.Fatalf("unowned path mutated: %v", kv.mutations())
		}
	}
	if journal.refusals != 1 || len(result.Conflicts) != 1 {
		t.Fatalf("refusals=%d conflicts=%+v", journal.refusals, result.Conflicts)
	}
	if _, ok := journal.states["secret:DATABASE_URL"]; ok {
		t.Fatal("refused reservation remained claimed")
	}
}

func TestAdoptedPathIsTakenOverWithCASOnObservedVersion(t *testing.T) {
	kv := newFakeKV()
	kv.externalWrite("apps/pay/DATABASE_URL", "theirs-1")
	kv.externalWrite("apps/pay/DATABASE_URL", "theirs-2")
	journal := newFakeJournal()
	// Adoption inserts an owned ledger row bound to the plan artifact.
	journal.states["secret:MANAGED_BY_HIKYO"] = adapter.Owned
	journal.states["secret:DATABASE_URL"] = adapter.Owned
	kv.paths["apps/pay/MANAGED_BY_HIKYO"] = &fakePath{current: 1, values: map[int64]string{1: adapter.SentinelName}, deleted: map[int64]bool{}, custom: map[string]string{MarkerKey: "tgt_1", VersionKey: "1"}}
	target := testTarget(t, kv)
	if _, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); err != nil {
		t.Fatal(err)
	}
	p := kv.paths["apps/pay/DATABASE_URL"]
	if p.current != 3 || p.values[3] != "postgres://one" || p.custom[MarkerKey] != "tgt_1" || p.custom[VersionKey] != "3" {
		t.Fatalf("adopted path = %+v", p)
	}
}

func TestExternalMovementOnOwnedPathIsConflictNotOverwrite(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	kv.externalWrite("apps/pay/DATABASE_URL", "hand-edited")
	kv.calls = nil
	result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("Sync() = %v, want conflict", err)
	}
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "hand-edited" {
		t.Fatalf("external edit overwritten with %q", got)
	}
	if len(result.Conflicts) != 1 || !slices.Contains(journal.conflicts, "secret:DATABASE_URL") {
		t.Fatalf("conflicts result=%+v journal=%v", result.Conflicts, journal.conflicts)
	}
	if journal.states["secret:DATABASE_URL"] != adapter.Owned {
		t.Fatalf("owned claim changed to %q on conflict", journal.states["secret:DATABASE_URL"])
	}
	// Teardown never deletes a moved path: it releases custody with a
	// recorded conflict and a warning, and still scrubs everything else.
	kv.calls = nil
	journal.conflicts = nil
	teardown, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal)
	if err != nil {
		t.Fatalf("teardown over moved path = %v", err)
	}
	if slices.Contains(kv.calls, "soft-delete:apps/pay/DATABASE_URL") {
		t.Fatal("teardown deleted a path that moved outside Hikyo")
	}
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "hand-edited" {
		t.Fatalf("teardown changed a moved path: %q", got)
	}
	if !slices.Contains(kv.calls, "soft-delete:apps/pay/MANAGED_BY_HIKYO") {
		t.Fatalf("teardown stopped at the moved path: %v", kv.calls)
	}
	if _, held := journal.states["secret:DATABASE_URL"]; held || !slices.Contains(journal.conflicts, "secret:DATABASE_URL") || len(teardown.Warnings) != 1 {
		t.Fatalf("moved path custody=%v conflicts=%v warnings=%v", held, journal.conflicts, teardown.Warnings)
	}
}

func TestCASRaceDuringWriteIsConflict(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	// Another writer lands between the marker patch and the CAS write.
	racer := &racingKV{fakeKV: kv, path: "apps/pay/DATABASE_URL"}
	_, err := (&Module{API: racer}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("Sync() = %v, want CAS conflict", err)
	}
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "racer" {
		t.Fatalf("racing write overwritten with %q", got)
	}
	if pending := kv.paths["apps/pay/DATABASE_URL"].custom[PendingKey]; pending != "" {
		t.Fatalf("lost check-and-set left pending marker %q", pending)
	}
	// The racer holds the version the withdrawn marker named; the next sync
	// must still see external movement, never "our write landed".
	for range 2 {
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrConflict) {
			t.Fatalf("replay after lost race = %v, want conflict", err)
		}
		if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "racer" {
			t.Fatalf("replay overwrote the racing write with %q", got)
		}
	}
}

func TestLostCASWithdrawalFailureIsReported(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	if _, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	withdraw := errors.New("withdraw refused")
	racer := &racingKV{fakeKV: kv, path: "apps/pay/DATABASE_URL", afterRace: func() { kv.failOn["patch-metadata:apps/pay/DATABASE_URL"] = withdraw }}
	_, err := (&Module{API: racer}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if !errors.Is(err, adapter.ErrConflict) || !errors.Is(err, withdraw) {
		t.Fatalf("Sync() = %v, want conflict carrying the failed withdrawal", err)
	}
	// The stale pending marker names the racer's version, so a kept claim
	// would replay it as "our write landed" and overwrite the racer. The
	// claim is released instead; without it the marker cannot authorize a
	// write over a live value.
	if _, held := journal.states["secret:DATABASE_URL"]; held || !slices.Contains(journal.conflicts, "secret:DATABASE_URL") {
		t.Fatalf("claim held=%v conflicts=%v after failed withdrawal", held, journal.conflicts)
	}
	delete(kv.failOn, "patch-metadata:apps/pay/DATABASE_URL")
	for range 2 {
		if _, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrConflict) {
			t.Fatalf("replay after failed withdrawal = %v, want conflict", err)
		}
		if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "racer" {
			t.Fatalf("replay overwrote the racing write with %q", got)
		}
	}
}

// A journal that cannot persist the release keeps the claim. The failure
// must stay loud (the stranded marker is named) without inheriting the
// write's provider class, which could schedule an immediate replay.
func TestLostCASWithdrawalWithFailedFinishStaysLoud(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	if _, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	finish := errors.New("journal down")
	racer := &racingKV{fakeKV: kv, path: "apps/pay/DATABASE_URL", afterRace: func() {
		kv.failOn["patch-metadata:apps/pay/DATABASE_URL"] = errors.New("withdraw refused")
		journal.finishErr = finish
	}}
	_, err := (&Module{API: racer}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if !errors.Is(err, finish) || errors.Is(err, errPendingStranded) || !strings.Contains(err.Error(), errPendingStranded.Error()) {
		t.Fatalf("Sync() = %v, want the journal failure naming the stranded marker", err)
	}
	if journal.states["secret:DATABASE_URL"] != adapter.Owned {
		t.Fatalf("claim = %q, want the unpersisted release to leave it owned", journal.states["secret:DATABASE_URL"])
	}
}

func TestCreateRaceLeavesConcurrentMetadataUntouched(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	journal.states["secret:MANAGED_BY_HIKYO"] = adapter.Owned
	kv.paths["apps/pay/MANAGED_BY_HIKYO"] = &fakePath{current: 1, values: map[int64]string{1: adapter.SentinelName}, deleted: map[int64]bool{}, custom: map[string]string{MarkerKey: "tgt_1", VersionKey: "1"}}
	// Another Hikyo target creates and marks the path after the absent read.
	racer := &racingKV{fakeKV: kv, path: "apps/pay/DATABASE_URL", afterRace: func() {
		kv.paths["apps/pay/DATABASE_URL"].custom = map[string]string{MarkerKey: "tgt_other", VersionKey: "1"}
	}}
	_, err := (&Module{API: racer}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(t, kv), Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("Sync() = %v, want CAS conflict", err)
	}
	p := kv.paths["apps/pay/DATABASE_URL"]
	if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "racer" || p.custom[MarkerKey] != "tgt_other" || p.custom[VersionKey] != "1" || len(p.custom) != 2 {
		t.Fatalf("concurrent creator's path = %q, %v; want untouched", got, p.custom)
	}
	for _, call := range kv.mutations() {
		if call == "patch-metadata:apps/pay/DATABASE_URL" {
			t.Fatalf("lost create touched metadata: %v", kv.mutations())
		}
	}
}

type racingKV struct {
	*fakeKV
	path      string
	afterRace func()
}

func (r *racingKV) WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error) {
	if path == r.path {
		r.externalWrite(path, "racer")
		if r.afterRace != nil {
			r.afterRace()
		}
	}
	return r.fakeKV.WriteCAS(ctx, mount, path, value, cas)
}

func TestAmbiguousWriteReplaysFromMetadataAlone(t *testing.T) {
	for _, landed := range []bool{true, false} {
		t.Run(fmt.Sprintf("landed=%v", landed), func(t *testing.T) {
			kv := newFakeKV()
			journal := newFakeJournal()
			target := testTarget(t, kv)
			module := &Module{API: kv}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
				t.Fatal(err)
			}
			transport := errors.New("connection reset")
			if landed {
				kv.applyOn["write:apps/pay/DATABASE_URL"] = transport
			} else {
				kv.failOn["write:apps/pay/DATABASE_URL"] = transport
			}
			next := slices.Clone(manifest[:1])
			next[0].Value = "postgres://two"
			_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: next, Ledger: journal.ledger()}, journal)
			if !errors.Is(err, adapter.ErrIndeterminate) {
				t.Fatalf("Sync() = %v, want indeterminate", err)
			}
			if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
				t.Fatalf("ambiguous write state = %q, want dispatched", journal.states["secret:DATABASE_URL"])
			}
			if pending := kv.paths["apps/pay/DATABASE_URL"].custom[PendingKey]; pending != "2" {
				t.Fatalf("pending marker = %q, want 2", pending)
			}
			// Replay converges without conflict in both crash windows.
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: next, Ledger: journal.ledger()}, journal); err != nil {
				t.Fatalf("replay = %v", err)
			}
			p := kv.paths["apps/pay/DATABASE_URL"]
			if got, _ := kv.value("apps/pay/DATABASE_URL"); got != "postgres://two" || p.custom[PendingKey] != "" || p.custom[VersionKey] != fmt.Sprint(p.current) {
				t.Fatalf("replayed path = %+v", p)
			}
			if journal.states["secret:DATABASE_URL"] != adapter.Owned || len(journal.conflicts) != 0 {
				t.Fatalf("replay state=%q conflicts=%v", journal.states["secret:DATABASE_URL"], journal.conflicts)
			}
		})
	}
}

func TestAmbiguousCreateReplays(t *testing.T) {
	for _, landed := range []bool{true, false} {
		t.Run(fmt.Sprintf("landed=%v", landed), func(t *testing.T) {
			kv := newFakeKV()
			journal := newFakeJournal()
			target := testTarget(t, kv)
			timeout := errors.New("timeout")
			if landed {
				kv.applyOn["write:apps/pay/DATABASE_URL"] = timeout
			} else {
				kv.failOn["write:apps/pay/DATABASE_URL"] = timeout
			}
			module := &Module{API: kv}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); !errors.Is(err, adapter.ErrIndeterminate) {
				t.Fatalf("Sync() = %v, want indeterminate", err)
			}
			if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
				t.Fatalf("ambiguous create state = %q, want dispatched", journal.states["secret:DATABASE_URL"])
			}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); err != nil {
				t.Fatalf("replay = %v", err)
			}
			p := kv.paths["apps/pay/DATABASE_URL"]
			if got, ok := kv.value("apps/pay/DATABASE_URL"); !ok || got != "postgres://one" || p.custom[MarkerKey] != "tgt_1" || p.custom[VersionKey] != fmt.Sprint(p.current) || p.custom[PendingKey] != "" {
				t.Fatalf("replayed create = %q, %v, %+v", got, ok, p)
			}
			if journal.states["secret:DATABASE_URL"] != adapter.Owned || len(journal.conflicts) != 0 {
				t.Fatalf("replay state=%q conflicts=%v", journal.states["secret:DATABASE_URL"], journal.conflicts)
			}
		})
	}
}

func TestFailedMarkAfterCreateReplays(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	kv.failOn["patch-metadata:apps/pay/DATABASE_URL"] = errors.New("timeout")
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("Sync() = %v, want indeterminate", err)
	}
	if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
		t.Fatal("unmarked create must remain dispatched")
	}
	if custom := kv.paths["apps/pay/DATABASE_URL"].custom; custom[MarkerKey] != "" {
		t.Fatalf("custom metadata = %v, want the unmarked create", custom)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); err != nil {
		t.Fatalf("replay = %v", err)
	}
	p := kv.paths["apps/pay/DATABASE_URL"]
	if p.current != 2 || p.custom[MarkerKey] != "tgt_1" || p.custom[VersionKey] != "2" || p.custom[PendingKey] != "" {
		t.Fatalf("replayed path = %+v", p)
	}
}

func TestDefinitiveCreateFailureReleasesReservation(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	journal.states["secret:MANAGED_BY_HIKYO"] = adapter.Owned
	kv.paths["apps/pay/MANAGED_BY_HIKYO"] = &fakePath{current: 1, values: map[int64]string{1: adapter.SentinelName}, deleted: map[int64]bool{}, custom: map[string]string{MarkerKey: "tgt_1", VersionKey: "1"}}
	kv.failOn["write:apps/pay/DATABASE_URL"] = &ResponseError{Status: 400}
	_, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(t, kv), Manifest: manifest[:1], Ledger: journal.ledger()}, journal)
	if err == nil || errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("Sync() = %v, want definitive failure", err)
	}
	if _, ok := journal.states["secret:DATABASE_URL"]; ok {
		t.Fatal("definitive create failure kept the reservation")
	}
}

func TestPruneSoftDeletesAndReAddReclaimsOwnPath(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatal(err)
	}
	kv.calls = nil
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(kv.calls, "soft-delete:apps/pay/LOG_LEVEL") {
		t.Fatalf("prune calls = %v", kv.calls)
	}
	p := kv.paths["apps/pay/LOG_LEVEL"]
	if !p.deleted[1] || p.values[1] != "debug" {
		t.Fatalf("prune was not a recoverable soft delete: %+v", p)
	}
	if journal.states["variable:LOG_LEVEL"] != adapter.Released {
		t.Fatalf("pruned state = %q", journal.states["variable:LOG_LEVEL"])
	}
	// Re-adding the key reclaims this target's own soft-deleted path.
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest, Ledger: journal.ledger()}, journal); err != nil {
		t.Fatalf("re-add = %v", err)
	}
	if got, ok := kv.value("apps/pay/LOG_LEVEL"); !ok || got != "debug" || kv.paths["apps/pay/LOG_LEVEL"].current != 2 {
		t.Fatalf("re-add = %q, %v", got, ok)
	}
}

func TestReclassificationRetiresOppositeSurfaceWithoutPruningDesiredPath(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	config := []adapter.ManifestEntry{{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug"}}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: config}, journal); err != nil {
		t.Fatal(err)
	}
	// Adoption of the reclassified row creates the new surface claim while the
	// asynchronous old claim still exists.
	journal.states["secret:LOG_LEVEL"] = adapter.Owned
	secret := []adapter.ManifestEntry{{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.SecretClassification, Value: "sensitive"}}
	kv.calls = nil
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: secret, Ledger: journal.ledger()}, journal); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(kv.calls, "soft-delete:apps/pay/LOG_LEVEL") {
		t.Fatalf("reclassification pruned its desired path: %v", kv.calls)
	}
	if got, ok := kv.value("apps/pay/LOG_LEVEL"); !ok || got != "sensitive" {
		t.Fatalf("reclassified path = %q, %t, want sensitive", got, ok)
	}
	if journal.states["variable:LOG_LEVEL"] != adapter.Released {
		t.Fatalf("opposite-surface ownership claim = %q, want released", journal.states["variable:LOG_LEVEL"])
	}
}

func TestTeardownPrunesSentinelLastAndNeverDestroys(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatal(err)
	}
	kv.calls = nil
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
		t.Fatal(err)
	}
	want := []string{"soft-delete:apps/pay/DATABASE_URL", "soft-delete:apps/pay/LOG_LEVEL", "soft-delete:apps/pay/MANAGED_BY_HIKYO"}
	if got := kv.mutations(); !slices.Equal(got, want) {
		t.Fatalf("teardown mutations = %v, want sentinel-last %v", got, want)
	}
	for path, p := range kv.paths {
		if len(p.values) == 0 || p.custom[MarkerKey] != "tgt_1" {
			t.Fatalf("teardown destroyed versions or metadata of %s: %+v", path, p)
		}
	}
}

func TestMountMovementRefusesEveryWrite(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	kv.mount.UUID = "remounted-uuid"
	_, err := (&Module{API: kv}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal)
	if !errors.Is(err, adapter.ErrDestinationID) {
		t.Fatalf("Sync() = %v, want destination identity refusal", err)
	}
	if len(kv.mutations()) != 0 {
		t.Fatalf("mutations after mount movement: %v", kv.mutations())
	}
	kv.mount = Mount{Type: "kv", Version: "1", UUID: "mount-uuid-1"}
	if _, err := (&Module{API: kv}).Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: manifest, Gate: func(context.Context) error { return nil }}); err == nil || !strings.Contains(err.Error(), "KV version 2") {
		t.Fatalf("Plan() on KV v1 = %v", err)
	}
}

func TestPlanIsValueBlindAndClassifiesOwnership(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); err != nil {
		t.Fatal(err)
	}
	kv.externalWrite("apps/pay/LOG_LEVEL", "theirs")
	blind := []adapter.ManifestEntry{
		{KeyID: "key_db", CanonicalName: "DATABASE_URL", Classification: adapter.SecretClassification},
		{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification},
		{KeyID: "key_new", CanonicalName: "NEW", Classification: adapter.SecretClassification},
	}
	kv.calls = nil
	plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: blind, Ledger: journal.ledger(), Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]adapter.Disposition{}
	for _, change := range plan.Changes {
		got[change.EffectiveName] = change.Disposition
	}
	want := map[string]adapter.Disposition{"MANAGED_BY_HIKYO": adapter.Update, "DATABASE_URL": adapter.Update, "LOG_LEVEL": adapter.Conflict, "NEW": adapter.Create}
	for name, disposition := range want {
		if got[name] != disposition {
			t.Errorf("%s = %q, want %q", name, got[name], disposition)
		}
	}
	if len(kv.mutations()) != 0 {
		t.Fatalf("Plan mutated the destination: %v", kv.mutations())
	}
}

func TestSealedServerFailsConnectionTest(t *testing.T) {
	kv := newFakeKV()
	kv.sealed = true
	_, err := (&Module{API: kv}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: testTarget(t, kv).Destination, Gate: func(context.Context) error { return nil }})
	var response *ResponseError
	if !errors.As(err, &response) || !response.Sealed {
		t.Fatalf("TestConnection() = %v, want sealed", err)
	}
}

func TestConnectionResolvesDestinationIdentity(t *testing.T) {
	kv := newFakeKV()
	destination := adapter.Destination{Kind: adapter.Repository, Owner: "secret", Name: "apps/pay"}
	connection, err := (&Module{API: kv}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := DestinationID(kv.mount, "apps/pay")
	if connection.DestinationID != want || connection.DestinationID <= 0 {
		t.Fatalf("destination id = %d, want %d", connection.DestinationID, want)
	}
	other, _ := DestinationID(kv.mount, "apps/other")
	if other == want {
		t.Fatal("distinct path prefixes on one mount share a destination id")
	}
}

func TestValidateDestinationRefusesUnsafeAddressing(t *testing.T) {
	for _, destination := range []adapter.Destination{
		{Kind: adapter.Organization, Owner: "secret"},
		{Kind: adapter.Repository, Owner: "secret", Name: ""},
		{Kind: adapter.Repository, Owner: "secret", Name: "../x"},
		{Kind: adapter.Repository, Owner: "sys", Name: "x"},
		{Kind: adapter.Repository, Owner: "secret/", Name: "x"},
		{Kind: adapter.Repository, Owner: "secret", Name: "x", Environment: "prod"},
	} {
		if err := ValidateDestination(destination); err == nil {
			t.Errorf("ValidateDestination(%+v) accepted", destination)
		}
	}
	if err := ValidateDestination(adapter.Destination{Kind: adapter.Repository, Owner: "kv/team-a", Name: "apps/pay-svc/prod"}); err != nil {
		t.Fatalf("nested mount refused: %v", err)
	}
}

type lookupRefusedKV struct{ *fakeKV }

func (lookupRefusedKV) LookupSelf(context.Context) (TokenInfo, error) {
	return TokenInfo{}, errors.Join(adapter.ErrProviderAuth, &ResponseError{Status: 403})
}

func TestConnectionToleratesPolicyWithoutLookupSelf(t *testing.T) {
	kv := newFakeKV()
	connection, err := (&Module{API: lookupRefusedKV{kv}}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.Repository, Owner: "secret", Name: "apps"}, Gate: func(context.Context) error { return nil }})
	if err != nil || connection.DestinationID <= 0 || !connection.CredentialExpiresAt.IsZero() {
		t.Fatalf("TestConnection() = %+v, %v; want success with unknown expiry", connection, err)
	}
}

func TestPrunePreservesConcurrentExternalVersion(t *testing.T) {
	kv := newFakeKV()
	journal := newFakeJournal()
	target := testTarget(t, kv)
	module := &Module{API: kv}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatal(err)
	}
	kv.beforeDelete = func(path string) { kv.externalWrite(path, "concurrent-external-value") }
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}, journal); err != nil {
		t.Fatal(err)
	}
	p := kv.paths["apps/pay/LOG_LEVEL"]
	if !p.deleted[1] || p.deleted[2] || p.values[2] != "concurrent-external-value" {
		t.Fatalf("prune affected concurrent external version: %+v", p)
	}
}

func TestFailedCreateFinalizeNeverOverwritesOrPrunesLaterExternalEdit(t *testing.T) {
	for _, teardown := range []bool{false, true} {
		t.Run(fmt.Sprint(teardown), func(t *testing.T) {
			kv := newFakeKV()
			journal := newFakeJournal()
			target := testTarget(t, kv)
			module := &Module{API: kv}
			kv.failOn["patch-metadata:apps/pay/DATABASE_URL"] = &ResponseError{Status: 403}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1]}, journal); !errors.Is(err, adapter.ErrIndeterminate) {
				t.Fatalf("create finalize: %v", err)
			}
			if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
				t.Fatal("create finalize refusal must retain dispatched claim")
			}
			kv.externalWrite("apps/pay/DATABASE_URL", "external")
			req := adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger()}
			if teardown {
				req.Manifest = nil
				req.Teardown = true
			}
			result, err := module.Sync(t.Context(), req, journal)
			if !teardown && !errors.Is(err, adapter.ErrConflict) {
				t.Fatalf("replay = %v", err)
			}
			if teardown && (err != nil || len(result.Warnings) == 0) {
				t.Fatalf("teardown = %+v, %v", result, err)
			}
			if value, ok := kv.value("apps/pay/DATABASE_URL"); !ok || value != "external" {
				t.Fatalf("external version altered: %q %v", value, ok)
			}
		})
	}
}
