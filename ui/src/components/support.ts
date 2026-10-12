import type { SupportSpread } from '@/api/types'
import { formatDuration } from './day-bars'
// Display helpers shared by the support pages.
export const percent=(part:number,whole:number)=>whole>0?Math.round(100*part/whole)+'%':'–'
export const seconds=(s:number|null|undefined)=>s==null?'–':formatDuration(s)
// "4 s · p90 12 s" for a spread; "–" without samples.
export const spreadText=(s:SupportSpread)=>s.median==null?'–':`${seconds(s.median)} · p90 ${seconds(s.p90)}`
export const LOST_REASON:Record<string,string>={'no-agent-available':'No agent available','caller-hung-up-while-ringing':'Caller hung up while ringing','rang-out':'Rang out'}
export const lostText=(outcome:string,reason:string)=>outcome==='abandoned-before-group'?'Hung up during welcome':LOST_REASON[reason]??reason
