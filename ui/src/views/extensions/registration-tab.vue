<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { UiAlert, UiInput, UiButton, UiDataTable, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import { useLatest } from '@/components/latest'
import type { RegistrationStatus, RegistrationEvent, RegistrationHistory, Gap } from '@/api/types'
const props=defineProps<{extension:string;from:string;to:string}>();const at=ref(''),status=ref<RegistrationStatus>(),events=ref<RegistrationHistory>(),gaps=ref<Gap[]>([]),error=ref(''),page=ref(1),size=ref(25),loading=ref(false)
const statusRequest=useLatest(),historyRequest=useLatest()
async function loadStatus(){const signal=statusRequest.start();error.value='';try{const response=await get<{status:RegistrationStatus;gaps:Gap[]}>('/api/asterisk/registration/status/{extension}',{at:at.value?new Date(at.value).toISOString():undefined},{extension:props.extension},signal);if(signal.aborted)return;status.value=response.status;gaps.value=response.gaps}catch(e){if(!signal.aborted)error.value=explain(e)}}
async function loadHistory(reset=false){if(reset)page.value=1;const signal=historyRequest.start();loading.value=true;try{const v=await get<RegistrationHistory>('/api/asterisk/registration/events',{from:props.from,to:props.to,extension:props.extension,page:page.value,page_size:size.value},{},signal);if(!signal.aborted)events.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
// The history is paged server side. A response without a total (or one that
// says it was truncated) cannot be paged reliably, so the view says it shows
// only the first events and asks for a narrower period instead.
const paged=computed(()=>typeof events.value?.total==='number')
const truncated=computed(()=>{const v=events.value;if(!v)return false;if(v.truncated||v.has_more)return true;return !paged.value&&v.items.length>=size.value})
const load=()=>{void loadStatus();void loadHistory(true)}
onMounted(load);watch(()=>props.extension,load)
const columns:Column<RegistrationEvent>[]=[{key:'time',label:'Time',format:e=>new Date(e.time).toLocaleString()},{key:'status',label:'Status'},{key:'contact',label:'Contact'}]
</script>
<template><section aria-label="Registration history" class="space-y-3"><h3>Registration history</h3><div class="flex items-end gap-3"><UiInput id="registration-at" v-model="at" label="Observed at (blank for now)" type="datetime-local"/><UiButton @click="loadStatus">Inspect</UiButton></div><UiAlert v-if="error" kind="warning">{{error}}</UiAlert><p v-if="status">{{status.registered?'Registered':'Not registered'}} · {{status.status}} · {{status.certainty}}</p><UiAlert v-if="gaps.length" kind="warning">Registration was not observed during this interval.</UiAlert><UiAlert v-if="truncated" kind="info">Showing only the first {{events?.items.length}} registration events of this period. Choose a shorter period to see the rest.</UiAlert><UiDataTable :items="events?.items??[]" :columns="columns" row-key="id" :total="paged?events?.total:undefined" :page="page" :page-size="size" :loading="loading" caption="Registration events, newest first" empty-title="No registration events" empty-text="Nothing was observed for this extension in the period." @update:page="page=$event;loadHistory()" @update:page-size="size=$event;loadHistory(true)"/></section></template>
