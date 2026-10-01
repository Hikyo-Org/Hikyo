package importer

import "testing"

func TestStrictManifestDecodePreservesEnvironmentIdentityAndRefusals(t *testing.T) {
	var manifest Manifest
	source := `{"phase_completion":{"imported":{"Production":true,"production":false}}}`
	if err := strictDecode([]byte(source), "manifest", &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.PhaseCompletion.Imported["Production"] || manifest.PhaseCompletion.Imported["production"] {
		t.Fatal("environment identities conflated")
	}
	for _, test := range []struct {
		source string
		code   Code
	}{
		{`{"phase_completion":{"imported":{"Production":true,"Production":false}}}`, CodeDuplicateKey},
		{`{"phase_completion":{"imported":{},"IMPORTED":{}}}`, CodeDuplicateKey},
		{`{"phase_completion":{"future_field":true}}`, CodeVersion},
		{source + `{}`, CodeMalformed},
	} {
		err := strictDecode([]byte(test.source), "manifest", &manifest)
		refusal, ok := err.(*Error)
		if !ok || refusal.Code != test.code {
			t.Fatalf("refusal for %s: %v", test.source, err)
		}
	}
}
