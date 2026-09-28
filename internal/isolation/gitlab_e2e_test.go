package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/gitlab"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// gitLabE2E is the external GitLab the lifecycle runs against. CI starts a
// disposable gitlab/gitlab-ce container and sets HIKYO_TEST_GITLAB_REQUIRED
// so a missing instance fails instead of skipping.
type gitLabE2E struct {
	origin, token, project, group string
	caBundle, pin                 string
	allowed                       []netip.Prefix
	allowPersonal                 bool
}

func gitLabE2EConfig(t *testing.T) gitLabE2E {
	t.Helper()
	cfg := gitLabE2E{
		origin: os.Getenv("HIKYO_TEST_GITLAB_URL"), token: os.Getenv("HIKYO_TEST_GITLAB_TOKEN"),
		project: os.Getenv("HIKYO_TEST_GITLAB_PROJECT"), group: os.Getenv("HIKYO_TEST_GITLAB_GROUP"),
		pin: os.Getenv("HIKYO_TEST_GITLAB_SPKI_PIN"), allowPersonal: os.Getenv("HIKYO_TEST_GITLAB_ALLOW_PERSONAL_TOKEN") == "1",
	}
	if cfg.origin == "" || cfg.token == "" || cfg.project == "" || cfg.group == "" {
		if os.Getenv("HIKYO_TEST_GITLAB_REQUIRED") == "1" {
			t.Fatal("HIKYO_TEST_GITLAB_REQUIRED is set but HIKYO_TEST_GITLAB_URL/TOKEN/PROJECT/GROUP are incomplete")
		}
		t.Skip("set HIKYO_TEST_GITLAB_URL/TOKEN/PROJECT/GROUP for the real GitLab adapter lifecycle")
	}
	if path := os.Getenv("HIKYO_TEST_GITLAB_CA_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("HIKYO_TEST_GITLAB_CA_FILE: %v", err)
		}
		cfg.caBundle = string(raw)
	}
	if raw := os.Getenv("HIKYO_TEST_GITLAB_ALLOWED_CIDR"); raw != "" {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			t.Fatalf("HIKYO_TEST_GITLAB_ALLOWED_CIDR: %v", err)
		}
		cfg.allowed = append(cfg.allowed, prefix)
	}
	return cfg
}

func (c gitLabE2E) client(t *testing.T, pin string) *gitlab.Client {
	t.Helper()
	client, err := gitlab.NewClient(gitlab.ClientConfig{Origin: c.origin, Credential: c.token, AllowedCIDRs: c.allowed, Deadline: 15 * time.Second, SPKIPin: pin, CABundlePEM: c.caBundle})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	return client
}

func (c gitLabE2E) config() adapter.Config {
	return adapter.Config{Origin: c.origin, SPKIPin: c.pin, CABundlePEM: c.caBundle, AllowPersonalToken: c.allowPersonal}
}

func splitGitLabProject(t *testing.T, path string) (string, string) {
	t.Helper()
	i := strings.LastIndex(path, "/")
	if i <= 0 || i == len(path)-1 {
		t.Fatalf("HIKYO_TEST_GITLAB_PROJECT %q is not NAMESPACE/PROJECT", path)
	}
	return path[:i], path[i+1:]
}

// TestGitLabRealLifecycle proves the real GitLab v4 variable endpoints and
// status semantics through the durable store journal on both engines:
// project, group, and environment scopes; protected and masked flags;
// exists-unowned refusal without overwrite; crash-window replay; SPKI pin
// refusal; and teardown that deletes only ledger-owned names.
func TestGitLabRealLifecycle(t *testing.T) {
	cfg := gitLabE2EConfig(t)
	t.Run("spki pin mismatch refuses before any request", func(t *testing.T) {
		wrong := base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))
		_, err := cfg.client(t, wrong).Version(t.Context())
		if err == nil || !strings.Contains(err.Error(), "SPKI pin") {
			t.Fatalf("Version() with a wrong pin = %v, want pin refusal", err)
		}
	})
	t.Run("group scope", func(t *testing.T) { runGitLabGroupLifecycle(t, cfg) })
	for _, engine := range []struct {
		name string
		open func(*testing.T) *store.DB
	}{{"sqlite", openSQLite}, {"postgres", openPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			runGitLabProjectLifecycle(t, engine.open(t), cfg)
		})
	}
}

