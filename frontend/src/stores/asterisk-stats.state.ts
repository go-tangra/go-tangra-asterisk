import { defineStore } from 'pinia';

import {
  StatsService,
  type GetExtensionStatsResponse,
  type ListExtensionStatsResponse,
  type OverviewResponse,
  type RingGroupStatsResponse,
  type TimeBucket,
} from '../api/services';

export const useAsteriskStatsStore = defineStore('asterisk-stats', () => {
  async function overview(params: {
    from: string;
    to: string;
    bucket?: TimeBucket;
  }): Promise<OverviewResponse> {
    return await StatsService.overview(params);
  }

  async function listExtensions(params: {
    from: string;
    to: string;
    extension?: string;
    page?: number;
    pageSize?: number;
  }): Promise<ListExtensionStatsResponse> {
    return await StatsService.listExtensions(params);
  }

  async function getExtension(
    extension: string,
    params: { from: string; to: string; bucket?: TimeBucket },
  ): Promise<GetExtensionStatsResponse> {
    return await StatsService.getExtension(extension, params);
  }

  async function ringGroup(
    ringGroupNumber: string,
    params: { from: string; to: string },
  ): Promise<RingGroupStatsResponse> {
    return await StatsService.ringGroup(ringGroupNumber, params);
  }

  function $reset() {}

  return { $reset, overview, listExtensions, getExtension, ringGroup };
});
