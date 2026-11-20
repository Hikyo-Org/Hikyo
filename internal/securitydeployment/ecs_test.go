//go:build darwin || linux

package securitydeployment

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
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

func ecsRaw(value any) jsontext.Value {
	raw, err := json.Marshal(value, jsonwire.Stable)
	if err != nil {
		panic(err)
	}
	return raw
}

func ecsTestDefinition(c Config, arn, image string) map[string]jsontext.Value {
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
	return map[string]jsontext.Value{"taskDefinitionArn": ecsRaw(arn), "status": ecsRaw("ACTIVE"), "revision": ecsRaw(1), "family": ecsRaw(p.TaskFamily), "taskRoleArn": ecsRaw(p.TaskRoleARN), "executionRoleArn": ecsRaw(p.ExecutionRoleARN), "networkMode": ecsRaw("awsvpc"),
		"requiresCompatibilities": ecsRaw([]string{"FARGATE"}), "cpu": ecsRaw("256"), "memory": ecsRaw("512"), "runtimePlatform": ecsRaw(map[string]string{"cpuArchitecture": cpu, "operatingSystemFamily": "LINUX"}),
		"containerDefinitions": ecsRaw([]any{container}), "volumes": ecsRaw([]any{volume(p.UpgradeVolume, p.UpgradeFileSystemID, p.UpgradeAccessPointID), volume(p.RootVolume, p.RootFileSystemID, p.RootAccessPointID), map[string]string{"name": p.TemporaryVolume}})}
}

type ecsFakeRunner struct {
	t                 *testing.T
	config            Config
	service           ecsService
	definitions       map[string]map[string]jsontext.Value
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
	registerErr       error
	runTaskErr        error
	runTaskFailures   bool
	// The call commits and then reports an error, as a retried request does.
	runTaskLateErr  error
	runTaskRunning  bool
	registerLateErr error
	listTasksFail   bool
	// deregisterErr fails before committing, deregisterLateErr after; unreadableInactive makes ECS refuse to describe INACTIVE revisions.
	deregisterErr, deregisterLateErr error
	unreadableInactive               bool
	reorderTags                      bool
	describes                        int
	missingJob                       int
	missingStopped                   int
	staleDesired                     int
	staleRestart                     int
	defTags                          map[string]jsontext.Value
	definitionBefore                 string
	listDefsFail                     bool
	// emptyFirstLogPage puts an empty page before the event; secondLogPage a second event after it.
	emptyFirstLogPage, secondLogPage bool
}

// ecsAWSOrder returns container definitions in AWS response order, not the
// sorted order json.Marshal gives a Go map.
func ecsAWSOrder(raw jsontext.Value) jsontext.Value {
	var containers []map[string]jsontext.Value
	if json.Unmarshal(raw, &containers) != nil {
		panic("container definitions")
	}
	var out strings.Builder
	out.WriteString("[")
	for i, container := range containers {
		if i > 0 {
			out.WriteString(",")
		}
		out.WriteString("{")
		keys := []string{"name", "image", "essential", "stopTimeout"}
		for key := range container {
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys[4:])
		slices.Reverse(keys[4:])
		for j, key := range keys {
			if j > 0 {
				out.WriteString(",")
			}
			fmt.Fprintf(&out, "%q:%s", key, container[key])
		}
		out.WriteString("}")
	}
	out.WriteString("]")
	return jsontext.Value(out.String())
}

