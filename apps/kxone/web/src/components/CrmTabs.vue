<template>
  <div class="crm-tabs">
    <div ref="tablist" class="crm-tablist" role="tablist" :aria-label="label" @keydown="onKeydown">
      <button
        v-for="(tab, index) in tabs"
        :id="tabID(tab.value)"
        :key="tab.value"
        :ref="el => setTabRef(el, index)"
        type="button"
        role="tab"
        :class="{ active: modelValue === tab.value }"
        :aria-selected="modelValue === tab.value"
        :aria-controls="panelID(tab.value)"
        :tabindex="modelValue === tab.value ? 0 : -1"
        @click="$emit('update:modelValue', tab.value)"
      >
        {{ tab.label }} <span v-if="tab.count !== undefined">{{ tab.count }}</span>
      </button>
    </div>
    <section
      :id="panelID(modelValue)"
      class="crm-tabpanel"
      role="tabpanel"
      :aria-labelledby="tabID(modelValue)"
      tabindex="0"
    >
      <slot :active="modelValue" />
    </section>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  modelValue: { type: String, required: true },
  tabs: { type: Array, required: true },
  label: { type: String, default: '内容分类' },
  idPrefix: { type: String, default: 'crm-tab' }
})
const emit = defineEmits(['update:modelValue'])
const tabRefs = ref([])

const tabID = value => `${props.idPrefix}-${value}`
const panelID = value => `${props.idPrefix}-${value}-panel`
const setTabRef = (el, index) => { if (el) tabRefs.value[index] = el }

const onKeydown = event => {
  const current = props.tabs.findIndex(tab => tab.value === props.modelValue)
  let next = current
  if (event.key === 'ArrowRight') next = (current + 1) % props.tabs.length
  else if (event.key === 'ArrowLeft') next = (current - 1 + props.tabs.length) % props.tabs.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = props.tabs.length - 1
  else return
  event.preventDefault()
  emit('update:modelValue', props.tabs[next].value)
  requestAnimationFrame(() => tabRefs.value[next]?.focus())
}
</script>

<style scoped>
.crm-tablist { display: flex; gap: var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); }
.crm-tablist button { position: relative; min-height: 48px; padding: 0; border: 0; background: transparent; color: var(--kx-color-text-muted); font-size: var(--kx-font-14); font-weight: 500; cursor: pointer; }
.crm-tablist button::after { position: absolute; right: 0; bottom: -1px; left: 0; height: 2px; background: transparent; content: ''; }
.crm-tablist button.active { font-weight: 600; color: var(--kx-color-text); }
.crm-tablist button.active::after { background: var(--kx-color-text); }
.crm-tablist button span { margin-left: var(--kx-space-1); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.crm-tabpanel:focus { outline-offset: calc(var(--kx-focus-offset) * -1); }
@media (max-width: 767px) { .crm-tablist { gap: 0; } .crm-tablist button { flex: 1; } }
</style>
