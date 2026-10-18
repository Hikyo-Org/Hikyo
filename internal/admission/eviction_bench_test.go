package admission

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// BenchmarkEvictionPressure measures bounded bursts against the full tracking
// sets. Each epoch starts with 4096 IPs and 4096 hashed accounts. Fixture setup
// and latency sorting are untimed. The two workers share the real limiter:
// successful admissions race new-source, unknown-account failure traffic.
// No Argon2id, HTTP, audit persistence or shared datastore is simulated here.
// Use -benchtime=100x (or another fixed epoch count) for reproducible profiles.
func BenchmarkEvictionPressure(b *testing.B) {
	for _, scenario := range []string{"full_live", "partially_expired", "churn"} {
		b.Run(scenario, func(b *testing.B) {
			const burst = 128
			var legitimate, failures []time.Duration
			b.StopTimer()
			for range b.N {
				now := time.Unix(1_800_000_000, 0)
				l, err := New(Config{ArgonMemoryKiB: 64 * 1024, Now: func() time.Time { return now }})
				if err != nil {
					b.Fatal(err)
				}
				for i := range MaxTrackedSubjects {
					at, until := now.Add(-time.Second), now.Add(MaxAccountBackoff)
					if scenario == "partially_expired" && i%4 == 0 {
						at, until = now.Add(-time.Minute), now
					}
					if scenario == "churn" {
						at = now.Add(-time.Duration(i%61) * time.Second)
						until = now.Add(time.Duration(i%61) * time.Second)
					}
					l.ipHits[fmt.Sprintf("seed-ip-%d", i)] = []time.Time{at}
					l.accounts[bucketKey(fmt.Sprintf("seed-account-%d", i))] = accountBackoff{failures: cappedFailures, until: until}
				}
				// Preallocate worker-owned observations so latency recording does not
				// contend or allocate inside the measured flow.
				good, bad := make([]time.Duration, burst), make([]time.Duration, burst)
				var wg sync.WaitGroup
				start := make(chan struct{})
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					for i := range burst {
						began := time.Now()
						// Revisit a seeded source once, within its IP allowance.
						release, err := l.Enter(context.Background(), fmt.Sprintf("seed-ip-%d", MaxTrackedSubjects-1-i))
						if err != nil {
							b.Error(err)
							return
						}
						_ = l.AccountDelay("legitimate")
						l.RecordSuccess("legitimate")
						release()
						good[i] = time.Since(began)
					}
				}()
				go func() {
					defer wg.Done()
					<-start
					for i := range burst {
						began := time.Now()
						ip, account := fmt.Sprintf("new-ip-%d", i), fmt.Sprintf("new-account-%d", i)
						release, err := l.Enter(context.Background(), ip)
						if err != nil {
							b.Error(err)
							return
						}
						_ = l.AccountDelay(account)
						// Six failures reach the first live backoff; subsequent requests use
						// another identifier. This isolates eviction rather than KDF cost.
						for range FailuresBeforeBackoff + 1 {
							l.RecordFailure(account)
						}
						release()
						if scenario == "churn" && i%2 == 0 {
							l.RecordSuccess(account)
						}
						bad[i] = time.Since(began)
					}
				}()
				b.StartTimer()
				close(start)
				wg.Wait()
				b.StopTimer()
				if len(l.ipHits) > MaxTrackedSubjects || len(l.accounts) > MaxTrackedSubjects {
					b.Fatal("tracking bound exceeded")
				}
				legitimate = append(legitimate, good...)
				failures = append(failures, bad...)
			}
			for name, samples := range map[string][]time.Duration{"legitimate": legitimate, "failure": failures} {
				slices.Sort(samples)
				b.ReportMetric(float64(samples[(len(samples)-1)*50/100].Nanoseconds()), name+"-p50-ns")
				b.ReportMetric(float64(samples[(len(samples)-1)*99/100].Nanoseconds()), name+"-p99-ns")
			}
			b.ReportMetric(2*burst, "flows/epoch")
		})
	}
}
