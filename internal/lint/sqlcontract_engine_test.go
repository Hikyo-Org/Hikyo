package lint

import "testing"

func TestEngineOnlyContractIsExactAndReasoned(t *testing.T) {
	q := Query{Name: "testEngineProtocol", Cmd: "exec", SQL: "SELECT pg_advisory_xact_lock(1,2)"}
	api := generatedContract{BindSitesKnown: true}
	original := approvedEngineOnlyQueries
	approvedEngineOnlyQueries = map[string]map[string]engineQueryPin{"postgres": {q.Name: {SQLHash: q.Hash(), APIHash: api.hash(), Reason: "held transaction protocol"}}}
	defer func() { approvedEngineOnlyQueries = original }()
	if got := checkEngineOnlyQuery("postgres", q, api, true); len(got) != 0 {
		t.Fatal(got)
	}
	changed := q
	changed.SQL = "SELECT pg_advisory_xact_lock(1,3)"
	if got := checkEngineOnlyQuery("postgres", changed, api, true); len(got) == 0 {
		t.Fatal("SQL drift accepted")
	}
	altered := api
	altered.BindSites = []string{"Different"}
	if got := checkEngineOnlyQuery("postgres", q, altered, true); len(got) == 0 {
		t.Fatal("API drift accepted")
	}
	if got := checkEngineOnlyQuery("postgres", q, api, false); len(got) == 0 {
		t.Fatal("missing generation accepted")
	}
	if got := checkEngineOnlyQuery("sqlite", q, api, true); len(got) == 0 {
		t.Fatal("unknown engine accepted")
	}
	approvedEngineOnlyQueries["postgres"][q.Name] = engineQueryPin{SQLHash: q.Hash(), APIHash: api.hash()}
	if got := checkEngineOnlyQuery("postgres", q, api, true); len(got) == 0 {
		t.Fatal("unjustified protocol accepted")
	}
}
