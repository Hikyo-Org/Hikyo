package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestExistingProjectProjectionAuthenticatesScopeAndConsumesRoot(t *testing.T) {
	ks := newMemStore()
	root := newRoot(t)
	kr, err := LoadKeyring(t.Context(), ks, bytes.Clone(root))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, sealer, err := kr.PrepareNewProject("org", "project")
	if err != nil {
		t.Fatal(err)
	}
	ks.tier3[t3key(PurposeProject, "org", "project")] = []WrappedKey{wrapped}
	aad := ProjectFieldAAD{OrgID: "org", ProjectID: "project", EnvironmentID: "env", SnapshotID: "snapshot", KeyID: "key", OwnerTable: "snapshot_entries", OwnerRowID: "entry", FieldTag: "snapshot_value"}
	ciphertext, err := sealer.SealField(aad, []byte("private setting"))
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"none", "root", "project", "snapshot", "ciphertext", "duplicate", "missing-key"} {
		t.Run(failure, func(t *testing.T) {
			input := bytes.Clone(root)
			project := "project"
			field := ExistingProjectField{Name: "SETTING", AAD: aad, Ciphertext: bytes.Clone(ciphertext)}
			fields := []ExistingProjectField{field}
			switch failure {
			case "root":
				input[0] ^= 1
			case "project":
				project = "another"
			case "snapshot":
				fields[0].AAD.SnapshotID = "another"
			case "ciphertext":
				fields[0].Ciphertext[len(ciphertext)-1] ^= 1
			case "duplicate":
				fields = append(fields, field)
			case "missing-key":
				delete(ks.tier3, t3key(PurposeProject, "org", "project"))
				defer func() { ks.tier3[t3key(PurposeProject, "org", "project")] = []WrappedKey{wrapped} }()
			}
			values, err := OpenExistingProjectFields(t.Context(), ks, input, "org", project, fields)
			if !bytes.Equal(input, make([]byte, KeySize)) {
				t.Fatal("root not consumed")
			}
			if failure == "none" {
				if err != nil || values["SETTING"] != "private setting" {
					t.Fatalf("valid projection: %v", err)
				}
			} else if err == nil || values != nil || strings.Contains(err.Error(), "private setting") {
				t.Fatal("invalid projection accepted or leaked")
			}
		})
	}
}
