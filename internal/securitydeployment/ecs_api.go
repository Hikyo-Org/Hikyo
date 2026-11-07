package securitydeployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func (e *Executor) timeoutSeconds() int {
	if e.c.TimeoutSeconds == 0 {
		return 900
	}
	return e.c.TimeoutSeconds
}

func (e *Executor) aws(ctx context.Context, service, action string, input, output any) error {
	if service != "ecs" && service != "logs" && service != "application-autoscaling" && service != "elbv2" {
		return ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	endpoint := service
	if service == "elbv2" {
		endpoint = "elasticloadbalancing"
	}
	args := []string{"--profile", e.c.ECS.Profile, "--region", e.c.ECS.Region, "--endpoint-url", "https://" + endpoint + "." + e.c.ECS.Region + ".amazonaws.com", "--output", "json", "--no-cli-pager", "--no-cli-auto-prompt", "--no-paginate", "--cli-connect-timeout", "10", "--cli-read-timeout", "30", service, action, "--cli-input-json", "file:///dev/stdin"}
	data, err := e.run(ctx, e.c.ECS.AWS, args, raw)
	if err != nil && ecsDefiniteFailure(err) {
		return errors.Join(ecsFailure(action), errECSNoEffect, err)
	}
	if err != nil {
		return errors.Join(ecsFailure(action), err)
	}
	if output == nil {
		return nil
	}
	// AWS responses may add informational fields. Execution contracts below
	// explicitly validate every field that can affect the selected operation.
	if len(data) == 0 || len(data) > maxRecordBytes || definitions.RejectDuplicateMembers(data) != nil || json.Unmarshal(data, output) != nil {
		return ecsFailure("response decoding")
	}
	return nil
}

func (e *Executor) ecsService(ctx context.Context) (ecsService, error) {
	var response struct {
		Services []ecsService      `json:"services"`
		Failures []json.RawMessage `json:"failures"`
	}
	err := e.aws(ctx, "ecs", "describe-services", map[string]any{"cluster": e.c.ECS.ClusterARN, "services": []string{e.c.ECS.ServiceARN}, "include": []string{"TAGS"}}, &response)
	if err != nil {
		return ecsService{}, err
	}
	if len(response.Services) != 1 || len(response.Failures) != 0 {
		return ecsService{}, ErrConflict
	}
	return response.Services[0], nil
}

func (e *Executor) ecsDefinition(ctx context.Context, arn string) (ecsDefinition, error) {
	if !e.c.ECS.definitionARN(arn) {
		return ecsDefinition{}, ErrInvalid
	}
	var response struct {
		Definition map[string]json.RawMessage `json:"taskDefinition"`
		Tags       []ecsTag                   `json:"tags"`
	}
	if err := e.aws(ctx, "ecs", "describe-task-definition", map[string]any{"taskDefinition": arn, "include": []string{"TAGS"}}, &response); err != nil {
		if ecsDefinitionUnreadable.MatchString(err.Error()) {
			return ecsDefinition{}, errors.Join(err, errECSDefinitionRetired)
		}
		return ecsDefinition{}, err
	}
	var status, returned string
	if json.Unmarshal(response.Definition["status"], &status) == nil && status == "INACTIVE" && json.Unmarshal(response.Definition["taskDefinitionArn"], &returned) == nil && returned == arn {
		return ecsDefinition{}, errors.Join(ErrUnsupported, errECSDefinitionRetired, errECSDefinitionInactive)
	}
	definition, err := e.validateECSDefinition(response.Definition)
	if err != nil || definition.ARN != arn {
		return ecsDefinition{}, errors.Join(err, ErrUnsupported)
	}
	definition.Tags = response.Tags
	return definition, nil
}

// errECSDefinitionRetired marks a revision that reads INACTIVE or that ECS will no
// longer describe: DescribeTaskDefinition serves an INACTIVE revision only while
// something references it.
var errECSDefinitionRetired = errors.New("security deployment: ECS task definition is deregistered")

// errECSDefinitionInactive is a deregistration ECS reported, not one inferred from an unreadable revision.
var errECSDefinitionInactive = errors.New("security deployment: ECS task definition is INACTIVE")

var ecsDefinitionUnreadable = regexp.MustCompile(`(?s)exited with status 254\b.*\(ClientException\) when calling the DescribeTaskDefinition operation`)

type ecsTask struct {
	ARN           string          `json:"taskArn"`
	Cluster       string          `json:"clusterArn"`
	Definition    string          `json:"taskDefinitionArn"`
	StartedBy     string          `json:"startedBy"`
	Group         string          `json:"group"`
	LastStatus    string          `json:"lastStatus"`
	DesiredStatus string          `json:"desiredStatus"`
	Health        string          `json:"healthStatus"`
	StoppedAt     json.RawMessage `json:"stoppedAt"`
	Containers    []struct {
		Name       string `json:"name"`
		Image      string `json:"image"`
		Digest     string `json:"imageDigest"`
		LastStatus string `json:"lastStatus"`
		Health     string `json:"healthStatus"`
		ExitCode   *int   `json:"exitCode"`
		Interfaces []struct {
			PrivateIPv4 string `json:"privateIpv4Address"`
		} `json:"networkInterfaces"`
	} `json:"containers"`
}

func (e *Executor) ecsTasks(ctx context.Context, arns []string) ([]ecsTask, error) {
	if len(arns) == 0 {
		return []ecsTask{}, nil
	}
	if len(arns) > 100 {
		return nil, ErrUnsupported
	}
	for _, arn := range arns {
		if !e.c.ECS.taskARN(arn) {
			return nil, ErrInvalid
		}
	}
	var response struct {
		Tasks    []ecsTask `json:"tasks"`
		Failures []struct {
			ARN    string `json:"arn"`
			Reason string `json:"reason"`
		} `json:"failures"`
	}
	if err := e.aws(ctx, "ecs", "describe-tasks", map[string]any{"cluster": e.c.ECS.ClusterARN, "tasks": arns}, &response); err != nil {
		return nil, err
	}
	if len(response.Failures) != 0 || len(response.Tasks) != len(arns) {
		missing := len(response.Tasks)+len(response.Failures) == len(arns)
		for _, failure := range response.Failures {
			missing = missing && failure.Reason == "MISSING" && contains(arns, failure.ARN)
		}
		if missing {
			return nil, errors.Join(ErrConflict, errECSTaskMissing)
		}
		return nil, ErrConflict
	}
	seen := map[string]bool{}
	for _, task := range response.Tasks {
		if !e.c.ECS.taskARN(task.ARN) || task.Cluster != e.c.ECS.ClusterARN || seen[task.ARN] || !contains(arns, task.ARN) {
			return nil, ErrConflict
		}
		seen[task.ARN] = true
	}
	return response.Tasks, nil
}

// errECSTaskMissing marks requested tasks as absent from describe-tasks, which
// is how ECS reports a task it no longer retains and also one it has not yet indexed.
var errECSTaskMissing = errors.New("security deployment: ECS does not report the task")

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (e *Executor) ecsServiceTasks(ctx context.Context) ([]ecsTask, error) {
	var response struct {
		ARNs []string `json:"taskArns"`
		Next string   `json:"nextToken"`
	}
	if err := e.aws(ctx, "ecs", "list-tasks", map[string]any{"cluster": e.c.ECS.ClusterARN, "serviceName": e.c.ECS.serviceName(), "desiredStatus": "RUNNING", "maxResults": 100}, &response); err != nil {
		return nil, err
	}
	if response.Next != "" || len(response.ARNs) > 1 {
		return nil, ErrUnsupported
	}
	tasks, err := e.ecsTasks(ctx, response.ARNs)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if task.Group != "service:"+e.c.ECS.serviceName() {
			return nil, ErrConflict
		}
	}
	return tasks, nil
}

