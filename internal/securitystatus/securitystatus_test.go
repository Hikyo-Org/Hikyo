package securitystatus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `{"format":"hikyo.security-maintenance-status/v1","enrolled":true,"phase":"current","protection_gap":false,"applicable":[],"coverage":[],"covered_components":[],"catalog_sequence":4,"known_until":"2026-11-02T12:00:00Z"}`

func write(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSeparatesAbsentFromUnreadable(t *testing.T) {
	oversized := valid[:len(valid)-1] + `,"detail":"` + strings.Repeat("x", MaxBytes) + `"}`
	for _, tc := range []struct {
		name string
		path string
		want Report
	}{
		{"unset", "", Report{}},
		{"missing", filepath.Join(t.TempDir(), "absent.json"), Report{}},
		{"directory", t.TempDir(), Report{Unreadable: true}},
		{"malformed", write(t, `{"format":`), Report{Unreadable: true}},
		{"empty", write(t, ``), Report{Unreadable: true}},
		{"oversized", write(t, oversized), Report{Unreadable: true}},
		{"unknown member", write(t, valid[:len(valid)-1]+`,"operation":{}}`), Report{Unreadable: true}},
		{"duplicate member", write(t, valid[:len(valid)-1]+`,"phase":"blocked"}`), Report{Unreadable: true}},
		{"trailing data", write(t, valid+`{}`), Report{Unreadable: true}},
		{"another format", write(t, strings.Replace(valid, "status/v1", "status/v2", 1)), Report{Unreadable: true}},
		{"no phase", write(t, strings.Replace(valid, `"phase":"current"`, `"phase":""`, 1)), Report{Unreadable: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Load(tc.path); got.Reported != tc.want.Reported || got.Unreadable != tc.want.Unreadable {
				t.Fatalf("Load = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A projected ConfigMap file is a symlink into the volume, so it is followed; the
// size bound and the regular-file check still apply to what it reaches.
func TestLoadFollowsALinkButChecksItsTarget(t *testing.T) {
	oversized := valid[:len(valid)-1] + `,"detail":"` + strings.Repeat("x", MaxBytes) + `"}`
	for name, tc := range map[string]struct {
		target string
		want   Report
	}{
		"regular file": {write(t, valid), Report{Reported: true}},
		"directory":    {t.TempDir(), Report{Unreadable: true}},
		"oversized":    {write(t, oversized), Report{Unreadable: true}},
		"malformed":    {write(t, `{"format":`), Report{Unreadable: true}},
		"dangling":     {filepath.Join(t.TempDir(), "absent.json"), Report{}},
	} {
		t.Run(name, func(t *testing.T) {
			link := filepath.Join(t.TempDir(), "link.json")
			if err := os.Symlink(tc.target, link); err != nil {
				t.Skip("symbolic links unavailable:", err)
			}
			if got := Load(link); got.Reported != tc.want.Reported || got.Unreadable != tc.want.Unreadable {
				t.Fatalf("Load = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoadReturnsTheValidStatus(t *testing.T) {
	got := Load(write(t, valid))
	if !got.Reported || got.Unreadable || !got.Status.Enrolled || got.Status.Phase != "current" || got.Status.CatalogSequence != 4 || got.Status.KnownUntil.IsZero() {
		t.Fatalf("Load = %+v", got)
	}
}

func TestDetailRedactsValuesKeepsReasonsAndCutsOnACharacterBoundary(t *testing.T) {
	got := Detail(`pull "registry.example/app" failed: token=abc123 at https://user:pw@host/x`, 1024)
	for _, secret := range []string{"registry.example", "abc123", "user:pw"} {
		if strings.Contains(got, secret) {
			t.Fatalf("%q survived: %s", secret, got)
		}
	}
	if !strings.Contains(got, "pull") || !strings.Contains(got, "failed") {
		t.Fatalf("reason lost: %s", got)
	}
	// The length bound can cut a quoted value before its closing quote.
	if got := Redact("refused 'pw"); got != "refused '[redacted]'" {
		t.Fatalf("unterminated quote kept its value: %s", got)
	}
	if got := Redact("can't start\nexit 1"); got != "can't start\nexit 1" {
		t.Fatalf("apostrophe before a later line redacted: %q", got)
	}
	// A multiline value cut before its closing quote is redacted to the end.
	for text, want := range map[string]string{
		"refused 'hunter2\nsecond":          "refused '[redacted]'",
		"refused 'hunter2\nsecond' and 'x'": "refused '[redacted]'",
		"refused 'hunter2\nsecond 'tail'":   "refused '[redacted]'",
		"env \"hunter2\nsecond":             "env \"[redacted]\"",
		"line 'a'\nnext \"b\"\ncan't stop":  "line '[redacted]'\nnext \"[redacted]\"\ncan't stop",
	} {
		if got := Redact(text); got != want {
			t.Fatalf("Redact(%q) = %q", text, got)
		}
	}
	// An apostrophe earlier on the line must not pair with the value's opening quote.
	for _, text := range []string{"can't apply: refused 'hunter2 x'", "can't apply: refused 'hunter2 x"} {
		if got := Redact(text); got != "can't apply: refused '[redacted]'" {
			t.Fatalf("Redact(%q) = %q", text, got)
		}
	}
	cut := Detail(strings.Repeat("é", 100), 5)
	if len(cut) > 5 || cut != "éé" {
		t.Fatalf("cut = %q", cut)
	}
}
