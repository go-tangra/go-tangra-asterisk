<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { computed } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideEye } from 'shell/vben/icons';

import { Space, Tag, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { $t } from 'shell/locales';

import { useAsteriskCdrStore } from '../../stores/asterisk-cdr.state';
import type { Call, Disposition } from '../../api/services';
import { formatDateTime } from '../../utils/datetime';

import CallDrawer from './call-drawer.vue';

const cdrStore = useAsteriskCdrStore();

const dispositionOptions = computed(() => [
  { value: 'DISPOSITION_ANSWERED', label: $t('asterisk.disposition.ANSWERED') },
  { value: 'DISPOSITION_NO_ANSWER', label: $t('asterisk.disposition.NO_ANSWER') },
  { value: 'DISPOSITION_BUSY', label: $t('asterisk.disposition.BUSY') },
  { value: 'DISPOSITION_FAILED', label: $t('asterisk.disposition.FAILED') },
]);

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

// `valueFormat` must be a Day.js format token; the earlier 'iso' shortcut
// was silently ignored, so the picker reset to "now" whenever a user chose
// a date.
// Real ISO 8601 with the user's timezone offset (e.g.
// 2026-04-30T00:00:00+03:00). The previous format was
// 'YYYY-MM-DDTHH:mm:ss[Z]' — but [Z] is a dayjs escape for a literal
// 'Z' character, NOT a timezone conversion. That made the picker
// output Sofia local time labelled as UTC, so the server queried a
// window 3 hours off the user's intent and returned nothing.
const PICKER_FORMAT = 'YYYY-MM-DDTHH:mm:ssZ';

// Format a Date as ISO 8601 with the LOCAL timezone offset
// (e.g. 2026-04-30T00:00:00+03:00). Must match PICKER_FORMAT byte-for-byte
// — if the picker's v-model value doesn't match valueFormat exactly the
// picker silently resets to "now" on every change.
function toLocalIso(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  const tzMin = -d.getTimezoneOffset();
  const sign = tzMin >= 0 ? '+' : '-';
  const abs = Math.abs(tzMin);
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}

function defaultFromIso(): string {
  const from = new Date();
  from.setDate(from.getDate() - 7);
  from.setHours(0, 0, 0, 0);
  return toLocalIso(from);
}

function defaultToIso(): string {
  return toLocalIso(new Date(Date.now() + 60_000));
}

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'DatePicker',
      fieldName: 'from',
      label: $t('asterisk.page.calls.from'),
      defaultValue: defaultFromIso(),
      componentProps: { showTime: true, valueFormat: PICKER_FORMAT, style: 'width: 100%' },
    },
    {
      component: 'DatePicker',
      fieldName: 'to',
      label: $t('asterisk.page.calls.to'),
      defaultValue: defaultToIso(),
      componentProps: { showTime: true, valueFormat: PICKER_FORMAT, style: 'width: 100%' },
    },
    {
      component: 'Input',
      fieldName: 'src',
      label: $t('asterisk.page.calls.src'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Input',
      fieldName: 'dst',
      label: $t('asterisk.page.calls.dst'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Input',
      fieldName: 'extension',
      label: $t('asterisk.page.calls.extension'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Select',
      fieldName: 'disposition',
      label: $t('asterisk.page.calls.disposition'),
      componentProps: {
        options: dispositionOptions,
        placeholder: $t('ui.placeholder.select'),
        allowClear: true,
      },
    },
  ],
};

const gridOptions: VxeGridProps<Call> = {
  height: 'auto',
  stripe: false,
  toolbarConfig: {
    custom: true,
    export: true,
    refresh: true,
    zoom: true,
  },
  rowConfig: { isHover: true },
  pagerConfig: {
    enabled: true,
    pageSize: 50,
    pageSizes: [20, 50, 100, 200],
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const resp = await cdrStore.listCalls({
          from: formValues?.from || defaultFromIso(),
          to: formValues?.to || defaultToIso(),
          src: formValues?.src,
          dst: formValues?.dst,
          extension: formValues?.extension,
          disposition: formValues?.disposition,
          page: (page.currentPage ?? 1) - 1,
          pageSize: page.pageSize,
        });
        return {
          items: resp.calls ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },
  columns: [
    { title: $t('ui.table.seq'), type: 'seq', width: 50 },
    {
      title: $t('asterisk.page.calls.calldate'),
      field: 'calldate',
      width: 170,
      formatter: ({ cellValue }) => formatDateTime(cellValue),
    },
    {
      title: $t('asterisk.page.calls.src'),
      field: 'src',
      width: 160,
      slots: { default: 'caller' },
    },
    {
      title: $t('asterisk.page.calls.dst'),
      field: 'dst',
      width: 160,
      slots: { default: 'destination' },
    },
    {
      title: $t('asterisk.page.calls.direction'),
      field: 'direction',
      width: 100,
    },
    {
      title: $t('asterisk.page.calls.disposition'),
      field: 'disposition',
      width: 130,
      slots: { default: 'disposition' },
    },
    {
      title: $t('asterisk.page.calls.duration'),
      field: 'durationSeconds',
      width: 100,
      formatter: ({ cellValue }) => formatDuration(cellValue),
    },
    {
      title: $t('asterisk.page.calls.billsec'),
      field: 'billsecSeconds',
      width: 100,
      formatter: ({ cellValue }) => formatDuration(cellValue),
    },
    {
      title: $t('asterisk.page.calls.pickup'),
      field: 'pickupSeconds',
      width: 100,
      formatter: ({ cellValue }) =>
        cellValue == null
          ? $t('asterisk.page.calls.noPickup')
          : `${cellValue}s`,
    },
    {
      title: $t('asterisk.page.calls.answeredExtension'),
      field: 'answeredExtension',
      width: 130,
    },
    {
      title: $t('asterisk.page.calls.legCount'),
      field: 'legCount',
      width: 80,
    },
    {
      title: '',
      field: 'actions',
      width: 90,
      fixed: 'right',
      slots: { default: 'actions' },
    },
  ],
  columnConfig: {
    resizable: true,
  },
};

const [Drawer, drawerApi] = useVbenDrawer({ connectedComponent: CallDrawer });
const [Grid, gridApi] = useVbenVxeGrid({ formOptions, gridOptions });

function viewCall(row: Call) {
  drawerApi.setData({ linkedid: row.linkedid });
  drawerApi.open();
}

function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return '0s';
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return m === 0 ? `${s}s` : `${m}m ${s}s`;
}

// callerDisplay returns the user-friendly originator of a call:
//  - inbound: external caller's number (src)
//  - outbound/internal: the originating extension (e.g. "40")
function callerDisplay(row: Call): string {
  if (row.direction === 'outbound' || row.direction === 'internal') {
    return row.originatingExtension || row.src;
  }
  return row.src;
}

// destinationDisplay returns the user-friendly destination:
//  - inbound: the DID the caller actually dialed (e.g. "35924392222")
//  - outbound/internal: dst is the dialed external/internal number
function destinationDisplay(row: Call): string {
  if (row.direction === 'inbound') {
    return row.did || row.dst;
  }
  return row.dst;
}
</script>

<template>
  <Page :auto-content-height="true" :title="$t('asterisk.page.calls.title')">
    <Grid>
      <template #caller="{ row }">
        {{ callerDisplay(row) }}
      </template>
      <template #destination="{ row }">
        {{ destinationDisplay(row) }}
      </template>
      <template #disposition="{ row }">
        <Tag :color="dispositionColor(row.disposition)">
          {{ dispositionLabel(row.disposition) }}
        </Tag>
      </template>
      <template #actions="{ row }">
        <Space>
          <Button type="link" size="small" @click="viewCall(row)">
            <template #icon>
              <LucideEye />
            </template>
          </Button>
        </Space>
      </template>
    </Grid>
    <Drawer />
  </Page>
</template>
