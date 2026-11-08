package app

import (
	"bytes"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func TestParseCgroupMemory(t *testing.T) {
	for raw, want := range map[string]int64{
		"536870912\n":           536870912,
		"max\n":                 0, // cgroup v2 unlimited
		"9223372036854771712\n": 0, // cgroup v1 unlimited sentinel
		"":                      0,
		"garbage":               0,
		"0":                     0,
	} {
		got, ok := parseCgroupMemory(raw)
		if got != want || ok != (want != 0) {
			t.Errorf("parseCgroupMemory(%q) = %d, %v; want %d", raw, got, ok, want)
		}
	}
}

func TestApplyMemoryLimit(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	saved := cgroupMemoryFiles
	t.Cleanup(func() { cgroupMemoryFiles = saved })
	ceilingFile := filepath.Join(t.TempDir(), "memory.max")
	cgroupMemoryFiles = []string{filepath.Join(t.TempDir(), "absent"), ceilingFile}

	for _, tc := range []struct {
		name, gomemlimit, ceiling string
		budgetMiB                 int
		wantLimit                 int64
		wantWarn                  bool
	}{
		{name: "operator_value_wins", gomemlimit: "400MiB", ceiling: "536870912", wantLimit: math.MaxInt64},
		{name: "unlimited", ceiling: "max", wantLimit: math.MaxInt64},
		{name: "fits_default_budget", ceiling: "536870912", wantLimit: 429496730},
		{name: "too_small_for_budget", ceiling: "402653184", wantLimit: 322122548, wantWarn: true},
		{name: "configured_budget", ceiling: "536870912", budgetMiB: 512, wantLimit: 429496730, wantWarn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			debug.SetMemoryLimit(math.MaxInt64)
			t.Setenv("GOMEMLIMIT", tc.gomemlimit)
			if err := os.WriteFile(ceilingFile, []byte(tc.ceiling+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			applyMemoryLimit(&config.Config{AdmissionBudgetMiB: tc.budgetMiB}, slog.New(slog.NewTextHandler(&logs, nil)))
			if got := debug.SetMemoryLimit(-1); got != tc.wantLimit {
				t.Errorf("memory limit = %d, want %d", got, tc.wantLimit)
			}
			if warned := strings.Contains(logs.String(), "level=WARN"); warned != tc.wantWarn {
				t.Errorf("warned = %v, want %v: %s", warned, tc.wantWarn, logs.String())
			}
		})
	}
}
