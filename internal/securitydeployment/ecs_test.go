//go:build darwin || linux

package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const ecsTestAccount = "123456789012"
const ecsTestRegion = "eu-west-1"
const ecsTestPrefix = "arn:aws:ecs:" + ecsTestRegion + ":" + ecsTestAccount + ":"

func ecsTestIdentity(version string, sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat-" + version)), ManifestSHA256: releaseidentity.Hash([]byte("manifest-" + version))}
}

func ecsTestConfig(t *testing.T) Config {
	t.Helper()
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	return Config{Kind: ECS, StateDirectory: state, ImageRepository: "ghcr.io/hikyo-org/hikyo", TimeoutSeconds: 10, ECS: &ECSConfig{
		AWS: "/usr/bin/aws", Profile: "customer", Region: ecsTestRegion, ClusterARN: ecsTestPrefix + "cluster/hikyo", ServiceARN: ecsTestPrefix + "service/hikyo/server", ServiceCreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Container: "server", Binary: "/usr/local/bin/hikyo", TaskFamily: "hikyo", TaskRoleARN: "arn:aws:iam::" + ecsTestAccount + ":role/hikyo-task", ExecutionRoleARN: "arn:aws:iam::" + ecsTestAccount + ":role/hikyo-execution",
		DatabaseSecretARN: "arn:aws:secretsmanager:" + ecsTestRegion + ":" + ecsTestAccount + ":secret:hikyo-production-abcdef", ScratchSecretARN: "arn:aws:secretsmanager:" + ecsTestRegion + ":" + ecsTestAccount + ":secret:hikyo-scratch-abcdef",
		UpgradeVolume: "upgrade", UpgradeFileSystemID: "fs-11111111", UpgradeAccessPointID: "fsap-11111111", UpgradeMountPath: "/var/lib/hikyo-upgrade",
		RootVolume: "root", RootFileSystemID: "fs-11111111", RootAccessPointID: "fsap-22222222", RootMountPath: "/run/hikyo-root", TemporaryVolume: "temporary",
		LogGroup: "/customer/hikyo", LogPrefix: "maintenance", ReadinessTargetGroupARN: "arn:aws:elasticloadbalancing:" + ecsTestRegion + ":" + ecsTestAccount + ":targetgroup/hikyo/0123456789abcdef", ReadinessPort: 8081,
		OwnerTag: "customer-hikyo", ExclusiveServiceOwner: true,
	}}
}

func ecsRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func ecsTestDefinition(c Config, arn, image string) map[string]json.RawMessage {
	p := c.ECS
	cpu := "X86_64"
	if runtime.GOARCH == "arm64" {
		cpu = "ARM64"
	}
	environment := []map[string]string{{"name": "HIKYO_UPGRADE_UNATTENDED", "value": "true"}, {"name": "HIKYO_AUTO_MIGRATE", "value": "true"}, {"name": "HIKYO_HA", "value": "false"},
		{"name": "HIKYO_UPGRADE_STATE_DIR", "value": p.UpgradeMountPath}, {"name": "HIKYO_ROOT_KEY_FILE", "value": p.RootMountPath + "/root-key"}, {"name": "HIKYO_OPERATIONAL_LISTEN", "value": "0.0.0.0:8081"}, {"name": "HIKYO_EXTERNAL_ORIGIN", "value": "https://secrets.example.org"}}
	container := map[string]any{"name": p.Container, "image": image, "essential": true, "entryPoint": []string{p.Binary}, "command": []string{"server"}, "user": "65532:65532", "readonlyRootFilesystem": true, "stopTimeout": 30,
		"environment": environment, "secrets": []map[string]string{{"name": "HIKYO_DB", "valueFrom": p.DatabaseSecretARN}, {"name": "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", "valueFrom": p.ScratchSecretARN}},
		"mountPoints":  []map[string]any{{"sourceVolume": p.UpgradeVolume, "containerPath": p.UpgradeMountPath, "readOnly": false}, {"sourceVolume": p.RootVolume, "containerPath": p.RootMountPath, "readOnly": true}, {"sourceVolume": p.TemporaryVolume, "containerPath": "/tmp", "readOnly": false}},
		"portMappings": []map[string]any{{"containerPort": 8080, "protocol": "tcp"}}, "healthCheck": map[string]any{"command": []string{"CMD", p.Binary, "security-maintenance", "health", "--liveness"}, "interval": 10, "timeout": 5, "retries": 3},
		"logConfiguration": map[string]any{"logDriver": "awslogs", "options": map[string]string{"awslogs-region": p.Region, "awslogs-group": p.LogGroup, "awslogs-stream-prefix": p.LogPrefix, "mode": "blocking"}},
		"linuxParameters":  map[string]any{"capabilities": map[string]any{"drop": []string{"ALL"}}}}
	volume := func(name, fs, ap string) map[string]any {
		return map[string]any{"name": name, "efsVolumeConfiguration": map[string]any{"fileSystemId": fs, "rootDirectory": "/", "transitEncryption": "ENABLED", "authorizationConfig": map[string]string{"accessPointId": ap, "iam": "ENABLED"}}}
	}
	return map[string]json.RawMessage{"taskDefinitionArn": ecsRaw(arn), "status": ecsRaw("ACTIVE"), "revision": ecsRaw(1), "family": ecsRaw(p.TaskFamily), "taskRoleArn": ecsRaw(p.TaskRoleARN), "executionRoleArn": ecsRaw(p.ExecutionRoleARN), "networkMode": ecsRaw("awsvpc"),
		"requiresCompatibilities": ecsRaw([]string{"FARGATE"}), "cpu": ecsRaw("256"), "memory": ecsRaw("512"), "runtimePlatform": ecsRaw(map[string]string{"cpuArchitecture": cpu, "operatingSystemFamily": "LINUX"}),
		"containerDefinitions": ecsRaw([]any{container}), "volumes": ecsRaw([]any{volume(p.UpgradeVolume, p.UpgradeFileSystemID, p.UpgradeAccessPointID), volume(p.RootVolume, p.RootFileSystemID, p.RootAccessPointID), map[string]string{"name": p.TemporaryVolume}})}
}

type ecsFakeRunner struct {
	t                 *testing.T
	config            Config
	service           ecsService
	definitions       map[string]map[string]json.RawMessage
	tasks             map[string]ecsTask
	logs              map[string]Observation
	logTimes          map[string]int64
	database          Observation
	target            Target
	commands          []string
	updates           []int
	registered        int
	deregistered      []string
	jobs              int
	autoscaling       bool
	readiness         bool
	badLogs           bool
	badRuntime        bool
	driftStage        bool
	ambiguousStop     bool
	wrongRegistration bool
	serviceMutation   func()
}

