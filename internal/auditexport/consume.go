// Package auditexport validates the negotiated audit export completion contract.
package auditexport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// MaxLineBytes bounds one event or receipt including its LF, matching the TS consumer.
const MaxLineBytes = 1 << 20

// Consume writes only event lines into an UNCOMMITTED staging destination. The
// caller owns resp.Body and must close it, discard staging on any error, and
// publish/update collection state only after this function returns success.
// Memory is bounded by one line, irrespective of the export's total size.
func Consume(resp *http.Response, dst io.Writer) (apigen.AuditExportReceipt, error) {
	var receipt apigen.AuditExportReceipt
	if resp == nil || resp.Body == nil || dst == nil {
		return receipt, fmt.Errorf("audit export: response body and staging writer are required")
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-ndjson" || resp.StatusCode != http.StatusOK || resp.Header.Get("Hikyo-Audit-Receipt") != "v1" {
		return receipt, fmt.Errorf("audit export: negotiated completion unavailable (HTTP %d); retain previous collection state", resp.StatusCode)
	}
	id, snapshot := resp.Header.Get("Hikyo-Audit-Export-ID"), resp.Header.Get("Hikyo-Audit-Snapshot-Time")
	if id == "" || snapshot == "" {
		return receipt, fmt.Errorf("audit export: missing response identity or snapshot")
	}
	reader := bufio.NewReaderSize(resp.Body, MaxLineBytes+1)
	var rows int64
	terminal := false
	for {
		line, err := reader.ReadSlice('\n')
		if err == io.EOF && len(line) == 0 {
			break
		}
		if err != nil {
			return receipt, fmt.Errorf("audit export: incomplete or oversized line: %w", err)
		}
		if len(line) > MaxLineBytes || !utf8.Valid(line) {
			return receipt, fmt.Errorf("audit export: oversized line or invalid UTF-8")
		}
		if terminal {
			return receipt, fmt.Errorf("audit export: data follows terminal receipt")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil || fields == nil {
			return receipt, fmt.Errorf("audit export: invalid JSON object")
		}
		if raw, ok := fields["export_receipt"]; ok {
			if len(fields) != 1 {
				return receipt, fmt.Errorf("audit export: invalid receipt envelope")
			}
			var receiptFields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &receiptFields); err != nil || !required(receiptFields, "version", "started_event_id", "snapshot_time", "rows_streamed", "outcome", "outcome_recorded") {
				return receipt, fmt.Errorf("audit export: missing required receipt fields")
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&receipt); err != nil {
				return receipt, fmt.Errorf("audit export: invalid receipt: %w", err)
			}
			var receiptSnapshot string
			if json.Unmarshal(receiptFields["snapshot_time"], &receiptSnapshot) != nil || receipt.SnapshotTime.IsZero() || receipt.Version != apigen.AuditExportReceiptV1 || receipt.StartedEventId != id || receipt.RowsStreamed != rows || receiptSnapshot != snapshot {
				return receipt, fmt.Errorf("audit export: receipt identity, snapshot or row count mismatch")
			}
			_, hasCause := receiptFields["cause"]
			if receipt.Outcome != apigen.AuditExportReceiptOutcomeSuccess || !receipt.OutcomeRecorded || hasCause {
				return receipt, fmt.Errorf("audit export: incomplete (%s); no verified export saved; retain previous collection state", receipt.Outcome)
			}
			terminal = true
			continue
		}
		var event apigen.AuditEvent
		if !required(fields, "seq", "id", "type", "schema_version", "occurred_at", "occurred_asserted", "recorded_at", "actor_class", "scope_class", "outcome", "origin", "payload") || json.Unmarshal(line, &event) != nil || event.Id == "" || event.Seq < 0 {
			return receipt, fmt.Errorf("audit export: invalid event envelope")
		}
		if n, err := dst.Write(line); err != nil {
			return receipt, fmt.Errorf("audit export: staging write failed: %w", err)
		} else if n != len(line) {
			return receipt, fmt.Errorf("audit export: staging write failed: %w", io.ErrShortWrite)
		}
		rows++
	}
	if !terminal {
		return receipt, fmt.Errorf("audit export: missing terminal receipt; retain previous collection state")
	}
	return receipt, nil
}

func required(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}
