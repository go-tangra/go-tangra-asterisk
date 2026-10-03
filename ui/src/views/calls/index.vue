<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiInput, UiSelect, UiDataTable, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { Call, Page, Capabilities } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import CallDrawer from './call-drawer.vue'
import { useLatest } from '@/components/latest'
import { formatTime, learnTimezone } from '@/components/timezone'
import { useRowActivation } from '@/components/rows'
const {from,to,query}=usePeriod()
const src=ref(''),dst=ref(''),extension=ref(''),direction=ref(''),disposition=ref('')
const page=ref(1),size=ref(25),sort=ref({key:'start',dir:'desc' as 'asc'|'desc'})
const result=ref<Page<Call>>(),caps=ref<Capabilities>({}),selected=ref(''),error=ref(''),loading=ref(false)
const latest=useLatest()
async function load(reset=false){if(reset)page.value=1;const signal=latest.start();error.value='';loading.value=true;try{const v=await get<Page<Call>>('/api/asterisk/calls',{...query(),src:src.value,dst:dst.value,extension:extension.value,direction:direction.value,disposition:disposition.value,page:page.value,page_size:size.value,sort:sort.value.key,order:sort.value.dir},{},signal);if(!signal.aborted)result.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
onMounted(async()=>{try{caps.value=await get<Capabilities>('/api/asterisk/capabilities');learnTimezone((caps.value as Record<string,unknown>).timezone)}catch(e){error.value=explain(e)};await load()})
const rows=useRowActivation<Call>(c=>c.linkedid,c=>'Open call '+c.linkedid+' from '+(c.src||'unknown')+' to '+(c.dst||'unknown'),()=>result.value?.items??[],c=>{selected.value=c.linkedid})
const columns:Column<Call>[]=[{key:'start',label:'Started',sortable:true,format:c=>formatTime(c.start)},{key:'src',label:'Caller',sortable:true},{key:'dst',label:'Destination',sortable:true},{key:'direction',label:'Direction'},{key:'disposition',label:'Outcome'},{key:'durationSeconds',label:'Duration (s)',sortable:true},{key:'answeredExtension',label:'Answered by'},{key:'linkedid',label:'Call ID'}]
</script>
<template><UiPage title="Call history"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="load(true)"/><div class="flex flex-wrap gap-3"><UiInput id="caller" v-model="src" label="Caller"/><UiInput id="destination" v-model="dst" label="Destination"/><UiInput id="extension" v-model="extension" label="Extension"/><UiSelect id="direction" v-model="direction" label="Direction" :options="['inbound','outbound','internal','unknown'].map(value=>({title:value,value}))"/><UiSelect id="disposition" v-model="disposition" label="Outcome" :options="['ANSWERED','NO ANSWER','BUSY','FAILED'].map(value=>({title:value,value}))"/></div></template><UiAlert v-if="error" kind="error">{{error}}</UiAlert><UiCard :padded="false"><div @keydown="rows.onKeydown"><UiDataTable :items="result?.items??[]" :columns="columns" row-key="linkedid" :row-attrs="rows.rowAttrs" :total="result?.total??0" :page="page" :page-size="size" :sort="sort" :loading="loading" caption="Logical calls — select one (click, or Enter on a focused row) to investigate" empty-title="No calls" empty-text="Try another period or filter." clickable @row-click="selected=$event.linkedid" @update:page="page=$event;load()" @update:page-size="size=$event;load(true)" @update:sort="sort=$event;load(true)"/></div></UiCard><CallDrawer v-if="selected" :key="selected" :linkedid="selected" :capabilities="caps" @close="selected=''"/></UiPage></template>
