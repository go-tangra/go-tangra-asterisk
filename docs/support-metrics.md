# Support-team metrics: definitions (phase 0)

What the refactored module measures, and how each number is derived from
Asterisk data. Written from a survey of the production FreePBX host
(`tools/phase0/survey.sh`) and a full weekday of anonymised calls. Phase 1
implements these rules; the golden fixtures in
`internal/support/testdata/golden/` pin them.

## The PBX this is written for

- Asterisk 22, FreePBX 17, MariaDB. CDR and CEL go to `asteriskcdrdb` over
  ODBC; there is no purge job, so both hold the full history (since 2025-10).
- **One support group: ring group 600**, strategy `ringall`, ring time 300 s,
  busy members skipped (`cwignore`), no confirmation, failover **hang up**.
- **Support team** = the members of ring group 600 that are extensions,
  excluding 201–211 (not support staff). Overridable in module settings.
- **Inbound path:** trunk `ITD` → announcement 1 (welcome, ~18 s) → ring
  group 600. A few DIDs go straight to an extension (`from-did-direct`);
  others go to announcement 2 (voicemail), fax, or a misc
  destination. No time conditions: out-of-hours calls reach group 600 and
  find nobody.
- **Outbound:** agents dial directly; one route (`Default`) over trunk `ITD`.
  Trunks `Kamailio` and `elevenlabs` exist; `elevenlabs` calls are never
  attributed to a person.
- **Follow-me** on every extension rings only the extension itself for 20 s,
  except 19 (a mobile, 3 s) and 44 (extension plus mobile).
- **Volume:** about 4,000 calls / 22,000 CDR rows a month. An inbound group
  call produces one CDR row per member phone that rang (8–13 typically).
- **CEL** records every event type. **CDR** is in simple mode and does not
  log unanswered calls that never reach a Dial.
- **Recordings:** ~94 % of CDR rows reference a file; files on disk start in
  2026-03, so older calls have none.
- **Quality:** practically every answered leg has `rtpqos`/`peerrtpqos`
  (plus `rxjitter`, `rxploss`, `rtt` columns). No dialplan change needed.

## Why the current numbers mislead

- Every member phone that rang for a group call counts as a "missed" call
  for that member, although a colleague answered. On a sample weekday each
  member "missed" 80–107 calls that were in fact served.
- CDR `billsec` on a group leg includes ringing, because the trunk channel
  was already answered by the announcement. Talk time must come from CEL
  bridge enter/exit; `billsec` overstates it by ~5 %.
- Pickup time from the caller's `CHAN_START` includes the ~18 s
  announcement. Agent answer time must start at the agent leg's ring.

## Call classification

Calls are grouped by `linkedid`. The first channel decides the family.

| Family | Rule | Kinds |
|---|---|---|
| Inbound | first channel is a trunk (`PJSIP/ITD-…`) | **group** (a `ext-group` Dial, or the `rg-<n>` MixMonitor, appears), **direct** (Dial to an extension from `from-did-direct`), **other** (fax, voicemail-only, misc destination) |
| Outbound | first channel is an extension and a trunk channel joins | per trunk; `elevenlabs` kept apart |
| Internal | extension to extension only | not part of support metrics |
| Ignored | an extension went off-hook and never dialled (no second channel) | dropped |

### Inbound group call outcome

| Outcome | Rule |
|---|---|
| **answered** | an agent leg has `ANSWER` |
| **abandoned before group** | caller hung up during the announcement; the group never started |
| **lost: no agent available** | the group started (recording `rg-600`) but no agent leg was created — nobody online, or all busy (skipped) — and the call was hung up by failover |
| **lost: caller hung up while ringing** | agent legs rang, none answered, the caller's channel hung up first |
| **lost: rang out** | agent legs rang for the full group time (300 s) and failover hung up |

### Agent ring-leg outcome (per member, per group call)

Read from the agent leg's CEL `HANGUP` `extra` (`hangupcause`, `hangupsource`).

