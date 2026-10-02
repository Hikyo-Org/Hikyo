package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/scimproto"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestSCIMGroupMemberLimitWireRefusal(t *testing.T) {
	got := scimError(service.ErrSCIMGroupMemberLimit)
	if got.Status != http.StatusBadRequest || got.SCIMType != scimproto.TypeInvalidValue || !strings.Contains(got.Detail, "1000") {
		t.Fatalf("member-limit refusal must name ceiling as SCIM invalidValue: %#v", got)
	}
}
