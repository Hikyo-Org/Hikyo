package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

type stubUpdates struct {
	status updatecheck.Status
	job    updater.Job
}

func (s stubUpdates) GetStatus(context.Context, service.Actor) (updatecheck.Status, error) {
	return s.status, nil
}

func (s stubUpdates) Request(context.Context, service.Actor, string) (updater.Job, error) {
	return s.job, nil
}

func (s stubUpdates) GetJob(context.Context, service.Actor, string) (updater.Job, error) {
	return s.job, nil
}

func TestUpdateStatusWireCarriesChannelAndRelease(t *testing.T) {
	published := time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)
	api := &API{Updates: stubUpdates{status: updatecheck.Status{
		Channel:        updatecheck.ChannelNightly,
		CurrentVersion: "1.0.0",
		LatestVersion:  "1.1.0-nightly.20260824.42.g176e6e67",
		URL:            "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.g176e6e67",
		Available:      true,
		Prerelease:     true,
		PublishedAt:    published,
	}}}
	response, err := api.GetUpdateStatus(t.Context(), apigen.GetUpdateStatusRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetUpdateStatusResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	want := "{\"apply_supported\":false,\"available\":true,\"channel\":\"nightly\",\"current_version\":\"1.0.0\",\"latest_version\":\"1.1.0-nightly.20260824.42.g176e6e67\",\"prerelease\":true,\"published_at\":\"2026-08-24T02:00:00Z\",\"release_url\":\"https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.g176e6e67\"}\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestInstanceUpdateWireCarriesDurableJobState(t *testing.T) {
	requested := time.Date(2026, 8, 24, 3, 0, 0, 0, time.UTC)
	job := updater.Job{
		ID: "upd_0198aa", Backend: updater.BackendFlux, Version: "1.1.0",
		State: updater.StateQueued, Phase: "queued", RequestedAt: requested,
	}
	api := &API{Updates: stubUpdates{job: job}}
	response, err := api.RequestInstanceUpdate(t.Context(), apigen.RequestInstanceUpdateRequestObject{
		Body: &apigen.InstanceUpdateRequest{Version: "1.1.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitRequestInstanceUpdateResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	want := "{\"backend\":\"flux\",\"id\":\"upd_0198aa\",\"phase\":\"queued\",\"requested_at\":\"2026-08-24T03:00:00Z\",\"state\":\"queued\",\"version\":\"1.1.0\"}\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %s, want %s", recorder.Body.String(), want)
	}
}
