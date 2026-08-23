# Issue #198 — native TLS and operational listener

## Delivered contract

- Public API/UI/SSE and operational `/healthz`, `/readyz`, `/metrics` use separate listeners and routers. Operational default: `127.0.0.1:8081`.
- A non-loopback public listener requires a validated TLS pair or exact trusted proxy CIDRs. TLS keys must be mode `0400` or `0600`.
- Native TLS uses Go defaults with TLS 1.2 minimum and an atomic certificate holder. HSTS is emitted only for non-loopback native TLS.
- Certificate files are polled by modification time and size every 10 seconds. `SIGHUP` triggers immediate reload without cancelling serve. A bad replacement retains the last known-good pair and increments a label-free failure counter.
- Both listeners bind before boot returns. Either listener's unexpected failure shuts down both; normal cancellation shares one five-second graceful-shutdown budget.

## Operational metrics

- `hikyo_tls_cert_not_after_timestamp_seconds`
- `hikyo_tls_reload_failures_total`

Both are available only from the operational router. Existing retention metrics moved with them.

## Packaging and docs

- Helm accepts either `tls.existingSecret` (`tls.crt` and `tls.key`) or `network.trustedProxyCIDRs`, exposes container ports 8080/8081, and probes the `ops` port with HTTP. A rootless same-image init and watcher stage the root-owned Secret into an owner-mode `0400` emptyDir key, preserving renewal reloads without weakening file policy.
- `Dockerfile.release` documents both ports with `EXPOSE 8080 8081`.
- README, installation, configuration, self-hosting, and self-hoster checklist now describe native TLS first and proxy mode second.
- Ingress, ACME, mTLS, TLS on the operational listener, reference proxy Compose, and systemd units remain outside #198.

## Deliberate timeout shape

`ReadHeaderTimeout` is 10 seconds and `MaxHeaderBytes` is 64 KiB. `WriteTimeout` remains unset because public SSE is long-lived; its heartbeat/write-deadline contract remains the documented exception.
