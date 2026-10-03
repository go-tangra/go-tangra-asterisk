import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import RegistrationTab from '../../src/views/extensions/registration-tab.vue'
const json=(body:unknown)=>({ok:true,status:200,json:async()=>body})
const events=(n:number)=>Array.from({length:n},(_,i)=>({id:i+1,time:'2026-10-01T10:00:00Z',endpoint:'101',contact:'sip:101@pbx',status:'Created',expire:''}))
function serve(history:unknown){const fetch=vi.fn(async(url:string)=>url.includes('/registration/status/')?json({status:{extension:'101',at:'',status:'Reachable',registered:true,certainty:'observed',lastEvent:null,expiresAt:null},gaps:[]}):json(history));vi.stubGlobal('fetch',fetch);return fetch}
afterEach(()=>vi.unstubAllGlobals())
const props={extension:'101',from:'2026-10-01T00:00:00Z',to:'2026-10-02T00:00:00Z'}
it('says the history is cut when the response cannot be paged',async()=>{serve({items:events(25)});const tab=mount(RegistrationTab,{props});await flushPromises();expect(tab.text()).toContain('Showing only the first 25 registration events')})
it('pages the history when the response carries a total',async()=>{const fetch=serve({items:events(25),total:60,page:1,page_size:25});const tab=mount(RegistrationTab,{props});await flushPromises();expect(tab.text()).not.toContain('Showing only the first');const url=String(fetch.mock.calls.find(c=>String(c[0]).includes('/registration/events'))?.[0]);expect(url).toContain('page=1');expect(url).toContain('page_size=25')})
it('honours an explicit truncation flag',async()=>{serve({items:events(3),total:3,truncated:true});const tab=mount(RegistrationTab,{props});await flushPromises();expect(tab.text()).toContain('Showing only the first 3 registration events')})