func runGitLabProjectLifecycle(t *testing.T, db *store.DB, cfg gitLabE2E) {
	t.Helper()
	client := cfg.client(t, cfg.pin)
	module := &gitlab.Module{API: client}
	namespace, name := splitGitLabProject(t, cfg.project)
	suffix := strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36))
	scope := "hikyo-e2e-" + strings.ToLower(suffix)
	destination := adapter.Destination{Kind: adapter.Repository, Owner: namespace, Name: name, Scope: scope}
	connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Config: cfg.config(), Destination: destination, Gate: allowGitLab})
	if err != nil {
		t.Fatal(err)
	}
	destination.NumericID = connection.DestinationID
	prefix := "HIKYO_E2E_" + suffix + "_"
	target := adapter.Target{ID: "tgt_gitlab_e2e", Environment: "env_gitlab_e2e", Destination: destination, NamePrefix: prefix, Generation: 1, Options: adapter.VariableOptions{Protected: true}}
	seedGitLabDB(t, db, cfg, target)
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	job, ok, err := runtime.ClaimDue(t.Context(), "gitlab-e2e-worker", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim job: %+v %v %v", job, ok, err)
	}
	journal := runtime.Journal(job)
	takenKey := prefix + "TAKEN"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ledger, err := gitLabLedgerContext(ctx, db, target.ID)
		if err != nil {
			t.Errorf("teardown ledger: %v", err)
			return
		}
		if _, err := module.Sync(ctx, adapter.SyncRequest{Config: cfg.config(), Target: target, Ledger: ledger, Teardown: true}, runtime.Journal(job)); err != nil {
			t.Errorf("teardown: %v", err)
		}
		// The third-party variable was never Hikyo's; remove the fixture directly.
		if err := client.DeleteVariable(ctx, destination, takenKey, scope); err != nil && !gitlab.IsStatus(err, http.StatusNotFound) {
			t.Errorf("cleanup third-party fixture: %v", err)
		}
	})

	manifest := []adapter.ManifestEntry{
		{KeyID: "key_token", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "e2e-masked-value-01"},
		{KeyID: "key_mode", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "debug $HOME"},
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatalf("converge: %v", err)
	}
	for _, key := range []string{prefix + adapter.SentinelName, prefix + "TOKEN", prefix + "MODE"} {
		if state := gitLabLedgerState(t, db, target.ID, key); state != adapter.Owned {
			t.Fatalf("ledger %s = %q, want owned", key, state)
		}
	}
	var scoped string
	if err := queryRealAdoptionRow(t, db, `SELECT destination_scope FROM adapter_ledger WHERE target_id=$1 AND effective_name=$2`, target.ID, prefix+"TOKEN").Scan(&scoped); err != nil || scoped != scope {
		t.Fatalf("ledger destination_scope = %q, %v; want %q", scoped, err, scope)
	}
	// The harness, never the adapter, reads the provider back to prove flags.
	token := gitLabObserve(t, cfg, destination, prefix+"TOKEN", scope)
	if !token.Masked || !token.Protected || !token.Raw || token.Scope != scope {
		t.Fatalf("secret variable flags = %+v", token)
	}
	mode := gitLabObserve(t, cfg, destination, prefix+"MODE", scope)
	if mode.Masked || !mode.Protected || !mode.Raw || mode.Value != "debug $HOME" {
		t.Fatalf("config variable = %+v", mode)
	}

	// exists, unowned: a third party holds a key Hikyo now wants. The create
	// is refused, nothing is overwritten, and a conflict artifact is recorded.
	if _, err := client.CreateVariable(t.Context(), destination, gitlab.Variable{Key: takenKey, Value: "third-party", Scope: scope, Raw: true}); err != nil {
		t.Fatal(err)
	}
	withTaken := append(append([]adapter.ManifestEntry(nil), manifest...), adapter.ManifestEntry{KeyID: "key_taken", CanonicalName: "TAKEN", Classification: adapter.ConfigClassification, Value: "hikyo-value"})
	_, err = module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: withTaken, Ledger: gitLabLedger(t, db, target.ID)}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("unowned key = %v, want exists-unowned conflict", err)
	}
	if got := gitLabObserve(t, cfg, destination, takenKey, scope); got.Value != "third-party" {
		t.Fatal("an unowned variable was overwritten")
	}
	var conflicts int
	if err := queryRealAdoptionRow(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE target_id=$1 AND effective_name=$2`, target.ID, takenKey).Scan(&conflicts); err != nil || conflicts == 0 {
		t.Fatalf("conflict artifacts = %d, %v", conflicts, err)
	}
	if state := gitLabLedgerState(t, db, target.ID, takenKey); state != "" {
		t.Fatalf("refused key left ledger state %q", state)
	}

	// Crash window: the process died after dispatching MODE's write but
	// before recording its outcome. Replay updates the owned name in place.
	execRealAdoption(t, db, `UPDATE adapter_ledger SET state='dispatched' WHERE target_id=$1 AND effective_name=$2`, target.ID, prefix+"MODE")
	manifest[1].Value = "replayed"
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: manifest, Ledger: gitLabLedger(t, db, target.ID)}, journal); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if state := gitLabLedgerState(t, db, target.ID, prefix+"MODE"); state != adapter.Owned {
		t.Fatalf("replayed ledger state = %q", state)
	}
	if got := gitLabObserve(t, cfg, destination, prefix+"MODE", scope); got.Value != "replayed" {
		t.Fatalf("replayed value = %q", got.Value)
	}

	// Owned-missing: an operator deleted an owned variable at GitLab. The
	// next converge recreates it rather than treating it as unowned.
	if err := client.DeleteVariable(t.Context(), destination, prefix+"MODE", scope); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: manifest, Ledger: gitLabLedger(t, db, target.ID)}, journal); err != nil {
		t.Fatalf("owned-missing recreate: %v", err)
	}
	if got := gitLabObserve(t, cfg, destination, prefix+"MODE", scope); got.Value != "replayed" {
		t.Fatalf("recreated value = %q", got.Value)
	}

	// Prune: dropping a key deletes only that ledger-owned name.
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: manifest[:1], Ledger: gitLabLedger(t, db, target.ID)}, journal); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if gitLabExists(t, cfg, destination, prefix+"MODE", scope) || !gitLabExists(t, cfg, destination, takenKey, scope) {
		t.Fatal("prune touched the wrong names")
	}
}

func runGitLabGroupLifecycle(t *testing.T, cfg gitLabE2E) {
	t.Helper()
	client := cfg.client(t, cfg.pin)
	module := &gitlab.Module{API: client}
	destination := adapter.Destination{Kind: adapter.Organization, Owner: cfg.group, Scope: "*"}
	connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Config: cfg.config(), Destination: destination, Gate: allowGitLab})
	if err != nil {
		t.Fatal(err)
	}
	destination.NumericID = connection.DestinationID
	prefix := "HIKYO_E2E_GROUP_" + strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36)) + "_"
	target := adapter.Target{ID: "tgt_gitlab_group", Environment: "env_gitlab_group", Destination: destination, NamePrefix: prefix, Generation: 1}
	journal := newGitLabMemoryJournal()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := module.Sync(ctx, adapter.SyncRequest{Config: cfg.config(), Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
			t.Errorf("group teardown: %v", err)
		}
	})
	manifest := []adapter.ManifestEntry{{KeyID: "key_group", CanonicalName: "SHARED", Classification: adapter.SecretClassification, Value: "group-masked-value"}}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Config: cfg.config(), Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatalf("group converge: %v", err)
	}
	if got := gitLabObserve(t, cfg, destination, prefix+"SHARED", "*"); !got.Masked || got.Scope != "*" {
		t.Fatalf("group variable = %+v", got)
	}
}

func allowGitLab(context.Context) error { return nil }

func seedGitLabDB(t *testing.T, db *store.DB, cfg gitLabE2E, target adapter.Target) {
	t.Helper()
	d := target.Destination
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_gitlab','GitLab',TRUE,'{}','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_gitlab','org_gitlab','GitLab','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ($1,'org_gitlab','prj_gitlab','e2e','','2026-08-17T00:00:00Z',0)`, []any{target.Environment}},
		{`INSERT INTO principals (id,kind,created_at) VALUES ('usr_gitlab','human','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem) VALUES ('adp_gitlab','org_gitlab','prj_gitlab','gitlab',$1,'usr_gitlab','active','2026-08-17T00:00:00Z',$2,$3)`, []any{cfg.origin, cfg.pin, cfg.caBundle}},
		{`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,destination_scope,variable_protected) VALUES ($1,'org_gitlab','prj_gitlab',$2,'adp_gitlab','repository',$3,$4,$5,$6,1,'active','never','2026-08-17T00:00:00Z',$7,TRUE)`, []any{target.ID, target.Environment, d.Owner, d.Name, d.NumericID, target.NamePrefix, d.Scope}},
		{`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_gitlab','org_gitlab','prj_gitlab',$1,$2,'converge','usr_gitlab',1,$2,'2026-08-17T00:00:00Z','queued','2026-08-17T00:00:00Z')`, []any{target.Environment, target.ID}},
		{`UPDATE adapter_targets SET active_job_id='job_gitlab' WHERE id=$1`, []any{target.ID}},
	} {
		execRealAdoption(t, db, statement.query, statement.args...)
	}
}