func ecsFixture(t *testing.T) (*Executor, *ecsFakeRunner) {
	t.Helper()
	c := ecsTestConfig(t)
	p := c.ECS
	sourceImage := c.ImageRepository + "@sha256:" + strings.Repeat("1", 64)
	target := Target{Identity: ecsTestIdentity("1.0.1", 2), Image: c.ImageRepository + "@sha256:" + strings.Repeat("2", 64), RuntimeImageSHA256: strings.Repeat("4", 64), EligibilityUntil: time.Now().Add(time.Hour)}
	f := &ecsFakeRunner{t: t, config: c, definitions: map[string]map[string]json.RawMessage{}, tasks: map[string]ecsTask{}, logs: map[string]Observation{}, logTimes: map[string]int64{}, target: target, readiness: true,
		database: Observation{Platform: runtime.GOOS + "/" + runtime.GOARCH, Engine: releaseidentity.Postgres, Identity: ecsTestIdentity("1.0.0", 1), Instance: "customer-installation", Generation: 3, RestoreEpoch: 1, Incarnation: strings.Repeat("b", 64), Phase: "healthy", Maintenance: false, Ready: false}}
	f.service.ARN, f.service.Name, f.service.ClusterARN, f.service.Status = p.ServiceARN, p.serviceName(), p.ClusterARN, "ACTIVE"
	f.service.CreatedAt = ecsRaw(p.ServiceCreatedAt)
	f.service.TaskDefinition = ecsTestPrefix + "task-definition/hikyo:1"
	f.service.LaunchType, f.service.PlatformVersion, f.service.PlatformFamily, f.service.Scheduling = "FARGATE", "1.4.0", "Linux", "REPLICA"
	f.service.Desired, f.service.Running, f.service.Pending, f.service.HealthGrace, f.service.Rebalancing = 1, 1, 0, 70, "DISABLED"
	f.service.Controller.Type = "ECS"
	f.service.Configuration.Minimum = 0
	f.service.Configuration.Maximum = 100
	f.service.Network = ecsRaw(map[string]any{"awsvpcConfiguration": map[string]any{"subnets": []string{"subnet-11111111"}, "securityGroups": []string{"sg-11111111"}, "assignPublicIp": "DISABLED"}})
	f.service.Tags = []ecsTag{{Key: "hikyo.dev/security-maintenance-owner", Value: p.OwnerTag}}
	f.service.LoadBalancers = append(f.service.LoadBalancers, struct {
		Group     string `json:"targetGroupArn"`
		Container string `json:"containerName"`
		Port      int    `json:"containerPort"`
	}{p.ReadinessTargetGroupARN, p.Container, 8080})
	f.finishDeployment()
	f.definitions[f.service.TaskDefinition] = ecsTestDefinition(c, f.service.TaskDefinition, sourceImage)
	task := f.serviceTask(ecsTestPrefix+"task/hikyo/"+strings.Repeat("a", 32), f.service.TaskDefinition, sourceImage, strings.Repeat("3", 64))
	f.tasks[task.ARN] = task
	e, err := NewWithDependencies(c, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, f
}

func (f *ecsFakeRunner) finishDeployment() {
	f.service.Deployments = nil
	var deployment struct {
		Status         string `json:"status"`
		TaskDefinition string `json:"taskDefinition"`
		Rollout        string `json:"rolloutState"`
		Desired        int    `json:"desiredCount"`
		Running        int    `json:"runningCount"`
		Pending        int    `json:"pendingCount"`
	}
	deployment.Status, deployment.TaskDefinition, deployment.Rollout = "PRIMARY", f.service.TaskDefinition, "COMPLETED"
	deployment.Desired, deployment.Running, deployment.Pending = f.service.Desired, f.service.Running, f.service.Pending
	f.service.Deployments = append(f.service.Deployments, deployment)
}

func (f *ecsFakeRunner) serviceTask(arn, definition, image, digest string) ecsTask {
	var task ecsTask
	task.ARN, task.Cluster, task.Definition, task.Group = arn, f.config.ECS.ClusterARN, definition, "service:"+f.config.ECS.serviceName()
	task.LastStatus, task.DesiredStatus, task.Health = "RUNNING", "RUNNING", "HEALTHY"
	var container struct {
		Name       string `json:"name"`
		Image      string `json:"image"`
		Digest     string `json:"imageDigest"`
		LastStatus string `json:"lastStatus"`
		Health     string `json:"healthStatus"`
		ExitCode   *int   `json:"exitCode"`
		Interfaces []struct {
			PrivateIPv4 string `json:"privateIpv4Address"`
		} `json:"networkInterfaces"`
	}
	container.Name, container.Image, container.Digest, container.LastStatus, container.Health = f.config.ECS.Container, image, "sha256:"+digest, "RUNNING", "HEALTHY"
	container.Interfaces = append(container.Interfaces, struct {
		PrivateIPv4 string `json:"privateIpv4Address"`
	}{"10.0.0.10"})
	task.Containers = append(task.Containers, container)
	return task
}

func ecsInputString(input map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(input[key], &value)
	return value
}

func (f *ecsFakeRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if command.Path != f.config.ECS.AWS || len(command.Args) < 4 || command.Args[len(command.Args)-2] != "--cli-input-json" || command.Args[len(command.Args)-1] != "file:///dev/stdin" {
		return nil, errors.New("unexpected installed CLI invocation")
	}
	service, action := command.Args[len(command.Args)-4], command.Args[len(command.Args)-3]
	f.commands = append(f.commands, service+"/"+action)
	var input map[string]json.RawMessage
	if json.Unmarshal(command.Input, &input) != nil {
		return nil, errors.New("invalid CLI stdin JSON")
	}
	respond := func(value any) ([]byte, error) { return json.Marshal(value) }
	switch service + "/" + action {
	case "ecs/describe-services":
		return respond(map[string]any{"services": []ecsService{f.service}, "failures": []any{}})
	case "ecs/describe-task-definition":
		arn := ecsInputString(input, "taskDefinition")
		definition, ok := f.definitions[arn]
		if !ok {
			return nil, errors.New("unknown task definition")
		}
		return respond(map[string]any{"taskDefinition": definition, "tags": []any{}})
	case "ecs/list-tasks":
		started := ecsInputString(input, "startedBy")
		family := ecsInputString(input, "family")
		arns := []string{}
		for arn, task := range f.tasks {
			if started != "" {
				if task.StartedBy == started && task.LastStatus != "STOPPED" {
					arns = append(arns, arn)
				}
			} else if family != "" && strings.HasPrefix(task.Definition, ecsTestPrefix+"task-definition/"+family+":") && task.DesiredStatus == "RUNNING" {
				arns = append(arns, arn)
			} else if family == "" && task.Group == "service:"+f.config.ECS.serviceName() && task.DesiredStatus == "RUNNING" {
				arns = append(arns, arn)
			}
		}
		return respond(map[string]any{"taskArns": arns})
	case "ecs/describe-tasks":
		var arns []string
		_ = json.Unmarshal(input["tasks"], &arns)
		tasks := []ecsTask{}
		for _, arn := range arns {
			task, ok := f.tasks[arn]
			if !ok {
				return nil, errors.New("unknown task")
			}
			tasks = append(tasks, task)
		}
		return respond(map[string]any{"tasks": tasks, "failures": []any{}})
	case "application-autoscaling/describe-scalable-targets":
		targets := []any{}
		if f.autoscaling {
			targets = append(targets, map[string]string{"ResourceId": "service/hikyo/server"})
		}
		return respond(map[string]any{"ScalableTargets": targets})
	case "elbv2/describe-target-groups":
		return respond(map[string]any{"TargetGroups": []any{map[string]any{"TargetGroupArn": f.config.ECS.ReadinessTargetGroupARN, "HealthCheckProtocol": "HTTP", "HealthCheckPath": "/readyz", "HealthCheckPort": "8081", "TargetType": "ip", "Matcher": map[string]string{"HttpCode": "200"}}}})
	case "elbv2/describe-target-health":
		state := "healthy"
		if !f.readiness {
			state = "unhealthy"
		}
		return respond(map[string]any{"TargetHealthDescriptions": []any{map[string]any{"Target": map[string]any{"Id": "10.0.0.10", "Port": 8080}, "TargetHealth": map[string]string{"State": state}}}})
	case "ecs/register-task-definition":
		f.registered++
		arn := ecsTestPrefix + fmt.Sprintf("task-definition/hikyo:%d", f.registered+1)
		definition := map[string]json.RawMessage{}
		for key, value := range input {
			if key != "tags" {
				definition[key] = value
			}
		}
		definition["taskDefinitionArn"], definition["revision"], definition["status"] = ecsRaw(arn), ecsRaw(f.registered+1), ecsRaw("ACTIVE")
		if f.wrongRegistration {
			definition["taskRoleArn"] = ecsRaw("arn:aws:iam::" + ecsTestAccount + ":role/other")
		}
		f.definitions[arn] = definition
		return respond(map[string]any{"taskDefinition": definition})
	case "ecs/deregister-task-definition":
		arn := ecsInputString(input, "taskDefinition")
		definition, ok := f.definitions[arn]
		if !ok {
			return nil, errors.New("unknown deregistration")
		}
		definition["status"] = ecsRaw("INACTIVE")
		f.deregistered = append(f.deregistered, arn)
		return respond(map[string]any{"taskDefinition": definition})
	case "ecs/run-task":
		f.jobs++
		definition := ecsInputString(input, "taskDefinition")
		raw := f.definitions[definition]
		var containers []ecsContainerDefinition
		_ = json.Unmarshal(raw["containerDefinitions"], &containers)
		var overrides struct {
			Containers []struct {
				Name    string   `json:"name"`
				Command []string `json:"command"`
			} `json:"containerOverrides"`
		}
		_ = json.Unmarshal(input["overrides"], &overrides)
		if len(overrides.Containers) != 1 || overrides.Containers[0].Name != f.config.ECS.Container {
			return nil, errors.New("unexpected task overrides")
		}
		args := overrides.Containers[0].Command
		isStage := len(args) == 4 && args[0] == "security-maintenance" && args[1] == "stage" && args[2] == "--expected-manifest" && args[3] == string(f.target.Identity.ManifestSHA256)
		if !isStage && !reflect.DeepEqual(args, []string{"security-maintenance", "inspect", "--database-only"}) {
			return nil, errors.New("arbitrary task command")
		}
		arn := ecsTestPrefix + fmt.Sprintf("task/hikyo/%032x", f.jobs)
		digest := strings.Repeat("3", 64)
		if containers[0].Image == f.target.Image {
			digest = f.target.RuntimeImageSHA256
		}
		task := f.serviceTask(arn, definition, containers[0].Image, digest)
		task.StartedBy, task.Group = ecsInputString(input, "startedBy"), ecsInputString(input, "group")
		task.LastStatus, task.DesiredStatus, task.Health = "STOPPED", "STOPPED", "UNKNOWN"
		zero := 0
		task.Containers[0].ExitCode = &zero
		task.Containers[0].LastStatus = "STOPPED"
		task.StoppedAt = ecsRaw(time.Now().UTC())
		if f.badRuntime && isStage {
			task.Containers[0].Digest = "sha256:" + strings.Repeat("9", 64)
		}
		observation := f.database
		observation.Ready = false
		if isStage {
			observation.StagedTarget = f.target.Identity
			observation.RouteSHA256 = releaseidentity.Hash([]byte("actual staged route"))
			observation.SchemaChanges = true
			observation.RequiresMaintenance = true
			observation.Phase = "staged"
			if f.driftStage {
				f.database.Generation++
			}
		}
		f.tasks[arn] = task
		f.logs[arn[strings.LastIndexByte(arn, '/')+1:]] = observation
		f.logTimes[arn[strings.LastIndexByte(arn, '/')+1:]] = time.Now().UnixMilli()
		return respond(map[string]any{"tasks": []ecsTask{task}, "failures": []any{}})
	case "logs/get-log-events":
		stream := ecsInputString(input, "logStreamName")
		id := stream[strings.LastIndexByte(stream, '/')+1:]
		observation, ok := f.logs[id]
		if !ok {
			return nil, errors.New("foreign log stream")
		}
		events := []any{map[string]any{"timestamp": f.logTimes[id], "message": string(ecsRaw(observation))}}
		if f.badLogs {
			events = append(events, map[string]any{"timestamp": f.logTimes[id], "message": "unexpected second result"})
		}
		return respond(map[string]any{"events": events, "nextForwardToken": "fixed"})
	case "ecs/stop-task":
		arn := ecsInputString(input, "task")
		task, ok := f.tasks[arn]
		if !ok {
			return nil, errors.New("unknown stop")
		}
		if task.Group == "service:"+f.config.ECS.serviceName() {
			return nil, errors.New("must stop service through desired state")
		}
		task.LastStatus = "STOPPED"
		task.DesiredStatus = "STOPPED"
		f.tasks[arn] = task
		return respond(map[string]any{"task": task})
	case "ecs/update-service":
		var desired int
		_ = json.Unmarshal(input["desiredCount"], &desired)
		f.updates = append(f.updates, desired)
		if desired == 0 {
			for arn, task := range f.tasks {
				if task.Group == "service:"+f.config.ECS.serviceName() {
					task.LastStatus = "STOPPED"
					task.DesiredStatus = "STOPPED"
					f.tasks[arn] = task
				}
			}
			f.service.Desired, f.service.Running, f.service.Pending = 0, 0, 0
			f.finishDeployment()
			if f.ambiguousStop {
				return nil, errors.New("lost stop response")
			}
		} else {
			for _, task := range f.tasks {
				if task.Group == "service:"+f.config.ECS.serviceName() && task.LastStatus != "STOPPED" {
					return nil, errors.New("overlapping old writer")
				}
			}
			definition := ecsInputString(input, "taskDefinition")
			f.service.TaskDefinition = definition
			f.service.Desired, f.service.Running, f.service.Pending = 1, 1, 0
			f.finishDeployment()
			task := f.serviceTask(ecsTestPrefix+"task/hikyo/"+strings.Repeat("c", 32), definition, f.target.Image, f.target.RuntimeImageSHA256)
			f.tasks[task.ARN] = task
			f.database.Identity = f.target.Identity
			f.database.Generation++
		}
		if f.serviceMutation != nil {
			f.serviceMutation()
		}
		return respond(map[string]any{"service": f.service})
	default:
		return nil, fmt.Errorf("unexpected fake API action %s/%s", service, action)
	}
}

func TestECSPrepareReplacesOnlyImageAndStagesBeforeFullStop(t *testing.T) {
	e, f := ecsFixture(t)
	observed, err := e.Observe(t.Context())
	if err != nil || !observed.Ready || observed.Engine != releaseidentity.Postgres {
		t.Fatalf("source observation %+v %v", observed, err)
	}
	staged, err := e.Prepare(t.Context(), f.target)
	if err != nil {
		t.Fatal(err)
	}
	if staged.StagedTarget != f.target.Identity || staged.Identity != f.database.Identity || staged.Ready || len(f.updates) != 0 {
		t.Fatalf("invalid staging: %+v updates=%v", staged, f.updates)
	}
	if _, err := e.Prepare(t.Context(), Target{Identity: f.target.Identity, Image: f.target.Image, RuntimeImageSHA256: f.target.RuntimeImageSHA256}); err != nil {
		t.Fatal("same immutable target re-preparation", err)
	}
	if f.registered != 1 {
		t.Fatal("re-preparation created a second target revision")
	}
	before := f.definitions[ecsTestPrefix+"task-definition/hikyo:1"]
	after := f.definitions[ecsTestPrefix+"task-definition/hikyo:2"]
	sourceDefinition, err := e.validateECSDefinition(before)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := sourceDefinition.withImage(f.target.Image)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := e.validateECSDefinition(after)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registered.Registration, expected) {
		t.Fatal("registration changed execution fields besides image")
	}
	if err := e.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.updates, []int{0, 1}) {
		t.Fatalf("missing full stop %v", f.updates)
	}
	running, err := e.Observe(t.Context())
	if err != nil || running.Identity != f.target.Identity || running.RuntimeImageSHA256 != f.target.RuntimeImageSHA256 || !running.Ready || running.Generation != 4 {
		t.Fatalf("running proof %+v %v", running, err)
	}
	j, err := e.journal()
	if err != nil || j.Phase != "healthy" {
		t.Fatalf("outcome not durable %+v %v", j, err)
	}
	for _, task := range f.tasks {
		if strings.HasPrefix(task.Group, "hikyo-security:") && task.LastStatus != "STOPPED" {
			t.Fatal("owned preparation task retained")
		}
	}
	if _, err := os.Stat(e.ecsJobPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed job intent retained", err)
	}
}

