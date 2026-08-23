package compose

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestBuildRenderPlanLiveOfflineEquivalence(t *testing.T) {
	targets := []RenderTarget{
		{Name: "api", KeyIDs: []string{"key_url", "key_mode"}},
		{Name: "worker", KeyIDs: []string{"key_mode"}},
	}
	rows := []RenderSourceRow{
		{KeyID: "key_url", Name: "DATABASE_URL", Classification: "secret", State: RenderRowValued, Value: "postgres://db"},
		{KeyID: "key_mode", Name: "APP_MODE", Classification: "config", State: RenderRowValued, Value: "production"},
	}

	live, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeyRefuseNotDelivered,
		Targets:    targets,
		Rows:       rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	offline, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeyRefuseNotInSnapshot,
		Targets:    targets,
		Rows:       append([]RenderSourceRow(nil), rows...),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(live, offline) {
		t.Fatalf("equivalent live/offline plans differ:\n live: %#v\noffline: %#v", live, offline)
	}
}

func TestBuildRenderPlanGolden(t *testing.T) {
	plan, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeySkip,
		Targets: []RenderTarget{
			{Name: "api", KeyIDs: []string{"key_url", "key_unset", "key_projected", "key_path"}},
			{Name: "worker", KeyIDs: []string{"key_bad_name", "key_multiline"}},
		},
		Rows: []RenderSourceRow{
			{KeyID: "key_url", Name: "DATABASE_URL", Classification: "config", State: RenderRowValued, Value: "postgres://db"},
			{KeyID: "key_unset", Name: "OPTIONAL", Classification: "config", State: RenderRowNoValue},
			{KeyID: "key_path", Name: "PATH", Classification: "config", State: RenderRowValued, Value: "/srv/bin"},
			{KeyID: "key_bad_name", Name: "BAD-NAME", Classification: "config", State: RenderRowValued, Value: "x"},
			{KeyID: "key_multiline", Name: "MULTILINE", Classification: "config", State: RenderRowValued, Value: "a\nb"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/renderplan/config-only.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("render plan golden mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestBuildRenderPlanRejectsUnknownAbsentKeyPolicy(t *testing.T) {
	_, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeyPolicy("bogus"),
		Targets:    []RenderTarget{{Name: "api", KeyIDs: []string{"key_url"}}},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown absent-key policy")
	}
}

func TestBuildRenderPlanRejectsUnknownRowState(t *testing.T) {
	_, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeySkip,
		Targets:    []RenderTarget{{Name: "api", KeyIDs: []string{"key_url"}}},
		Rows: []RenderSourceRow{{
			KeyID: "key_url", Name: "DATABASE_URL", State: RenderRowState("bogus"), Value: "postgres://db",
		}},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown render-row state")
	}
}

func TestBuildRenderPlanRejectsValueForNonValuedRow(t *testing.T) {
	for _, state := range []RenderRowState{RenderRowNoValue, RenderRowUnrevealedSecret} {
		t.Run(string(state), func(t *testing.T) {
			_, err := BuildRenderPlan(RenderInput{
				AbsentKeys: AbsentKeySkip,
				Targets:    []RenderTarget{{Name: "api", KeyIDs: []string{"key_url"}}},
				Rows: []RenderSourceRow{{
					KeyID: "key_url", Name: "DATABASE_URL", State: state, Value: "postgres://db",
				}},
			})
			if err == nil {
				t.Fatal("expected an error for a non-valued row carrying a value")
			}
		})
	}
}

func TestBuildRenderPlanModelsFullProjectionRefusals(t *testing.T) {
	plan, err := BuildRenderPlan(RenderInput{
		AbsentKeys: AbsentKeyRefuseNotDelivered,
		Targets: []RenderTarget{{
			Name: "api", KeyIDs: []string{"key_missing", "key_secret"},
		}},
		Rows: []RenderSourceRow{{
			KeyID: "key_secret", Name: "DB_PASSWORD", Classification: "secret", State: RenderRowUnrevealedSecret,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderRefusal{
		{Target: "api", Key: "key_missing", Kind: RenderRefusalKeyNotDelivered},
		{Target: "api", Key: "DB_PASSWORD", Kind: RenderRefusalSecretUnrevealed},
	}
	if !reflect.DeepEqual(plan.Refusals, want) {
		t.Fatalf("refusals = %#v, want %#v", plan.Refusals, want)
	}
}
