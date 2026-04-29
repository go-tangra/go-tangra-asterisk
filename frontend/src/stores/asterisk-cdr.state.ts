import { defineStore } from 'pinia';

import {
  CdrService,
  type CallDirection,
  type Disposition,
  type GetCallResponse,
  type ListCallsResponse,
} from '../api/services';

export const useAsteriskCdrStore = defineStore('asterisk-cdr', () => {
  async function listCalls(params: {
    from: string;
    to: string;
    src?: string;
    dst?: string;
    extension?: string;
    disposition?: Disposition;
    direction?: CallDirection;
    page?: number;
    pageSize?: number;
  }): Promise<ListCallsResponse> {
    return await CdrService.listCalls(params);
  }

  async function getCall(linkedid: string): Promise<GetCallResponse> {
    return await CdrService.getCall(linkedid);
  }

  function $reset() {}

  return { $reset, listCalls, getCall };
});
