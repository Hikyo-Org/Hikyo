package audit

import (
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/freetext"
)

// FreeTextBound is the shared free-text byte bound.
const FreeTextBound = freetext.FreeTextBound

// RedactionMarker replaces Hikyo bearer tokens in free text.
const RedactionMarker = freetext.RedactionMarker

// RedactTokens uses the shared token grammar.
func RedactTokens(s string) string { return freetext.RedactTokens(s) }

// SanitizeFreeText applies the shared free-text hygiene before audit capture.
func SanitizeFreeText(s string) string { return freetext.SanitizeFreeText(s) }

// checkSanitized is the write-boundary re-check: a free-text value that
// SanitizeFreeText would change is refused, because an emitter that skipped
// sanitization is a bug, not something to paper over silently.
func checkSanitized(s string) error {
	if s != SanitizeFreeText(s) {
		return fmt.Errorf("free-text value was not sanitized before the write boundary")
	}
	return nil
}
