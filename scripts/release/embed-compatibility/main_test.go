package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func declaration() []byte {
	return []byte(`{"schema":"hikyo.dev/upgrade-compatibility/v1","profile":"stable/v1","version":"1.0.0","sequence":1,"commit":"` + strings.Repeat("a", 40) + `","engines":[{"migrations":{"engine":"sqlite","entries":[]},"schema_sha256":"` + strings.Repeat("b", 64) + `","sources":[]}]}`)
}

func TestLargeDeclarationCompilesAndRetainsExactBinding(t *testing.T) {
	// A single Linux exec argument is limited to 128 KiB. Valid JSON whitespace
	// grows the fixture beyond that limit without inventing migration history.
	raw := append(declaration(), bytes.Repeat([]byte(" "), 160<<10)...)
	dir, err := os.MkdirTemp(".", ".embed-regression-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	input := filepath.Join(dir, "declaration.json")
	write(t, input, raw)
	digest := string(releaseidentity.Hash(raw))
	if err := generate(input, digest, "stable", filepath.Join(dir, "declaration_generated.go")); err != nil {
		t.Fatal(err)
	}
	implementation, err := os.ReadFile("../../../internal/buildcompat/declaration.go")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "declaration.go"), implementation)
	write(t, filepath.Join(dir, "binding_test.go"), []byte(fmt.Sprintf(`package buildcompat
import "testing"
const DevelopmentVersion = "0.0.0+local.dev"
func TestBinding(t *testing.T) {
 raw, d, err := Current()
 if err != nil || len(raw) != %d || d.Version != "1.0.0" { t.Fatalf("binding: length=%%d declaration=%%+v error=%%v", len(raw), d, err) }
 raw[0] = 'X'
 if _, _, err := Current(); err != nil { t.Fatal("caller mutated embedded declaration", err) }
 declarationSHA256 = "%s"
 if _, _, err := Current(); err == nil { t.Fatal("digest mismatch accepted") }
}
func TestLinkerOverride(t *testing.T) {
 raw, _, err := Current()
 if err != nil || len(raw) != %d { t.Fatalf("fixture linker stamp lost: length=%%d error=%%v", len(raw), err) }
}
`, len(raw), strings.Repeat("c", 64), len(declaration()))))
	pkg := "github.com/Hikyo-Org/hikyo/scripts/release/embed-compatibility/" + filepath.Base(dir)
	cmd := exec.Command("go", "test", "-count=1", "-run=^TestBinding$", "-ldflags=-X "+pkg+".declarationSHA256="+digest, "./"+filepath.ToSlash(dir))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile and run large embedded declaration: %v\n%s", err, output)
	}
	flags := "-X " + pkg + ".declarationSHA256=" + string(releaseidentity.Hash(declaration())) + " -X " + pkg + ".encodedDeclaration=" + base64.StdEncoding.EncodeToString(declaration())
	cmd = exec.Command("go", "test", "-count=1", "-run=^TestLinkerOverride$", "-ldflags="+flags, "./"+filepath.ToSlash(dir))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("existing linker fixtures: %v\n%s", err, output)
	}
}

func TestGenerateRejectsInvalidReleaseAndClearsStaleStamp(t *testing.T) {
	for _, tc := range []struct {
		name, channel, digest string
		raw                   []byte
		missing               bool
	}{
		{name: "missing nightly", channel: "nightly", missing: true},
		{name: "missing stable", channel: "stable", missing: true},
		{name: "digest without file", digest: strings.Repeat("a", 64), missing: true},
		{name: "mismatched digest", raw: declaration(), digest: strings.Repeat("a", 64)},
		{name: "invalid JSON", raw: []byte("{")},
		{name: "oversized", raw: bytes.Repeat([]byte(" "), 5<<20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "generated.go")
			write(t, output, []byte("stale"))
			input := ""
			if !tc.missing {
				input = filepath.Join(dir, "input.json")
				write(t, input, tc.raw)
				if tc.digest == "" {
					tc.digest = string(releaseidentity.Hash(tc.raw))
				}
			}
			if err := generate(input, tc.digest, tc.channel, output); err == nil {
				t.Fatal("invalid release accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("stale release stamp retained: %v", err)
			}
		})
	}
}

func TestUnstampedSnapshotClearsPreviousRelease(t *testing.T) {
	for _, channel := range []string{"", "off"} {
		output := filepath.Join(t.TempDir(), "generated.go")
		write(t, output, []byte("stale"))
		if err := generate("", "", channel, output); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("snapshot retained release stamp: %v", err)
		}
	}
}

func write(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
