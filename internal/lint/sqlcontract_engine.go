package lint

import "fmt"

type engineQueryPin struct {
	SQLHash string
	APIHash string
	Reason  string
}

func checkEngineOnlyQuery(engine string, q Query, api generatedContract, generated bool) []string {
	approved, ok := approvedEngineOnlyQueries[engine][q.Name]
	if !ok {
		return []string{fmt.Sprintf("sqlpredicate: query %q exists on %s only without a reviewed engine protocol pin", q.Name, engine)}
	}
	if !generated || !api.BindSitesKnown || approved.Reason == "" || approved.SQLHash != q.Hash() || approved.APIHash != api.hash() {
		return []string{fmt.Sprintf("sqlpredicate: %s-only query %q changed its reviewed engine protocol: sql=%s api=%s", engine, q.Name, q.Hash(), api.hash())}
	}
	return nil
}
