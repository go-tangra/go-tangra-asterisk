import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { backoff } from '../../src/components/backoff'
import { elapsed } from '../../src/components/clock'
import LiveView from '../../src/views/live/index.vue'
const json=(body:unknown,status=200)=>({ok:status<400,status,json:async()=>body})
const t0=Date.parse('2026-10-12T10:00:00Z')
const board={fresh:true,updated:'2026-10-12T10:00:00Z',waiting:[{linkedid:'w',caller:'0888123456',stage:'ringing',since:'2026-10-12T09:59:30Z',ringing:['41','49']}],agents:[
 {extension:'45',name:'',member:true,state:'available',since:'2026-10-12T09:00:00Z',dnd:false,call:null},
 {extension:'49',name:'Tsvetomila',member:true,state:'on-call',since:'2026-10-12T09:58:00Z',dnd:false,call:{linkedid:'c',direction:'inbound',peer:'0899000000',since:'2026-10-12T09:58:00Z'}},
 {extension:'41',name:'',member:true,state:'ringing',since:'2026-10-12T09:59:55Z',dnd:false,call:{linkedid:'w',direction:'inbound',peer:'0888123456',since:'2026-10-12T09:59:50Z'}},
 {extension:'30',name:'',member:true,state:'offline',since:null,dnd:false,call:null},
 {extension:'22',name:'',member:false,state:'on-call',since:null,dnd:false,call:{linkedid:'i',direction:'internal',peer:'36',since:'2026-10-12T09:59:00Z'}}]}
let hidden=false,fetch:ReturnType<typeof vi.fn>
beforeEach(()=>{vi.useFakeTimers();vi.setSystemTime(t0);hidden=false;vi.spyOn(Math,'random').mockReturnValue(1);Object.defineProperty(document,'hidden',{configurable:true,get:()=>hidden});fetch=vi.fn(async(url:string)=>url.includes('/live/board')?json(board):url.includes('/live/calls')?json({calls:[],generation:1,fresh:true,updated:''}):json({timezone:'Europe/Sofia'}));vi.stubGlobal('fetch',fetch)})
afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals();vi.restoreAllMocks()})
const boardCalls=()=>fetch.mock.calls.filter(c=>String(c[0]).includes('/live/board')).length
it('jitters an exponential delay capped at 30 seconds',()=>{expect(backoff(0,()=>0)).toBe(500);expect(backoff(0,()=>1)).toBe(1000);expect(backoff(3,()=>1)).toBe(8000);expect(backoff(20,()=>1)).toBe(30000);expect(backoff(20,()=>0)).toBe(15000)})
it('formats running timers',()=>{expect(elapsed(4000)).toBe('0:04');expect(elapsed(247000)).toBe('4:07');expect(elapsed(3753000)).toBe('1:02:33');expect(elapsed(-5)).toBe('0:00')})
it('orders agents by what they are doing and shows calls and waiting callers',async()=>{const view=mount(LiveView);await flushPromises();expect(view.findAll('[data-agent]').map(a=>a.attributes('data-agent'))).toEqual(['41','22','49','45','30']);const talk=view.get('[data-agent="49"]').text();expect(talk).toContain('Tsvetomila');expect(talk).toContain('2:00');expect(talk).toContain('0899000000');expect(view.get('[data-agent="22"]').text()).toContain('Not in team');const waiting=view.get('[data-test="waiting"]').text();expect(waiting).toContain('0:30');expect(waiting).toContain('Ringing 2 phones: 41, 49');expect(view.text()).toContain('On a call · 2');view.unmount()})
it('polls every 2 s while visible and stops while hidden',async()=>{const view=mount(LiveView);await flushPromises();expect(boardCalls()).toBe(1);await vi.advanceTimersByTimeAsync(2000);expect(boardCalls()).toBe(2);hidden=true;document.dispatchEvent(new Event('visibilitychange'));await vi.advanceTimersByTimeAsync(10000);expect(boardCalls()).toBe(2);hidden=false;document.dispatchEvent(new Event('visibilitychange'));await flushPromises();expect(boardCalls()).toBe(3);view.unmount()})
it('backs off after errors and stops on a denied session',async()=>{fetch.mockImplementation(async()=>json({code:'DEPENDENCY_UNAVAILABLE'},503));const view=mount(LiveView);await flushPromises();expect(boardCalls()).toBe(1);await vi.advanceTimersByTimeAsync(999);expect(boardCalls()).toBe(1);await vi.advanceTimersByTimeAsync(1);expect(boardCalls()).toBe(2);view.unmount();fetch.mockClear();fetch.mockImplementation(async()=>json({code:'DENIED'},403));const denied=mount(LiveView);await flushPromises();await vi.advanceTimersByTimeAsync(60000);expect(boardCalls()).toBe(1);denied.unmount()})