func ecsFixture(t *testing.T) (*Executor, *ecsFakeRunner) {
	t.Helper()
	c := ecsTestConfig(t)
	p := c.ECS
	sourceImage := c.ImageRepository + "@sha256:" + strings.Repeat("1", 64)
	target := Target{Identity: ecsTestIdentity("1.0.1", 2), Image: c.ImageRepository + "@sha256:" + strings.Repeat("2", 64), RuntimeImageSHA256: strings.Repeat("4", 64), EligibilityUntil: time.Now().Add(time.Hour)}
	f := &ecsFakeRunner{t: t, config: c, definitions: map[string]map[string]jsontext.Value{}, tasks: map[string]ecsTask{}, logs: map[string]Observation{}, logTimes: map[string]int64{}, target: target, readiness: true,
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
	f.definitions[f.service.TaskDefinition]["containerDefinitions"] = ecsAWSOrder(f.definitions[f.service.TaskDefinition]["containerDefinitions"])
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

func ecsInputString(input map[string]jsontext.Value, key string) string {
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
	var input map[string]jsontext.Value
	if json.Unmarshal(command.Input, &input) != nil {
		return nil, errors.New("invalid CLI stdin JSON")
	}
	respond := func(value any) ([]byte, error) {
		return json.Marshal(value, jsonwire.Stable)
	}
	switch service + "/" + action {
	case "ecs/describe-services":
		s := f.service
		f.describes++
		if f.reorderTags && f.describes%2 == 0 {
			s.Tags = slices.Clone(s.Tags)
			slices.Reverse(s.Tags)
		}
		if f.staleDesired > 0 && s.Desired == 0 {
			f.staleDesired--
			s.Desired = 1
		}
		if f.staleRestart > 0 && s.Desired == 1 && len(f.updates) == 2 {
			f.staleRestart--
			s.Desired, s.Running, s.TaskDefinition = 0, 0, f.definitionBefore
			f.finishDeploymentOn(&s)
		}
		return respond(map[string]any{"services": []ecsService{s}, "failures": []any{}})
	case "ecs/describe-task-definition":
		arn := ecsInputString(input, "taskDefinition")
		definition, ok := f.definitions[arn]
		if !ok {
			return nil, errors.New("unknown task definition")
		}
		if f.unreadableInactive && string(definition["status"]) == `"INACTIVE"` {
			return nil, errors.New("security deployment: installed command exited with status 254: An error occurred (ClientException) when calling the DescribeTaskDefinition operation: Unable to describe task definition.")
		}
		tags := jsontext.Value("[]")
		if raw, ok := f.defTags[arn]; ok {
			tags = raw
		}
		return respond(map[string]any{"taskDefinition": definition, "tags": tags})
	case "ecs/list-tasks":
		started := ecsInputString(input, "startedBy")
		family := ecsInputString(input, "family")
		status := ecsInputString(input, "desiredStatus")
		if status == "" {
			status = "RUNNING"
		}
		// AWS: when startedBy is the filter, it must be the only one.
		if started != "" && (family != "" || len(input["desiredStatus"]) != 0 || len(input["serviceName"]) != 0) {
			return nil, ecsExit(254)
		}
		if started != "" && f.listTasksFail {
			return nil, errors.New("list-tasks unavailable")
		}
		arns := []string{}
		for arn, task := range f.tasks {
			if started != "" {
				if task.StartedBy == started && task.DesiredStatus == status {
					arns = append(arns, arn)
				}
			} else if family != "" && strings.HasPrefix(task.Definition, ecsTestPrefix+"task-definition/"+family+":") && task.DesiredStatus == status {
				arns = append(arns, arn)
			} else if family == "" && task.Group == "service:"+f.config.ECS.serviceName() && task.DesiredStatus == status {
				arns = append(arns, arn)
			}
		}
		slices.Sort(arns)
		var limit int
		_ = json.Unmarshal(input["maxResults"], &limit)
		start := 0
		if token := ecsInputString(input, "nextToken"); token != "" {
			start, _ = strconv.Atoi(strings.TrimPrefix(token, "n/"))
		}
		next := ""
		if limit > 0 && len(arns)-start > limit {
			next = "n/" + strconv.Itoa(start+limit)
			arns = arns[start : start+limit]
		} else {
			arns = arns[start:]
		}
		return respond(map[string]any{"taskArns": arns, "nextToken": next})
	case "ecs/describe-tasks":
		var arns []string
		_ = json.Unmarshal(input["tasks"], &arns)
		if len(arns) > 100 {
			return nil, ecsExit(254)
		}
		tasks := []ecsTask{}
		failures := []any{}
		for _, arn := range arns {
			task, ok := f.tasks[arn]
			if ok && f.missingJob > 0 && task.StartedBy != "" {
				f.missingJob--
				ok = false
			}
			if ok && f.missingStopped > 0 && task.DesiredStatus == "STOPPED" && task.StartedBy == "" {
				f.missingStopped--
				ok = false
			}
			if !ok {
				failures = append(failures, map[string]string{"arn": arn, "reason": "MISSING"})
				continue
			}
			tasks = append(tasks, task)
		}
		return respond(map[string]any{"tasks": tasks, "failures": failures})
	case "ecs/list-task-definitions":
		if f.listDefsFail {
			return nil, errors.New("list-task-definitions unavailable")
		}
		prefix := ecsInputString(input, "familyPrefix")
		arns := []string{}
		for arn, definition := range f.definitions {
			if strings.HasPrefix(strings.TrimPrefix(arn, ecsTestPrefix+"task-definition/"), prefix) && string(definition["status"]) == `"ACTIVE"` {
				arns = append(arns, arn)
			}
		}
		slices.SortFunc(arns, func(a, b string) int {
			var x, y int
			_, _ = fmt.Sscanf(a[strings.LastIndexByte(a, ':')+1:], "%d", &x)
			_, _ = fmt.Sscanf(b[strings.LastIndexByte(b, ':')+1:], "%d", &y)
			return y - x
		})
		return respond(map[string]any{"taskDefinitionArns": arns})
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
		if f.registerErr != nil {
			return nil, f.registerErr
		}
		f.registered++
		arn := ecsTestPrefix + fmt.Sprintf("task-definition/hikyo:%d", f.registered+1)
		definition := map[string]jsontext.Value{}
		for key, value := range input {
			if key != "tags" {
				definition[key] = value
			}
		}
		definition["taskDefinitionArn"], definition["revision"], definition["status"] = ecsRaw(arn), ecsRaw(f.registered+1), ecsRaw("ACTIVE")
		definition["containerDefinitions"] = ecsAWSOrder(definition["containerDefinitions"])
		if f.wrongRegistration {
			definition["taskRoleArn"] = ecsRaw("arn:aws:iam::" + ecsTestAccount + ":role/other")
		}
		f.definitions[arn] = definition
		if f.defTags == nil {
			f.defTags = map[string]jsontext.Value{}
		}
		f.defTags[arn] = input["tags"]
		if f.registerLateErr != nil {
			return nil, f.registerLateErr
		}
		return respond(map[string]any{"taskDefinition": definition})
	case "ecs/deregister-task-definition":
		arn := ecsInputString(input, "taskDefinition")
		definition, ok := f.definitions[arn]
		if !ok {
			return nil, errors.New("unknown deregistration")
		}
		if f.deregisterErr != nil {
			return nil, f.deregisterErr
		}
		definition["status"] = ecsRaw("INACTIVE")
		f.deregistered = append(f.deregistered, arn)
		if f.deregisterLateErr != nil {
			return nil, f.deregisterLateErr
		}
		return respond(map[string]any{"taskDefinition": definition})
	case "ecs/run-task":
		if f.runTaskErr != nil {
			return nil, f.runTaskErr
		}
		if f.runTaskFailures {
			return respond(map[string]any{"tasks": []ecsTask{}, "failures": []any{map[string]string{"reason": "RESOURCE:FARGATE"}}})
		}
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
		if f.runTaskRunning {
			task.LastStatus, task.DesiredStatus, task.Containers[0].ExitCode, task.Containers[0].LastStatus = "RUNNING", "RUNNING", nil, "RUNNING"
		}
		f.tasks[arn] = task
		f.logs[arn[strings.LastIndexByte(arn, '/')+1:]] = observation
		f.logTimes[arn[strings.LastIndexByte(arn, '/')+1:]] = time.Now().UnixMilli()
		if f.runTaskLateErr != nil {
			return nil, f.runTaskLateErr
		}
		return respond(map[string]any{"tasks": []ecsTask{task}, "failures": []any{}})
	case "logs/get-log-events":
		stream := ecsInputString(input, "logStreamName")
		id := stream[strings.LastIndexByte(stream, '/')+1:]
		observation, ok := f.logs[id]
		if !ok {
			return nil, errors.New("foreign log stream")
		}
		event := map[string]any{"timestamp": f.logTimes[id], "message": string(ecsRaw(observation))}
		second := map[string]any{"timestamp": f.logTimes[id], "message": "unexpected second result"}
		pages := [][]any{{event}}
		switch {
		case f.badLogs:
			pages = [][]any{{event, second}}
		case f.emptyFirstLogPage:
			pages = [][]any{{}, {event}}
		case f.secondLogPage:
			pages = [][]any{{event}, {second}}
		}
		// Token "p/N" names page N; past the last page the passed token returns unchanged, as at a stream's end.
		page := 0
		if token := ecsInputString(input, "nextToken"); token != "" {
			page, _ = strconv.Atoi(strings.TrimPrefix(token, "p/"))
		}
		events := []any{}
		if page < len(pages) {
			events = pages[page]
		}
		return respond(map[string]any{"events": events, "nextForwardToken": "p/" + strconv.Itoa(min(page+1, len(pages)))})
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
		// Tags come only from DescribeServices with include=TAGS.
		updated := f.service
		updated.Tags = nil
		return respond(map[string]any{"service": updated})
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
	// Built independently of withImage: the source registration with only the image replaced.
	expected := maps.Clone(sourceDefinition.Registration)
	var containers []map[string]any
	if err := json.Unmarshal(expected["containerDefinitions"], &containers); err != nil || len(containers) != 1 {
		t.Fatal("source container definitions", err)
	}
	containers[0]["image"] = f.target.Image
	expected["containerDefinitions"] = ecsRaw(containers)
	registered, err := e.validateECSDefinition(after)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest, _ := ecsRegistrationDigest(expected)
	registeredDigest, _ := ecsRegistrationDigest(registered.Registration)
	if registeredDigest != expectedDigest {
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
	for _, name := range []string{"source generation", "runtime digest", "multiple results", "result on a later page", "registration substitution"} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			switch name {
			case "source generation":
				f.driftStage = true
			case "runtime digest":
				f.badRuntime = true
			case "multiple results":
				f.badLogs = true
			case "result on a later page":
				f.secondLogPage = true
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

// CloudWatch may return an empty page with more events behind its token.
func TestECSTaskProofFollowsAnEmptyLogPage(t *testing.T) {
	e, f := ecsFixture(t)
	f.emptyFirstLogPage = true
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("proof behind an empty page refused: %v", err)
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

func ecsExit(status int) error {
	if status == 254 {
		return errors.New("security deployment: installed command exited with status 254: aws: [ERROR]: An error occurred (InvalidParameterException) when calling the operation")
	}
	return fmt.Errorf("security deployment: installed command exited with status %d", status)
}

func TestECSDefiniteRunTaskFailureDoesNotWedgeHealthyService(t *testing.T) {
	for name, set := range map[string]func(*ecsFakeRunner){
		"not sent":    func(f *ecsFakeRunner) { f.runTaskErr = ecsExit(252) },
		"no capacity": func(f *ecsFakeRunner) { f.runTaskFailures = true },
	} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			set(f)
			if _, err := e.Observe(t.Context()); err == nil {
				t.Fatal("failed job reported healthy")
			}
			if _, err := os.Stat(e.ecsJobPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("job intent retained after definite failure", err)
			}
			f.runTaskErr, f.runTaskFailures = nil, false
			if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
				t.Fatalf("healthy service wedged: %+v %v", o, err)
			}
		})
	}
}

func TestECSLostRunTaskReplyRetainsRecoveryIntent(t *testing.T) {
	for name, lost := range map[string]error{"unclassified exit": ecsExit(255), "cancelled": context.DeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			f.runTaskErr = lost
			if _, err := e.Observe(t.Context()); err == nil {
				t.Fatal("lost reply reported healthy")
			}
			f.runTaskErr = nil
			// Unlisted until the window passes, the possible task is waited for, not cleared.
			if _, err := e.Observe(t.Context()); !errors.Is(err, ErrNotReady) {
				t.Fatalf("lost reply forgot a possible task: %v", err)
			}
		})
	}
}

func TestECSNeverSentRegisterFailureClearsIntentAndRetries(t *testing.T) {
	e, f := ecsFixture(t)
	f.registerErr = ecsExit(252)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("unsent registration prepared")
	}
	if _, err := os.Stat(e.ecsRegistrationPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("registration intent retained although nothing was sent", err)
	}
	f.registerErr = nil
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("unsent registration prevented retry: %v", err)
	}
}

