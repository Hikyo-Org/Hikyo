package server

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestTransitStateRejectsInvalidDelayBeforeService(t *testing.T) {
	for _, seconds := range []int64{-1, math.MinInt64, -18_446_657_674, int64(service.MaxTransitDeletionDelay/time.Second) + 1, math.MaxInt64} {
		api := &API{}
		_, err := api.ChangeTransitKeyState(t.Context(), apigen.ChangeTransitKeyStateRequestObject{Body: &apigen.ChangeTransitKeyStateJSONRequestBody{DelaySeconds: &seconds}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("delay %d: got %v, want ErrInvalid", seconds, err)
		}
	}
}
