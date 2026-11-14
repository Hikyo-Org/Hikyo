package securitydeployment

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

type ecsPrepared struct {
	Format             string                 `json:"format"`
	Target             Target                 `json:"target"`
	Source             ecsSnapshot            `json:"source"`
	Nonce              string                 `json:"nonce"`
	TargetDefinition   string                 `json:"target_definition"`
	RegistrationSHA256 releaseidentity.Digest `json:"registration_sha256"`
}

type ecsRegistrationIntent struct {
	Format             string                 `json:"format"`
	Nonce              string                 `json:"nonce"`
	Target             Target                 `json:"target"`
	Definition         string                 `json:"definition,omitempty"`
	RegistrationSHA256 releaseidentity.Digest `json:"registration_sha256"`
	// CreatedAt starts the settle window of a registration whose reply was lost.
	// A record without it gets its window started when recovery first reads it.
	CreatedAt time.Time `json:"created_at"`
	// Retiring is written before DeregisterTaskDefinition, so a lost reply is
	// settled by reading the revision instead of refusing the INACTIVE one.
	Retiring bool `json:"retiring,omitzero"`
}

const ecsPreparedFormat = "hikyo.security-ecs-preparation/v1"
const ecsRegistrationFormat = "hikyo.security-ecs-registration/v1"

func removeECSRecord(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return filedurability.SyncDirectory(filepath.Dir(path))
}

func (e *Executor) ecsPreparedPath() string {
	return filepath.Join(e.c.StateDirectory, "ecs-prepared.json")
}
func (e *Executor) ecsRegistrationPath() string {
	return filepath.Join(e.c.StateDirectory, "ecs-registration.json")
}

func (e *Executor) readECSPrepared() (ecsPrepared, error) {
	var prepared ecsPrepared
	raw, err := privateFile(e.ecsPreparedPath())
	if err != nil {
		return prepared, err
	}
	if decode(raw, &prepared) != nil || prepared.Format != ecsPreparedFormat || e.target(prepared.Target) != nil || !ecsTaskIDPattern.MatchString(prepared.Nonce) || !e.c.ECS.definitionARN(prepared.TargetDefinition) || prepared.RegistrationSHA256.Validate() != nil || !validObservation(prepared.Source.Source) {
		return prepared, ErrInvalid
	}
	return prepared, nil
}

func (e *Executor) writeECSRecord(path string, value any) error {
	raw, err := json.Marshal(value, jsonwire.Stable)
	if err != nil || len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	return securefile.WriteAtomic(path, raw, 0600)
}

func (e *Executor) readECSRegistration() (ecsRegistrationIntent, error) {
	var intent ecsRegistrationIntent
	raw, err := privateFile(e.ecsRegistrationPath())
	if err != nil {
		return intent, err
	}
	if decode(raw, &intent) != nil || intent.Format != ecsRegistrationFormat || !ecsTaskIDPattern.MatchString(intent.Nonce) || e.target(intent.Target) != nil || intent.RegistrationSHA256.Validate() != nil || (intent.Definition != "" && !e.c.ECS.definitionARN(intent.Definition)) || (intent.Retiring && intent.Definition == "") {
		return intent, ErrInvalid
	}
	return intent, nil
}

func ownedECSRegistration(definition ecsDefinition, nonce string) bool {
	return slices.Contains(definition.Tags, ecsTag{Key: "hikyo.dev/security-registration", Value: nonce})
}

