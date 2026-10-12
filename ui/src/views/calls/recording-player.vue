<script setup lang="ts">
import { ref } from 'vue'
import { recordingURL } from '@/api/client'
// preload="none": the file is fetched only on play, so opening a call does
// not count as listening to it (the server logs every playback).
defineProps<{linkedid:string}>();const emit=defineEmits<{played:[]}>();const failed=ref(false)
</script>
<template><section aria-label="Call recording"><p v-if="failed">The recording file is not on the PBX any more (removed by its retention) or could not be read.</p><template v-else><audio controls preload="none" :src="recordingURL(linkedid)" @error="failed=true" @play.once="emit('played')">Your browser does not support recording playback.</audio><p class="text-sm">Playback is logged with your user and the time.</p></template></section></template>