func TestECSLostOrRefusedRegisterReplyIsSettledByReadingTheRevision(t *testing.T) {
	for name, set := range map[string]func(*ecsFakeRunner){
		"refused, nothing registered": func(f *ecsFakeRunner) { f.registerErr = ecsExit(254) },
		"lost, nothing registered":    func(f *ecsFakeRunner) { f.registerErr = ecsExit(255) },
		"refused after committing":    func(f *ecsFakeRunner) { f.registerLateErr = ecsExit(254) },
		"lost after committing":       func(f *ecsFakeRunner) { f.registerLateErr = ecsExit(255) },
	} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			set(f)
			if _, err := e.Prepare(t.Context(), f.target); err == nil {
				t.Fatal("unconfirmed registration prepared")
			}
			if _, err := os.Stat(e.ecsRegistrationPath()); err != nil {
				t.Fatal("intent cleared before a read proved the outcome", err)
			}
			committed := f.registered
			f.registerErr, f.registerLateErr = nil, nil
			// Absence of the revision proves nothing until the listing has settled.
			ecsLaterThanSettle(e)
			if _, err := e.Prepare(t.Context(), f.target); err != nil {
				t.Fatalf("next run did not settle the registration: %v", err)
			}
			if committed == 1 && !slices.Contains(f.deregistered, ecsTestPrefix+"task-definition/hikyo:2") {
				t.Fatal("committed revision lost track of", f.deregistered)
			}
			if _, err := os.Stat(e.ecsRegistrationPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("settled intent retained", err)
			}
		})
	}
}

