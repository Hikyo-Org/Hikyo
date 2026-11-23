// Package badredact is the negative fixture for the redaction analyzer:
// every construct here must be caught, or the analyzer is vacuous.
package badredact

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Leak formats and marshals sensitive types outside their owning package,
// and mirrors audit content into the ops log.
func Leak(kr *crypto.Keyring, ps *crypto.ProjectSealer, ev audit.Event, row store.AuditEvent) {
	fmt.Printf("keyring: %v\n", kr)       // sensitive → fmt // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%#v", ps)            // sensitive → fmt // want "passes sensitive type .*internal/crypto\\.ProjectSealer to fmt"
	_, _ = json.Marshal(kr)               // sensitive → encoding/json // want "passes sensitive type .*internal/crypto\\.Keyring to encoding/json"
	_, _ = jsonv2.Marshal(kr)             // sensitive → encoding/json/v2 // want "passes sensitive type .*internal/crypto\\.Keyring to encoding/json/v2"
	slog.Info("audit event", "event", ev) // audit content → slog // want "logs audit content .*internal/audit\\.Event"
	log.Printf("row: %v", row)            // audit content → log // want "logs audit content .*internal/store\\.AuditEvent"
}

// Erasure evasions: the argument's own static type is `any`, but the value
// is unchanged: the analyzer must look through the conversion and the map
// index.
func LeakByErasure(kr *crypto.Keyring, ev audit.Event) {
	slog.Info("audit", "event", any(ev))     // erased audit content // want "logs audit content .*internal/audit\\.Event"
	log.Printf("field: %v", ev.Payload["x"]) // pinned map indexed // want "logs audit content .*internal/audit\\.Payload"
	fmt.Printf("%v", any(kr))                // erased sensitive type // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
}

// Both LocalKeys forms must be guarded, including static-type erasure.
func LeakLocalKeys(value crypto.LocalKeys, pointer *crypto.LocalKeys) {
	fmt.Printf("%#v", value)                 // want "passes sensitive type .*internal/crypto\\.LocalKeys to fmt"
	fmt.Printf("%v", any(pointer))           // want "passes sensitive type .*internal/crypto\\.LocalKeys to fmt"
	_, _ = json.Marshal(value)               // want "passes sensitive type .*internal/crypto\\.LocalKeys to encoding/json"
	slog.Info("local keys", "keys", pointer) // want "passes sensitive type .*internal/crypto\\.LocalKeys to log/slog"
}

type inner struct{ name string }

type holder struct {
	keyring *crypto.Keyring
	seq     int
	ev      audit.Event
	inner   inner
	get     func() any
	box     anybox
}

// A field selected from a struct that merely holds a sensitive value is not a
// leak: only the selected field is read (#1468). TestRedactFormatNarrowingDifferential
// lists every diagnostic this drops. Narrowing, NarrowingKeepsLeaks and Promoted
// must stay flagged.
func NarrowingNonSensitive(h holder, hp *holder) {
	_ = fmt.Sprintf("%d", h.seq)
	_ = fmt.Sprintf("%s", h.inner.name)
	_ = fmt.Sprintf("%d", (*hp).seq)
	_ = fmt.Sprintf("%d", hp.seq)
	_ = fmt.Sprintf("%d", mkHolder().seq)
	slog.Info("n", "n", h.seq)
	_ = fmt.Sprintf("%d", h.count())
}

func mkHolder() holder { return holder{} }

// Selections that can still read the pinned value stay flagged: the pinned
// field itself, a method on it, and an audit-content field of a holder.
func Narrowing(h holder) {
	_ = fmt.Sprintf("%v", h.keyring)              // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", any(h.keyring))         // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", h.keyring.String())     // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	slog.Info("audit", "actor", h.ev.Actor)       // want "passes sensitive type .*internal/crypto\\.Keyring to log/slog" "logs audit content .*internal/audit\\.Event"
	slog.Info("audit", "seq", h.ev.SchemaVersion) // want "passes sensitive type .*internal/crypto\\.Keyring to log/slog" "logs audit content .*internal/audit\\.Event"
}

func (h holder) count() int { return h.seq }

func (h holder) anyKey() any { return h.keyring }

// reveal and revealBytes build text or bytes from the held key material.
func (h holder) reveal() string       { return h.keyring.String() }
func (h holder) revealBytes() []byte  { return []byte(h.keyring.String()) }
func (h holder) revealArray() [4]byte { return [4]byte{} }
func (h holder) evAny() any           { return h.ev }

type anybox struct{ v any }

func box(v any) anybox { return anybox{v: v} }

// Leaks through a call argument, a composite literal, a method returning
// `any`, or a field walk several levels into audit content.
func NarrowingKeepsLeaks(h holder, kr *crypto.Keyring, ev audit.Event, row store.AuditEvent) {
	_ = fmt.Sprintf("%v", h.anyKey())                            // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", box(kr).v)                             // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", struct{ v any }{kr}.v)                 // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	slog.Info("audit", "v", box(ev).v)                           // want "logs audit content .*internal/audit\\.Event"
	slog.Info("audit", "v", h.evAny())                           // want "passes sensitive type .*internal/crypto\\.Keyring to log/slog" "logs audit content .*internal/audit\\.Event"
	_ = fmt.Sprintf("%s", h.reveal())                            // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%x", h.revealBytes())                       // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%x", h.revealArray())                       // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", h.get())                               // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", h.box.v)                               // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	slog.Info("audit", "id", ev.Actor.ID)                        // want "logs audit content .*internal/audit\\.Event"
	slog.Info("audit", "id", row.Actor.ID)                       // want "logs audit content .*internal/store\\.AuditEvent"
	slog.Info("audit", "at", ev.OccurredAt.Format(time.RFC3339)) // want "logs audit content .*internal/audit\\.Event"
}

type wrap struct{ *crypto.Keyring }

// A method or field promoted from an embedded sensitive value stays flagged.
func Promoted(w wrap) {
	_ = fmt.Sprintf("%v", w.String()) // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
}
