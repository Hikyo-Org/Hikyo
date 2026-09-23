package oidcrp

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Hikyo-side issuer belt is gone (#588 d3): go-oidc's check is the only
// ID-token issuer check, so nothing outside a test may switch it off or relax
// it. SkipIssuerCheck disables it; InsecureIssuerURLContext decouples the
// discovery URL from the pinned issuer. Test files may use the latter to stand
// in for a provider's real discovery document (issuer_test.go does).
func TestNothingRelaxesTheLibraryIssuerCheck(t *testing.T) {
	relaxation := regexp.MustCompile(`\bSkipIssuerCheck\s*:|\bInsecureIssuerURLContext\s*\(`)
	for _, root := range []string{"../../internal", "../../cmd", "../../api"} {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if relaxation.Match(body) {
				t.Errorf("%s relaxes go-oidc's issuer check (%s)", path, relaxation.Find(body))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
