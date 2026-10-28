package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

const awsReplayChildEnv = "HIKYO_TEST_AWS_REPLAY_CHILD"
const awsReplayCrashExit = 23

// Only the anonymous stdin pipe carries the root and database DSN. The child
// obtains its writer descriptor from sealed SQL via the normal loader. It has
// neither oracle/cleanup credentials nor any permission to reserve creates.
type awsReplayInput struct {
	Store           store.Config
	Material        devupgrade.Material
	Root            []byte
	Manifest        realAWSManifest
	Owned           map[string]string
	Current         map[string]string
	Creations       []string
	TargetID, JobID  string
	CrashARN        string
	Clock           time.Time
}

type awsReplayLoader struct {
	awsRealLoader
	targetID, jobID string
}

func (l awsReplayLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if job.TargetID != l.targetID || job.ID != l.jobID || job.Kind != adapter.Converge {
		return adapter.LoadedSync{}, errors.New("replay child refused an unrelated job")
	}
	return l.awsRealLoader.Load(ctx, job, journal)
}

func (h *realAWSDurable) processReplay(targetID, name, jobID string) {
	t := h.t
	root, err := (probeRootSource{db: h.db}).Current(t.Context())
	if err != nil {
		t.Fatal("cannot retain original fixture root for process replay")
	}
	defer crypto.Zero(root)
	value, ok := isolationCustody.Load(h.db)
	material, materialOK := value.(devupgrade.Material)
	if !ok || !materialOK {
		t.Fatal("process replay requires the original signed admission material")
	}
	cfg := store.Config{Engine: h.db.Engine()}
	if cfg.Engine == store.EngineSQLite {
		rows, err := h.db.SQLiteRead().QueryContext(t.Context(), "PRAGMA database_list")
		if err != nil {
			t.Fatal("cannot locate owned SQLite fixture")
		}
		for rows.Next() {
			var sequence int
			var database, path string
			if err := rows.Scan(&sequence, &database, &path); err != nil {
				rows.Close()
				t.Fatal("cannot identify owned SQLite fixture")
			}
			if database == "main" {
				cfg.Path = path
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil || cfg.Path == "" {
			t.Fatal("owned SQLite fixture path is missing")
		}
	} else {
		cfg.DSN = h.db.PG().Config().ConnString()
	}
	h.s.mu.Lock()
	owned, current := make(map[string]string), make(map[string]string)
	for arn, n := range h.s.owned {
		owned[arn] = n
	}
	for n, arn := range h.s.current {
		current[n] = arn
	}
	creations := append([]string(nil), h.s.creations...)
	h.s.mu.Unlock()
	input := awsReplayInput{Store: cfg, Material: material, Root: root, Manifest: h.s.manifest, Owned: owned, Current: current, Creations: creations, TargetID: targetID, JobID: jobID, Clock: h.clock.Add(time.Second), CrashARN: h.s.arn(t, name)}
	// The original worker stops here. Two distinct OS processes perform the
	// uncertain write and recovery; neither inherits its pointers or keyring.
	ledgerQuery := fmt.Sprintf(`SELECT id FROM adapter_ledger WHERE target_id='%s' AND surface='secret' AND effective_name='%s' AND state='owned'`, targetID, name)
	ledgerID := queryString(t, h.db, ledgerQuery)
	if ledgerID == "" {
		t.Fatal("process replay requires existing exact owned custody")
	}
	h.worker = nil
	h.runReplayChild(input, awsReplayCrashExit)
	if queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE id='%s' AND target_id='%s' AND surface='secret' AND effective_name='%s' AND state='owned'`, ledgerID, targetID, name)) != 1 || queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE job_id='%s' AND target_id='%s' AND surface='secret' AND effective_name='%s' AND outcome IS NULL`, jobID, targetID, name)) != 1 {
		t.Fatal("hard process exit did not retain exact owned custody and unfinished intent")
	}
	input.CrashARN = ""
	input.Clock = input.Clock.Add(adapter.LeaseTime + adapter.RetryCap + time.Second)
	h.runReplayChild(input, 0)
	if queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE job_id='%s' AND outcome='unknown'`, jobID)) != 1 || queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE job_id='%s' AND outcome='success'`, jobID)) != 1 {
		t.Fatal("fresh process failed to close the crash window and finish the same job")
	}
	h.clock = input.Clock.Add(time.Minute)
	h.restartWorker()
}

