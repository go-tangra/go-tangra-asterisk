# Implementation validation — 2026-10-03

Implementation source, embedded Vue remote, deployment definitions and acceptance harnesses are present. **Release acceptance is incomplete.** 49 of 56 tasks are marked complete; the seven environment-dependent acceptance tasks remain unchecked in `specs/001-asterisk-v4/tasks.md`.

## Checks executed

| Check | Result / evidence |
|---|---|
| Published Go dependency resolution | Framework v4.3.1, auth/portal SDK v4.1.0; `go mod tidy` succeeded, go.sum retained |
| `go test -race ./...` | Passed: grouping/pickup/direction, timezone/DST, multi-contact/expiry/gaps, fake AMI protocol/reconciliation/cancellation, collector normalization, recording ranges/traversal/symlinks, monitoring bounds/missing/foreign samples, every-route tenant/session/permission denial, startup cleanup, SSE snapshots/reconnect/revocation/shutdown |
| `go test -race -tags ui ./...` | Passed with built federation assets, including embedded manifest/remote entry |
| `go vet ./...` | Passed |
| `make build`, `make build-no-ui` | Both binary variants built; `-buildvcs=false` is explicit because this workspace has an empty restricted .git directory rather than a checkout |
| `make check-generated` | Generated OpenAPI TypeScript types match committed schema.d.ts |
| `make ui-check` | TypeScript and no-legacy checks passed; two Vitest cases passed |
| Production UI build | Passed using existing installed published V4 dependencies from the sibling DNS workspace; built manifest exposes routes/nav and production singleton imports come from the host |
| MySQL/performance test compilation | `go test -tags integration ./tests/integration -run '^$'` passed; **no database acceptance run** |
| Browser harness discovery | Playwright lists six workflow tests; **no portal acceptance run** |

No production deployment, PBX source schema changes, dialplan changes, or portal publication occurred. Framework insecure identity/policy overrides appear only in the failed-start fixture test; the service entry point does not offer those switches.

## Release blockers and required environment

1. **Fresh UI package installation:** `NODE_AUTH_TOKEN` is unset. Registry installation failed with missing authorization; local validation used an existing installed `@go-tangra/ui` 4.3.0 package and its published lockfile. Run `make ui-install` with GitHub Packages read access to verify the reproducible fresh install. No token was stored in the repository.
2. **MySQL integration:** Docker socket access is denied even outside the execution sandbox. No MySQL server or dedicated fixture DSNs were supplied. Tests compile but fixture SQL, read-only grants, ownership adoption and MySQL window queries have not been exercised here.
3. **Actual V4 portal:** No running mesh/auth/gateway deployment or authenticated browser storage state was supplied. Real gateway lease/permission-seeding retries, portal SSE flushing/timeouts, remote loading, browser recording seeking, optional-off portal workflows and the six browser scenarios remain unverified.
4. **Performance:** No SC-004/SC-005 measurements are claimed. Reference tests seed 100,000 legs and run 10 concurrent history/report viewers, plus 50 live registry calls. Browser receipt-to-display and reconnect timing must also be measured through the actual portal.

The execution host is Linux amd64 with Go 1.26.8 selected by the module, Node v22.14.0 and the installed V4 UI kit 4.3.0. CPU/storage/database host details and measured p95/reconnect/browser results must be recorded when the reference fixture is run, rather than inferred from unit-test timing.

## MySQL acceptance commands

The fixture setup writes only dedicated databases whose names are enforced by the test harness. Keep these credentials separate from any deployed PBX.

```sh
docker compose -f deploy/fixtures.yaml up -d --wait
export ASTERISK_FIXTURE_ADMIN_DSN='fixture_writer:fixture-writer@tcp(127.0.0.1:13306)/asterisk_fixture_cdr'
export ASTERISK_FIXTURE_CDR_DSN='fixture_reader:fixture-reader@tcp(127.0.0.1:13306)/asterisk_fixture_cdr'
export ASTERISK_FIXTURE_REGISTRATION_DSN='fixture_writer:fixture-writer@tcp(127.0.0.1:13306)/asterisk_fixture_registration'
make test-integration
export ASTERISK_BENCHMARK_OUTPUT=/tmp/asterisk-reference-results.json
make benchmark
```

`test-integration` deliberately fails for missing fixture credentials; it does not silently skip acceptance. The fixture uses MySQL 8.4, UTC source DATETIME values, 100,000 legs grouped into 50,000 calls with answered/busy ties, 50 active live calls and 10 concurrent viewers. Record MySQL buffer settings, CPU model/count, RAM, storage and network placement with the results. The test checks conservative combined list/report latency; the published criterion applies to each interaction. Portal display latency remains a separate browser gate.

## Portal acceptance commands

Deploy the module with one binding, SELECT-only source accounts, module-owned registration database, observer AMI credentials, read-only recording mount and a dedicated monitoring upstream. Seed known fixture history, an accessible recording and registration outage events within the selected UI period. Log in as an investigator and export browser storage state securely outside the repository.

```sh
export E2E_BASE='https://your-v4-portal.example'
export E2E_STORAGE_STATE='/secure/path/investigator-state.json'
export E2E_RECORDING_LINKEDID='seeded-recording-call'
make test-e2e
```

Install Playwright's browser on the acceptance host if absent. Use a trusted HTTPS certificate. The browser harness requires these settings and fails if missing. Repeat history with AMI, registration, recordings, directory names, CEL/quality and monitoring disabled. Repeat every data route with foreign tenant, denied permissions and revoked session; the local automated security suite covers server refusal, but actual gateway propagation still needs deployment verification. Exercise auth/gateway restarts, invalid legacy ownership, source failure readiness, SIGTERM with active streams, and rollback as described in the quickstart.

Before release, measure live changes through the portal at <=2 seconds and a fresh post-reconnect snapshot at <=10 seconds with 10 viewers, including slow-consumer overflow and AMI outage. Confirm historical gaps persist and source writes remain zero. Mark remaining tasks complete only after these recorded acceptance results pass.
