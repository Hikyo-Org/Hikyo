package sealedreceiver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

// The protocol behavior of the reference receiver is exercised end to end in
// internal/adapter/sealedwebhook and internal/isolation; these tests pin the
// package's own contract: which versions it serves and that State is a copy.

func v2Config(t *testing.T) Config {
	t.Helper()
	identity, _, err := sealedhook.GenerateHybridRecipient()
	if err != nil {
		t.Fatal(err)
	}
	edPEM, _, _ := sealedhook.GenerateSigningKey()
	ed, _ := sealedhook.ParseSigningKeyPEM(edPEM)
	pqPEM, _, _ := sealedhook.GenerateMLDSASigningKey()
	pq, _ := sealedhook.ParseMLDSASigningKeyPEM(pqPEM)
	acker, _ := sealedhook.NewSignerV2(ed, pq)
	return Config{
		HybridIdentity: identity, AckSignerV2: acker, TargetID: "payments", InstanceID: "hikyo", Generation: 1,
		SendersV2: []sealedhook.PinnedSenderV2{{Ed: ed.Public(), PQ: pq.Public()}},
	}
}

func TestNewRequiresAVersionAndItsMaterial(t *testing.T) {
	good := v2Config(t)
	if _, err := New(good); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*Config){
		"no identity at all":          func(c *Config) { c.HybridIdentity = "" },
		"no version 2 ack signer":     func(c *Config) { c.AckSignerV2 = nil },
		"no version 2 sender pairs":   func(c *Config) { c.SendersV2 = nil },
		"no generation":               func(c *Config) { c.Generation = 0 },
		"a version 1 identity alone":  func(c *Config) { c.Identity = "AGE-SECRET-KEY-1x" },
		"a classical hybrid identity": func(c *Config) { c.HybridIdentity = "AGE-SECRET-KEY-1x" },
	} {
		t.Run(name, func(t *testing.T) {
			c := good
			edit(&c)
			if _, err := New(c); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestAPQRequiredReceiverDoesNotServeVersionOne(t *testing.T) {
	r, err := New(v2Config(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{sealedhook.SyncPath, "/hikyo/v3/sync", sealedhook.SyncPathV2 + "?x=1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", sealedhook.ContentTypeV2)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPathV2, strings.NewReader("{}"))
	req.Header.Set("Content-Type", sealedhook.ContentType) // version 1 media type on the version 2 path
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong media type answered %d", rec.Code)
	}
}

func TestStateIsACopy(t *testing.T) {
	r, err := New(v2Config(t))
	if err != nil {
		t.Fatal(err)
	}
	r.Seed("prod", "secret", "A", "1")
	state := r.State()
	state.Values["prod"]["secret/A"] = Entry{Value: "mutated"}
	state.Seen["k"] = outcome{}
	if r.Snapshot("prod")["secret/A"].Value != "1" || len(r.State().Seen) != 0 {
		t.Fatal("State returned a view into the live store")
	}
}
