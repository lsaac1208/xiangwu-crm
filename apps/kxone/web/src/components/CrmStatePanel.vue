<template>
  <section class="crm-state-panel" :class="`crm-state-panel--${state}`" :role="state === 'error' ? 'alert' : 'status'" :aria-live="state === 'loading' ? 'polite' : undefined">
    <van-loading v-if="state === 'loading'" size="24" aria-hidden="true" />
    <van-icon v-else :name="icon" aria-hidden="true" />
    <strong>{{ title }}</strong>
    <p v-if="description">{{ description }}</p>
    <button v-if="actionLabel" type="button" @click="$emit('action')">{{ actionLabel }}</button>
  </section>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  state: { type: String, default: 'empty' },
  title: { type: String, required: true },
  description: { type: String, default: '' },
  actionLabel: { type: String, default: '' }
})
defineEmits(['action'])
const icon = computed(() => props.state === 'error' ? 'warning-o' : 'notes-o')
</script>

<style scoped>
.crm-state-panel { display: grid; min-height: 180px; place-items: center; align-content: center; gap: var(--kx-space-2); padding: var(--kx-space-6); color: var(--kx-color-text-secondary); text-align: center; }
.crm-state-panel > .van-icon { color: var(--kx-color-text-muted); font-size: 28px; }
.crm-state-panel--error > .van-icon { color: var(--kx-color-danger); }
.crm-state-panel strong { color: var(--kx-color-text); font-size: var(--kx-font-16); font-weight: 600; }
.crm-state-panel p { max-width: 480px; color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.6; }
.crm-state-panel button { min-height: var(--kx-control-current-height); padding: 0 var(--kx-space-4); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-brand); font-size: var(--kx-font-14); font-weight: 500; cursor: pointer; }
</style>
