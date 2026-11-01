package securitydeployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// ECSConfig is a customer-owned Fargate/ALB/EFS contract. It deliberately
// excludes external deployment controllers and shared desired-state writers.
// ECS UpdateService has no resource-version precondition: customer IAM must
// enforce ExclusiveServiceOwner independently of this process's local lock.
type ECSConfig struct {
	AWS                     string    `json:"aws"`
	Profile                 string    `json:"profile"`
	Region                  string    `json:"region"`
	ClusterARN              string    `json:"cluster_arn"`
	ServiceARN              string    `json:"service_arn"`
	ServiceCreatedAt        time.Time `json:"service_created_at"`
	Container               string    `json:"container"`
	Binary                  string    `json:"binary"`
	TaskFamily              string    `json:"task_family"`
	TaskRoleARN             string    `json:"task_role_arn"`
	ExecutionRoleARN        string    `json:"execution_role_arn"`
	DatabaseSecretARN       string    `json:"database_secret_arn"`
	ScratchSecretARN        string    `json:"scratch_secret_arn"`
	UpgradeVolume           string    `json:"upgrade_volume"`
	UpgradeFileSystemID     string    `json:"upgrade_file_system_id"`
	UpgradeAccessPointID    string    `json:"upgrade_access_point_id"`
	UpgradeMountPath        string    `json:"upgrade_mount_path"`
	RootVolume              string    `json:"root_volume"`
	RootFileSystemID        string    `json:"root_file_system_id"`
	RootAccessPointID       string    `json:"root_access_point_id"`
	RootMountPath           string    `json:"root_mount_path"`
	TemporaryVolume         string    `json:"temporary_volume"`
	LogGroup                string    `json:"log_group"`
	LogPrefix               string    `json:"log_prefix"`
	ReadinessTargetGroupARN string    `json:"readiness_target_group_arn"`
	ReadinessPort           int       `json:"readiness_port"`
	OwnerTag                string    `json:"owner_tag"`
	ExclusiveServiceOwner   bool      `json:"exclusive_service_owner"`
}

var ecsRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]$`)
var ecsIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,79}$`)
var ecsLogPattern = regexp.MustCompile(`^[A-Za-z0-9/_.-]{1,256}$`)
var ecsTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (c ECSConfig) validate() error {
	if !safePath(c.AWS) || !safeName(c.Profile) || !ecsRegionPattern.MatchString(c.Region) ||
		!safeName(c.Container) || !safePath(c.Binary) || !safeName(c.TaskFamily) || c.ServiceCreatedAt.IsZero() ||
		!c.ExclusiveServiceOwner || !safeName(c.OwnerTag) || len(c.OwnerTag) > 32 ||
		!safeName(c.UpgradeVolume) || !safeName(c.RootVolume) || !safeName(c.TemporaryVolume) || c.UpgradeVolume == c.RootVolume || c.TemporaryVolume == c.RootVolume || c.TemporaryVolume == c.UpgradeVolume ||
		!ecsIDPattern.MatchString(c.UpgradeFileSystemID) || !strings.HasPrefix(c.UpgradeFileSystemID, "fs-") ||
		!ecsIDPattern.MatchString(c.RootFileSystemID) || !strings.HasPrefix(c.RootFileSystemID, "fs-") ||
		!ecsIDPattern.MatchString(c.UpgradeAccessPointID) || !strings.HasPrefix(c.UpgradeAccessPointID, "fsap-") ||
		!ecsIDPattern.MatchString(c.RootAccessPointID) || !strings.HasPrefix(c.RootAccessPointID, "fsap-") ||
		c.UpgradeAccessPointID == c.RootAccessPointID || !safePath(c.UpgradeMountPath) || !safePath(c.RootMountPath) ||
		c.UpgradeMountPath == "/" || c.RootMountPath == "/" || pathContains(c.UpgradeMountPath, c.RootMountPath) || pathContains(c.RootMountPath, c.UpgradeMountPath) ||
		!ecsLogPattern.MatchString(c.LogGroup) || !ecsLogPattern.MatchString(c.LogPrefix) || strings.Contains(c.LogPrefix, "/") ||
		c.ReadinessPort < 1024 || c.ReadinessPort > 65535 {
		return ErrInvalid
	}
	cluster, err := splitAWSARN(c.ClusterARN)
	if err != nil || cluster.service != "ecs" || cluster.region != c.Region || !strings.HasPrefix(cluster.resource, "cluster/") || strings.Count(cluster.resource, "/") != 1 {
		return ErrInvalid
	}
	service, err := splitAWSARN(c.ServiceARN)
	if err != nil || service.service != "ecs" || service.region != c.Region || service.account != cluster.account || !strings.HasPrefix(service.resource, "service/"+strings.TrimPrefix(cluster.resource, "cluster/")+"/") || strings.Count(service.resource, "/") != 2 {
		return ErrInvalid
	}
	for _, role := range []string{c.TaskRoleARN, c.ExecutionRoleARN} {
		arn, err := splitAWSARN(role)
		if err != nil || arn.service != "iam" || arn.region != "" || arn.account != cluster.account || !strings.HasPrefix(arn.resource, "role/") {
			return ErrInvalid
		}
	}
	for _, secret := range []string{c.DatabaseSecretARN, c.ScratchSecretARN} {
		arn, err := splitAWSARN(secret)
		if err != nil || arn.service != "secretsmanager" || arn.region != c.Region || arn.account != cluster.account || !strings.HasPrefix(arn.resource, "secret:") {
			return ErrInvalid
		}
	}
	if c.DatabaseSecretARN == c.ScratchSecretARN {
		return ErrInvalid
	}
	group, err := splitAWSARN(c.ReadinessTargetGroupARN)
	if err != nil || group.service != "elasticloadbalancing" || group.region != c.Region || group.account != cluster.account || !strings.HasPrefix(group.resource, "targetgroup/") {
		return ErrInvalid
	}
	return nil
}

