<template>
  <section class="agent-run-evidence" aria-label="本次分析的数据与查询依据">
    <p class="agent-run-snapshot">数据时点：<time v-if="snapshotAt" :datetime="snapshotAt">{{ formatTime(snapshotAt) }}</time><span v-else>{{ loading ? '正在读取…' : metadata ? '本次未建立数据快照' : '暂不可用' }}</span><span v-if="snapshotAt">（上海时间）</span></p>
    <p v-if="error" class="agent-evidence-error" role="alert">{{ error }} <button type="button" :disabled="loading" @click="load">重试读取依据</button></p>
    <details @toggle="onToggle">
      <summary>数据与查询依据</summary>
      <p v-if="loading" role="status">正在读取本次保存的结果…</p>
      <template v-else-if="loaded">
        <p class="agent-evidence-hint">最终采用的结果会随回答导出；查询过程可能包含已纠正或未采用的数据。打开客户记录会显示该客户的当前资料。</p>
        <p v-if="selection.notice" class="agent-evidence-hint">{{ selection.notice }}</p>
        <p v-if="!resultIds.length" class="agent-evidence-hint">本次没有保存数据查询结果。</p>
        <div v-if="selection.result_ids.length" aria-label="最终采用的查询结果">
          <h3 class="agent-evidence-title">{{ selection.status === 'legacy_presented' ? '历史回答明确展示的结果' : '最终采用的结果' }}</h3>
          <AgentResult v-for="id in selection.result_ids" :key="id" :block="resultBlock(id)" :results="results" @result="rememberResult" />
        </div>
        <details v-if="selection.process_result_ids.length" class="agent-query-process">
          <summary>查询过程（{{ selection.process_result_ids.length }} 项，未作为结论导出）</summary>
          <AgentResult v-for="id in selection.process_result_ids" :key="id" :block="resultBlock(id)" :results="results" @result="rememberResult" />
        </details>
      </template>
    </details>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { agentAPI } from '@/api/agent'
import AgentResult from './AgentResult.vue'

const props = defineProps({ runId: { type: String, required: true }, run: { type: Object, default: null } })
const emit = defineEmits(['result'])
const fetchedRun = ref(null), results = ref({}), resultIds = ref([]), loading = ref(false), loaded = ref(false), error = ref('')
const emptySelection = () => ({ status: 'unavailable', result_ids: [], process_result_ids: [], notice: '' })
const selection = ref(emptySelection())
const resultBlock = id => ({ type: 'table', title: `查询结果 ${resultIds.value.indexOf(id) + 1}`, result_id: id })
const metadata = computed(() => fetchedRun.value || (props.run?.id === props.runId ? props.run : null))
const snapshotAt = computed(() => metadata.value?.snapshot_at || '')
const formatTime = value => {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
}
let requestVersion = 0
const rememberResult = result => {
  if (!resultIds.value.includes(result.id)) return
  results.value = { ...results.value, [result.id]: result }
  emit('result', result)
}
const load = async () => {
  if (loading.value || !props.runId) return
  const version = ++requestVersion, id = props.runId
  loading.value = true; error.value = ''
  try {
    const response = await agentAPI.run(id)
    if (version !== requestVersion) return
    if (response.run?.id !== id || !Array.isArray(response.results)) throw new Error('本次分析依据不完整，请重试')
    const seen = new Set()
    for (const result of response.results) {
      if (!result?.id || seen.has(result.id) || !Array.isArray(result.columns) || !Array.isArray(result.rows)) throw new Error('本次分析依据格式不正确，请重试')
      seen.add(result.id)
    }
    const ids = response.results.map(result => result.id)
    const classification = response.result_selection || { status: 'unavailable', result_ids: [], process_result_ids: ids, notice: '历史回答未提供最终引用清单；以下结果仅供核对查询过程。' }
    if (!['explicit', 'legacy_presented', 'unavailable', 'unfinished'].includes(classification.status) || !Array.isArray(classification.result_ids) || !Array.isArray(classification.process_result_ids)) throw new Error('结果采用范围不完整，请重试')
    const classified = [...classification.result_ids, ...classification.process_result_ids]
    if (classified.length !== ids.length || new Set(classified).size !== ids.length || classified.some(id => !seen.has(id))) throw new Error('结果采用范围与保存结果不一致，请重试')
    fetchedRun.value = response.run
    results.value = Object.fromEntries(response.results.map(result => [result.id, result]))
    resultIds.value = response.results.map(result => result.id)
    selection.value = classification
    loaded.value = true
    for (const result of response.results) emit('result', result)
  } catch (failure) {
    if (version !== requestVersion) return
    error.value = failure.response?.data?.message || failure.message || '本次分析依据读取失败，请重试'
  } finally { if (version === requestVersion) loading.value = false }
}
const onToggle = event => { if (event.target.open && !loaded.value) load() }
watch(() => props.runId, () => {
  requestVersion++; fetchedRun.value = null; results.value = {}; resultIds.value = []; selection.value = emptySelection(); loaded.value = false; loading.value = false; error.value = ''
  // Conversation summaries usually supply the timestamp. Older paginated
  // messages can lack that summary, so fetch their own saved run once.
  if (!snapshotAt.value) load()
}, { immediate: true })
onBeforeUnmount(() => { requestVersion++ })
</script>

<style scoped>
.agent-run-evidence { min-width: 0; margin-top: 18px; padding-top: 12px; border-top: 1px solid var(--kx-color-border); }
.agent-run-snapshot, .agent-evidence-hint { margin: 0 0 10px; color: var(--kx-color-text-secondary); font-size: 12px; line-height: 1.7; overflow-wrap: anywhere; }
.agent-run-evidence summary { padding: 6px 0; color: var(--kx-color-text-secondary); font-size: 13px; cursor: pointer; }
.agent-evidence-title { margin: 14px 0 8px; font-size: 14px; }
.agent-evidence-error { font-size: 13px; line-height: 1.7; color: var(--kx-color-danger); }
.agent-run-evidence button { min-height: 36px; padding: 5px 9px; border: 1px solid var(--kx-color-border); border-radius: 8px; color: var(--kx-color-text); background: var(--kx-color-surface); }
</style>
