import type { components } from './schema.d'
export type Call = components['schemas']['Call'] & {extensions?:string[]}
export interface Page<T> {items:T[];total:number;page:number;page_size:number;sort:string;order:'asc'|'desc'}
export interface Capability {available:boolean;fresh:boolean}
export type Capabilities = Record<string,Capability>
export interface QoS {rxJitterMs:number|null;txJitterMs:number|null;rttMs:number|null;rxLossPercent:number|null;txLossPercent:number|null;rxMos:number|null;txMos:number|null;quality:string}
export interface Leg {uniqueid:string;start:string;channel:string;dstChannel:string;src:string;dst:string;disposition:string;durationSeconds:number;talkSeconds:number;localQuality?:QoS;peerQuality?:QoS}
export interface CallDetail {summary:Call;legs:Leg[];timeline:{time:string;type:string;channel:string;uniqueid:string}[];celAvailable:boolean;qualityAvailable:boolean;registration?:{statuses:RegistrationStatus[];gaps:Gap[]}}
export interface Bucket extends Record<string, unknown> {start:string;total:number;answered:number;missed:number}
export interface Overview {timezone:string;total:number;answered:number;missed:number;meanTalkSeconds:number;meanPickupSeconds:number|null;series:Bucket[]}
export interface Extension extends Record<string, unknown> {timezone:string;extension:string;name:string;total:number;inbound:number;outbound:number;answered:number;missed:number;talkSeconds:number;meanTalkSeconds:number;meanPickupSeconds:number|null;workloadShare:number;busiestHour:number;hourOfDay:number[];series:Bucket[]}
export interface Ringgroup {total:number;answered:number;noAnswer:number;allBusy:number;failed:number;missedCalls:Call[]}
export interface RegistrationEvent extends Record<string, unknown> {id:number;time:string;endpoint:string;contact:string;status:string;expire:string}
// Registration history: paged like Page<T>; total/page fields and the
// truncation flags are optional so older or capped responses still render.
export interface RegistrationHistory {items:RegistrationEvent[];total?:number;page?:number;page_size?:number;truncated?:boolean;has_more?:boolean}
export interface RegistrationStatus {extension:string;at:string;status:string;registered:boolean;certainty:string;lastEvent:RegistrationEvent|null;expiresAt:string|null}
export interface Gap {start:string;end:string}
export interface LiveCall {linkedid:string;channels:{uniqueid:string;channel:string;caller:string;connected:string;state:string;bridge:string}[]}
export interface LiveSnapshot {calls:LiveCall[];generation:number;fresh:boolean;updated:string}
export interface LiveUpdate {call?:LiveCall;linkedid?:string;generation:number;fresh:boolean}
export interface MetricSeries {labels:Record<string,string>;samples:{time:string;value:number|null;hasValue:boolean}[]}