func pathContains(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

type awsARN struct{ service, region, account, resource string }

func splitAWSARN(value string) (awsARN, error) {
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || len(parts[4]) != 12 || parts[5] == "" || strings.ContainsAny(value, "\x00\r\n\t ") {
		return awsARN{}, ErrInvalid
	}
	if _, err := strconv.ParseUint(parts[4], 10, 64); err != nil {
		return awsARN{}, ErrInvalid
	}
	return awsARN{service: parts[2], region: parts[3], account: parts[4], resource: parts[5]}, nil
}

func (c ECSConfig) clusterName() string {
	arn, _ := splitAWSARN(c.ClusterARN)
	return strings.TrimPrefix(arn.resource, "cluster/")
}

func (c ECSConfig) serviceName() string {
	arn, _ := splitAWSARN(c.ServiceARN)
	parts := strings.Split(arn.resource, "/")
	return parts[len(parts)-1]
}

func (c ECSConfig) taskARN(value string) bool {
	arn, err := splitAWSARN(value)
	cluster, _ := splitAWSARN(c.ClusterARN)
	if err != nil || arn.service != "ecs" || arn.region != c.Region || arn.account != cluster.account || !strings.HasPrefix(arn.resource, "task/"+c.clusterName()+"/") {
		return false
	}
	return ecsTaskIDPattern.MatchString(strings.TrimPrefix(arn.resource, "task/"+c.clusterName()+"/"))
}

func (c ECSConfig) definitionARN(value string) bool {
	arn, err := splitAWSARN(value)
	cluster, _ := splitAWSARN(c.ClusterARN)
	if err != nil || arn.service != "ecs" || arn.region != c.Region || arn.account != cluster.account || !strings.HasPrefix(arn.resource, "task-definition/"+c.TaskFamily+":") {
		return false
	}
	revision, err := strconv.ParseUint(strings.TrimPrefix(arn.resource, "task-definition/"+c.TaskFamily+":"), 10, 64)
	return err == nil && revision > 0
}

type ecsTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ecsService struct {
	ARN              string            `json:"serviceArn"`
	Name             string            `json:"serviceName"`
	ClusterARN       string            `json:"clusterArn"`
	Status           string            `json:"status"`
	CreatedAt        json.RawMessage   `json:"createdAt"`
	TaskDefinition   string            `json:"taskDefinition"`
	LaunchType       string            `json:"launchType"`
	PlatformVersion  string            `json:"platformVersion"`
	PlatformFamily   string            `json:"platformFamily"`
	Scheduling       string            `json:"schedulingStrategy"`
	Desired          int               `json:"desiredCount"`
	Running          int               `json:"runningCount"`
	Pending          int               `json:"pendingCount"`
	HealthGrace      int               `json:"healthCheckGracePeriodSeconds"`
	Rebalancing      string            `json:"availabilityZoneRebalancing"`
	CapacityStrategy []json.RawMessage `json:"capacityProviderStrategy"`
	Controller       struct {
		Type string `json:"type"`
	} `json:"deploymentController"`
	Configuration struct {
		Minimum  int    `json:"minimumHealthyPercent"`
		Maximum  int    `json:"maximumPercent"`
		Strategy string `json:"strategy"`
		Circuit  struct {
			Enable   bool `json:"enable"`
			Rollback bool `json:"rollback"`
		} `json:"deploymentCircuitBreaker"`
		Alarms struct {
			Enable   bool `json:"enable"`
			Rollback bool `json:"rollback"`
		} `json:"alarms"`
		Hooks []json.RawMessage `json:"lifecycleHooks"`
	} `json:"deploymentConfiguration"`
	Network       json.RawMessage `json:"networkConfiguration"`
	LoadBalancers []struct {
		Group     string `json:"targetGroupArn"`
		Container string `json:"containerName"`
		Port      int    `json:"containerPort"`
	} `json:"loadBalancers"`
	Deployments []struct {
		Status         string `json:"status"`
		TaskDefinition string `json:"taskDefinition"`
		Rollout        string `json:"rolloutState"`
		Desired        int    `json:"desiredCount"`
		Running        int    `json:"runningCount"`
		Pending        int    `json:"pendingCount"`
	} `json:"deployments"`
	Tags []ecsTag `json:"tags"`
}

func parseAWSDate(raw json.RawMessage) (time.Time, error) {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return time.Parse(time.RFC3339Nano, value)
	}
	seconds, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || seconds < 1 || seconds > 1e11 {
		return time.Time{}, ErrInvalid
	}
	whole := int64(seconds)
	return time.Unix(whole, int64((seconds-float64(whole))*1e9)).UTC(), nil
}

