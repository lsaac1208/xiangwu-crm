<template>
  <section class="agent-result" :aria-label="block.title || '查询结果'" :data-result-id="block.result_id">
    <h3 v-if="block.title">{{ block.title }}</h3>
    <p v-if="loading" role="status">正在读取查询结果…</p>
    <div v-else-if="error" role="alert"><p>{{ error }}</p><button type="button" @click="load">重试读取结果</button></div>
    <template v-else-if="result">
      <p v-if="block.type === 'chart' && chartLoading" role="status">正在读取回答时的图表范围…</p>
      <p v-else-if="block.type === 'chart' && chartError" role="alert">{{ chartError }} <button type="button" @click="load">重试读取图表</button></p>
      <AgentChart v-else-if="block.type === 'chart' && option" :option="option" :label="`${block.title || '数据图表'}，固定使用回答时的结果范围`" />
      <p v-else-if="block.type === 'chart'">图表维度不适用，请查看下方数据。</p>
      <component :is="block.type === 'chart' ? 'details' : 'div'" class="agent-result-data">
        <summary v-if="block.type === 'chart'">查看数据</summary>
        <div class="agent-table-scroll" tabindex="0" role="region" :aria-label="block.title ? `${block.title}数据表` : '查询数据表'">
          <table><caption class="sr-only">{{ block.title || '查询结果' }}</caption><thead><tr><th v-for="(column, index) in result.columns" :key="index" scope="col">{{ column }}</th></tr></thead><tbody><tr v-for="(row, index) in pageRows" :key="index"><td v-for="(value, column) in row" :key="column">{{ resultCell(value) }}</td></tr></tbody></table>
          <p v-if="!result.rows.length">没有匹配记录。</p>
        </div>
        <div v-if="result.rows.length > 50" class="agent-pagination"><button type="button" :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ Math.ceil(result.rows.length / 50) }}</span><button type="button" :disabled="page * 50 >= result.rows.length" @click="page++">下一页</button></div>
        <button v-if="canReadMore && !noMore" type="button" :disabled="moreLoading" class="agent-more-results" @click="loadMore">{{ moreLoading ? '正在读取已保存明细…' : '读取更多明细' }}</button>
        <p v-if="moreError" role="alert">{{ moreError }}</p>
      </component>
      <details class="agent-result-basis">
        <summary>依据</summary>
        <p class="agent-result-meta">数据时点 {{ formatTime(result.snapshot_at) }} · 共 {{ result.total_rows ?? result.rows.length }} 行<span v-if="result.truncated">，当前已读取 {{ result.rows.length }} 行预览</span></p>
        <p v-if="result.materialized_complete === false" class="agent-result-meta">本次保存的明细达到上限，仅保存 {{ result.materialized_rows }} 行；导出也包含此范围，请缩小条件后分析。</p>
        <p v-if="result.coverage" class="agent-result-meta">分析范围：{{ result.coverage }}</p>
        <p v-if="block.type === 'chart' && chartResult" class="agent-result-meta">{{ chartResult.rows.length ? `图表固定显示第${chartResult.offset + 1}至${chartResult.offset + chartResult.rows.length}行` : '图表保存范围为空' }}；下方表格可继续读取已保存明细，导出包含已保存数据。</p>
        <p v-if="noMore" class="agent-result-meta">本次回答没有保存更多明细；已读取的范围和结果仍可核对。</p>
        <pre v-if="result.query" class="agent-query" tabindex="0" aria-label="查询依据">{{ result.query }}</pre>
        <template v-if="result.query">
          <p class="agent-result-meta">绑定参数（按绑定顺序，JSON；[] 表示无绑定参数）：</p>
          <pre v-if="Array.isArray(result.query_params)" class="agent-query" tabindex="0" aria-label="查询绑定参数">{{ JSON.stringify(result.query_params, null, 2) }}</pre>
          <p v-else class="agent-result-meta" aria-label="查询绑定参数">未保存：该结果缺少绑定参数，含占位符的筛选值无法核对。</p>
        </template>
        <div v-if="sources.length" class="agent-source-list"><span>依据记录</span><router-link v-for="(source, index) in sources" :key="index" :to="sourcePath(source)" :aria-disabled="!source.lead_id" @click="!source.lead_id && $event.preventDefault()">{{ source.label || `记录 ${source.id}` }}</router-link></div>
      </details>
    </template>
  </section>
</template>

<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { agentAPI } from '@/api/agent'
import { chartOption, chartRange, fixedChartResult, resultCell, safeResult } from '@/utils/agentAnswer'

