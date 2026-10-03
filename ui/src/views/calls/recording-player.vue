<script setup lang="ts">
import { ref } from 'vue'
import { recordingURL } from '@/api/client'
defineProps<{linkedid:string}>();const failed=ref(false)
</script>
<template><section aria-label="Call recording"><p v-if="failed">Recording unavailable.</p><audio v-else controls preload="metadata" :src="recordingURL(linkedid)" @error="failed=true">Your browser does not support recording playback.</audio></section></template>