func (e *Executor) validateECSService(s ecsService, desired int) error {
	c := e.c.ECS
	created, err := parseAWSDate(s.CreatedAt)
	if err != nil || created.Sub(c.ServiceCreatedAt).Abs() > time.Microsecond || s.ARN != c.ServiceARN || s.Name != c.serviceName() || s.ClusterARN != c.ClusterARN || s.Status != "ACTIVE" ||
		!c.definitionARN(s.TaskDefinition) || s.LaunchType != "FARGATE" || len(s.CapacityStrategy) != 0 || s.PlatformFamily != "Linux" || s.Scheduling != "REPLICA" ||
		s.Controller.Type != "ECS" || s.Desired != desired || s.Pending < 0 || s.Running < 0 || s.HealthGrace < e.timeoutSeconds()+60 || s.Rebalancing != "DISABLED" ||
		s.Configuration.Minimum != 0 || s.Configuration.Maximum != 100 || s.Configuration.Circuit.Rollback || s.Configuration.Alarms.Enable || s.Configuration.Alarms.Rollback || len(s.Configuration.Hooks) != 0 ||
		(s.Configuration.Strategy != "" && s.Configuration.Strategy != "ROLLING") || len(s.LoadBalancers) != 1 || s.LoadBalancers[0].Group != c.ReadinessTargetGroupARN || s.LoadBalancers[0].Container != c.Container {
		return ErrUnsupported
	}
	version := strings.Split(s.PlatformVersion, ".")
	if len(version) != 3 || version[0] != "1" {
		return ErrUnsupported
	}
	minor, err := strconv.Atoi(version[1])
	if err != nil || minor < 4 {
		return ErrUnsupported
	}
	patch, err := strconv.Atoi(version[2])
	if err != nil || patch < 0 {
		return ErrUnsupported
	}
	owner := false
	seen := map[string]bool{}
	for _, tag := range s.Tags {
		if seen[tag.Key] {
			return ErrUnsupported
		}
		seen[tag.Key] = true
		if strings.HasPrefix(tag.Key, "aws:cloudformation:") || strings.HasPrefix(tag.Key, "terraform:") || strings.HasPrefix(tag.Key, "argocd.") {
			return ErrUnsupported
		}
		if tag.Key == "hikyo.dev/security-maintenance-owner" && tag.Value == c.OwnerTag {
			owner = true
		}
	}
	if !owner {
		return ErrUnsupported
	}
	var network struct {
		VPC struct {
			Subnets        []string `json:"subnets"`
			SecurityGroups []string `json:"securityGroups"`
			PublicIP       string   `json:"assignPublicIp"`
		} `json:"awsvpcConfiguration"`
	}
	if json.Unmarshal(s.Network, &network) != nil || len(network.VPC.Subnets) == 0 || len(network.VPC.Subnets) > 16 || len(network.VPC.SecurityGroups) == 0 || len(network.VPC.SecurityGroups) > 5 || network.VPC.PublicIP != "DISABLED" {
		return ErrUnsupported
	}
	for _, subnet := range network.VPC.Subnets {
		if !ecsIDPattern.MatchString(subnet) || !strings.HasPrefix(subnet, "subnet-") {
			return ErrUnsupported
		}
	}
	for _, group := range network.VPC.SecurityGroups {
		if !ecsIDPattern.MatchString(group) || !strings.HasPrefix(group, "sg-") {
			return ErrUnsupported
		}
	}
	return nil
}

