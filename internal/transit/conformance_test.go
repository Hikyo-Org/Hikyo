package transit_test

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/transit"
	"github.com/Hikyo-Org/hikyo/internal/transit/transittest"
)

// memKeys is the smallest crypto.KeyStore that boots a real keyring, so the
// software provider seals under a genuine project DEK in these tests.
type memKeys struct {
	mu     sync.Mutex
	master []crypto.WrappedKey
	tier3  map[string][]crypto.WrappedKey
}

func scope(p crypto.Purpose, org, project string) string {
	return string(p) + "|" + org + "|" + project
}

func (m *memKeys) ActiveMasterWrappers(context.Context) ([]crypto.WrappedKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]crypto.WrappedKey(nil), m.master...), nil
}

func (m *memKeys) ActiveTier3(_ context.Context, p crypto.Purpose, org, project string) (crypto.WrappedKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.tier3[scope(p, org, project)]
	if len(rows) == 0 {
		return crypto.WrappedKey{}, crypto.ErrNoKey
	}
	return rows[len(rows)-1], nil
}

func (m *memKeys) Tier3Versions(_ context.Context, p crypto.Purpose, org, project string) ([]crypto.WrappedKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]crypto.WrappedKey(nil), m.tier3[scope(p, org, project)]...), nil
}

func (m *memKeys) AllOpenableTier3(context.Context) ([]crypto.WrappedKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []crypto.WrappedKey
	for _, rows := range m.tier3 {
		out = append(out, rows...)
	}
	return out, nil
}

func (m *memKeys) CreateHierarchy(_ context.Context, master crypto.WrappedKey, tier3 []crypto.WrappedKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.master) > 0 {
		return crypto.ErrKeyExists
	}
	m.master = []crypto.WrappedKey{master}
	for _, k := range tier3 {
		m.tier3[scope(k.Purpose, k.OrgID, k.ProjectID)] = append(m.tier3[scope(k.Purpose, k.OrgID, k.ProjectID)], k)
	}
	return nil
}

func (m *memKeys) CreateTier3(_ context.Context, k crypto.WrappedKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := scope(k.Purpose, k.OrgID, k.ProjectID)
	if len(m.tier3[s]) > 0 {
		return crypto.ErrKeyExists
	}
	m.tier3[s] = append(m.tier3[s], k)
	return nil
}

func newKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	root := make([]byte, crypto.KeySize)
	if _, err := rand.Read(root); err != nil {
		t.Fatal(err)
	}
	kr, err := crypto.LoadKeyring(t.Context(), &memKeys{tier3: map[string][]crypto.WrappedKey{}}, root)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func target(a crypto.TransitAlgorithm, version uint32) transit.Target {
	return transit.Target{
		Binding: crypto.TransitBinding{
			Algorithm: a, OrgID: "org_1", ProjectID: "prj_1", EnvID: "env_1", KeyID: "tk_1", Version: version,
		},
		VersionRowID: "tkv_1_" + string(rune('0'+version)),
	}
}

// providers returns every custody implementation the suite must hold for. The
// suite is the proof of transit ADR D3's "identical semantics": any behaviour a
// test below pins holds for software and external custody alike.
func providers(t *testing.T) map[string]transit.Custody {
	return map[string]transit.Custody{
		"software": &transit.Software{Keyring: newKeyring(t)},
		"external": transittest.NewExternal(),
	}
}

func TestCustodyConformanceEncryption(t *testing.T) {
	for name, p := range providers(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			tg := target(crypto.TransitXChaCha20Poly1305, 1)
			v, err := p.Create(ctx, tg)
			if err != nil {
				t.Fatal(err)
			}
			if v.PublicKey != nil {
				t.Error("encryption key exposed a public key")
			}
			if (len(v.Sealed) > 0) == (v.ExternalRef != "") {
				t.Fatalf("exactly one of sealed material or external reference must be set: %+v", v)
			}
			rec, err := p.Encrypt(ctx, tg, v, []byte("hello"), []byte("ctx"))
			if err != nil {
				t.Fatal(err)
			}
			pt, err := p.Decrypt(ctx, tg, v, rec, []byte("ctx"))
			if err != nil || string(pt) != "hello" {
				t.Fatalf("round trip = %q, %v", pt, err)
			}
			if _, err := p.Decrypt(ctx, tg, v, rec, []byte("other")); !errors.Is(err, crypto.ErrDecrypt) {
				t.Errorf("wrong context: %v", err)
			}
			// A second version is independent material: its key never opens the
			// first version's ciphertext even when told it is that version.
			tg2 := target(crypto.TransitXChaCha20Poly1305, 2)
			v2, err := p.Create(ctx, tg2)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Decrypt(ctx, tg, v2, rec, []byte("ctx")); err == nil {
				t.Error("version 2 material opened version 1 ciphertext")
			}
			if _, err := p.Sign(ctx, tg, v, []byte("m")); err == nil {
				t.Error("encryption key signed")
			}
		})
	}
}

