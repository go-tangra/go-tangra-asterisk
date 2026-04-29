<script lang="ts" setup>
import { ref, watch } from 'vue';

import { useVbenDrawer } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import {
  Card,
  Col,
  Descriptions,
  DescriptionsItem,
  Empty,
  Row,
  Spin,
  Statistic,
  TabPane,
  Table,
  Tabs,
  Tag,
} from 'ant-design-vue';

import { useAsteriskCdrStore } from '../../stores/asterisk-cdr.state';
import { useAsteriskStatsStore } from '../../stores/asterisk-stats.state';
import { useAsteriskRegistrationStore } from '../../stores/asterisk-registration.state';
import type {
  Call,
  CallDirection,
  Disposition,
  GetExtensionStatsResponse,
  GetRegistrationStatusResponse,
  RegStatus,
  RegistrationEvent,
} from '../../api/services';
import { formatDate, formatDateTime } from '../../utils/datetime';

interface DrawerArgs {
  extension: string;
  from: string;
  to: string;
}

const cdrStore = useAsteriskCdrStore();
const statsStore = useAsteriskStatsStore();
const regStore = useAsteriskRegistrationStore();

const args = ref<DrawerArgs | null>(null);
const activeTab = ref<CallDirection | 'stats' | 'registration'>('outbound');

const regStatus = ref<GetRegistrationStatusResponse | null>(null);
const regEvents = ref<RegistrationEvent[]>([]);
const regLoading = ref(false);
const regLoaded = ref(false);
const regError = ref<string>('');

const stats = ref<GetExtensionStatsResponse | null>(null);
const statsLoading = ref(false);

// One bucket per direction so switching tabs re-uses already-fetched data.
const calls = ref<Record<CallDirection, Call[]>>({
  outbound: [],
  inbound: [],
  internal: [],
});
const loaded = ref<Record<CallDirection, boolean>>({
  outbound: false,
  inbound: false,
  internal: false,
});
const loading = ref<Record<CallDirection, boolean>>({
  outbound: false,
  inbound: false,
  internal: false,
});

async function loadStats(): Promise<void> {
  if (!args.value || stats.value || statsLoading.value) return;
  statsLoading.value = true;
  try {
    stats.value = await statsStore.getExtension(args.value.extension, {
      from: args.value.from,
      to: args.value.to,
      bucket: 'TIME_BUCKET_DAY',
    });
  } finally {
    statsLoading.value = false;
  }
}

async function loadDirection(direction: CallDirection): Promise<void> {
  if (!args.value || loaded.value[direction] || loading.value[direction]) return;
  loading.value[direction] = true;
  try {
    const resp = await cdrStore.listCalls({
      from: args.value.from,
      to: args.value.to,
      extension: args.value.extension,
      direction,
      page: 0,
      pageSize: 200,
    });
    calls.value[direction] = resp.calls ?? [];
    loaded.value[direction] = true;
  } finally {
    loading.value[direction] = false;
  }
}

function resetState(): void {
  stats.value = null;
  statsLoading.value = false;
  calls.value = { outbound: [], inbound: [], internal: [] };
  loaded.value = { outbound: false, inbound: false, internal: false };
  loading.value = { outbound: false, inbound: false, internal: false };
  regStatus.value = null;
  regEvents.value = [];
  regLoading.value = false;
  regLoaded.value = false;
  regError.value = '';
  activeTab.value = 'outbound';
}

async function loadRegistration(): Promise<void> {
  if (!args.value || regLoaded.value || regLoading.value) return;
  regLoading.value = true;
  regError.value = '';
  try {
    // Status "now" + the audit log over the selected window. The two are
    // independent, but the table is the more useful one when AMI capture
    // is enabled — running them in parallel keeps the tab responsive.
    const [status, events] = await Promise.all([
      regStore.getStatus(args.value.extension, args.value.to),
      regStore.listEvents({
        extension: args.value.extension,
        from: args.value.from,
        to: args.value.to,
        page: 0,
        pageSize: 200,
      }),
    ]);
    regStatus.value = status;
    regEvents.value = events.events ?? [];
    regLoaded.value = true;
  } catch (err) {
    // Most likely cause: AMI capture is not configured (server returns
    // AMI_DISABLED → 503). Surface the message instead of leaving the tab
    // blank so the operator knows what to do.
    regError.value = err instanceof Error ? err.message : 'Failed to load registration data';
  } finally {
    regLoading.value = false;
  }
}