func (e *Executor) ecsNoAutoscaling(ctx context.Context) error {
	var response struct {
		Targets []json.RawMessage `json:"ScalableTargets"`
		Next    string            `json:"NextToken"`
	}
	input := map[string]any{"ServiceNamespace": "ecs", "ResourceIds": []string{"service/" + e.c.ECS.clusterName() + "/" + e.c.ECS.serviceName()}, "ScalableDimension": "ecs:service:DesiredCount", "MaxResults": 100}
	if err := e.aws(ctx, "application-autoscaling", "describe-scalable-targets", input, &response); err != nil {
		return err
	}
	if len(response.Targets) != 0 || response.Next != "" {
		return ErrUnsupported
	}
	return nil
}

func (e *Executor) ecsOnlyServiceWriter(ctx context.Context, sourceTask string) error {
	var response struct {
		ARNs []string `json:"taskArns"`
		Next string   `json:"nextToken"`
	}
	if err := e.aws(ctx, "ecs", "list-tasks", map[string]any{"cluster": e.c.ECS.ClusterARN, "family": e.c.ECS.TaskFamily, "desiredStatus": "RUNNING", "maxResults": 100}, &response); err != nil {
		return err
	}
	if response.Next != "" || len(response.ARNs) != 1 || response.ARNs[0] != sourceTask {
		return ErrUnsupported
	}
	// A task told to stop keeps its database access until it reaches STOPPED.
	// Stopped tasks stay listed for at least an hour, so page through them.
	token := ""
	for page := 0; page < 50; page++ {
		var stopping struct {
			ARNs []string `json:"taskArns"`
			Next string   `json:"nextToken"`
		}
		input := map[string]any{"cluster": e.c.ECS.ClusterARN, "family": e.c.ECS.TaskFamily, "desiredStatus": "STOPPED", "maxResults": 100}
		if token != "" {
			input["nextToken"] = token
		}
		if err := e.aws(ctx, "ecs", "list-tasks", input, &stopping); err != nil {
			return err
		}
		tasks, err := e.ecsTasks(ctx, stopping.ARNs)
		if errors.Is(err, errECSTaskMissing) {
			// Reaped after the listing, or not yet indexed: neither proves it stopped,
			// so a fresh listing decides.
			return ErrNotReady
		}
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.LastStatus != "STOPPED" {
				return ErrNotReady
			}
		}
		if stopping.Next == "" {
			return nil
		}
		token = stopping.Next
	}
	// Stopped tasks age out of the listing, so a family this busy qualifies later.
	return ErrNotReady
}

