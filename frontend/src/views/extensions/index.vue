<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideEye } from 'shell/vben/icons';

import { Space, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { $t } from 'shell/locales';

import { useAsteriskStatsStore } from '../../stores/asterisk-stats.state';
import type { ExtensionStat } from '../../api/services';

import ExtensionDrawer from './extension-drawer.vue';

const statsStore = useAsteriskStatsStore();

// Real ISO 8601 with the user's timezone offset (e.g.
// 2026-04-30T08:00:00+03:00). Previous format ended in '[Z]' (literal
// Z, NOT a UTC conversion) — the picker output local time labelled as
// UTC, so the server queried a window 3 hours off in Sofia and missed
// today's calls.
const PICKER_FORMAT = 'YYYY-MM-DDTHH:mm:ssZ';

// Format a Date as ISO 8601 with the LOCAL timezone offset. Must match
// PICKER_FORMAT byte-for-byte — if v-model and valueFormat disagree,
// the picker silently resets to "now" on every change.
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

// Default window: last 24 hours up to "now". Operators land on the page
// expecting today's activity, not a 30-day backlog.
function defaultFromIso(): string {
  return toLocalIso(new Date(Date.now() - 24 * 60 * 60 * 1000));
}
function defaultToIso(): string {
  return toLocalIso(new Date());
}

// Render seconds as a compact "Hh Mm" / "Mm Ss" string. Operators care
// about hours and minutes for workload — the raw seconds count from the
// API is too noisy.
function formatTalkTime(seconds: number): string {
  if (!seconds || seconds <= 0) return '0m';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return `${m}m`;
  return `${seconds}s`;
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
      fieldName: 'extension',
      label: $t('asterisk.page.extensions.extension'),
      componentProps: { allowClear: true },
    },
  ],
};

const gridOptions: VxeGridProps<ExtensionStat> = {
  height: 'auto',
  stripe: false,
  toolbarConfig: { custom: true, export: true, refresh: true, zoom: true },
  rowConfig: { isHover: true },
  pagerConfig: {
    enabled: true,
    pageSize: 50,
    pageSizes: [20, 50, 100, 200],
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const resp = await statsStore.listExtensions({
          from: formValues?.from || defaultFromIso(),
          to: formValues?.to || defaultToIso(),
          extension: formValues?.extension,
          page: (page.currentPage ?? 1) - 1,
          pageSize: page.pageSize,
        });
        return {
          items: resp.extensions ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },
  columns: [
    { title: $t('ui.table.seq'), type: 'seq', width: 50 },
    { title: $t('asterisk.page.extensions.extension'), field: 'extension', width: 110 },
    { title: $t('asterisk.page.extensions.displayName'), field: 'displayName', width: 200 },
    { title: $t('asterisk.page.extensions.totalCalls'), field: 'totalCalls', width: 90 },
    { title: $t('asterisk.page.extensions.inboundCalls'), field: 'inboundCalls', width: 90 },
    { title: $t('asterisk.page.extensions.outboundCalls'), field: 'outboundCalls', width: 100 },
    { title: $t('asterisk.page.extensions.answeredCalls'), field: 'answeredCalls', width: 100 },
    { title: $t('asterisk.page.extensions.missedCalls'), field: 'missedCalls', width: 90 },
    {
      title: $t('asterisk.page.extensions.missRate'),
      field: 'missRate',
      width: 130,
      formatter: ({ cellValue }) => `${(Number(cellValue) * 100).toFixed(1)}%`,
    },
    {
      title: $t('asterisk.page.extensions.totalTalk'),
      field: 'totalTalkSeconds',
      width: 130,
      formatter: ({ cellValue }) => formatTalkTime(Number(cellValue) || 0),
    },
    {
      title: $t('asterisk.page.extensions.handledShare'),
      field: 'handledShare',
      width: 120,
      formatter: ({ cellValue }) => `${(Number(cellValue) * 100).toFixed(1)}%`,
    },
    {
      title: $t('asterisk.page.extensions.avgPickup'),
      field: 'avgPickupSeconds',
      width: 110,
      formatter: ({ cellValue }) => `${Number(cellValue).toFixed(1)}s`,
    },
    {
      title: $t('asterisk.page.extensions.avgTalk'),
      field: 'avgTalkSeconds',
      width: 110,
      formatter: ({ cellValue }) => `${Number(cellValue).toFixed(1)}s`,
    },
    {
      title: $t('asterisk.page.extensions.busiestHour'),
      field: 'busiestHour',
      width: 110,
      formatter: ({ cellValue }) => `${cellValue}:00`,
    },
    {
      title: '',
      field: 'actions',
      width: 90,
      fixed: 'right',
      slots: { default: 'actions' },
    },
  ],
  columnConfig: { resizable: true },
};

const [Drawer, drawerApi] = useVbenDrawer({ connectedComponent: ExtensionDrawer });
const [Grid, gridApi] = useVbenVxeGrid({ formOptions, gridOptions });

async function viewExtension(row: ExtensionStat) {
  // Use the current form's from/to so the popup matches what the user is
  // looking at in the grid, not arbitrary defaults.
  const values = (await gridApi.formApi?.getValues?.()) ?? {};
  drawerApi.setData({
    extension: row.extension,
    from: values.from || defaultFromIso(),
    to: values.to || defaultToIso(),
  });
  drawerApi.open();
}
</script>

<template>
  <Page :auto-content-height="true" :title="$t('asterisk.page.extensions.title')">
    <Grid>
      <template #actions="{ row }">
        <Space>
          <Button type="link" size="small" @click="viewExtension(row)">
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
