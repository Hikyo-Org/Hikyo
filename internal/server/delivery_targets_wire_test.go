package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Delivery-target condition reporting (#788) at the transport: every refusal
// class reaches its closed wire code, the list renders against the contract,
// and an over-size report is refused without reaching the report path.

const deliveryTargetsPath = api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f11/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f12/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f13/delivery-targets"

// dtDelivery records which service door the transport took.
type dtDelivery struct {
	stubDelivery
	reportErr error
	calls     *[]string
	scope     *domain.Scope
	report    *deliverytarget.Report
	list      service.DeliveryTargetList
}

func (s dtDelivery) ReportTarget(_ context.Context, _ string, scope domain.Scope, report deliverytarget.Report) error {
	*s.calls = append(*s.calls, "report")
	*s.scope, *s.report = scope, report
	return s.reportErr
}

func (s dtDelivery) RefuseOversizeReport(_ context.Context, _ string, scope domain.Scope) error {
	*s.calls = append(*s.calls, "oversize")
	*s.scope = scope
	return service.ErrReportTooLarge
}

func (s dtDelivery) ListTargets(context.Context, service.Actor, domain.Scope) (service.DeliveryTargetList, error) {
	return s.list, nil
}

func newDTDelivery(reportErr error) dtDelivery {
	return dtDelivery{reportErr: reportErr, calls: &[]string{}, scope: &domain.Scope{}, report: &deliverytarget.Report{}}
}

func wireReport() apigen.DeliveryTargetReportRequest {
	return apigen.DeliveryTargetReportRequest{
		Vocabulary: 1,
		Target: apigen.DeliveryTargetRef{
			ClusterId: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f00", InstanceUid: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f01",
			Namespace: "apps", Name: "api.web", Uid: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f02",
		},
		Generation: 3, ObservedGeneration: 3, ReportedAt: time.Unix(1_800_000_000, 0).UTC(),
		ReportIntervalSeconds: 600, Lifecycle: "Synced",
		Conditions: []apigen.DeliveryTargetCondition{{Type: "Ready", Status: "True", Reason: "Reconciled", ObservedGeneration: 3}},
		Reporter:   apigen.DeliveryTargetReporter{Integration: "kubernetes-operator", Version: "1.4.0-rc.1+build.7"},
	}
}

func TestDeliveryTargetReportReachesTheService(t *testing.T) {
	del := newDTDelivery(nil)
	srv := federationServer(t, stubFederation{}, del)
	resp, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", wireReport())
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("report -> %d: %s", resp.StatusCode, payload)
	}
	got := *del.report
	if got.Target.Name != "api.web" || got.Reporter != "kubernetes-operator" || got.ReporterVersion != "1.4.0-rc.1+build.7" ||
		len(got.Conditions) != 1 || got.Conditions[0].Reason != "Reconciled" || del.scope.Env != "env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f13" {
		t.Fatalf("service received %+v in %+v", got, *del.scope)
	}
}

