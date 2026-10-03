# Source inventory and V4 migration

Source: `/home/jadmin/projects/go-tangra/go-tangra-asterisk` (read only during implementation). V4 patterns: sibling DNS V4 runtime/manifest/UI, Tangra framework, auth verifier and portal gateway SDK. Published baselines are framework v4.3.1, auth/portal SDK v4.1.0 and UI kit v4.3.0; the module imports no legacy common SDK, Kratos v2 bootstrap or Wire.

| Source capability | Reusable behavior | V4 surface |
|---|---|---|
| `internal/data/cdr_repo.go`, `extension.go` | Channel extension parsing, logical leg grouping, directions, CEL pickup | `/api/asterisk/calls`, `/calls/{linkedid}` |
| `stats_repo.go` | Logical counts, external workload, overview/extensions/ringgroup drilldowns | `/api/asterisk/stats/*`, `/directory/extensions` |
| `pjsip_reg_repo.go`, `internal/ami/*` | Contact observations, expiry and AMI frames | `/api/asterisk/registration/*` |
| `internal/calls/*`, public `/calls/stream` | Channel/bridge registry and snapshot streaming | `/api/asterisk/live/calls`, `/live/calls/stream` |
| Public `/recordings/{linkedid}` | Recording lookup and byte ranges | `/api/asterisk/recordings/{linkedid}`, two permissions |
| `rtpqos.go` | Local/peer jitter, loss, RTT, MES/MOS and worst-direction bands | Call-detail leg enrichment |
| `prometheus.go`, `internal/exporter/*`, public `/metrics` | Normalized samples and PBX metrics | Protected `/api/asterisk/dashboard/*`; private admin `/metrics` |
| Legacy frontend views | Six operator workflows and filters | Vue 3 V4 kit federated remote |

The API prefix applies to all route suffixes in the last column. Public legacy aliases, proto descriptors, menu YAML registration and the old frontend are omitted. Authenticated tenant headers never choose a database. Verified auth sessions and revocations are independently checked by the service; OpenAPI drives gateway routes and handler validation.

## Deterministic history/report rules

Group complete call legs by linkedid. Logical start is the earliest leg; selected periods contain starts in `[from,to)`. Sort legs by start, sequence, uniqueid and channel; ties in list sorting use linkedid. Any ANSWERED leg wins, then BUSY, then FAILED, then NO ANSWER, as required by the V4 specification. This deliberately follows the spec's any-answer rule rather than the legacy repository's additional non-empty destination-channel restriction for announcement legs. The first ordered answered leg determines answering extension and pickup, with CHAN_START/ANSWER from that same uniqueid. Missing CEL pickup remains null. Duration and talk use logical maxima, never summed duplicated legs.

Per-extension counts include involvement once per logical call; answered talk belongs to the answering extension (or outbound originating extension). Workload shares use external answered talk only; internal/unknown traffic does not enter the denominator. Ringgroup membership matches any leg destination. Hour/day/week buckets use the configured IANA zone; repeated DST hours have different UTC timestamps. Report time labels and hour histograms use the PBX display zone returned by the report API. History queries have a 200,000-leg bound and a five-second default deadline; requests exceeding it must use a narrower period.

Quality parsing preserves missing individual measurements as null, rejects nonfinite/negative numbers, and exposes separate local/peer measurements. Registration evaluates each contact separately. Removed and expired contacts do not register an endpoint; an endpoint is registered if a qualifying nonexpired contact remains. Unreachable is observed reachability failure; no evidence is unknown, and observation gaps are uncertain. Reconnect removes contacts absent from reconciliation and leaves historical gaps intact.

## Ownership and adoption

Back up the legacy **module-owned** registration database. Point `registration_dsn` at it, verify its history belongs exclusively to the configured tenant/PBX, then set `adopt_registration: true` for the one-time bootstrap. The operator assertion is necessary because legacy rows have no tenant identifier. Bootstrap locks the migration, checks the legacy columns, creates versioned ownership/gap tables, and refuses another stored binding. Adoption never copies or rewrites legacy event rows. Runtime requires existing ownership/version; it does not run migrations. New installations bootstrap a fresh database without adoption enabled.

Source CDR/config DSNs must identify databases separate from registration storage. Provide SELECT-only credentials and never reuse module-write credentials for PBX sources. Do not add indexes or quality columns automatically. QoS capture/dialplan instrumentation, recording creation and PBX configuration are manual operator changes outside this migration.

## Configuration mapping and rollback

| Legacy setting | V4 setting/environment |
|---|---|
| `cdr_dsn` | `binding.cdr_dsn` / `ASTERISK_CDR_DSN` |
| `config_dsn` | `binding.config_dsn` / `ASTERISK_CONFIG_DSN` |
| `tangra_dsn` | `binding.registration_dsn` / `ASTERISK_REGISTRATION_DSN` |
| `ami.*` | `ami.*`, independent of registration DSN |
| `prometheus_url` | `binding.monitoring_url` / `ASTERISK_MONITORING_URL`, dedicated upstream required |
| recordings base | `binding.recording_root` / `ASTERISK_RECORDING_ROOT`, read-only mount |
| admin endpoint/common registration | SPIFFE discovery, V4 auth/portal SDKs and mesh policy |
| implicit Sofia conversion | `binding.source_timezone`, `binding.timezone` |

To roll back, stop V4 so gateway leases deregister and workers/streams close; restore the previous portal module deployment and its database backup if needed. PBX source databases remain untouched. Retain the backup and ownership/gap metadata for forward recovery; no automatic destructive down migration is supplied.