// retireECSRegistration deregisters the intent's never-selected revision and then
// removes the registration record. The retiring marker is durable before the call.
func (e *Executor) retireECSRegistration(ctx context.Context, intent ecsRegistrationIntent) error {
	arn := intent.Definition
	if !e.c.ECS.definitionARN(arn) || intent.RegistrationSHA256.Validate() != nil {
		return ErrInvalid
	}
	service, err := e.ecsService(ctx)
	if err != nil {
		return err
	}
	if service.ARN != e.c.ECS.ServiceARN || service.TaskDefinition == arn {
		return ErrRecoveryRequired
	}
	if service.Desired != 0 && service.Desired != 1 {
		return ErrUnsupported
	}
	if err := e.validateECSService(service, service.Desired); err != nil {
		return err
	}
	definition, err := e.ecsDefinition(ctx, arn)
	// The record already sent the deregistration and lost its reply, or ECS reports the revision
	// retired by any hand; either way nothing is left to retire.
	if errors.Is(err, errECSDefinitionInactive) || intent.Retiring && errors.Is(err, errECSDefinitionRetired) {
		return removeECSRecord(e.ecsRegistrationPath())
	}
	if err != nil {
		return err
	}
	actual, err := ecsRegistrationDigest(definition.Registration)
	if err != nil || actual != intent.RegistrationSHA256 || !ownedECSRegistration(definition, intent.Nonce) {
		return ErrConflict
	}
	if !intent.Retiring {
		intent.Retiring = true
		if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
			return err
		}
	}
	var response struct {
		Definition map[string]jsontext.Value `json:"taskDefinition"`
	}
	if err := e.aws(ctx, "ecs", "deregister-task-definition", map[string]any{"taskDefinition": arn}, &response); err != nil {
		return err
	}
	var returned string
	var status string
	if json.Unmarshal(response.Definition["taskDefinitionArn"], &returned) != nil || returned != arn || json.Unmarshal(response.Definition["status"], &status) != nil || status != "INACTIVE" {
		return ErrConflict
	}
	return removeECSRecord(e.ecsRegistrationPath())
}

// retireECSPrepared hands a prepared revision to a registration record first, so
// its retirement resumes through recoverECSRegistration after any interruption.
func (e *Executor) retireECSPrepared(ctx context.Context, prepared ecsPrepared) error {
	intent := ecsRegistrationIntent{Format: ecsRegistrationFormat, Nonce: prepared.Nonce, Target: prepared.Target, Definition: prepared.TargetDefinition, RegistrationSHA256: prepared.RegistrationSHA256, CreatedAt: e.now().UTC()}
	if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
		return err
	}
	if err := removeECSRecord(e.ecsPreparedPath()); err != nil {
		return err
	}
	return e.retireECSRegistration(ctx, intent)
}

func (e *Executor) recoverECSRegistration(ctx context.Context) error {
	intent, err := e.readECSRegistration()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if intent.Definition == "" {
		if intent.CreatedAt.IsZero() {
			// A record from before CreatedAt: start its settle window now.
			intent.CreatedAt = e.now().UTC()
			if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
				return err
			}
			return ErrNotReady
		}
		// A lost reply has no revision handle, so find it by its exact digest and nonce.
		// ECS listings are eventually consistent: absence counts only once settled.
		// shortcut: only the newest ACTIVE revision is checked, so one hidden behind a
		// later foreign registration leaks as an unused definition, not a running task.
		settled := e.now().Sub(intent.CreatedAt) > ecsJobSettle
		var listed struct {
			ARNs []string `json:"taskDefinitionArns"`
			Next string   `json:"nextToken"`
		}
		if err := e.aws(ctx, "ecs", "list-task-definitions", map[string]any{"familyPrefix": e.c.ECS.TaskFamily, "status": "ACTIVE", "sort": "DESC", "maxResults": 100}, &listed); err != nil {
			return err
		}
		newest := slices.IndexFunc(listed.ARNs, e.c.ECS.definitionARN)
		if newest < 0 && listed.Next != "" {
			return ErrRecoveryRequired
		}
		var definition ecsDefinition
		if newest >= 0 {
			if definition, err = e.ecsDefinition(ctx, listed.ARNs[newest]); err != nil {
				return err
			}
		}
		digest, err := ecsRegistrationDigest(definition.Registration)
		if err != nil {
			return err
		}
		if newest < 0 || digest != intent.RegistrationSHA256 || definition.Container.Image != intent.Target.Image || !ownedECSRegistration(definition, intent.Nonce) {
			if !settled {
				return ErrNotReady
			}
			return removeECSRecord(e.ecsRegistrationPath())
		}
		intent.Definition = definition.ARN
		if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
			return err
		}
	}
	if prepared, err := e.readECSPrepared(); err == nil && prepared.TargetDefinition == intent.Definition && sameTarget(prepared.Target, intent.Target) && prepared.RegistrationSHA256 == intent.RegistrationSHA256 {
		return removeECSRecord(e.ecsRegistrationPath())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return e.retireECSRegistration(ctx, intent)
}

