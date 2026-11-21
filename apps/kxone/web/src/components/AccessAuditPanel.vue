<template>
  <section class="audit-panel" aria-labelledby="audit-title">
    <h2 id="audit-title">客户资料访问记录</h2>
    <p>记录保留 90 天。可按账号、线索和日期查询。</p>
    <form class="audit-filters" @submit.prevent="load(1)">
      <label>账号 ID<input v-model="filters.account_id" type="number" min="1" /></label>
      <label>线索 ID<input v-model="filters.lead_id" type="number" min="1" /></label>
      <label>开始日期<input v-model="filters.start_date" type="date" /></label>
      <label>结束日期<input v-model="filters.end_date" type="date" /></label>
      <van-button native-type="submit" type="primary" :loading="loading">查询</van-button>
    </form>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-else-if="!loading && !items.length">暂无匹配记录</p>
    <ul class="audit-results" aria-live="polite">
      <li v-for="item in items" :key="item.id">
        <strong>账号 {{ item.account_id }} · {{ item.role }}</strong>
        <span>{{ formatTime(item.created_at) }} · {{ item.outcome === 'allowed' ? '允许' : '拒绝' }}</span>
        <span>{{ actionLabel(item.action) }} · {{ item.aggregate ? '汇总访问' : `${item.object_count} 个对象` }}</span>
        <span v-if="item.object_count">对象：{{ objectLabel(item.object_ids) }}</span>
        <small>请求编号：{{ item.request_id }}</small>
      </li>
    </ul>
    <nav class="audit-pages" aria-label="访问记录分页">
      <van-button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</van-button>
      <span>第 {{ page }} 页，共 {{ total }} 条</span>
      <van-button :disabled="page * 50 >= total || loading" @click="load(page + 1)">下一页</van-button>
    </nav>
  </section>
</template>
<script setup>
import { onMounted, reactive, ref } from 'vue'
import api from '@/api'
const filters = reactive({ account_id: '', lead_id: '', start_date: '', end_date: '' })
const loading = ref(false), error = ref(''), items = ref([]), page = ref(1), total = ref(0)
const load = async target => {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const params = { page: target, page_size: 50, ...Object.fromEntries(Object.entries(filters).filter(([, value]) => value)) }
    const result = await api.get('/admin/access-audits', { params })
    items.value = result.items; total.value = result.total; page.value = target
  } catch { error.value = '访问记录加载失败，请重试查询' } finally { loading.value = false }
}
const formatTime = value => new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
const objectLabel = value => { try { return JSON.parse(value).map(id => id.replace('lead:', '线索 ')).join('、') } catch { return '' } }
const actionLabel = value => {
  if (value.includes('/agent/')) return '分析历史'
  if (value.includes('/stats/')) return '统计看板'
  if (value.includes('/appointments')) return '预约资料'
  if (value.includes('/public-pool')) return '公海线索'
  if (value.includes('/workbench')) return '工作台'
  return value.endsWith('/:id') ? '线索详情' : '线索列表'
}
onMounted(() => load(1))
</script>
<style scoped>
.audit-panel { padding: var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
.audit-panel h2 { margin-top: 0; font-size: var(--kx-font-16); }
.audit-panel p, .audit-panel small { color: var(--kx-color-text-secondary); }
.audit-filters { display: flex; flex-wrap: wrap; align-items: end; gap: var(--kx-space-3); }
.audit-filters label { display: grid; gap: var(--kx-space-1); min-width: 0; }
.audit-filters input { width: 100%; min-width: 0; min-height: var(--kx-control-current-height); box-sizing: border-box; color: var(--kx-color-text); background: var(--kx-color-surface); border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); }
.audit-results { padding: 0; list-style: none; }
.audit-results li { display: grid; gap: var(--kx-space-2); padding: var(--kx-space-3) 0; border-bottom: 1px solid var(--kx-color-border); overflow-wrap: anywhere; }
.audit-pages { display: flex; flex-wrap: wrap; align-items: center; gap: var(--kx-space-3); }
@media (max-width: 767px) { .audit-filters label { width: calc(50% - var(--kx-space-3)); } }
</style>
