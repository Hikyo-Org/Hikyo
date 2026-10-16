package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type topologyRevisionService struct {
	historyRevisionService
	events <-chan service.AdvisoryEvent
}

func (s topologyRevisionService) Watch(context.Context, service.Actor, domain.Scope) (<-chan service.AdvisoryEvent, error) {
	return s.events, nil
}

func TestTopologyEventsRequireExplicitAccept(t *testing.T) {
	for _, tt := range []struct {
		name    string
		accept  []string
		include bool
	}{
		{name: "legacy default"},
		{name: "legacy SSE", accept: []string{"text/event-stream"}},
		{name: "explicit", accept: []string{"text/event-stream; hikyo-topology=1"}, include: true},
		{name: "media list", accept: []string{"application/json, text/event-stream; hikyo-topology=1; q=0.5"}, include: true},
		{name: "multiple headers", accept: []string{"application/json", "text/event-stream; hikyo-topology=1"}, include: true},
		{name: "quoted extension", accept: []string{`text/event-stream; hikyo-topology="1"`}, include: true},
		{name: "quoted comma in list", accept: []string{`application/json; extra="a,b", text/event-stream; hikyo-topology=1`}, include: true},
		{name: "quoted lookalike", accept: []string{`application/json; extra="a,text/event-stream; hikyo-topology=1,b"`}},
		{name: "escaped quote lookalike", accept: []string{`application/json; extra="a\",text/event-stream; hikyo-topology=1,b"`}},
		{name: "disabled", accept: []string{"text/event-stream; hikyo-topology=0"}},
		{name: "wrong media", accept: []string{"application/json; hikyo-topology=1"}},
		{name: "wildcard", accept: []string{"*/*; hikyo-topology=1"}},
		{name: "unacceptable", accept: []string{"text/event-stream; hikyo-topology=1; q=0"}},
		{name: "invalid quality", accept: []string{"text/event-stream; hikyo-topology=1; q=NaN"}},
		{name: "malformed", accept: []string{"text/event-stream; hikyo-topology"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan service.AdvisoryEvent, 2)
			events <- service.AdvisoryEvent{Type: service.AdvisoryEnvironmentsChanged, ProjectID: "prj_real"}
			events <- service.AdvisoryEvent{Type: service.AdvisoryPublished, EnvironmentID: "env_real", Revision: 1}
			close(events)
			a := &API{Revisions: topologyRevisionService{events: events}}
			r := httptest.NewRequest("GET", "/api/v1/orgs/org_real/projects/prj_real/events", nil)
			for _, value := range tt.accept {
				r.Header.Add("Accept", value)
			}
			ctx := context.WithValue(t.Context(), requestKey{}, r)
			response, err := a.WatchProjectEvents(ctx, apigen.WatchProjectEventsRequestObject{Org: "org_real", Project: "prj_real"})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			if err := response.VisitWatchProjectEventsResponse(&deadlineRecorder{ResponseRecorder: recorder}); err != nil {
				t.Fatal(err)
			}
			body := recorder.Body.String()
			if got := strings.Contains(body, "event: environments.changed\n"); got != tt.include {
				t.Fatalf("topology included = %t, want %t: %s", got, tt.include, body)
			}
			if !strings.Contains(body, "event: revision.published\ndata: {\"environment_id\":\"env_real\",\"revision\":1,\"type\":\"revision.published\"}") {
				t.Fatalf("existing event changed or disappeared: %s", body)
			}
			if tt.include && !strings.Contains(body, "data: {\"project_id\":\"prj_real\",\"type\":\"environments.changed\"}\n") {
				t.Fatalf("wrong project-only payload: %s", body)
			}
			if recorder.Header().Get("Vary") != "Accept" {
				t.Fatalf("negotiated response missing Vary: Accept: %v", recorder.Header())
			}
		})
	}
}

func TestTopologyWireContainsOnlyProjectMetadata(t *testing.T) {
	// Even accidental environment fields on this internal event cannot escape
	// into the project-only wire contract.
	ev := service.AdvisoryEvent{
		Type: service.AdvisoryEnvironmentsChanged, ProjectID: "prj_real",
		EnvironmentID: "env_hidden", KeyID: "key_hidden", KeyName: "hidden",
		ActorID: "usr_hidden", Revision: 42,
	}
	want := map[string]any{"type": "environments.changed", "project_id": "prj_real"}
	if got := wireAdvisory(ev); !reflect.DeepEqual(got, want) {
		encoded, _ := json.Marshal(got)
		t.Fatalf("topology disclosed extra metadata: %s", encoded)
	}
	if acceptsAdvisoryTopology(nil) {
		t.Fatal("missing request opted in to topology events")
	}
}