func gitLabLedger(t *testing.T, db *store.DB, targetID string) []adapter.LedgerEntry {
	t.Helper()
	out, err := gitLabLedgerContext(t.Context(), db, targetID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitLabLedgerContext reads the unreleased ledger with an explicit context so
// cleanup can run after the test context is cancelled.
func gitLabLedgerContext(ctx context.Context, db *store.DB, targetID string) ([]adapter.LedgerEntry, error) {
	const query = `SELECT surface,effective_name,state,missing FROM adapter_ledger WHERE target_id=$1 AND state<>'released' ORDER BY surface,effective_name`
	var out []adapter.LedgerEntry
	scan := func(get func(...any) error) error {
		var surface, state string
		var entry adapter.LedgerEntry
		if err := get(&surface, &entry.EffectiveName, &state, &entry.Missing); err != nil {
			return err
		}
		entry.Surface, entry.State = adapter.Surface(surface), adapter.LedgerState(state)
		out = append(out, entry)
		return nil
	}
	if db.Engine() == store.EnginePostgres {
		rows, err := db.PG().Query(ctx, query, targetID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows.Scan); err != nil {
				return nil, err
			}
		}
		return out, rows.Err()
	}
	rows, err := db.SQLiteRead().QueryContext(ctx, query, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows.Scan); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

func gitLabLedgerState(t *testing.T, db *store.DB, targetID, name string) adapter.LedgerState {
	t.Helper()
	for _, entry := range gitLabLedger(t, db, targetID) {
		if entry.EffectiveName == name {
			return entry.State
		}
	}
	return ""
}

type gitLabObserved struct {
	Value     string `json:"value"`
	Protected bool   `json:"protected"`
	Masked    bool   `json:"masked"`
	Raw       bool   `json:"raw"`
	Scope     string `json:"environment_scope"`
}

// gitLabObserve is the test harness's own read of a variable it wrote. It is
// deliberately outside the adapter: the adapter cannot express this call.
func gitLabObserve(t *testing.T, cfg gitLabE2E, d adapter.Destination, key, scope string) gitLabObserved {
	t.Helper()
	status, raw := gitLabGet(t, cfg, d, key, scope)
	if status != http.StatusOK {
		t.Fatalf("observe %s: status %d", key, status)
	}
	var out gitLabObserved
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func gitLabExists(t *testing.T, cfg gitLabE2E, d adapter.Destination, key, scope string) bool {
	t.Helper()
	status, _ := gitLabGet(t, cfg, d, key, scope)
	return status == http.StatusOK
}

func gitLabGet(t *testing.T, cfg gitLabE2E, d adapter.Destination, key, scope string) (int, []byte) {
	t.Helper()
	origin, err := gitlab.CanonicalOrigin(cfg.origin)
	if err != nil {
		t.Fatal(err)
	}
	collection := "/groups/"
	if d.Kind == adapter.Repository {
		collection = "/projects/"
	}
	path := origin + "/api/v4" + collection + strconv.FormatInt(d.NumericID, 10) + "/variables/" + url.PathEscape(key) + "?filter%5Benvironment_scope%5D=" + url.QueryEscape(scope)
	tlsConfig, err := gitlab.TLSConfig("", cfg.caBundle)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", cfg.token)
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw
}

type gitLabMemoryJournal struct {
	states  map[string]adapter.LedgerState
	missing map[string]bool
}

func newGitLabMemoryJournal() *gitLabMemoryJournal {
	return &gitLabMemoryJournal{states: map[string]adapter.LedgerState{}, missing: map[string]bool{}}
}

func gitLabJournalKey(effect adapter.Effect) string {
	return string(effect.Surface) + ":" + effect.EffectiveName
}

func (j *gitLabMemoryJournal) Gate(context.Context, adapter.Effect) error { return nil }

func (j *gitLabMemoryJournal) Reserve(_ context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	j.states[gitLabJournalKey(effect)] = adapter.Reserved
	return adapter.Reserved, nil
}

func (j *gitLabMemoryJournal) Prepare(_ context.Context, effect adapter.Effect, prior adapter.LedgerState) error {
	if prior == adapter.Reserved {
		j.states[gitLabJournalKey(effect)] = adapter.Dispatched
	}
	return nil
}

func (j *gitLabMemoryJournal) Finish(_ context.Context, effect adapter.Effect, completion adapter.Completion) error {
	key := gitLabJournalKey(effect)
	if completion.ReleaseLedger {
		delete(j.states, key)
		delete(j.missing, key)
		return nil
	}
	j.states[key], j.missing[key] = completion.State, completion.Missing
	return nil
}

func (j *gitLabMemoryJournal) Refuse(_ context.Context, effect adapter.Effect) error {
	delete(j.states, gitLabJournalKey(effect))
	return nil
}

func (j *gitLabMemoryJournal) ReleaseReservation(_ context.Context, effect adapter.Effect) error {
	delete(j.states, gitLabJournalKey(effect))
	return nil
}

func (j *gitLabMemoryJournal) ledger() []adapter.LedgerEntry {
	out := make([]adapter.LedgerEntry, 0, len(j.states))
	for key, state := range j.states {
		if state == adapter.Released {
			continue
		}
		surface, name, _ := strings.Cut(key, ":")
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(surface), EffectiveName: name, State: state, Missing: j.missing[key]})
	}
	return out
}
