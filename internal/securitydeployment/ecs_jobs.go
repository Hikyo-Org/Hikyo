package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

type ecsJobIntent struct {
	Format     string    `json:"format"`
	Nonce      string    `json:"nonce"`
	Definition string    `json:"definition"`
	TaskARN    string    `json:"task_arn,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

const ecsJobFormat = "hikyo.security-ecs-job/v1"

func (e *Executor) ecsJobPath() string { return filepath.Join(e.c.StateDirectory, "ecs-job.json") }

func (e *Executor) saveECSJob(intent ecsJobIntent) error {
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(e.ecsJobPath(), raw, 0600)
}

func (e *Executor) readECSJob() (ecsJobIntent, error) {
	var intent ecsJobIntent
	raw, err := privateFile(e.ecsJobPath())
	if err != nil {
		return intent, err
	}
	if decode(raw, &intent) != nil || intent.Format != ecsJobFormat || !ecsTaskIDPattern.MatchString(intent.Nonce) || !e.c.ECS.definitionARN(intent.Definition) || intent.CreatedAt.IsZero() || intent.TaskARN != "" && !e.c.ECS.taskARN(intent.TaskARN) {
		return intent, ErrInvalid
	}
	return intent, nil
}

func (e *Executor) ownedECSJob(task ecsTask, intent ecsJobIntent) bool {
	return e.c.ECS.taskARN(task.ARN) && task.Cluster == e.c.ECS.ClusterARN && task.Definition == intent.Definition && task.StartedBy == "hs-"+intent.Nonce && task.Group == "hikyo-security:"+e.c.ECS.OwnerTag && len(task.Containers) == 1 && task.Containers[0].Name == e.c.ECS.Container
}

func (e *Executor) recoverECSJob(ctx context.Context) error {
	intent, err := e.readECSJob()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if intent.TaskARN == "" {
		var found struct {
			ARNs []string `json:"taskArns"`
			Next string   `json:"nextToken"`
		}
		if err := e.aws(ctx, "ecs", "list-tasks", map[string]any{"cluster": e.c.ECS.ClusterARN, "startedBy": "hs-" + intent.Nonce, "maxResults": 100}, &found); err != nil {
			return err
		}
		if found.Next != "" || len(found.ARNs) != 1 {
			return ErrRecoveryRequired
		}
		intent.TaskARN = found.ARNs[0]
		if !e.c.ECS.taskARN(intent.TaskARN) {
			return ErrRecoveryRequired
		}
		if err := e.saveECSJob(intent); err != nil {
			return err
		}
	}
	return e.cleanupECSJob(ctx, intent)
}

func (e *Executor) cleanupECSJob(ctx context.Context, intent ecsJobIntent) error {
	tasks, err := e.ecsTasks(ctx, []string{intent.TaskARN})
	if err != nil {
		return err
	}
	if len(tasks) != 1 || !e.ownedECSJob(tasks[0], intent) {
		return ErrConflict
	}
	if tasks[0].LastStatus != "STOPPED" {
		var stopped struct {
			Task ecsTask `json:"task"`
		}
		if err := e.aws(ctx, "ecs", "stop-task", map[string]any{"cluster": e.c.ECS.ClusterARN, "task": intent.TaskARN, "reason": "Hikyo owned security preparation cleanup"}, &stopped); err != nil {
			return err
		}
		if !e.ownedECSJob(stopped.Task, intent) {
			return ErrConflict
		}
		if err := e.waitECS(ctx, func(wait context.Context) (bool, error) {
			current, err := e.ecsTasks(wait, []string{intent.TaskARN})
			if err != nil {
				return false, err
			}
			if len(current) != 1 || !e.ownedECSJob(current[0], intent) {
				return false, ErrConflict
			}
			return current[0].LastStatus == "STOPPED", nil
		}); err != nil {
			return err
		}
	}
	current, err := e.readECSJob()
	if err != nil {
		return err
	}
	if current.Nonce != intent.Nonce || current.Definition != intent.Definition || current.TaskARN != intent.TaskARN {
		return ErrConflict
	}
	return removeECSRecord(e.ecsJobPath())
}

// ecsJob runs only a fixed stage/inspect verb in an isolated, exact-image task.
// It never makes the customer service run arbitrary remotely supplied commands.
// Structured output is read only from this task's configured CloudWatch stream.
func (e *Executor) ecsJob(ctx context.Context, service ecsService, definition, runtimeDigest, purpose string, command []string) (observed Observation, returnErr error) {
	if !e.c.ECS.definitionARN(definition) || (purpose != "stage" && purpose != "inspect") {
		return observed, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(e.timeoutSeconds())*time.Second)
	defer cancel()
	proof, err := e.ecsDefinition(ctx, definition)
	if err != nil {
		return observed, err
	}
	if releaseidentity.Digest(runtimeDigest).Validate() != nil {
		return observed, ErrInvalid
	}
	if err := e.recoverECSJob(ctx); err != nil {
		return observed, err
	}
	nonce, err := ecsNonce()
	if err != nil {
		return observed, err
	}
	intent := ecsJobIntent{Format: ecsJobFormat, Nonce: nonce, Definition: definition, CreatedAt: time.Now().UTC()}
	if err := e.saveECSJob(intent); err != nil {
		return observed, err
	}
	// Persist the idempotency/ownership tuple before the API call. A lost reply
	// leaves this record for exact startedBy recovery, never blind task cleanup.
	input := map[string]any{"cluster": e.c.ECS.ClusterARN, "taskDefinition": definition, "launchType": "FARGATE", "platformVersion": service.PlatformVersion, "networkConfiguration": json.RawMessage(service.Network), "count": 1,
		"clientToken": purpose + "-" + nonce, "startedBy": "hs-" + nonce, "group": "hikyo-security:" + e.c.ECS.OwnerTag, "enableExecuteCommand": false,
		"overrides": map[string]any{"containerOverrides": []map[string]any{{"name": e.c.ECS.Container, "command": command}}},
		"tags":      []ecsTag{{Key: "hikyo.dev/security-maintenance-owner", Value: e.c.ECS.OwnerTag}, {Key: "hikyo.dev/security-job", Value: nonce}}}
	var launched struct {
		Tasks    []ecsTask         `json:"tasks"`
		Failures []json.RawMessage `json:"failures"`
	}
	if err := e.aws(ctx, "ecs", "run-task", input, &launched); err != nil {
		return observed, err
	}
	if len(launched.Tasks) != 1 || len(launched.Failures) != 0 || !e.ownedECSJob(launched.Tasks[0], intent) {
		return observed, ErrConflict
	}
	intent.TaskARN = launched.Tasks[0].ARN
	if err := e.saveECSJob(intent); err != nil {
		return observed, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		returnErr = errors.Join(returnErr, e.cleanupECSJob(cleanup, intent))
	}()
	var finished ecsTask
	if err := e.waitECS(ctx, func(wait context.Context) (bool, error) {
		tasks, err := e.ecsTasks(wait, []string{intent.TaskARN})
		if err != nil {
			return false, err
		}
		if len(tasks) != 1 || !e.ownedECSJob(tasks[0], intent) {
			return false, ErrConflict
		}
		finished = tasks[0]
		return finished.LastStatus == "STOPPED", nil
	}); err != nil {
		return observed, err
	}
	if finished.Containers[0].ExitCode == nil || *finished.Containers[0].ExitCode != 0 || finished.Containers[0].Image != proof.Container.Image || finished.Containers[0].Digest != "sha256:"+runtimeDigest {
		return observed, ecsFailure("preparation/inspection process or runtime image")
	}
	return e.ecsJobObservation(ctx, intent, finished)
}

func (e *Executor) ecsJobObservation(ctx context.Context, intent ecsJobIntent, finished ecsTask) (Observation, error) {
	var observed Observation
	stopped, err := parseAWSDate(finished.StoppedAt)
	if err != nil {
		return observed, ecsFailure("job completion timestamp")
	}
	taskID := intent.TaskARN[strings.LastIndexByte(intent.TaskARN, '/')+1:]
	if !ecsTaskIDPattern.MatchString(taskID) {
		return observed, ErrInvalid
	}
	stream := e.c.ECS.LogPrefix + "/" + e.c.ECS.Container + "/" + taskID
	logs, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempts := 0; attempts < 30; attempts++ {
		var events struct {
			Events []struct {
				Timestamp int64  `json:"timestamp"`
				Message   string `json:"message"`
			} `json:"events"`
		}
		err := e.aws(logs, "logs", "get-log-events", map[string]any{"logGroupName": e.c.ECS.LogGroup, "logStreamName": stream, "startTime": intent.CreatedAt.Add(-time.Second).UnixMilli(), "endTime": stopped.Add(time.Second).UnixMilli(), "startFromHead": true, "limit": 2}, &events)
		if err == nil && len(events.Events) > 0 {
			if len(events.Events) != 1 || events.Events[0].Timestamp < intent.CreatedAt.Add(-time.Second).UnixMilli() || events.Events[0].Timestamp > stopped.Add(time.Second).UnixMilli() || decode([]byte(events.Events[0].Message), &observed) != nil || !validObservation(observed) {
				return Observation{}, ecsFailure("structured task proof")
			}
			return observed, nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-logs.Done():
			timer.Stop()
			return Observation{}, ecsFailure("task log availability")
		case <-timer.C:
		}
	}
	return Observation{}, ecsFailure("task log availability")
}
