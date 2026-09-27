package server

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestSecondsRefusesOverflow(t *testing.T) {
	if d, err := seconds(86400); err != nil || d != 24*time.Hour {
		t.Fatalf("seconds(86400) = %v, %v", d, err)
	}
	for _, v := range []int64{-1, math.MaxInt64/int64(time.Second) + 1, 18_446_744_074} {
		if _, err := seconds(v); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("seconds(%d) err = %v, want ErrInvalid", v, err)
		}
	}
}
