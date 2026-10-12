<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiDataTable, UiStatGrid, UiStatTile, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { SupportQuality, SupportQualityGroup, SupportPoorCall, Capabilities } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import { useLatest } from '@/components/latest'
import { useRowActivation } from '@/components/rows'
import { formatTime } from '@/components/timezone'
import { percent } from '@/components/support'
import CallDrawer from '../calls/call-drawer.vue'
// Call quality as FreePBX measures it: on inbound calls the provider line
// (trunk side), on outbound calls the agent's handset (phone side). Poor =
// loss > 3 %, jitter > 30 ms, round trip > 300 ms or a poor MOS.
const {from,to,query}=usePeriod(7)
const data=ref<SupportQuality>(),caps=ref<Capabilities>({}),selected=ref(''),error=ref(''),loading=ref(false)
const latest=useLatest()
async function load(){const signal=latest.start();error.value='';loading.value=true;try{const v=await get<SupportQuality>('/api/asterisk/support/quality',query(),{},signal);if(!signal.aborted)data.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
onMounted(async()=>{try{caps.value=await get<Capabilities>('/api/asterisk/capabilities')}catch{/* no recordings in the drawer */};await load()})
const num=(v:number|null|undefined,unit:string,digits=1)=>v==null?'–':v.toFixed(digits)+unit
const groupColumns=(label:string):Column<SupportQualityGroup>[]=>[
 {key:'key',label},
 {key:'calls',label:'Calls',align:'end'},
 {key:'poor',label:'Poor',align:'end',format:g=>`${g.poor} (${percent(g.poor,g.calls)})`},
 {key:'mos',label:'MOS (median)',align:'end',format:g=>num(g.mos.median,'',2)},
 {key:'loss',label:'Loss p90',align:'end',format:g=>num(g.lossPercent.p90,' %')},
 {key:'jitter',label:'Jitter p90',align:'end',format:g=>num(g.jitterMs.p90,' ms')},
 {key:'rtt',label:'Round trip p90',align:'end',format:g=>num(g.rttMs.p90,' ms',0),hideOnStack:true},
]
const SIDE={trunk:'Provider line',phone:'Handset'} as const
const rows=useRowActivation<SupportPoorCall>(c=>c.linkedid,c=>'Open call '+c.linkedid,()=>data.value?.poorCalls??[],c=>{selected.value=c.linkedid})
const poorColumns:Column<SupportPoorCall>[]=[
 {key:'start',label:'Time',format:c=>formatTime(c.start)},
 {key:'side',label:'Measured on',format:c=>SIDE[c.side]},
 {key:'agent',label:'Agent'},
 {key:'device',label:'Phone',format:c=>c.device||'–',hideOnStack:true},
 {key:'loss',label:'Loss',align:'end',format:c=>num(c.lossPercent,' %')},
 {key:'jitter',label:'Jitter',align:'end',format:c=>num(c.jitterMs,' ms')},
 {key:'rtt',label:'Round trip',align:'end',format:c=>num(c.rttMs,' ms',0)},
 {key:'mos',label:'MOS',align:'end',format:c=>num(c.mos,'',2)},
]
</script>
<template><UiPage title="Call quality" subtitle="Packet loss, jitter and delay of answered calls"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="load"/></template>
<UiAlert v-if="error" kind="error">{{error}}</UiAlert>
<div v-if="data" class="space-y-4">
 <p class="text-sm">FreePBX measures one side of each call: <strong>inbound</strong> calls on the provider line (PBX ↔ provider), <strong>outbound</strong> calls on the agent's handset (PBX ↔ phone). A call is poor with loss over 3 %, jitter over 30 ms, round trip over 300 ms or a poor MOS. At most 31 days.</p>
 <UiStatGrid :cols="4">
  <UiStatTile title="Provider line: poor" :value="percent(data.trunk.poor,data.trunk.calls)" :color="data.trunk.poor?'warning':'success'" icon="mdi-server-network" :subtitle="`${data.trunk.poor} of ${data.trunk.calls} inbound calls`"/>
  <UiStatTile title="Provider line: MOS" :value="num(data.trunk.mos.median,'',2)" icon="mdi-signal" :subtitle="'loss p90 '+num(data.trunk.lossPercent.p90,' %')+' · jitter p90 '+num(data.trunk.jitterMs.p90,' ms')"/>
  <UiStatTile title="Handsets: poor" :value="percent(data.phone.poor,data.phone.calls)" :color="data.phone.poor?'warning':'success'" icon="mdi-headset" :subtitle="`${data.phone.poor} of ${data.phone.calls} outbound calls`"/>
  <UiStatTile title="Handsets: MOS" :value="num(data.phone.mos.median,'',2)" icon="mdi-signal" :subtitle="'loss p90 '+num(data.phone.lossPercent.p90,' %')+' · jitter p90 '+num(data.phone.jitterMs.p90,' ms')"/>
 </UiStatGrid>
 <div class="grid gap-4 lg:grid-cols-2">
  <UiCard :padded="false"><UiDataTable :items="data.byAgent" :columns="groupColumns('Agent')" row-key="key" caption="Handsets by agent (outbound calls)" empty-title="No measured outbound calls"/></UiCard>
  <UiCard :padded="false"><UiDataTable :items="data.byDevice" :columns="groupColumns('Phone model')" row-key="key" caption="Handsets by phone model (outbound calls)" empty-title="No measured outbound calls"/></UiCard>
 </div>
 <UiCard :padded="false"><UiDataTable :items="data.byTrunk" :columns="groupColumns('Trunk')" row-key="key" caption="Provider line by trunk (inbound calls)" empty-title="No measured inbound calls"/></UiCard>
 <UiCard :padded="false"><div @keydown="rows.onKeydown"><UiDataTable :items="data.poorCalls" :columns="poorColumns" row-key="linkedid" :row-attrs="rows.rowAttrs" caption="Poor calls, newest first: select one to listen" empty-title="No poor calls" empty-text="Every measured call in this period was fine." clickable @row-click="selected=$event.linkedid"/></div></UiCard>
</div>
<p v-else-if="loading">Loading…</p>
<CallDrawer v-if="selected" :key="selected" :linkedid="selected" :capabilities="caps" @close="selected=''"/></UiPage></template>
