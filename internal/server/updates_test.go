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
)

type stubUpdates struct {
	status updatecheck.Status
}

func (s stubUpdates) GetStatus(context.Context, service.Actor) (updatecheck.Status, error) {
	return s.status, nil
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
	want := "{\"available\":true,\"channel\":\"nightly\",\"current_version\":\"1.0.0\",\"latest_version\":\"1.1.0-nightly.20260824.42.g176e6e67\",\"prerelease\":true,\"published_at\":\"2026-08-24T02:00:00Z\",\"release_url\":\"https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.g176e6e67\"}\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %s, want %s", recorder.Body.String(), want)
	}
}
