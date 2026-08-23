package config

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestKnownEnvCoversEveryGetenv(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	fileSet := token.NewFileSet()
	missing := map[string][]string{}

	for _, root := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path == filepath.Join(repoRoot, "internal", "operator") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			parsed, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 || !isEnvironmentLookup(call.Fun) {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil || !strings.HasPrefix(key, "HIKYO_") || knownEnv[key] {
					return true
				}
				position := fileSet.Position(literal.Pos())
				missing[key] = append(missing[key], fmt.Sprintf("%s:%d", position.Filename, position.Line))
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Errorf("knownEnv is missing %s, consumed at %s", key, strings.Join(missing[key], ", "))
	}
}

func isEnvironmentLookup(expr ast.Expr) bool {
	switch fn := expr.(type) {
	case *ast.Ident:
		return fn.Name == "getenv" || fn.Name == "lookupEnv"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "Getenv" || fn.Sel.Name == "LookupEnv"
	default:
		return false
	}
}

func TestConsumedServerEnvKeysDoNotWarn(t *testing.T) {
	const (
		newRootKeyFile = "/run/credentials/hikyo-new-root-key"
		directoryProxy = "https://proxy.example.com"
	)
	pairs := []string{
		"HIKYO_NEW_ROOT_KEY_FILE", newRootKeyFile,
		"HIKYO_DIRECTORY_PROXY", directoryProxy,
	}
	cfg, warnings, err := Load("server", []string{"--dev"}, env(pairs...), environFrom(pairs...))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("consumed server environment keys must not warn, got %v", warnings)
	}
	if cfg.NewRootKeyFile != newRootKeyFile {
		t.Fatalf("NewRootKeyFile = %q, want %q", cfg.NewRootKeyFile, newRootKeyFile)
	}
	if cfg.DirectoryProxy != directoryProxy {
		t.Fatalf("DirectoryProxy = %q, want %q", cfg.DirectoryProxy, directoryProxy)
	}
}
