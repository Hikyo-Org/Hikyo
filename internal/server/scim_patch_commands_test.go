package server

import (
	"encoding/json"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestPathlessGroupPatchPreservesAddVersusReplace(t *testing.T) {
	for _, tc := range []struct {
		op      string
		wantAdd bool
	}{
		{op: "add", wantAdd: true},
		{op: "replace", wantAdd: false},
	} {
		t.Run(tc.op, func(t *testing.T) {
			var body map[string]any
			raw := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"` + tc.op + `","value":{"members":[{"value":"usr_3"}]}}]}`
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				t.Fatal(err)
			}
			commands, scimErr := scimPatchGroupCommands(body)
			if scimErr != nil {
				t.Fatal(scimErr)
			}
			if len(commands) != 1 {
				t.Fatalf("commands = %#v, want one", commands)
			}
			_, add := commands[0].(service.GroupPatchAddMembers)
			_, replace := commands[0].(service.GroupPatchReplaceMembers)
			if add != tc.wantAdd || replace == tc.wantAdd {
				t.Fatalf("command = %T, add=%t replace=%t", commands[0], add, replace)
			}
		})
	}
}
