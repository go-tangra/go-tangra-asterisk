import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { abilitiesPlugin } from '@casl/vue'
import { createMongoAbility } from '@casl/ability'
import Calls from '../../src/views/calls/index.vue'
import Player from '../../src/views/calls/recording-player.vue'
const json=(body:unknown,status=200)=>({ok:status<400,status,json:async()=>body})
const page={timezone:'Europe/Sofia',team:['41','49'],names:{'49':'Tsvetomila'},total:2,page:1,page_size:25,items:[
 {linkedid:'a',start:'2026-10-08T08:46:01Z',type:'answered',kind:'inbound-group',outcome:'answered',lostReason:'',agent:'49',caller:'0888123456',waitSeconds:41,talkSeconds:72,rang:['49','41'],recorded:true,quality:'poor'},
 {linkedid:'b',start:'2026-10-08T08:14:02Z',type:'lost',kind:'inbound-group',outcome:'lost',lostReason:'no-agent-available',agent:'',caller:'0899000000',waitSeconds:19,talkSeconds:null,rang:[],recorded:false,quality:''}]}
afterEach(()=>vi.unstubAllGlobals())
async function mountCalls(fetch:ReturnType<typeof vi.fn>){vi.stubGlobal('fetch',fetch);const router=createRouter({history:createMemoryHistory(),routes:[{path:'/asterisk',name:'asterisk-calls',component:Calls},{path:'/asterisk/calls/log',name:'asterisk-call-log',component:{template:'<p/>'}}]});await router.push('/asterisk');await router.isReady();const w=mount(Calls,{global:{plugins:[router,[abilitiesPlugin,createMongoAbility([])]]}});await flushPromises();return w}
it('lists classified calls with agent names, outcomes and quality',async()=>{const fetch=vi.fn(async(url:string)=>url.includes('/support/calls')?json(page):json({}));const w=await mountCalls(fetch);const text=w.text();expect(text).toContain('49 · Tsvetomila');expect(text).toContain('No agent available');expect(text).toContain('poor');expect(text).toContain('1 min')})
it('sends the filters it was given',async()=>{const fetch=vi.fn(async(url:string)=>url.includes('/support/calls')?json(page):json({}));const w=await mountCalls(fetch);await w.get('#call-caller').setValue('0888');await w.findAll('button').find(b=>b.text()==='Search')!.trigger('click');await flushPromises();const url=String(fetch.mock.calls.filter(c=>String(c[0]).includes('/support/calls')).pop()![0]);expect(url).toContain('caller=0888');expect(url).toContain('page=1');expect(url).not.toContain('agent=')})
it('points to the raw call log when the search is unavailable',async()=>{const fetch=vi.fn(async(url:string)=>url.includes('/support/calls')?json({code:'FEATURE_UNAVAILABLE'},503):json({}));const w=await mountCalls(fetch);expect(w.text()).toContain('Open the raw call log')})
it('fetches a recording only when played',()=>{const w=mount(Player,{props:{linkedid:'a'}});const audio=w.get('audio');expect(audio.attributes('preload')).toBe('none');audio.trigger('play');expect(w.emitted('played')).toHaveLength(1)})
