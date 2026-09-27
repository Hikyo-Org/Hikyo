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
		api := &API{Transit: &service.Transit{}}
		_, err := api.ChangeTransitKeyState(t.Context(), apigen.ChangeTransitKeyStateRequestObject{Body: &apigen.ChangeTransitKeyStateJSONRequestBody{DelaySeconds: &seconds}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("delay %d: got %v, want ErrInvalid", seconds, err)
		}
	}
}

func TestTransitHandlersWithoutServiceReturnNotFound(t *testing.T) {
	api := &API{}
	t.Run("ListTransitKeys", func(t *testing.T) {
		_, err := api.ListTransitKeys(t.Context(), apigen.ListTransitKeysRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("CreateTransitKey", func(t *testing.T) {
		_, err := api.CreateTransitKey(t.Context(), apigen.CreateTransitKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("ShowTransitKey", func(t *testing.T) {
		_, err := api.ShowTransitKey(t.Context(), apigen.ShowTransitKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("ConfigureTransitKey", func(t *testing.T) {
		_, err := api.ConfigureTransitKey(t.Context(), apigen.ConfigureTransitKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("RotateTransitKey", func(t *testing.T) {
		_, err := api.RotateTransitKey(t.Context(), apigen.RotateTransitKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("ChangeTransitKeyState", func(t *testing.T) {
		_, err := api.ChangeTransitKeyState(t.Context(), apigen.ChangeTransitKeyStateRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TrimTransitKey", func(t *testing.T) {
		_, err := api.TrimTransitKey(t.Context(), apigen.TrimTransitKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitEncrypt", func(t *testing.T) {
		_, err := api.TransitEncrypt(t.Context(), apigen.TransitEncryptRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitDecrypt", func(t *testing.T) {
		_, err := api.TransitDecrypt(t.Context(), apigen.TransitDecryptRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitRewrap", func(t *testing.T) {
		_, err := api.TransitRewrap(t.Context(), apigen.TransitRewrapRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitDataKey", func(t *testing.T) {
		_, err := api.TransitDataKey(t.Context(), apigen.TransitDataKeyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitSign", func(t *testing.T) {
		_, err := api.TransitSign(t.Context(), apigen.TransitSignRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitVerify", func(t *testing.T) {
		_, err := api.TransitVerify(t.Context(), apigen.TransitVerifyRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitHMAC", func(t *testing.T) {
		_, err := api.TransitHMAC(t.Context(), apigen.TransitHMACRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
	t.Run("TransitVerifyHMAC", func(t *testing.T) {
		_, err := api.TransitVerifyHMAC(t.Context(), apigen.TransitVerifyHMACRequestObject{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("got %v, want not-found", err)
		}
	})
}
