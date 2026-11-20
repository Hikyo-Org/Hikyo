// Package badredact is the negative fixture for the redaction analyzer:
// every construct here must be caught, or the analyzer is vacuous.
package badredact

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log"
	"log/slog"

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
// is unchanged — the analyzer must look through the conversion and the map
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

type holder struct {
	keyring *crypto.Keyring
	seq     int
	ev      audit.Event
}

// A field of a struct that merely holds a sensitive value is not the value;
// the sensitive field itself and a field walk of audit content still are.
func Narrowing(h holder) {
	_ = fmt.Sprintf("id_%d", h.seq)
	_ = fmt.Sprintf("%v", h.keyring)      // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", any(h.keyring)) // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%d", h.count())
	_ = fmt.Sprintf("%v", h.keyring.String()) // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	slog.Info("audit", "actor", h.ev.Actor)   // want "logs audit content .*internal/audit\\.Event"
}

func (h holder) count() int { return h.seq }

func (h holder) anyKey() any { return h.keyring }
func (h holder) evAny() any  { return h.ev }

type anybox struct{ v any }

func box(v any) anybox { return anybox{v: v} }

// Narrowing a selection must not hide a pinned value that reaches the output
// through a call argument, a composite literal, or a method returning `any`.
func NarrowingKeepsLeaks(h holder, kr *crypto.Keyring, ev audit.Event) {
	_ = fmt.Sprintf("%v", h.anyKey())            // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", box(kr).v)             // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	_ = fmt.Sprintf("%v", struct{ v any }{kr}.v) // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
	slog.Info("audit", "v", box(ev).v)           // want "logs audit content .*internal/audit\\.Event"
	slog.Info("audit", "v", h.evAny())           // want "passes sensitive type .*internal/crypto\\.Keyring to log/slog" "logs audit content .*internal/audit\\.Event"
}

type wrap struct{ *crypto.Keyring }

// A method or field promoted from an embedded sensitive value stays flagged.
func Promoted(w wrap) {
	_ = fmt.Sprintf("%v", w.String()) // want "passes sensitive type .*internal/crypto\\.Keyring to fmt"
}
