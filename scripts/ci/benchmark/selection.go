package benchmark

import (
	"fmt"
	"strings"
)

// Workloads are bound into the bot's exact-head/run/attempt authorization. Only
// these fixed commands can become workflow outputs; paths never become shell.
type workloads uint8

const (
	workflowReadsWrites workloads = 1 << iota
	deliveryPolling
	secretScanning
	routerAdmission
	browserMatrix
	allGoWorkloads = workflowReadsWrites | deliveryPolling | secretScanning | routerAdmission
	allPRWorkloads = allGoWorkloads | browserMatrix
)

func (w workloads) goMeasurement() bool      { return w == 0 || w&allGoWorkloads != 0 }
func (w workloads) browserMeasurement() bool { return w == 0 || w&browserMatrix != 0 }

func (w workloads) command() string {
	// Zero is the legacy authorization format. Honor already-admitted requests
	// with their original complete suite rather than silently changing scope.
	if w == 0 {
		return "go test -bench=. ./internal/crypto/ ./internal/dotenv/ ./internal/scanning/ ./internal/server/ ./internal/service/"
	}
	var commands []string
	if w&workflowReadsWrites != 0 {
		commands = append(commands, "go test -bench='^Benchmark(ValuesList|RevisionExport|EnvironmentSignals|PublishSingleDraftLargeProject|ImportMixedExisting|HistoryPageLongLived|RevisionDiffSparseLargeSnapshot|AuditBulkDisclosure300)$' ./internal/service/")
	}
	if w&deliveryPolling != 0 {
		commands = append(commands, "go test -bench='^BenchmarkDeliveryPoll$/^sqlite$/^(typical|render-limit)$/^full$/^(changed|unchanged)$' ./internal/service/")
	}
	if w&secretScanning != 0 {
		commands = append(commands, "go test -bench='^BenchmarkScan$' ./internal/scanning/")
	}
	if w&routerAdmission != 0 {
		commands = append(commands, "go test -bench='^BenchmarkAuthenticatedAPIRequests$' ./internal/server/")
	}
	return strings.Join(commands, "\n")
}

func (w workloads) description() string {
	if w == 0 {
		return "legacy complete Go suite and browser Matrix"
	}
	var names []string
	for _, group := range []struct {
		bit  workloads
		name string
	}{
		{workflowReadsWrites, "service read/write workflows"},
		{deliveryPolling, "representative delivery polls"},
		{secretScanning, "64 KiB secret scan"},
		{routerAdmission, "router admission overhead"},
		{browserMatrix, "browser Matrix"},
	} {
		if w&group.bit != 0 {
			names = append(names, group.name)
		}
	}
	return strings.Join(names, ", ")
}

func pathWorkloads(name string) workloads {
	if name == "" || strings.HasSuffix(name, ".md") {
		return 0
	}
	switch {
	case name == ".nvmrc", name == "scripts/ci/install-corepack.sh", name == ".github/workflows/matrix-performance.yml":
		return browserMatrix
	case strings.HasPrefix(name, "web/"), strings.HasPrefix(name, "clients/ts/"):
		if strings.Contains(name, ".stories.") || strings.Contains(name, ".test.") {
			return 0
		}
		return browserMatrix
	case strings.HasPrefix(name, "api/"):
		return allPRWorkloads
	case strings.HasPrefix(name, "internal/service/"):
		// The public router also invokes service.TrackCallerActivity and
		// ResolvedCallerActivity. Direct service fixtures do not cover them.
		return workflowReadsWrites | deliveryPolling | routerAdmission
	case strings.HasPrefix(name, "internal/server/"):
		return routerAdmission
	case strings.HasPrefix(name, "internal/scanning/"):
		// Scanning also runs inside the real mixed-import workflow.
		return workflowReadsWrites | secretScanning
	case strings.HasPrefix(name, "internal/dotenv/"):
		return workflowReadsWrites | deliveryPolling
	case name == "go.mod", name == "go.sum", name == "sqlc.yaml", strings.HasPrefix(name, "internal/"), strings.HasPrefix(name, "cmd/"):
		// Shared authorization, crypto, store, schema, fixtures and unknown
		// runtime owners are deliberately conservative across Go consumers.
		return allGoWorkloads
	case strings.HasPrefix(name, "scripts/bench/"), strings.HasPrefix(name, "scripts/ci/benchmark/"),
		strings.HasPrefix(name, "scripts/ci/codspeed-"), strings.HasPrefix(name, "scripts/ci/pr-benchmark-control/"),
		name == ".github/workflows/codspeed.yml", name == ".github/workflows/pr-benchmark.yml", name == ".github/workflows/benchmark-control.yml":
		return allPRWorkloads
	default:
		return 0
	}
}

func changedWorkloads(repository string, pr pull) (workloads, error) {
	if pr.ChangedFiles <= 0 || pr.ChangedFiles > 3000 {
		return 0, fmt.Errorf("cannot classify %d changed files safely; request the benchmark after the complete file list is available", pr.ChangedFiles)
	}
	var pages [][]changedFile
	if err := api(fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", repository, pr.Number), &pages); err != nil {
		return 0, err
	}
	var selected workloads
	count := 0
	for _, page := range pages {
		for _, file := range page {
			if file.Filename == "" {
				return 0, fmt.Errorf("changed file has no name")
			}
			count++
			selected |= pathWorkloads(file.Filename) | pathWorkloads(file.PreviousFilename)
		}
	}
	if count != pr.ChangedFiles {
		return 0, fmt.Errorf("incomplete changed files: received %d of %d", count, pr.ChangedFiles)
	}
	return selected, nil
}