func TestECSRegisterRecoveryStaysPinnedWhenTheReadFails(t *testing.T) {
	e, f := ecsFixture(t)
	f.registerLateErr = ecsExit(255)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("lost registration prepared")
	}
	f.registerLateErr, f.listDefsFail = nil, true
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("failed read treated as proof")
	}
	if _, err := os.Stat(e.ecsRegistrationPath()); err != nil {
		t.Fatal("intent cleared without proof", err)
	}
	if f.registered != 1 || len(f.deregistered) != 0 {
		t.Fatal("acted on an unread revision", f.registered, f.deregistered)
	}
	f.listDefsFail = false
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("recovery did not resume: %v", err)
	}
}

// Pins the runner message ecsDefiniteFailure parses, using real process exits.
func TestECSDefiniteFailureMatchesCommandRunner(t *testing.T) {
	for status, definite := range map[int]bool{252: true, 253: true, 254: false, 255: false, 1: false} {
		_, err := CommandRunner{}.Run(t.Context(), Command{Path: "/bin/sh", Args: []string{"-c", fmt.Sprintf("exit %d", status)}})
		if err == nil || ecsDefiniteFailure(err) != definite {
			t.Fatalf("status %d classified wrongly: %v", status, err)
		}
	}
	if _, err := (CommandRunner{}).Run(t.Context(), Command{Path: "/nonexistent/aws"}); err == nil || !ecsDefiniteFailure(err) {
		t.Fatalf("unstartable CLI sent no request: %v", err)
	}
	// Real aws-cli 2.38.4 stderr for a 400, exit 254. The CLI retries internally, so
	// the first attempt may have committed even though the visible reply is a refusal.
	if ecsDefiniteFailure(errors.New("security deployment: installed command exited with status 254: aws: [ERROR]: An error occurred (AccessDeniedException) when calling the RunTask operation: probe")) {
		t.Fatal("service refusal after a possible retry classified as no effect")
	}
	for _, code := range []string{"ServerException", "ServiceUnavailableException", "InternalFailure"} {
		if ecsDefiniteFailure(fmt.Errorf("security deployment: installed command exited with status 254: aws: [ERROR]: An error occurred (%s) when calling the RunTask operation (reached max retries: 1): probe", code)) {
			t.Fatalf("%s may have had an effect", code)
		}
	}
	for _, message := range []string{"An error occurred (503) when calling the RunTask operation", "An error occurred (ThrottlingException) when calling the RunTask operation (reached max retries: 2): rate"} {
		if ecsDefiniteFailure(errors.New("security deployment: installed command exited with status 254: aws: [ERROR]: " + message)) {
			t.Fatalf("%s may have had an effect", message)
		}
	}
	if ecsDefiniteFailure(errors.New("security deployment: installed command exited with status 255: exited with status 254")) {
		t.Fatal("stderr text overrode the real exit status")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (CommandRunner{}).Run(cancelled, Command{Path: "/bin/sh", Args: []string{"-c", "exit 254"}}); err == nil || ecsDefiniteFailure(err) {
		t.Fatalf("cancelled call classified definite: %v", err)
	}
}

func ecsLaterThanSettle(e *Executor) {
	e.now = func() time.Time { return time.Now().Add(ecsJobSettle + time.Minute) }
}

func TestECSRefusedOrLostRunTaskIsSettledByReadingTheTask(t *testing.T) {
	for name, set := range map[string]func(*ecsFakeRunner){
		"refused, nothing launched":              func(f *ecsFakeRunner) { f.runTaskErr = ecsExit(254) },
		"lost, nothing launched":                 func(f *ecsFakeRunner) { f.runTaskErr = ecsExit(255) },
		"refused after launching":                func(f *ecsFakeRunner) { f.runTaskLateErr = ecsExit(254) },
		"lost after launching":                   func(f *ecsFakeRunner) { f.runTaskLateErr = ecsExit(255) },
		"refused after launching, still running": func(f *ecsFakeRunner) { f.runTaskLateErr, f.runTaskRunning = ecsExit(254), true },
	} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			set(f)
			if _, err := e.Observe(t.Context()); err == nil {
				t.Fatal("unconfirmed job reported healthy")
			}
			if _, err := os.Stat(e.ecsJobPath()); err != nil {
				t.Fatal("job intent cleared before a read proved the outcome", err)
			}
			launched := f.jobs > 0
			f.runTaskErr, f.runTaskLateErr, f.runTaskRunning = nil, nil, false
			ecsLaterThanSettle(e)
			if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
				t.Fatalf("recovery did not settle the job: %+v %v", o, err)
			}
			if _, err := os.Stat(e.ecsJobPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("settled job intent retained", err)
			}
			for arn, task := range f.tasks {
				if task.Group == "hikyo-security:"+f.config.ECS.OwnerTag && task.LastStatus != "STOPPED" {
					t.Fatal("job task left running", arn)
				}
			}
			if launched && f.jobs != 2 {
				t.Fatal("expected the recovered job plus one inspection", f.jobs)
			}
		})
	}
}

