package service

import (
	"errors"
	"io"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

type shortExportWriter struct{}

func (shortExportWriter) Write(line []byte) (int, error) { return len(line) - 1, nil }

func TestAuditExportShortWriteIsNotACompleteEvent(t *testing.T) {
	if err := writeLine(shortExportWriter{}, store.AuditEvent{RawPayload: `{}`}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short sink write = %v, want io.ErrShortWrite", err)
	}
}
