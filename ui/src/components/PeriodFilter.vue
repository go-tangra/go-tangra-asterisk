<script setup lang="ts">
import { UiInput, UiButton } from '@go-tangra/ui'
import { pbxTimezone } from './timezone'
defineProps<{from:string;to:string;loading?:boolean}>()
defineEmits<{ 'update:from':[string]; 'update:to':[string]; submit:[] }>()
</script>
<template><div class="flex flex-wrap items-end gap-3"><UiInput id="period-from" :model-value="from" :label="'From ('+pbxTimezone+')'" type="datetime-local" @update:model-value="$emit('update:from',String($event))"/><UiInput id="period-to" :model-value="to" :label="'To, exclusive ('+pbxTimezone+')'" type="datetime-local" @update:model-value="$emit('update:to',String($event))"/><UiButton :disabled="loading" @click="$emit('submit')">Apply</UiButton></div></template>
