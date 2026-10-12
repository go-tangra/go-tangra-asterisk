import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { abilitiesPlugin } from '@casl/vue'
import { createMongoAbility } from '@casl/ability'
import Quality from '../../src/views/quality/index.vue'
const json=(body:unknown)=>({ok:true,status:200,json:async()=>body})
const s=(median:number|null,p90:number|null)=>({median,p90})
const g=(key:string,calls:number,poor:number)=>({key,calls,poor,lossPercent:s(0,7.41),jitterMs:s(2,45),rttMs:s(20,30),mos:s(4.32,null)})
const report={trunk:g('trunk',110,1),phone:g('phone',42,1),byTrunk:[g('ITD',110,1)],byAgent:[g('45',8,1),g('49',3,0)],byDevice:[g('Yealink T46U',8,1)],poorCalls:[{linkedid:'c',start:'2026-10-08T09:31:42Z',type:'outbound',agent:'45',trunk:'ITD',device:'Yealink T46U',side:'phone',lossPercent:7.41,jitterMs:2,rttMs:20,mos:4.3}]}
afterEach(()=>vi.unstubAllGlobals())
it('shows both measured sides, the groups and the poor calls',async()=>{vi.stubGlobal('fetch',vi.fn(async(url:string)=>url.includes('/support/quality')?json(report):json({})));const w=mount(Quality,{global:{plugins:[[abilitiesPlugin,createMongoAbility([])]]}});await flushPromises();const text=w.text();expect(text).toContain('1 of 110 inbound calls');expect(text).toContain('1 of 42 outbound calls');expect(text).toContain('4.32');expect(text).toContain('7.4 %');expect(text).toContain('Yealink T46U');expect(text).toContain('Handset');expect(text).toContain('1 (13%)')})
