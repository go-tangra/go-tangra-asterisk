<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import {
  Alert,
  Card,
  Col,
  Empty,
  Row,
  Spin,
  Statistic,
  Table,
  Tag,
} from 'ant-design-vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
} from 'echarts/components';

import {
  DashboardService,
  LiveCallsService,
  type InstantSample,
  type LiveCall,
  type RangeSeries,
} from '../../api/services';

use([
  CanvasRenderer,
  LineChart,
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
]);

// 30-second refresh keeps the page roughly in sync with Prometheus's
// default scrape interval without hammering the gateway.
const REFRESH_MS = 30_000;

// 1-hour rolling window for the time-series panel; step is chosen to
// produce ~60 datapoints which renders smoothly in ECharts.
const RANGE_WINDOW_MS = 60 * 60 * 1000;
const RANGE_STEP_SECONDS = 60;

interface DashboardSnapshot {
  up?: InstantSample;
  uptimeSeconds?: number;
  currentCalls?: number;
  channelsActive?: number;
  scrapeDuration?: number;
  scrapeErrorsTotal?: number;
  endpoints: InstantSample[];
  peers: InstantSample[];
  peerLatency: InstantSample[];
  queueCallers: InstantSample[];
  queueCompleted: InstantSample[];
  queueAbandoned: InstantSample[];
  queueMembers: InstantSample[];
}

const loading = ref(false);
const error = ref<string>('');
const lastUpdated = ref<Date | null>(null);

const snapshot = ref<DashboardSnapshot>({
  endpoints: [],
  peers: [],
  peerLatency: [],
  queueCallers: [],
  queueCompleted: [],
  queueAbandoned: [],
  queueMembers: [],
});

const callsSeries = ref<RangeSeries | null>(null);

// Live calls — fed by both the initial REST snapshot and the SSE event
// stream. Indexed by linkedid so updates collapse to a single row.
const liveCalls = ref<Map<string, LiveCall>>(new Map());
const liveError = ref<string>('');
let eventSource: EventSource | null = null;
let endedTimers = new Map<string, ReturnType<typeof setTimeout>>();
const ENDED_FLASH_MS = 4000;

function applySnapshot(calls: LiveCall[]): void {
  const next = new Map<string, LiveCall>();
  for (const c of calls) next.set(c.linkedid, c);
  liveCalls.value = next;
}

function applyEvent(payload: { type: string; call?: LiveCall; endedCall?: LiveCall }): void {
  // Mutate-then-reassign so Vue's reactivity tracks the change
  // (Map doesn't deep-track entries on its own).
  const next = new Map(liveCalls.value);
  if (payload.type === 'call.ended' && payload.endedCall) {
    next.set(payload.endedCall.linkedid, payload.endedCall);
    liveCalls.value = next;
    // Flash the ended row briefly then remove it so operators see the
    // hangup without the row vanishing mid-glance.
    const id = payload.endedCall.linkedid;
    const prev = endedTimers.get(id);
    if (prev) clearTimeout(prev);
    endedTimers.set(
      id,
      setTimeout(() => {
        const m = new Map(liveCalls.value);
        m.delete(id);
        liveCalls.value = m;
        endedTimers.delete(id);
      }, ENDED_FLASH_MS),
    );
  } else if (payload.call) {
    next.set(payload.call.linkedid, payload.call);
    liveCalls.value = next;
  }
}

function connectStream(): void {
  // The asterisk module's HTTP server is reverse-proxied at
  // /modules/asterisk/* by admin-service; the auth cookie is
  // forwarded automatically. Go's httputil.ReverseProxy auto-detects
  // text/event-stream and switches to flushing-streaming mode (1.19+).
  const url = '/modules/asterisk/calls/stream';
  const es = new EventSource(url, { withCredentials: true });

  es.addEventListener('snapshot', (e) => {
    try {
      applySnapshot(JSON.parse((e as MessageEvent).data));
    } catch {
      /* ignore malformed snapshot */
    }
  });
  for (const t of ['call.started', 'call.updated', 'call.ended']) {
    es.addEventListener(t, (e) => {
      try {
        applyEvent(JSON.parse((e as MessageEvent).data));
      } catch {
        /* ignore */
      }
    });
  }
  es.onerror = () => {
    // Browser auto-reconnects; just surface that we're momentarily
    // disconnected so the operator knows what they're looking at.
    liveError.value = 'Live stream disconnected — retrying…';
  };
  es.onopen = () => {
    liveError.value = '';
  };

  eventSource = es;
}

