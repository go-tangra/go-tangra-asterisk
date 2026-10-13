import type { MetricSeries } from '@/api/types'
import { formatDuration } from '@/components/day-bars'
import { pbxTimezone } from '@/components/timezone'
// Geometry and labels for the Monitoring chart. Points sit on a real time axis
// from the period start to its end (a series that began an hour ago fills only
// the last hour), and a line breaks where samples are missing for more than
// two steps. The chart is drawn on a W x H box; the y scale starts at zero.
export const W=1000,H=200
// Labels every series carries (the binding and the scrape) say nothing to a reader.
const HIDDEN=new Set(['__name__','instance','job','pbx_id','tenant_id'])
export type Unit='count'|'ms'|'seconds'|'percent'|'mos'|'flag'
export function seriesName(labels:Record<string,string>,fallback:string):string{
 const parts=Object.entries(labels).filter(([k])=>!HIDDEN.has(k)).sort(([a],[b])=>a.localeCompare(b)).map(([k,v])=>k+' '+v)
 return parts.join(' · ')||fallback
}
export const seriesKey=(labels:Record<string,string>)=>JSON.stringify(Object.entries(labels).sort(([a],[b])=>a.localeCompare(b)))
export function formatValue(v:number|null|undefined,unit:Unit):string{
 if(v===null||v===undefined||!Number.isFinite(v))return '—'
 switch(unit){
  case 'ms':return (v>=100?Math.round(v):Math.round(v*10)/10)+' ms'
  case 'seconds':return formatDuration(v)
  case 'percent':return Math.round(v*10)/10+' %'
  case 'mos':return v.toFixed(2)
  case 'flag':return v>=1?'yes':'no'
  default:return Number.isInteger(v)?String(v):String(Math.round(v*100)/100)
 }
}
export interface Line {key:string;name:string;paths:string[];latest:number|null}
export interface Chart {lines:Line[];hidden:number;max:number;yTicks:{y:number;label:string}[];xTicks:{x:number;label:string}[]}
const short=new Map<string,Intl.DateTimeFormat>()
function tick(ms:number,span:number,tz:string):string{
 const day=span>36*3600_000,key=tz+day;let f=short.get(key)
 if(!f){f=new Intl.DateTimeFormat(undefined,{timeZone:tz,...(day?{month:'2-digit',day:'2-digit'}:{}),hour:'2-digit',minute:'2-digit'});short.set(key,f)}
 return f.format(new Date(ms))
}
// A nice upper bound for the y axis: 1, 2 or 5 times a power of ten.
function ceiling(max:number):number{if(max<=0)return 1;const p=10**Math.floor(Math.log10(max));for(const m of [1,2,5,10])if(m*p>=max)return m*p;return 10*p}
export function buildChart(series:MetricSeries[],from:string,to:string,stepSeconds:number,unit:Unit,fallback:string,limit=8,tz=pbxTimezone.value):Chart{
 const x0=Date.parse(from),x1=Date.parse(to),span=Math.max(1,x1-x0),gap=2*stepSeconds*1000
 // Nothing asked yet (or an invalid period): no chart rather than invalid dates.
 if(!series.length||!Number.isFinite(x0)||!Number.isFinite(x1))return {lines:[],hidden:0,max:1,yTicks:[],xTicks:[]}
 const latest=(s:MetricSeries)=>{for(let i=s.samples.length-1;i>=0;i--){const p=s.samples[i];if(p?.hasValue&&p.value!==null)return p.value}return null}
 const ordered=[...series].sort((a,b)=>(latest(b)??-Infinity)-(latest(a)??-Infinity)||seriesName(a.labels,fallback).localeCompare(seriesName(b.labels,fallback)))
 const shown=ordered.slice(0,limit)
 const values=shown.flatMap(s=>s.samples.filter(p=>p.hasValue&&p.value!==null).map(p=>p.value as number))
 const max=unit==='flag'?1:ceiling(Math.max(0,...values))
 const lines=shown.map(s=>{
  const paths:string[]=[];let current:string[]=[],last=-Infinity
  for(const p of s.samples){
   const t=Date.parse(p.time)
   if(!p.hasValue||p.value===null||t-last>gap){if(current.length)paths.push(current.join(' '));current=[]}
   if(p.hasValue&&p.value!==null){current.push(`${((t-x0)/span*W).toFixed(1)},${(H-Math.max(0,p.value)/max*H).toFixed(1)}`);last=t}
  }
  if(current.length)paths.push(current.join(' '))
  return {key:seriesKey(s.labels),name:seriesName(s.labels,fallback),paths,latest:latest(s)}
 })
 const yTicks=[max,max/2,0].map(v=>({y:H-v/max*H,label:formatValue(v,unit==='flag'?'count':unit)}))
 const xTicks=[0,0.25,0.5,0.75,1].map(f=>({x:f*W,label:tick(x0+f*span,span,tz)}))
 return {lines,hidden:Math.max(0,series.length-limit),max,yTicks,xTicks}
}
