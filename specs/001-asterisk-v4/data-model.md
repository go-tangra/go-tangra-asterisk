# Data Model: Asterisk V4

## PBXBinding

Trusted configuration: tenant ID, PBX ID, CDR DSN, optional config/registration DSNs, recording root, AMI settings, isolated monitoring URL, display timezone. Secrets are redacted. One binding per process; verified identity tenant must equal binding tenant before repository access. Source identifiers are unique only within the binding.

## LogicalCall, CallLeg and CallEvent

LogicalCall key `(pbx_id, linkedid)`; start, caller/name/number, destination, direction, disposition, duration/talk/pickup seconds, answered/originating extension, DID, leg count, recording availability. CallLeg key `(pbx_id, uniqueid, source row identity)`; channel pair, start, source/destination, last application/data, outcome, duration, billsec and optional local/peer QoS. CallEvent contains event time, type, channel, uniqueid, application, caller and routing context.

Read from PBX tables without migrations. Group by linkedid, order first/last legs with deterministic tie-breakers, prefer ANSWERED if any leg answered. Pickup is CHAN_START to ANSWER on the answered leg, null when not derivable. Unknown direction remains unknown. Dates use `[from,to)`; page >=1, default 25, maximum size 200; explicit sort allowlist.

## ExtensionStat and RinggroupStat

Extension string preserves leading zeros; optional display name; logical totals, inbound/outbound/answered/missed, pickup/talk means, total talk, workload share, busiest hour, selected bucket series and 24 hourly bins. External workload denominator excludes internal calls. Empty denominators produce zero, absent measurements remain absent. Ringgroup has total, answered/no-answer/all-busy/failed and bounded recent missed calls. Bucket boundaries use configured IANA timezone, with unambiguous UTC bucket timestamps.

## RegistrationEvent and DerivedStatus

Retain legacy table fields: id, event_time, endpoint, AOR, contact_uri, status, user_agent, via_address, reg_expire, rtt_usec. Table is owned by the module and exclusively bound to configured tenant/PBX; adoption checks binding before serving existing rows. Add versioned observation-gap storage in the module-owned database only.

States: unknown -> created/updated/reachable/unqualified; unreachable changes reachability; removed or expiry ends known registration. Track multiple contacts separately; endpoint is registered if at least one nonexpired contact qualifies under the documented evaluator. Historical state uses observations at or before `at`; no evidence and outage intervals are explicitly uncertain. Reconcile via PJSIPShowContacts seeds current observations and closes current-state gaps without backfilling history.

## LiveCall and LiveChannel

Keys `(pbx_id, linkedid)` and `(pbx_id, uniqueid)`; channel state, caller/connected identities, extension/context, bridge, created/updated times. Channel/bridge events update the registry; hangup removes channel and empty call. A generation/freshness flag distinguishes stale state during outage. Bounded subscribers receive fresh snapshot after reconnect or overflow.

## RecordingReference and RTPQoS

Recording reference comes from authorized CDR lookup, never arbitrary request path. Confined canonical/symlink-safe file access supports relative date directories and valid legacy paths inside the root. Missing files are unavailable. QoS includes local and peer receive/transmit jitter, counts/loss/percent, RTT, MES/MOS and quality band; malformed/absent fields do not become fictitious good quality.

## MetricSeries and CapabilityStatus

MetricSeries has label map, UTC times, values and missing-value markers. Instant/range shapes preserve legacy normalization, with validated positive step and bounded point count; only the binding's isolated upstream is used. CapabilityStatus indicates history readiness and availability/freshness of names, CEL, quality, AMI, registration, live calls, recordings and monitoring without disclosing configuration secrets.
