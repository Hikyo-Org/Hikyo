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
