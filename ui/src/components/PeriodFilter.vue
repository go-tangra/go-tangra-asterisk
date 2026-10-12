<script setup lang="ts">
import { UiInput, UiButton } from '@go-tangra/ui'
import { pbxTimezone } from './timezone'
defineProps<{from:string;to:string;loading?:boolean}>()
defineEmits<{ 'update:from':[string]; 'update:to':[string]; submit:[] }>()
</script>
<!-- The page's filter bar: the period, then the page's own filters (default
slot) and its buttons (actions slot, Apply by default) on one grid, so a full
set of filters takes two rows on a desktop. -->
<template><div class="grid w-full grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] items-end gap-3"><UiInput id="period-from" :model-value="from" :label="'From ('+pbxTimezone+')'" type="datetime-local" @update:model-value="$emit('update:from',String($event))"/><UiInput id="period-to" :model-value="to" :label="'To, exclusive ('+pbxTimezone+')'" type="datetime-local" @update:model-value="$emit('update:to',String($event))"/><slot/><div class="flex items-end gap-2"><slot name="actions"><UiButton :disabled="loading" @click="$emit('submit')">Apply</UiButton></slot></div></div></template>