func TestECSContractRejectsUnqualifiedTopologyAndOwners(t *testing.T) {
	for _, name := range []string{"no exclusive owner", "different creation", "cloudformation", "autoscaling", "rollback", "extra replica", "unmanaged writer", "public task", "ephemeral state", "root writable", "readiness liveness", "different task role", "mutable source"} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			switch name {
			case "no exclusive owner":
				e.c.ECS.ExclusiveServiceOwner = false
			case "different creation":
				f.service.CreatedAt = ecsRaw(e.c.ECS.ServiceCreatedAt.Add(time.Hour))
			case "cloudformation":
				f.service.Tags = append(f.service.Tags, ecsTag{Key: "aws:cloudformation:stack-id", Value: "stack"})
			case "autoscaling":
				f.autoscaling = true
			case "rollback":
				f.service.Configuration.Circuit.Rollback = true
			case "extra replica":
				f.service.Desired = 2
			case "unmanaged writer":
				for _, source := range f.tasks {
					extra := source
					extra.ARN = ecsTestPrefix + "task/hikyo/" + strings.Repeat("d", 32)
					extra.Group = "independent-run-task"
					f.tasks[extra.ARN] = extra
					break
				}
			case "public task":
				f.service.Network = ecsRaw(map[string]any{"awsvpcConfiguration": map[string]any{"subnets": []string{"subnet-11111111"}, "securityGroups": []string{"sg-11111111"}, "assignPublicIp": "ENABLED"}})
			default:
				definition := f.definitions[f.service.TaskDefinition]
				var containers []map[string]any
				_ = json.Unmarshal(definition["containerDefinitions"], &containers)
				switch name {
				case "ephemeral state":
					definition["volumes"] = ecsRaw([]map[string]string{{"name": e.c.ECS.UpgradeVolume}})
				case "root writable":
					mounts := containers[0]["mountPoints"].([]any)
					mounts[1].(map[string]any)["readOnly"] = false
				case "readiness liveness":
					containers[0]["healthCheck"].(map[string]any)["command"] = []string{"CMD", e.c.ECS.Binary, "security-maintenance", "health"}
				case "different task role":
					definition["taskRoleArn"] = ecsRaw("arn:aws:iam::" + ecsTestAccount + ":role/other")
				case "mutable source":
					containers[0]["image"] = "ghcr.io/hikyo-org/hikyo:latest"
				}
				definition["containerDefinitions"] = ecsRaw(containers)
			}
			if err := Validate(e.c); name == "no exclusive owner" {
				if err == nil {
					t.Fatal("unowned service accepted")
				}
				return
			}
			if _, err := e.Prepare(t.Context(), f.target); err == nil {
				t.Fatal("unqualified service accepted")
			}
			if len(f.updates) != 0 {
				t.Fatal("qualification failure changed service")
			}
		})
	}
}

