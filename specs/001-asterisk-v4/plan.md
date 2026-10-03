# Implementation Plan: Asterisk module for Tangra V4

**Branch**: No Git checkout available; feature `001-asterisk-v4` | **Date**: 2026-10-02 | **Spec**: [spec.md](spec.md)

**Input**: `specs/001-asterisk-v4/spec.md`

## Summary

Create `github.com/go-tangra/go-tangra-asterisk/v4`, preserving the legacy module's implemented call history, statistics, registration, live-call, recording, quality and monitoring behavior. Replace bootstrap, identity, registration and legacy UI integration with the verified V4 patterns. Browser APIs use `/api/asterisk`; no legacy public gRPC contract is required. PBX-owned MySQL stays read-only, and the optional module-owned MySQL registration store is retained.

## Technical Context

**Language/Version**: Go 1.26.3, toolchain 1.26.8; TypeScript/Vue 3.5.

**Primary Dependencies**: Tangra `/v4` v4.3.1; auth and portal `/sdk/v4` v4.1.0; framework transport/edge; MySQL driver; kin-openapi; Vue Router 5, Pinia 4, CASL, Zod 4, `@go-tangra/ui` 4.3.x, Vite federation. Optional LCM SDK v4.1.0 only if deployment uses enrollment. Pin compatible published versions in setup; these are observed local baselines, not a claim about latest releases.

**Storage**: Existing PBX CDR/CEL/queue-log and config MySQL databases; optional module-owned registration MySQL table; in-memory live registry. No added PostgreSQL/Valkey requirement.

**Testing**: Go unit/contract/integration tests, race detector, fake AMI/Prometheus, disposable MySQL fixture, Vitest and Playwright portal acceptance.

**Target Platform**: Linux, container and systemd deployment, V4 mesh/portal.

**Project Type**: Service with embedded federated web UI.

**Performance Goals**: SC-004: p95 history/report interactions <=2s with 100k legs/50 active calls/10 viewers; live receipt-to-display <=2s. Record hardware and benchmark fixture.

**Constraints**: One configured tenant owns the PBX; bounded queries/pages/stream buffers; read-only PBX credentials; recordings mounted read-only; no insecure transport fallback.

**Scale/Scope**: Six workflows, one PBX binding per process, independently optional registration store, AMI, recordings and monitoring.

## Constitution Check

The repository constitution is an unratified placeholder template, so it supplies no actionable gates. Do not treat commented example principles as adopted rules or invent a ratification. Pre-research check passes against the user-authorized scope. Design check passes: explicit V4 runtime, verified tenant identity, least-privilege PBX access, bounded optional workers and acceptance validation. Constitution adoption is separate work, not a blocker or silent mutation here.

## Project Structure

```text
specs/001-asterisk-v4/
  spec.md  plan.md  research.md  data-model.md  quickstart.md  tasks.md
  contracts/http-api.md  contracts/platform.md
  checklists/requirements.md
cmd/asterisksvc/                   # main.go, version.go
internal/app/                     # app.go, registration.go
internal/config/                  # config.go
internal/httpapi/                 # router, middleware and story handlers
internal/pbx/                     # ownership binding, pools, schema probes
internal/cdr/                     # repository, grouping, extension and QoS parsing
internal/stats/                   # aggregates and bucket semantics
internal/registration/            # repository, state evaluator, migrations
internal/ami/                     # protocol, listener and reconciliation
internal/calls/                   # registry
internal/recordings/              # confined file streaming
internal/dashboard/               # isolated metrics client
internal/exporter/                # PBX metrics collectors
pkg/asteriskmanifest/             # permissions, roles, nav and manifest
api/openapi/                      # asterisk.yaml and embed.go
ui/                              # V4 kit views, API client, federation and embedding
configs/ deploy/ tests/ docs/
```

**Structure Decision**: Explicit constructor wiring follows V4 module examples; copy reusable domain logic selectively and adapt it behind tenant-bound interfaces rather than carrying Kratos v2 bootstrap, Wire, common registration or Ant Design/VxeTable into the new project.

## Design and Delivery

1. Establish runtime, trusted identity, module manifest and read-only PBX binding before enabling data routes.
2. Deliver call history with a functioning portal remote as MVP; implement statistics as the next P1 slice.
3. Add optional registration and live monitoring with one shared AMI listener; add recording/quality and dashboards.
4. Maintain a capability response; disabled routes return explicit feature-unavailable errors rather than appearing absent.
5. Do not expose unrestricted shared PromQL. Initial monitoring uses a dedicated PBX/tenant upstream; shared endpoints require a separately proven isolation adapter.
6. Use V4 administrative health/metrics paths and mesh policies. Dedicated Prometheus PBX scraping is private/network-restricted and never an anonymous portal data route.

## Complexity Tracking

No constitution violations identified. Retaining two source pools and an optional module-owned store is required by PBX ownership, not a new general persistence platform.
