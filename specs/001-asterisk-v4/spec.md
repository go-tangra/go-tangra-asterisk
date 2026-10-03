# Feature Specification: Asterisk module for Tangra V4

**Feature Branch**: No Git branch created; active feature `001-asterisk-v4`.

**Created**: 2026-10-02

**Status**: Implementation present; deployment acceptance pending (see docs/validation.md)

**Input**: User description: "use speckit to create a specification and tasks for a project like /home/jadmin/projects/go-tangra/go-tangra-asterisk but to be compatible with V4 of the tangra framework"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse and investigate call history (Priority: P1)

An authorized operator opens Asterisk in the Tangra V4 portal, filters logical calls, and investigates individual call legs and event timelines.

**Why this priority**: This is the core read-only troubleshooting workflow and the smallest useful replacement.

**Independent Test**: With a sample PBX history and AMI disabled, use the portal to filter calls and open a detail drawer; verify another tenant cannot retrieve the same records.

**Acceptance Scenarios**:

1. **Given** multiple legs sharing a call identifier, **When** an operator lists calls in a selected period, **Then** one logical row is shown per call with correct direction, outcome, duration, answering and originating extensions.
2. **Given** a selected call, **When** its details are opened, **Then** all legs and chronological events are shown, with missing optional diagnostics explicitly unavailable.
3. **Given** an unauthorized user or a tenant without the configured PBX binding, **When** they request history or detail, **Then** no PBX data is disclosed.

### User Story 2 - Analyze extension and ringgroup performance (Priority: P1)

An authorized supervisor reviews aggregate call outcomes, workload and extension/ringgroup drilldowns for a period.

**Why this priority**: Reports explain missed calls and staffing needs.

**Independent Test**: Use known multi-leg call fixtures to verify overview, extension shares, histograms and ringgroup outcome totals with AMI disabled.

**Acceptance Scenarios**:

1. **Given** answered and missed calls, **When** statistics are requested, **Then** totals count logical calls, pickup uses observed answer timing, and internal traffic is excluded from external workload shares.
2. **Given** ringgroup calls with answered, no-answer, busy and failed outcomes, **When** the ringgroup is inspected, **Then** all four categories and recent missed calls are available.
3. **Given** a period crossing a daylight-saving change, **When** time charts are viewed, **Then** buckets follow the configured display timezone consistently.

### User Story 3 - Inspect device registration history (Priority: P2)

An operator checks whether an extension was registered now or at a past call time and reviews the observed registration events.

**Why this priority**: Explains unreachable phones and misleading call outcomes.

**Independent Test**: Replay contact events, advance time past expiry, disconnect/reconnect the listener, and verify current and historical states.

**Acceptance Scenarios**:

1. **Given** captured contact events, **When** an extension or an instant is selected, **Then** status, last event, expiry and the registered-extension snapshot are consistent with the observed history.
2. **Given** AMI reconnects, **When** contacts are reconciled, **Then** current state recovers and the unobserved historical interval is identified as uncertain.
3. **Given** registration capture is disabled, **When** registration is requested, **Then** a feature-unavailable result is shown while call history and statistics remain usable.

### User Story 4 - Monitor live calls (Priority: P2)

An operator sees active calls and receives changes without refreshing the page.

**Why this priority**: Supports real-time troubleshooting independently of historical reports.

**Independent Test**: Replay channel and bridge events, reconnect a browser, and verify a fresh snapshot and removal of ended calls.

**Acceptance Scenarios**:

1. **Given** AMI is connected, **When** a live view opens, **Then** the current snapshot and subsequent call changes are displayed.
2. **Given** the listener disconnects or a browser consumes slowly, **When** state becomes unreliable, **Then** stale state is indicated and a new snapshot restores consistency with bounded buffering.
3. **Given** another tenant or missing permission, **When** a live snapshot or stream is requested, **Then** neither channel nor caller data is disclosed.

### User Story 5 - Play recordings and inspect call quality (Priority: P2)

An authorized operator plays a call recording and inspects directional media quality when available.

**Why this priority**: Preserves diagnostic features present in the source implementation.

**Independent Test**: Seek within a fixture recording; inspect quality on both sides of a call; attempt escaped file paths and unauthorized access.

**Acceptance Scenarios**:

1. **Given** an accessible recording and recording permission, **When** playback is started or seeking is used, **Then** the correct recording is streamed and seeking works.
2. **Given** a missing file, disabled recordings, or absent quality columns, **When** the call is opened, **Then** history remains usable and unavailable diagnostics are identified.
3. **Given** a malicious recording path or a symlink outside the configured root, **When** access is attempted, **Then** the file is not served.

### User Story 6 - View PBX monitoring dashboards (Priority: P3)

An authorized operator opens PBX health and media-quality dashboards and changes their period or extension selection.

**Why this priority**: Completes operational parity without blocking the history MVP.

**Independent Test**: Provide controlled monitoring responses and inspect dashboard series, missing values, directory names and optional-service failure states.

**Acceptance Scenarios**:

1. **Given** configured monitoring, **When** a dashboard is viewed, **Then** current and historical series display the selected PBX's metrics with missing values represented correctly.
2. **Given** monitoring is absent or unavailable, **When** the dashboard opens, **Then** its degraded state is clear and history stays available.
3. **Given** a query attempting to access another tenant's metrics, **When** it is submitted, **Then** no other tenant's data is returned.

### Edge Cases

