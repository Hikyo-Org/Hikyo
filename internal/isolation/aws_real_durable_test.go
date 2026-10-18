package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// One opt-in entry point owns the entire real-provider suite and its single
// creation budget. Subtests must not load independent sandbox registries.
func TestAWSSecretsManagerRealSmoke(t *testing.T) {
	s := loadRealAWSSandbox(t)
	if !t.Run("auth-boundaries", func(t *testing.T) { runRealAWSAuthBoundaries(t, s) }) {
		t.Fatal("auth boundary preflight failed before mutations")
	}
	t.Run("lifecycle", func(t *testing.T) {
		forEngines(t, func(t *testing.T, db *store.DB) {
			h := newRealAWSDurable(t, db, s, "static")
			h.lifecycle()
		})
	})
	t.Run("role-auth", func(t *testing.T) { runRealAWSRoleAuth(t, s) })
	t.Run("kms-denial", func(t *testing.T) { runRealAWSKMSDenial(t, s) })
	t.Run("iam-denial", func(t *testing.T) { runRealAWSIAMDenial(t, s) })
	t.Run("adoption-witness", func(t *testing.T) { runRealAWSAdoptionWitness(t, s) })
	t.Run("promoted-replay", func(t *testing.T) { runRealAWSPromotedReplay(t, s) })
}

func runRealAWSRoleAuth(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	for _, mode := range []string{"ambient", "assume-role", "web-identity"} {
		t.Run(mode, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h := newRealAWSDurable(t, db, s, mode)
				name := h.prefix + "role-document"
				target := h.create(name, adapter.JSONObject)
				h.publish("role-synthetic", "role-alpha")
				h.drain()
				h.converged(target.ID)
				if s.value(t, name) != `{"ALPHA":"role-alpha","SHARED_KEY":"role-synthetic"}` {
					t.Fatal("role-auth delivery differs from the synthetic source")
				}
				// Client construction must refuse these very same descriptors
				// without the node operator's explicit workload opt-in.
				c, err := awssm.NewClient(awssm.ClientConfig{Origin: h.origin, Credential: h.descriptor, Deadline: 15 * time.Second})
				if c != nil {
					c.Forget()
				}
				if !errors.Is(err, awssm.ErrWorkloadIdentityDisabled) {
					t.Fatal("workload descriptor bypassed the node opt-in")
				}
				if mode == "assume-role" {
					d, err := awssm.ParseDescriptor(h.descriptor)
					if err != nil {
						t.Fatal("cannot derive negative role fixture")
					}
					d.ExternalID += ":rejected"
					bad, err := json.Marshal(d)
					if err != nil {
						t.Fatal("cannot encode negative role fixture")
					}
					negative, err := awssm.NewClient(awssm.ClientConfig{Origin: h.origin, Credential: string(bad), WorkloadIdentity: true, Deadline: 15 * time.Second})
					crypto.Zero(bad)
					if err != nil {
						t.Fatal("negative role fixture did not reach STS")
					}
					_, identityErr := negative.ResolveIdentity(t.Context())
					negative.Forget()
					if !errors.Is(identityErr, adapter.ErrProviderAuth) {
						t.Fatal("STS did not enforce the exact external ID trust condition")
					}
				}
				s.checkWriterDenials(t, mode, name, s.manifest.Prefix+"static/sqlite/document")
				h.remove(target.ID, false)
				h.deleted(name, true)
				h.audit()
			})
		})
	}
}

