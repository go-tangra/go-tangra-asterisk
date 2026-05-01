<script lang="ts" setup>
import { computed, ref, watch } from 'vue';

import { useVbenDrawer } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import { Alert, Card, Descriptions, DescriptionsItem, Tag, Table, Tabs, TabPane, Tooltip, Spin, Empty } from 'ant-design-vue';

import { useAsteriskCdrStore } from '../../stores/asterisk-cdr.state';
import { useAsteriskRegistrationStore } from '../../stores/asterisk-registration.state';
import type {
  Call,
  CallLeg,
  CelEvent,
  Disposition,
  RegStatus,
  RegisteredEndpoint,
  RTPQoS,
} from '../../api/services';
import { formatDateTime } from '../../utils/datetime';

const cdrStore = useAsteriskCdrStore();
const regStore = useAsteriskRegistrationStore();

const summary = ref<Call | null>(null);
const legs = ref<CallLeg[]>([]);
const timeline = ref<CelEvent[]>([]);
const loading = ref(false);

// Snapshot of registered endpoints at the call start time. The "online"
// list is what the operator most cares about for missed-call drilldowns
// — who could have answered? Loads in parallel with the call detail; an
// AMI_DISABLED error is surfaced inline rather than failing the drawer.
const onlineAtCall = ref<RegisteredEndpoint[]>([]);
const onlineLoaded = ref(false);
const onlineError = ref<string>('');

const [Drawer, drawerApi] = useVbenDrawer({
  onOpenChange: async (isOpen: boolean) => {
    if (!isOpen) return;
    const data = drawerApi.getData<{ linkedid: string }>();
    if (!data?.linkedid) return;

    loading.value = true;
    onlineAtCall.value = [];
    onlineLoaded.value = false;
    onlineError.value = '';
    recordingError.value = '';
    try {
      const resp = await cdrStore.getCall(data.linkedid);
      summary.value = resp.summary;
      legs.value = resp.legs ?? [];
      timeline.value = resp.timeline ?? [];

      // Now that we know the call's start time, fetch the online snapshot.
      try {
        const online = await regStore.registeredAt(resp.summary.calldate);
        onlineAtCall.value = online.endpoints ?? [];
        onlineLoaded.value = true;
      } catch (err) {
        onlineError.value = err instanceof Error ? err.message : 'Failed to load online endpoints';
      }
    } finally {
      loading.value = false;
    }
  },
});

watch(
  () => drawerApi.isOpen,
  (open) => {
    if (!open) {
      summary.value = null;
      legs.value = [];
      timeline.value = [];
      onlineAtCall.value = [];
      onlineLoaded.value = false;
      onlineError.value = '';
    }
  },
);

function regStatusLabel(s: RegStatus): string {
  switch (s) {
    case 'REG_STATUS_CREATED':
      return 'Registered';
    case 'REG_STATUS_UPDATED':
      return 'Refreshed';
    case 'REG_STATUS_REACHABLE':
      return 'Reachable';
    case 'REG_STATUS_UNQUALIFIED':
      return 'Unqualified';
    default:
      return '—';
  }
}

function dispositionLabel(d: Disposition | string): string {
  switch (d) {
    case 'DISPOSITION_ANSWERED':
      return $t('asterisk.disposition.ANSWERED');
    case 'DISPOSITION_NO_ANSWER':
      return $t('asterisk.disposition.NO_ANSWER');
    case 'DISPOSITION_BUSY':
      return $t('asterisk.disposition.BUSY');
    case 'DISPOSITION_FAILED':
      return $t('asterisk.disposition.FAILED');
    default:
      return $t('asterisk.disposition.UNSPECIFIED');
  }
}

function dispositionColor(d: Disposition | string): string {
  switch (d) {
    case 'DISPOSITION_ANSWERED':
      return '#52C41A';
    case 'DISPOSITION_NO_ANSWER':
      return '#FA8C16';
    case 'DISPOSITION_BUSY':
      return '#1890FF';
    case 'DISPOSITION_FAILED':
      return '#FF4D4F';
    default:
      return '#999999';
  }
}