func TestECSFoundStoppedJobClearsOnceSettled(t *testing.T) {
	e, f := ecsFixture(t)
	f.runTaskLateErr = ecsExit(255)
	if _, err := e.Observe(t.Context()); err == nil {
		t.Fatal("lost reply reported healthy")
	}
	f.runTaskLateErr = nil
	// A startedBy listing shows only desired-RUNNING tasks, so a stopped job is
	// indistinguishable from one not yet visible until the settle window passes.
	if _, err := e.Observe(t.Context()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("an unlisted job cleared before it settled: %v", err)
	}
	ecsLaterThanSettle(e)
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("a settled stopped job did not clear the record: %+v %v", o, err)
	}
}

func TestECSJobRecoveryStaysPinnedWithoutConclusiveReads(t *testing.T) {
	e, f := ecsFixture(t)
	f.runTaskErr = ecsExit(255)
	if _, err := e.Observe(t.Context()); err == nil {
		t.Fatal("lost reply reported healthy")
	}
	f.runTaskErr, f.listTasksFail = nil, true
	ecsLaterThanSettle(e)
	if _, err := e.Observe(t.Context()); err == nil {
		t.Fatal("a failed listing treated as absence")
	}
	if _, err := os.Stat(e.ecsJobPath()); err != nil {
		t.Fatal("job intent cleared without proof", err)
	}
	f.listTasksFail = false
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("recovery did not resume: %+v %v", o, err)
	}
}

