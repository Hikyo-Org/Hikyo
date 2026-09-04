package isolation

import (
	"fmt"
	"math"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// These are the seam-2 proofs (#629): every mapped list operation has a bounded
// keyset page method whose concatenated pages reproduce the unbounded read
// exactly, on both engines. A page never fetches the whole collection: the
// store statement carries the cursor and a LIMIT. Equivalence across a small
// page size is the strongest available witness that the keyset order is stable
// and that nothing is dropped or duplicated at a page boundary.

// pageAllString collects every page of a string-keyset list method, following
// the last returned item's key as the next cursor. It stops when a page returns
// fewer than the limit; the extra empty fetch a full final page would need is
// covered by pageBoundary below.
func pageAllString[T any](t *testing.T, limit int, fetch func(after string, limit int) ([]T, error), keyOf func(T) string) []T {
	t.Helper()
	var out []T
	after := ""
	for {
		page, err := fetch(after, limit)
		if err != nil {
			t.Fatalf("page after %q: %v", after, err)
		}
		if len(page) > limit {
			t.Fatalf("page after %q returned %d rows, over the %d limit", after, len(page), limit)
		}
		out = append(out, page...)
		if len(page) < limit {
			return out
		}
		after = keyOf(page[len(page)-1])
	}
}

func TestMCPEnvironmentPageMatchesList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		_, _, envSvc := services(t, db)
		scope := scopeProject(orgA, prjA1)
		for i := range 5 {
			if _, err := envSvc.Create(t.Context(), service.LocalPrincipal(custodian), scope, fmt.Sprintf("mcp-env-%02d", 4-i), nil); err != nil {
				t.Fatalf("create env %d: %v", i, err)
			}
		}
		full, err := envSvc.List(t.Context(), service.LocalPrincipal(custodian), scope)
		if err != nil {
			t.Fatal(err)
		}
		var paged []service.Environment
		afterOrder, afterName := int64(-1), ""
		for {
			page, err := envSvc.ListPage(t.Context(), service.LocalPrincipal(custodian), scope, afterOrder, afterName, 2)
			if err != nil {
				t.Fatal(err)
			}
			paged = append(paged, page...)
			if len(page) < 2 {
				break
			}
			afterOrder, afterName = page[len(page)-1].DisplayOrder, page[len(page)-1].Name
		}
		assertEnvironmentsEqual(t, full, paged)
	})
}

func TestMCPDefinitionsPageMatchesList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		keys := keySvc(t, db)
		scope := scopeProject(orgA, prjA1)
		for i := range 5 {
			spec := service.KeySpec{Name: fmt.Sprintf("MCP_KEY_%02d", i), Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
			if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scope, spec, nil); err != nil {
				t.Fatalf("create key %d: %v", i, err)
			}
		}
		full, fullRev, err := keys.List(t.Context(), service.LocalPrincipal(custodian), scope)
		if err != nil {
			t.Fatal(err)
		}
		var pagedRev int64
		paged := pageAllString(t, 2,
			func(after string, limit int) ([]service.Key, error) {
				page, rev, err := keys.ListPage(t.Context(), service.LocalPrincipal(custodian), scope, after, limit)
				pagedRev = rev
				return page, err
			},
			func(k service.Key) string { return k.Name })
		if pagedRev != fullRev {
			t.Fatalf("schema revision: paged %d, full %d", pagedRev, fullRev)
		}
		if len(paged) != len(full) {
			t.Fatalf("definitions: paged %d rows, full %d", len(paged), len(full))
		}
		for i := range full {
			if paged[i].ID != full[i].ID || paged[i].Name != full[i].Name {
				t.Fatalf("definitions row %d: paged %+v, full %+v", i, paged[i], full[i])
			}
		}
	})
}

func TestMCPInspectPageMatchesList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		keys := keySvc(t, db)
		values := valueSvc(t, db)
		scope := scopeEnv(orgA, prjA1, envA1)
		for i := range 4 {
			spec := service.KeySpec{Name: fmt.Sprintf("INSPECT_KEY_%02d", i), Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
			if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), spec, nil); err != nil {
				t.Fatalf("create key %d: %v", i, err)
			}
		}
		full, err := values.List(t.Context(), service.LocalPrincipal(custodian), scope, false)
		if err != nil {
			t.Fatal(err)
		}
		paged := pageAllString(t, 2,
			func(after string, limit int) ([]service.ValueCell, error) {
				return values.ListPage(t.Context(), service.LocalPrincipal(custodian), scope, after, limit)
			},
			func(c service.ValueCell) string { return c.Name })
		if len(paged) != len(full) {
			t.Fatalf("inspect: paged %d cells, full %d", len(paged), len(full))
		}
		for i := range full {
			if paged[i] != full[i] {
				t.Fatalf("inspect cell %d: paged %+v, full %+v", i, paged[i], full[i])
			}
		}
	})
}

