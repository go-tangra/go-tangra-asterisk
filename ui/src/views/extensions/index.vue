<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAbility } from '@casl/vue'
import { UiPage, UiBarList, UiCard, UiAlert, UiDataTable, UiDrawer, type Column } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { Extension, Page } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import RegistrationTab from './registration-tab.vue'
import { useRowActivation } from '@/components/rows'
import { useLatest } from '@/components/latest'
const {from,to,query}=usePeriod();const ability=useAbility();const data=ref<Page<Extension>>(),selected=ref<Extension>(),error=ref(''),page=ref(1),size=ref(25),sort=ref({key:'total',dir:'desc' as 'asc'|'desc'}),loading=ref(false)
const listing=useLatest(),detail=useLatest()
async function load(){const signal=listing.start();error.value='';loading.value=true;try{const v=await get<Page<Extension>>('/api/asterisk/stats/extensions',{...query(),page:page.value,page_size:size.value,sort:sort.value.key,order:sort.value.dir},{},signal);if(!signal.aborted)data.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
async function open(row:Extension){const signal=detail.start();error.value='';try{const v=await get<Extension>('/api/asterisk/stats/extensions/{extension}',query(),{extension:row.extension},signal);if(!signal.aborted)selected.value=v}catch(e){if(!signal.aborted)error.value=explain(e)}}
onMounted(load)
const rows=useRowActivation<Extension>(e=>e.extension,e=>'Open extension '+e.extension+(e.name?' '+e.name:''),()=>data.value?.items??[],open)
const columns:Column<Extension>[]=[{key:'extension',label:'Extension',sortable:true},{key:'name',label:'Name'},{key:'total',label:'Calls',sortable:true},{key:'answered',label:'Answered'},{key:'missed',label:'Missed'},{key:'talkSeconds',label:'Talk (s)',sortable:true},{key:'workloadShare',label:'External workload',format:e=>(e.workloadShare*100).toFixed(1)+'%'}]
</script>
<template><UiPage title="Extensions"><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="page=1;load()"/></template><UiAlert v-if="error" kind="error">{{error}}</UiAlert><UiCard :padded="false"><div @keydown="rows.onKeydown"><UiDataTable :items="data?.items??[]" :columns="columns" row-key="extension" :row-attrs="rows.rowAttrs" :total="data?.total??0" :page="page" :page-size="size" :sort="sort" :loading="loading" caption="Extension performance" clickable @row-click="open" @update:page="page=$event;load()" @update:page-size="size=$event;page=1;load()" @update:sort="sort=$event;page=1;load()"/></div></UiCard><UiDrawer :model-value="!!selected" :title="selected?.extension" size="lg" @close="detail.cancel();selected=undefined"><div v-if="selected" class="space-y-4"><p>{{selected.name}} · {{selected.total}} calls · pickup {{selected.meanPickupSeconds?.toFixed(1)??'Unavailable'}} s</p><h3>Hour of day</h3><UiBarList :items="selected.hourOfDay.map((count,hour)=>({label:hour+':00',value:count}))"/><h3>Selected period</h3><ol><li v-for="b in selected.series" :key="b.start">{{new Date(b.start).toLocaleString(undefined,{timeZone:selected.timezone})}} · {{b.total}} calls</li></ol><RegistrationTab v-if="ability.can('read','AsteriskRegistration')" :extension="selected.extension" :from="query().from" :to="query().to"/></div></UiDrawer></UiPage></template>
