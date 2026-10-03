import { ref } from 'vue'
import { get } from '@/api/client'
// One rule for every time on screen: it is shown, and typed into period
// filters, in the PBX binding's timezone — never the browser's. The binding
// timezone is learnt from the API (capabilities "timezone" when the module
// exposes it, otherwise the "timezone" field of stats responses); until then
// the binding default applies.
export const FALLBACK_TIMEZONE='Europe/Sofia'
export const pbxTimezone=ref(FALLBACK_TIMEZONE)
const valid=(tz:string)=>{try{new Intl.DateTimeFormat('en-US',{timeZone:tz});return true}catch{return false}}
export function learnTimezone(tz:unknown){if(typeof tz==='string'&&tz!==''&&valid(tz))pbxTimezone.value=tz}
let pending:Promise<void>|undefined
export function loadTimezone():Promise<void>{pending??=get<Record<string,unknown>>('/api/asterisk/capabilities').then(caps=>learnTimezone(caps.timezone),()=>{pending=undefined});return pending}
const display=new Map<string,Intl.DateTimeFormat>()
export function formatTime(value:string|number|Date,tz=pbxTimezone.value):string{
 const d=new Date(value);if(Number.isNaN(d.getTime()))return ''
 let f=display.get(tz);if(!f){f=new Intl.DateTimeFormat(undefined,{timeZone:tz,year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',timeZoneName:'short'});display.set(tz,f)}
 return f.format(d)
}
// Wall-clock fields of an instant in tz, read back as if they were UTC.
function wall(ms:number,tz:string):number{
 const p:Record<string,number>={};for(const part of new Intl.DateTimeFormat('en-US',{timeZone:tz,hourCycle:'h23',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}).formatToParts(new Date(ms)))p[part.type]=Number(part.value)
 return Date.UTC(p.year??1970,(p.month??1)-1,p.day??1,p.hour??0,p.minute??0,p.second??0)
}
const offset=(ms:number,tz:string)=>wall(ms,tz)-(ms-(((ms%1000)+1000)%1000))
// datetime-local value ("YYYY-MM-DDTHH:mm") for an instant, in tz.
export function toWallClock(ms:number,tz=pbxTimezone.value):string{return new Date(wall(ms,tz)).toISOString().slice(0,16)}
// Instant (ISO, UTC) of a datetime-local value read in tz; '' when invalid.
export function fromWallClock(value:string,tz=pbxTimezone.value):string{
 const m=/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value);if(!m)return ''
 const local=Date.UTC(Number(m[1]),Number(m[2])-1,Number(m[3]),Number(m[4]),Number(m[5]),Number(m[6]??0))
 let ms=local-offset(local,tz);const corrected=offset(ms,tz);if(local-corrected!==ms)ms=local-corrected
 return new Date(ms).toISOString()
}