func (h *realAWSDurable) runReplayChild(input awsReplayInput, expectedExit int) {
	t := h.t
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 128<<10 {
		t.Fatal("cannot encode bounded replay input")
	}
	defer crypto.Zero(raw)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("cannot locate the running test executable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestAWSSecretsManagerReplayProcess$", "-test.count=1", "-test.timeout=80s")
	// Avoid inheriting AWS sources, provider endpoints or unrelated test DSNs.
	// Only a nonsecret child-mode marker and ordinary OS runtime inputs cross.
	for _, key := range []string{"PATH", "TMPDIR", "SYSTEMROOT"} {
		if value, present := os.LookupEnv(key); present {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, awsReplayChildEnv+"=approved")
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.WaitDelay = 3 * time.Second
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			t.Fatal("replay process did not start or terminate normally")
		}
		exitCode = exited.ExitCode()
	}
	if ctx.Err() != nil || exitCode != expectedExit {
		t.Fatalf("replay process exit=%d; expected=%d", exitCode, expectedExit)
	}
}

func TestAWSSecretsManagerReplayProcess(t *testing.T) {
	if os.Getenv(awsReplayChildEnv) != "approved" {
		t.Skip("only the approved real suite launches this child process")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		t.Fatal("invalid bounded replay input")
	}
	defer crypto.Zero(raw)
	var input awsReplayInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		t.Fatal("invalid replay input shape")
	}
	if err := decoder.Decode(new(any)); err != io.EOF || len(input.Root) != crypto.KeySize || input.TargetID == "" || input.JobID == "" || input.Manifest.validate(time.Now()) != nil {
		t.Fatal("invalid replay authorization or custody")
	}
	defer crypto.Zero(input.Root)
	if input.Store.Engine != store.EngineSQLite && input.Store.Engine != store.EnginePostgres {
		t.Fatal("invalid replay datastore engine")
	}
	s, err := importAWSReplayCustody(input)
	if err != nil {
		t.Fatal("invalid original replay custody reservations")
	}
	s.t = t
	if input.CrashARN != "" && (!strings.HasPrefix(s.owned[input.CrashARN], input.Manifest.Prefix+"static/") || input.Current[s.owned[input.CrashARN]] != input.CrashARN) {
		t.Fatal("invalid replay crash resource identity")
	}
	admission, err := upgradegate.RunDevelopment(t.Context(), upgradegate.Request{
		Store: upgrade.Config{Engine: releaseidentity.Engine(input.Store.Engine), Path: input.Store.Path, DSN: input.Store.DSN},
		BundleDirectory: input.Material.Directory, Pinned: input.Material.Pinned,
		Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(input.Store.Engine),
		Mode: upgradegate.Boot, AllowMigrations: false, RootKey: input.Root,
	})
	if err != nil {
		t.Fatal("replay failed existing signed-custody admission")
	}
	db, err := store.Open(t.Context(), input.Store, admission.Admission)
	if err != nil {
		t.Fatal("replay could not reopen the existing datastore")
	}
	t.Cleanup(func() { db.Close() })
	h := &realAWSDurable{t: t, db: db, s: s, keyring: loadAndRegisterKeyring(t, db, bytes.Clone(input.Root)), clock: input.Clock, origin: "https://secretsmanager." + input.Manifest.Region + ".amazonaws.com"}
	h.configureRuntime("static")
	h.worker.ID = fmt.Sprintf("real-aws-replay-%d", os.Getpid())
	loader, ok := h.worker.Loader.(awsRealLoader)
	if !ok {
		t.Fatal("unexpected replay loader")
	}
	h.worker.Loader = awsReplayLoader{awsRealLoader: loader, targetID: input.TargetID, jobID: input.JobID}
	if input.CrashARN != "" {
		h.afterPut = func(_ context.Context, arn string) error {
			if arn == input.CrashARN {
				// Deliberately bypass every defer, including journal Finish.
				os.Exit(awsReplayCrashExit)
			}
			return errors.New("unexpected replay write identity")
		}
	}
	h.drain()
	h.converged(input.TargetID)
}