func TestECSMissingJobTaskIsAbsentOnlyOnceSettled(t *testing.T) {
	e, f := ecsFixture(t)
	intent := ecsJobIntent{Format: ecsJobFormat, Nonce: strings.Repeat("d", 32), Definition: f.service.TaskDefinition, TaskARN: ecsTestPrefix + "task/hikyo/" + strings.Repeat("e", 32), CreatedAt: time.Now().UTC()}
	if err := e.saveECSJob(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Observe(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a task ECS may not have indexed yet counted as gone: %v", err)
	}
	if _, err := os.Stat(e.ecsJobPath()); err != nil {
		t.Fatal("job intent cleared too early", err)
	}
	ecsLaterThanSettle(e)
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("reaped task kept the installation pinned: %+v %v", o, err)
	}
	if _, err := os.Stat(e.ecsJobPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("job intent retained for a reaped task", err)
	}
}

func TestECSTimeoutKeepsItsOwnBound(t *testing.T) {
	c := ecsTestConfig(t)
	c.TimeoutSeconds = maxECSTimeoutSeconds
	if err := Validate(c); err != nil {
		t.Fatalf("ECS refused its own bound: %v", err)
	}
	c.TimeoutSeconds++
	if err := Validate(c); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ECS accepted more than an hour: %v", err)
	}
}

func TestECSServiceTagOrderIsNotSourceDrift(t *testing.T) {
	e, f := ecsFixture(t)
	f.service.Tags = append(f.service.Tags, ecsTag{Key: "team", Value: "db"})
	f.reorderTags = true
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("reordered tags reported as drift: %v", err)
	}
}

func TestECSJobTaskNotYetIndexedAfterRunTaskIsAwaited(t *testing.T) {
	e, f := ecsFixture(t)
	f.missingJob = 1
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("not-yet-indexed job task aborted: %v", err)
	}
}

func TestECSStaleDesiredCountAfterScaleDownIsAwaited(t *testing.T) {
	e, f := ecsFixture(t)
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.staleDesired = 1
	if err := e.Apply(t.Context(), f.target); err != nil {
		t.Fatalf("stale desiredCount aborted apply: %v", err)
	}
}

func (f *ecsFakeRunner) finishDeploymentOn(s *ecsService) {
	saved := f.service
	f.service = *s
	f.finishDeployment()
	*s = f.service
	f.service = saved
}

func TestECSStaleScaledDownReadAfterRestartIsAwaited(t *testing.T) {
	e, f := ecsFixture(t)
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.definitionBefore, f.staleRestart = f.service.TaskDefinition, 1
	if err := e.Apply(t.Context(), f.target); err != nil {
		t.Fatalf("stale restart read aborted apply: %v", err)
	}
}

