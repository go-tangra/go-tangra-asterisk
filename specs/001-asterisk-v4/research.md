# Research: Asterisk V4

Research was performed against local primary source checkouts on 2026-10-02. Paths below are relative to `/home/jadmin/projects/go-tangra/`. No remote release assumptions are needed to produce these design artifacts.

## D1 — Feature baseline

**Decision**: Preserve source code capabilities, including live calls, recordings, directional RTP diagnostics and Prometheus dashboards; dashboard persistence is not required.
**Rationale**: `go-tangra-asterisk/README.md` understates the implementation. The five service proto files, `internal/server/http.go`, `recordings.go`, `calls_stream.go`, `internal/exporter/` and `frontend/src/views/` establish the wider scope.
**Alternatives considered**: README-only parity would omit working features; blind copying would preserve unsafe legacy authentication assumptions.

## D2 — V4 runtime and versions

**Decision**: Use Freya runtime with SPIFFE mTLS, operation policy, graceful lifecycle, audit/OTel and admin listener, with published `/v4` dependencies. Baseline Go 1.26.3/toolchain 1.26.8, framework v4.3.1 and auth/portal SDK v4.1.0.
**Rationale**: Verified in `go-tangra/go.mod`, `README.md`, `go-tangra-dns-v4/go.mod`, `internal/app/app.go` and `cmd/dnssvc/main.go`.
**Alternatives considered**: Keeping old Kratos v2/Wire bootstrap and common SDK would not implement the V4 lifecycle. Optional LCM enrollment can follow DNS identity wiring where required.

## D3 — Gateway and permission registration

**Decision**: Derive route permissions from embedded OpenAPI, register a leased portal manifest after readiness, separately seed auth roles/grants with retries. Module `asterisk`, API `/api/asterisk`, remote base `/m/asterisk/`, exposes `./routes` and `./nav`.
**Rationale**: DNS `pkg/dnsmanifest/manifest.go`, `internal/app/app.go`, `permissions.go` and portal `sdk/pkg/gatewayclient` are the concrete patterns. Validate manifest/handler drift and reject undeclared permissions.
**Alternatives considered**: Legacy descriptor/menus registration or invented registration protocol is unnecessary.

## D4 — Trusted ownership and authorization

**Decision**: Independently verify platform bearer identity and revocation, authorize each operation through auth, and enforce a single tenant/PBX binding before all reads. No caller-controlled tenant header selects a PBX.
**Rationale**: DNS `internal/httpapi/middleware.go` and `internal/authz/authz.go` verify user/tenant and permissions; source PBX tables contain no tenant discriminator. Legacy metadata parsing alone does not isolate a shared database.
**Alternatives considered**: Shared unpartitioned PBX access is rejected. A multi-PBX registry is outside initial scope.

## D5 — Data and optional features

**Decision**: Retain MySQL PBX schemas and compatible registration history; restrict migrations to the optional module-owned store. Probe CEL/quality columns and optional names without modifying source schemas. Historical registration is observed state with expiry and explicit gaps.
**Rationale**: Legacy `internal/data/mysql.go`, `pjsip_reg_repo.go`, `rtpqos.go` and repositories provide reusable behavior. Derive pickup from CEL answer/start timing; preserve grouping and direction with fixtures.
**Alternatives considered**: Converting all data to PostgreSQL would add migration work without a user requirement. Reconstructing lost AMI history would imply unavailable evidence.

## D6 — UI migration

**Decision**: Rebuild views with the V4 UI kit, portal HTTP client, CASL and typed OpenAPI; embed built remote under `/ui/` with a no-UI build fallback. Shared production singletons follow DNS configuration.
**Rationale**: DNS `ui/package.json`, `module-federation.config.ts`, `vite.config.ts`, `embed.go`; framework `ui/kit/README.md`.
**Alternatives considered**: Copying Ant Design/VxeTable and legacy shell imports conflicts with established V4 UI conventions. Dashboard views/filter parity does not imply saved-dashboard CRUD.

## D7 — Streams, recordings and monitoring

**Decision**: Protect both snapshots and SSE, preserve byte ranges and confined recording resolution, isolate monitoring upstream per tenant/PBX, and keep exporter scraping on a private endpoint. Set bounded stream lifetimes supported by portal route timeouts; heartbeat and fresh snapshot on reconnect. Verify the actual portal proxy streams/flushes before release.
**Rationale**: Legacy direct HTTP handlers bypass authentication; V4 DNS manifest supports route-specific timeout, while framework edge supplies bounded SSE. Shared PromQL cannot be made tenant-safe by trusting browser filters.
**Alternatives considered**: Anonymous data handlers, infinite buffers and unrestricted shared PromQL are rejected. Legacy public URL aliases are not needed.

All design unknowns are resolved by explicit initial-scope defaults. Implementation must verify published pins and portal stream behavior against these primary sources; this is validation work, not an unresolved product clarification.