async function loadLiveCallsSnapshot(): Promise<void> {
  try {
    const resp = await LiveCallsService.listActive();
    applySnapshot(resp.calls ?? []);
  } catch (e) {
    liveError.value = e instanceof Error ? e.message : 'Failed to load active calls';
  }
}
// Registered (available) extensions over time. We sum
// asterisk_pjsip_endpoint_up filtered to kind="extension" so trunks don't
// inflate the count — the exporter labels each endpoint by a
// trunk/extension heuristic.
const registeredExtensionsSeries = ref<RangeSeries | null>(null);

let timer: ReturnType<typeof setInterval> | null = null;

function firstScalar(samples: InstantSample[] | undefined): number | undefined {
  if (!samples || samples.length === 0) return undefined;
  const s = samples[0];
  return s && s.hasValue ? s.value : undefined;
}

async function instant(query: string): Promise<InstantSample[]> {
  try {
    const r = await DashboardService.query({ query });
    return r.series ?? [];
  } catch (e) {
    // Surface the first failure but don't kill the rest of the dashboard;
    // an exporter being down for one panel shouldn't blank the others.
    error.value = e instanceof Error ? e.message : String(e);
    return [];
  }
}

async function rangeQuery(query: string): Promise<RangeSeries | null> {
  const end = new Date();
  const start = new Date(end.getTime() - RANGE_WINDOW_MS);
  try {
    const r = await DashboardService.queryRange({
      query,
      start: start.toISOString(),
      end: end.toISOString(),
      stepSeconds: RANGE_STEP_SECONDS,
    });
    return r.series?.[0] ?? null;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    return null;
  }
}

async function refresh(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    const [
      up,
      uptime,
      current,
      channels,
      scrape,
      scrapeErrors,
      endpoints,
      peers,
      peerLatency,
      queueCallers,
      queueCompleted,
      queueAbandoned,
      queueMembers,
      callsRange,
      registeredExtensionsRange,
    ] = await Promise.all([
      instant('asterisk_up'),
      instant('asterisk_uptime_seconds'),
      // Active calls = unique Linkedid count from CoreShowChannels
      // (added in freepbx-exporter v0.1.3). One per logical call
      // regardless of leg count, including ringing-only calls.
      instant('asterisk_calls_active'),
      instant('asterisk_channels_active'),
      instant('asterisk_scrape_duration_seconds'),
      instant('sum(asterisk_scrape_errors_total)'),
      instant('asterisk_pjsip_endpoints'),
      instant('asterisk_sip_peers'),
      instant('asterisk_sip_peer_latency_milliseconds'),
      instant('asterisk_queue_callers'),
      instant('asterisk_queue_completed_calls'),
      instant('asterisk_queue_abandoned_calls'),
      instant('asterisk_queue_members'),
      rangeQuery('asterisk_calls_active'),
      rangeQuery('sum(asterisk_pjsip_endpoint_up{kind="extension"})'),
    ]);

    snapshot.value = {
      up: up[0],
      uptimeSeconds: firstScalar(uptime),
      currentCalls: firstScalar(current),
      channelsActive: firstScalar(channels),
      scrapeDuration: firstScalar(scrape),
      scrapeErrorsTotal: firstScalar(scrapeErrors),
      endpoints,
      peers,
      peerLatency,
      queueCallers,
      queueCompleted,
      queueAbandoned,
      queueMembers,
    };
    callsSeries.value = callsRange;
    registeredExtensionsSeries.value = registeredExtensionsRange;
    lastUpdated.value = new Date();
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  refresh();
  timer = setInterval(refresh, REFRESH_MS);
  loadLiveCallsSnapshot();
  connectStream();
});

onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  for (const t of endedTimers.values()) clearTimeout(t);
  endedTimers.clear();
});

// ---- panel helpers ----