type ecsContainerDefinition struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	Essential   bool     `json:"essential"`
	EntryPoint  []string `json:"entryPoint"`
	Command     []string `json:"command"`
	User        string   `json:"user"`
	ReadOnly    bool     `json:"readonlyRootFilesystem"`
	Privileged  bool     `json:"privileged"`
	StopTimeout int      `json:"stopTimeout"`
	Environment []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"environment"`
	Secrets []struct {
		Name      string `json:"name"`
		ValueFrom string `json:"valueFrom"`
	} `json:"secrets"`
	Mounts []struct {
		Source   string `json:"sourceVolume"`
		Path     string `json:"containerPath"`
		ReadOnly bool   `json:"readOnly"`
	} `json:"mountPoints"`
	Health struct {
		Command     []string `json:"command"`
		Interval    int      `json:"interval"`
		Timeout     int      `json:"timeout"`
		Retries     int      `json:"retries"`
		StartPeriod int      `json:"startPeriod"`
	} `json:"healthCheck"`
	Log struct {
		Driver  string            `json:"logDriver"`
		Options map[string]string `json:"options"`
		Secrets []json.RawMessage `json:"secretOptions"`
	} `json:"logConfiguration"`
	Linux struct {
		Capabilities struct {
			Add  []string `json:"add"`
			Drop []string `json:"drop"`
		} `json:"capabilities"`
		Devices []json.RawMessage `json:"devices"`
		Init    bool              `json:"initProcessEnabled"`
		Tmpfs   []json.RawMessage `json:"tmpfs"`
	} `json:"linuxParameters"`
}

type ecsDefinition struct {
	ARN          string
	Registration map[string]json.RawMessage
	Container    ecsContainerDefinition
}

func (e *Executor) validateECSDefinition(raw map[string]json.RawMessage) (ecsDefinition, error) {
	c := e.c.ECS
	result := ecsDefinition{Registration: map[string]json.RawMessage{}}
	registrationFields := []string{"family", "taskRoleArn", "executionRoleArn", "networkMode", "containerDefinitions", "volumes", "requiresCompatibilities", "cpu", "memory", "runtimePlatform", "ephemeralStorage", "enableFaultInjection", "placementConstraints"}
	readOnlyFields := []string{"taskDefinitionArn", "revision", "status", "requiresAttributes", "compatibilities", "registeredAt", "registeredBy", "deregisteredAt"}
	for key, value := range raw {
		if slices.Contains(registrationFields, key) {
			result.Registration[key] = value
		} else if !slices.Contains(readOnlyFields, key) {
			return result, ErrUnsupported
		}
	}
	readString := func(key string) string { var value string; _ = json.Unmarshal(raw[key], &value); return value }
	result.ARN = readString("taskDefinitionArn")
	if !c.definitionARN(result.ARN) || readString("family") != c.TaskFamily || readString("status") != "ACTIVE" || readString("taskRoleArn") != c.TaskRoleARN || readString("executionRoleArn") != c.ExecutionRoleARN || readString("networkMode") != "awsvpc" {
		return result, ErrUnsupported
	}
	var compat []string
	if json.Unmarshal(raw["requiresCompatibilities"], &compat) != nil || len(compat) != 1 || compat[0] != "FARGATE" {
		return result, ErrUnsupported
	}
	var platform struct {
		CPU string `json:"cpuArchitecture"`
		OS  string `json:"operatingSystemFamily"`
	}
	expectedCPU := "X86_64"
	if runtime.GOARCH == "arm64" {
		expectedCPU = "ARM64"
	}
	if json.Unmarshal(raw["runtimePlatform"], &platform) != nil || platform.OS != "LINUX" || platform.CPU != expectedCPU {
		return result, ErrUnsupported
	}
	for _, key := range []string{"cpu", "memory"} {
		value, err := strconv.ParseUint(readString(key), 10, 32)
		if err != nil || value == 0 {
			return result, ErrUnsupported
		}
	}
	if fault := raw["enableFaultInjection"]; len(fault) > 0 && string(fault) != "false" {
		return result, ErrUnsupported
	}
	if placement := raw["placementConstraints"]; len(placement) > 0 && string(placement) != "[]" && string(placement) != "null" {
		return result, ErrUnsupported
	}
	var containers []map[string]json.RawMessage
	if json.Unmarshal(raw["containerDefinitions"], &containers) != nil || len(containers) != 1 {
		return result, ErrUnsupported
	}
	allowedContainer := []string{"name", "image", "cpu", "memory", "memoryReservation", "essential", "entryPoint", "command", "environment", "secrets", "mountPoints", "portMappings", "healthCheck", "logConfiguration", "user", "readonlyRootFilesystem", "linuxParameters", "stopTimeout", "startTimeout", "dockerLabels", "ulimits", "workingDirectory", "systemControls", "volumesFrom", "privileged", "versionConsistency"}
	for key := range containers[0] {
		if !slices.Contains(allowedContainer, key) {
			return result, ErrUnsupported
		}
	}
	for _, key := range []string{"systemControls", "volumesFrom", "ulimits"} {
		if value := containers[0][key]; len(value) > 0 && string(value) != "[]" && string(value) != "null" {
			return result, ErrUnsupported
		}
	}
	containerRaw, _ := json.Marshal(containers[0])
	if json.Unmarshal(containerRaw, &result.Container) != nil {
		return result, ErrUnsupported
	}
	s := result.Container
	if s.Name != c.Container || !strings.HasPrefix(s.Image, e.c.ImageRepository+"@sha256:") || imageDigest(s.Image) == "" || !s.Essential || s.Privileged || !s.ReadOnly || s.User != "65532:65532" ||
		(len(s.EntryPoint) != 0 && !slices.Equal(s.EntryPoint, []string{c.Binary})) || !slices.Equal(s.Command, []string{"server"}) || s.StopTimeout < 30 || s.StopTimeout > 120 ||
		!slices.Equal(s.Health.Command, []string{"CMD", c.Binary, "security-maintenance", "health", "--liveness"}) || s.Health.Interval < 5 || s.Health.Timeout < 2 || s.Health.Retries < 1 ||
		s.Log.Driver != "awslogs" || len(s.Log.Secrets) != 0 || s.Log.Options["awslogs-region"] != c.Region || s.Log.Options["awslogs-group"] != c.LogGroup || s.Log.Options["awslogs-stream-prefix"] != c.LogPrefix || s.Log.Options["mode"] != "blocking" ||
		len(s.Linux.Capabilities.Add) != 0 || !slices.Contains(s.Linux.Capabilities.Drop, "ALL") || len(s.Linux.Devices) != 0 || len(s.Linux.Tmpfs) != 0 {
		return result, ErrUnsupported
	}
	for key := range s.Log.Options {
		if !slices.Contains([]string{"awslogs-region", "awslogs-group", "awslogs-stream-prefix", "mode"}, key) {
			return result, ErrUnsupported
		}
	}
	environment := map[string]string{}
	for _, value := range s.Environment {
		if _, ok := environment[value.Name]; ok {
			return result, ErrUnsupported
		}
		environment[value.Name] = value.Value
	}
	if environment["HIKYO_UPGRADE_UNATTENDED"] != "true" || environment["HIKYO_AUTO_MIGRATE"] != "true" || environment["HIKYO_HA"] != "false" || environment["HIKYO_CONFIG_ROLLOUT_ENROLLMENT"] != "" ||
		environment["HIKYO_OPERATIONAL_LISTEN"] != "0.0.0.0:"+strconv.Itoa(c.ReadinessPort) || !pathContains(c.UpgradeMountPath, environment["HIKYO_UPGRADE_STATE_DIR"]) ||
		environment["HIKYO_ROOT_KEY_FILE"] != c.RootMountPath+"/root-key" || environment["HIKYO_DB"] != "" || environment["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"] != "" {
		return result, ErrUnsupported
	}
	secrets := map[string]string{}
	for _, value := range s.Secrets {
		if _, ok := secrets[value.Name]; ok {
			return result, ErrUnsupported
		}
		if _, ok := environment[value.Name]; ok {
			return result, ErrUnsupported
		}
		secrets[value.Name] = value.ValueFrom
	}
	if len(secrets) != 2 || secrets["HIKYO_DB"] != c.DatabaseSecretARN || secrets["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"] != c.ScratchSecretARN {
		return result, ErrUnsupported
	}
	if len(s.Mounts) != 3 {
		return result, ErrUnsupported
	}
	upgradeMount, rootMount, tmpMount := false, false, false
	for _, mount := range s.Mounts {
		if mount.Source == c.UpgradeVolume && mount.Path == c.UpgradeMountPath && !mount.ReadOnly {
			upgradeMount = true
		} else if mount.Source == c.RootVolume && mount.Path == c.RootMountPath && mount.ReadOnly {
			rootMount = true
		} else if mount.Source == c.TemporaryVolume && mount.Path == "/tmp" && !mount.ReadOnly {
			tmpMount = true
		} else {
			return result, ErrUnsupported
		}
	}
	if !upgradeMount || !rootMount || !tmpMount {
		return result, ErrUnsupported
	}
	var volumes []struct {
		Name string `json:"name"`
		EFS  struct {
			FS            string `json:"fileSystemId"`
			Root          string `json:"rootDirectory"`
			Transit       string `json:"transitEncryption"`
			Authorization struct {
				AP  string `json:"accessPointId"`
				IAM string `json:"iam"`
			} `json:"authorizationConfig"`
		} `json:"efsVolumeConfiguration"`
	}
	if json.Unmarshal(raw["volumes"], &volumes) != nil || len(volumes) != 3 {
		return result, ErrUnsupported
	}
	seenVolumes := map[string]bool{}
	for _, volume := range volumes {
		if seenVolumes[volume.Name] {
			return result, ErrUnsupported
		}
		seenVolumes[volume.Name] = true
		if volume.Name == c.TemporaryVolume {
			if volume.EFS.FS != "" || volume.EFS.Authorization.AP != "" {
				return result, ErrUnsupported
			}
			continue
		}
		if volume.EFS.Transit != "ENABLED" || volume.EFS.Authorization.IAM != "ENABLED" || (volume.EFS.Root != "" && volume.EFS.Root != "/") {
			return result, ErrUnsupported
		}
		if volume.Name == c.UpgradeVolume {
			if volume.EFS.FS != c.UpgradeFileSystemID || volume.EFS.Authorization.AP != c.UpgradeAccessPointID {
				return result, ErrUnsupported
			}
		} else if volume.Name == c.RootVolume {
			if volume.EFS.FS != c.RootFileSystemID || volume.EFS.Authorization.AP != c.RootAccessPointID {
				return result, ErrUnsupported
			}
		} else {
			return result, ErrUnsupported
		}
	}
	if !seenVolumes[c.UpgradeVolume] || !seenVolumes[c.RootVolume] || !seenVolumes[c.TemporaryVolume] {
		return result, ErrUnsupported
	}
	return result, nil
}

func (d ecsDefinition) withImage(image string) (map[string]json.RawMessage, error) {
	copy := make(map[string]json.RawMessage, len(d.Registration))
	for key, value := range d.Registration {
		copy[key] = slices.Clone(value)
	}
	var containers []map[string]json.RawMessage
	if json.Unmarshal(copy["containerDefinitions"], &containers) != nil || len(containers) != 1 {
		return nil, ErrInvalid
	}
	encoded, _ := json.Marshal(image)
	containers[0]["image"] = encoded
	raw, err := json.Marshal(containers)
	if err != nil {
		return nil, err
	}
	copy["containerDefinitions"] = raw
	return copy, nil
}

func ecsRegistrationDigest(registration map[string]json.RawMessage) (releaseidentity.Digest, error) {
	raw, err := json.Marshal(registration)
	if err != nil {
		return "", err
	}
	return releaseidentity.Hash(raw), nil
}

func ecsServiceContractDigest(service ecsService) (releaseidentity.Digest, error) {
	// Runtime counts/deployment progress are observations. Everything that
	// could alter execution, ownership or networking is pinned independently.
	service.Desired, service.Running, service.Pending = 0, 0, 0
	service.Deployments = nil
	raw, err := json.Marshal(service)
	if err != nil {
		return "", err
	}
	return releaseidentity.Hash(raw), nil
}

func ecsSourceIdentityError() error {
	return errors.New("ECS source task, applied database or service ownership changed")
}

func ecsFailure(action string) error {
	return fmt.Errorf("security deployment: ECS %s failed; retain the exact operation journal", action)
}