func TestECSRegistrationRecoveryRetiresOnlyItsOwnNonce(t *testing.T) {
	e, f := ecsFixture(t)
	f.registerErr = ecsExit(255)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("lost register succeeded")
	}
	source, err := e.ecsDefinition(t.Context(), f.service.TaskDefinition)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := source.withImage(f.target.Image)
	if err != nil {
		t.Fatal(err)
	}
	foreign := ecsTestPrefix + "task-definition/hikyo:9"
	reg["taskDefinitionArn"], reg["revision"], reg["status"] = ecsRaw(foreign), ecsRaw(9), ecsRaw("ACTIVE")
	f.definitions[foreign] = reg
	f.registerErr = nil
	ecsLaterThanSettle(e)
	_, _ = e.Prepare(t.Context(), f.target)
	if slices.Contains(f.deregistered, foreign) {
		t.Fatal("recovery retired a foreign revision with no registration nonce")
	}
}

func TestECSSoleWriterWaitsForStoppingFamilyTasks(t *testing.T) {
	e, f := ecsFixture(t)
	// More stopped tasks than one page or one describe batch holds, sorted before the stopping one.
	for i := 0; i < 150; i++ {
		task := f.serviceTask(ecsTestPrefix+fmt.Sprintf("task/hikyo/%032x", 0x100+i), f.service.TaskDefinition, "", strings.Repeat("3", 64))
		task.Group, task.LastStatus, task.DesiredStatus = "family:hikyo", "STOPPED", "STOPPED"
		f.tasks[task.ARN] = task
	}
	stopping := f.serviceTask(ecsTestPrefix+"task/hikyo/"+strings.Repeat("f", 32), f.service.TaskDefinition, "", strings.Repeat("3", 64))
	stopping.Group, stopping.DesiredStatus = "family:hikyo", "STOPPED"
	f.tasks[stopping.ARN] = stopping
	if _, err := e.Observe(t.Context()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("a family task still shutting down qualified as sole writer: %v", err)
	}
	stopping.LastStatus = "STOPPED"
	f.tasks[stopping.ARN] = stopping
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("stopped family tasks blocked qualification: %+v %v", o, err)
	}
}

// A stopped task ECS purges between the listing and describe-tasks proves nothing
// either way, so qualification waits for a fresh listing instead of failing.
func TestECSSoleWriterRelistsAFamilyTaskReportedMissing(t *testing.T) {
	e, f := ecsFixture(t)
	stopped := f.serviceTask(ecsTestPrefix+"task/hikyo/"+strings.Repeat("e", 32), f.service.TaskDefinition, "", strings.Repeat("3", 64))
	stopped.Group, stopped.LastStatus, stopped.DesiredStatus = "family:hikyo", "STOPPED", "STOPPED"
	f.tasks[stopped.ARN] = stopped
	f.missingStopped = 1
	if _, err := e.Observe(t.Context()); !errors.Is(err, ErrNotReady) || errors.Is(err, ErrConflict) {
		t.Fatalf("a purged family task did not wait for a fresh listing: %v", err)
	}
	if o, err := e.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("the fresh listing did not qualify: %+v %v", o, err)
	}
}

func TestECSLostDeregisterReplyDoesNotBlockLaterPrepare(t *testing.T) {
	for name, set := range map[string]func(*ecsFakeRunner){
		"lost before committing":                    func(f *ecsFakeRunner) { f.deregisterErr = ecsExit(255) },
		"lost after committing":                     func(f *ecsFakeRunner) { f.deregisterLateErr = ecsExit(255) },
		"lost after committing, no longer readable": func(f *ecsFakeRunner) { f.deregisterLateErr, f.unreadableInactive = ecsExit(255), true },
	} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			// A failed stage job retires its never-selected revision.
			f.badRuntime = true
			set(f)
			if _, err := e.Prepare(t.Context(), f.target); err == nil {
				t.Fatal("failed stage prepared")
			}
			intent, err := e.readECSRegistration()
			if err != nil || intent.Definition != ecsTestPrefix+"task-definition/hikyo:2" {
				t.Fatalf("retirement not recorded before deregistering: %+v %v", intent, err)
			}
			f.badRuntime, f.deregisterErr, f.deregisterLateErr = false, nil, nil
			if _, err := e.Prepare(t.Context(), f.target); err != nil {
				t.Fatalf("lost deregister reply blocked the next Prepare: %v", err)
			}
			if !slices.Contains(f.deregistered, ecsTestPrefix+"task-definition/hikyo:2") {
				t.Fatal("abandoned revision left ACTIVE", f.deregistered)
			}
		})
	}
}

