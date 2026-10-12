import { fromWallClock, toWallClock, pbxTimezone } from '@/components/timezone'
import type { RegistrationSegment, RegistrationTimeline } from '@/api/types'
// A registration timeline cut into calendar days of the PBX timezone, one bar
// per day. Offsets are minutes from that day's midnight, so a bar is drawn on
// a 0..length scale (length is 1440, or 1380/1500 on a DST change day). The
// part of a day after the timeline's end (the future) has no piece.
export interface DayPiece {state:RegistrationSegment['state'];x:number;width:number;start:string;end:string;contact?:string|undefined;userAgent?:string|undefined;viaAddress?:string|undefined}
export interface DayTotals {online:number;offline:number;unknown:number}
export interface DayBar {day:string;start:string;length:number;pieces:DayPiece[];totals:DayTotals}
const MIN=60_000
const nextDay=(day:string)=>new Date(Date.parse(day+'T00:00:00Z')+86_400_000).toISOString().slice(0,10)
export function dayBars(t:Pick<RegistrationTimeline,'from'|'to'|'segments'>,tz=pbxTimezone.value):DayBar[]{
 const from=Date.parse(t.from),to=Date.parse(t.to);if(!(to>from))return []
 const bars:DayBar[]=[]
 for(let day=toWallClock(from,tz).slice(0,10);;day=nextDay(day)){
  const start=Date.parse(fromWallClock(day+'T00:00',tz)),end=Date.parse(fromWallClock(nextDay(day)+'T00:00',tz))
  if(start>=to)break
  const bar:DayBar={day,start:new Date(start).toISOString(),length:(end-start)/MIN,pieces:[],totals:{online:0,offline:0,unknown:0}}
  for(const s of t.segments){
   const a=Math.max(Date.parse(s.start),start,from),b=Math.min(Date.parse(s.end),end,to);if(!(b>a))continue
   bar.pieces.push({state:s.state,x:(a-start)/MIN,width:(b-a)/MIN,start:new Date(a).toISOString(),end:new Date(b).toISOString(),contact:s.contact,userAgent:s.userAgent,viaAddress:s.viaAddress})
   bar.totals[s.state]+=(b-a)/1000
  }
  bars.push(bar)
 }
 return bars
}
// "1 h 54 min", "35 min", "40 s"; the summary and day labels use it.
export function formatDuration(seconds:number):string{
 const s=Math.round(seconds);if(s<60)return s+' s'
 const m=Math.round(s/60),h=Math.floor(m/60);return h?(m%60?`${h} h ${m%60} min`:`${h} h`):m+' min'
}