- Reject reversed/empty date ranges, unsupported filters, oversized pages and invalid query steps; use deterministic pagination for tied timestamps.
- Empty periods, missing display names, unknown channel formats and missing optional event/quality columns must not fabricate data or crash other features.
- Expired contacts, multiple contacts for one endpoint, duplicate events and AMI outage gaps must retain truthful observed-state semantics.
- Recording authorization applies to partial requests as well as full downloads; raw paths are never accepted from clients.
- Monitoring failure, missing optional stores and registration retries must not prevent read-only history startup when its required dependencies are healthy.
- Reusing a call or extension identifier in another PBX must never cross the configured ownership boundary.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The module MUST appear in Tangra V4 navigation and use the platform session, permission and tenant context; protected direct access MUST fail without trusted identity.
- **FR-002**: Every data request MUST resolve the authenticated tenant to an explicitly configured PBX ownership binding; unknown or mismatched tenants MUST fail closed, including recordings, streams and monitoring.
- **FR-003**: Users MUST list logical calls with inclusive start/exclusive end, caller, destination, extension, direction and disposition filters and bounded pagination (maximum 200 records per page).
- **FR-004**: Call detail MUST show all legs and chronological events; any answered leg takes precedence in the logical call outcome, with documented deterministic ties.
- **FR-005**: The module MUST show overview, per-extension and ringgroup statistics, daily/hourly distributions and extension display names when available, preserving the source's logical-call and pickup-time semantics.
- **FR-006**: Time-series buckets MUST use a configured display timezone, defaulting to Europe/Sofia; stored instants MUST remain unambiguous.
- **FR-007**: Optional registration capture MUST preserve observed contact changes, expose current/historical status, event history and registered-at snapshots, and reconcile on reconnect without claiming to recover missing history.
- **FR-008**: Optional live monitoring MUST supply an initial snapshot, subsequent updates, disconnect/stale indication and bounded client buffering with reconnection recovery.
- **FR-009**: Recording access MUST require a separate recording permission and a tenant-authorized call lookup; playback MUST support seeking and constrain access to the configured recording root.
- **FR-010**: Call quality MUST expose available receive/transmit jitter, loss, latency, quality scores and bands for local and peer legs; absent or malformed quality MUST not break call detail.
- **FR-011**: Optional monitoring MUST support instant and range series, selected periods and extension directory names, bounded execution and explicit unavailable results; tenant isolation MUST hold for every accepted query.
- **FR-012**: The module MUST preserve external PBX data and configuration: no writes or migrations to PBX-owned databases, no dialing/hangup/control operations, and no automatic dialplan changes.
- **FR-013**: Optional capabilities MUST advertise availability and degrade independently. Required history-source failure MUST make readiness fail and return a bounded service-unavailable result.
- **FR-014**: Operators MUST configure, start and stop the module through the V4 deployment lifecycle, with secret-safe logs, dependency health and graceful worker/stream shutdown.
- **FR-015**: Release validation MUST include automated acceptance coverage for tenant isolation, permission denial, call grouping/statistics, registration outages, streaming recovery and recording confinement, plus portal smoke validation.

### Key Entities

- **PBX Binding**: Trusted tenant-to-PBX ownership, source connections, timezone and optional capability settings.
- **Logical Call / Call Leg / Call Event**: Grouped call, individual channel record and timeline observation scoped to the PBX.
- **Extension / Ringgroup**: Device or call distribution destination with names and aggregated outcomes.
- **Registration Event / Status**: Observed contact change and derived state at an instant, including expiry and uncertainty.
- **Live Call / Channel**: Active channel/bridge snapshot and freshness status.
- **Recording / Quality Observation**: Authorized call-associated media and optional directional measurements.
- **Metric Series**: Tenant-scoped monitoring labels, times and values with explicit missing samples.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All six workflows pass their acceptance scenarios through the V4 portal; the history workflow also passes with all optional features disabled.
- **SC-002**: Known fixture calls and reports match expected logical-call counts, outcomes and pickup values in 100% of checked cases.
- **SC-003**: All negative tenant and permission cases disclose zero records, file bytes, live events or monitoring samples.
- **SC-004**: In the documented reference environment with 100,000 history legs, 50 active calls and 10 concurrent viewers, 95% of first-page history/report interactions complete within 2 seconds and live changes appear within 2 seconds of receipt.
- **SC-005**: Following reconnection, a live viewer sees a consistent fresh snapshot within 10 seconds; historical observation gaps remain visible.
- **SC-006**: Every missing optional dependency has a verified unavailable/degraded state while history remains functional, and PBX write attempts remain zero throughout acceptance validation.

## Assumptions

- The source project's implemented behavior, rather than its older README alone, defines feature parity. Transport URLs, packaging and legacy browser dependencies may change to match V4.
- Initial deployment binds one PBX exclusively to one configured tenant. Multiple tenants sharing unpartitioned PBX tables are unsupported and rejected; multi-PBX provisioning UI is outside scope.
- PBX sources remain existing FreePBX/Asterisk installations; operators supply read-only history/config credentials and optionally AMI observation credentials, a module-owned registration store, recording mount and monitoring endpoint.
- Preserve the module-owned MySQL registration store and allow existing history to be adopted after ownership validation; do not require a new database engine solely for migration.
- Monitoring is bound to the same tenant/PBX, using an isolated upstream or a proven isolation adapter; unrestricted queries against a shared upstream are prohibited.
- Saved dashboard CRUD, PBX provisioning/control, importing PBX history and legacy URL compatibility are outside scope. Existing dashboard views and filters are included.
- Performance thresholds are proposed acceptance targets, not measurements of the legacy project; the reference fixture and hardware must be recorded during validation.