const AgentChart = defineAsyncComponent(() => import('./AgentChart.vue'))
const route = useRoute()
const sourcePath = source => source.lead_id ? { path: `/leads/${source.lead_id}`, query: { return_to: route.fullPath } } : '#'
const props = defineProps({ block: { type: Object, required: true }, results: { type: Object, default: () => ({}) } })
const emit = defineEmits(['result'])
const loadedResult = ref(null), loading = ref(false), error = ref(''), page = ref(1), moreLoading = ref(false), moreError = ref(''), noMore = ref(false)
const chartResult = ref(null), chartLoading = ref(false), chartError = ref('')
const result = computed(() => loadedResult.value || safeResult(props.block, props.results))
const pageRows = computed(() => result.value?.rows.slice((page.value - 1) * 50, page.value * 50) || [])
const canReadMore = computed(() => result.value && result.value.rows.length < (result.value.materialized_rows ?? result.value.total_rows))
const sources = computed(() => result.value?.sources || [])
const colors = () => ['--kx-color-brand', '--kx-green-600', '--kx-amber-600', '--kx-primitive-hex-0891b2'].map(token => getComputedStyle(document.documentElement).getPropertyValue(token).trim()).filter(Boolean)
const option = computed(() => chartOption(props.block, chartResult.value, colors()))
let requestVersion = 0
const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '未知'
const loadChart = async (version, available) => {
  if (props.block.type !== 'chart') return
  chartLoading.value = true
  try {
    let fixed = fixedChartResult(props.block, available)
    if (!fixed) {
      const range = chartRange(props.block)
      const response = await agentAPI.result(props.block.result_id, range.offset, range.limit || 1)
      if (version !== requestVersion) return
      fixed = fixedChartResult(props.block, response.result || response)
    }
    if (!fixed) throw new Error('回答时的图表范围读取不完整')
    if (version === requestVersion) chartResult.value = fixed
  } catch (failure) {
    if (version === requestVersion) chartError.value = failure.response?.data?.message || failure.message || '图表读取失败，请重试'
  } finally { if (version === requestVersion) chartLoading.value = false }
}
const load = async () => {
  const version = ++requestVersion
  error.value = ''; loadedResult.value = null; chartResult.value = null; chartError.value = ''
  const available = safeResult(props.block, props.results)
  if (available) { await loadChart(version, available); return }
  if (!props.block.result_id) { error.value = '回答未关联可核对的查询结果'; return }
  loading.value = true
  try {
    const response = await agentAPI.result(props.block.result_id)
    if (version !== requestVersion) return
    const candidate = response.result || response
    if (!safeResult(props.block, { [props.block.result_id]: candidate })) throw new Error('查询结果格式不正确')
    loadedResult.value = candidate
    emit('result', candidate)
    await loadChart(version, candidate)
  } catch (failure) { if (version === requestVersion) error.value = failure.response?.data?.message || failure.message || '结果读取失败，请重试' }
  finally { if (version === requestVersion) loading.value = false }
}
const loadMore = async () => {
  if (!result.value || moreLoading.value) return
  const version = requestVersion, current = result.value
  moreLoading.value = true; moreError.value = ''
  try {
    const response = await agentAPI.result(current.id, current.rows.length, 200), next = response.result || response
    if (version !== requestVersion) return
    if (!safeResult(props.block, { [current.id]: next }) || JSON.stringify(next.columns) !== JSON.stringify(current.columns)) throw new Error('已保存结果的结构不一致')
    if (!next.rows.length) { noMore.value = true; return }
    const rows = [...current.rows, ...next.rows]
    loadedResult.value = { ...current, rows, returned_rows: rows.length, offset: 0, has_more: next.has_more, materialized_rows: next.materialized_rows ?? current.materialized_rows, materialized_complete: next.materialized_complete ?? current.materialized_complete, truncated: rows.length < current.total_rows }
    emit('result', loadedResult.value)
  } catch (failure) { if (version === requestVersion) moreError.value = failure.response?.data?.message || '更多明细读取失败，请重试' }
  finally { if (version === requestVersion) moreLoading.value = false }
}
watch(() => props.block.result_id, () => { page.value = 1; noMore.value = false; moreError.value = ''; load() }, { immediate: true })
onBeforeUnmount(() => { requestVersion++ })
</script>

<style scoped>
.agent-result { min-width: 0; margin: 12px 0; padding: 12px 14px; border: 1px solid var(--kx-color-border); border-radius: 14px; background: var(--kx-color-surface); }
.agent-result h3 { margin: 0 0 8px; font-size: 14px; font-weight: 600; color: var(--kx-color-text-secondary); }
.agent-result-meta { margin: 0 0 12px; color: var(--kx-color-text-secondary); font-size: 12px; overflow-wrap: anywhere; }
.agent-chart { width: 100%; height: 300px; min-width: 0; }
.agent-result-basis { margin-top: 10px; }
.agent-result-basis summary, .agent-result-data summary { cursor: pointer; color: var(--kx-color-text-secondary); font-size: 13px; }
.agent-table-scroll { position: relative; max-width: 100%; overflow: auto; margin-top: 12px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
th, td { max-width: 280px; padding: 9px 12px; border-bottom: 1px solid var(--kx-color-border); white-space: nowrap; }
th { background: var(--kx-color-surface-subtle); font-weight: 600; }
.agent-pagination { display: flex; justify-content: center; align-items: center; gap: 12px; margin-top: 12px; font-size: 12px; }
.agent-more-results { display: block; margin: 12px auto; }
.agent-result button { min-height: 36px; padding: 6px 12px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-result button:disabled { opacity: .5; }
.agent-query { max-width: 100%; overflow: auto; color: var(--kx-color-text-secondary); font-size: 12px; }
.agent-source-list { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; font-size: 12px; }
.agent-source-list a { color: var(--kx-color-brand); }
.agent-source-list [aria-disabled=true] { color: var(--kx-color-text-secondary); text-decoration: none; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
@media (max-width: 767px) { .agent-result { padding: 12px; } .agent-chart { height: 240px; } }
</style>
