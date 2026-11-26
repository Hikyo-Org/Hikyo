package server

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// resolveRules is a RuleService whose Resolve answers a fixed list.
type resolveRules struct {
	RuleService
	views []service.AccessView
}

func (r resolveRules) Resolve(context.Context, service.Actor, domain.Scope, domain.Capability, string) ([]service.AccessView, error) {
	return r.views, nil
}

// answer builds a view through the embedded answer's promoted fields, so this
// package never imports the chokepoint.
func answer(id string, state service.AccessState, class domain.PrincipalClass) service.AccessView {
	var v service.AccessView
	v.Principal, v.State, v.Class = domain.PrincipalID(id), state, class
	return v
}

func resolveWire(t *testing.T, views []service.AccessView) (apigen.AccessResolution, []byte, error) {
	t.Helper()
	api := &API{Rules: resolveRules{views: views}}
	resp, err := api.ResolveAccess(t.Context(), apigen.ResolveAccessRequestObject{
		Org: "org_a", Project: "prj_a", Environment: "env_a",
		Params: apigen.ResolveAccessParams{Permission: apigen.RuleCapabilityReveal},
	})
	if err != nil {
		return apigen.AccessResolution{}, nil, err
	}
	out := apigen.AccessResolution(resp.(apigen.ResolveAccess200JSONResponse))
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return out, raw, nil
}

func TestResolveAccessGroupsEveryState(t *testing.T) {
	view := func(id string, state service.AccessState, class domain.PrincipalClass) service.AccessView {
		return answer(id, state, class)
	}
	out, _, err := resolveWire(t, []service.AccessView{
		view("usr_can", service.AccessCan, domain.ClassHuman),
		view("usr_exc", service.AccessExcepted, domain.ClassHuman),
		view("usr_see", service.AccessNeedsSee, domain.ClassHuman),
		view("mch_none", service.AccessNone, domain.ClassWorkload),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Can) != 1 || out.Can[0].PrincipalId != "usr_can" || out.Can[0].Kind != apigen.Human ||
		len(out.Excepted) != 1 || out.Excepted[0].PrincipalId != "usr_exc" ||
		len(out.NeedsSee) != 1 || out.NeedsSee[0].PrincipalId != "usr_see" ||
		len(out.None) != 1 || out.None[0].PrincipalId != "mch_none" || out.None[0].Kind != apigen.Machine {
		t.Fatalf("grouping = %+v", out)
	}
}

func TestResolveAccessEmptyGroupsAreArrays(t *testing.T) {
	_, raw, err := resolveWire(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{`"can":[]`, `"excepted":[]`, `"needs_see":[]`, `"none":[]`} {
		if !strings.Contains(string(raw), group) {
			t.Fatalf("%s missing from %s", group, raw)
		}
	}
	_, raw, err = resolveWire(t, []service.AccessView{answer("usr_a", service.AccessCan, domain.ClassHuman)})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"grants":[]`, `"rules":[]`, `"excepts":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("%s missing from %s", field, raw)
		}
	}
}

// An answer the transport cannot place is a server fault, never a quiet
// `none`, and a principal with no known class is never labelled a machine.
func TestResolveAccessRefusesWhatItCannotClassify(t *testing.T) {
	for name, v := range map[string]service.AccessView{
		"unknown state": answer("usr_a", "mystery", domain.ClassHuman),
		"unknown class": answer("usr_b", service.AccessCan, ""),
	} {
		if _, _, err := resolveWire(t, []service.AccessView{v}); err == nil {
			t.Fatalf("%s: answered instead of failing", name)
		}
	}
}