func (e *Executor) registerECSTarget(ctx context.Context, source ecsSnapshot, target Target) (ecsPrepared, error) {
	var result ecsPrepared
	if err := e.recoverECSRegistration(ctx); err != nil {
		return result, err
	}
	currentHash, err := ecsSnapshotDigest(source)
	if err != nil {
		return result, err
	}
	previous, err := e.readECSPrepared()
	if err == nil {
		oldHash, err := ecsSnapshotDigest(previous.Source)
		if err != nil {
			return result, err
		}
		if sameTarget(previous.Target, target) && oldHash == currentHash {
			definition, err := e.ecsDefinition(ctx, previous.TargetDefinition)
			if err != nil {
				return result, err
			}
			digest, err := ecsRegistrationDigest(definition.Registration)
			if err != nil || digest != previous.RegistrationSHA256 || definition.Container.Image != target.Image {
				return result, ErrConflict
			}
			return previous, nil
		}
		if previous.TargetDefinition != source.Service.TaskDefinition {
			if err := e.retireECSPrepared(ctx, previous); err != nil {
				return result, err
			}
		} else if err := removeECSRecord(e.ecsPreparedPath()); err != nil {
			return result, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	definition := ecsDefinition{Registration: source.Definition}
	registration, err := definition.withImage(target.Image)
	if err != nil {
		return result, err
	}
	digest, err := ecsRegistrationDigest(registration)
	if err != nil {
		return result, err
	}
	nonce, err := ecsNonce()
	if err != nil {
		return result, err
	}
	intent := ecsRegistrationIntent{Format: ecsRegistrationFormat, Nonce: nonce, Target: target, RegistrationSHA256: digest, CreatedAt: e.now().UTC()}
	if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
		return result, err
	}
	request := make(map[string]jsontext.Value)
	for key, value := range registration {
		request[key] = value
	}
	request["tags"], err = json.Marshal([]ecsTag{{Key: "hikyo.dev/security-maintenance-owner", Value: e.c.ECS.OwnerTag}, {Key: "hikyo.dev/security-registration", Value: nonce}, {Key: "hikyo.dev/security-release-manifest", Value: string(target.Identity.ManifestSHA256)}}, jsonwire.Stable)
	if err != nil {
		return result, err
	}
	var response struct {
		Definition map[string]jsontext.Value `json:"taskDefinition"`
	}
	if err := e.aws(ctx, "ecs", "register-task-definition", request, &response); err != nil {
		if errors.Is(err, errECSNoEffect) {
			return result, errors.Join(err, removeECSRecord(e.ecsRegistrationPath()))
		}
		return result, err
	}
	registered, err := e.validateECSDefinition(response.Definition)
	if err != nil {
		return result, err
	}
	actual, err := ecsRegistrationDigest(registered.Registration)
	if err != nil || actual != digest || registered.Container.Image != target.Image {
		return result, ErrConflict
	}
	intent.Definition = registered.ARN
	if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
		return result, err
	}
	result = ecsPrepared{Format: ecsPreparedFormat, Target: target, Source: source, Nonce: nonce, TargetDefinition: registered.ARN, RegistrationSHA256: digest}
	if err := e.writeECSRecord(e.ecsPreparedPath(), result); err != nil {
		return result, err
	}
	if err := removeECSRecord(e.ecsRegistrationPath()); err != nil {
		return result, err
	}
	return result, nil
}