// Import original reservations without allocating a fresh budget. Historical
// replacement ARNs consume distinct reservations even when their names match.
func importAWSReplayCustody(input awsReplayInput) (*realAWSSandbox, error) {
	if len(input.Creations) == 0 || len(input.Creations) > realAWSMaxCreations || len(input.Owned) == 0 || len(input.Owned) > len(input.Creations) {
		return nil, errors.New("invalid replay creation provenance")
	}
	s := &realAWSSandbox{manifest: input.Manifest, owned: make(map[string]string), replayOnly: true}
	remaining := make(map[string]int)
	for _, name := range input.Creations {
		if err := s.checkName(name); err != nil {
			return nil, err
		}
		remaining[name]++
	}
	s.creations = append([]string(nil), input.Creations...)
	for arn, name := range input.Owned {
		if remaining[name] == 0 {
			return nil, errors.New("replay resource lacks its original creation reservation")
		}
		remaining[name]--
		if err := s.remember(name, arn); err != nil {
			return nil, err
		}
	}
	current := make(map[string]string)
	for name, arn := range input.Current {
		if s.owned[arn] != name || name == "" {
			return nil, errors.New("invalid replay current resource identity")
		}
		current[name] = arn
	}
	for _, name := range s.owned {
		if current[name] == "" {
			return nil, errors.New("missing replay current resource identity")
		}
	}
	s.current = current
	return s, nil
}

func TestAWSRealReplayCustodyImport(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	name := m.Prefix + "static/document"
	arn := "arn:aws:secretsmanager:" + m.Region + ":" + m.Account + ":secret:" + name + "-abcdef"
	replacement := strings.TrimSuffix(arn, "abcdef") + "ghijkl"
	fixture := func() awsReplayInput {
		return awsReplayInput{Manifest: m, Creations: []string{name, name}, Owned: map[string]string{arn: name, replacement: name}, Current: map[string]string{name: replacement}}
	}
	t.Run("original-and-replacement", func(t *testing.T) {
		input := fixture()
		s, err := importAWSReplayCustody(input)
		if err != nil || s.owned[arn] != name || s.current[name] != replacement || len(s.creations) != 2 {
			t.Fatal("original custody did not survive import")
		}
		if err := s.reserveCreate(name); err == nil || len(s.creations) != 2 {
			t.Fatal("import granted new creation authority")
		}
		input.Creations[0] = "changed"
		input.Current[name] = arn
		if s.creations[0] != name || s.current[name] != replacement {
			t.Fatal("import retained mutable input aliases")
		}
	})
	for _, test := range []struct {
		name string
		alter func(*awsReplayInput)
	}{
		{"missing-reservation", func(in *awsReplayInput) { in.Creations = nil }},
		{"replacement-without-reservation", func(in *awsReplayInput) { in.Creations = []string{name} }},
		{"over-budget", func(in *awsReplayInput) { in.Creations = make([]string, realAWSMaxCreations+1) }},
		{"foreign-reservation", func(in *awsReplayInput) { in.Creations[0] = "foreign/name" }},
		{"foreign-arn", func(in *awsReplayInput) { delete(in.Owned, arn); in.Owned[strings.Replace(arn, m.Account, "999999999999", 1)] = name }},
		{"missing-current", func(in *awsReplayInput) { in.Current = nil }},
		{"unknown-current", func(in *awsReplayInput) { in.Current[name] = arn + "bad" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := fixture()
			test.alter(&input)
			if _, err := importAWSReplayCustody(input); err == nil {
				t.Fatal("invalid custody import accepted")
			}
		})
	}
}
