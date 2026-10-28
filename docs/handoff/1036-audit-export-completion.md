# #1036: verifiable audit export completion

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/1036>
PR: <https://github.com/Hikyo-Org/Hikyo/pull/1044>

## Ownership and integration

This change owns export completion, its generated contract and reusable clients.
#1035 / #1045 owns new instance routes, the actual audit query/export CLI and
Audit page controls/navigation. Merge #1044 before #1045 and rerun that lane's
instance, CLI and browser acceptance on the integrated source. #1037 owns the
independent receiver/destination, collection authorization and live acceptance.
This handoff grants no unattended instance authority or destination access.

The supported integration seams are:

- Go: `internal/auditexport.Consume(resp *http.Response, dst io.Writer)
  (apigen.AuditExportReceipt, error)`. The caller closes the response and owns
  private staging. Only success allows publishing the staged plain-event JSONL.
- Browser: `downloadAuditExport(url: string, signal?: AbortSignal): Promise<Blob>`
  in `web/src/api/audit.ts`. Save the returned Blob only after resolution; surface
  rejection as an export failure. It negotiates, rejects redirects/off-origin
  URLs and uses same-origin credentials. `verifiedAuditExportBlob(Response)` is
  available when the caller already has a response.
- Streaming TypeScript: `consumeAuditExport(Response, write)` in
  `clients/ts/src/auditExport.ts`. `write` receives original event-line bytes and
  may be asynchronous for backpressure. It must write only to unpublished
  staging, since rejection can follow the last event.
- Instance producer: `Audits.InstanceExportActorReceipt` returns the same
  `service.ExportReceipt` as `ExportActor`. #1035 must use it through
  `auditExportStream`, with the instance route's negotiated `receipt` flag.
  Existing error-only instance service entry points remain available.

## Wire contract and compatibility

Request `receipt=v1`. A supporting server returns:

```text
Content-Type: application/x-ndjson
Hikyo-Audit-Receipt: v1
Hikyo-Audit-Export-ID: <durable export_started event id>
Hikyo-Audit-Snapshot-Time: <effective inclusive recorded_at upper bound>
```

After zero or more ordinary event lines, exactly one line contains
`export_receipt`. Completion requires all of the following, then clean transport
EOF immediately after that line's LF:

1. `version` is integer 1.
2. Nonempty `started_event_id` matches `Hikyo-Audit-Export-ID`.
3. Required `snapshot_time` matches `Hikyo-Audit-Snapshot-Time` exactly.
4. Integer `rows_streamed` equals the number of received event lines.
5. `outcome` is `success`, `outcome_recorded` is true, and `cause` is absent.
6. No second receipt, event, blank line, malformed line or trailing bytes follows.

The upper bound is the earlier of the requested `to` and the server-fixed cutoff.
It is correlated with normalized filters in the durable INTENT. The count is the
number of event lines accepted by the sink, not a count of public sequence gaps.
The receipt is an authenticated response contract, not a signed archival proof.

Unnegotiated requests retain plain event-only JSONL. Older servers or clients
without a valid negotiated receipt cannot prove completion. Supported consumers
fail closed on that response, even for HTTP 200 and even for an empty body.
They never retry silently in legacy mode. Adding these fields to the existing
unmerged v1 draft does not change already shipped plain JSONL.

Both consumers bound each line to 1 MiB including LF and preserve event bytes,
unknown additive event fields and int64 precision. Larger lines fail explicitly.
Streaming consumers hold one line, not the full export. Browser Blob downloads
are capped at 64 MiB of event bytes; larger downloads require CLI/disk staging.

## Failure behavior

Failure before any event bytes retains the ordinary non-200 refusal. After HTTP
200, page-read, writer-barrier and terminal-outcome-write failures are incomplete.
A disconnect can leave valid event JSONL with no receipt. A receipt that cannot
be written or whose transport fails after arrival also cannot establish completion.
Short writes are sink failures and cannot increment the count as full lines.

Per-page bearer resolution/authorization, the export budget, fixed cutoff,
retention-change refusal and commit-order paging remain owned by the existing
service/store path. A revoked reader receives no further page of events.
The existing `ErrExportUnpaired` behavior remains: a revoked caller cannot mint
the proof needed to write `audit.export_completed`; its started event remains a
reconciliation case. Its receipt says `revoked`, `outcome_recorded=false` and
contains only its own export metadata/count. This is not a new proof-free writer
or a waiver of the audit ADR's existing review obligations.

## Collection recipe

Use the supported CLI command from #1035 once the dependency is integrated.
An external receiver must perform the same protocol validation before committing
any collection state. HTTP status, successful JSON parsing and callbacks that
wrote event lines are insufficient proof.

1. Choose one authorized retained scope and explicit filters; acquire its export
   budget through the ordinary server route with `receipt=v1`.
2. Create a private disposable staging file. Stream event lines through the
   consumer. Do not emit them to stdout, publish an archive or advance a cursor.
3. On any protocol, transport, authorization or staging error, discard staging,
   retain the last complete collection state and surface missed collection.
   Do not claim the failed export's evidence was preserved.
4. After successful validation, flush and durably commit the archive and receipt
   using the destination's atomic publication policy. Advance collection state
   only after that destination commit. Preserve origin, requested scope/filters,
   receipt and archive integrity metadata together in the restricted destination.
5. Replay the full retained authorized scope and deduplicate by stable event ID
   and content. A duplicate ID with different content must fail. Neither the
   last public `seq` nor its maximum is a safe continuation cursor on PostgreSQL;
   this change introduces no new resumable cursor. Retention limits the history
   that remains available and successful export cannot recover pruned records.

## Validation and remaining gates

The baseline identity regression failed because the draft response lacked
`Hikyo-Audit-Export-ID`. Follow-up validation records belong here after execution.
The integration commit is deliberately available before final validation so
#1035 can exercise the actual commands and UI without duplicating this protocol.

Maintainer security review remains required before merge. Cross-provider review
is SKIPPED under the current unavailable subscription boundary, never CLEAN.
Hosted current-head feedback and full merge-candidate proof are separate from
local tests. No merge, release, deployment, live-provider or external-destination
acceptance is authorized or established by this source change.