func (e *Executor) prepareECS(ctx context.Context, target Target) (Observation, error) {
	if err := e.recoverECSJob(ctx); err != nil {
		return Observation{}, err
	}
	if err := e.recoverECSRegistration(ctx); err != nil {
		return Observation{}, err
	}
	source, err := e.ecsCurrent(ctx, true)
	if err != nil {
		return Observation{}, err
	}
	prepared, err := e.registerECSTarget(ctx, source, target)
	if err != nil {
		return Observation{}, err
	}
	staged, err := e.ecsJob(ctx, source.Service, prepared.TargetDefinition, targetRuntimeDigest(target), "stage", []string{"security-maintenance", "stage", "--expected-manifest", string(target.Identity.ManifestSHA256)})
	if err != nil {
		// No service mutation has occurred. Only this known, never-selected
		// revision may be retired; a failure leaves its registration record for recovery.
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return Observation{}, errors.Join(err, e.retireECSPrepared(cleanup, prepared))
	}
	staged.Image, staged.RuntimeImageSHA256 = source.Source.Image, source.Source.RuntimeImageSHA256
	if staged.Engine != releaseidentity.Postgres || !sameSource(source.Source, staged) || staged.StagedTarget != target.Identity || staged.Phase != "staged" || staged.Ready || staged.RouteSHA256.Validate() != nil {
		return Observation{}, ErrConflict
	}
	after, err := e.ecsCurrent(ctx, true)
	if err != nil {
		return Observation{}, err
	}
	beforeHash, _ := ecsSnapshotDigest(source)
	afterHash, _ := ecsSnapshotDigest(after)
	if beforeHash != afterHash {
		return Observation{}, ecsSourceIdentityError()
	}
	j := &deploymentJournal{Format: journalFormat, Phase: "prepared", Target: target, Source: source.Source, Staged: staged, DesiredSHA256: beforeHash}
	if err := e.save(j); err != nil {
		return Observation{}, err
	}
	return staged, nil
}

func targetRuntimeDigest(target Target) string {
	if target.RuntimeImageSHA256 != "" {
		return target.RuntimeImageSHA256
	}
	return imageDigest(target.Image)
}

func (e *Executor) updateECSService(ctx context.Context, desired int, definition string) error {
	if desired != 0 && desired != 1 || !e.c.ECS.definitionARN(definition) {
		return ErrInvalid
	}
	input := map[string]any{"cluster": e.c.ECS.ClusterARN, "service": e.c.ECS.ServiceARN, "desiredCount": desired}
	if desired == 1 {
		input["taskDefinition"] = definition
		input["forceNewDeployment"] = true
	}
	var response struct {
		Service ecsService `json:"service"`
	}
	if err := e.aws(ctx, "ecs", "update-service", input, &response); err != nil {
		return err
	}
	if err := e.validateECSServiceContract(response.Service, desired); err != nil {
		return err
	}
	if response.Service.TaskDefinition != definition {
		return ErrConflict
	}
	return nil
}

