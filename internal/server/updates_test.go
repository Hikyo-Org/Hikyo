package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

type stubUpdates struct {
	status     updatecheck.Status
	report     securitystatus.Report
	reportErr  error
	job        updater.Job
	requestErr error
}

func (s stubUpdates) GetStatus(context.Context, service.Actor) (updatecheck.Status, error) {
	return s.status, nil
}

func (s stubUpdates) GetSecurityMaintenance(context.Context, service.Actor) (securitystatus.Report, error) {
	return s.report, s.reportErr
}

func (s stubUpdates) Request(context.Context, service.Actor, string) (updater.Job, error) {
	return s.job, s.requestErr
}

func TestRetiredInstanceUpdateReturnsUnsupportedConflict(t *testing.T) {
	refusal := fmt.Errorf("%w: %w", domain.ErrConflict, updater.ErrRemoteApplyDisabled)
	api := &API{Updates: stubUpdates{requestErr: refusal}}
	response, err := api.RequestInstanceUpdate(t.Context(), apigen.RequestInstanceUpdateRequestObject{
		Body: &apigen.InstanceUpdateRequest{Version: "1.2.3"},
	})
	if response != nil || !errors.Is(err, updater.ErrRemoteApplyDisabled) {
		t.Fatalf("response=%v error=%v, want disabled refusal", response, err)
	}
	recorder := httptest.NewRecorder()
	writeError(recorder, wireErrorFor(err), err.Error())
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "migration-safe rollback") {
		t.Fatalf("unsupported wire response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
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

func TestSecurityMaintenanceWireStatesAreDistinct(t *testing.T) {
	checked := time.Date(2026, 11, 4, 11, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		report securitystatus.Report
		want   string
	}{
		{"not reported", securitystatus.Report{}, `{"reported":false,"unreadable":false}`},
		{"unreadable", securitystatus.Report{Unreadable: true}, `{"reported":false,"unreadable":true}`},
		{"reported", securitystatus.Report{Reported: true, Status: securitystatus.Public{
			Mode: "automatic", Enrolled: true, Phase: "blocked", Reason: "retry_backoff", ProtectionGap: true,
			Applicable: []string{"HK-SA-1"}, CatalogSequence: 7, LastAttempt: checked, Action: "hikyo security-maintenance reconcile --confirm abc",
		}}, `{"reported":true,"status":{"action":"hikyo security-maintenance reconcile --confirm abc","applicable":["HK-SA-1"],"catalog_sequence":7,"coverage":[],"covered_components":[],"enrolled":true,"last_attempt":"2026-11-04T11:30:00Z","mode":"automatic","phase":"blocked","protection_gap":true,"reason":"retry_backoff"},"unreadable":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &API{Updates: stubUpdates{report: tc.report}}
			response, err := api.GetSecurityMaintenance(t.Context(), apigen.GetSecurityMaintenanceRequestObject{})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			if err := response.VisitGetSecurityMaintenanceResponse(recorder); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != tc.want {
				t.Fatalf("status = %d body = %s, want %s", recorder.Code, recorder.Body.String(), tc.want)
			}
		})
	}
}

func TestSecurityMaintenanceRefusalReachesTheWire(t *testing.T) {
	api := &API{Updates: stubUpdates{reportErr: domain.ErrUnauthorized}}
	response, err := api.GetSecurityMaintenance(t.Context(), apigen.GetSecurityMaintenanceRequestObject{})
	if response != nil || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("response=%v error=%v, want the service refusal", response, err)
	}
	recorder := httptest.NewRecorder()
	writeError(recorder, wireErrorFor(err), err.Error())
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("refusal status = %d, want 403", recorder.Code)
	}
}
