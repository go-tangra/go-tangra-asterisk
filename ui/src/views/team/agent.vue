<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAbility } from '@casl/vue'
import { UiPage, UiAlert, UiCard, UiStatGrid, UiStatTile, UiButton } from '@go-tangra/ui'
import { get, explain } from '@/api/client'
import type { SupportAgent, RegistrationTimeline } from '@/api/types'
import { usePeriod } from '@/components/period'
import PeriodFilter from '@/components/PeriodFilter.vue'
import { useLatest } from '@/components/latest'
import { learnTimezone } from '@/components/timezone'
import { percent, seconds, spreadText } from '@/components/support'
import RegistrationTimelineView from '../extensions/registration-timeline.vue'
// One agent: how they handled the team's calls, their own calls, and when
// they were online. The period comes from the Team page when opened there.
const route=useRoute(),router=useRouter(),ability=useAbility()
const extension=computed(()=>String(route.params.extension))
const {from,to,query}=usePeriod(7)
if(typeof route.query.from==='string'&&typeof route.query.to==='string'){from.value=route.query.from;to.value=route.query.to}
const agent=ref<SupportAgent>(),error=ref(''),loading=ref(false),period=ref(query()),online=ref<RegistrationTimeline>()
const request=useLatest()
async function load(){const signal=request.start();error.value='';loading.value=true;period.value=query();try{const v=await get<{timezone:string;items:SupportAgent[]}>('/api/asterisk/support/agents',period.value,{},signal);if(signal.aborted)return;learnTimezone(v.timezone);agent.value=v.items.find(a=>a.extension===extension.value);if(!agent.value)error.value='Extension '+extension.value+' took no part in support calls in this period.'}catch(e){if(!signal.aborted)error.value=explain(e)}finally{if(!signal.aborted)loading.value=false}}
onMounted(load);watch(extension,load)
const talk=computed(()=>agent.value?agent.value.inbound.talkSeconds+agent.value.inbound.directTalkSeconds+agent.value.outbound.talkSeconds:0)
const handled=computed(()=>agent.value?agent.value.inbound.answered+agent.value.inbound.directAnswered+agent.value.outbound.connected:0)
const occupancy=computed(()=>online.value&&online.value.summary.onlineSeconds>0?percent(talk.value,online.value.summary.onlineSeconds):'–')
const title=computed(()=>agent.value?.name?`${extension.value} · ${agent.value.name}`:extension.value)
</script>
<template><UiPage :title="title" subtitle="Support agent"><template #actions><UiButton variant="text" icon="mdi-arrow-left" @click="router.push({name:'asterisk-team'})">Support team</UiButton></template><template #filters><PeriodFilter v-model:from="from" v-model:to="to" :loading="loading" @submit="load"/></template>
<UiAlert v-if="error" kind="warning">{{error}}</UiAlert>
<div v-if="agent" class="space-y-4">
 <UiAlert v-if="!agent.member" kind="info">{{extension}} is not in the support team (ring group members, minus the exclusions in the module settings).</UiAlert>
 <h3>Team calls (ring group)</h3>
 <UiStatGrid :cols="6">
  <UiStatTile title="Answered" :value="agent.inbound.answered" color="success" icon="mdi-phone-check" :subtitle="(agent.inbound.answerShare*100).toFixed(0)+'% of the team\'s'"/>
  <UiStatTile title="Rang for" :value="agent.inbound.offered" icon="mdi-phone-ring" :subtitle="`${agent.inbound.colleagueAnswered} taken by a colleague`"/>
  <UiStatTile title="Declined" :value="agent.inbound.declined" :color="agent.inbound.declined?'warning':'neutral'" icon="mdi-phone-cancel" :subtitle="percent(agent.inbound.declined,agent.inbound.offered)+' of ringing'"/>
  <UiStatTile title="Rang while lost" :value="agent.inbound.rangWhileLost" icon="mdi-phone-missed" subtitle="calls nobody answered"/>
  <UiStatTile title="Time to answer" :value="seconds(agent.inbound.timeToAnswerSeconds.median)" icon="mdi-timer-outline" :subtitle="spreadText(agent.inbound.timeToAnswerSeconds)"/>
  <UiStatTile title="Talk" :value="seconds(agent.inbound.talkSeconds)" icon="mdi-headset" :subtitle="agent.inbound.answered?'avg '+seconds(agent.inbound.talkSeconds/agent.inbound.answered):''"/>
 </UiStatGrid>
 <h3>Own calls</h3>
 <UiStatGrid :cols="4">
  <UiStatTile title="Outbound connected" :value="`${agent.outbound.connected} / ${agent.outbound.attempts}`" icon="mdi-phone-outgoing" :subtitle="percent(agent.outbound.connected,agent.outbound.attempts)+' connected'"/>
  <UiStatTile title="Outbound talk" :value="seconds(agent.outbound.talkSeconds)" icon="mdi-headset"/>
  <UiStatTile title="Callbacks" :value="agent.outbound.callbacks" icon="mdi-phone-return" subtitle="lost callers called back"/>
  <UiStatTile title="Direct calls answered" :value="agent.inbound.directAnswered" icon="mdi-phone-incoming" :subtitle="seconds(agent.inbound.directTalkSeconds)+' talk'"/>
 </UiStatGrid>
 <UiCard v-if="ability.can('read','AsteriskRegistration')">
  <h3>Online</h3>
  <p class="text-sm">{{handled}} calls handled, {{seconds(talk)}} on the phone<template v-if="online"> · online {{seconds(online.summary.onlineSeconds)}} · on calls {{occupancy}} of online time</template>.</p>
  <RegistrationTimelineView :extension="extension" :from="period.from" :to="period.to" @loaded="online=$event"/>
 </UiCard>
</div>
<p v-else-if="loading">Loading…</p>
</UiPage></template>
