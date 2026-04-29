import { defineStore } from 'pinia';

import {
  RegistrationService,
  type GetRegistrationStatusResponse,
  type ListRegisteredAtResponse,
  type ListRegistrationEventsResponse,
} from '../api/services';

export const useAsteriskRegistrationStore = defineStore('asterisk-registration', () => {
  async function getStatus(
    extension: string,
    at?: string,
  ): Promise<GetRegistrationStatusResponse> {
    return await RegistrationService.getStatus(extension, at ? { at } : {});
  }

  async function listEvents(params: {
    from: string;
    to: string;
    extension?: string;
    page?: number;
    pageSize?: number;
  }): Promise<ListRegistrationEventsResponse> {
    return await RegistrationService.listEvents(params);
  }

  async function registeredAt(at?: string): Promise<ListRegisteredAtResponse> {
    return await RegistrationService.registeredAt(at ? { at } : {});
  }

  function $reset() {}

  return { $reset, getStatus, listEvents, registeredAt };
});