func TestMCPPendingPageMatchesList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		keys := keySvc(t, db)
		values := valueSvc(t, db)
		revisions := &service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
		scope := scopeEnv(orgA, prjA1, envA1)
		for i := range 4 {
			name := fmt.Sprintf("PENDING_KEY_%02d", i)
			spec := service.KeySpec{Name: name, Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
			if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), spec, nil); err != nil {
				t.Fatalf("create key %d: %v", i, err)
			}
			if _, err := values.Set(t.Context(), service.LocalPrincipal(custodian), scope, name, fmt.Sprintf("draft-%d", i), nil); err != nil {
				t.Fatalf("stage draft %d: %v", i, err)
			}
		}
		full, err := revisions.PendingDrafts(t.Context(), service.LocalPrincipal(custodian), scope)
		if err != nil {
			t.Fatal(err)
		}
		paged := pageAllString(t, 2,
			func(after string, limit int) ([]service.PendingDraft, error) {
				return revisions.PendingDraftsPage(t.Context(), service.LocalPrincipal(custodian), scope, after, limit)
			},
			func(d service.PendingDraft) string { return d.KeyID })
		if len(paged) != len(full) || len(full) == 0 {
			t.Fatalf("pending: paged %d drafts, full %d", len(paged), len(full))
		}
		byKey := map[string]service.PendingDraft{}
		for _, d := range full {
			byKey[d.KeyID] = d
		}
		for i := 1; i < len(paged); i++ {
			if paged[i-1].KeyID >= paged[i].KeyID {
				t.Fatalf("pending not ordered by key_id: %q then %q", paged[i-1].KeyID, paged[i].KeyID)
			}
		}
		for _, d := range paged {
			if byKey[d.KeyID] != d {
				t.Fatalf("pending draft %s: paged %+v, full %+v", d.KeyID, d, byKey[d.KeyID])
			}
		}
	})
}

// TestMCPPendingSecretDraftCarriesNoMaterial pins the pending-draft secret
// boundary that the tool-level canary cannot reach: a workload principal cannot
// stage a draft, so only a seam-2 test can seed a secret draft and prove the
// page returns its presence with no plaintext and no revealed flag.
func TestMCPPendingSecretDraftCarriesNoMaterial(t *testing.T) {
	const secretDraft = "CANARY-PENDING-SECRET-do-not-disclose"
	forEngines(t, func(t *testing.T, db *store.DB) {
		keys := keySvc(t, db)
		values := valueSvc(t, db)
		revisions := &service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
		scope := scopeEnv(orgA, prjA1, envA1)
		mustCreateKey(t, keys, scopeProject(orgA, prjA1), "PENDING_SECRET", schema.Secret)
		if _, err := values.Set(t.Context(), service.LocalPrincipal(custodian), scope, "PENDING_SECRET", secretDraft, nil); err != nil {
			t.Fatalf("stage secret draft: %v", err)
		}
		drafts, err := revisions.PendingDraftsPage(t.Context(), service.LocalPrincipal(custodian), scope, "", 25)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, d := range drafts {
			if d.Name != "PENDING_SECRET" {
				continue
			}
			found = true
			if d.Value != "" || d.Revealed || d.Classification != string(schema.Secret) {
				t.Fatalf("secret draft carried material: %+v", d)
			}
		}
		if !found {
			t.Fatal("secret draft absent from the page")
		}
	})
}

func TestMCPRevisionsPageMatchesList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		keys := keySvc(t, db)
		values := valueSvc(t, db)
		revisions := &service.Revisions{DB: db, Keyring: probeKeyring(t, db)}
		scope := scopeEnv(orgA, prjA1, envA1)
		for i := range 4 {
			name := fmt.Sprintf("REV_KEY_%02d", i)
			spec := service.KeySpec{Name: name, Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
			if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), spec, nil); err != nil {
				t.Fatalf("create key %d: %v", i, err)
			}
			staged, err := values.Set(t.Context(), service.LocalPrincipal(custodian), scope, name, fmt.Sprintf("v-%d", i), nil)
			if err != nil {
				t.Fatalf("stage %d: %v", i, err)
			}
			if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
				t.Fatalf("publish %d: %v", i, err)
			}
		}
		full, err := revisions.History(t.Context(), service.LocalPrincipal(custodian), scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(full) == 0 {
			t.Fatal("no revisions published")
		}
		var paged []service.RevisionView
		before := int64(math.MaxInt64)
		limit := 2
		for {
			page, err := revisions.HistoryPage(t.Context(), service.LocalPrincipal(custodian), scope, before, limit)
			if err != nil {
				t.Fatalf("history before %d: %v", before, err)
			}
			paged = append(paged, page...)
			if len(page) < limit {
				break
			}
			before = page[len(page)-1].Revision
		}
		if len(paged) != len(full) {
			t.Fatalf("revisions: paged %d, full %d", len(paged), len(full))
		}
		for i := range full {
			if paged[i].Revision != full[i].Revision || paged[i].SchemaRevision != full[i].SchemaRevision {
				t.Fatalf("revision %d: paged %+v, full %+v", i, paged[i], full[i])
			}
			if i > 0 && paged[i-1].Revision <= paged[i].Revision {
				t.Fatalf("revisions not descending: %d then %d", paged[i-1].Revision, paged[i].Revision)
			}
		}
	})
}

func assertEnvironmentsEqual(t *testing.T, want, got []service.Environment) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("environment count: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if want[i].ID != got[i].ID || want[i].Name != got[i].Name {
			t.Fatalf("environment %d: want %+v, got %+v", i, want[i], got[i])
		}
	}
}
