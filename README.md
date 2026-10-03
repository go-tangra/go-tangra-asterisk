# Tangra V4 Asterisk

A read-only PBX observer for the Tangra V4 portal. It provides logical call history, extension/ringgroup statistics, observed registration history, live-call SSE, confined recording playback, directional RTP diagnostics and dedicated PBX monitoring. One service instance binds one PBX exclusively to one authenticated tenant.

## Build and verification

Use Go 1.26.8 (selected by `go.mod`) and Node 22. GitHub Packages access is required for `@go-tangra/ui`; set `NODE_AUTH_TOKEN` outside the repository. Published Go dependencies and the UI package lock are committed.

```sh
make ui-install
make generate check-generated
make test test-race ui-check
make build
```

`make build` embeds the federation remote (`./routes`, `./nav`) at `/ui/`; the portal proxies it at `/m/asterisk/`. `make build-no-ui` creates a backend-only binary. The default UI development entry denies permission-dependent actions; use the portal to receive session and CASL abilities.

## Configure and run

Populate the variables referenced by `configs/dev.yaml`: `ASTERISK_TENANT_ID`, `ASTERISK_PBX_ID`, `ASTERISK_CDR_DSN`, `ASTERISK_AUTH_TARGET`, `ASTERISK_AUTH_ISSUER` and `ASTERISK_PORTAL_TARGET`. Optional variables are `ASTERISK_CONFIG_DSN`, `ASTERISK_REGISTRATION_DSN`, `ASTERISK_RECORDING_ROOT`, `ASTERISK_MONITORING_URL`. Empty optional values disable their features. DSNs use the Go MySQL driver format; the source database timezone is explicit and defaults to Europe/Sofia. Source MySQL DATETIME values represent that zone; instants returned to clients are UTC.

Supply a SPIRE workload socket and a matching service identity, trust domain and mesh policy. There is no plaintext/insecure service fallback. Adjust the gateway workload SPIFFE ID in `deploy/policy.yaml` and provide matching auth/portal peer policies allowing this service's outbound SDK calls. Discovery entries are mesh gRPC targets, following the platform deployment's endpoint conventions. Admin `/healthz`, `/readyz`, `/metrics` default to loopback port 9094; non-loopback admin requires mTLS and explicit `allow_non_loopback`. Admin readiness includes source availability. Keep scraping private; never proxy `/metrics` as a browser API.

Grant only SELECT on PBX CDR/CEL and configuration schemas. The service runs no PBX migrations or control actions. The history source is required; CEL, RTP columns and directory names are probed and optional. Mount recordings read-only. Use a dedicated Prometheus upstream for the configured PBX/tenant, set `monitoring_dedicated: true`, and isolate its scrape targets. This setting is an operator assertion of dedicated storage, not an automatic partitioning mechanism.

Registration uses a **separate module-owned** MySQL database. Bootstrap is explicit:

```sh
./bin/asterisksvc bootstrap -config configs/dev.yaml
./bin/asterisksvc -config configs/dev.yaml
```

AMI is independently optional: set `ami.enabled`, address, username and secret in a private configuration file or use `${VARIABLE}` expansion. Use an AMI account whose read privileges cover call/contact events and `CoreShowChannels`/`PJSIPShowContacts` plus the optional read-only `CoreSettings`, `CoreStatus`, `PJSIPShowEndpoints`, `SIPpeers` and `QueueStatus` metrics actions; grant no dialing, hangup or configuration privileges. TLS is supported with normal certificate verification. Live monitoring works without registration storage. History works without AMI. Reconciliation records current contacts and identifies unobserved intervals; it never reconstructs lost events.

Viewer permissions exclude recordings. Investigators have all six module permissions. Owner/admin grants are investigator; operator/member/auditor grants are viewer. Authorization remains server-side for snapshots, ranges, streams and monitoring. Recording routes require both `calls:read` and `recordings:read`.

## Deployment and acceptance

`Dockerfile` builds as non-root and uses a BuildKit `npm_token` secret. `deploy/compose.yaml` joins an existing mesh network and publishes no host ports. `deploy/tangra-asterisk.service` is the systemd alternative. Adjust paths, domain, discovery targets and environment files for your installation; keep PBX/dialplan changes manual.

Use `deploy/fixtures.yaml` for a disposable MySQL 8.4 fixture. See [docs/validation.md](docs/validation.md) for commands, measured checks and release blockers. Full portal/MySQL/performance acceptance is required before release; a passing unit suite does not establish these deployment results.
