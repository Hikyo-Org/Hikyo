package encryptedreceiver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/encryptedhook"
)

// The protocol behavior of the reference receiver is exercised end to end in
// internal/adapter/encryptedwebhook and internal/isolation; these tests pin the
// package's own contract: which versions it serves and that State is a copy.

func v2Config(t *testing.T) Config {
	t.Helper()
	identity, _, err := encryptedhook.GenerateHybridRecipient()
	if err != nil {
		t.Fatal(err)
	}
	edPEM, _, _ := encryptedhook.GenerateSigningKey()
	ed, _ := encryptedhook.ParseSigningKeyPEM(edPEM)
	pqPEM, _, _ := encryptedhook.GenerateMLDSASigningKey()
	pq, _ := encryptedhook.ParseMLDSASigningKeyPEM(pqPEM)
	acker, _ := encryptedhook.NewSignerV2(ed, pq)
	return Config{
		HybridIdentity: identity, AckSignerV2: acker, TargetID: "payments", InstanceID: "hikyo", Generation: 1,
		SendersV2: []encryptedhook.PinnedSenderV2{{Ed: ed.Public(), PQ: pq.Public()}},
	}
}

func TestNewRequiresAVersionAndItsMaterial(t *testing.T) {
	good := v2Config(t)
	if _, err := New(good); err != nil {
		t.Fatal(err)
	}
	// Real, parseable classical material, so each refusal below is the
	// receiver's own rule and not a parse error.
	identityV1, _, err := encryptedhook.GenerateRecipient()
	if err != nil {
		t.Fatal(err)
	}
	edPEM, _, _ := encryptedhook.GenerateSigningKey()
	ed, _ := encryptedhook.ParseSigningKeyPEM(edPEM)
	pinnedV1 := []encryptedhook.PinnedKey{{Key: ed.Public()}}
	// A complete version 1 receiver is accepted too.
	v1Only := Config{Identity: identityV1, AckSigner: ed, Senders: pinnedV1, TargetID: "payments", InstanceID: "hikyo", Generation: 1}
	if _, err := New(v1Only); err != nil {
		t.Fatalf("a complete version 1 receiver was refused: %v", err)
	}
	for name, cfg := range map[string]func() Config{
		"no identity at all":                       func() Config { c := good; c.HybridIdentity = ""; return c },
		"no version 2 ack signer":                  func() Config { c := good; c.AckSignerV2 = nil; return c },
		"no version 2 sender pairs":                func() Config { c := good; c.SendersV2 = nil; return c },
		"no generation":                            func() Config { c := good; c.Generation = 0; return c },
		"version 1 identity without an ack signer": func() Config { c := v1Only; c.AckSigner = nil; return c },
		"version 1 identity without sender pins":   func() Config { c := v1Only; c.Senders = nil; return c },
		"a classical identity in the hybrid slot": func() Config {
			c := good
			c.HybridIdentity = identityV1
			return c
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg()); err == nil {
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
	for _, path := range []string{encryptedhook.SyncPath, "/hikyo/v3/sync", encryptedhook.SyncPathV2 + "?x=1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", encryptedhook.ContentTypeV2)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, encryptedhook.SyncPathV2, strings.NewReader("{}"))
	req.Header.Set("Content-Type", encryptedhook.ContentType) // version 1 media type on the version 2 path
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