func TestCustodyConformanceSigningAndMAC(t *testing.T) {
	for name, p := range providers(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			st := target(crypto.TransitEd25519, 1)
			sv, err := p.Create(ctx, st)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := p.Sign(ctx, st, sv, []byte("msg"))
			if err != nil {
				t.Fatal(err)
			}
			if !crypto.VerifyTransitSignature(sv.PublicKey, []byte("msg"), sig) {
				t.Error("signature does not verify under the stored public key")
			}
			if crypto.VerifyTransitSignature(sv.PublicKey, []byte("other"), sig) {
				t.Error("signature verified another message")
			}
			mt := target(crypto.TransitHMACSHA256, 1)
			mv, err := p.Create(ctx, mt)
			if err != nil {
				t.Fatal(err)
			}
			a, err := p.MAC(ctx, mt, mv, []byte("msg"))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := p.MAC(ctx, mt, mv, []byte("msg"))
			if !crypto.EqualTransitMAC(a, b) {
				t.Error("MAC is not deterministic")
			}
			if _, err := p.Encrypt(ctx, mt, mv, []byte("x"), nil); err == nil {
				t.Error("hmac key encrypted")
			}
		})
	}
}

func TestCustodyConformanceMissingMaterial(t *testing.T) {
	for name, p := range providers(t) {
		t.Run(name, func(t *testing.T) {
			_, err := p.Encrypt(t.Context(), target(crypto.TransitXChaCha20Poly1305, 1), transit.Version{}, []byte("x"), nil)
			if !errors.Is(err, transit.ErrMaterialMissing) {
				t.Fatalf("empty version = %v, want ErrMaterialMissing", err)
			}
		})
	}
}

// An unavailable external provider fails closed and does no work; there is no
// path through which software custody answers in its place.
func TestExternalUnavailableFailsClosed(t *testing.T) {
	ctx := t.Context()
	ext := transittest.NewExternal()
	tg := target(crypto.TransitXChaCha20Poly1305, 1)
	v, err := ext.Create(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ext.Encrypt(ctx, tg, v, []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ext.SetAvailable(false)
	before := ext.Calls.Load()
	if _, err := ext.Create(ctx, tg); !errors.Is(err, transit.ErrUnavailable) {
		t.Errorf("create: %v", err)
	}
	if out, err := ext.Encrypt(ctx, tg, v, []byte("x"), nil); !errors.Is(err, transit.ErrUnavailable) || out != nil {
		t.Errorf("encrypt: %v %x", err, out)
	}
	if out, err := ext.Decrypt(ctx, tg, v, rec, nil); !errors.Is(err, transit.ErrUnavailable) || out != nil {
		t.Errorf("decrypt: %v", err)
	}
	if err := ext.Destroy(ctx, tg, v); !errors.Is(err, transit.ErrUnavailable) {
		t.Errorf("destroy: %v", err)
	}
	if ext.Calls.Load() != before {
		t.Error("unavailable provider performed work")
	}
	// The registry resolves by recorded kind only: with no external provider
	// registered, an external key is unavailable, never served by software.
	reg := transit.NewRegistry(&transit.Software{Keyring: newKeyring(t)})
	if _, err := reg.Resolve(transit.CustodyExternal); !errors.Is(err, transit.ErrUnavailable) {
		t.Errorf("unregistered external kind resolved: %v", err)
	}
	ext.SetAvailable(true)
	if err := ext.Destroy(ctx, tg, v); err != nil {
		t.Fatal(err)
	}
	if ext.Holds(v.ExternalRef) {
		t.Error("destroy left the material")
	}
	if _, err := ext.Decrypt(ctx, tg, v, rec, nil); !errors.Is(err, transit.ErrMaterialMissing) {
		t.Errorf("decrypt after destroy: %v", err)
	}
}

// The software provider's material is a project_field envelope anchored to the
// version row: presenting it for another row or another key refuses.
func TestSoftwareMaterialIsRowBound(t *testing.T) {
	ctx := t.Context()
	p := &transit.Software{Keyring: newKeyring(t)}
	tg := target(crypto.TransitXChaCha20Poly1305, 1)
	v, err := p.Create(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	if v.SealedDEKVersion == 0 {
		t.Error("sealed DEK version not reported for the writer fence")
	}
	moved := tg
	moved.VersionRowID = "tkv_other"
	if _, err := p.Encrypt(ctx, moved, v, []byte("x"), nil); err == nil {
		t.Error("material transplanted to another version row opened")
	}
	otherKey := tg
	otherKey.Binding.KeyID = "tk_2"
	if _, err := p.Encrypt(ctx, otherKey, v, []byte("x"), nil); err == nil {
		t.Error("material transplanted to another key opened")
	}
}

func TestParseCustodyKind(t *testing.T) {
	for _, ok := range []string{"software", "external"} {
		if _, err := transit.ParseCustodyKind(ok); err != nil {
			t.Error(err)
		}
	}
	if _, err := transit.ParseCustodyKind("hsm"); err == nil {
		t.Error("unknown custody accepted")
	}
}
