package lint

import (
	"os"
	"path/filepath"
	"testing"
)

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
