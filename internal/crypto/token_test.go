package crypto

import (
	"bytes"
	"context"
	"testing"
)

type keyCaptureReader struct {
	key []byte
}

func (r *keyCaptureReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0xA5
	}
	if len(p) == KeySize && r.key == nil {
		r.key = p
	}
	return len(p), nil
}

func TestScopedTokenFamiliesPreserveGoldenVectorsAndScopeSeparation(t *testing.T) {
	t.Parallel()

	// Fixed root, scope and payload make these known-answer vectors protocol
	// sentinels: changing a label or field encoding changes the literal output.
	newKeyring := func() *Keyring {
		kr := &Keyring{}
		kr.token.adopt(keyHandle{key: bytes.Repeat([]byte{0x42}, KeySize)})
		return kr
	}
	type tokenFn func(*Keyring, string, string, string, []byte) (string, error)
	tests := []struct {
		name string
		tag  tokenFn
		want string
	}{
		{name: "change token", tag: (*Keyring).ChangeToken, want: "v1:poH7aqEWVkpCymIJegWYLoe2hA7Bip7csZ01BkgMyDA"},
		{name: "delivery cursor", tag: (*Keyring).DeliveryCursor, want: "v1:4_dpQeRHtYsAhf1FiIZWqjKtXvRyY90a90MD8SNuJGE"},
		{name: "occurrence token", tag: (*Keyring).OccurrenceToken, want: "v1:AMuqmqeGm-zZv8roG7AuxyQyQjcDPr2mswE482p7KtY"},
		{name: "publish preview", tag: (*Keyring).PublishPreviewToken, want: "v1:wt3UZgkb_Ep1Ehs_GLEZt0PThYFi1Xfk--sft1gdfoI"},
	}
	seen := make(map[string]string, len(tests))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kr := newKeyring()
			got, err := tt.tag(kr, "org_1", "project_1", "production", []byte("canonical payload"))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("golden token = %q, want %q", got, tt.want)
			}
			if otherPurpose, ok := seen[got]; ok {
				t.Errorf("same scope and payload matched %s token", otherPurpose)
			}
			seen[got] = tt.name

			otherScope, err := tt.tag(kr, "org_2", "project_1", "production", []byte("canonical payload"))
			if err != nil {
				t.Fatal(err)
			}
			if otherScope == got {
				t.Error("different organizations produced the same token")
			}

			left, err := tt.tag(kr, "a", "bc", "production", []byte("canonical payload"))
			if err != nil {
				t.Fatal(err)
			}
			right, err := tt.tag(kr, "ab", "c", "production", []byte("canonical payload"))
			if err != nil {
				t.Fatal(err)
			}
			if left == right {
				t.Error("boundary-shifted scopes produced the same token")
			}
		})
	}
}

func TestTokenKeyRotationAdoptIsMonotonic(t *testing.T) {
	ctx := context.Background()
	kr, err := LoadKeyring(ctx, newMemStore(), newRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	const payload = "canonical payload"

	staleV2, adoptStaleV2, abortStaleV2, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abortStaleV2()
	v2, adoptV2, abortV2, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abortV2()
	if staleV2.Version != v2.Version {
		t.Fatalf("concurrent candidates = versions %d and %d, want same predecessor", staleV2.Version, v2.Version)
	}
	adoptV2()
	tokenV2, err := kr.ChangeToken("org_1", "project_1", "production", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}

	v3, adoptV3, abortV3, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	defer abortV3()
	if v3.Version <= v2.Version {
		t.Fatalf("successor version = %d, want greater than %d", v3.Version, v2.Version)
	}
	adoptV3()
	tokenV3, err := kr.ChangeToken("org_1", "project_1", "production", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if tokenV3 == tokenV2 {
		t.Fatal("successor rotation did not change token")
	}

	adoptStaleV2()
	afterLateAdopt, err := kr.ChangeToken("org_1", "project_1", "production", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if afterLateAdopt != tokenV3 {
		t.Fatal("late predecessor adopt regressed live token key")
	}
}

func TestDerivationKeyRotationAbortZeroesCandidate(t *testing.T) {
	kr, err := LoadKeyring(context.Background(), newMemStore(), newRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	rnd := &keyCaptureReader{}
	kr.rnd = rnd

	_, _, abort, err := kr.PrepareTokenKeyRotation()
	if err != nil {
		t.Fatal(err)
	}
	if rnd.key == nil {
		t.Fatal("rotation did not draw candidate key material")
	}
	abort()
	if !bytes.Equal(rnd.key, make([]byte, KeySize)) {
		t.Fatal("aborted rotation retained candidate key material")
	}
}

func TestDerivationRefusesUninitializedHandle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("uninitialized derivation key did not panic")
		}
	}()
	_, _ = (&Keyring{}).ChangeToken("org_1", "project_1", "production", []byte("payload"))
}
