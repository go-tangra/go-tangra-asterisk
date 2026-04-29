# go-tangra-asterisk

Read-only CDR viewer and PJSIP registration tracker for FreePBX. Queries the
existing `asteriskcdrdb` and `asterisk` MySQL databases directly, augmented
with an optional AMI listener that captures registration events into a
module-owned table.

## Features

- **Call History** — Filterable list of calls (linkedid-grouped) with
  per-leg drilldown and CEL timeline.
- **Per-Extension Stats** — Total / inbound / outbound / answered / missed,
  workload share, average pickup and talk time, busiest hour, calls-per-day
  histogram, hour-of-day distribution.
- **Per-Extension Drawer** — Outbound / Inbound / Internal call lists, the
  stats summary, and a Registration tab.
- **Ringgroup Stats** — Inbound traffic to a ringgroup split into Answered
  / Nobody-answered / All-operators-busy / Failed, with the missed-calls
  list (clickable through to the call detail drawer).
- **Registration Log** *(optional)* — When the AMI listener is enabled,
  every PJSIP `ContactStatus` event is persisted with timestamp, contact
  URI, user-agent, Via address, and registration expiry, so you can
  answer "was extension X registered at time T?".
- **Multi-Tenant** — Standard platform auth context (tenant_id injected
  via gRPC metadata).

## gRPC Services

| Service | Endpoints | Purpose |
|---|---|---|
| `AsteriskCdrService` | `ListCalls`, `GetCall` | CDR list and per-call drilldown |
| `AsteriskStatsService` | `Overview`, `ListExtensionStats`, `GetExtensionStats`, `RingGroupStats` | Aggregated metrics |
| `AsteriskRegistrationService` | `GetRegistrationStatus`, `ListRegistrationEvents` | PJSIP registration history (requires AMI) |

**Port:** 9800 (gRPC, mTLS) — exposed via the admin gateway as
`/admin/v1/modules/asterisk/v1/...`. No public HTTP server: the embedded
frontend is served by the admin-service.

## Architecture

```
cmd/server/
├── main.go                  # Bootstrap, registration lifecycle, AMI listener start
├── wire.go / wire_gen.go    # Wire DI
└── assets/                  # Embedded registration assets
internal/
├── server/
│   └── grpc.go              # gRPC :9800, mTLS + middleware stack
├── service/
│   ├── cdr_service.go       # ListCalls, GetCall
│   ├── stats_service.go     # Overview, ext stats, ringgroup stats
│   └── registration_service.go  # PJSIP registration history
├── data/
│   ├── config.go            # Loads MySQL DSNs + AMI config
│   ├── mysql.go             # *sql.DB pools (cdr + config + tangra), auto-migration
│   ├── extension.go         # Channel → extension parsing
│   ├── cdr_repo.go          # ListCalls, GetCall
│   ├── stats_repo.go        # Overview + per-ext + ringgroup aggregates
│   └── pjsip_reg_repo.go    # Registration event log (Insert / GetStatusAt / ListEvents)
├── ami/
│   ├── protocol.go          # AMI wire protocol (line-based key:value frames)
│   └── listener.go          # Connect / login / subscribe / dispatch / reconcile
└── cert/                    # mTLS cert manager
protos/asterisk/service/v1/
├── cdr.proto                # AsteriskCdrService
├── stats.proto              # AsteriskStatsService (incl. ringgroup)
├── registration.proto       # AsteriskRegistrationService
└── asterisk_error.proto
```

## Data Sources

| DSN | DB | Purpose | Owner |
|---|---|---|---|
| `cdr_dsn` | `asteriskcdrdb` | CDR + CEL + queuelog | FreePBX (read-only) |
| `config_dsn` | `asterisk` | `users` for display names | FreePBX (read-only, best-effort) |
| `tangra_dsn` | `tangra_asterisk` | `pjsip_registration_events` | This module (auto-migrated) |

The tangra DB is optional. When `tangra_dsn` is blank the registration
endpoints return `AMI_DISABLED` (HTTP 503) — everything else continues to
work normally.

## Pickup Time

Defined as the elapsed seconds from `CHAN_START` to `ANSWER` on the
answered leg, looked up in `cel`. We don't use `cdr.duration - cdr.billsec`
because ring-all dials produce 5+ legs that all start simultaneously and
most end unanswered — the difference does not represent caller-perceived
latency.

## Extension Parsing

Channels are formatted as `PJSIP/<ext>-<hash>` (or `Local/<ext>@...` for
ringgroup fan-out). The repo strips the technology prefix and trailing
dash-suffix to recover the extension number. Display names come from the
optional join to `asterisk.users`.

## Direction Inference

Direction is inferred from the first CDR leg's channel pair:
- **inbound**: trunk → extension (channel not extension-shaped, dstchannel is)
- **outbound**: extension → trunk
- **internal**: extension → extension

The `ListCalls` RPC accepts a `direction` filter that matches this logic
via SQL regex on the first leg.

