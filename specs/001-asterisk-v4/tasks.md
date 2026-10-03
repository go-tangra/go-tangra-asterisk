# Tasks: Asterisk module for Tangra V4

**Input**: Design documents in `specs/001-asterisk-v4/`

**Prerequisites**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), contracts/ and [quickstart.md](quickstart.md).

**Tests**: Explicitly required by FR-015. Write meaningful fixture/contract cases before the corresponding implementation; implementation-phase checks must be demonstrated, not claimed here.

**Organization**: Setup and foundation followed by one phase per user story. All paths below are planned repository-relative source paths. `[P]` denotes disjoint files that can be handled concurrently after prerequisites, not automatic agent delegation.

## Phase 1: Setup (Shared Infrastructure)

Establish the published V4 build and source baseline.

- [X] T001 Initialize github.com/go-tangra/go-tangra-asterisk/v4 with Go/toolchain and published framework/auth/portal pins from research D2 in go.mod; omit legacy common SDK and Wire
- [X] T002 [P] Record source-feature inventory, reusable logic and legacy-to-V4 route mapping in docs/migration.md
- [X] T003 Create V4 kit Vue/federation workspace and package-access configuration in ui/package.json, ui/module-federation.config.ts and ui/vite.config.ts
- [X] T004 Add planned generation/build/test/UI/integration/e2e/benchmark targets with reproducible dependency handling in Makefile

## Phase 2: Foundational (Blocking Prerequisites)

Block all data stories until trusted identity, ownership and shared contracts exist.

- [X] T005 [P] Implement validated config, secret redaction, exclusive tenant/PBX binding and capability defaults in internal/config/config.go and configs/dev.yaml
- [X] T006 [P] Define typed route/schema/error/list contracts including disabled capabilities in api/openapi/asterisk.yaml and api/openapi/embed.go
- [X] T007 Implement Freya secure runtime, workload identity/policy, admin health and graceful startup/shutdown in internal/app/app.go and cmd/asterisksvc/main.go
- [X] T008 Implement verified platform sessions/revocation, auth permission checks and tenant-binding guard for ordinary and streaming requests in internal/httpapi/middleware.go
- [X] T009 Implement bounded read-only PBX pools, optional schema probes and readiness checks in internal/pbx/pools.go and internal/pbx/binding.go
- [X] T010 Implement permission/role/grant/nav/CASL declarations and OpenAPI-derived manifest validation in pkg/asteriskmanifest/manifest.go
- [X] T011 Implement readiness-triggered leased gateway registration and independent retrying auth seeding in internal/app/registration.go
- [X] T012 Implement route dispatch, capability response and bounded error handling in internal/httpapi/router.go and internal/httpapi/capabilities.go
- [X] T013 Implement shared portal-aware typed API client and remote shell/nav/route exports in ui/src/api/client.ts, ui/src/routes.ts and ui/src/nav.ts
- [X] T014 Add foundational trust/binding/role/route-drift and failed-start cleanup tests in tests/contract/platform_test.go and tests/security/binding_test.go

## Phase 3: User Story 1 - Browse call history (Priority: P1) — MVP

Independent test: filter fixture calls and drill down through portal with optional features off; deny foreign tenant.

- [X] T015 [P] [US1] Write call-list/detail contract fixtures for ties, direction, grouping, pagination, optional fields and denial in tests/contract/calls_test.go
- [X] T016 [P] [US1] Port deterministic extension parsing and logical call grouping from legacy internal/data into internal/cdr/extension.go and internal/cdr/grouping.go
- [X] T017 [US1] Implement tenant-bound parameterized CDR/CEL list/detail repositories with bounded timeout and schema degradation in internal/cdr/repository.go
- [X] T018 [US1] Implement call list/detail handlers and capability-aware optional detail enrichers in internal/httpapi/calls.go
- [X] T019 [US1] Build V4 call table/filter and leg/timeline drawer in ui/src/views/calls/index.vue and ui/src/views/calls/call-drawer.vue
- [X] T020 [US1] Embed built UI remote with no-UI fallback and wire secure /ui/ serving in ui/embed.go, ui/embed_stub.go and internal/app/ui.go
- [ ] T021 [US1] Validate history MVP, portal remote and optional-off/error flows in ui/tests/e2e/history.spec.ts

## Phase 4: User Story 2 - Extension/ringgroup performance (Priority: P1)

Independent test: compare known fixture totals, workload, pickup and DST buckets with AMI disabled.