func TestDeliveryTargetReportRefusalsMapToTheirCodes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   apigen.ErrorCode
		detail string
	}{
		{"unauthorized", domain.ErrNotFound, http.StatusNotFound, apigen.ErrorCodeNotFound, ""},
		{"revoked credential", domain.ErrUnauthenticated, http.StatusUnauthorized, apigen.ErrorCodeUnauthenticated, ""},
		{"quota", fmt.Errorf("%w: quota", domain.ErrLimitExceeded), http.StatusConflict, apigen.ErrorCodeLimitExceeded, ""},
		{"size", service.ErrReportTooLarge, http.StatusRequestEntityTooLarge, apigen.ErrorCodePayloadTooLarge, ""},
		{"vocabulary", vocabularyErr{detail: "conditions[0].reason"}, http.StatusUnprocessableEntity, apigen.ErrorCodeUnprocessable, "conditions[0].reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := federationServer(t, stubFederation{}, newDTDelivery(tc.err))
			resp, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", wireReport())
			if resp.StatusCode != tc.status {
				t.Fatalf("-> %d, want %d: %s", resp.StatusCode, tc.status, payload)
			}
			body := decodeError(t, payload)
			if body.Error.Code != tc.code {
				t.Fatalf("code %q, want %q", body.Error.Code, tc.code)
			}
			if got := ""; body.Error.Detail != nil {
				got = *body.Error.Detail
				if got != tc.detail {
					t.Fatalf("detail %q, want %q", got, tc.detail)
				}
			} else if tc.detail != "" {
				t.Fatalf("detail absent, want %q", tc.detail)
			}
		})
	}
	// The quota refusal is the NAMED bound.
	srv := federationServer(t, stubFederation{}, newDTDelivery(domain.ErrLimitExceeded))
	_, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", wireReport())
	if msg := decodeError(t, payload).Error.Message; !strings.Contains(msg, fmt.Sprintf("at most %d delivery targets", deliverytarget.MaxRowsPerPrincipal)) {
		t.Fatalf("limit_exceeded message %q does not name the delivery-target bound", msg)
	}
}

// vocabularyErr is the service's 422 shape: ErrReportVocabulary carrying the
// member as its SafeDetail (the isolation suite pins that the real service
// returns exactly this).
type vocabularyErr struct{ detail string }

func (e vocabularyErr) Error() string      { return e.detail }
func (e vocabularyErr) Unwrap() error      { return service.ErrReportVocabulary }
func (e vocabularyErr) SafeDetail() string { return e.detail }

// TestDeliveryTargetOversizeReportIsRefusedUnparsed: a body past 8 KiB never
// reaches the report path or the contract's body validation, and the size door
// receives the scope the route named.
func TestDeliveryTargetOversizeReportIsRefusedUnparsed(t *testing.T) {
	del := newDTDelivery(nil)
	srv := federationServer(t, stubFederation{}, del)
	pad := json.RawMessage(`{"pad":"` + strings.Repeat("x", deliverytarget.MaxReportBytes) + `"}`)
	resp, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", pad)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize report -> %d: %s", resp.StatusCode, payload)
	}
	// The bound is signalled to net/http (MaxBytesReader on the root writer,
	// not LimitReader): the server closes the connection after the refusal
	// instead of draining the rest of the body. The client folds the wire's
	// `Connection: close` into resp.Close.
	if !resp.Close {
		t.Fatal("oversize refusal kept the connection open")
	}
	if got := *del.calls; !slices.Equal(got, []string{"oversize"}) {
		t.Fatalf("service doors = %v, want only the size refusal", got)
	}
	if del.scope.Org != "org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f11" || del.scope.Env != "env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f13" {
		t.Fatalf("size refusal scope = %+v, want the route's", *del.scope)
	}
	// Exactly at the bound the body is parsed as a report.
	within := newDTDelivery(nil)
	srv = federationServer(t, stubFederation{}, within)
	raw, err := json.Marshal(wireReport())
	if err != nil {
		t.Fatal(err)
	}
	padded := json.RawMessage(string(raw) + strings.Repeat(" ", deliverytarget.MaxReportBytes-len(raw)))
	if resp, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", padded); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("report at the bound -> %d: %s", resp.StatusCode, payload)
	}
}

