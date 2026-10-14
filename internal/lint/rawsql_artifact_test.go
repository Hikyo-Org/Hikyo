package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestArtifactDisposableDatabaseProtocolsRejectDrift(t *testing.T) {
	root := repoRoot(t)
	filename := filepath.Join(root, "scripts/security/artifact-harness/runtime.go")
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	protocols, err := reviewedPins.rawProtocols()
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]RawSQLProtocol{}
	for _, name := range []string{"createOwnedDisposableDatabase", "dropOwnedDisposableDatabase"} {
		owner := "scripts/security/artifact-harness/runtime.go:" + name
		pin, exists := protocols[owner]
		if !exists {
			t.Fatal("missing named disposable-database protocol: " + owner)
		}
		pins[owner] = pin
	}
	change := func(old, replacement string) string {
		t.Helper()
		if !strings.Contains(source, old) {
			t.Fatal("missing mutation target: " + old)
		}
		return strings.Replace(source, old, replacement, 1)
	}
	const call = `createOwnedDisposableDatabase(ctx, conn, "hikyo_security_0123456789abcdef01234567")`
	for _, tt := range []struct {
		name, source, alternateFilename, refusal string
	}{
		{name: "owned lifecycle", source: source},
		{name: "create statement", source: change(`"CREATE DATABASE "`, `"CREATE ROLE "`), refusal: "createOwnedDisposableDatabase changed its reviewed engine protocol"},
		{name: "drop statement", source: change(`"DROP DATABASE "`, `"DROP TABLE "`), refusal: "dropOwnedDisposableDatabase changed its reviewed engine protocol"},
		{name: "tenant select", source: change("\t\tdatabaseCreated = true", "\t\tdatabaseCreated = true\n\t\t_, _ = conn.Exec(ctx, \"SELECT * FROM environments\")"), refusal: "newTarget executes handwritten SQL outside a named engine protocol"},
		{name: "tenant update", source: change("\t\tdatabaseCreated = true", "\t\tdatabaseCreated = true\n\t\t_, _ = conn.Exec(ctx, \"UPDATE environments SET name = 'unowned'\")"), refusal: "newTarget executes handwritten SQL outside a named engine protocol"},
		{name: "unsafe identifier", source: change("return pgx.Identifier{database}.Sanitize(), nil", "return database, nil"), refusal: "changed reviewed helper dependency scripts/security/artifact-harness/runtime.go:ownedDisposableDatabaseIdentifier"},
		{name: "name validation drift", source: change("hex.EncodeToString(decoded) != nonce", "len(decoded) != 12"), refusal: "changed reviewed helper dependency scripts/security/artifact-harness/runtime.go:ownedDisposableDatabaseIdentifier"},
		{name: "unowned caller name", source: change(`database = "hikyo_security_"`, `database = "unowned_"`), refusal: "newTarget references protocol"},
		{name: "lost creation guard", source: change("if err = createOwnedDisposableDatabase", "databaseCreated = true\n\t\tif err = createOwnedDisposableDatabase"), refusal: "newTarget references protocol"},
		{name: "unreviewed caller", source: source + "\nfunc anotherCaller(ctx context.Context, conn *pgx.Conn) error { return " + call + " }\n", refusal: "anotherCaller references protocol"},
		{name: "unreviewed alias", source: source + "\nfunc aliasCaller(ctx context.Context, conn *pgx.Conn) error { create := createOwnedDisposableDatabase; return create(ctx, conn, \"hikyo_security_0123456789abcdef01234567\") }\n", refusal: "aliasCaller references protocol"},
		{name: "wrong function owner", source: strings.ReplaceAll(source, "createOwnedDisposableDatabase", "anotherCreateOwner"), refusal: "anotherCreateOwner executes handwritten SQL outside a named engine protocol"},
		{name: "neighboring file", source: source, alternateFilename: filepath.Join(filepath.Dir(filename), "unreviewed_runtime.go"), refusal: "unreviewed_runtime.go:createOwnedDisposableDatabase executes handwritten SQL outside a named engine protocol"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			overlay := map[string][]byte{filename: []byte(tt.source)}
			if tt.alternateFilename != "" {
				overlay[filename] = []byte("package main\n")
				overlay[tt.alternateFilename] = []byte(tt.source)
			}
			// Parse and typecheck the real harness with an in-memory source overlay.
			// Mutation inputs remain valid Go and exercise the actual pinned boundary.
			pkgs, err := packages.Load(&packages.Config{
				Dir: root, Overlay: overlay,
				Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
					packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
			}, Module+"/scripts/security/artifact-harness")
			if err != nil {
				t.Fatal(err)
			}
			if len(pkgs) == 0 {
				t.Fatal("mutation loaded no typed harness")
			}
			for _, pkg := range pkgs {
				for _, err := range pkg.Errors {
					t.Fatal(err)
				}
			}
			findings := CheckRawSQL(pkgs, root, Module, pins)
			if tt.refusal == "" {
				if len(findings) != 0 {
					t.Fatalf("reviewed disposable-database lifecycle refused: %v", findings)
				}
			} else {
				assertFindings(t, findings, []string{tt.refusal})
			}
		})
	}
}