func (e *Executor) ecsReadiness(ctx context.Context, service ecsService, task ecsTask) error {
	c := e.c.ECS
	var groups struct {
		Groups []struct {
			ARN        string `json:"TargetGroupArn"`
			Protocol   string `json:"HealthCheckProtocol"`
			Path       string `json:"HealthCheckPath"`
			Port       string `json:"HealthCheckPort"`
			TargetType string `json:"TargetType"`
			Matcher    struct {
				HTTP string `json:"HttpCode"`
			} `json:"Matcher"`
		} `json:"TargetGroups"`
	}
	if err := e.aws(ctx, "elbv2", "describe-target-groups", map[string]any{"TargetGroupArns": []string{c.ReadinessTargetGroupARN}}, &groups); err != nil {
		return err
	}
	if len(groups.Groups) != 1 {
		return ErrUnsupported
	}
	group := groups.Groups[0]
	if group.ARN != c.ReadinessTargetGroupARN || group.Protocol != "HTTP" || group.Path != "/readyz" || group.Port != fmt.Sprint(c.ReadinessPort) || group.TargetType != "ip" || group.Matcher.HTTP != "200" {
		return ErrUnsupported
	}
	if len(task.Containers) != 1 || task.Containers[0].Name != c.Container || len(task.Containers[0].Interfaces) != 1 {
		return ErrUnsupported
	}
	address := task.Containers[0].Interfaces[0].PrivateIPv4
	if address == "" {
		return ErrUnsupported
	}
	var health struct {
		Targets []struct {
			Target struct {
				ID   string `json:"Id"`
				Port int    `json:"Port"`
			} `json:"Target"`
			Health struct {
				State string `json:"State"`
			} `json:"TargetHealth"`
		} `json:"TargetHealthDescriptions"`
	}
	port := service.LoadBalancers[0].Port
	if err := e.aws(ctx, "elbv2", "describe-target-health", map[string]any{"TargetGroupArn": c.ReadinessTargetGroupARN, "Targets": []map[string]any{{"Id": address, "Port": port}}}, &health); err != nil {
		return err
	}
	if len(health.Targets) != 1 || health.Targets[0].Target.ID != address || health.Targets[0].Target.Port != port || health.Targets[0].Health.State != "healthy" {
		return ErrNotReady
	}
	return nil
}

type ecsSnapshot struct {
	Service    ecsService                 `json:"service"`
	Definition map[string]json.RawMessage `json:"definition"`
	SourceTask string                     `json:"source_task"`
	Source     Observation                `json:"source"`
}

