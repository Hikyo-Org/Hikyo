package benchmark

import (
	"fmt"
	"math"
	"time"
)

// The forecast uses a rolling window, not an assumed provider reset date.
// Admission always reserves a complete job against actual 32-day spending.
type forecast struct {
	now, observedSince                              time.Time
	dailyMinutes, dailyJobs, recentRequestedMinutes int
}

func (f *forecast) add(j job, minutes int, daily bool) {
	if minutes == 0 {
		return
	}
	if daily {
		f.dailyMinutes += minutes
		f.dailyJobs++
	}
	since := maxTime(*j.StartedAt, f.now.Add(-7*24*time.Hour))
	if since.Before(f.observedSince) {
		f.observedSince = since
	}
	if !daily && !j.CompletedAt.Before(f.now.Add(-7*24*time.Hour)) {
		f.recentRequestedMinutes += minutes
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (f forecast) projectedMinutes() int {
	// Reserve daily coverage even before the first successful calibration.
	// Measured costs already include whole-job rounding and a minute of margin.
	dailyCost := float64(reservation)
	if f.dailyJobs > 0 {
		dailyCost = float64(f.dailyMinutes) / float64(f.dailyJobs)
	}
	days := max(1, f.now.Sub(f.observedSince).Hours()/24)
	return int(math.Ceil(32*dailyCost+32*float64(f.recentRequestedMinutes)/days)) + reservation
}

func admitAutomatic(u usage, f forecast) (bool, string) {
	if allowed, reason := admit(u, "workflow_dispatch", false); !allowed {
		return false, reason
	}
	projected := f.projectedMinutes()
	if projected > monthlyLimit {
		return false, fmt.Sprintf("projected 32-day usage is %d/%d minutes; daily coverage and another complete job are included", projected, monthlyLimit)
	}
	return true, fmt.Sprintf("projected 32-day usage is %d/%d minutes", projected, monthlyLimit)
}
