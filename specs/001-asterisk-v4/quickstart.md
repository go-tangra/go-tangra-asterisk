# Quickstart Validation: Asterisk V4

The implementation and test harnesses are present. Local build, race, contract, security, AMI/SSE integration and UI checks have run; the environment-dependent acceptance gates remain open in docs/validation.md.

## Prerequisites

Go 1.26.8, Node/pnpm matching the V4 UI baseline, container runtime for integration fixtures, published V4 module and GitHub Packages access, a V4 auth/portal/mesh stack with trusted workload identity. Supply a fixture PBX MySQL with read-only CDR/CEL/config credentials, one exclusive tenant binding and optional module-owned registration MySQL, fake/real AMI, read-only recording root and dedicated monitoring upstream. Use test secrets; record fixture seed and host hardware.

## Commands

```bash
make generate
make test
make test-integration
make ui-check
make build
./bin/asterisksvc bootstrap -config configs/dev.yaml
./bin/asterisksvc -config configs/dev.yaml
make test-e2e
make benchmark
```

`bootstrap` migrates only the optional module-owned store and validates ownership/schema adoption. Runtime refuses invalid identity/binding/configuration. A missing optional store leaves registration unavailable while AMI live monitoring may still run. `make build` embeds the built federation remote; package download tokens use environment/BuildKit secrets.

## Acceptance scenarios

1. US1: Log in through V4 portal, open Asterisk, filter tied/multi-leg calls and inspect timeline. Disable every optional dependency and repeat. Verify no AMI, quality or missing names blocks history.
2. US2: Verify seeded logical totals, external workload shares, pickup values, ringgroup outcomes and timezone/DST buckets against fixture expectations.
3. US3: Replay all contact states/multiple contacts, expire registrations, interrupt AMI and reconnect; historical gaps remain uncertain while current contacts reconcile.
4. US4: Open live view, replay channel/bridge/hangup events, reconnect browser, simulate slow consumption and AMI loss. Confirm fresh snapshots, stale indication and bounded resources through the actual portal proxy.
5. US5: Play/seek recording, request invalid range, missing file, traversal and symlink escape. Verify local/peer quality and absent/malformed fields.
6. US6: Inspect instant/range monitoring and extension names, unavailable monitoring, oversized/invalid query and attempted other-tenant query.
7. For every data route, recording range and stream, repeat with no session, wrong tenant, revoked session and missing permission. Expect zero disclosed data. Reject duplicate tenant ownership of an unpartitioned PBX.
8. Check healthy registration leases/permission seeding after portal/auth restart, private scrape policy, secret redaction, SIGTERM shutdown and persistent registration history after restart. Verify source credentials cannot perform writes and no source migration runs.
9. Benchmark 100,000 history legs/50 active calls/10 viewers: SC-004 p95 <=2s and live receipt-to-display <=2s. Reconnect snapshot <=10s under SC-005. Record hardware/results in docs/validation.md; failure blocks release or requires an explicitly revised acceptance target.

See contracts/ for exact routes and platform guarantees and data-model.md for grouping/state rules.