func TestECSRefusesSourceDriftRuntimeAndUnscopedTaskProof(t *testing.T) {
	for _, name := range []string{"source generation", "runtime digest", "multiple results", "registration substitution"} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			switch name {
			case "source generation":
				f.driftStage = true
			case "runtime digest":
				f.badRuntime = true
			case "multiple results":
				f.badLogs = true
			case "registration substitution":
				f.wrongRegistration = true
			}
			if _, err := e.Prepare(t.Context(), f.target); err == nil {
				t.Fatal("invalid source/artifact proof accepted")
			}
			if len(f.updates) != 0 {
				t.Fatal("bad preparation stopped source")
			}
		})
	}
}

func TestECSApplyRechecksDesiredSourceAndEligibilityBeforeMutation(t *testing.T) {
	for _, name := range []string{"desired changed", "restore changed", "expired"} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			if _, err := e.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "desired changed":
				f.service.Tags = append(f.service.Tags, ecsTag{Key: "customer-change", Value: "new"})
			case "restore changed":
				f.database.RestoreEpoch++
			case "expired":
				f.target.EligibilityUntil = time.Now().Add(-time.Second)
			}
			if err := e.Apply(t.Context(), f.target); err == nil {
				t.Fatal("unsafe replacement accepted")
			}
			if len(f.updates) != 0 {
				t.Fatal("preapply refusal mutated service")
			}
		})
	}
}

