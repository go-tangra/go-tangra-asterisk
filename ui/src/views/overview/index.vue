<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiPage, UiBarList, UiCard, UiAlert, UiSelect, UiInput, UiButton, UiDataTable, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { Overview, Bucket, Ringgroup } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import { useLatest } from '@/components/latest'
import { formatTime, learnTimezone } from '@/components/timezone'
const {from,to,query}=usePeriod();const bucket=ref('day'),data=ref<Overview>(),ring=ref<Ringgroup>(),group=ref('600'),error=ref('')
const overview=useLatest(),ringgroup=useLatest()
async function load(){const signal=overview.start();error.value='';try{const v=await get<Overview>('/api/asterisk/stats/overview',{...query(),bucket:bucket.value},{},signal);if(!signal.aborted){learnTimezone(v.timezone);data.value=v}}catch(e){if(!signal.aborted)error.value=explain(e)}}
async function drill(){const signal=ringgroup.start();error.value='';try{const v=await get<Ringgroup>('/api/asterisk/stats/ringgroups/{ring_group}',query(),{ring_group:group.value},signal);if(!signal.aborted)ring.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}}
onMounted(load)
const columns:Column<Bucket>[]=[{key:'start',label:'Bucket',format:b=>formatTime(b.start)},{key:'total',label:'Calls'},{key:'answered',label:'Answered'},{key:'missed',label:'Missed'}]
</script>
<template><UiPage title="Overview"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" @submit="load"/><UiSelect id="bucket" v-model="bucket" label="Bucket" :clearable="false" :options="['hour','day','week'].map(value=>({title:value,value}))" @change="load"/></template><UiAlert v-if="error" kind="error">{{error}}</UiAlert><div v-if="data" class="space-y-4"><div class="grid gap-3 md:grid-cols-4"><UiCard v-for="(v,key) in {Calls:data.total,Answered:data.answered,Missed:data.missed,'Mean talk (s)':data.meanTalkSeconds.toFixed(1)}" :key="key"><h3>{{key}}</h3><p class="text-2xl">{{v}}</p></UiCard></div><p>Mean pickup: {{data.meanPickupSeconds?.toFixed(1)??'Unavailable'}} seconds</p><UiCard><h3>Recent call distribution (up to 48 buckets)</h3><UiBarList :items="data.series.slice(-48).map(b=>({label:formatTime(b.start),value:b.total}))"/></UiCard><UiDataTable :items="data.series" :columns="columns" row-key="start" caption="Call distribution"/><UiCard><div class="flex items-end gap-3"><UiInput id="ringgroup" v-model="group" label="Ringgroup"/><UiButton @click="drill">Inspect</UiButton></div><dl v-if="ring" class="grid grid-cols-2 gap-2"><dt>Total</dt><dd>{{ring.total}}</dd><dt>Answered</dt><dd>{{ring.answered}}</dd><dt>No answer</dt><dd>{{ring.noAnswer}}</dd><dt>All busy</dt><dd>{{ring.allBusy}}</dd><dt>Failed</dt><dd>{{ring.failed}}</dd></dl><ol v-if="ring"><li v-for="call in ring.missedCalls" :key="call.linkedid">{{formatTime(call.start)}} · {{call.src}} · {{call.disposition}}</li></ol></UiCard></div></UiPage></template>