func (e *Executor) applyECS(ctx context.Context, j *deploymentJournal) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(e.timeoutSeconds())*time.Second)
	defer cancel()
	prepared, err := e.readECSPrepared()
	if err != nil {
		return err
	}
	if !sameTarget(prepared.Target, j.Target) {
		return ErrConflict
	}
	source, err := e.ecsCurrent(ctx, true)
	if err != nil {
		return err
	}
	hash, err := ecsSnapshotDigest(source)
	if err != nil || hash != j.DesiredSHA256 || !sameSource(source.Source, j.Source) {
		return ErrConflict
	}
	preparedHash, err := ecsSnapshotDigest(prepared.Source)
	if err != nil || preparedHash != hash {
		return ErrConflict
	}
	definition, err := e.ecsDefinition(ctx, prepared.TargetDefinition)
	if err != nil {
		return err
	}
	digest, err := ecsRegistrationDigest(definition.Registration)
	if err != nil || digest != prepared.RegistrationSHA256 || definition.Container.Image != j.Target.Image {
		return ErrConflict
	}
	// UpdateService has no CAS. The customer must enforce exclusive mutation
	// authority in IAM; this final observed snapshot check cannot replace that.
	if err := e.beginReplacement(j); err != nil {
		return err
	}
	if err := e.updateECSService(ctx, 0, source.Service.TaskDefinition); err != nil {
		return err
	}
	if err := e.waitECS(ctx, func(wait context.Context) (bool, error) {
		service, err := e.ecsService(wait)
		if err != nil {
			return false, err
		}
		// ECS reads are eventually consistent: a stale pre-scale-down count is not done yet.
		desired := 0
		if service.Desired == 1 {
			desired = 1
		}
		if err := e.validateECSService(service, desired); err != nil {
			return false, err
		}
		original, _ := ecsServiceContractDigest(source.Service)
		current, _ := ecsServiceContractDigest(service)
		if original != current || service.TaskDefinition != source.Service.TaskDefinition {
			return false, ErrConflict
		}
		tasks, err := e.ecsTasks(wait, []string{source.SourceTask})
		if err != nil {
			return false, err
		}
		if len(tasks) != 1 || tasks[0].Definition != source.Service.TaskDefinition || tasks[0].Group != "service:"+e.c.ECS.serviceName() {
			return false, ErrConflict
		}
		if desired != 0 || tasks[0].LastStatus != "STOPPED" || service.Running != 0 || service.Pending != 0 {
			return false, nil
		}
		remaining, err := e.ecsServiceTasks(wait)
		if err != nil {
			return false, err
		}
		return len(remaining) == 0, nil
	}); err != nil {
		return err
	}
	if err := e.ecsNoAutoscaling(ctx); err != nil {
		return err
	}
	if err := e.updateECSService(ctx, 1, prepared.TargetDefinition); err != nil {
		return err
	}
	if err := e.waitECS(ctx, func(wait context.Context) (bool, error) {
		service, err := e.ecsService(wait)
		if err != nil {
			return false, err
		}
		// A stale read of the scaled-down service: same contract checks, not done yet.
		if (service.Desired == 0 || service.TaskDefinition == source.Service.TaskDefinition) && service.Desired <= 1 {
			return false, e.validateECSService(service, service.Desired)
		}
		current, err := e.ecsCurrent(wait, false)
		if errors.Is(err, ErrNotReady) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if current.Service.TaskDefinition != prepared.TargetDefinition {
			return false, ErrConflict
		}
		tasks, err := e.ecsTasks(wait, []string{current.SourceTask})
		if err != nil {
			return false, err
		}
		if len(tasks) != 1 || tasks[0].Containers[0].Image != j.Target.Image || tasks[0].Containers[0].Digest != "sha256:"+targetRuntimeDigest(j.Target) {
			return false, ErrConflict
		}
		return true, nil
	}); err != nil {
		return err
	}
	observed, err := e.observeECS(ctx)
	if err != nil {
		return err
	}
	if observed.Identity != j.Target.Identity || observed.Image != j.Target.Image || observed.RuntimeImageSHA256 != targetRuntimeDigest(j.Target) || observed.Instance != j.Source.Instance || observed.RestoreEpoch != j.Source.RestoreEpoch || observed.Incarnation != j.Source.Incarnation || observed.Generation <= j.Source.Generation || !observed.Ready || observed.Maintenance || observed.Phase != "healthy" {
		return ErrNotReady
	}
	return nil
}

func (e *Executor) observeECS(ctx context.Context) (Observation, error) {
	snapshot, err := e.ecsCurrent(ctx, true)
	if err != nil {
		return Observation{}, err
	}
	j, err := e.journal()
	if err != nil {
		return Observation{}, err
	}
	if j != nil && snapshot.Source.Identity == j.Target.Identity {
		if snapshot.Source.Image != j.Target.Image || snapshot.Source.RuntimeImageSHA256 != targetRuntimeDigest(j.Target) {
			return Observation{}, ErrConflict
		}
	}
	return snapshot.Source, nil
}