func TestECSAmbiguousStopRetainsExactOperationWithoutOldImageRollback(t *testing.T) {
	e, f := ecsFixture(t)
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.ambiguousStop = true
	if err := e.Apply(t.Context(), f.target); err == nil {
		t.Fatal("lost stop reply succeeded")
	}
	if !reflect.DeepEqual(f.updates, []int{0}) {
		t.Fatal("old or target image restarted after ambiguity", f.updates)
	}
	j, err := e.journal()
	if err != nil || j.Phase != "applying" || !sameTarget(j.Target, f.target) {
		t.Fatalf("lost durable pin %+v %v", j, err)
	}
	if err := e.Apply(t.Context(), f.target); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("ambiguous operation retried", err)
	}
	if _, err := e.Prepare(t.Context(), f.target); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("ambiguous operation replaced", err)
	}
}

func TestECSExpiryAfterAdmittedStopDoesNotStrandSameTarget(t *testing.T) {
	e, f := ecsFixture(t)
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	e.now = func() time.Time { return now }
	f.target.EligibilityUntil = now.Add(time.Second)
	f.serviceMutation = func() {
		if f.service.Desired == 0 {
			now = now.Add(time.Hour)
		}
	}
	if err := e.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.updates, []int{0, 1}) {
		t.Fatal("admitted operation stranded by expiry", f.updates)
	}
}