## AMI Registration Capture

When `ASTERISK_AMI_HOST` is set, the module connects to Asterisk's Manager
Interface and subscribes to `ContactStatus` events. Each event lands in
`tangra_asterisk.pjsip_registration_events`:

| Status | Meaning |
|---|---|
| `Created` | First registration after expiry / fresh device |
| `Updated` | Refresh re-registration (requires `send_contact_status_on_update_registration=yes`) |
| `Reachable` | Qualify probe succeeded |
| `Unreachable` | Qualify probe failed — device went silent without unregistering |
| `Removed` | Clean unregister or expiry processed by Asterisk |

On reconnect, the listener issues `PJSIPShowContacts` to seed the current
state — closing the gap that opens whenever the listener is offline.

### Required Asterisk-side configuration

`/etc/asterisk/manager.conf`:

```ini
[tangra]
secret = <strong-secret>
deny = 0.0.0.0/0
permit = <tangra-asterisk-network>
read = system
write =
```

`/etc/asterisk/pjsip.conf`:

```ini
[global]
type=global
send_contact_status_on_update_registration = yes
```

Reload: `asterisk -rx "manager reload" -rx "pjsip reload"`.

## Configuration

```yaml
data:
  asterisk:
    cdr_dsn:    "root:***@tcp(percona:3306)/asteriskcdrdb?parseTime=true&loc=UTC"
    config_dsn: "root:***@tcp(percona:3306)/asterisk?parseTime=true&loc=UTC"
    tangra_dsn: "root:***@tcp(percona:3306)/tangra_asterisk?parseTime=true&loc=UTC"
    max_open_conns: 25
    max_idle_conns: 5
    conn_max_lifetime: 30m
    query_timeout: 30s
    ami:
      host: ""             # blank disables the listener
      port: 5038
      username: tangra
      secret: ""
      reconnect_delay: 5s
```

Environment overrides:

| Var | Effect |
|---|---|
| `ASTERISK_CDR_DSN` | Overrides `data.asterisk.cdr_dsn` |
| `ASTERISK_CONFIG_DSN` | Overrides `data.asterisk.config_dsn` |
| `ASTERISK_TANGRA_DSN` | Overrides `data.asterisk.tangra_dsn` (blank → AMI feature off) |
| `ASTERISK_AMI_HOST` | Overrides `data.asterisk.ami.host` (blank → listener off) |
| `ASTERISK_AMI_PORT` | Overrides `data.asterisk.ami.port` |
| `ASTERISK_AMI_USERNAME` | Overrides `data.asterisk.ami.username` |
| `ASTERISK_AMI_SECRET` | Overrides `data.asterisk.ami.secret` |
| `ASTERISK_CA_CERT_PATH` | mTLS CA path |
| `ASTERISK_SERVER_CERT_PATH` | mTLS server cert |
| `ASTERISK_SERVER_KEY_PATH` | mTLS server key |
| `ADMIN_GRPC_ENDPOINT` | Admin gateway for module registration |
| `GRPC_ADVERTISE_ADDR` | Address admin gateway will dial back |

## Build

```bash
make build-server       # Build binary to ./bin/asterisk-server
make proto              # Regenerate proto stubs + descriptor.bin
make wire               # Regenerate Wire DI graph
make generate           # proto + wire + go mod tidy
make docker             # Build Docker image
make test               # go test -race ./...
```

Frontend is built separately and embedded via `//go:embed`:

```bash
cd frontend && pnpm install && pnpm build
# vite writes to ../cmd/server/assets/frontend-dist/
```

## Docker

```bash
docker run -p 9800:9800 -p 9801:9801 \
  -e ASTERISK_CDR_DSN=... \
  -e ASTERISK_CONFIG_DSN=... \
  -e ASTERISK_TANGRA_DSN=... \
  -e ASTERISK_AMI_HOST=freepbx \
  -e ASTERISK_AMI_SECRET=secret \
  ghcr.io/go-tangra/go-tangra-asterisk:latest
```

Runs as non-root user `asterisk` (UID 1000). Multi-stage build: Node 20
builds the frontend, Go 1.25 builds the server with the frontend embedded.

## Dependencies

- **Framework**: Kratos v2
- **DB driver**: `github.com/go-sql-driver/mysql`
- **DI**: Google Wire
- **Protobuf**: Buf
- **Frontend**: Vue 3 + Ant Design Vue 4 + VxeTable, federated via Module
  Federation

## Notes

- The module does not run any migrations against FreePBX databases.
  Auto-migration applies only to its own `tangra_asterisk.pjsip_registration_events`
  table, which is created on startup if missing.
- AMI is best-effort: events are not delivered while the listener is
  disconnected. The `PJSIPShowContacts` reconcile on reconnect bounds the
  state drift but does not give you a perfect history during outages.
- The bucket timezone for per-day / hour-of-day series is hardcoded to
  `Europe/Sofia` (`internal/data/stats_repo.go:21`). Change `displayTZ`
  for other deployments.
