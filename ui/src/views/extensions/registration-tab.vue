<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { UiAlert, UiInput, UiButton } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { RegistrationStatus, RegistrationEvent, Gap, Page } from '@/api/types'
const props=defineProps<{extension:string;from:string;to:string}>();const at=ref(''),status=ref<RegistrationStatus>(),events=ref<Page<RegistrationEvent>>(),gaps=ref<Gap[]>([]),error=ref('')
async function load(){error.value='';try{const response=await get<{status:RegistrationStatus;gaps:Gap[]}>('/api/asterisk/registration/status/{extension}',{at:at.value?new Date(at.value).toISOString():undefined},{extension:props.extension});status.value=response.status;gaps.value=response.gaps;events.value=await get<Page<RegistrationEvent>>('/api/asterisk/registration/events',{from:props.from,to:props.to,extension:props.extension})}catch(e){error.value=explain(e)}}
onMounted(load);watch(()=>props.extension,load)
</script>
<template><section aria-label="Registration history" class="space-y-3"><h3>Registration history</h3><div class="flex items-end gap-3"><UiInput id="registration-at" v-model="at" label="Observed at (blank for now)" type="datetime-local"/><UiButton @click="load">Inspect</UiButton></div><UiAlert v-if="error" kind="warning">{{error}}</UiAlert><p v-if="status">{{status.registered?'Registered':'Not registered'}} · {{status.status}} · {{status.certainty}}</p><UiAlert v-if="gaps.length" kind="warning">Registration was not observed during this interval.</UiAlert><ol><li v-for="event in events?.items" :key="event.id">{{new Date(event.time).toLocaleString()}} · {{event.status}} · {{event.contact}}</li></ol></section></template>
