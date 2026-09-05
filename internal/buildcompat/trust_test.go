package buildcompat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestProductionTrustRequiresExactImmutableStamp(t *testing.T) {
	oldRoot, oldKey := encodedTrustRoot, encodedRecoveryPublicKey
	t.Cleanup(func() { encodedTrustRoot, encodedRecoveryPublicKey = oldRoot, oldKey })
	fixture := testfixture.New(t)
	stamp := func() {
		encodedTrustRoot = base64.StdEncoding.EncodeToString(fixture.Pinned.Root)
		encodedRecoveryPublicKey = base64.StdEncoding.EncodeToString(fixture.Pinned.RecoveryPublicKey)
	}
	cases := map[string]func(){
		"unstamped":           func() { encodedTrustRoot = "" },
		"missing key":         func() { encodedRecoveryPublicKey = "" },
		"invalid base64":      func() { encodedTrustRoot = "!" },
		"noncanonical base64": func() { encodedTrustRoot += "\n" },
		"hash-matching invalid key": func() {
			var root releasetrust.Root
			if err := json.Unmarshal(fixture.Pinned.Root, &root); err != nil {
				t.Fatal(err)
			}
			raw := []byte("not a public key")
			root.Recovery.SHA256 = string(releaseidentity.Hash(raw))
			encodedTrustRoot = base64.StdEncoding.EncodeToString(testfixture.JSON(t, root))
			encodedRecoveryPublicKey = base64.StdEncoding.EncodeToString(raw)
		},
		"oversized": func() {
			encodedTrustRoot = strings.Repeat("A", base64.StdEncoding.EncodedLen(releasetrust.MaxDocumentBytes)+1)
		},
		"key substitution": func() {
			encodedRecoveryPublicKey = base64.StdEncoding.EncodeToString(testfixture.New(t).Pinned.RecoveryPublicKey)
		},
		"duplicate root member": func() {
			raw := bytes.Replace(fixture.Pinned.Root, []byte(`"schema":`), []byte(`"schema":"ignored","schema":`), 1)
			encodedTrustRoot = base64.StdEncoding.EncodeToString(raw)
		},
		"unknown root member": func() {
			raw := append([]byte(`{"unknown":true,`), fixture.Pinned.Root[1:]...)
			encodedTrustRoot = base64.StdEncoding.EncodeToString(raw)
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			stamp()
			damage()
			got, err := ProductionTrust()
			if err == nil || len(got.Root) != 0 {
				t.Fatal("invalid production trust accepted")
			}
		})
	}
	stamp()
	pinned, err := ProductionTrust()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releasetrust.VerifySnapshot(pinned, fixture.Material(t), releaseidentity.SnapshotFloor{}); err != nil {
		t.Fatal(err)
	}
	pinned.Root[0] = 'X'
	pinned.RecoveryPublicKey[0] = 'X'
	again, err := ProductionTrust()
	if err != nil || !bytes.Equal(again.Root, fixture.Pinned.Root) || !bytes.Equal(again.RecoveryPublicKey, fixture.Pinned.RecoveryPublicKey) {
		t.Fatal("caller changed build trust")
	}
}
