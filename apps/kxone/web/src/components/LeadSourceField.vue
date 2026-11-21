<template>
  <van-field :model-value="modelValue" name="source_channel" label="来源渠道" :required="required" :rules="required ? [{ required: true, message: '请选择来源渠道' }] : []">
    <template #input>
      <select class="lead-source-select" aria-label="来源渠道" :aria-required="required" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
        <option value="">{{ required ? '请选择来源渠道' : '未填写（可补填）' }}</option>
        <option v-if="legacyValue && !sources.includes(legacyValue)" :value="legacyValue">{{ legacyValue }}（原有来源）</option>
        <option v-for="source in sources" :key="source" :value="source">{{ source }}</option>
      </select>
    </template>
  </van-field>
</template>

<script setup>
import sources from '../../../pkg/types/manual_lead_sources.json'
defineProps({ modelValue: { type: String, default: '' }, required: Boolean, legacyValue: { type: String, default: '' } })
defineEmits(['update:modelValue'])
</script>

<style scoped>
.lead-source-select { width: 100%; min-width: 0; min-height: var(--kx-control-current-height); padding: var(--kx-space-2) var(--kx-space-3); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text); font: inherit; transition: border-color var(--kx-motion-fast), background var(--kx-motion-fast); }
.lead-source-select:focus-visible { outline: var(--kx-focus-width) solid var(--kx-color-focus); outline-offset: var(--kx-focus-offset); border-color: var(--kx-color-brand); }
@media (max-width: 767px) { .lead-source-select { font-size: var(--kx-font-16); } }
</style>