// Resolve the per-leg disposition Asterisk recorded into something an
// operator can act on. Two heuristics layered:
//
//   1. Server-side cross-check (preferred): if the dialed extension's
//      registration log says it was NOT registered at the leg's
//      calldate (or its last status was REMOVED/UNREACHABLE/UNQUALIFIED),
//      relabel to "Stale registration" — Asterisk dialed a phantom
//      contact in its PJSIP table that never actually responded.
//
//   2. Client-side billsec heuristic (fallback): a real SIP 486 BUSY
//      arrives in <1s. A 'BUSY' with billsec ≥ 30s is almost always
//      chan_pjsip's INVITE timeout coerced into BUSY by FreePBX
//      defaults — the phone never responded. Relabel "Unreachable"
//      so operators don't mistakenly think a phone was on another call.
//
// Returns { label, color, hint } where hint is a tooltip string
// explaining the relabel (or '' when no relabel was needed).
interface ResolvedDisposition {
  label: string;
  color: string;
  hint: string;
}

function resolveLegDisposition(leg: {
  disposition: string;
  billsecSeconds: number;
  dialedExtensionRegistration?: DialedExtensionRegistration;
}): ResolvedDisposition {
  const baseLabel = dispositionLabel(leg.disposition);
  const baseColor = dispositionColor(leg.disposition);

  // (1) Use the registration log when present.
  const reg = leg.dialedExtensionRegistration;
  if (reg && leg.disposition === 'DISPOSITION_BUSY') {
    const offline = reg.registered === false ||
      reg.lastStatus === 'REG_STATUS_REMOVED' ||
      reg.lastStatus === 'REG_STATUS_UNREACHABLE' ||
      reg.lastStatus === 'REG_STATUS_UNQUALIFIED';
    if (offline) {
      const last = reg.lastStatus
        ? reg.lastStatus.replace(/^REG_STATUS_/, '').toLowerCase()
        : 'unknown';
      return {
        label: 'Stale registration',
        color: '#FAAD14',
        hint:
          `CDR recorded BUSY, but the dialed extension was not registered at the time ` +
          `of the call (last status: ${last}). Asterisk likely had a stale PJSIP contact ` +
          `and the INVITE timed out — no human ever picked up.`,
      };
    }
  }

  // (2) Billsec heuristic for BUSY. A real SIP 486 BUSY HERE returns
  // sub-second; only INVITE timeouts coerce-to-BUSY take this long.
  if (leg.disposition === 'DISPOSITION_BUSY' && leg.billsecSeconds >= 30) {
    return {
      label: 'Unreachable',
      color: '#FAAD14',
      hint:
        `CDR recorded BUSY but the leg lasted ${leg.billsecSeconds}s — too long for a ` +
        `real SIP 486 (which arrives in <1s). This is almost always chan_pjsip's INVITE ` +
        `timeout to a phone that wasn't actually responding, coerced to BUSY by ` +
        `FreePBX's Dial defaults.`,
    };
  }

  return { label: baseLabel, color: baseColor, hint: '' };
}

// callerLabel and destinationLabel surface the user's mental model of the call
// (who placed it, where they were trying to reach) regardless of how FreePBX
// internally routed it. callerSubtitle/destinationSubtitle expose the raw
// routing (CallerID used on the trunk, ringgroup that fanned out the call) so
// the technical detail isn't hidden — just no longer the primary value.
function callerLabel(s: Call): string {
  if (s.direction === 'outbound' || s.direction === 'internal') {
    return s.originatingExtension || s.src;
  }
  return s.src;
}

function callerSubtitle(s: Call): string {
  if (s.direction === 'outbound' || s.direction === 'internal') {
    // Outgoing CallerID presented to the far end.
    return s.src ? `as CID ${s.src}` : '';
  }
  return s.cnam ? `(${s.cnam})` : '';
}

function destinationLabel(s: Call): string {
  if (s.direction === 'inbound') {
    return s.did || s.dst;
  }
  return s.dst;
}

function destinationSubtitle(s: Call): string {
  if (s.direction === 'inbound') {
    const parts: string[] = [];
    if (s.dst && s.dst !== s.did) parts.push(`via ${s.dst}`);
    if (s.answeredExtension) parts.push(`→ ext ${s.answeredExtension}`);
    return parts.join(' ');
  }
  return '';
}

