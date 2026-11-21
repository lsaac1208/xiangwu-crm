<template>
  <span v-if="readonly" class="crm-rate" role="img" :aria-label="modelValue ? `意向 ${modelValue} 星，满分 5 星` : '尚未评级'">
    <span aria-hidden="true" class="crm-rate" :style="{ gap: `${gutter}px` }"><van-icon v-for="value in 5" :key="value" :name="value <= modelValue ? 'star' : voidIcon" :size="size" :color="value <= modelValue ? color : voidColor" /></span>
  </span>
  <span v-else class="crm-rate crm-rate-input" role="radiogroup" aria-label="意向星级" :style="{ gap: `${gutter}px` }">
    <label v-for="value in 5" :key="value" :style="{ fontSize: `${size}px` }">
      <input type="radio" :name="groupName" :value="value" :checked="modelValue === value" :aria-label="`${value} 星`" @change="$emit('update:modelValue', value)" />
      <van-icon aria-hidden="true" :name="value <= modelValue ? 'star' : voidIcon" :color="value <= modelValue ? color : voidColor" />
    </label>
  </span>
</template>
<script setup>
import { useId } from 'vue'
defineOptions({ inheritAttrs: false })
defineProps({
  modelValue: { type: Number, default: 0 }, readonly: Boolean,
  size: { type: [Number, String], default: 20 }, gutter: { type: [Number, String], default: 4 },
  color: { type: String, default: 'var(--kx-color-rating)' },
  voidColor: { type: String, default: 'var(--kx-color-border-strong)' }, voidIcon: { type: String, default: 'star-o' }
})
defineEmits(['update:modelValue'])
const groupName = `rating-${useId()}`
</script>
<style scoped>
.crm-rate { display: inline-flex; vertical-align: middle; }
.crm-rate-input label { position: relative; display: inline-flex; align-items: center; justify-content: center; cursor: pointer; min-width: 24px; min-height: var(--kx-control-current-height); }
.crm-rate-input :deep(.van-icon) { pointer-events: none; }
.crm-rate-input input { position: absolute; inset: 0; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: pointer; }
.crm-rate-input label:has(input:focus-visible) { outline: 2px solid var(--kx-color-brand); outline-offset: 2px; border-radius: var(--kx-radius-1); }
@media (max-width: 767px), (pointer: coarse) { .crm-rate-input { width: 220px; max-width: 100%; flex-wrap: wrap; gap: 0 !important; } .crm-rate-input label { flex: 0 0 44px; min-width: 44px; min-height: 44px; } }
</style>
