package server

import (
	"bytes"
	"crypto/mldsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
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

// The public key a response carries is the published encoding: Ed25519 raw,
// ML-DSA-65 as SPKI (DEC-07). The public surface does not offer ML-DSA keys yet
// (HOLD), so this exercises the response builder directly.
func TestTransitKeyResponsePublishesTheAlgorithmEncoding(t *testing.T) {
	// The view is built by field assignment: the handler layer must not import the
	// datastore package, and that rule applies to its tests too.
	view := func(alg string, public []byte) service.TransitKeyView {
		var v service.TransitKeyView
		v.ID, v.Algorithm, v.Custody, v.State, v.LatestVersion = "tk", alg, "software", "active", 1
		v.CreatedAt, v.UpdatedAt = "2026-11-03T00:00:00Z", "2026-11-03T00:00:00Z"
		v.Versions = slices.Grow(v.Versions, 1)[:1]
		v.Versions[0].Version, v.Versions[0].PublicKey, v.Versions[0].HasMaterial = 1, public, true
		v.Versions[0].CreatedAt = "2026-11-03T00:00:00Z"
		return v
	}
	k, err := crypto.OpenTransitKey(make([]byte, crypto.TransitMaterialSize),
		crypto.TransitBinding{Algorithm: crypto.TransitMLDSA65, OrgID: "o", ProjectID: "p", EnvID: "e", KeyID: "tk", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := k.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	out, err := transitKeyResponse(view("mldsa65", raw))
	if err != nil || out.Versions[0].PublicKey == nil {
		t.Fatalf("response: %v", err)
	}
	der, err := base64.StdEncoding.DecodeString(*out.Versions[0].PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("published ML-DSA public key is not SPKI: %v", err)
	}
	if pk, ok := parsed.(*mldsa.PublicKey); !ok || !bytes.Equal(pk.Bytes(), raw) {
		t.Fatal("published SPKI is not the stored key")
	}
	edRaw := bytes.Repeat([]byte{7}, 32)
	out, err = transitKeyResponse(view("ed25519", edRaw))
	if err != nil || *out.Versions[0].PublicKey != base64.StdEncoding.EncodeToString(edRaw) {
		t.Fatalf("ed25519 public key changed on the wire: %v", err)
	}
	// A stored key of the wrong shape fails visibly rather than being published.
	if _, err := transitKeyResponse(view("mldsa65", edRaw)); err == nil {
		t.Fatal("a malformed stored ML-DSA key was published")
	}
	if _, err := transitKeyResponse(view("mldsa87", raw)); err == nil {
		t.Fatal("an unknown algorithm published a key")
	}
}