func runRealAWSKMSDenial(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newRealAWSDurable(t, db, s, "kms-denied")
		target := h.create(h.prefix+"denied-document", adapter.JSONObject)
		h.publish("kms-synthetic", "kms-alpha")
		h.drain()
		state := h.inspect(target.ID)
		if state.LastErrorClass != adapter.ErrorClassAuth || state.Health() == adapter.HealthConverged || state.ConvergedRevision != nil {
			t.Fatal("real KMS denial was not recorded as an unsuccessful auth refusal")
		}
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome='success' AND disposition IN ('create','update')`, target.ID)); got != 0 {
			t.Fatal("KMS refusal recorded a successful value write")
		}
		h.audit()
	})
}

type realAWSDurable struct {
	t                          *testing.T
	db                         *store.DB
	s                          *realAWSSandbox
	svc                        *service.Adapters
	keyring                    *crypto.Keyring
	worker                     *adapter.Worker
	clock                      time.Time
	build                      func(adapter.Config, string) (*adapter.ModuleLease, error)
	descriptor, origin, prefix string
	adapterID                  string
	afterPut                   func(context.Context, string) error
	losePut                    bool
	failCurrentMarker          bool
	finishAdoption             func(string) error
	secrets                    []string
}

func newRealAWSDurable(t *testing.T, db *store.DB, s *realAWSSandbox, mode string) *realAWSDurable {
	t.Helper()
	raw, _ := s.writer(t, mode) // identity proof precedes every mutation
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_real_aws_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_real_aws_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	// Fixture declarations only; values and publication go through services.
	execRaw(t, db, `INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at) VALUES ('key_real_alpha','org_a','prj_a1','ALPHA','','secret','',FALSE,'','{"rule":{"type":"string"}}','none','none',NULL,`+ts+`)`)
	h := &realAWSDurable{t: t, db: db, s: s, keyring: probeKeyring(t, db), clock: time.Now().UTC(), descriptor: raw, origin: "https://secretsmanager." + s.manifest.Region + ".amazonaws.com", prefix: s.manifest.Prefix + mode + "/" + string(db.Engine()) + "/"}
	h.configureRuntime(mode)
	t.Cleanup(func() { h.descriptor = "" })
	return h
}

func (h *realAWSDurable) configureRuntime(mode string) {
	h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		if config.Origin != h.origin {
			return nil, errors.New("real AWS fixture origin changed")
		}
		client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, WorkloadIdentity: mode != "static" && mode != "kms-denied", Deadline: 15 * time.Second})
		if err != nil {
			return nil, err
		}
		var module adapter.Module = &awssm.Module{API: &realAWSWriter{API: client, t: h.t, s: h.s, losePut: &h.losePut, failCurrentMarker: &h.failCurrentMarker, afterPut: func(ctx context.Context, id string) error {
			if h.afterPut != nil {
				return h.afterPut(ctx, id)
			}
			return nil
		}}}
		if h.finishAdoption != nil {
			module = &awsAdoptionFinishModule{Module: module, finish: h.finishAdoption}
		}
		return adapter.NewModuleLease(module, client.Forget)
	}
	h.svc = &service.Adapters{DB: h.db, Keyring: h.keyring, ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		if provider != adapter.AWSSecretsManagerProvider {
			return nil, errors.New("unexpected real AWS fixture provider")
		}
		return h.build(config, credential)
	}}
	h.restartWorker()
}

func (h *realAWSDurable) restartWorker() {
	runtime := store.NewAdapterRuntime(h.db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, h.db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID)})
			return err
		})
	})
	h.worker = &adapter.Worker{Store: runtime, Loader: awsRealLoader{awsTestLoader{runtime: runtime, keyring: h.keyring, build: h.build}}, ID: "real-aws-" + string(h.db.Engine()), Now: func() time.Time { return h.clock }, Jitter: func(d time.Duration) time.Duration { return d }, Wait: func(context.Context, time.Duration) error { return nil }}
}

func (h *realAWSDurable) create(name string, kind adapter.DestinationKind) store.AdapterTarget {
	h.t.Helper()
	input := service.AdapterTargetInput{EnvironmentID: string(envA1), DestinationKind: string(kind), DestinationOwner: h.s.manifest.Account, DestinationName: name, DestinationEnvironment: h.s.manifest.KMSKeyARN, KeySelection: &service.AdapterKeySelection{Names: []string{"SHARED_KEY", "ALPHA"}}}
	scope := domain.Scope{Org: orgA, Project: prjA1}
	if h.adapterID != "" {
		target, err := h.svc.AddTarget(h.t.Context(), service.LocalPrincipal(alice), scope, h.adapterID, input)
		if err != nil {
			h.t.Fatal("real AWS target creation failed")
		}
		return target
	}
	created, err := h.svc.Create(h.t.Context(), service.LocalPrincipal(alice), scope, service.CreateAdapterRequest{Provider: string(adapter.AWSSecretsManagerProvider), Origin: h.origin, Credential: []byte(h.descriptor), Target: input})
	if err != nil {
		h.t.Fatal("real AWS adapter creation failed")
	}
	h.adapterID = created.Adapter.ID
	return created.Targets[0]
}

func (h *realAWSDurable) publish(shared, alpha string) {
	h.t.Helper()
	values := service.Values{DB: h.db, Keyring: h.keyring}
	var versions []string
	for _, row := range []struct{ name, value string }{{"SHARED_KEY", shared}, {"ALPHA", alpha}} {
		staged, err := values.Set(h.t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, row.name, row.value, nil)
		if err != nil {
			h.t.Fatal("synthetic source staging failed")
		}
		versions = append(versions, staged.VersionID)
		h.secrets = append(h.secrets, row.value)
	}
	h.publishVersions(versions)
}

func (h *realAWSDurable) publishVersions(versions []string) {
	h.t.Helper()
	revisions := service.Revisions{DB: h.db, Keyring: h.keyring}
	if _, err := revisions.PublishPlanned(h.t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, service.PublishRequest{VersionIDs: versions}); err != nil {
		h.t.Fatal("synthetic publication failed")
	}
}

func (h *realAWSDurable) drain() {
	h.t.Helper()
	for range 24 {
		// Services enqueue against wall time, which can overtake the fixture
		// clock during real provider calls. Preserve deliberate replay advances.
		if now := time.Now().UTC(); h.clock.Before(now) {
			h.clock = now
		}
		h.clock = h.clock.Add(time.Second)
		worked, err := h.worker.RunOnce(h.t.Context())
		if err != nil {
			h.t.Fatal("real AWS durable worker failed")
		}
		if !worked {
			return
		}
	}
	h.t.Fatal("real AWS worker did not drain within its bounded attempts")
}

func (h *realAWSDurable) inspect(id string) store.AdapterTarget {
	h.t.Helper()
	view, err := h.svc.InspectTarget(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, id)
	if err != nil {
		h.t.Fatal("durable AWS target inspection failed")
	}
	// Ordinary reads must not contain any part of the protected descriptor.
	raw, err := json.Marshal(view)
	if err != nil {
		h.t.Fatal("cannot inspect ordinary target representation")
	}
	d, _ := awssm.ParseDescriptor(h.descriptor)
	for _, secret := range []string{d.SecretKey, d.SessionToken} {
		if secret != "" && bytesContain(raw, secret) {
			h.t.Fatal("ordinary target read exposed credentials")
		}
	}
	return view.Target
}

func bytesContain(raw []byte, secret string) bool { return strings.Contains(string(raw), secret) }

func (h *realAWSDurable) converged(id string) {
	h.t.Helper()
	state := h.inspect(id)
	if state.Health() != adapter.HealthConverged || state.ConvergedRevision == nil || state.LastErrorClass != "" {
		h.t.Fatal("durable AWS target did not converge")
	}
}

func (h *realAWSDurable) metadata(name string) awssm.SecretMetadata {
	h.t.Helper()
	lease, err := h.build(adapter.Config{Origin: h.origin}, h.descriptor)
	if err != nil {
		h.t.Fatal("AWS metadata client failed")
	}
	defer lease.Release()
	module, ok := lease.Module.(*awssm.Module)
	if !ok {
		h.t.Fatal("unexpected AWS module")
	}
	meta, err := module.API.DescribeSecret(h.t.Context(), h.s.arn(h.t, name))
	if err != nil {
		h.t.Fatal("AWS fixture metadata read failed")
	}
	return meta
}

func (h *realAWSDurable) deleted(name string, want bool) {
	h.t.Helper()
	if h.metadata(name).Deleted != want {
		h.t.Fatal("AWS scheduled deletion state differs from expected lifecycle")
	}
}

func (h *realAWSDurable) resync(id string) string {
	h.t.Helper()
	result, err := h.svc.SyncTarget(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, id)
	if err != nil {
		h.t.Fatal("durable resync refused")
	}
	return result.JobID
}

func (h *realAWSDurable) remove(id string, retain bool) {
	h.t.Helper()
	if _, err := h.svc.RemoveTarget(h.t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, id, retain); err != nil {
		h.t.Fatal("durable target removal refused")
	}
	h.clock = h.clock.Add(2 * adapter.LeaseTime)
	h.drain()
}

func (h *realAWSDurable) audit() {
	h.t.Helper()
	d, _ := awssm.ParseDescriptor(h.descriptor)
	for _, secret := range append(h.secrets, d.SecretKey, d.SessionToken) {
		if secret == "" {
			continue
		}
		// Parameterize secrets and suppress driver diagnostics: the shared
		// queryInt helper prints SQL on failure and must not receive canaries.
		var count int64
		var err error
		if h.db.Engine() == store.EnginePostgres {
			err = h.db.PG().QueryRow(h.t.Context(), `SELECT COUNT(*) FROM audit_tenant_events WHERE strpos(payload,$1)>0`, secret).Scan(&count)
		} else {
			err = h.db.SQLiteRead().QueryRowContext(h.t.Context(), `SELECT COUNT(*) FROM audit_tenant_events WHERE instr(payload,?)>0`, secret).Scan(&count)
		}
		if err != nil {
			h.t.Fatal("protected-material audit query failed")
		}
		if count > 0 {
			h.t.Fatal("AWS audit contains protected material")
		}
	}
}

func (h *realAWSDurable) lifecycle() {
	t := h.t
	scope := domain.Scope{Org: orgA, Project: prjA1}
	actor := service.LocalPrincipal(alice)
	jsonName, keysName := h.prefix+"document", h.prefix+"keys/"
	jsonTarget := h.create(jsonName, adapter.JSONObject)
	keysTarget := h.create(keysName, adapter.PerKey)
	h.publish("shared\nline é", "alpha synthetic")
	h.drain()
	h.converged(jsonTarget.ID)
	h.converged(keysTarget.ID)
	if h.s.value(t, jsonName) != `{"ALPHA":"alpha synthetic","SHARED_KEY":"shared\nline é"}` || h.s.value(t, keysName+"SHARED_KEY") != "shared\nline é" {
		t.Fatal("AWS canonical JSON or per-key delivery differs from source")
	}
	if h.metadata(jsonName).KMSKeyID != h.s.manifest.KMSKeyARN {
		t.Fatal("customer KMS key was not applied")
	}

	if _, err := h.svc.PauseTarget(t.Context(), actor, scope, jsonTarget.ID); err != nil {
		t.Fatal("pause failed")
	}
	h.publish("second synthetic", "second alpha")
	h.drain()
	h.converged(keysTarget.ID)
	if h.inspect(jsonTarget.ID).Health() != adapter.HealthPaused || h.s.value(t, jsonName) != `{"ALPHA":"alpha synthetic","SHARED_KEY":"shared\nline é"}` {
		t.Fatal("paused target changed provider state")
	}
	resumed, err := h.svc.ResumeTarget(t.Context(), actor, scope, jsonTarget.ID)
	if err != nil || resumed.Enqueue.JobID == "" {
		t.Fatal("resume did not name the catch-up job")
	}
	h.drain()
	h.converged(jsonTarget.ID)
	if state := h.inspect(jsonTarget.ID); state.ConvergedRevision == nil || *state.ConvergedRevision != resumed.Revision {
		t.Fatal("resume converged a different revision")
	}

	// A child exits immediately after the real provider accepts PutSecretValue,
	// before any journal outcome. A second fresh process reopens the admitted
	// SQL store and key hierarchy and resumes the same durable job.
	before := len(h.metadata(jsonName).Stages)
	jobID := h.resync(jsonTarget.ID)
	h.processReplay(jsonTarget.ID, jsonName, jobID)
	h.converged(jsonTarget.ID)
	if len(h.metadata(jsonName).Stages) != before+1 {
		t.Fatal("process replay created a duplicate AWS version")
	}
	h.credentialFences(jsonTarget.ID, jsonName)

	// Remove and restore a source value while retaining the same target. The
	// production worker schedules deletion and restores within its window.
	values := service.Values{DB: h.db, Keyring: h.keyring}
	staged, err := values.Unset(t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, "SHARED_KEY")
	if err != nil {
		t.Fatal("source unset failed")
	}
	h.publishVersions([]string{staged.VersionID})
	h.drain()
	h.deleted(keysName+"SHARED_KEY", true)
	h.publish("restored synthetic", "restored alpha")
	h.drain()
	h.deleted(keysName+"SHARED_KEY", false)
	if h.s.value(t, keysName+"SHARED_KEY") != "restored synthetic" {
		t.Fatal("restore did not deliver the new source")
	}

	h.partialConflict(keysTarget.ID, keysName)

	// Interleave a real external write after Hikyo stages its candidate but
	// before conditional promotion. Preserve the foreign AWSCURRENT and let
	// the sibling target finish independently.
	jsonARN := h.s.arn(t, jsonName)
	h.afterPut = func(ctx context.Context, id string) error {
		if id != jsonARN {
			return nil
		}
		h.afterPut = nil
		return h.s.call(ctx, false, "PutSecretValue", id, map[string]any{"SecretString": "external synthetic", "ClientRequestToken": strings.Repeat("e", 64)}, nil)
	}
	h.publish("sibling synthetic", "sibling alpha")
	h.drain()
	if h.afterPut != nil {
		t.Fatal("concurrent AWS edit injection did not execute")
	}
	drift := h.inspect(jsonTarget.ID)
	if drift.LastErrorClass != adapter.ErrorClassConflict || !drift.DriftAttention || drift.Health() == adapter.HealthConverged || h.s.value(t, jsonName) != "external synthetic" {
		t.Fatal("external edit was not preserved as a loud conflict")
	}
	h.converged(keysTarget.ID)
	h.adoption()
	movedName := h.prefix + "moved-document"
	h.moveTarget(keysTarget.ID, keysName, movedName)
	h.retainAdapter(jsonTarget.ID, keysTarget.ID)
	h.deleted(jsonName, false)
	h.deleted(movedName, false)
	h.deleted(keysName+"SHARED_KEY", true)
	h.deleted(keysName+"ALPHA", true)
	if queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'`, keysTarget.ID)) != 0 {
		t.Fatal("retained adapter kept active durable custody")
	}
	linked := queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects e JOIN audit_tenant_events i ON i.id=e.intent_audit_id JOIN audit_tenant_events o ON o.id=e.outcome_audit_id WHERE e.target_id='%s' AND i.type='adapter.push_intent' AND o.type='adapter.push_outcome'`, keysTarget.ID))
	if linked < 3 {
		t.Fatal("durable INTENT/OUTCOME linkage is missing")
	}
	h.audit()
}

func (h *realAWSDurable) adoption() {
	t := h.t
	name := h.prefix + "adopted"
	var seeded struct {
		ARN string `json:"ARN"`
	}
	if err := h.s.call(t.Context(), false, "CreateSecret", name, map[string]any{"SecretString": "legacy synthetic", "KmsKeyId": h.s.manifest.KMSKeyARN, "ClientRequestToken": strings.Repeat("a", 64)}, &seeded); err != nil {
		t.Fatal("synthetic adoption seed failed")
	}
	if err := h.s.remember(name, seeded.ARN); err != nil {
		t.Fatal(err)
	}
	target := h.create(name, adapter.JSONObject)
	plan, err := h.svc.Plan(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, target.ID)
	if err != nil || plan.ArtifactID == "" {
		t.Fatal("unowned AWS secret did not produce an adoption artifact")
	}
	if _, err := h.svc.Adopt(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: plan.ArtifactID, ExpectedGeneration: target.Generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
		t.Fatal("version-bound AWS adoption failed")
	}
	h.losePut = true
	h.drain()
	if h.losePut || h.s.value(t, name) != "legacy synthetic" {
		t.Fatal("interrupted adoption did not preserve its approved predecessor")
	}
	h.restartWorker()
	h.clock = h.clock.Add(adapter.RetryCap + time.Second)
	h.drain()
	h.converged(target.ID)
	if len(h.metadata(name).Stages) != 2 || h.s.value(t, name) != `{"ALPHA":"sibling alpha","SHARED_KEY":"sibling synthetic"}` {
		t.Fatal("adoption replay did not converge exactly once")
	}
	h.remove(target.ID, false)
	h.deleted(name, true)
}
