import { asteriskApi, type RequestOptions } from './client';

// ---- shared enums (string form, matching gRPC-gateway JSON marshalling) ----

export type Disposition =
  | 'DISPOSITION_UNSPECIFIED'
  | 'DISPOSITION_ANSWERED'
  | 'DISPOSITION_NO_ANSWER'
  | 'DISPOSITION_BUSY'
  | 'DISPOSITION_FAILED';

export type TimeBucket =
  | 'TIME_BUCKET_UNSPECIFIED'
  | 'TIME_BUCKET_HOUR'
  | 'TIME_BUCKET_DAY'
  | 'TIME_BUCKET_WEEK';

// ---- proto types ----

export interface Call {
  linkedid: string;
  calldate: string;
  src: string;
  clid: string;
  cnum: string;
  cnam: string;
  dst: string;
  direction: string;
  disposition: Disposition;
  durationSeconds: number;
  billsecSeconds: number;
  pickupSeconds?: number;
  answeredExtension?: string;
  originatingExtension?: string;
  did: string;
  legCount: number;
  recordingFile: string;
}

// Inbound/outbound numeric fields are populated by ExtensionStat too.
// They are derived from direction × role: inbound = answered an inbound
// call; outbound = originated an outbound call. Internal calls don't
// contribute to either bucket.

export interface CallLeg {
  uniqueid: string;
  calldate: string;
  channel: string;
  dstchannel: string;
  src: string;
  dst: string;
  lastapp: string;
  lastdata: string;
  disposition: Disposition;
  durationSeconds: number;
  billsecSeconds: number;
  extension?: string;
  recordingFile: string;
}

export interface CelEvent {
  eventTime: string;
  eventtype: string;
  channame: string;
  uniqueid: string;
  appname: string;
  appdata: string;
  cidName: string;
  cidNum: string;
  exten: string;
  context: string;
}

export interface TimeBucketCount {
  bucketStart: string;
  total: number;
  answered: number;
  missed: number;
}

export interface ExtensionStat {
  extension: string;
  displayName: string;
  totalCalls: number;
  answeredCalls: number;
  missedCalls: number;
  inboundCalls: number;
  outboundCalls: number;
  totalTalkSeconds: number;
  handledShare: number;
  missRate: number;
  avgPickupSeconds: number;
  avgTalkSeconds: number;
  busiestHour: number;
}

// ---- request/response shapes ----

export interface ListCallsResponse {
  calls: Call[];
  total: number;
}

export interface GetCallResponse {
  summary: Call;
  legs: CallLeg[];
  timeline: CelEvent[];
}

export interface OverviewResponse {
  totalCalls: number;
  answeredCalls: number;
  missedCalls: number;
  busyCalls: number;
  failedCalls: number;
  answerRate: number;
  avgPickupSeconds: number;
  avgTalkSeconds: number;
  series: TimeBucketCount[];
}

export interface ListExtensionStatsResponse {
  extensions: ExtensionStat[];
  total: number;
}

export interface GetExtensionStatsResponse {
  summary: ExtensionStat;
  series: TimeBucketCount[];
  hourOfDay: TimeBucketCount[];
}

export interface MissedRingGroupCall {
  linkedid: string;
  calldate: string;
  src: string;
  clid: string;
  did: string;
  disposition: Disposition;
  ringSeconds: number;
}

export interface RingGroupStatsResponse {
  ringGroup: string;
  total: number;
  answered: number;
  noAnswer: number;
  allBusy: number;
  failed: number;
  missedCalls: MissedRingGroupCall[];
}

export type RegStatus =
  | 'REG_STATUS_UNSPECIFIED'
  | 'REG_STATUS_UNKNOWN'
  | 'REG_STATUS_CREATED'
  | 'REG_STATUS_UPDATED'
  | 'REG_STATUS_REACHABLE'
  | 'REG_STATUS_UNREACHABLE'
  | 'REG_STATUS_REMOVED'
  | 'REG_STATUS_UNQUALIFIED';

export interface RegistrationEvent {
  id: number;
  eventTime: string;
  endpoint: string;
  aor: string;
  contactUri: string;
  status: RegStatus;
  userAgent: string;
  viaAddress: string;
  regExpire?: string;
  rttUsec: number;
}

export interface GetRegistrationStatusResponse {
  extension: string;
  registered: boolean;
  status: RegStatus;
  lastEvent?: RegistrationEvent;
}

export interface ListRegistrationEventsResponse {
  events: RegistrationEvent[];
  total: number;
}

export interface RegisteredEndpoint {
  endpoint: string;
  contactUri: string;
  userAgent: string;
  viaAddress: string;
  status: RegStatus;
  lastEventTime: string;
  regExpire?: string;
}

export interface ListRegisteredAtResponse {
  at: string;
  endpoints: RegisteredEndpoint[];
}

// ---- query helpers ----

function buildQuery(params: Record<string, unknown>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue;
    q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}

// ---- service objects ----

export type CallDirection = 'inbound' | 'outbound' | 'internal';

export const CdrService = {
  listCalls: (
    params: {
      from: string;
      to: string;
      src?: string;
      dst?: string;
      extension?: string;
      disposition?: Disposition;
      direction?: CallDirection;
      page?: number;
      pageSize?: number;
    },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<ListCallsResponse>(
      `/calls${buildQuery(params)}`,
      options,
    ),

  getCall: (linkedid: string, options?: RequestOptions) =>
    asteriskApi.get<GetCallResponse>(
      `/calls/${encodeURIComponent(linkedid)}`,
      options,
    ),
};

export const StatsService = {
  overview: (
    params: { from: string; to: string; bucket?: TimeBucket },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<OverviewResponse>(
      `/stats/overview${buildQuery(params)}`,
      options,
    ),

  listExtensions: (
    params: {
      from: string;
      to: string;
      extension?: string;
      page?: number;
      pageSize?: number;
    },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<ListExtensionStatsResponse>(
      `/stats/extensions${buildQuery(params)}`,
      options,
    ),

  getExtension: (
    extension: string,
    params: { from: string; to: string; bucket?: TimeBucket },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<GetExtensionStatsResponse>(
      `/stats/extensions/${encodeURIComponent(extension)}${buildQuery(params)}`,
      options,
    ),

  ringGroup: (
    ringGroup: string,
    params: { from: string; to: string },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<RingGroupStatsResponse>(
      `/stats/ringgroups/${encodeURIComponent(ringGroup)}${buildQuery(params)}`,
      options,
    ),
};

export const RegistrationService = {
  getStatus: (
    extension: string,
    params: { at?: string } = {},
    options?: RequestOptions,
  ) =>
    asteriskApi.get<GetRegistrationStatusResponse>(
      `/registration/status/${encodeURIComponent(extension)}${buildQuery(params)}`,
      options,
    ),

  listEvents: (
    params: {
      from: string;
      to: string;
      extension?: string;
      page?: number;
      pageSize?: number;
    },
    options?: RequestOptions,
  ) =>
    asteriskApi.get<ListRegistrationEventsResponse>(
      `/registration/events${buildQuery(params)}`,
      options,
    ),

  registeredAt: (
    params: { at?: string } = {},
    options?: RequestOptions,
  ) =>
    asteriskApi.get<ListRegisteredAtResponse>(
      `/registration/online${buildQuery(params)}`,
      options,
    ),
};