const [Drawer, drawerApi] = useVbenDrawer({
  onOpenChange: async (open: boolean) => {
    if (!open) return;
    const data = drawerApi.getData<DrawerArgs>();
    if (!data) return;
    resetState();
    args.value = data;
    // Stats summary loads eagerly so the header is populated regardless of
    // which tab the user lands on.
    await Promise.all([loadStats(), loadDirection('outbound')]);
  },
});

watch(
  () => drawerApi.isOpen,
  (open) => {
    if (!open) {
      args.value = null;
      resetState();
    }
  },
);

watch(activeTab, (tab) => {
  if (tab === 'stats') return;
  if (tab === 'registration') {
    loadRegistration();
    return;
  }
  loadDirection(tab);
});

function dispositionColor(d: Disposition): string {
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

function dispositionLabel(d: Disposition): string {
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

function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return '0s';
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return m === 0 ? `${s}s` : `${m}m ${s}s`;
}

// Caller side: for outbound/internal it's the originating extension, for
// inbound it's the external src.
function callerDisplay(row: Call): string {
  if (row.direction === 'outbound' || row.direction === 'internal') {
    return row.originatingExtension || row.src;
  }
  return row.src;
}

// Destination side: for inbound it's the DID actually dialed, otherwise
// the dialed number/extension.
function destinationDisplay(row: Call): string {
  if (row.direction === 'inbound') {
    return row.did || row.dst;
  }
  return row.dst;
}

// Hour-of-day buckets are emitted in UTC; convert to Sofia so 09:00 reads
// as the operator's local hour.
function bucketHour(iso: string): number {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return 0;
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Europe/Sofia',
    hour: '2-digit',
    hour12: false,
  }).formatToParts(d);
  const h = parts.find((p) => p.type === 'hour')?.value ?? '0';
  return Number.parseInt(h, 10);
}

const callColumns = [
  { title: $t('asterisk.page.calls.calldate'), key: 'calldate', width: 170 },
  { title: $t('asterisk.page.calls.src'), key: 'src', width: 140 },
  { title: $t('asterisk.page.calls.dst'), key: 'dst', width: 140 },
  { title: $t('asterisk.page.calls.disposition'), key: 'disposition', width: 120 },
  { title: $t('asterisk.page.calls.duration'), key: 'duration', width: 100 },
  { title: $t('asterisk.page.calls.billsec'), key: 'billsec', width: 100 },
  { title: $t('asterisk.page.calls.pickup'), key: 'pickup', width: 90 },
];

const seriesColumns = [
  { title: 'Bucket', dataIndex: 'bucketStart', key: 'bucketStart', width: 200 },
  { title: 'Total', dataIndex: 'total', key: 'total', width: 100 },
  { title: 'Answered', dataIndex: 'answered', key: 'answered', width: 100 },
  { title: 'Missed', dataIndex: 'missed', key: 'missed', width: 100 },
];

const hourColumns = [
  { title: 'Hour', dataIndex: 'hour', key: 'hour', width: 80 },
  { title: 'Total', dataIndex: 'total', key: 'total', width: 100 },
  { title: 'Answered', dataIndex: 'answered', key: 'answered', width: 100 },
  { title: 'Missed', dataIndex: 'missed', key: 'missed', width: 100 },
];

// Map RegStatus enum values to human-readable labels and tag colors.
function regStatusLabel(s: RegStatus): string {
  switch (s) {
    case 'REG_STATUS_CREATED':
      return 'Registered';
    case 'REG_STATUS_UPDATED':
      return 'Refreshed';
    case 'REG_STATUS_REACHABLE':
      return 'Reachable';
    case 'REG_STATUS_UNREACHABLE':
      return 'Unreachable';
    case 'REG_STATUS_REMOVED':
      return 'Unregistered';
    case 'REG_STATUS_UNQUALIFIED':
      return 'Unqualified';
    case 'REG_STATUS_UNKNOWN':
      return 'Unknown';
    default:
      return '—';
  }
}

function regStatusColor(s: RegStatus): string {
  switch (s) {
    case 'REG_STATUS_CREATED':
    case 'REG_STATUS_UPDATED':
    case 'REG_STATUS_REACHABLE':
      return '#52C41A';
    case 'REG_STATUS_UNREACHABLE':
    case 'REG_STATUS_REMOVED':
      return '#FF4D4F';
    case 'REG_STATUS_UNQUALIFIED':
    case 'REG_STATUS_UNKNOWN':
      return '#FA8C16';
    default:
      return '#999999';
  }
}

