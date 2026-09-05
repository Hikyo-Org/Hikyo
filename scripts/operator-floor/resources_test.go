package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func processFixture() (string, string, string) {
	id, hash := strings.Repeat("a", 64), strings.Repeat("b", 64)
	raw := fmt.Sprintf("container_id %s\ncgroup /sys/fs/cgroup/test/cri-containerd-%s.scope\npids_before 123\nstart_time_before 456\nexecutable /hikyo\nexecutable_sha256 %s\ncmdline_base64 %s\nName: hikyo\nPid: 123\nTgid: 123\nVmRSS: 30000 kB\nVmHWM: 40000 kB\nstart_time_after 456\npids_after 123\n", id, id, hash, base64.StdEncoding.EncodeToString([]byte("/hikyo\x00operator\x00")))
	return raw, id, hash
}

func TestOperatorRSSRequiresExactProcessAndStrictBound(t *testing.T) {
	raw, id, hash := processFixture()
	got, err := verifyProcess(raw, id, hash)
	if err != nil || got.PeakRSS != 40000*1024 || got.RSS != 30000*1024 {
		t.Fatalf("valid measurement: %+v %v", got, err)
	}
	below := strings.Replace(raw, "VmHWM: 40000", "VmHWM: 131071", 1)
	if _, err := verifyProcess(below, id, hash); err != nil {
		t.Fatalf("one KiB below threshold: %v", err)
	}
	for name, bad := range map[string]string{
		"missing peak":          strings.Replace(raw, "VmHWM: 40000 kB\n", "", 1),
		"missing RSS":           strings.Replace(raw, "VmRSS: 30000 kB\n", "", 1),
		"zero peak":             strings.Replace(raw, "VmHWM: 40000", "VmHWM: 0", 1),
		"invalid units":         strings.Replace(raw, "VmHWM: 40000 kB", "VmHWM: 40000 bytes", 1),
		"overflow":              strings.Replace(raw, "VmHWM: 40000", "VmHWM: 18446744073709551615", 1),
		"equal limit":           strings.Replace(raw, "VmHWM: 40000", "VmHWM: 131072", 1),
		"above limit":           strings.Replace(raw, "VmHWM: 40000", "VmHWM: 131073", 1),
		"peak below RSS":        strings.Replace(raw, "VmHWM: 40000", "VmHWM: 29999", 1),
		"multiple processes":    strings.Replace(raw, "pids_before 123", "pids_before 123 124", 1),
		"multiline processes":   strings.Replace(raw, "pids_after 123", "pids_after 123\n124", 1),
		"changed PID":           strings.Replace(raw, "pids_after 123", "pids_after 124", 1),
		"changed start":         strings.Replace(raw, "start_time_after 456", "start_time_after 457", 1),
		"wrong status PID":      strings.Replace(raw, "Pid: 123", "Pid: 124", 1),
		"wrong executable":      strings.Replace(raw, "executable /hikyo", "executable /fixture", 1),
		"wrong hash":            strings.Replace(raw, "executable_sha256 "+hash, "executable_sha256 "+id, 1),
		"wrong container":       strings.Replace(raw, "container_id "+id, "container_id "+hash, 1),
		"wrong cgroup":          strings.Replace(raw, "cri-containerd-"+id, "cri-containerd-"+hash, 1),
		"wrong command":         strings.Replace(raw, base64.StdEncoding.EncodeToString([]byte("/hikyo\x00operator\x00")), base64.StdEncoding.EncodeToString([]byte("/hikyo\x00server\x00")), 1),
		"duplicate measurement": raw + "VmHWM: 20000 kB\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifyProcess(bad, id, hash); err == nil {
				t.Fatal("invalid process RSS evidence accepted")
			}
		})
	}
}

func TestOperatorCgroupKeepsLimitsAndRequiresOOMCounters(t *testing.T) {
	// File cache may make this cgroup total temporarily exceed memory.max.
	valid := "20000 100000\n134217728\n0\n134328320\nlow 0\nhigh 0\nmax 64\noom 0\noom_kill 0\n"
	if peak, err := verifyCgroup(valid, "20000 100000", "134217728"); err != nil || peak != 134328320 {
		t.Fatalf("RSS gate must retain honest cgroup diagnostic: %d %v", peak, err)
	}
	for name, bad := range map[string]string{
		"missing events":  "20000 100000\n134217728\n0\n134328320\n",
		"missing oom":     strings.Replace(valid, "oom 0\n", "", 1),
		"missing kill":    strings.Replace(valid, "oom_kill 0\n", "", 1),
		"oom":             strings.Replace(valid, "oom 0", "oom 1", 1),
		"kill":            strings.Replace(valid, "oom_kill 0", "oom_kill 1", 1),
		"invalid event":   strings.Replace(valid, "oom 0", "oom nope", 1),
		"duplicate event": valid + "oom 0\n",
		"swap":            strings.Replace(valid, "\n0\n", "\n1\n", 1),
		"CPU":             strings.Replace(valid, "20000 100000", "max 100000", 1),
		"memory":          strings.Replace(valid, "134217728", "268435456", 1),
		"missing peak":    strings.Replace(valid, "134328320", "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifyCgroup(bad, "20000 100000", "134217728"); err == nil {
				t.Fatal("invalid cgroup evidence accepted")
			}
		})
	}
}

func TestResourceEvidenceCannotPassMissingOrRestartedOperator(t *testing.T) {
	raw, id, hash := processFixture()
	for _, fault := range []string{"none", "restarted", "not-ready", "no-running-state", "missing-process-file", "missing-node-oom"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			status := fmt.Sprintf(`{"items":[{"status":{"containerStatuses":[{"name":"operator","containerID":"containerd://%s","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-09-05T00:00:00Z"}}}]}}]}`, id)
			if fault == "restarted" {
				status = strings.Replace(status, `"restartCount":0`, `"restartCount":1`, 1)
			}
			if fault == "not-ready" {
				status = strings.Replace(status, `"ready":true`, `"ready":false`, 1)
			}
			if fault == "no-running-state" {
				status = strings.Replace(status, `"running"`, `"terminated"`, 1)
			}
			files := map[string]string{"pods.json": status, "operator-process.txt": raw, "operator-cgroup.txt": "20000 100000\n134217728\n0\n134328320\noom 0\noom_kill 0\n", "node-cgroup.txt": "400000 100000\n4294967296\n0\n1000000000\noom 0\noom_kill 0\n"}
			if fault == "missing-process-file" {
				delete(files, "operator-process.txt")
			}
			if fault == "missing-node-oom" {
				files["node-cgroup.txt"] = strings.Replace(files["node-cgroup.txt"], "oom 0\n", "", 1)
			}
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := verifyResources(dir, hash)
			if fault == "none" && err != nil {
				t.Fatal(err)
			}
			if fault != "none" && err == nil {
				t.Fatal("invalid resource evidence accepted")
			}
			if fault != "none" {
				if _, err := os.Stat(filepath.Join(dir, "resource-verification.json")); !os.IsNotExist(err) {
					t.Fatal("failure published resource verdict")
				}
			}
		})
	}
}
