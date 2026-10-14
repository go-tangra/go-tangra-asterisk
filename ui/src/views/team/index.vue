<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { UiPage, UiAlert, UiCard, UiDataTable, UiStatGrid, UiStatTile, UiTabs, UiButton, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { SupportSummary, SupportAgent, SupportLost, Page } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import { useRowActivation } from '@/components/rows'
import { useLatest } from '@/components/latest'
import { formatTime, learnTimezone } from '@/components/timezone'
import { percent, seconds, spreadText, lostText } from '@/components/support'
// The support team at a glance (docs/support-metrics.md): how the callers
// were served, then each agent, then the calls nobody took.
const {from,to,query}=usePeriod(7);const router=useRouter()
const summary=ref<SupportSummary>(),agents=ref<SupportAgent[]>([]),lost=ref<Page<SupportLost>>(),error=ref(''),loading=ref(false),tab=ref('agents'),others=ref(false),lostPage=ref(1),lostSize=ref(25)
const request=useLatest(),lostRequest=useLatest()
async function load(){const signal=request.start();error.value='';loading.value=true;try{const [s,a]=await Promise.all([get<SupportSummary>('/api/asterisk/support/summary',query(),{},signal),get<{timezone:string;items:SupportAgent[]}>('/api/asterisk/support/agents',query(),{},signal)]);if(signal.aborted)return;learnTimezone(s.timezone);summary.value=s;agents.value=a.items;lostPage.value=1;void loadLost()}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
async function loadLost(){const signal=lostRequest.start();try{const v=await get<Page<SupportLost>>('/api/asterisk/support/lost',{...query(),page:lostPage.value,page_size:lostSize.value},{},signal);if(!signal.aborted)lost.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}}
onMounted(load)
const shown=computed(()=>agents.value.filter(a=>others.value||a.member))
const hidden=computed(()=>agents.value.filter(a=>!a.member).length)
const inbound=computed(()=>summary.value?.inbound)
const open=(a:SupportAgent)=>void router.push({name:'asterisk-agent',params:{extension:a.extension},query:{from:from.value,to:to.value}})
const rows=useRowActivation<SupportAgent>(a=>a.extension,a=>'Open agent '+a.extension+(a.name?' '+a.name:''),()=>shown.value,open)
const columns:Column<SupportAgent>[]=[
 {key:'extension',label:'Agent'},
 {key:'answered',label:'Answered',align:'end',format:a=>String(a.inbound.answered)},
 {key:'share',label:'Share',align:'end',format:a=>(a.share*100).toFixed(0)+'%'},
 {key:'answerShare',label:'Answer share',align:'end',format:a=>(a.inbound.answerShare*100).toFixed(0)+'%',hideOnStack:true},
 {key:'declined',label:'Declined',align:'end',format:a=>String(a.inbound.declined)},
 {key:'pickup',label:'Time to answer',align:'end',format:a=>spreadText(a.inbound.timeToAnswerSeconds),hideOnStack:true},
 {key:'talk',label:'Inbound talk',align:'end',format:a=>seconds(a.inbound.talkSeconds)},
 {key:'outbound',label:'Outbound',align:'end',format:a=>`${a.outbound.connected} / ${a.outbound.attempts}`},
 {key:'outTalk',label:'Outbound talk',align:'end',format:a=>seconds(a.outbound.talkSeconds),hideOnStack:true},
 {key:'callbacks',label:'Callbacks',align:'end',format:a=>String(a.outbound.callbacks)},
]
const lostColumns:Column<SupportLost>[]=[
 {key:'start',label:'Time',format:l=>formatTime(l.start)},
 {key:'caller',label:'Caller'},
 {key:'reason',label:'What happened',format:l=>lostText(l.outcome,l.reason)},
 {key:'waitSeconds',label:'Waited',align:'end',format:l=>seconds(l.waitSeconds)},
 {key:'rang',label:'Phones rang',align:'end',format:l=>String(l.rang.length)},
 {key:'callbackAt',label:'Called back',format:l=>l.callbackAt?`${formatTime(l.callbackAt)} by ${l.callbackBy}`:'No'},
]
const tabs=computed(()=>[{key:'agents',label:'Agents',count:shown.value.length},{key:'lost',label:'Lost calls',count:lost.value?.total??0}])
</script>
<template><UiPage title="Support team" subtitle="Ring group calls, agents and lost calls"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="load"/></template>
<UiAlert v-if="error" kind="error">{{error}}</UiAlert>
<div v-if="summary&&inbound" class="space-y-4">
 <UiStatGrid :cols="6">
  <UiStatTile title="Calls to the team" :value="inbound.calls" icon="mdi-phone-incoming" :subtitle="`${inbound.directCalls} direct`"/>
  <UiStatTile title="Answered" :value="percent(inbound.answered,inbound.calls)" color="success" icon="mdi-phone-check" :subtitle="`${inbound.answered} calls`"/>
  <UiStatTile title="Hung up during welcome" :value="inbound.abandonedBeforeGroup" color="warning" icon="mdi-account-voice" :subtitle="percent(inbound.abandonedBeforeGroup,inbound.calls)+' of calls'"/>
  <UiStatTile title="Lost" :value="inbound.lost" color="error" icon="mdi-phone-missed" :subtitle="`${inbound.lostByReason['no-agent-available']??0} no agent · ${inbound.lostByReason['caller-hung-up-while-ringing']??0} while ringing`"/>
  <UiStatTile title="Time to answer" :value="seconds(inbound.waitSeconds.median)" icon="mdi-timer-outline" :subtitle="'once phones ring · p90 '+seconds(inbound.waitSeconds.p90)"/>
  <UiStatTile title="Called back" :value="inbound.calledBack" icon="mdi-phone-return" :subtitle="`of ${inbound.lost+inbound.abandonedBeforeGroup} lost or abandoned`"/>
 </UiStatGrid>
 <p class="text-sm">Outbound: {{summary.outbound.connected}} connected of {{summary.outbound.attempts}} attempts · {{seconds(summary.outbound.talkSeconds)}} talk. Inbound talk {{seconds(inbound.talkSeconds)}}.<template v-if="summary.ingestedAt"> Data up to {{formatTime(summary.ingestedAt)}}.</template></p>
 <UiTabs v-model="tab" :tabs="tabs"/>
 <UiCard v-if="tab==='agents'" :padded="false"><div @keydown="rows.onKeydown"><UiDataTable :items="shown" :columns="columns" row-key="extension" :row-attrs="rows.rowAttrs" :loading="loading" caption="Agents, most answered first" empty-title="No agents" empty-text="No ring group members were found in the FreePBX configuration." clickable @row-click="open"><template #cell-extension="{row}"><span class="font-semibold">{{row.extension}}</span><span v-if="row.name"> · {{row.name}}</span></template></UiDataTable></div></UiCard>
 <p v-if="tab==='agents'&&hidden" class="text-sm">{{hidden}} other {{hidden===1?'extension':'extensions'}} took part but {{hidden===1?'is':'are'}} not in the team. <UiButton size="sm" variant="text" @click="others=!others">{{others?'Hide them':'Show them'}}</UiButton></p>
 <UiCard v-if="tab==='lost'" :padded="false"><UiDataTable :items="lost?.items??[]" :columns="lostColumns" row-key="linkedid" :total="lost?.total??0" :page="lostPage" :page-size="lostSize" caption="Lost and abandoned calls, newest first" empty-title="No lost calls" empty-text="Every caller in this period was answered." @update:page="lostPage=$event;loadLost()" @update:page-size="lostSize=$event;lostPage=1;loadLost()"/></UiCard>
</div>
<p v-else-if="loading">Loading…</p>
</UiPage></template>
