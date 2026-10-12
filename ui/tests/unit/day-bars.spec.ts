import { expect, it } from 'vitest'
import { dayBars, formatDuration } from '../../src/components/day-bars'
const seg=(start:string,end:string,state:'online'|'offline'|'unknown',contact?:string)=>({start,end,state,...(contact?{contact}:{})})
it('cuts the timeline at PBX midnights and drops the future',()=>{
 // Sofia is UTC+3 in October: local midnight of 12 Oct is 11 Oct 21:00Z.
 const bars=dayBars({from:'2026-10-11T07:36:00Z',to:'2026-10-12T07:36:00Z',segments:[seg('2026-10-11T07:36:00Z','2026-10-11T17:11:00Z','offline'),seg('2026-10-11T17:11:00Z','2026-10-12T05:08:00Z','unknown'),seg('2026-10-12T05:08:00Z','2026-10-12T07:36:00Z','online','sip:49@pbx')]},'Europe/Sofia')
 expect(bars.map(b=>b.day)).toEqual(['2026-10-11','2026-10-12'])
 const [first,second]=bars
 expect(first?.length).toBe(1440)
 expect(first?.pieces.map(p=>[p.state,p.x,p.width])).toEqual([['offline',636,575],['unknown',1211,229]])
 expect(second?.pieces.map(p=>[p.state,p.x,p.width])).toEqual([['unknown',0,488],['online',488,148]])
 expect(second?.pieces[1]?.contact).toBe('sip:49@pbx')
 expect(second?.totals).toEqual({online:148*60,offline:0,unknown:488*60})
})
it('gives a DST change day its real length',()=>{
 // Sofia leaves summer time on 25 Oct 2026: that day lasts 25 hours.
 const bars=dayBars({from:'2026-10-24T21:00:00Z',to:'2026-10-25T22:00:00Z',segments:[seg('2026-10-24T21:00:00Z','2026-10-25T22:00:00Z','online')]},'Europe/Sofia')
 expect(bars).toHaveLength(1)
 expect(bars[0]?.length).toBe(1500)
 expect(bars[0]?.pieces[0]?.width).toBe(1500)
})
it('returns no bars for an empty range',()=>{expect(dayBars({from:'2026-10-12T00:00:00Z',to:'2026-10-12T00:00:00Z',segments:[]},'UTC')).toEqual([])})
it('formats durations',()=>{expect(formatDuration(40)).toBe('40 s');expect(formatDuration(35*60)).toBe('35 min');expect(formatDuration(6847)).toBe('1 h 54 min');expect(formatDuration(7200)).toBe('2 h')})
