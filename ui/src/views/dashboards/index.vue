<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiSelect, UiButton } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { MetricSeries } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import { useLatest } from '@/components/latest'
import { toWallClock } from '@/components/timezone'
import { buildChart, formatValue, seriesKey, seriesName, H, W, type Unit } from '@/components/metric-chart'
const {from,to,query}=usePeriod();const extension=ref(''),metric=ref('asterisk_active_calls'),error=ref(''),loading=ref(false),range=ref<MetricSeries[]>([]),instant=ref<MetricSeries[]>([]),directory=ref<{title:string;value:string}[]>([])
const metrics:{title:string;value:string;unit:Unit}[]=[{title:'Active calls',value:'asterisk_active_calls',unit:'count'},{title:'AMI freshness',value:'asterisk_ami_fresh',unit:'flag'},{title:'PBX uptime',value:'asterisk_uptime_seconds',unit:'seconds'},{title:'Active channels',value:'asterisk_channels_active',unit:'count'},{title:'SIP peer availability',value:'asterisk_sip_peer_up',unit:'flag'},{title:'PJSIP endpoint availability',value:'asterisk_pjsip_endpoint_up',unit:'flag'},{title:'Queue callers',value:'asterisk_queue_callers',unit:'count'},{title:'Queue completions',value:'asterisk_queue_completed_calls',unit:'count'},{title:'Queue abandonments',value:'asterisk_queue_abandoned_calls',unit:'count'},{title:'Registered contacts',value:'asterisk_contact_registered',unit:'flag'},{title:'Contact round-trip time',value:'asterisk_contact_rtt_milliseconds',unit:'ms'},{title:'Receive media quality',value:'asterisk_rtp_receive_mos',unit:'mos'}]
const selected=computed(()=>metrics.find(m=>m.value===metric.value)??metrics[0]!)
// Quick ranges end at "now" every time the chart loads, so new samples show up;
// typing a date leaves the quick range.
const ranges=[{title:'1 h',hours:1},{title:'6 h',hours:6},{title:'24 h',hours:24},{title:'7 d',hours:168}]
const quick=ref<number|null>(24)
function anchor(){if(quick.value===null)return;const end=Date.now()+60_000;from.value=toWallClock(end-quick.value*3600_000);to.value=toWallClock(end)}
function useRange(hours:number){quick.value=hours;void load()}
const asked=ref({from:'',to:'',step:60})
const latest=useLatest()
async function load(){
 anchor();const signal=latest.start();error.value='';loading.value=true
 try{
  const q=metric.value+(extension.value?'{extension='+JSON.stringify(extension.value)+'}':'');const period=query()
  const step=Math.max(60,Math.ceil((Date.parse(period.to)-Date.parse(period.from))/1000/500))
  const [series,now]=await Promise.all([get<{items:MetricSeries[]}>('/api/asterisk/dashboard/query_range',{query:q,start:period.from,end:period.to,stepSeconds:step},{},signal),get<{items:MetricSeries[]}>('/api/asterisk/dashboard/query',{query:q},{},signal)])
  if(signal.aborted)return;asked.value={from:period.from,to:period.to,step};range.value=series.items;instant.value=now.items
 }catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}
}
onMounted(async()=>{try{directory.value=(await get<{items:{extension:string;name:string}[]}>('/api/asterisk/directory/extensions')).items.map(e=>({title:e.extension+' '+e.name,value:e.extension}))}catch(e){error.value=explain(e)};await load()})
const chart=computed(()=>buildChart(range.value,asked.value.from,asked.value.to,asked.value.step,selected.value.unit,selected.value.title,8,undefined,new Set(instant.value.map(i=>seriesKey(i.labels)))))
const current=(key:string)=>instant.value.find(i=>seriesKey(i.labels)===key)?.samples[0]?.value
const STROKES=['stroke-primary','stroke-secondary','stroke-accent','stroke-info','stroke-warning','stroke-error','stroke-success','stroke-base-content']
const FILLS=['fill-primary','fill-secondary','fill-accent','fill-info','fill-warning','fill-error','fill-success','fill-base-content']
const SWATCHES=['bg-primary','bg-secondary','bg-accent','bg-info','bg-warning','bg-error','bg-success','bg-base-content']
const dot=(path:string)=>{const [x,y]=path.split(',').map(Number);return {x:x??0,y:y??0}}
// Every series' current value, for metrics with more series than the chart draws.
const table=computed(()=>instant.value.map(s=>({key:seriesKey(s.labels),name:seriesName(s.labels,selected.value.title),value:s.samples[0]?.value})).sort((a,b)=>a.name.localeCompare(b.name,undefined,{numeric:true})))
</script>
<template><UiPage title="PBX monitoring"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" @update:from="quick=null" @update:to="quick=null"><UiSelect id="metric" v-model="metric" label="Metric" :clearable="false" :options="metrics" @update:model-value="load"/><UiSelect id="monitor-extension" v-model="extension" label="Extension" :options="directory" @update:model-value="load"/><div class="join" role="group" aria-label="Quick range"><UiButton v-for="r in ranges" :key="r.hours" class="join-item" :variant="quick===r.hours?'solid':'soft'" :data-test="'range-'+r.hours" @click="useRange(r.hours)">{{r.title}}</UiButton></div><template #actions><UiButton :disabled="loading" @click="load">Refresh</UiButton></template></PeriodFilter></template>
<UiAlert v-if="error" kind="warning">{{error}}</UiAlert>
<UiCard v-else :title="selected.title">
 <p v-if="!chart.lines.length" class="text-sm text-base-content/60" data-test="chart-empty">No samples in this period.</p>
 <div v-else class="flex flex-col gap-3">
  <div class="grid grid-cols-[auto_1fr] gap-x-3">
   <div class="flex h-56 flex-col justify-between text-right text-xs text-base-content/60 tabular-nums"><span v-for="t in chart.yTicks" :key="t.y" class="leading-none">{{t.label}}</span></div>
   <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="h-56 w-full overflow-visible" role="img" :aria-label="selected.title" data-test="chart">
    <line v-for="t in chart.yTicks" :key="t.y" x1="0" :y1="t.y" :x2="W" :y2="t.y" class="stroke-base-content/15" stroke-width="1" vector-effect="non-scaling-stroke"/>
    <template v-for="(l,i) in chart.lines" :key="l.key"><template v-for="(p,j) in l.paths" :key="j"><polyline v-if="p.includes(' ')" :points="p" fill="none" stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke" :class="STROKES[i%STROKES.length]"/><circle v-else :cx="dot(p).x" :cy="dot(p).y" r="3" :class="FILLS[i%FILLS.length]"/></template></template>
   </svg>
   <div/>
   <div class="mt-1 flex justify-between text-xs text-base-content/60 tabular-nums" data-test="time-axis"><span v-for="t in chart.xTicks" :key="t.x">{{t.label}}</span></div>
  </div>
  <ul class="flex flex-wrap gap-x-5 gap-y-1 text-sm" data-test="legend">
   <li v-for="(l,i) in chart.lines" :key="l.key" class="flex items-center gap-2"><span class="inline-block size-2.5 rounded-full" :class="SWATCHES[i%SWATCHES.length]"/><span>{{l.name}}</span><span class="font-semibold tabular-nums">{{formatValue(current(l.key),selected.unit)}}</span></li>
  </ul>
  <p v-if="chart.hidden" class="text-sm text-base-content/60">{{chart.hidden}} more series are not drawn. Pick an extension to narrow the chart; every current value is listed below.</p>
 </div>
</UiCard>
<UiCard v-if="!error && chart.hidden" title="Current values"><div class="grid grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] gap-x-6 gap-y-1 text-sm" data-test="current-values"><div v-for="r in table" :key="r.key" class="flex justify-between gap-2"><span class="truncate" :title="r.name">{{r.name}}</span><span class="shrink-0 whitespace-nowrap font-semibold tabular-nums">{{formatValue(r.value,selected.unit)}}</span></div></div></UiCard>
</UiPage></template>
