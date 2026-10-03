<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ApiError } from '@go-tangra/ui/api'
import { UiPage, UiAlert, UiCard } from '@go-tangra/ui'
import { BASE, get, explain } from '@/api/client'
import type { LiveSnapshot, LiveUpdate, LiveCall } from '@/api/types'
import { backoff } from '@/components/backoff'
import { useLatest } from '@/components/latest'
const calls=ref<LiveCall[]>([]),fresh=ref(false),error=ref('');let generation=0,stream:EventSource|undefined,retry:ReturnType<typeof setTimeout>|undefined,stopped=false,attempt=0
const latest=useLatest()
function apply(update:LiveUpdate,type:string){if(update.generation!==generation)return;fresh.value=update.fresh;if(type==='upsert'&&update.call){calls.value=calls.value.filter(c=>c.linkedid!==update.call?.linkedid).concat(update.call)}else if(type==='remove'){calls.value=calls.value.filter(c=>c.linkedid!==update.linkedid)}}
// The stream is held only while the tab is visible; reconnects back off
// exponentially (1 s → 30 s, jittered) and reset once a stream opens.
function disconnect(){latest.cancel();clearTimeout(retry);retry=undefined;stream?.close();stream=undefined}
function schedule(){clearTimeout(retry);if(stopped||document.hidden)return;retry=setTimeout(connect,backoff(attempt++))}
async function connect(){disconnect();if(stopped||document.hidden)return;const signal=latest.start();error.value='';try{const caps=await get<Record<string,{available:boolean}>>('/api/asterisk/capabilities',{},{},signal);if(signal.aborted)return;if(!caps.live?.available){error.value='Live monitoring is unavailable.';return};const snapshot=await get<LiveSnapshot>('/api/asterisk/live/calls',{},{},signal);if(signal.aborted)return;calls.value=snapshot.calls;generation=snapshot.generation;fresh.value=snapshot.fresh;const source=new EventSource(BASE+'/live/calls/stream',{withCredentials:true});stream=source;source.addEventListener('open',()=>{attempt=0});source.addEventListener('snapshot',event=>{const v=JSON.parse((event as MessageEvent).data) as LiveSnapshot;calls.value=v.calls;generation=v.generation;fresh.value=v.fresh});for(const type of ['upsert','remove','status'])source.addEventListener(type,event=>apply(JSON.parse((event as MessageEvent).data) as LiveUpdate,type));source.onerror=()=>{if(stream!==source)return;fresh.value=false;disconnect();schedule()}}catch(e){if(signal.aborted)return;error.value=explain(e);fresh.value=false;if(!(e instanceof ApiError && [401,403].includes(e.status)))schedule()}}
function visibility(){if(document.hidden){disconnect();fresh.value=false}else connect()}
onMounted(()=>{document.addEventListener('visibilitychange',visibility);connect()});onUnmounted(()=>{stopped=true;document.removeEventListener('visibilitychange',visibility);disconnect()})
</script>
<template><UiPage title="Live calls"><UiAlert v-if="error" kind="error">{{error}}</UiAlert><UiAlert v-if="!fresh" kind="warning">Live state is stale. Waiting for a fresh snapshot.</UiAlert><p>{{calls.length}} active calls</p><div class="grid gap-3 md:grid-cols-2"><UiCard v-for="call in calls" :key="call.linkedid"><h3 class="font-mono">{{call.linkedid}}</h3><p v-for="ch in call.channels" :key="ch.uniqueid">{{ch.caller}} → {{ch.connected}} · {{ch.state}} · {{ch.channel}}</p></UiCard></div></UiPage></template>
