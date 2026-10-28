package audit

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const RefusalSampleLimit = 32
const RefusalStateBytes = 16384

type RefusalStage string

const (
	RefusalArtifact  RefusalStage = "artifact"
	RefusalConflict  RefusalStage = "credential-conflict"
	RefusalProof     RefusalStage = "proof"
	RefusalCSRF      RefusalStage = "csrf"
	RefusalAdmission RefusalStage = "admission"
)

func RefusalStages() []RefusalStage {
	return []RefusalStage{RefusalArtifact, RefusalConflict, RefusalProof, RefusalCSRF, RefusalAdmission}
}

// RefusalSample carries only transport-derived attribution and closed operation
// vocabulary. It never carries a credential identity or artifact-state cause.
type RefusalSample struct {
	Stage     RefusalStage `json:"stage"`
	SourceIP  string       `json:"source_ip"`
	RequestID string       `json:"request_id"`
	Operation string       `json:"operation"`
}

var refusalOperation = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._:/-]{0,127}$`)

func (s RefusalSample) Validate() error {
	if refusalStageIndex(s.Stage) < 0 {
		return fmt.Errorf("audit: invalid refusal stage")
	}
	ip, err := netip.ParseAddr(s.SourceIP)
	if err != nil || ip.Zone() != "" || ip.Unmap().String() != s.SourceIP {
		return fmt.Errorf("audit: invalid refusal source")
	}
	id, err := uuid.Parse(s.RequestID)
	if err != nil || id.Version() != 7 || id.String() != s.RequestID {
		return fmt.Errorf("audit: invalid refusal request id")
	}
	if !refusalOperation.MatchString(s.Operation) || SanitizeFreeText(s.Operation) != s.Operation {
		return fmt.Errorf("audit: invalid refusal operation")
	}
	return nil
}

func refusalStageIndex(stage RefusalStage) int {
	for i, value := range RefusalStages() {
		if stage == value {
			return i
		}
	}
	return -1
}

// RefusalWindow is the singleton's bounded durable staging shape. Only its
// dedicated writer may mutate it. Counts are exact for committed updates.
type RefusalWindow struct {
	Counts  []int64         `json:"counts"`
	Samples []RefusalSample `json:"samples"`
}

func NewRefusalWindow() RefusalWindow {
	return RefusalWindow{Counts: make([]int64, 5), Samples: []RefusalSample{}}
}

func (w RefusalWindow) Validate() error {
	if len(w.Counts) != 5 || len(w.Samples) > RefusalSampleLimit {
		return fmt.Errorf("audit: invalid refusal window bounds")
	}
	var sampled [5]int64
	for _, s := range w.Samples {
		if err := s.Validate(); err != nil {
			return err
		}
		sampled[refusalStageIndex(s.Stage)]++
	}
	for i, count := range w.Counts {
		if count < sampled[i] || count < 0 {
			return fmt.Errorf("audit: invalid refusal count")
		}
	}
	return nil
}

func (w *RefusalWindow) Add(s RefusalSample) error {
	if err := s.Validate(); err != nil {
		return err
	}
	index := refusalStageIndex(s.Stage)
	if w.Counts[index] == math.MaxInt64 {
		return fmt.Errorf("audit: refusal counter saturated")
	}
	w.Counts[index]++
	if len(w.Samples) < RefusalSampleLimit {
		w.Samples = append(w.Samples, s)
	}
	return nil
}

func (w RefusalWindow) Empty() bool {
	for _, n := range w.Counts {
		if n != 0 {
			return false
		}
	}
	return true
}

// RefusalPayload produces one stage's closed summary, preserving the sample
// pairing without parallel arrays or delimiter encoding.
func (w RefusalWindow) RefusalPayload(stage RefusalStage, start int64) Payload {
	samples := []Payload{}
	for _, s := range w.Samples {
		if s.Stage == stage {
			samples = append(samples, Payload{"source_ip": s.SourceIP, "request_id": s.RequestID, "operation": s.Operation})
		}
	}
	count := w.Counts[refusalStageIndex(stage)]
	return Payload{"window_start": start, "window_end": start + 60, "stage": string(stage), "count": count, "unsampled_count": count - int64(len(samples)), "samples": samples}
}

type refusalReceipt struct {
	mu        sync.Mutex
	sample    RefusalSample
	dedicated bool
}
type refusalReceiptKey struct{}
type refusalArtifactProbeKey struct{}

// WithRefusalArtifactProbe keeps the shared receipt and admission result, but
// an incidental dead-cookie liveness check is not the final authentication
// outcome. The actual handler/chokepoint or CSRF refusal owns that decision.
func WithRefusalArtifactProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, refusalArtifactProbeKey{}, true)
}

func WithRefusalReceipt(ctx context.Context, source, requestID string) context.Context {
	return context.WithValue(ctx, refusalReceiptKey{}, &refusalReceipt{sample: RefusalSample{SourceIP: source, RequestID: requestID, Operation: "unmatched"}})
}

func requestReceipt(ctx context.Context) *refusalReceipt {
	r, _ := ctx.Value(refusalReceiptKey{}).(*refusalReceipt)
	return r
}

func NoteRefusal(ctx context.Context, stage RefusalStage) {
	if probe, _ := ctx.Value(refusalArtifactProbeKey{}).(bool); probe && stage == RefusalArtifact {
		return
	}
	if r := requestReceipt(ctx); r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.sample.Stage == "" || stage == RefusalAdmission {
			r.sample.Stage = stage
		}
	}
}

// SetRefusalOperation is called only with a validated registry member. Raw
// headers, paths, arguments and JSON-RPC ids must never reach this setter.
func SetRefusalOperation(ctx context.Context, operation string) {
	if r := requestReceipt(ctx); r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.sample.Operation = operation
	}
}

func RefusalRequestID(ctx context.Context) string {
	if r := requestReceipt(ctx); r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.sample.RequestID
	}
	return ""
}

func PendingRefusal(ctx context.Context) (RefusalSample, bool) {
	if r := requestReceipt(ctx); r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.sample, !r.dedicated && r.sample.Stage != ""
	}
	return RefusalSample{}, false
}

// MarkDedicatedRefusal is a COMMIT receipt, never an insert/capture receipt.
func MarkDedicatedRefusal(ctx context.Context) {
	if r := requestReceipt(ctx); r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.dedicated = true
	}
}

func IsDedicatedRefusal(e Event) bool {
	if e.Type == EventAuthArtifactClassRefused || e.Type == EventFederationRefused || e.Type == EventSCIMCredentialRefused {
		return true
	}
	return e.Type != EventRequestRefused && e.Outcome == OutcomeFailure && strings.HasPrefix(string(e.Type), "auth.")
}

func refusalInt(value any) int64 {
	switch n := value.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return -1
}

func validateRefusalSummary(e Event) error {
	p := e.Payload
	start, end, count, unsampled := refusalInt(p["window_start"]), refusalInt(p["window_end"]), refusalInt(p["count"]), refusalInt(p["unsampled_count"])
	stage, _ := p["stage"].(string)
	samples, _ := p["samples"].([]Payload)
	if e.Actor.Class != ActorUnauthenticated || e.Actor.ID != "" || e.Actor.CredentialID != "" || e.AuthorityID != "" || e.Object != (Object{}) || e.SourceIP != "" || e.UserAgent != "" || e.Origin != OriginSystem || start%60 != 0 || start > math.MaxInt64-60 || end != start+60 || count-int64(len(samples)) != unsampled {
		return fmt.Errorf("audit: invalid refusal summary envelope or counts")
	}
	for _, p := range samples {
		ip, _ := p["source_ip"].(string)
		id, _ := p["request_id"].(string)
		operation, _ := p["operation"].(string)
		if err := (RefusalSample{Stage: RefusalStage(stage), SourceIP: ip, RequestID: id, Operation: operation}).Validate(); err != nil {
			return err
		}
	}
	return nil
}
