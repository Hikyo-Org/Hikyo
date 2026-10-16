package dynamic

import (
	"fmt"
	"math"
	"time"
)

// MaxTTLSeconds is the largest whole-second TTL representable by time.Duration.
const MaxTTLSeconds int64 = math.MaxInt64 / int64(time.Second)

// TTLDuration validates a positive lease TTL before converting seconds to a
// duration. Callers handle an omitted renewal ceiling before calling this helper.
func TTLDuration(seconds int64) (time.Duration, error) {
	if seconds <= 0 || seconds > MaxTTLSeconds {
		return 0, fmt.Errorf("dynamic: max_ttl_seconds must be between 1 and %d", MaxTTLSeconds)
	}
	return time.Duration(seconds) * time.Second, nil
}