const regColumns = [
  { title: 'Time', key: 'eventTime', dataIndex: 'eventTime', width: 170 },
  { title: 'Status', key: 'status', dataIndex: 'status', width: 130 },
  { title: 'Contact', key: 'contactUri', dataIndex: 'contactUri' },
  { title: 'User-Agent', key: 'userAgent', dataIndex: 'userAgent', width: 200 },
  { title: 'Via', key: 'viaAddress', dataIndex: 'viaAddress', width: 130 },
];
</script>

<template>
  <Drawer
    :title="args ? `${$t('asterisk.page.extensions.extension')} ${args.extension}` : ''"
    :footer="false"
    width="1100px"
  >
    <Spin :spinning="statsLoading">
      <Descriptions v-if="stats" :column="2" bordered size="small" style="margin-bottom: 16px">
        <DescriptionsItem :label="$t('asterisk.page.extensions.extension')" :span="2">
          {{ stats.summary.extension }}
          <span v-if="stats.summary.displayName"> — {{ stats.summary.displayName }}</span>
        </DescriptionsItem>
      </Descriptions>

      <Row v-if="stats" :gutter="16" style="margin-bottom: 16px">
        <Col :span="6">
          <Card>
            <Statistic
              :title="$t('asterisk.page.extensions.totalCalls')"
              :value="stats.summary.totalCalls"
            />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              :title="$t('asterisk.page.extensions.missRate')"
              :value="`${(stats.summary.missRate * 100).toFixed(1)}%`"
            />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              :title="$t('asterisk.page.extensions.avgPickup')"
              :value="`${stats.summary.avgPickupSeconds.toFixed(1)}s`"
            />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              :title="$t('asterisk.page.extensions.avgTalk')"
              :value="`${stats.summary.avgTalkSeconds.toFixed(1)}s`"
            />
          </Card>
        </Col>
      </Row>
    </Spin>

    <Tabs v-model:active-key="activeTab">
      <TabPane key="outbound" :tab="$t('asterisk.page.extensions.tabs.outbound')">
        <Spin :spinning="loading.outbound">
          <Table
            v-if="calls.outbound.length > 0"
            :columns="callColumns"
            :data-source="calls.outbound.map((c, i) => ({ ...c, key: c.linkedid || i }))"
            :pagination="{ pageSize: 20, showSizeChanger: false }"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'calldate'">{{ formatDateTime(record.calldate) }}</template>
              <template v-else-if="column.key === 'src'">{{ callerDisplay(record) }}</template>
              <template v-else-if="column.key === 'dst'">{{ destinationDisplay(record) }}</template>
              <template v-else-if="column.key === 'disposition'">
                <Tag :color="dispositionColor(record.disposition)">{{ dispositionLabel(record.disposition) }}</Tag>
              </template>
              <template v-else-if="column.key === 'duration'">{{ formatDuration(record.durationSeconds) }}</template>
              <template v-else-if="column.key === 'billsec'">{{ formatDuration(record.billsecSeconds) }}</template>
              <template v-else-if="column.key === 'pickup'">
                {{ record.pickupSeconds == null ? $t('asterisk.page.calls.noPickup') : `${record.pickupSeconds}s` }}
              </template>
            </template>
          </Table>
          <Empty v-else-if="loaded.outbound" :description="$t('asterisk.page.extensions.noCalls')" />
        </Spin>
      </TabPane>

      <TabPane key="inbound" :tab="$t('asterisk.page.extensions.tabs.inbound')">
        <Spin :spinning="loading.inbound">
          <Table
            v-if="calls.inbound.length > 0"
            :columns="callColumns"
            :data-source="calls.inbound.map((c, i) => ({ ...c, key: c.linkedid || i }))"
            :pagination="{ pageSize: 20, showSizeChanger: false }"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'calldate'">{{ formatDateTime(record.calldate) }}</template>
              <template v-else-if="column.key === 'src'">{{ callerDisplay(record) }}</template>
              <template v-else-if="column.key === 'dst'">{{ destinationDisplay(record) }}</template>
              <template v-else-if="column.key === 'disposition'">
                <Tag :color="dispositionColor(record.disposition)">{{ dispositionLabel(record.disposition) }}</Tag>
              </template>
              <template v-else-if="column.key === 'duration'">{{ formatDuration(record.durationSeconds) }}</template>
              <template v-else-if="column.key === 'billsec'">{{ formatDuration(record.billsecSeconds) }}</template>
              <template v-else-if="column.key === 'pickup'">
                {{ record.pickupSeconds == null ? $t('asterisk.page.calls.noPickup') : `${record.pickupSeconds}s` }}
              </template>
            </template>
          </Table>
          <Empty v-else-if="loaded.inbound" :description="$t('asterisk.page.extensions.noCalls')" />
        </Spin>
      </TabPane>

      <TabPane key="internal" :tab="$t('asterisk.page.extensions.tabs.internal')">
        <Spin :spinning="loading.internal">
          <Table
            v-if="calls.internal.length > 0"
            :columns="callColumns"
            :data-source="calls.internal.map((c, i) => ({ ...c, key: c.linkedid || i }))"
            :pagination="{ pageSize: 20, showSizeChanger: false }"
            size="small"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'calldate'">{{ formatDateTime(record.calldate) }}</template>
              <template v-else-if="column.key === 'src'">{{ callerDisplay(record) }}</template>
              <template v-else-if="column.key === 'dst'">{{ destinationDisplay(record) }}</template>
              <template v-else-if="column.key === 'disposition'">
                <Tag :color="dispositionColor(record.disposition)">{{ dispositionLabel(record.disposition) }}</Tag>
              </template>
              <template v-else-if="column.key === 'duration'">{{ formatDuration(record.durationSeconds) }}</template>
              <template v-else-if="column.key === 'billsec'">{{ formatDuration(record.billsecSeconds) }}</template>
              <template v-else-if="column.key === 'pickup'">
                {{ record.pickupSeconds == null ? $t('asterisk.page.calls.noPickup') : `${record.pickupSeconds}s` }}
              </template>
            </template>
          </Table>
          <Empty v-else-if="loaded.internal" :description="$t('asterisk.page.extensions.noCalls')" />
        </Spin>
      </TabPane>

      <TabPane key="stats" :tab="$t('asterisk.page.extensions.tabs.stats')">
        <Spin :spinning="statsLoading">
          <Card v-if="stats" title="Calls per day" style="margin-bottom: 16px">
            <Table
              :columns="seriesColumns"
              :data-source="(stats.series ?? []).map((s, i) => ({ ...s, key: i, bucketStart: formatDate(s.bucketStart) }))"
              :pagination="false"
              size="small"
            />
          </Card>

          <Card v-if="stats" title="Hour of day">
            <Table
              :columns="hourColumns"
              :data-source="(stats.hourOfDay ?? []).map((s, i) => ({ ...s, key: i, hour: `${bucketHour(s.bucketStart)}:00` }))"
              :pagination="false"
              size="small"
            />
          </Card>
        </Spin>
      </TabPane>

      <TabPane key="registration" tab="Registration">
        <Spin :spinning="regLoading">
          <div v-if="regError" style="color: #FF4D4F; padding: 12px 0">{{ regError }}</div>

          <Row v-if="regStatus" :gutter="16" style="margin-bottom: 16px">
            <Col :span="8">
              <Card>
                <Statistic
                  title="Current state"
                  :value="regStatus.registered ? 'Registered' : 'Not registered'"
                  :value-style="{ color: regStatus.registered ? '#52C41A' : '#FF4D4F' }"
                />
                <div style="font-size: 12px; color: #888; margin-top: 4px">
                  {{ regStatusLabel(regStatus.status) }}
                </div>
              </Card>
            </Col>
            <Col :span="16">
              <Card>
                <Descriptions :column="1" size="small">
                  <DescriptionsItem label="Last contact">
                    {{ regStatus.lastEvent?.contactUri || '—' }}
                  </DescriptionsItem>
                  <DescriptionsItem label="User-Agent">
                    {{ regStatus.lastEvent?.userAgent || '—' }}
                  </DescriptionsItem>
                  <DescriptionsItem label="Via">
                    {{ regStatus.lastEvent?.viaAddress || '—' }}
                  </DescriptionsItem>
                  <DescriptionsItem label="Last event">
                    {{ regStatus.lastEvent ? formatDateTime(regStatus.lastEvent.eventTime) : '—' }}
                  </DescriptionsItem>
                </Descriptions>
              </Card>
            </Col>
          </Row>

          <div v-if="regLoaded && regEvents.length > 0">
            <div style="font-weight: 500; margin-bottom: 8px">Event log</div>
            <Table
              :columns="regColumns"
              :data-source="regEvents.map((e) => ({ ...e, key: e.id }))"
              :pagination="{ pageSize: 20, showSizeChanger: false }"
              size="small"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.key === 'eventTime'">
                  {{ formatDateTime(record.eventTime) }}
                </template>
                <template v-else-if="column.key === 'status'">
                  <Tag :color="regStatusColor(record.status)">
                    {{ regStatusLabel(record.status) }}
                  </Tag>
                </template>
              </template>
            </Table>
          </div>
          <Empty
            v-else-if="regLoaded && !regError"
            description="No registration events captured in this period."
          />
        </Spin>
      </TabPane>
    </Tabs>
  </Drawer>
</template>
