<script lang="ts" setup>
import { onMounted, ref } from 'vue';

import { Page, useVbenDrawer } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import CallDrawer from '../calls/call-drawer.vue';

import {
  Button,
  Card,
  Col,
  DatePicker,
  Empty,
  Form,
  FormItem,
  Input,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
} from 'ant-design-vue';

import { useAsteriskStatsStore } from '../../stores/asterisk-stats.state';
import type {
  Disposition,
  OverviewResponse,
  RingGroupStatsResponse,
  TimeBucket,
} from '../../api/services';
import { formatDate, formatDateTime } from '../../utils/datetime';

const statsStore = useAsteriskStatsStore();

// `value-format` must be a Day.js format token. The earlier 'iso' shortcut
// is not recognised, so the picker silently reset to "now" whenever a user
// chose a date. The format below has no milliseconds — initial values must
// match it byte-for-byte or the picker desyncs from v-model and the
// "From" filter silently sticks at the default.
const PICKER_FORMAT = 'YYYY-MM-DDTHH:mm:ss[Z]';

// Strip milliseconds so the string matches PICKER_FORMAT exactly.
function toPickerFormat(d: Date): string {
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

function defaultFromIso(): string {
  const d = new Date();
  d.setDate(d.getDate() - 30);
  d.setHours(0, 0, 0, 0);
  return toPickerFormat(d);
}

function defaultToIso(): string {
  return toPickerFormat(new Date(Date.now() + 60_000));
}

const filters = ref({
  from: defaultFromIso(),
  to: defaultToIso(),
  bucket: 'TIME_BUCKET_DAY' as TimeBucket,
  ringGroup: '600',
});

const data = ref<OverviewResponse | null>(null);
const loading = ref(false);

const ringGroupData = ref<RingGroupStatsResponse | null>(null);
const ringGroupLoading = ref(false);

const bucketOptions = [
  { value: 'TIME_BUCKET_HOUR', label: 'Hour' },
  { value: 'TIME_BUCKET_DAY', label: 'Day' },
  { value: 'TIME_BUCKET_WEEK', label: 'Week' },
];

async function load() {
  loading.value = true;
  try {
    data.value = await statsStore.overview({
      from: filters.value.from,
      to: filters.value.to,
      bucket: filters.value.bucket,
    });
  } finally {
    loading.value = false;
  }
  loadRingGroup();
}

async function loadRingGroup() {
  if (!filters.value.ringGroup) {
    ringGroupData.value = null;
    return;
  }
  ringGroupLoading.value = true;
  try {
    ringGroupData.value = await statsStore.ringGroup(filters.value.ringGroup, {
      from: filters.value.from,
      to: filters.value.to,
    });
  } finally {
    ringGroupLoading.value = false;
  }
}

onMounted(load);

function dispositionColor(d: Disposition): string {
  switch (d) {
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

const missedColumns = [
  { title: $t('asterisk.page.calls.calldate'), key: 'calldate', dataIndex: 'calldate', width: 170 },
  { title: $t('asterisk.page.calls.src'), key: 'src', dataIndex: 'src', width: 160 },
  { title: 'DID', key: 'did', dataIndex: 'did', width: 140 },
  { title: $t('asterisk.page.calls.disposition'), key: 'disposition', width: 130 },
  { title: 'Ring (s)', key: 'ringSeconds', dataIndex: 'ringSeconds', width: 90 },
];

const [CallDetailDrawer, callDrawerApi] = useVbenDrawer({ connectedComponent: CallDrawer });

function openMissedCall(linkedid: string) {
  if (!linkedid) return;
  callDrawerApi.setData({ linkedid });
  callDrawerApi.open();
}

const seriesColumns = [
  { title: 'Bucket', dataIndex: 'bucketStart', key: 'bucketStart', width: 200 },
  { title: 'Total', dataIndex: 'total', key: 'total', width: 100 },
  { title: 'Answered', dataIndex: 'answered', key: 'answered', width: 100 },
  { title: 'Missed', dataIndex: 'missed', key: 'missed', width: 100 },
];

// Hour granularity needs the time component; day and week only need the
// date — otherwise the user sees a misleading "00:00:00" and reads the
// row as "calls at midnight" rather than "calls on this day/week".
function formatBucket(iso: string): string {
  return filters.value.bucket === 'TIME_BUCKET_HOUR'
    ? formatDateTime(iso)
    : formatDate(iso);
}

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

// Calls where no leg ever answered = nobody picked up the office. This is
// the headline metric for unanswered demand: outside hours, all agents
// busy, or just nobody answering.
function unansweredRate(d: OverviewResponse): string {
  if (!d.totalCalls) return '0% of all calls';
  return `${((d.missedCalls / d.totalCalls) * 100).toFixed(1)}% of all calls`;
}

function secs(v: number): string {
  return `${v.toFixed(1)}s`;
}
</script>

<template>
  <Page :auto-content-height="true" :title="$t('asterisk.page.overview.title')">
    <Card style="margin-bottom: 16px">
      <Form layout="inline">
        <FormItem :label="$t('asterisk.page.calls.from')">
          <DatePicker v-model:value="filters.from" show-time :value-format="PICKER_FORMAT" />
        </FormItem>
        <FormItem :label="$t('asterisk.page.calls.to')">
          <DatePicker v-model:value="filters.to" show-time :value-format="PICKER_FORMAT" />
        </FormItem>
        <FormItem label="Bucket">
          <Select v-model:value="filters.bucket" :options="bucketOptions" style="width: 120px" />
        </FormItem>
        <FormItem label="Ringgroup">
          <Input v-model:value="filters.ringGroup" placeholder="600" style="width: 100px" />
        </FormItem>
        <FormItem>
          <Button type="primary" :loading="loading" @click="load">Apply</Button>
        </FormItem>
      </Form>
    </Card>

    <Row :gutter="16" style="margin-bottom: 16px">
      <Col :span="6">
        <Card>
          <Statistic :title="$t('asterisk.page.overview.totalCalls')" :value="data?.totalCalls ?? 0" />
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic
            :title="$t('asterisk.page.overview.answeredCalls')"
            :value="data?.answeredCalls ?? 0"
            :value-style="{ color: '#52C41A' }"
          />
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic
            :title="$t('asterisk.page.overview.missedCalls')"
            :value="data?.missedCalls ?? 0"
            :value-style="{ color: '#FA8C16' }"
          />
          <div style="font-size: 12px; color: #888; margin-top: 4px">
            {{ data ? unansweredRate(data) : '' }}
          </div>
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic
            :title="$t('asterisk.page.overview.answerRate')"
            :value="data ? pct(data.answerRate) : '—'"
          />
        </Card>
      </Col>
    </Row>

    <Row :gutter="16" style="margin-bottom: 16px">
      <Col :span="6">
        <Card>
          <Statistic :title="$t('asterisk.page.overview.busyCalls')" :value="data?.busyCalls ?? 0" />
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic :title="$t('asterisk.page.overview.failedCalls')" :value="data?.failedCalls ?? 0" />
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic
            :title="$t('asterisk.page.overview.avgPickup')"
            :value="data ? secs(data.avgPickupSeconds) : '—'"
          />
        </Card>
      </Col>
      <Col :span="6">
        <Card>
          <Statistic
            :title="$t('asterisk.page.overview.avgTalk')"
            :value="data ? secs(data.avgTalkSeconds) : '—'"
          />
        </Card>
      </Col>
    </Row>

    <Card :title="$t('asterisk.page.overview.series')" style="margin-bottom: 16px">
      <Table
        :columns="seriesColumns"
        :data-source="(data?.series ?? []).map((s, i) => ({ ...s, key: i, bucketStart: formatBucket(s.bucketStart) }))"
        :pagination="{ pageSize: 50 }"
        size="small"
      />
    </Card>

    <Card
      v-if="filters.ringGroup"
      :title="`Ringgroup ${filters.ringGroup} — inbound traffic`"
      :loading="ringGroupLoading"
    >
      <Row :gutter="16" style="margin-bottom: 16px">
        <Col :span="6">
          <Card>
            <Statistic title="Total inbound" :value="ringGroupData?.total ?? 0" />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              title="Answered"
              :value="ringGroupData?.answered ?? 0"
              :value-style="{ color: '#52C41A' }"
            />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              title="Nobody answered"
              :value="ringGroupData?.noAnswer ?? 0"
              :value-style="{ color: '#FA8C16' }"
            />
          </Card>
        </Col>
        <Col :span="6">
          <Card>
            <Statistic
              title="All operators busy"
              :value="ringGroupData?.allBusy ?? 0"
              :value-style="{ color: '#1890FF' }"
            />
          </Card>
        </Col>
      </Row>

      <div v-if="ringGroupData && ringGroupData.missedCalls.length > 0">
        <div style="font-weight: 500; margin-bottom: 8px">Missed calls</div>
        <Table
          :columns="missedColumns"
          :data-source="ringGroupData.missedCalls.map((c, i) => ({ ...c, key: c.linkedid || i }))"
          :pagination="{ pageSize: 20, showSizeChanger: false }"
          :row-class-name="() => 'missed-row'"
          :custom-row="(record: any) => ({ onClick: () => openMissedCall(record.linkedid) })"
          size="small"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'calldate'">{{ formatDateTime(record.calldate) }}</template>
            <template v-else-if="column.key === 'src'">
              {{ record.src }}<span v-if="record.clid && record.clid !== record.src"> ({{ record.clid }})</span>
            </template>
            <template v-else-if="column.key === 'disposition'">
              <Tag :color="dispositionColor(record.disposition)">{{ dispositionLabel(record.disposition) }}</Tag>
            </template>
          </template>
        </Table>
      </div>
      <Empty
        v-else-if="ringGroupData"
        description="No missed inbound calls in this period."
      />
    </Card>

    <CallDetailDrawer />
  </Page>
</template>

<style scoped>
.missed-row {
  cursor: pointer;
}
</style>