func TestDeliveryTargetListRendersAgainstTheContract(t *testing.T) {
	at := time.Unix(1_800_000_000, 0).UTC()
	del := newDTDelivery(nil)
	del.list = service.DeliveryTargetList{
		Reporters: []service.DeliveryTargetReporter{
			{PrincipalID: "mch_one", LastContactAt: at},
			{PrincipalID: "mch_two", QuotaRefusedAt: at},
		},
		Targets: []service.DeliveryTarget{{
			ID: "dtr_one", PrincipalID: "mch_one", ReceivedAt: at, State: deliverytarget.StateRefused,
			RefusalCause: deliverytarget.RefusalVocabulary, RefusedAt: at.Add(time.Minute),
			Report: deliverytarget.Report{
				Vocabulary: 1, Target: deliverytarget.Target{
					ClusterID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f00", InstanceUID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f01",
					Namespace: "apps", Name: "api", UID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f02",
				},
				Generation: 2, ObservedGeneration: 2, ReportedAt: at, ReportIntervalSeconds: 300, Lifecycle: "Synced",
				Conditions: []deliverytarget.Condition{{Type: "Ready", Status: "True", Reason: "Reconciled", ObservedGeneration: 2}},
				Reporter:   deliverytarget.ReporterKubernetesOperator, ReporterVersion: "1.0.0",
			},
		}},
	}
	srv := federationServer(t, stubFederation{}, del)
	resp, payload := call(t, srv, http.MethodGet, deliveryTargetsPath, "hik_1_cli_abc", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list -> %d: %s", resp.StatusCode, payload)
	}
	var got apigen.DeliveryTargetList
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Principals) != 2 || got.Principals[0].LastContactAt == nil || got.Principals[0].QuotaRefusedAt != nil ||
		got.Principals[1].LastContactAt != nil || got.Principals[1].QuotaRefusedAt == nil {
		t.Fatalf("principals = %s", payload)
	}
	if len(got.Targets) != 1 || got.Targets[0].State != "refused" || got.Targets[0].Refusal == nil || got.Targets[0].Refusal.Cause != "vocabulary" {
		t.Fatalf("targets = %s", payload)
	}
}

func TestMetaAdvertisesTheDeliveryTargetReportCapability(t *testing.T) {
	srv := federationServer(t, stubFederation{}, stubDelivery{})
	resp, payload := call(t, srv, http.MethodGet, api.PathPrefix+"/meta", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("meta -> %d: %s", resp.StatusCode, payload)
	}
	var meta apigen.Meta
	if err := json.Unmarshal(payload, &meta); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(meta.ProtocolCapabilities, apigen.ProtocolCapability("delivery-target-report/1")) {
		t.Fatalf("protocol_capabilities = %v, want delivery-target-report/1", meta.ProtocolCapabilities)
	}
	if meta.ApiRevision != api.Revision {
		t.Fatalf("api_revision = %d, want %d", meta.ApiRevision, api.Revision)
	}
}

// TestDeliveryTargetConditionGrammarAtTheWire: a condition type or reason the
// server's vocabulary does not know, but which fits the Kubernetes condition
// grammar, passes the contract and reaches the service (whose 422 names the
// member); a string outside the grammar is a contract 400 that never does.
func TestDeliveryTargetConditionGrammarAtTheWire(t *testing.T) {
	for _, tc := range []struct {
		name          string
		typ, reason   string
		reachesServer bool
	}{
		{"unknown qualified type", "example.com/Healthy", "Reconciled", true},
		{"unknown reason", "Ready", "SomethingNew", true},
		{"type outside the grammar", "not a type", "Reconciled", false},
		{"reason outside the grammar", "Ready", "9starts-with-digit", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			del := newDTDelivery(nil)
			srv := federationServer(t, stubFederation{}, del)
			body := wireReport()
			body.Conditions[0].Type, body.Conditions[0].Reason = tc.typ, tc.reason
			resp, payload := call(t, srv, http.MethodPost, deliveryTargetsPath, "hik_1_wl_abc", body)
			if tc.reachesServer {
				if resp.StatusCode != http.StatusNoContent || len(*del.calls) != 1 || del.report.Conditions[0].Type != tc.typ {
					t.Fatalf("-> %d, service saw %v: %s", resp.StatusCode, *del.calls, payload)
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest || len(*del.calls) != 0 {
				t.Fatalf("-> %d, service saw %v: %s", resp.StatusCode, *del.calls, payload)
			}
		})
	}
}
