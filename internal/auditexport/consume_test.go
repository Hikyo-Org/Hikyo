package auditexport

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const testSnapshot = "2026-10-28T00:00:00Z"
const testEvent = `{"seq":9223372036854775807,"id":"evt_1","type":"future.event","schema_version":1,"occurred_at":"2026-01-01T00:00:00Z","occurred_asserted":false,"recorded_at":"2026-01-01T00:00:00Z","actor_class":"system","scope_class":"instance","outcome":"success","origin":"system","payload":{"future":true},"future_field":true}` + "\n"

func testReceipt(rows int) string {
	return fmt.Sprintf(`{"export_receipt":{"version":1,"started_event_id":"evt_export","snapshot_time":"%s","rows_streamed":%d,"outcome":"success","outcome_recorded":true}}`+"\n", testSnapshot, rows)
}

func testResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{
		"Content-Type": {"application/x-ndjson"}, "Hikyo-Audit-Receipt": {"v1"},
		"Hikyo-Audit-Export-Id": {"evt_export"}, "Hikyo-Audit-Snapshot-Time": {testSnapshot},
	}}
}

func TestConsume(t *testing.T) {
	for _, rows := range []int{0, 1, 1101} {
		t.Run(fmt.Sprintf("success_%d", rows), func(t *testing.T) {
			var dst bytes.Buffer
			events := strings.Repeat(testEvent, rows)
			receipt, err := Consume(testResponse(events+testReceipt(rows)), &dst)
			if err != nil || receipt.RowsStreamed != int64(rows) || dst.String() != events {
				t.Fatalf("receipt=%+v err=%v staged=%d bytes", receipt, err, dst.Len())
			}
		})
	}
	valid := testEvent + testReceipt(1)
	for name, body := range map[string]string{
		"empty": "", "partial_valid_jsonl": testEvent, "missing_lf": strings.TrimSuffix(valid, "\n"),
		"wrong_count": testEvent + testReceipt(2), "wrong_identity": strings.Replace(valid, "evt_export", "evt_other", 1),
		"wrong_snapshot":    strings.Replace(valid, testSnapshot, "2026-10-27T00:00:00Z", 1),
		"failure":           strings.Replace(valid, `"outcome":"success","outcome_recorded"`, `"outcome":"failure","outcome_recorded"`, 1),
		"revoked":           strings.Replace(valid, `"outcome":"success","outcome_recorded"`, `"outcome":"revoked","outcome_recorded"`, 1),
		"not_recorded":      strings.Replace(valid, `"outcome_recorded":true`, `"outcome_recorded":false`, 1),
		"cause":             strings.Replace(valid, `"outcome_recorded":true`, `"outcome_recorded":true,"cause":null`, 1),
		"duplicate_receipt": valid + testReceipt(1), "event_after_receipt": valid + testEvent,
		"trailing_blank": valid + "\n", "invalid_json": "no\n" + testReceipt(1),
		"invalid_event": "{}\n" + testReceipt(1), "invalid_utf8": strings.Replace(valid, "future.event", "\xff", 1),
		"oversized_line":        strings.Repeat(" ", MaxLineBytes) + valid,
		"missing_required":      strings.Replace(valid, `"version":1,`, "", 1),
		"null_required":         strings.Replace(valid, `"rows_streamed":1`, `"rows_streamed":null`, 1),
		"receipt_unknown_field": strings.Replace(valid, `"version":1,`, `"version":1,"extra":true,`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			// Model a collector: staging may contain partial rows; published
			// evidence and its previous collection state must remain untouched.
			published := "previous complete export"
			var staged bytes.Buffer
			if _, err := Consume(testResponse(body), &staged); err == nil {
				published = staged.String()
				t.Fatal("uncertain export accepted")
			}
			if published != "previous complete export" {
				t.Fatal("incomplete export advanced collection")
			}
		})
	}
	for _, header := range []string{"Hikyo-Audit-Receipt", "Hikyo-Audit-Export-ID", "Hikyo-Audit-Snapshot-Time", "Content-Type"} {
		t.Run("missing_"+header, func(t *testing.T) {
			resp := testResponse(valid)
			resp.Header.Del(header)
			var dst bytes.Buffer
			if _, err := Consume(resp, &dst); err == nil || dst.Len() != 0 {
				t.Fatalf("unnegotiated response accepted: %v", err)
			}
		})
	}
	t.Run("read_error_after_receipt", func(t *testing.T) {
		resp := testResponse(valid)
		resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(valid), failingReader{}))
		if _, err := Consume(resp, io.Discard); err == nil {
			t.Fatal("receipt without clean transport EOF accepted")
		}
	})
	t.Run("staging_write_failure", func(t *testing.T) {
		if _, err := Consume(testResponse(valid), failingWriter{}); err == nil {
			t.Fatal("failed destination accepted")
		}
	})
	t.Run("staging_short_write", func(t *testing.T) {
		if _, err := Consume(testResponse(valid), shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("short staging write = %v", err)
		}
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("transport failed") }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type shortWriter struct{}

func (shortWriter) Write(line []byte) (int, error) { return len(line) - 1, nil }
