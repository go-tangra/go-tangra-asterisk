# Browser HTTP Contract

All paths below are relative to `/api/asterisk` and require verified tenant binding plus the permission shown. JSON uses camelCase for domain fields; list envelopes follow V4 `{items,total,page,page_size,sort,order}`. Page defaults to 1, page size 25/max 200; deterministic allowlisted sorting. Timestamps are RFC3339 UTC, intervals `[from,to)`. Legacy proto field meanings are retained in typed OpenAPI schemas, not legacy gRPC/URL compatibility.

| Method/path | Permission | Input and output |
| --- | --- | --- |
| GET /capabilities | calls:read | Availability/freshness without secrets |
| GET /calls | calls:read | from,to,src,dst,extension,direction,disposition,page,page_size,sort,order; paged logical calls |
| GET /calls/{linkedid} | calls:read | summary, legs, timeline; optional quality and call-time registration |
| GET /stats/overview | stats:read | from,to,bucket(hour/day/week); totals, means, series |
| GET /stats/extensions | stats:read | from,to,extension,pagination/sort; paged extension stats |
| GET /stats/extensions/{extension} | stats:read | from,to,bucket; summary, series, hourOfDay |
| GET /stats/ringgroups/{ring_group} | stats:read | from,to; outcome counts and bounded missedCalls |
| GET /directory/extensions | stats:read | directory items with optional names |
| GET /registration/status/{extension} | registration:read | optional at (now); status, registered, certainty, lastEvent |
| GET /registration/events | registration:read | from,to,extension,pagination; observed events |
| GET /registration/online | registration:read | optional at; endpoints plus uncertainty/gap metadata |
| GET /live/calls | live:read | current calls and freshness |
| GET /live/calls/stream | live:read | text/event-stream; snapshot, upsert, remove, status; periodic heartbeat comments |
| GET /recordings/{linkedid} | recordings:read + calls:read | CDR-selected media; full 200, valid range 206, unsatisfiable range 416 |
| GET /dashboard/query | dashboard:read | query, optional time; normalized instant samples with hasValue |
| GET /dashboard/query_range | dashboard:read | query,start,end,stepSeconds>0; bounded series with missing markers |

Error envelope `{code,message,requestId}`; 400 invalid input, 401 missing/invalid session, 403 denied tenant/permission, 404 missing record/media, 503 required dependency unavailable or FEATURE_UNAVAILABLE for disabled optional capability. No secrets, raw SQL or file roots in responses. Monitoring upstream failures return bounded dependency errors; validate query length, time window and sample limits server-side.

SSE sends snapshot first; update payloads include call identifier and generation; stale status is emitted on AMI loss. On slow subscriber overflow, terminate the stream and reconnect for a snapshot. Do not promise durable event replay. Streams enforce configured maximum lifetime and cancellation, flush frames through portal, and release resources on disconnect. Reauthorization occurs on every reconnect and stream lifetime bounds session freshness.

Instant/range queries may pass PromQL only to the configured dedicated upstream. Shared upstream support requires a separately validated isolation implementation; query string label filters from the user are not an isolation boundary.
