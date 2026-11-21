<template>
  <aside class="lead-notification-alert" :class="{ unknown: alert.kind === 'unknown' }" :role="alert.kind === 'unknown' ? 'status' : 'alert'">
    <div><strong>{{ alert.title }}</strong><p>{{ alert.message }}</p><router-link v-if="settingsTo" :to="settingsTo">查看通知投递状态</router-link></div>
    <button v-if="alert.dismissible" type="button" @click="$emit('dismiss')">知道了</button>
  </aside>
</template>
<script setup>
defineProps({ alert: { type: Object, required: true }, settingsTo: { type: String, default: '' } })
defineEmits(['dismiss'])
</script>
<style scoped>
.lead-notification-alert { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--kx-space-3); margin-bottom: var(--kx-space-4); padding: var(--kx-space-4); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-card-radius); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); overflow-wrap: anywhere; }
.lead-notification-alert > div { min-width: 0; }
.lead-notification-alert strong { display: block; font-size: var(--kx-font-14); }
.lead-notification-alert p { margin: var(--kx-space-2) 0; font-size: var(--kx-font-13); line-height: 1.6; }
.lead-notification-alert a { display: inline-block; min-height: var(--kx-mobile-control-height); color: inherit; font-size: var(--kx-font-13); text-decoration: underline; }
.lead-notification-alert button { flex-shrink: 0; min-height: var(--kx-mobile-control-height); padding: 0 var(--kx-space-2); border: 0; background: transparent; color: inherit; font: inherit; cursor: pointer; }
.lead-notification-alert.unknown { border-color: var(--kx-color-border); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); }
</style>
