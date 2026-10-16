package ci_test

import (
	"bytes"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Exercise the shipped linker configuration: govulncheck cannot distinguish
// linked packages from module-wide advisories when ELF symbols are stripped.
func TestReleaseBinaryRetainsScannerSymbols(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Builds []struct {
			ID      string   `yaml:"id"`
			LDFlags []string `yaml:"ldflags"`
		} `yaml:"builds"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	var flags []string
	for _, build := range config.Builds {
		if build.ID == "hikyo" {
			flags = build.LDFlags
		}
	}
	if len(flags) == 0 {
		t.Fatal("missing shipped linker flags")
	}
	tmpl, err := template.New("ldflags").Parse(strings.Join(flags, " "))
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, map[string]any{
		"Env": map[string]string{}, "Version": "test", "Commit": strings.Repeat("a", 40), "CommitDate": "2026-10-16T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\nfunc main() { println(\"scanner symbol fixture\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", rendered.String(), "-o", binary, source)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build release linker fixture: %v\n%s", err, out)
	}
	f, err := elf.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	symbols, err := f.Symbols()
	if err != nil {
		t.Fatalf("release flags stripped the symbol table required by govulncheck: %v", err)
	}
	for _, symbol := range symbols {
		if symbol.Name == "main.main" {
			return
		}
	}
	t.Fatal("release binary lacks its linked main.main symbol")
}
