package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedQueryReviewInventoryRejectsAmbiguousJSON(t *testing.T) {
	valid := `{"authority":"owner","tests":["boundary_test.go:TestBoundary"],"sql_hash":{"sqlite":"sql","postgres":"sql"},"api_hash":{"sqlite":"api","postgres":"api"}}`
	for name, source := range map[string]string{
		"duplicate query":  `{"Query":` + valid + `,"Query":` + valid + `}`,
		"duplicate engine": `{"Query":` + strings.Replace(valid, `"sqlite":"sql"`, `"sqlite":"sql","sqlite":"different"`, 1) + `}`,
		"unknown field":    `{"Query":` + strings.Replace(valid, `"tests":`, `"flow_test":[],"tests":`, 1) + `}`,
		"trailing input":   `{"Query":` + valid + `} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseScopedQueryReviews([]byte(source)); err == nil {
				t.Fatal("ambiguous inventory accepted")
			}
		})
	}
}

func TestScopedQueryReviewInventoryRequiresBothEngineContracts(t *testing.T) {
	var definitions map[string]map[string]json.RawMessage
	if err := json.Unmarshal(scopedQueryReviewsJSON, &definitions); err != nil {
		t.Fatal(err)
	}
	const name = "AdapterMapping"
	for _, kind := range []string{"sql_hash", "api_hash"} {
		for caseName, hashes := range map[string]map[string]string{
			"missing sqlite":   {"postgres": "pin"},
			"missing postgres": {"sqlite": "pin"},
			"unknown engine":   {"sqlite": "pin", "oracle": "pin"},
			"extra engine":     {"sqlite": "pin", "postgres": "pin", "oracle": "pin"},
			"missing hashes":   nil,
			"empty hash":       {"sqlite": "", "postgres": "pin"},
			"blank hash":       {"sqlite": "pin", "postgres": " "},
		} {
			t.Run(kind+"/"+caseName, func(t *testing.T) {
				original := definitions[name][kind]
				defer func() { definitions[name][kind] = original }()
				modified, err := json.Marshal(hashes)
				if err != nil {
					t.Fatal(err)
				}
				definitions[name][kind] = modified
				source, err := json.Marshal(definitions)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parseScopedQueryReviews(source); err == nil {
					t.Fatal("incomplete or unknown engine contract accepted")
				}
			})
		}
	}
	reviews, err := readScopedQueryReviews()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		var hashes map[string]string
		if err := json.Unmarshal(definitions[name]["sql_hash"], &hashes); err != nil {
			t.Fatal(err)
		}
		if reviews[engine][name].SQLHash != hashes[engine] {
			t.Fatal("engine-specific SQL pin lost")
		}
	}
}

func TestReviewedScopedQueryRequiresExactSQLAPIAndEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "boundary_test.go"), []byte("package fixture\nimport \"testing\"\nfunc TestBoundary(t *testing.T) { t.Log(\"nested\") }\nfunc TestUnrelated(t *testing.T) {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	q := Query{Name: "nested", SQL: "SELECT id FROM items WHERE org_id=sqlc.arg(chain_org)"}
	api := generatedContract{BindSitesKnown: true}
	review := scopedQueryReview{SQLHash: q.Hash(), APIHash: api.hash(), Authority: "Exact proof-derived owning org, project and retained snapshot membership.", Tests: []string{"boundary_test.go:TestBoundary"}}
	review.FlowTests = []string{"boundary_test.go:TestUnrelated"}
	if got := checkScopedQueryReview("sqlite", q, api, true, review, root); len(got) != 0 {
		t.Fatal(got)
	}
	for name, modify := range map[string]func(*scopedQueryReview){
		"SQL drift":         func(r *scopedQueryReview) { r.SQLHash = "changed" },
		"API drift":         func(r *scopedQueryReview) { r.APIHash = "changed" },
		"missing authority": func(r *scopedQueryReview) { r.Authority = " " },
		"missing evidence":  func(r *scopedQueryReview) { r.Tests = nil },
		"flow-only reference": func(r *scopedQueryReview) {
			r.Tests = []string{"boundary_test.go:TestUnrelated"}
			r.FlowTests = []string{"boundary_test.go:TestBoundary"}
		},
		"missing flow":   func(r *scopedQueryReview) { r.FlowTests = []string{"boundary_test.go:TestDeleted"} },
		"missing test":   func(r *scopedQueryReview) { r.Tests = []string{"boundary_test.go:TestDeleted"} },
		"unrelated body": func(r *scopedQueryReview) { r.Tests = []string{"boundary_test.go:TestUnrelated"} },
		"path escape":    func(r *scopedQueryReview) { r.Tests = []string{"../outside_test.go:TestBoundary"} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := review
			modify(&bad)
			if got := checkScopedQueryReview("sqlite", q, api, true, bad, root); len(got) == 0 {
				t.Fatal("incomplete review accepted")
			}
		})
	}
	if got := checkScopedQueryReview("sqlite", q, api, false, review, root); len(got) == 0 {
		t.Fatal("missing generated API accepted")
	}
	api.BindSitesKnown = false
	if got := checkScopedQueryReview("sqlite", q, api, true, review, root); len(got) == 0 {
		t.Fatal("unknown bindings accepted")
	}
	api.BindSitesKnown = true
	q.Annotation = "instance-scoped"
	if got := checkScopedQueryReview("sqlite", q, api, true, review, root); len(got) == 0 {
		t.Fatal("tenant query relabeled instance-scoped")
	}
}
