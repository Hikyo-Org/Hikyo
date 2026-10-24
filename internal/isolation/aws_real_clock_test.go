package isolation

import (
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAWSRealDrainClock(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		clockOffset time.Duration
		dueOffset   time.Duration
	}{
		{name: "wall-time", clockOffset: -time.Minute},
		{name: "future-replay", clockOffset: time.Hour, dueOffset: time.Hour},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				seedDeadlineTargets(t, db)
				now := time.Now().UTC()
				due := now.Add(scenario.dueOffset).Format("2006-01-02T15:04:05.000000Z")
				execRaw(t, db, fmt.Sprintf(`UPDATE adapter_outbox SET next_attempt_at='%s' WHERE id IN ('deadline_a','deadline_b')`, due))

				// Exercise the real claim, authorization and settlement paths,
				// without an AWS client or network-dependent latency. Wall-time
				// enqueue can overtake a fixture clock during provider calls;
				// an explicit future clock still models durable replay waits.
				module := &authorityProbeModule{}
				h := &realAWSDurable{t: t, db: db, clock: now.Add(scenario.clockOffset)}
				h.worker = &adapter.Worker{
					Store:  generatedAdapterRuntime(db),
					Loader: deadlineRuntimeLoader{module: module},
					ID:     "real-aws-clock",
					Now:    func() time.Time { return h.clock },
				}
				h.drain()

				completed := queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id IN ('deadline_a','deadline_b') AND state='succeeded' AND attempt_count=1`)
				if completed != 2 || module.calls != 2 {
					t.Fatalf("drain completed %d jobs and made %d module calls, want 2 each", completed, module.calls)
				}
			})
		})
	}
}
