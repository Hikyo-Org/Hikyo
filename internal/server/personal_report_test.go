package server

import (
	"encoding/json/v2"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestPersonalValueCountEmitsDeprecatedHoldersAlias(t *testing.T) {
	raw, err := json.Marshal(personalValueCountView(service.PersonalCount{KeyID: "key_1", Name: "DB", Members: 3}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["members"] != float64(3) || got["holders"] != float64(3) {
		t.Fatalf("count = %s, want members and the deprecated holders both 3", raw)
	}
}
