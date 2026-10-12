<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiDrawer, UiAlert, UiCard } from '@go-tangra/ui'
import { useAbility } from '@casl/vue'
import { get, explain } from '@/api/client'
import type { CallDetail, Capabilities, SupportListen } from '@/api/types'
import { useLatest } from '@/components/latest'
import { formatTime } from '@/components/timezone'
import RecordingPlayer from './recording-player.vue'
import QualityPanel from './quality-panel.vue'
const props=defineProps<{linkedid:string;capabilities:Capabilities}>();defineEmits<{close:[]}>()
const ability=useAbility();const detail=ref<CallDetail>(),error=ref(''),listens=ref<SupportListen[]>()
const latest=useLatest()
onMounted(async()=>{const signal=latest.start();try{const v=await get<CallDetail>('/api/asterisk/calls/{linkedid}',{},{linkedid:props.linkedid},signal);if(signal.aborted)return;detail.value=v;if(v.summary.hasRecording&&ability.can('read','AsteriskRecordings'))void loadListens(v.summary.start)}catch(e){if(!signal.aborted)error.value=explain(e)}})
// Who opened this recording (the audit keeps listens from the call onwards;
// the API serves up to 92 days at a time).
async function loadListens(start:string){const from=Date.parse(start),to=Math.min(Date.now()+60000,from+92*86400000);if(!(to>from))return;try{listens.value=(await get<{items:SupportListen[]}>('/api/asterisk/support/listens',{from:new Date(from).toISOString(),to:new Date(to).toISOString(),linkedid:props.linkedid})).items}catch{listens.value=undefined}}
</script>
<template><UiDrawer :model-value="true" :title="'Call '+linkedid" size="xl" @close="$emit('close')"><UiAlert v-if="error" kind="error">{{error}}</UiAlert><div v-if="detail" class="space-y-4"><UiCard><dl class="grid grid-cols-2 gap-2"><dt>Outcome</dt><dd>{{detail.summary.disposition}}</dd><dt>Direction</dt><dd>{{detail.summary.direction}}</dd><dt>Talk</dt><dd>{{detail.summary.talkSeconds}} seconds</dd><dt>Pickup</dt><dd>{{detail.summary.pickupSeconds??'Unavailable'}} seconds</dd></dl></UiCard><section v-if="capabilities.recordings?.available && detail.summary.hasRecording && ability.can('read','AsteriskRecordings')" class="space-y-2"><RecordingPlayer :linkedid="linkedid" @played="loadListens(detail.summary.start)"/><div v-if="listens"><h3 class="font-semibold">Listened by</h3><p v-if="!listens.length" class="text-sm">Nobody has listened to this recording yet.</p><ul v-else class="text-sm"><li v-for="l in listens" :key="l.at+l.userId">{{formatTime(l.at)}} · {{l.userId}}</li></ul></div></section><p v-else-if="!detail.summary.hasRecording">This call was not recorded.</p><p v-else>Recording unavailable or access restricted.</p><h3 class="font-semibold">Call legs</h3><UiCard v-for="leg in detail.legs" :key="leg.uniqueid+leg.start+leg.channel"><p>{{leg.channel}} → {{leg.dstChannel}} · {{leg.disposition}}</p><p>{{formatTime(leg.start)}} · {{leg.talkSeconds}} seconds talk</p><QualityPanel :local="leg.localQuality" :peer="leg.peerQuality"/></UiCard><h3 class="font-semibold">Timeline</h3><p v-if="!detail.celAvailable">Call events unavailable.</p><ol v-else><li v-for="(event,i) in detail.timeline" :key="i">{{formatTime(event.time)}} · {{event.type}} · {{event.channel}}</li></ol><div v-if="detail.registration"><h3 class="font-semibold">Registration at call time</h3><p v-for="status in detail.registration.statuses" :key="status.extension">{{status.extension}} · {{status.status}} · {{status.certainty}}</p><UiAlert v-if="detail.registration.gaps.length" kind="warning">Observation was interrupted at this time.</UiAlert></div></div></UiDrawer></template>