- [X] T022 [P] [US2] Write aggregation fixtures for multi-leg/internal/ringgroup/empty/DST cases in internal/stats/aggregation_test.go
- [X] T023 [P] [US2] Implement configurable timezone bucket boundaries and histogram rules in internal/stats/buckets.go
- [X] T024 [US2] Port overview, extension and ringgroup queries preserving logical-call and CEL pickup semantics in internal/stats/repository.go
- [X] T025 [US2] Implement stats and directory handlers with paging/sort validation in internal/httpapi/stats.go and internal/httpapi/directory.go
- [X] T026 [US2] Build overview, extension table/drawer and ringgroup drilldown in ui/src/views/overview/index.vue and ui/src/views/extensions/index.vue
- [ ] T027 [US2] Validate stats endpoints and portal reports against seeded expected values in tests/contract/stats_test.go and ui/tests/e2e/stats.spec.ts

## Phase 5: User Story 3 - Registration history (Priority: P2)

Independent test: replay contacts/expiry and outage recovery; verify historical uncertainty and disabled-feature response.

- [X] T028 [P] [US3] Write contact-state, multiple-contact, expiry and gap fixtures in internal/registration/state_test.go
- [X] T029 [P] [US3] Port AMI frame/login/event protocol with cancellable fake-server coverage in internal/ami/protocol.go and internal/ami/protocol_test.go
- [X] T030 [US3] Implement module-owned MySQL migrations, existing-table adoption/ownership checks, gap storage and bootstrap command in internal/registration/migrations/, internal/registration/repository.go and cmd/asterisksvc/bootstrap.go
- [X] T031 [US3] Implement contact state evaluator and historical/current/registered-at queries in internal/registration/state.go
- [X] T032 [US3] Implement optional shared AMI listener, reconnect/backoff, PJSIPShowContacts reconciliation and observation-gap tracking in internal/ami/listener.go
- [X] T033 [US3] Implement registration status/events/online handlers and extension registration tab in internal/httpapi/registration.go and ui/src/views/extensions/registration-tab.vue
- [X] T034 [US3] Connect call-time registration enrichment and test outage/restart/disabled-store degradation in internal/cdr/registration.go and tests/integration/registration_test.go

## Phase 6: User Story 4 - Live calls (Priority: P2)

Independent test: AMI call replay, browser/AMI reconnection and slow subscriber recovery through portal.

- [X] T035 [P] [US4] Write channel/bridge/hangup and stale-generation fixtures in internal/calls/registry_test.go
- [X] T036 [US4] Port active-call registry and bounded subscribers wired to shared AMI listener in internal/calls/registry.go
- [X] T037 [US4] Implement protected live snapshot and heartbeat SSE with bounded lifetime, overflow reconnect, freshness and cancellation in internal/httpapi/live.go
- [X] T038 [US4] Build live-call view with capability and stale status plus snapshot-based reconnection in ui/src/views/live/index.vue
- [ ] T039 [US4] Validate portal flush/timeouts, tenant denial, revoked-session reconnect and bounded slow consumers in tests/integration/live_test.go and ui/tests/e2e/live.spec.ts

## Phase 7: User Story 5 - Recordings and quality (Priority: P2)

Independent test: seek valid recording; deny escaped paths/foreign calls and handle absent or malformed QoS.

- [X] T040 [P] [US5] Write byte-range, traversal, symlink and permission test cases in internal/recordings/handler_test.go
- [X] T041 [P] [US5] Port local/peer RTP parser with malformed/missing-data and quality-band coverage in internal/cdr/rtpqos.go and internal/cdr/rtpqos_test.go
- [X] T042 [US5] Implement tenant-authorized call lookup and confined recording streaming with range handling in internal/recordings/handler.go and internal/httpapi/recordings.go
- [X] T043 [US5] Add optional QoS enrichment to call detail without requiring quality columns in internal/cdr/quality.go
- [X] T044 [US5] Build permission-aware playback/seek and directional quality panels in ui/src/views/calls/recording-player.vue and ui/src/views/calls/quality-panel.vue
- [ ] T045 [US5] Validate recording playback through portal and missing media/quality degradation in ui/tests/e2e/recordings.spec.ts

## Phase 8: User Story 6 - Monitoring dashboards (Priority: P3)

Independent test: fake monitoring responses, isolated queries, missing values and optional upstream failure.

- [X] T046 [P] [US6] Write instant/range normalization, bounds, upstream failure and isolation fixtures in internal/dashboard/client_test.go
- [X] T047 [P] [US6] Port PBX/quality metric collectors with bounded AMI reads and private scrape wiring in internal/exporter/collector.go and internal/app/metrics.go
- [X] T048 [US6] Implement dedicated tenant/PBX monitoring client, time/step/sample/query bounds and missing-value handling in internal/dashboard/client.go
- [X] T049 [US6] Implement protected instant/range dashboard handlers in internal/httpapi/dashboard.go
- [X] T050 [US6] Build V4 monitoring views and period/extension filters in ui/src/views/dashboards/index.vue
- [ ] T051 [US6] Validate monitoring directory/permissions/isolated-upstream/degraded states in tests/contract/dashboard_test.go and ui/tests/e2e/dashboard.spec.ts