const legColumns = [
  { title: 'side', key: 'side', width: 110 },
  { title: 'channel', dataIndex: 'channel', key: 'channel', width: 220 },
  { title: 'extension', dataIndex: 'extension', key: 'extension', width: 100 },
  { title: 'disposition', dataIndex: 'disposition', key: 'disposition', width: 130 },
  { title: 'duration', dataIndex: 'durationSeconds', key: 'durationSeconds', width: 90 },
  { title: 'billsec', dataIndex: 'billsecSeconds', key: 'billsecSeconds', width: 90 },
  { title: 'quality', key: 'quality', width: 320 },
];

// Parse a PJSIP/Local/SIP channel name down to the extension/peer
// label — same logic the backend uses, replicated client-side so we
// don't need a round-trip just to label the row.
function extractChannelLabel(ch: string | undefined): string {
  if (!ch) return '';
  // 'PJSIP/22-0000000c' → '22'    'PJSIP/ITD-0000000d' → 'ITD'
  // 'Local/600@from-internal-...' → '600'
  const m = /^[A-Za-z]+\/([^-@/]+)/.exec(ch);
  return m ? m[1] : ch;
}

// One CDR row in FreePBX's default config consolidates a 2-party call
// into a single row — channel = originator, dstchannel = answerer,
// rtpqos = originator's view, peerrtpqos = answerer's view.
//
// Operators want to see this as TWO rows in the legs table, one per
// side, each carrying the perspective of the channel that recorded
// it. We split here in the frontend so the underlying CDR API stays
// unchanged.
//
// Failed/unanswered calls (no dstchannel) fall through unsplit.
interface DisplayLeg {
  // Underlying CallLeg fields, copied through so the bodyCell template
  // doesn't need to special-case anything.
  uniqueid: string;
  channel: string;
  extension: string;
  disposition: string;
  durationSeconds: number;
  billsecSeconds: number;
  // Display extras.
  key: string;
  displayIndex: number;
  side: 'originator' | 'answerer';
  rtpQos?: RTPQoS;
  // Carried through so resolveLegDisposition can apply the registration
  // log heuristic. Same value on both originator and answerer rows of
  // a split CDR — the disposition is per-call, not per-perspective.
  dialedExtensionRegistration?: DialedExtensionRegistration;
  // Map back to the source CDR leg index so callQualitySummary can
  // reference 'leg 1 originator' instead of 'leg 1.5'.
  sourceLegIndex: number;
}

// One CDR row in FreePBX's default config consolidates a 2-party call
// into a single row — channel = originator, dstchannel = answerer.
// With the per-channel hangup_handler dialplan (extensions_override_
// freepbx.conf), each side now writes its own RTP perspective:
//   - rtpqos     ← master channel's (originator's) view
//   - peerrtpqos ← bridged peer's (answerer's) view
// We split each CDR row into TWO display rows so operators see both
// channels with their own quality data. Rows are only split when
// both dstchannel AND peerRtpQos are present — failed/unanswered
// calls (no dstchannel) and PBXs without the per-channel handler
// (no peerRtpQos) collapse to a single row cleanly.
const displayLegs = computed<DisplayLeg[]>(() => {
  const out: DisplayLeg[] = [];
  legs.value.forEach((leg, idx) => {
    out.push({
      uniqueid: leg.uniqueid,
      channel: leg.channel,
      extension: leg.extension || extractChannelLabel(leg.channel),
      disposition: leg.disposition,
      durationSeconds: leg.durationSeconds,
      billsecSeconds: leg.billsecSeconds,
      key: `${leg.uniqueid}-orig`,
      displayIndex: out.length + 1,
      side: 'originator',
      rtpQos: leg.rtpQos,
      dialedExtensionRegistration: leg.dialedExtensionRegistration,
      sourceLegIndex: idx,
    });
    if (leg.dstchannel && leg.dstchannel !== leg.channel && leg.peerRtpQos) {
      out.push({
        uniqueid: leg.uniqueid,
        channel: leg.dstchannel,
        extension: extractChannelLabel(leg.dstchannel),
        disposition: leg.disposition,
        durationSeconds: leg.durationSeconds,
        billsecSeconds: leg.billsecSeconds,
        key: `${leg.uniqueid}-ans`,
        displayIndex: out.length + 1,
        side: 'answerer',
        rtpQos: leg.peerRtpQos,
        dialedExtensionRegistration: leg.dialedExtensionRegistration,
        sourceLegIndex: idx,
      });
    }
  });
  return out;
});