function formatUptime(seconds?: number): string {
  if (seconds == null || !Number.isFinite(seconds)) return '—';
  const d = Math.floor(seconds / 86_400);
  const h = Math.floor((seconds % 86_400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatNumber(n?: number): string {
  if (n == null || !Number.isFinite(n)) return '—';
  return Math.round(n).toString();
}

function formatMillis(seconds?: number): string {
  if (seconds == null || !Number.isFinite(seconds)) return '—';
  return `${Math.round(seconds * 1000)} ms`;
}

const upTag = computed(() => {
  const v = snapshot.value.up;
  if (!v) return { color: '#999999', label: 'Unknown' };
  if (!v.hasValue) return { color: '#999999', label: 'Unknown' };
  return v.value === 1
    ? { color: '#52C41A', label: 'Up' }
    : { color: '#FF4D4F', label: 'Down' };
});

function lineChartOption(series: RangeSeries | null, name: string, color?: string) {
  if (!series) return null;
  const points = series.timestamps.map((t, i) => [
    new Date(t).getTime(),
    series.values[i] ?? 0,
  ]);
  return {
    grid: { left: 40, right: 16, top: 16, bottom: 28 },
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'time' },
    yAxis: { type: 'value', minInterval: 1 },
    series: [
      {
        name,
        type: 'line',
        showSymbol: false,
        smooth: true,
        data: points,
        areaStyle: { opacity: 0.15, color },
        lineStyle: { width: 2, color },
        itemStyle: color ? { color } : undefined,
      },
    ],
  };
}

const callsChartOption = computed(() =>
  lineChartOption(callsSeries.value, 'Active calls'),
);

const registeredExtensionsChartOption = computed(() =>
  lineChartOption(registeredExtensionsSeries.value, 'Registered extensions', '#52C41A'),
);

// PJSIP endpoint table: collapse the labels into a row per (kind, state).
interface EndpointRow {
  key: string;
  kind: string;
  deviceState: string;
  count: number;
}

const endpointRows = computed<EndpointRow[]>(() =>
  snapshot.value.endpoints
    .filter((s) => s.hasValue)
    .map((s, i) => ({
      key: `${i}`,
      kind: s.labels.kind || '—',
      deviceState: s.labels.device_state || '—',
      count: s.value,
    }))
    .sort((a, b) => b.count - a.count),
);

interface PeerRow {
  key: string;
  status: string;
  count: number;
}

const peerRows = computed<PeerRow[]>(() =>
  snapshot.value.peers
    .filter((s) => s.hasValue)
    .map((s, i) => ({
      key: `${i}`,
      status: s.labels.status || '—',
      count: s.value,
    }))
    .sort((a, b) => b.count - a.count),
);

interface PeerLatencyRow {
  key: string;
  peer: string;
  latencyMs: number;
}

const peerLatencyRows = computed<PeerLatencyRow[]>(() =>
  snapshot.value.peerLatency
    .filter((s) => s.hasValue)
    .map((s, i) => ({
      key: `${i}`,
      peer: s.labels.peer || '—',
      latencyMs: s.value,
    }))
    .sort((a, b) => b.latencyMs - a.latencyMs)
    .slice(0, 20),
);

// Queue rollup: index by queue name, fill in metrics from each metric set.
interface QueueRow {
  key: string;
  queue: string;
  callers: number;
  completed: number;
  abandoned: number;
  members: number;
}

const queueRows = computed<QueueRow[]>(() => {
  const byQueue = new Map<string, QueueRow>();
  function bucket(name: string): QueueRow {
    let row = byQueue.get(name);
    if (!row) {
      row = { key: name, queue: name, callers: 0, completed: 0, abandoned: 0, members: 0 };
      byQueue.set(name, row);
    }
    return row;
  }
  for (const s of snapshot.value.queueCallers) {
    if (!s.hasValue) continue;
    bucket(s.labels.queue || '—').callers = s.value;
  }
  for (const s of snapshot.value.queueCompleted) {
    if (!s.hasValue) continue;
    bucket(s.labels.queue || '—').completed = s.value;
  }
  for (const s of snapshot.value.queueAbandoned) {
    if (!s.hasValue) continue;
    bucket(s.labels.queue || '—').abandoned = s.value;
  }
  for (const s of snapshot.value.queueMembers) {
    if (!s.hasValue) continue;
    // queueMembers is labelled by queue+status; collapse to total members.
    bucket(s.labels.queue || '—').members += s.value;
  }
  return [...byQueue.values()].sort((a, b) => b.callers - a.callers);
});

const endpointColumns = [
  { title: 'Kind', dataIndex: 'kind', key: 'kind', width: 120 },
  { title: 'Device state', dataIndex: 'deviceState', key: 'deviceState' },
  { title: 'Count', dataIndex: 'count', key: 'count', width: 100 },
];

const peerColumns = [
  { title: 'Status', dataIndex: 'status', key: 'status' },
  { title: 'Count', dataIndex: 'count', key: 'count', width: 100 },
];

const peerLatencyColumns = [
  { title: 'Peer', dataIndex: 'peer', key: 'peer' },
  { title: 'Latency (ms)', dataIndex: 'latencyMs', key: 'latencyMs', width: 140 },
];

const queueColumns = [
  { title: 'Queue', dataIndex: 'queue', key: 'queue' },
  { title: 'Waiting', dataIndex: 'callers', key: 'callers', width: 100 },
  { title: 'Members', dataIndex: 'members', key: 'members', width: 100 },
  { title: 'Completed', dataIndex: 'completed', key: 'completed', width: 110 },
  { title: 'Abandoned', dataIndex: 'abandoned', key: 'abandoned', width: 120 },
];

// Live calls table — derived from the SSE-fed Map.
interface LiveCallRow {
  key: string;
  linkedid: string;
  from: string;
  to: string;
  state: string;
  channels: number;
  bridged: boolean;
  startedAt: string;
  durationSeconds: number;
}

const nowTick = ref(Date.now());
// Re-render durations every second without re-fetching anything.
let nowTimer: ReturnType<typeof setInterval> | null = null;
onMounted(() => {
  nowTimer = setInterval(() => {
    nowTick.value = Date.now();
  }, 1000);
});
onBeforeUnmount(() => {
  if (nowTimer) clearInterval(nowTimer);
});

// Asterisk's "no value" sentinels — sometimes leak through from older
// chan drivers. Backend already strips these, but defend in depth.
function clean(v: string | undefined): string {
  if (!v) return '';
  if (v === '<unknown>' || v === '<no name>' || v === '<not provided>') return '';
  return v;
}

function callerOf(c: LiveCall): string {
  // Originating leg is channels[0] (sorted by createdAt asc in registry).
  const c0 = c.channels[0];
  if (!c0) return '—';
  const num = clean(c0.callerIdNum) || clean(c0.exten);
  const name = clean(c0.callerIdName);
  if (!num && !name) return '—';
  if (name && name !== num) return num ? `${num} (${name})` : name;
  return num || '—';
}

function calleeOf(c: LiveCall): string {
  // Preferred destination signals, in order:
  //   1. connectedLineNum on the originating leg (set after a Dial())
  //   2. callerIdNum on a later leg (the answering / dialed party)
  //   3. exten on a later leg (ring group member being rung)
  //   4. exten on the originating leg (announcement/IVR step before bridge)
  const c0 = c.channels[0];
  for (let i = 1; i < c.channels.length; i++) {
    const ci = c.channels[i];
    if (ci) {
      const v = clean(ci.callerIdNum) || clean(ci.exten);
      if (v) return v;
    }
  }
  return clean(c0?.connectedLineNum) || clean(c0?.exten) || '—';
}

function callState(c: LiveCall): string {
  if (c.bridged) return 'In call';
  // Pick the most-active channel state across legs.
  const states = c.channels.map((ch) => ch.channelStateDesc || ch.channelState);
  if (states.includes('Up')) return 'Up';
  if (states.includes('Ringing')) return 'Ringing';
  if (states.includes('Ring')) return 'Ringing';
  return states[0] ?? '—';
}

function callStateColor(state: string): string {
  switch (state) {
    case 'In call':
      return '#52C41A';
    case 'Up':
      return '#1890FF';
    case 'Ringing':
      return '#FA8C16';
    default:
      return '#999999';
  }
}

const liveCallRows = computed<LiveCallRow[]>(() => {
  const rows: LiveCallRow[] = [];
  for (const c of liveCalls.value.values()) {
    const startedMs = new Date(c.startedAt).getTime();
    rows.push({
      key: c.linkedid,
      linkedid: c.linkedid,
      from: callerOf(c),
      to: calleeOf(c),
      state: callState(c),
      channels: c.channels.length,
      bridged: c.bridged,
      startedAt: c.startedAt,
      durationSeconds: Math.max(0, Math.floor((nowTick.value - startedMs) / 1000)),
    });
  }
  // Newest first matches operator expectation for a live grid.
  rows.sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime());
  return rows;
});

const liveCallColumns = [
  { title: 'From', dataIndex: 'from', key: 'from' },
  { title: 'To', dataIndex: 'to', key: 'to' },
  { title: 'State', key: 'state', width: 110 },
  { title: 'Legs', dataIndex: 'channels', key: 'channels', width: 70 },
  { title: 'Duration', key: 'duration', width: 110 },
];

function formatCallDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return m > 0 ? `${m}m ${s.toString().padStart(2, '0')}s` : `${s}s`;
}
</script>