## Phase 9: Polish & Cross-Cutting Concerns

Release only after all acceptance and deployment checks pass.

- [X] T052 [P] Provide non-root container and systemd deployment, SPIFFE/policy/portal registration, private scrape and read-only mounts in Dockerfile, deploy/compose.yaml, deploy/policy.yaml and deploy/tangra-asterisk.service
- [X] T053 [P] Document tenant binding, source read-only grants, legacy registration adoption, optional AMI setup, rollback and environment mapping in README.md and docs/migration.md; keep PBX/dialplan changes manual
- [X] T054 Add full MySQL/AMI/monitoring fixture integration suite and read-only/optional-failure/secret-redaction/shutdown checks in tests/integration/parity_test.go and tests/security/redaction_test.go
- [ ] T055 Measure SC-004/SC-005 reference fixture and optimize only proven bottlenecks in tests/integration/performance_test.go; record fixture/hardware/results in docs/validation.md
- [ ] T056 Run generation drift, Go race/integration, UI checks, six portal workflows and quickstart acceptance; record results and any release blockers in docs/validation.md

## Dependencies & Execution Order

Setup -> Foundation -> US1 -> US2 -> US3 -> US4 -> US5 -> US6 -> Polish is the recommended incremental order. Foundation blocks every story. US1 is the MVP including usable embedded portal UI; do not defer its registration/auth integration.

US2 reuses shared PBX binding/parsers; its reports can be validated independently of call-list UI, and links integrate with US1. US3 requires shared AMI protocol/listener and optional owned-store migration; call enrichment integrates after US1. US4 depends on the shared AMI listener from US3 but not registration storage; prove live-only operation with no registration DSN. US5 requires US1 call detail and authorized call lookup. US6 requires foundation and the shared AMI transport for exporter wiring, but dashboard queries can be tested against a fake dedicated upstream without AMI. Final acceptance requires all six stories.

Within a story, tests precede implementation, repositories precede handlers, handlers precede UI integration, and acceptance closes the phase. Do not concurrently edit shared router, manifest, OpenAPI or app wiring; merge those additions sequentially.

## Parallel Examples per Story

- US1: call contract fixtures and extension/grouping port after foundation; repository follows both.
- US2: aggregation fixture tests and timezone bucket implementation; report repository follows both.
- US3: state fixtures and AMI protocol/fake server; listener follows protocol and state/storage implementation.
- US4: registry fixtures may run alongside independent UI layout preparation; stream integration waits for registry and listener.
- US5: recording confinement/range fixtures and RTP parser in separate files; detail/UI integration follows both.
- US6: dashboard client fixtures and private exporter work after shared AMI exists; query handlers follow client.

## Requirement Coverage

| Requirements | Delivery phases |
| --- | --- |
| FR-001, FR-002 | Foundation; negative cases in every story; final security acceptance |
| FR-003, FR-004 | US1 |
| FR-005, FR-006 | US2 |
| FR-007 | US3 |
| FR-008 | US4 |
| FR-009, FR-010 | US5 |
| FR-011 | US6 |
| FR-012 | Foundation pools/binding; owned migrations US3; final read-only validation |
| FR-013 | Foundation capability/readiness; optional-off validation per story |
| FR-014 | Foundation lifecycle/registration; deployment and shutdown validation |
| FR-015 | Story test tasks and final full acceptance |

## Implementation Strategy

Deliver setup/foundation/US1 first and validate it with optional features disabled. Add US2 for core reporting, then optional registration/live/recording features and monitoring. Each checkpoint validates the story through the V4 portal while preserving previously delivered workflows. Source code reuse requires fixture-confirmed semantics; V4 auth/mesh/UI wiring is new. Do not mutate the source project.

## Task Summary

56 tasks total. Phase 1: Setup (Shared Infrastructure): 4, Phase 2: Foundational (Blocking Prerequisites): 10, US1: 7, US2: 6, US3: 7, US4: 5, US5: 6, US6: 6, Phase 9: Polish & Cross-Cutting Concerns: 5.

## Implementation checkpoint (2026-10-03)

49 implementation/test-harness tasks are complete. The seven unchecked tasks require real deployment acceptance: portal workflows (T021, T027, T039, T045, T051), measured reference performance (T055), and full release/quickstart acceptance (T056). These remain open; creating their test harnesses does not constitute running acceptance. See `docs/validation.md` for checks actually executed and environment blockers. No Git checkout or upstream source mutation was performed.
