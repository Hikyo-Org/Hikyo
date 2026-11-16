package cli

import (
	"regexp"
	"strconv"
	"time"
)

var daysDuration = regexp.MustCompile(`^([0-9]{1,6})d$`)

// durationSeconds parses a duration flag into whole seconds, truncating
// fractional seconds. It accepts everything time.ParseDuration does plus a
// whole-number day suffix such as 90d (24 hours each), the unit the rotation
// bounds are stated in. With allowZero the literal "0" is zero (the flag's
// "off"); otherwise anything below one second, and anything malformed, is an
// ExitUsage error naming the flag. The server owns every range bound.
func durationSeconds(flagName, raw string, allowZero bool) (int64, error) {
	if raw == "0" && allowZero {
		return 0, nil
	}
	d, err := parseDuration(raw)
	if err != nil || d < time.Second {
		return 0, failf(ExitUsage, "%s must be a duration of at least one second, such as 30m, 720h or 90d", flagName)
	}
	return int64(d / time.Second), nil
}

func parseDuration(raw string) (time.Duration, error) {
	if m := daysDuration.FindStringSubmatch(raw); m != nil {
		days, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(raw)
}
