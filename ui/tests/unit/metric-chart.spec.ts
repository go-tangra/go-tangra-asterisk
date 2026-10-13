import { expect, it } from 'vitest'
import { buildChart, formatValue, seriesName, H, W } from '../../src/components/metric-chart'
const base={__name__:'asterisk_active_calls',instance:'127.0.0.1:9094',job:'asterisk',pbx_id:'pbx-1',tenant_id:'00000000-0000-0000-0000-000000000001'}
const s=(labels:Record<string,string>,points:[string,number|null][])=>({labels,samples:points.map(([time,value])=>({time,value,hasValue:value!==null}))})
it('names a series by its own labels, not the binding and scrape labels',()=>{
 expect(seriesName(base,'Active calls')).toBe('Active calls')
 expect(seriesName({...base,endpoint:'201',aor:'201'},'x')).toBe('aor 201 · endpoint 201')
})
it('places points on the time axis of the whole period',()=>{
 // A series that began in the last quarter of the period starts at 3/4 of the width.
 const c=buildChart([s(base,[['2026-10-13T06:00:00Z',2],['2026-10-13T06:01:00Z',4]])],'2026-10-13T02:00:00Z','2026-10-13T07:20:00Z',60,'count','Active calls',8,'UTC')
 expect(c.max).toBe(5)
 const [p0,p1]=(c.lines[0]?.paths[0]??'').split(' ').map(p=>p.split(',').map(Number))
 expect(p0?.[0]).toBeCloseTo(W*240/320,1)
 expect(p0?.[1]).toBeCloseTo(H-2/5*H,1)
 expect(p1?.[1]).toBeCloseTo(H-4/5*H,1)
 expect(c.yTicks.map(t=>t.label)).toEqual(['5','2.5','0'])
 expect(c.xTicks).toHaveLength(5)
})
it('breaks a line where samples are missing for more than two steps',()=>{
 const c=buildChart([s(base,[['2026-10-13T06:00:00Z',1],['2026-10-13T06:01:00Z',1],['2026-10-13T06:10:00Z',1],['2026-10-13T06:11:00Z',null],['2026-10-13T06:12:00Z',1]])],'2026-10-13T06:00:00Z','2026-10-13T06:20:00Z',60,'count','x',8,'UTC')
 expect(c.lines[0]?.paths.map(p=>p.split(' ').length)).toEqual([2,1,1])
})
it('draws the highest series first and counts the rest as hidden',()=>{
 const many=Array.from({length:10},(_,i)=>s({...base,endpoint:String(200+i)},[['2026-10-13T06:00:00Z',i%2]]))
 const c=buildChart(many,'2026-10-13T05:00:00Z','2026-10-13T07:00:00Z',60,'flag','x',8,'UTC')
 expect(c.lines).toHaveLength(8)
 expect(c.hidden).toBe(2)
 expect(c.lines.slice(0,5).every(l=>l.latest===1)).toBe(true)
 expect(c.max).toBe(1)
})
it('returns no lines before a period is asked',()=>{expect(buildChart([s(base,[['2026-10-13T06:00:00Z',1]])],'','',60,'count','x',8,'UTC').lines).toEqual([])})
it('returns no lines without samples',()=>{expect(buildChart([],'2026-10-13T05:00:00Z','2026-10-13T07:00:00Z',60,'count','x',8,'UTC').lines).toEqual([])})
it('formats values by unit',()=>{
 expect(formatValue(12.345,'ms')).toBe('12.3 ms')
 expect(formatValue(250.4,'ms')).toBe('250 ms')
 expect(formatValue(6847,'seconds')).toBe('1 h 54 min')
 expect(formatValue(4.125,'mos')).toBe('4.13')
 expect(formatValue(1,'flag')).toBe('yes')
 expect(formatValue(0,'flag')).toBe('no')
 expect(formatValue(null,'count')).toBe('—')
 expect(formatValue(2,'count')).toBe('2')
})
