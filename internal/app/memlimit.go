package app

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/config"
)

// cgroupMemoryFiles are the container memory ceilings, cgroup v2 then v1.
// shortcut: reads only the process's root cgroup view (a container with a cgroup
// namespace, the common case); a host service capped by a nested cgroup such as
// systemd MemoryMax= is not detected and needs GOMEMLIMIT set explicitly.
var cgroupMemoryFiles = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// baselineMiB is the server's measured working set beside admitted Argon2id
// work: about 76 MiB idle and 148 MiB after one derivation.
const baselineMiB = 128

// applyMemoryLimit gives the Go runtime the container's memory ceiling when the
// operator has not set GOMEMLIMIT, so the collector reclaims garbage from
// admitted Argon2id derivations before the process reaches the cgroup limit.
// Admission bounds live work, not the garbage it leaves.
func applyMemoryLimit(cfg *config.Config, log *slog.Logger) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return // the runtime already applied the operator's value
	}
	ceiling, ok := cgroupMemoryCeiling()
	if !ok {
		log.Info("no container memory limit detected; set GOMEMLIMIT if the process runs under one")
		return
	}
	// A fifth stays outside the Go limit for memory the runtime does not
	// count: the mapped binary and SQLite's own allocations.
	limit := ceiling - ceiling/5
	debug.SetMemoryLimit(limit)
	log.Info("go memory limit set from the container memory limit", "container_bytes", ceiling, "limit_bytes", limit)
	budget := cfg.AdmissionBudgetMiB
	if budget <= 0 {
		budget = admission.DefaultBudgetMiB
	}
	if need := int64(budget+baselineMiB) << 20; limit < need {
		log.Warn("container memory limit cannot hold the pre-authentication admission budget plus the working set",
			"limit_bytes", limit, "needed_bytes", need, "admission_budget_mib", budget)
	}
}

func cgroupMemoryCeiling() (int64, bool) {
	for _, path := range cgroupMemoryFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseCgroupMemory(string(raw))
	}
	return 0, false
}

// parseCgroupMemory reads one ceiling file. "max" (v2) and the near-MaxInt64
// page-rounded sentinel (v1) both mean unlimited.
func parseCgroupMemory(raw string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 || value >= 1<<62 {
		return 0, false
	}
	return value, true
}
