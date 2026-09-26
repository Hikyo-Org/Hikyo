// Package sealedreceiver is the reference implementation of the receiver side
// of the sealed webhook protocol (docs/spec/sealed-webhook.md). It exists for
// documentation, interoperability, and adversarial tests; it is not shipped
// in the Hikyo release binary.
//
// It verifies the envelope signature against pinned sender keys before
// decrypting, authorizes the namespace with the binding carried inside the
// ciphertext, keeps an idempotency store so at-least-once delivery is safe,
// tracks which route owns each name so it never overwrites an unowned value,
// and signs every acknowledgement bound to the exact envelope digest.
package sealedreceiver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

// Config is the receiver's pinned trust material and tenant bindings.
type Config struct {
	Identity   string
	AckSigner  *sealedhook.Signer
	Senders    []sealedhook.PinnedKey
	TargetID   string
	InstanceID string
	Generation int64
	// Bindings maps a namespace to the binding credential the tenant set as
	// its Hikyo adapter credential.
	Bindings map[string]string
	Now      func() time.Time
	// AfterApply runs after a mutation is durably applied and before the
	// acknowledgement is written. Tests use it to simulate a crash window.
	AfterApply func(w http.ResponseWriter) bool
}

// Entry is one applied value and the route that owns it. An empty Route
// marks a pre-existing value Hikyo does not own.
type Entry struct {
	Value    string `json:"value"`
	Route    string `json:"route"`
	Revision int64  `json:"revision"`
}

type outcome struct {
	Status sealedhook.AckStatus `json:"status"`
	Reason string               `json:"reason,omitempty"`
}

// State is the receiver's durable state.
type State struct {
	Values map[string]map[string]Entry `json:"values"`
	Seen   map[string]outcome          `json:"seen"`
}

// Receiver is an http.Handler serving sealedhook.SyncPath.
type Receiver struct {
	cfg    Config
	opener *sealedhook.Opener
	mu     sync.Mutex
	state  State
	// Persist, when set, is called with the new state after every mutation.
	Persist func(State) error
}

// New validates the configuration.
func New(cfg Config) (*Receiver, error) {
	opener, err := sealedhook.NewOpener(cfg.Identity, cfg.Senders)
	if err != nil {
		return nil, err
	}
	if cfg.AckSigner == nil || !sealedhook.ValidID(cfg.TargetID) || !sealedhook.ValidID(cfg.InstanceID) || cfg.Generation < 1 {
		return nil, errors.New("sealedreceiver: ack key, target id, instance id, and generation are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Receiver{cfg: cfg, opener: opener, state: State{Values: map[string]map[string]Entry{}, Seen: map[string]outcome{}}}, nil
}

// SetAfterApply installs the crash-window hook (tests only).
func (r *Receiver) SetAfterApply(fn func(http.ResponseWriter) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.AfterApply = fn
}

// Restore replaces the state (for example from a state file after restart).
func (r *Receiver) Restore(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.Values == nil {
		s.Values = map[string]map[string]Entry{}
	}
	if s.Seen == nil {
		s.Seen = map[string]outcome{}
	}
	r.state = s
}

// Seed installs a pre-existing, unowned value.
func (r *Receiver) Seed(namespace, surface, name, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bucket(namespace)[surface+"/"+name] = Entry{Value: value}
}

// Snapshot returns a copy of one namespace, keyed "surface/NAME".
func (r *Receiver) Snapshot(namespace string) map[string]Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]Entry{}
	for k, v := range r.state.Values[namespace] {
		out[k] = v
	}
	return out
}

// Keys lists a namespace's keys in order.
func (r *Receiver) Keys(namespace string) []string {
	snap := r.Snapshot(namespace)
	out := make([]string, 0, len(snap))
	for k := range snap {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Receiver) bucket(namespace string) map[string]Entry {
	b := r.state.Values[namespace]
	if b == nil {
		b = map[string]Entry{}
		r.state.Values[namespace] = b
	}
	return b
}

func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost || req.URL.Path != sealedhook.SyncPath || req.URL.RawQuery != "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if media, _, err := mime.ParseMediaType(req.Header.Get("Content-Type")); err != nil || media != sealedhook.ContentType {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, sealedhook.MaxEnvelope+1))
	if err != nil || len(body) > sealedhook.MaxEnvelope {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	opened, err := r.opener.Open(body, r.cfg.Now())
	if err != nil {
		// No acknowledgement: an unauthenticated or expired envelope earns
		// nothing signed. The sender records the outcome as unknown.
		http.Error(w, "envelope refused", http.StatusBadRequest)
		return
	}
	status, reason, applied := r.apply(opened)
	r.mu.Lock()
	after := r.cfg.AfterApply
	r.mu.Unlock()
	if applied && after != nil && !after(w) {
		return
	}
	ack, err := r.cfg.AckSigner.SignAck(opened.Digest, opened.Envelope.IdempotencyKey, status, reason)
	if err != nil {
		http.Error(w, "ack", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", sealedhook.AckContentType)
	_, _ = w.Write(ack)
}

func (r *Receiver) apply(o sealedhook.Opened) (sealedhook.AckStatus, string, bool) {
	e := o.Envelope
	if e.TargetID != r.cfg.TargetID || e.InstanceID != r.cfg.InstanceID {
		return sealedhook.AckRejected, "wrong_target", false
	}
	if e.Generation != r.cfg.Generation {
		return sealedhook.AckRejected, "generation_mismatch", false
	}
	want, ok := r.cfg.Bindings[e.Namespace]
	if !ok || subtle.ConstantTimeCompare([]byte(want), []byte(o.Payload.Binding)) != 1 {
		return sealedhook.AckRejected, "unauthorized", false
	}
	if e.Op == sealedhook.OpProbe {
		return sealedhook.AckApplied, "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, seen := r.state.Seen[e.IdempotencyKey]; seen {
		if prior.Status == sealedhook.AckApplied {
			return sealedhook.AckAlreadyApplied, "", false
		}
		return prior.Status, prior.Reason, false
	}
	result := r.mutate(e, o.Payload)
	r.state.Seen[e.IdempotencyKey] = result
	if r.Persist != nil {
		if err := r.Persist(r.state); err != nil {
			delete(r.state.Seen, e.IdempotencyKey)
			return sealedhook.AckRejected, "persist_failed", false
		}
	}
	return result.Status, result.Reason, result.Status == sealedhook.AckApplied
}

func (r *Receiver) mutate(e sealedhook.Envelope, p sealedhook.Payload) outcome {
	b := r.bucket(e.Namespace)
	for _, n := range e.Names {
		cur, exists := b[n.Surface+"/"+n.Name]
		if exists && cur.Route != e.Route {
			return outcome{Status: sealedhook.AckConflict, Reason: "exists_unowned"}
		}
		if exists && cur.Revision > e.Source.Revision {
			return outcome{Status: sealedhook.AckRejected, Reason: "stale_revision"}
		}
	}
	switch e.Op {
	case sealedhook.OpUpsert:
		for _, v := range p.Values {
			b[v.Surface+"/"+v.Name] = Entry{Value: v.Value, Route: e.Route, Revision: e.Source.Revision}
		}
	case sealedhook.OpPrune:
		for _, n := range e.Names {
			delete(b, n.Surface+"/"+n.Name)
		}
	}
	return outcome{Status: sealedhook.AckApplied}
}

// MarshalState encodes the state for a state file.
func MarshalState(s State) ([]byte, error) { return json.MarshalIndent(s, "", "  ") }
