<script lang="ts" setup>
import { ref, watch } from 'vue';

import { useVbenDrawer } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import { Card, Descriptions, DescriptionsItem, Tag, Table, Tabs, TabPane, Spin, Empty } from 'ant-design-vue';

import { useAsteriskCdrStore } from '../../stores/asterisk-cdr.state';
import { useAsteriskRegistrationStore } from '../../stores/asterisk-registration.state';
import type {
  Call,
  CallLeg,
  CelEvent,
  Disposition,
  RegStatus,
  RegisteredEndpoint,
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
  { title: 'channel', dataIndex: 'channel', key: 'channel', width: 220 },
  { title: 'dstchannel', dataIndex: 'dstchannel', key: 'dstchannel', width: 220 },
  { title: 'extension', dataIndex: 'extension', key: 'extension', width: 100 },
  { title: 'disposition', dataIndex: 'disposition', key: 'disposition', width: 130 },
  { title: 'duration', dataIndex: 'durationSeconds', key: 'durationSeconds', width: 90 },
  { title: 'billsec', dataIndex: 'billsecSeconds', key: 'billsecSeconds', width: 90 },
];

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
          <Table
            :columns="legColumns"
            :data-source="legs"
            :pagination="false"
            row-key="uniqueid"
            size="small"
          />
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