func (e *Executor) ecsCurrent(ctx context.Context, inspect bool) (ecsSnapshot, error) {
	var snapshot ecsSnapshot
	// A leftover job task still running fails the sole-writer check below, so it
	// must be settled first or it would pin Observe until it exits on its own.
	if inspect {
		if err := e.recoverECSJob(ctx); err != nil {
			return snapshot, err
		}
	}
	service, err := e.ecsService(ctx)
	if err != nil {
		return snapshot, err
	}
	if err := e.validateECSService(service, 1); err != nil {
		return snapshot, err
	}
	if service.Running != 1 || service.Pending != 0 || len(service.Deployments) != 1 || service.Deployments[0].Status != "PRIMARY" || service.Deployments[0].TaskDefinition != service.TaskDefinition || service.Deployments[0].Rollout != "COMPLETED" || service.Deployments[0].Running != 1 || service.Deployments[0].Pending != 0 || service.Deployments[0].Desired != 1 {
		return snapshot, ErrNotReady
	}
	if err := e.ecsNoAutoscaling(ctx); err != nil {
		return snapshot, err
	}
	definition, err := e.ecsDefinition(ctx, service.TaskDefinition)
	if err != nil {
		return snapshot, err
	}
	tasks, err := e.ecsServiceTasks(ctx)
	if err != nil {
		return snapshot, err
	}
	if len(tasks) != 1 {
		return snapshot, ErrNotReady
	}
	task := tasks[0]
	if task.Definition != service.TaskDefinition || task.LastStatus != "RUNNING" || task.DesiredStatus != "RUNNING" || task.Health != "HEALTHY" || len(task.Containers) != 1 {
		return snapshot, ErrNotReady
	}
	if err := e.ecsOnlyServiceWriter(ctx, task.ARN); err != nil {
		return snapshot, err
	}
	container := task.Containers[0]
	if container.Name != e.c.ECS.Container || container.Image != definition.Container.Image || container.LastStatus != "RUNNING" || container.Health != "HEALTHY" || !strings.HasPrefix(container.Digest, "sha256:") || releaseidentity.Digest(strings.TrimPrefix(container.Digest, "sha256:")).Validate() != nil {
		return snapshot, ErrNotReady
	}
	if err := e.ecsReadiness(ctx, service, task); err != nil {
		return snapshot, err
	}
	snapshot = ecsSnapshot{Service: service, Definition: definition.Registration, SourceTask: task.ARN}
	if inspect {
		observed, err := e.ecsJob(ctx, service, definition.ARN, strings.TrimPrefix(container.Digest, "sha256:"), "inspect", []string{"security-maintenance", "inspect", "--database-only"})
		if err != nil {
			return snapshot, err
		}
		if !validObservation(observed) || observed.Engine != releaseidentity.Postgres || observed.Maintenance || observed.Phase != "healthy" || observed.Ready || observed.StagedTarget != (releaseidentity.Identity{}) {
			return snapshot, ErrNotReady
		}
		observed.Image, observed.RuntimeImageSHA256, observed.Ready = container.Image, strings.TrimPrefix(container.Digest, "sha256:"), true
		snapshot.Source = observed
		// Inspect jobs can take time. Recheck the exact service task after the
		// DB/schema observation, instead of combining observations across a roll.
		after, err := e.ecsCurrent(ctx, false)
		if err != nil {
			return snapshot, err
		}
		beforeHash, _ := ecsSnapshotDigest(snapshot)
		after.Source = observed
		afterHash, _ := ecsSnapshotDigest(after)
		if beforeHash != afterHash {
			return snapshot, ecsSourceIdentityError()
		}
	}
	return snapshot, nil
}

func ecsSnapshotDigest(snapshot ecsSnapshot) (releaseidentity.Digest, error) {
	contract, err := ecsServiceContractDigest(snapshot.Service)
	if err != nil {
		return "", err
	}
	definition, err := ecsRegistrationDigest(snapshot.Definition)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Contract, Definition releaseidentity.Digest
		Task                 string
		Source               Observation
	}{contract, definition, snapshot.SourceTask, snapshot.Source})
	if err != nil {
		return "", err
	}
	return releaseidentity.Hash(raw), nil
}

func (e *Executor) waitECS(ctx context.Context, check func(context.Context) (bool, error)) error {
	deadline, cancel := context.WithTimeout(ctx, time.Duration(e.timeoutSeconds())*time.Second)
	defer cancel()
	for attempts := 0; attempts < e.timeoutSeconds()+1; attempts++ {
		done, err := check(deadline)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-deadline.Done():
			timer.Stop()
			return errors.Join(ErrNotReady, deadline.Err())
		case <-timer.C:
		}
	}
	return ErrNotReady
}

func ecsNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// errECSNoEffect marks a CLI failure that sent nothing (252, 253, no process). Any
// other failure, even a service refusal, can be the visible end of an internal retry
// whose first attempt committed, so its operation record stays until a read settles it.
var errECSNoEffect = errors.New("security deployment: ECS request had no effect")

var ecsNeverSent = regexp.MustCompile(`^security deployment: installed command (could not start|exited with status 25[23]\b)`)

func ecsDefiniteFailure(err error) bool { return ecsNeverSent.MatchString(err.Error()) }
