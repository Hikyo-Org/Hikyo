package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const operatorMemoryLimit = uint64(128 << 20)

type processMeasurement struct {
	PID          uint64 `json:"node_pid"`
	StartTime    uint64 `json:"start_time_ticks"`
	ContainerID  string `json:"container_id"`
	Cgroup       string `json:"cgroup"`
	Executable   string `json:"executable"`
	BinarySHA256 string `json:"executable_sha256"`
	RSS          uint64 `json:"rss_bytes"`
	PeakRSS      uint64 `json:"rss_peak_bytes"`
}

func positiveNumber(raw string) (uint64, error) {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("missing or invalid positive measurement")
	}
	return value, nil
}

// The node-side reader is outside the measured container. Before/after PID and
// process start identity checks prevent accepting a replacement or partial read.
func verifyProcess(raw, containerID, binarySHA string) (processMeasurement, error) {
	var out processMeasurement
	fields := map[string][]string{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			if len(parts) == 1 && !strings.HasSuffix(parts[0], ":") {
				return out, errors.New("malformed process measurement")
			}
			continue
		}
		if _, duplicate := fields[parts[0]]; duplicate {
			return out, fmt.Errorf("duplicate process measurement %s", parts[0])
		}
		fields[parts[0]] = parts[1:]
	}
	one := func(key string) string {
		if len(fields[key]) != 1 {
			return ""
		}
		return fields[key][0]
	}
	if len(containerID) != 64 || len(binarySHA) != 64 || strings.Trim(containerID+binarySHA, "0123456789abcdef") != "" {
		return out, errors.New("invalid expected process identity")
	}
	pid, err := positiveNumber(one("pids_before"))
	if err != nil || one("pids_after") != one("pids_before") || one("Pid:") != one("pids_before") || one("Tgid:") != one("pids_before") {
		return out, errors.New("operator cgroup must retain exactly one identified process")
	}
	start, err := positiveNumber(one("start_time_before"))
	if err != nil || one("start_time_after") != one("start_time_before") {
		return out, errors.New("operator process changed during measurement")
	}
	cgroup := one("cgroup")
	if one("container_id") != containerID || !strings.HasPrefix(cgroup, "/sys/fs/cgroup/") || filepath.Clean(cgroup) != cgroup || filepath.Base(cgroup) != "cri-containerd-"+containerID+".scope" {
		return out, errors.New("operator process is not bound to the exact CRI cgroup")
	}
	command, err := base64.StdEncoding.DecodeString(one("cmdline_base64"))
	args := strings.Split(string(command), "\x00")
	if err != nil || len(args) < 3 || args[0] != "/hikyo" || args[1] != "operator" || one("Name:") != "hikyo" || one("executable") != "/hikyo" || one("executable_sha256") != binarySHA {
		return out, errors.New("measured process is not the source-built Hikyo operator")
	}
	readRSS := func(key string) (uint64, error) {
		parts := fields[key]
		if len(parts) != 2 || parts[1] != "kB" {
			return 0, errors.New("missing or invalid RSS units")
		}
		value, err := positiveNumber(parts[0])
		if err != nil || value > math.MaxUint64/1024 {
			return 0, errors.New("invalid RSS measurement")
		}
		return value * 1024, nil
	}
	rss, err := readRSS("VmRSS:")
	if err != nil {
		return out, err
	}
	peak, err := readRSS("VmHWM:")
	if err != nil || rss > peak || peak >= operatorMemoryLimit {
		return out, errors.New("operator peak RSS must be strictly below 128 MiB")
	}
	return processMeasurement{pid, start, containerID, cgroup, "/hikyo", binarySHA, rss, peak}, nil
}

// cgroup memory.peak includes file cache and kernel charges. Keep it as a
// diagnostic, not a substitute for the maintainer's process RSS requirement.
func verifyCgroup(raw, expectedCPU, expectedMemory string) (uint64, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 6 || lines[0] != expectedCPU || lines[1] != expectedMemory || lines[2] != "0" {
		return 0, errors.New("missing or incorrect effective cgroup limits")
	}
	peak, err := positiveNumber(lines[3])
	if err != nil {
		return 0, err
	}
	events := map[string]uint64{}
	for _, line := range lines[4:] {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return 0, errors.New("malformed cgroup memory event")
		}
		if _, duplicate := events[parts[0]]; duplicate {
			return 0, errors.New("duplicate cgroup memory event")
		}
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return 0, errors.New("invalid cgroup memory event")
		}
		events[parts[0]] = value
	}
	for _, key := range []string{"oom", "oom_kill"} {
		if value, present := events[key]; !present || value != 0 {
			return 0, fmt.Errorf("cgroup %s missing or nonzero", key)
		}
	}
	return peak, nil
}

func verifyResources(directory, binarySHA string) error {
	read := func(name string) (string, error) {
		raw, err := os.ReadFile(filepath.Join(directory, name))
		return string(raw), err
	}
	podBytes, err := read("pods.json")
	if err != nil {
		return err
	}
	var pods corev1.PodList
	if err := json.Unmarshal([]byte(podBytes), &pods); err != nil {
		return err
	}
	var statuses []corev1.ContainerStatus
	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "operator" {
				statuses = append(statuses, status)
			}
		}
	}
	if len(statuses) != 1 || !statuses[0].Ready || statuses[0].RestartCount != 0 || statuses[0].State.Running == nil || !strings.HasPrefix(statuses[0].ContainerID, "containerd://") {
		return errors.New("operator must be a single ready running container with zero restarts")
	}
	processBytes, err := read("operator-process.txt")
	if err != nil {
		return err
	}
	process, err := verifyProcess(processBytes, strings.TrimPrefix(statuses[0].ContainerID, "containerd://"), binarySHA)
	if err != nil {
		return err
	}
	operatorBytes, err := read("operator-cgroup.txt")
	if err != nil {
		return err
	}
	peak, err := verifyCgroup(operatorBytes, "20000 100000", "134217728")
	if err != nil {
		return fmt.Errorf("operator: %w", err)
	}
	nodeBytes, err := read("node-cgroup.txt")
	if err != nil {
		return err
	}
	if _, err := verifyCgroup(nodeBytes, "400000 100000", "4294967296"); err != nil {
		return fmt.Errorf("node: %w", err)
	}
	report := struct {
		Process processMeasurement `json:"process"`
		Peak    uint64             `json:"operator_memory_peak_bytes"`
	}{process, peak}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "resource-verification.json"), append(raw, '\n'), 0o600)
}
