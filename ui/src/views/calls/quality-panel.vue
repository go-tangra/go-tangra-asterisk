<script setup lang="ts">
import type { QoS } from '@/api/types'
defineProps<{local?:QoS|undefined;peer?:QoS|undefined}>()
const display=(value:number|null|undefined)=>value==null?'Unavailable':value.toFixed(2)
</script>
<template><div class="grid gap-3 md:grid-cols-2"><section v-for="(q,i) in [local,peer]" :key="i"><h4>{{i===0?'Local':'Peer'}} media quality</h4><p v-if="!q">Unavailable</p><dl v-else class="grid grid-cols-2 gap-1"><dt>Band</dt><dd>{{q.quality||'Unknown'}}</dd><dt>Receive / transmit MOS</dt><dd>{{display(q.rxMos)}} / {{display(q.txMos)}}</dd><dt>Receive / transmit jitter (ms)</dt><dd>{{display(q.rxJitterMs)}} / {{display(q.txJitterMs)}}</dd><dt>Receive / transmit loss (%)</dt><dd>{{display(q.rxLossPercent)}} / {{display(q.txLossPercent)}}</dd><dt>Round-trip (ms)</dt><dd>{{display(q.rttMs)}}</dd></dl></section></div></template>
