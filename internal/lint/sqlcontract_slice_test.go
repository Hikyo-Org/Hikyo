package lint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

func TestGeneratedSQLiteSliceBindsCanonicalRewrite(t *testing.T) {
	t.Run("project", func(t *testing.T) { testGeneratedSliceBindsCanonicalRewrite(t, false) })
	t.Run("environment", func(t *testing.T) { testGeneratedSliceBindsCanonicalRewrite(t, true) })
}

func testGeneratedSliceBindsCanonicalRewrite(t *testing.T, environment bool) {
	t.Helper()
	source := `package sqlitegen
 func read(ctx context.Context,arg Params) {
 query:=list
 var queryParams []interface{}
 queryParams=append(queryParams,arg.ChainOrg)
 queryParams=append(queryParams,arg.ChainProject)
 if len(arg.KeyIds)>0 {
 for _,v:=range arg.KeyIds { queryParams=append(queryParams,v) }
 query=strings.Replace(query,"/*SLICE:key_ids*/?",strings.Repeat(",?",len(arg.KeyIds))[1:],1)
 } else { query=strings.Replace(query,"/*SLICE:key_ids*/?","NULL",1) }
 rows,err:=q.db.QueryContext(ctx,query,queryParams...)
 _=rows;_=err
 }`
	sql := map[string]string{"list": "SELECT id FROM keys WHERE org_id=?1 AND project_id=?2 AND id IN (/*SLICE:key_ids*/?)"}
	expected := []string{"Org", "Project", "KeyIds"}
	if environment {
		source = strings.Replace(source, "queryParams=append(queryParams,arg.ChainProject)", "queryParams=append(queryParams,arg.ChainProject)\n queryParams=append(queryParams,arg.ChainEnv)", 1)
		sql["list"] = strings.Replace(sql["list"], "project_id=?2 AND", "project_id=?2 AND environment_id=?3 AND", 1)
		expected = []string{"Org", "Project", "Env", "KeyIds"}
	}
	run := func(text string) ([]string, bool) {
		t.Helper()
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", text, 0)
		if err != nil {
			t.Fatal(err)
		}
		return generatedBindSites(file.Decls[0].(*ast.FuncDecl).Body, sql, "sqlite")
	}
	got, known := run(source)
	if !known || !slices.Equal(got, expected) {
		t.Fatalf("canonical slice sites %v known=%t", got, known)
	}
	swapped := strings.Replace(strings.Replace(source, "arg.ChainOrg", "arg.TEMP", 1), "arg.ChainProject", "arg.ChainOrg", 1)
	swapped = strings.Replace(swapped, "arg.TEMP", "arg.ChainProject", 1)
	got, known = run(swapped)
	expected[0], expected[1] = expected[1], expected[0]
	if !known || !slices.Equal(got, expected) {
		t.Fatalf("swapped scope hidden: %v %t", got, known)
	}
	cases := map[string]string{
		"unknown rewrite":        strings.Replace(source, "strings.Replace(query", "evil.Replace(query", 1),
		"changed loop":           strings.Replace(source, "append(queryParams,v)", "append(queryParams,arg.ChainOrg)", 1),
		"wrong repeat":           strings.Replace(source, "len(arg.KeyIds))[1:]", "len(arg.Other))[1:]", 1),
		"changed empty branch":   strings.Replace(source, "\"NULL\"", "\"SELECT id FROM other\"", 1),
		"changed marker":         strings.Replace(source, "/*SLICE:key_ids*/?", "/*SLICE:other*/?", 1),
		"extra mutation":         strings.Replace(source, "rows,err:=", "query=\"SELECT id FROM other\"\nrows,err:=", 1),
		"wrong argument carrier": strings.Replace(source, "queryParams...", "otherParams...", 1),
		"interleaved scalar":     strings.Replace(source, "rows,err:=", "queryParams=append(queryParams,arg.ChainOrg)\nrows,err:=", 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if _, known := run(bad); known {
				t.Fatal("unknown slice rewrite proved")
			}
		})
	}
	reusedOrdinal := sql["list"] + " AND name=?3"
	if environment {
		reusedOrdinal = sql["list"] + " AND name=?4"
	}
	for name, badSQL := range map[string]string{
		"slice before scalar":  "SELECT id FROM keys WHERE id IN (/*SLICE:key_ids*/?) AND org_id=?2 AND project_id=?3",
		"slice reused ordinal": reusedOrdinal,
	} {
		t.Run(name, func(t *testing.T) {
			sql["list"] = badSQL
			if _, known := run(source); known {
				t.Fatal("unsupported slice position proved")
			}
		})
	}
}
