<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ApiError } from '@go-tangra/ui/api'
import { UiPage, UiAlert, UiCard, UiBadge, UiIcon, UiLiveIndicator } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { LiveBoard, LiveAgent, LiveSnapshot } from '@/api/types'
import { backoff } from '@/components/backoff'
import { useLatest } from '@/components/latest'
import { useNow, elapsed } from '@/components/clock'
import { formatTime } from '@/components/timezone'
// Who is doing what right now. The board is polled every 2 s while the tab is
// visible (it is small; the server builds it from AMI state), backing off
// after errors.
const board=ref<LiveBoard>(),channels=ref<LiveSnapshot>(),error=ref(''),showChannels=ref(false)
const now=useNow(),latest=useLatest()
let timer:ReturnType<typeof setTimeout>|undefined,attempt=0,stopped=false
async function poll(){clearTimeout(timer);if(stopped||document.hidden)return;const signal=latest.start();try{const v=await get<LiveBoard>('/api/asterisk/live/board',{},{},signal);if(signal.aborted)return;board.value=v;error.value='';attempt=0;if(showChannels.value){const c=await get<LiveSnapshot>('/api/asterisk/live/calls',{},{},signal);if(!signal.aborted)channels.value=c}timer=setTimeout(poll,2000)}catch(e){if(signal.aborted)return;error.value=explain(e);if(!(e instanceof ApiError&&[401,403].includes(e.status)))timer=setTimeout(poll,backoff(attempt++))}}
function visibility(){if(document.hidden){latest.cancel();clearTimeout(timer)}else void poll()}
function toggleChannels(e:Event){showChannels.value=(e.target as HTMLDetailsElement).open;if(showChannels.value)void poll()}
onMounted(()=>{document.addEventListener('visibilitychange',visibility);void poll()});onUnmounted(()=>{stopped=true;document.removeEventListener('visibilitychange',visibility);latest.cancel();clearTimeout(timer)})
const STATES={ringing:{label:'Ringing',color:'warning',icon:'mdi-phone-ring',order:0},'on-call':{label:'On a call',color:'primary',icon:'mdi-phone-in-talk',order:1},'on-hold':{label:'On hold',color:'secondary',icon:'mdi-phone-paused',order:2},available:{label:'Available',color:'success',icon:'mdi-phone-check',order:3},dnd:{label:'Do not disturb',color:'error',icon:'mdi-phone-off',order:4},offline:{label:'Offline',color:'neutral',icon:'mdi-phone-remove',order:5}} as const
type State=keyof typeof STATES
const state=(a:LiveAgent)=>STATES[a.state as State]??STATES.offline
const agents=computed(()=>[...(board.value?.agents??[])].sort((a,b)=>state(a).order-state(b).order||Number(a.extension)-Number(b.extension)))
const counts=computed(()=>(Object.keys(STATES) as State[]).map(s=>({state:s,...STATES[s],n:agents.value.filter(a=>a.state===s).length})).filter(c=>c.n>0))
const DIRECTION={inbound:{icon:'mdi-phone-incoming',label:'Inbound'},outbound:{icon:'mdi-phone-outgoing',label:'Outbound'},internal:{icon:'mdi-phone-forward',label:'Internal'}} as const
// Talking or ringing: time on the call; otherwise time in the state.
const clock=(a:LiveAgent)=>a.call?elapsed(now.value-Date.parse(a.call.since)):a.since?elapsed(now.value-Date.parse(a.since)):''
const sinceText=(a:LiveAgent)=>!a.call&&a.since?'since '+formatTime(a.since):''
</script>
<template><UiPage title="Live board" subtitle="Agents and waiting callers, now"><template #badges><UiLiveIndicator :connected="!!board?.fresh&&!error"/></template>
<UiAlert v-if="error" kind="error">{{error}}</UiAlert>
<UiAlert v-else-if="board&&!board.fresh" kind="warning">Live state is stale: the PBX connection is being re-established.</UiAlert>
<div v-if="board" class="space-y-4">
 <div class="flex flex-wrap gap-2" aria-label="Agents by state"><UiBadge v-for="c in counts" :key="c.state" :color="c.color" soft>{{c.label}} · {{c.n}}</UiBadge></div>
 <section aria-label="Waiting callers" class="space-y-2">
  <h3>Waiting callers ({{board.waiting.length}})</h3>
  <p v-if="!board.waiting.length" class="text-sm">Nobody is waiting.</p>
  <div class="grid gap-2 md:grid-cols-3"><UiCard v-for="w in board.waiting" :key="w.linkedid" data-test="waiting"><div class="flex items-center justify-between gap-2"><span class="font-mono">{{w.caller||'Unknown caller'}}</span><span class="font-mono text-lg">{{elapsed(now-Date.parse(w.since))}}</span></div><p class="text-sm">{{w.stage==='welcome'?'Listening to the welcome':`Ringing ${w.ringing.length} ${w.ringing.length===1?'phone':'phones'}: ${w.ringing.join(', ')}`}}</p></UiCard></div>
 </section>
 <section aria-label="Agents" class="space-y-2">
  <h3>Agents</h3>
  <p v-if="!agents.length" class="text-sm">No agents yet: the support team is read from the ring groups once the support reports run.</p>
  <ul class="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
   <li v-for="a in agents" :key="a.extension" :data-state="a.state" :data-agent="a.extension"><UiCard>
    <div class="flex items-start justify-between gap-2"><div class="space-y-1"><p class="font-semibold">{{a.extension}}<span v-if="a.name" class="font-normal"> · {{a.name}}</span></p><div class="flex flex-wrap gap-1"><UiBadge :color="state(a).color" size="sm" soft><UiIcon :name="state(a).icon" size="xs"/> {{state(a).label}}</UiBadge><UiBadge v-if="a.dnd&&a.state!=='dnd'" color="error" size="sm" outline>DND</UiBadge><UiBadge v-if="!a.member" size="sm" outline>Not in team</UiBadge></div></div><span v-if="a.state!=='offline'" class="font-mono text-lg">{{clock(a)}}</span></div>
    <p v-if="a.call" class="text-sm"><UiIcon :name="DIRECTION[a.call.direction].icon" size="xs"/> {{DIRECTION[a.call.direction].label}}<template v-if="a.call.peer"> · <span class="font-mono">{{a.call.peer}}</span></template></p>
    <p v-else-if="sinceText(a)" class="text-sm">{{sinceText(a)}}</p>
   </UiCard></li>
  </ul>
 </section>
 <details @toggle="toggleChannels"><summary>All active channels</summary><p v-if="!channels" class="text-sm">Loading…</p><ul v-else class="space-y-1"><li v-for="call in channels.calls" :key="call.linkedid" class="text-sm"><span class="font-mono">{{call.linkedid}}</span>: <span v-for="c in call.channels" :key="c.uniqueid">{{c.channel}} ({{c.state}}) </span></li><li v-if="!channels.calls.length" class="text-sm">No channels.</li></ul></details>
</div>
<p v-else-if="!error">Loading…</p>
</UiPage></template>