// A revision ECS reports INACTIVE is retired whoever deregistered it, so its record cannot block.
func TestECSInactiveRevisionClearsARecordWithoutTheRetiringMarker(t *testing.T) {
	e, f := ecsFixture(t)
	f.badRuntime, f.deregisterErr = true, ecsExit(255)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("failed stage prepared")
	}
	intent, err := e.readECSRegistration()
	if err != nil {
		t.Fatal(err)
	}
	intent.Retiring = false
	if err := e.writeECSRecord(e.ecsRegistrationPath(), intent); err != nil {
		t.Fatal(err)
	}
	// Deregistered by another hand before this record sent its call.
	f.definitions[intent.Definition]["status"] = jsontext.Value(`"INACTIVE"`)
	f.badRuntime, f.deregisterErr = false, nil
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("an INACTIVE revision blocked Prepare: %v", err)
	}
}

func TestECSRetiringRecordKeepsOwnershipChecksWhileActive(t *testing.T) {
	e, f := ecsFixture(t)
	f.badRuntime, f.deregisterErr = true, ecsExit(255)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("failed stage prepared")
	}
	intent, err := e.readECSRegistration()
	if err != nil || !intent.Retiring {
		t.Fatalf("retiring marker not durable before the call: %+v %v", intent, err)
	}
	f.defTags[intent.Definition] = ecsRaw([]ecsTag{{Key: "hikyo.dev/security-registration", Value: strings.Repeat("0", 32)}})
	f.badRuntime, f.deregisterErr = false, nil
	if _, err := e.Prepare(t.Context(), f.target); !errors.Is(err, ErrConflict) {
		t.Fatalf("a still-ACTIVE revision with another nonce was retired: %v", err)
	}
	if len(f.deregistered) != 0 {
		t.Fatal("deregistered a revision this record does not own", f.deregistered)
	}
}

func TestECSAmbiguousRegistrationWaitsForTheSettleWindow(t *testing.T) {
	e, f := ecsFixture(t)
	f.registerErr = ecsExit(255)
	if _, err := e.Prepare(t.Context(), f.target); err == nil {
		t.Fatal("lost registration prepared")
	}
	f.registerErr = nil
	if _, err := e.Prepare(t.Context(), f.target); !errors.Is(err, ErrNotReady) {
		t.Fatalf("an unlisted registration counted as never created: %v", err)
	}
	if _, err := os.Stat(e.ecsRegistrationPath()); err != nil || f.registered != 0 {
		t.Fatal("intent cleared or another revision registered inside the window", err, f.registered)
	}
	ecsLaterThanSettle(e)
	if _, err := e.Prepare(t.Context(), f.target); err != nil || f.registered != 1 {
		t.Fatalf("settled absence did not release the intent: %v %d", err, f.registered)
	}
}

func TestECSRegistrationRecordWithoutCreatedAtStartsItsWindow(t *testing.T) {
	e, f := ecsFixture(t)
	if err := e.writeECSRecord(e.ecsRegistrationPath(), map[string]any{"format": ecsRegistrationFormat, "nonce": strings.Repeat("d", 32), "target": f.target, "registration_sha256": strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Prepare(t.Context(), f.target); !errors.Is(err, ErrNotReady) {
		t.Fatalf("a record of unknown age was cleared at once: %v", err)
	}
	if intent, err := e.readECSRegistration(); err != nil || intent.CreatedAt.IsZero() {
		t.Fatalf("settle window not started: %+v %v", intent, err)
	}
	ecsLaterThanSettle(e)
	if _, err := e.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("settled old record kept Prepare blocked: %v", err)
	}
}

func TestECSUpgradeStateDirMustResolveOntoTheUpgradeVolume(t *testing.T) {
	for name, dir := range map[string]string{
		"traversal out of the volume": "/var/lib/hikyo-upgrade/../../../tmp/state",
		"relative":                    "var/lib/hikyo-upgrade/state",
		"on the temporary volume":     "/tmp/state",
	} {
		t.Run(name, func(t *testing.T) {
			e, f := ecsFixture(t)
			definition := f.definitions[f.service.TaskDefinition]
			var containers []map[string]any
			_ = json.Unmarshal(definition["containerDefinitions"], &containers)
			for _, value := range containers[0]["environment"].([]any) {
				if variable := value.(map[string]any); variable["name"] == "HIKYO_UPGRADE_STATE_DIR" {
					variable["value"] = dir
				}
			}
			definition["containerDefinitions"] = ecsRaw(containers)
			if _, err := e.Observe(t.Context()); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("state directory %q accepted: %v", dir, err)
			}
		})
	}
}