function sideTagColor(side: 'originator' | 'answerer'): string {
  return side === 'originator' ? '#1890FF' : '#722ED1';
}

// Quality colour palette — one shared definition so the per-direction
// badges (↓ rx / ↑ tx) and the overall worst-band tag agree.
function qualityColor(band: string): string {
  switch (band) {
    case 'QUALITY_EXCELLENT': return '#52C41A';
    case 'QUALITY_GOOD':      return '#73D13D';
    case 'QUALITY_FAIR':      return '#FAAD14';
    case 'QUALITY_POOR':      return '#FA8C16';
    case 'QUALITY_BAD':       return '#FF4D4F';
    default:                  return '#999999';
  }
}

function qualityLabel(band: string): string {
  switch (band) {
    case 'QUALITY_EXCELLENT': return 'Excellent';
    case 'QUALITY_GOOD':      return 'Good';
    case 'QUALITY_FAIR':      return 'Fair';
    case 'QUALITY_POOR':      return 'Poor';
    case 'QUALITY_BAD':       return 'Bad';
    default:                  return '—';
  }
}

// Map a quality band to an Ant Design Alert severity. Alert uses
// CSS variables internally so it picks up dark/light theme tokens
// without us hardcoding any colours.
function qualityAlertType(band: string): 'success' | 'info' | 'warning' | 'error' {
  switch (band) {
    case 'QUALITY_EXCELLENT':
    case 'QUALITY_GOOD':
      return 'success';
    case 'QUALITY_FAIR':
      return 'info';
    case 'QUALITY_POOR':
      return 'warning';
    case 'QUALITY_BAD':
      return 'error';
    default:
      return 'info';
  }
}

// Translate a per-direction MOS (1.0–4.5) into the same band ladder so
// rx and tx tags are coloured independently. Operators pinpoint the
// problem direction by looking at which side is red.
function bandFromMos(mos: number): string {
  if (mos <= 0)   return 'QUALITY_UNKNOWN';
  if (mos >= 4.3) return 'QUALITY_EXCELLENT';
  if (mos >= 4.0) return 'QUALITY_GOOD';
  if (mos >= 3.6) return 'QUALITY_FAIR';
  if (mos >= 3.1) return 'QUALITY_POOR';
  return 'QUALITY_BAD';
}

// Roll up worst observed quality across all legs of a call so the
// drawer can show one big "this call had problems on leg X, peer-side
// TX" summary at the top.
interface CallQualitySummary {
  band: string;
  worstLegIndex: number;
  worstSide: 'local' | 'peer';
  worstDirection: 'rx' | 'tx';
  worstMos: number;
}

// Walks the SAME displayLegs the table renders, so 'leg N' in the
// banner matches the row number the operator sees. Each display row
// already represents one side (originator/answerer), so we no longer
// need the old worstSide field — the row label is the side.
const callQualitySummary = computed<CallQualitySummary | null>(() => {
  let worst: CallQualitySummary | null = null;
  for (const dl of displayLegs.value) {
    const q = dl.rtpQos;
    if (!q) continue;
    const candidates: { mos: number; dir: 'rx' | 'tx' }[] = [
      { mos: q.rxMos, dir: 'rx' },
      { mos: q.txMos, dir: 'tx' },
    ];
    for (const c of candidates) {
      if (c.mos <= 0) continue;
      if (!worst || c.mos < worst.worstMos) {
        worst = {
          band: bandFromMos(c.mos),
          worstLegIndex: dl.displayIndex - 1, // back to 0-based for the +1 in the template
          worstSide: dl.side === 'originator' ? 'local' : 'peer',
          worstDirection: c.dir,
          worstMos: c.mos,
        };
      }
    }
  }
  return worst;
});

const timelineColumns = [
  { title: 'time', dataIndex: 'eventTime', key: 'eventTime', width: 180 },
  { title: 'event', dataIndex: 'eventtype', key: 'eventtype', width: 130 },
  { title: 'channel', dataIndex: 'channame', key: 'channame', width: 220 },
  { title: 'app', dataIndex: 'appname', key: 'appname', width: 130 },
  { title: 'data', dataIndex: 'appdata', key: 'appdata' },
];