| Outcome | Rule | Counts against the agent? |
|---|---|---|
| **answered** | leg has `ANSWER` | — (good) |
| **declined** | cause 17 and the hangup source is the agent's own channel | yes |
| **colleague answered** | cause 19 / no source, while another leg answered | no |
| **caller hung up** | cause 16 / no source, the call was lost | shared: reported, not a personal miss |
| **rang out** | still ringing at group timeout | shared, as above |
| **busy, skipped** | member was on a call, no leg created | no (but visible as availability) |

## Metrics

All per agent and period (PBX timezone), and for the team.

**Inbound (group 600)**
- offered = ring legs created for the agent; answered; declined; colleague
  answered; rang while the call was lost.
- answer share = answered / team answered (ring group calls only).
- **share** of the team's work = ½ × handled calls / team's handled calls +
  ½ × talk / team's talk, where handled calls are answered group and direct
  calls plus connected outbound calls, talk covers all three, and the team is
  the support team's members (their shares add up to 100 %). Counting both
  keeps one long call and many short ones from deciding it alone.
- **time to answer** = agent leg `CHAN_START` → `ANSWER` (median, p90).
- talk time = CEL `BRIDGE_ENTER` → `BRIDGE_EXIT` on the agent leg; average
  handle time = talk / answered.

**Inbound (direct DIDs)** — offered, answered, talk; reported separately.

**Outbound**
- attempts; connected (trunk leg `ANSWER`); not connected by dial status
  (`CANCEL` = agent gave up, `BUSY`, `NOANSWER`, `CONGESTION`/`CHANUNAVAIL`).
- setup time (agent `CHAN_START` → trunk `ANSWER`), talk time from the bridge.

**Team / caller side**
- calls, answered, lost by reason, abandoned in announcement.
- **caller wait** = group start → answer (excludes the announcement), and
  total = call start → answer.
- **callback**: a lost call counts as called back when an outbound call to the
  same number connects within 2 h; credited to the agent who made it.

**Presence** (from the registration timeline, extended in phase 3)
- online time, time on calls, **occupancy** = talk / online.

**Quality** — FreePBX logs the RTP report of the channel that owns the CDR
row, on the caller channel's last row (`peerrtpqos` repeats the same stream):
an **inbound** call is measured on the provider line (trunk ↔ PBX), an
**outbound** call on the agent's handset (PBX ↔ phone). Each call keeps its
report and the side measured; the worst direction gives loss %, jitter, RTT
and MOS. "Poor" = loss > 3 %, jitter > 30 ms, RTT > 300 ms or MOS below 3.6.
The handset side of inbound calls is not in CDR (AMI RTCP events would add
it).

## Open points carried into phase 1

- Direct-DID calls: none on the sampled weekday; rules follow the same CEL
  signals and get a fixture when one is captured.
- Follow-me to mobiles (19, 44): an answer on the mobile appears as a trunk
  leg answering; attribute it to the extension that owns the follow-me.
- Transfers (blind/attended): none in the sampled week; talk is split per
  bridge, so a transferred call credits both agents.
- What prunes recordings before 2026-03 (not cron, not a DB event).

## Golden fixtures

`internal/support/testdata/golden/<case>/` holds `cdr.tsv` and `cel.tsv` exactly
as the survey exports them (one call each, ids replaced, external numbers
replaced by `n0000000x`, addresses removed, date moved to 2026-01-08) and
`expected.json` with the outcome these rules give.

| Case | What it pins |
|---|---|
| `group-answered` | answer by 49 after 23 s of ringing, 18 s announcement, 72 s talk; one decline (41); eleven "colleague answered" legs |
| `group-caller-hung-up` | lost after 76 s ringing; three declines; three legs that rang until the caller left |
| `group-no-agent` | group reached, no agent leg, failover hang-up |
| `announcement-abandoned` | caller left during the welcome |
| `outbound-connected` | agent 45 via ITD, connected after 5 s, 26 s talk |
| `outbound-not-connected` | agent 48 gave up after 21 s (`CANCEL`) |
| `no-dial` | off-hook without dialling: ignored |
| `fax` | inbound fax: not agent work |
