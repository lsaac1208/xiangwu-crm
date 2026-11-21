<template>
  <!-- renderAgentMarkdown disables raw HTML and sanitizes an explicit tag and attribute allowlist. -->
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div class="agent-markdown" v-html="html"></div>
</template>
<script setup>
import { computed } from 'vue'
import { renderAgentMarkdown } from '@/utils/agentAnswer'
const props = defineProps({ text: { type: String, default: '' } })
const html = computed(() => renderAgentMarkdown(props.text))
</script>
