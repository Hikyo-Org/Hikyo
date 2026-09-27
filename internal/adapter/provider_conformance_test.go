package adapter

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestProviderSetIsPinnedAcrossSurfaces keeps the compiled-in provider set,
// the OpenAPI discriminator, and the newest provider CHECK of both database
// engines in lockstep. Adding a provider to one surface without the others
// fails here instead of at the first create on a real database.
func TestProviderSetIsPinnedAcrossSurfaces(t *testing.T) {
	want := make([]string, 0, len(SupportedProviders()))
	for _, provider := range SupportedProviders() {
		want = append(want, string(provider))
	}
	sort.Strings(want)

	spec, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)\n    AdapterProvider:\n.*?x-extensible-enum: \[([^\]]*)\]`).FindSubmatch(spec)
	if match == nil {
		t.Fatal("api/openapi.yaml has no AdapterProvider x-extensible-enum")
	}
	if got := splitList(string(match[1]), ","); !slices.Equal(got, want) {
		t.Errorf("OpenAPI AdapterProvider = %v, want %v", got, want)
	}

	check := regexp.MustCompile(`provider IN \(([^)]*)\)`)
	for _, engine := range []string{"sqlite", "postgres"} {
		files, err := filepath.Glob(filepath.Join("..", "store", "migrations", engine, "*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(files)
		var newest []byte
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if found := check.FindSubmatch(raw); found != nil && strings.Contains(string(raw), "adapters") {
				newest = found[1]
			}
		}
		if newest == nil {
			t.Fatalf("%s migrations declare no adapters provider CHECK", engine)
		}
		if got := splitList(strings.ReplaceAll(string(newest), "'", ""), ","); !slices.Equal(got, want) {
			t.Errorf("%s newest adapters provider CHECK = %v, want %v", engine, got, want)
		}
	}
}

func splitList(raw, sep string) []string {
	var out []string
	for _, part := range strings.Split(raw, sep) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	sort.Strings(out)
	return out
}