const recordingError = ref<string>('');

// Recording is served by the asterisk module's HTTP server, exposed via
// the platform's module-asset proxy at /modules/asterisk/recordings/...
// Path-encoded so unusual linkedids don't break the URL. The auth cookie
// is sent automatically by the browser.
function recordingUrl(s: Call): string {
  return `/modules/asterisk/recordings/${encodeURIComponent(s.linkedid)}`;
}

function onRecordingError() {
  recordingError.value =
    'Recording not available. Either the file is missing on disk or ASTERISK_RECORDINGS_PATH is not mounted.';
}

const onlineColumns = [
  { title: 'Extension', dataIndex: 'endpoint', key: 'endpoint', width: 120 },
  { title: 'Status', key: 'status', width: 130 },
  { title: 'User-Agent', dataIndex: 'userAgent', key: 'userAgent' },
  { title: 'Via', dataIndex: 'viaAddress', key: 'viaAddress', width: 150 },
];
</script>

<template>
  <Drawer
    :title="$t('asterisk.page.calls.drilldown')"
    :footer="false"
    width="900px"
  >
    <Spin :spinning="loading">
      <Descriptions v-if="summary" :column="2" bordered size="small">
        <DescriptionsItem :label="$t('asterisk.page.calls.linkedid')" :span="2">
          {{ summary.linkedid }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.calldate')">
          {{ formatDateTime(summary.calldate) }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.disposition')">
          <Tag :color="dispositionColor(summary.disposition)">
            {{ dispositionLabel(summary.disposition) }}
          </Tag>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.src')">
          {{ callerLabel(summary) }}
          <span v-if="callerSubtitle(summary)" style="color:#999;margin-left:4px">
            {{ callerSubtitle(summary) }}
          </span>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.dst')">
          {{ destinationLabel(summary) }}
          <span v-if="destinationSubtitle(summary)" style="color:#999;margin-left:4px">
            {{ destinationSubtitle(summary) }}
          </span>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.direction')">
          {{ summary.direction }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.answeredExtension')">
          {{ summary.answeredExtension || '—' }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.duration')">
          {{ summary.durationSeconds }}s
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.billsec')">
          {{ summary.billsecSeconds }}s
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.pickup')">
          {{
            summary.pickupSeconds == null
              ? $t('asterisk.page.calls.noPickup')
              : `${summary.pickupSeconds}s`
          }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('asterisk.page.calls.legCount')">
          {{ summary.legCount }}
        </DescriptionsItem>
      </Descriptions>

      <Card
        v-if="summary && summary.recordingFile"
        size="small"
        style="margin-top: 16px"
        title="Recording"
      >
        <audio
          :src="recordingUrl(summary)"
          controls
          preload="metadata"
          style="width: 100%"
          @error="onRecordingError"
        />
        <div v-if="recordingError" style="color: #FF4D4F; font-size: 12px; margin-top: 6px">
          {{ recordingError }}
        </div>
        <div style="font-size: 12px; color: #888; margin-top: 6px">
          {{ summary.recordingFile }}
        </div>
      </Card>

      <Tabs v-if="summary" style="margin-top: 16px">
        <TabPane key="online" :tab="`Online at call time (${onlineAtCall.length})`">
          <div v-if="onlineError" style="color: #FF4D4F; padding: 12px 0">
            {{ onlineError }}
          </div>
          <div v-else style="font-size: 12px; color: #888; margin-bottom: 8px">
            Extensions registered at {{ formatDateTime(summary.calldate) }}.
            For inbound ringgroup calls these are the operators that could
            potentially have answered.
          </div>
          <Table
            v-if="onlineAtCall.length > 0"
            :columns="onlineColumns"
            :data-source="onlineAtCall.map((e) => ({ ...e, key: e.endpoint }))"
            :pagination="{ pageSize: 20, showSizeChanger: false }"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'status'">
                <Tag color="#52C41A">{{ regStatusLabel(record.status) }}</Tag>
              </template>
            </template>
          </Table>
          <Empty
            v-else-if="onlineLoaded && !onlineError"
            description="Nobody was registered at this time."
          />
        </TabPane>
        <TabPane key="legs" :tab="$t('asterisk.page.calls.legs')">
          <Alert
            v-if="callQualitySummary"
            :type="qualityAlertType(callQualitySummary.band)"
            show-icon
            style="margin-bottom: 12px"
          >
            <template #message>
              <Tag :color="qualityColor(callQualitySummary.band)" style="margin-right: 8px">
                {{ qualityLabel(callQualitySummary.band) }}
              </Tag>
              Worst:
              <strong>row {{ callQualitySummary.worstLegIndex + 1 }}</strong>
              <span v-if="callQualitySummary.worstSide">
                · <strong>{{ callQualitySummary.worstSide === 'local' ? 'originator' : 'answerer' }} side</strong>
              </span>
              ·
              <strong>{{ callQualitySummary.worstDirection === 'rx' ? 'incoming (RX)' : 'outgoing (TX)' }}</strong>
              · MOS {{ callQualitySummary.worstMos.toFixed(2) }}
            </template>
            <template #description>
              Each row below shows one channel's RTP perspective. ↓ RX is what that channel heard;
              ↑ TX is what the other end reported hearing (via RTCP). When the originator and answerer
              rows disagree on the same direction, the difference points at the network path between
              one specific channel and the bridge — investigate that side.
            </template>
          </Alert>
          <Table
            :columns="legColumns"
            :data-source="displayLegs"
            :pagination="false"
            row-key="key"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'side'">
                <Tag :color="sideTagColor(record.side)">{{ record.side }}</Tag>
              </template>
              <template v-else-if="column.key === 'disposition'">
                <Tooltip
                  v-if="resolveLegDisposition(record).hint"
                  :title="resolveLegDisposition(record).hint"
                >
                  <Tag :color="resolveLegDisposition(record).color">
                    {{ resolveLegDisposition(record).label }}
                  </Tag>
                </Tooltip>
                <Tag v-else :color="resolveLegDisposition(record).color">
                  {{ resolveLegDisposition(record).label }}
                </Tag>
              </template>
              <template v-else-if="column.key === 'quality' && record.rtpQos">
                <div style="display: flex; gap: 6px; flex-wrap: wrap; align-items: center">
                  <Tag :color="qualityColor(bandFromMos(record.rtpQos.rxMos))">
                    ↓ RX {{ record.rtpQos.rxMos > 0 ? record.rtpQos.rxMos.toFixed(2) : '—' }}
                  </Tag>
                  <Tag :color="qualityColor(bandFromMos(record.rtpQos.txMos))">
                    ↑ TX {{ record.rtpQos.txMos > 0 ? record.rtpQos.txMos.toFixed(2) : '—' }}
                  </Tag>
                  <span
                    v-if="record.rtpQos.rxLossPercent + record.rtpQos.txLossPercent > 0"
                    :style="{
                      fontSize: '11px',
                      color: (record.rtpQos.rxLossPercent + record.rtpQos.txLossPercent) > 2
                        ? 'var(--ant-color-error)'
                        : 'var(--ant-color-text-secondary)',
                    }"
                  >
                    loss rx {{ record.rtpQos.rxLossPercent.toFixed(1) }}% / tx {{ record.rtpQos.txLossPercent.toFixed(1) }}%
                  </span>
                  <span
                    v-if="record.rtpQos.rttMs > 0"
                    :style="{
                      fontSize: '11px',
                      color: record.rtpQos.rttMs > 200
                        ? 'var(--ant-color-error)'
                        : 'var(--ant-color-text-secondary)',
                    }"
                  >
                    rtt {{ record.rtpQos.rttMs.toFixed(0) }}ms
                  </span>
                </div>
              </template>
              <template v-else-if="column.key === 'quality'">
                <span style="color: var(--ant-color-text-disabled)">—</span>
              </template>
            </template>
          </Table>
        </TabPane>
        <TabPane key="timeline" :tab="$t('asterisk.page.calls.timeline')">
          <Table
            :columns="timelineColumns"
            :data-source="timeline.map((e, i) => ({ ...e, key: i, eventTime: formatDateTime(e.eventTime) }))"
            :pagination="false"
            size="small"
          />
        </TabPane>
      </Tabs>
    </Spin>
  </Drawer>
</template>
