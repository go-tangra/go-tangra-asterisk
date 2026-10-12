<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ApiError } from '@go-tangra/ui/api'
import { UiPage, UiAlert, UiCard, UiInput, UiSelect, UiButton, UiDataTable, UiBadge, UiIcon, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { SupportCall, SupportCallPage, Capabilities } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import CallDrawer from './call-drawer.vue'
import { useLatest } from '@/components/latest'
import { formatTime, learnTimezone } from '@/components/timezone'
import { useRowActivation } from '@/components/rows'
import { seconds, lostText } from '@/components/support'
// Search the support calls (classified: answered, lost, outbound) by agent,
// caller, outcome, talk time, quality and recording; open one to listen.
const {from,to,query}=usePeriod(7)
const agent=ref(''),type=ref(''),caller=ref(''),minTalk=ref(''),quality=ref(''),recorded=ref('')
const page=ref(1),size=ref(25),result=ref<SupportCallPage>(),caps=ref<Capabilities>({}),selected=ref(''),error=ref(''),unavailable=ref(false),loading=ref(false)
const latest=useLatest()
async function load(reset=false){if(reset)page.value=1;const signal=latest.start();error.value='';loading.value=true;try{const v=await get<SupportCallPage>('/api/asterisk/support/calls',{...query(),agent:agent.value||undefined,type:type.value||undefined,caller:caller.value||undefined,min_talk:minTalk.value||undefined,quality:quality.value||undefined,recorded:recorded.value||undefined,page:page.value,page_size:size.value},{},signal);if(signal.aborted)return;learnTimezone(v.timezone);result.value=v}catch(e){if(signal.aborted)return;unavailable.value=e instanceof ApiError&&e.status===503;error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
onMounted(async()=>{try{caps.value=await get<Capabilities>('/api/asterisk/capabilities')}catch{/* the drawer then offers no recording */};await load()})
const names=computed(()=>result.value?.names??{})
const who=(ext:string)=>ext?(names.value[ext]?`${ext} · ${names.value[ext]}`:ext):'–'
const agents=computed(()=>(result.value?.team??[]).map(e=>({title:who(e),value:e})))
const TYPES={answered:{label:'Answered',color:'success',icon:'mdi-phone-check'},lost:{label:'Lost',color:'error',icon:'mdi-phone-missed'},outbound:{label:'Outbound',color:'primary',icon:'mdi-phone-outgoing'},'not-connected':{label:'Not connected',color:'neutral',icon:'mdi-phone-cancel'}} as const
const QUALITY={good:'success',fair:'warning',poor:'error'} as const
const rows=useRowActivation<SupportCall>(c=>c.linkedid,c=>'Open call '+c.linkedid,()=>result.value?.items??[],c=>{selected.value=c.linkedid})
const columns:Column<SupportCall>[]=[
 {key:'start',label:'Time',format:c=>formatTime(c.start)},
 {key:'type',label:'Call'},
 {key:'agent',label:'Agent',format:c=>who(c.agent)},
 {key:'caller',label:'Number'},
 {key:'waitSeconds',label:'Waited',align:'end',format:c=>seconds(c.waitSeconds),hideOnStack:true},
 {key:'talkSeconds',label:'Talk',align:'end',format:c=>seconds(c.talkSeconds)},
 {key:'quality',label:'Quality'},
 {key:'recorded',label:'Recording',format:c=>c.recorded?'Yes':'–'},
]
function clear(){agent.value=type.value=caller.value=minTalk.value=quality.value=recorded.value='';void load(true)}
</script>
<template><UiPage title="Calls" subtitle="Search the team's calls and recordings"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="load(true)">
 <UiSelect id="call-agent" v-model="agent" label="Agent" :options="agents"/>
 <UiSelect id="call-type" v-model="type" label="Call" :options="Object.entries(TYPES).map(([value,t])=>({title:t.label,value}))"/>
 <UiInput id="call-caller" v-model="caller" label="Number contains"/>
 <UiSelect id="call-talk" v-model="minTalk" label="Talk" :options="[{title:'At least 1 min',value:'60'},{title:'At least 5 min',value:'300'},{title:'At least 15 min',value:'900'}]"/>
 <UiSelect id="call-quality" v-model="quality" label="Quality" :options="[{title:'Poor only',value:'poor'}]"/>
 <UiSelect id="call-recorded" v-model="recorded" label="Recording" :options="[{title:'Recorded only',value:'true'}]"/>
 <template #actions><UiButton :disabled="loading" @click="load(true)">Search</UiButton><UiButton variant="text" @click="clear">Clear</UiButton></template>
</PeriodFilter></template>
<UiAlert v-if="error" kind="error">{{error}}<template v-if="unavailable"> The search needs the support reports (the module-owned database). <RouterLink :to="{name:'asterisk-call-log'}">Open the raw call log</RouterLink>.</template></UiAlert>
<UiCard :padded="false"><div @keydown="rows.onKeydown"><UiDataTable :items="result?.items??[]" :columns="columns" row-key="linkedid" :row-attrs="rows.rowAttrs" :total="result?.total??0" :page="page" :page-size="size" :loading="loading" caption="Calls, newest first: select one to see details and listen" empty-title="No calls" empty-text="Try another period or fewer filters." clickable @row-click="selected=$event.linkedid" @update:page="page=$event;load()" @update:page-size="size=$event;load(true)">
 <template #cell-type="{row}"><UiBadge :color="TYPES[row.type].color" size="sm" soft><UiIcon :name="TYPES[row.type].icon" size="xs"/> {{TYPES[row.type].label}}</UiBadge><span v-if="row.type==='lost'" class="text-sm"> {{lostText(row.outcome,row.lostReason)}}</span></template>
 <template #cell-quality="{row}"><UiBadge v-if="row.quality" :color="QUALITY[row.quality as keyof typeof QUALITY]" size="sm" outline>{{row.quality}}</UiBadge><span v-else>–</span></template>
</UiDataTable></div></UiCard>
<p class="text-sm"><RouterLink :to="{name:'asterisk-call-log'}">Raw call log</RouterLink>: every call as CDR records it, including internal and other calls.</p>
<CallDrawer v-if="selected" :key="selected" :linkedid="selected" :capabilities="caps" @close="selected=''"/></UiPage></template>