<template>
  <div style="padding: 16px">
    <Spin :spinning="loading && !lastUpdated">
      <Alert
        v-if="error"
        :message="error"
        type="warning"
        show-icon
        closable
        style="margin-bottom: 16px"
      />

      <Card
        title="Live calls"
        size="small"
        style="margin-bottom: 16px"
      >
        <template #extra>
          <span v-if="liveError" style="color: #FF4D4F; font-size: 12px">{{ liveError }}</span>
          <span v-else style="color: #52C41A; font-size: 12px">● live</span>
        </template>
        <Table
          v-if="liveCallRows.length > 0"
          :columns="liveCallColumns"
          :data-source="liveCallRows"
          :pagination="false"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'state'">
              <Tag :color="callStateColor(record.state)">{{ record.state }}</Tag>
            </template>
            <template v-else-if="column.key === 'duration'">
              {{ formatCallDuration(record.durationSeconds) }}
            </template>
          </template>
        </Table>
        <Empty v-else description="No active calls" />
      </Card>

      <Row :gutter="16" style="margin-bottom: 16px">
        <Col :span="6">
          <Card>
            <Statistic title="Asterisk" :value="upTag.label" :value-style="{ color: upTag.color }" />
            <div v-if="lastUpdated" style="font-size: 12px; color: #888; margin-top: 4px">
              Updated {{ lastUpdated.toLocaleTimeString() }}
            </div>
          </Card>
        </Col>
        <Col :span="6">
          <Card><Statistic title="Active calls" :value="formatNumber(snapshot.currentCalls)" /></Card>
        </Col>
        <Col :span="6">
          <Card><Statistic title="Active channels" :value="formatNumber(snapshot.channelsActive)" /></Card>
        </Col>
        <Col :span="6">
          <Card><Statistic title="Uptime" :value="formatUptime(snapshot.uptimeSeconds)" /></Card>
        </Col>
      </Row>

      <Row :gutter="16" style="margin-bottom: 16px">
        <Col :span="12">
          <Card title="Active calls (last 1h)" size="small">
            <VChart
              v-if="callsChartOption"
              :option="callsChartOption"
              :autoresize="true"
              style="height: 240px"
            />
            <Empty v-else description="No data" />
          </Card>
        </Col>
        <Col :span="12">
          <Card title="Registered extensions (last 1h)" size="small">
            <VChart
              v-if="registeredExtensionsChartOption"
              :option="registeredExtensionsChartOption"
              :autoresize="true"
              style="height: 240px"
            />
            <Empty v-else description="No data" />
          </Card>
        </Col>
      </Row>

      <Row :gutter="16" style="margin-bottom: 16px">
        <Col :span="12">
          <Card title="PJSIP endpoints" size="small">
            <Table
              v-if="endpointRows.length > 0"
              :columns="endpointColumns"
              :data-source="endpointRows"
              :pagination="false"
              size="small"
            />
            <Empty v-else description="No endpoints reported" />
          </Card>
        </Col>
        <Col :span="12">
          <Card title="SIP peers (chan_sip)" size="small">
            <Table
              v-if="peerRows.length > 0"
              :columns="peerColumns"
              :data-source="peerRows"
              :pagination="false"
              size="small"
            />
            <Empty v-else description="No chan_sip peers" />
          </Card>
        </Col>
      </Row>

      <Row :gutter="16" style="margin-bottom: 16px">
        <Col :span="12">
          <Card title="Top peer latency (top 20)" size="small">
            <Table
              v-if="peerLatencyRows.length > 0"
              :columns="peerLatencyColumns"
              :data-source="peerLatencyRows"
              :pagination="false"
              size="small"
            />
            <Empty v-else description="No peer latency samples" />
          </Card>
        </Col>
        <Col :span="12">
          <Card title="Queues" size="small">
            <Table
              v-if="queueRows.length > 0"
              :columns="queueColumns"
              :data-source="queueRows"
              :pagination="false"
              size="small"
            />
            <Empty v-else description="No queues configured" />
          </Card>
        </Col>
      </Row>

      <Card title="Scrape health" size="small">
        <Row :gutter="16">
          <Col :span="8">
            <Statistic title="Scrape duration" :value="formatMillis(snapshot.scrapeDuration)" />
          </Col>
          <Col :span="8">
            <Statistic title="Scrape errors (total)" :value="formatNumber(snapshot.scrapeErrorsTotal)" />
          </Col>
          <Col :span="8">
            <div style="font-size: 14px; color: rgba(0,0,0,0.45); margin-bottom: 4px">Status</div>
            <Tag :color="upTag.color">{{ upTag.label }}</Tag>
          </Col>
        </Row>
      </Card>
    </Spin>
  </div>
</template>
