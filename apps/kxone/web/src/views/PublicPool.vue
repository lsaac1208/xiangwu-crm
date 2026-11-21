<template>
  <div class="pool-page">
    <div class="pool-container">
      <header class="pool-header">
        <div class="pool-title-row">
          <button type="button" class="icon-button" aria-label="返回" @click="router.push('/leads')">
            <van-icon aria-hidden="true" name="arrow-left" />
          </button>
          <div>
            <h1>{{ userStore.poolTitle() }}</h1>
            <p>{{ resultLabel }}</p>
          </div>
        </div>
        <button type="button" class="icon-button" aria-label="刷新" :disabled="loading" @click="load(true)">
          <van-icon aria-hidden="true" name="replay" />
        </button>
      </header>

      <div v-if="!userStore.isPoolEnabled()" class="pool-disabled-notice">
        公海已关闭，仅可查看和领取存量线索；不会有新线索进入公海。
      </div>

      <section class="pool-panel" aria-label="公海工作区">
        <div class="pool-toolbar">
          <van-search v-model="keyword" clearable placeholder="搜索姓名、城市、来源" @search="load(true)" @clear="load(true)" />
        </div>
        <CrmStatePanel
          v-if="loadError && !leads.length"
          state="error"
          title="公海线索加载失败"
          description="当前数据没有被修改，请检查网络后重试。"
          action-label="重新加载"
          @action="load(true)"
        />
        <div class="pool-table-scroll" tabindex="0" role="region" aria-label="公海线索列表">
          <table v-if="leads.length" class="pool-table">
            <thead><tr><th>客户 / 线索</th><th>联系方式</th><th>星级</th><th>来源</th><th>城市</th><th>入池原因</th><th>入池时间</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="item in leads" :key="item.id">
                <td><strong>{{ item.customer_name || '未命名客户' }}</strong><small>{{ item.lead_no }}</small></td>
                <td>{{ item.customer_mobile_masked || '-' }}</td>
                <td><CrmRate :model-value="item.star_rating || 0" readonly size="14" color="var(--kx-color-rating)" void-color="var(--kx-color-border-strong)" /></td>
                <td>{{ item.source_channel || '-' }}</td>
                <td>{{ item.city || '-' }}</td>
                <td>{{ reasonText(item.pool_reason) }}</td>
                <td>{{ formatDateTime(item.pool_entered_at) }}</td>
                <td><van-button v-if="canClaim" size="small" type="primary" :loading="claimingId === item.id" :disabled="claimingId !== null" @click="claim(item)">认领</van-button></td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="leads.length" class="pool-mobile-list">
          <article v-for="item in leads" :key="item.id" class="pool-item">
            <div class="pool-item-head"><strong>{{ item.customer_name || '未命名客户' }}</strong><span class="pool-reason">{{ reasonText(item.pool_reason) }}</span></div>
            <div class="pool-item-meta"><span class="pool-item-phone">{{ item.customer_mobile_masked || '-' }}</span><span>{{ item.city || '-' }}</span><span>{{ item.source_channel || '-' }}</span></div>
            <div class="pool-item-reference"><span>{{ item.lead_no }}</span><span>入池 {{ formatDateTime(item.pool_entered_at) }}</span></div>
            <div class="pool-item-foot">
              <CrmRate :model-value="item.star_rating || 0" readonly size="14" color="var(--kx-color-rating)" void-color="var(--kx-color-border-strong)" />
              <van-button v-if="canClaim" size="small" type="primary" :loading="claimingId === item.id" :disabled="claimingId !== null" @click="claim(item)">认领</van-button>
            </div>
          </article>
        </div>

        <CrmStatePanel v-if="loading && !leads.length" state="loading" title="正在加载公海线索" />
        <CrmStatePanel v-else-if="!loadError && !loading && !leads.length" :title="keyword.trim() ? '没有匹配的公海线索' : '公海暂无线索'" :description="keyword.trim() ? '请调整搜索词后重试。' : '暂时没有可认领的线索。'" />
        <CrmStatePanel v-if="loadError && leads.length" state="error" title="后续线索加载失败" description="已加载的线索仍可查看，重试将继续加载下一页。" action-label="重试加载更多" @action="load(false)" />
        <div v-if="leads.length" class="pool-footer" :class="{ 'pool-footer-complete': !hasMore }">
          <span>{{ resultLabel }}</span>
          <van-button v-if="hasMore && !loadError" size="small" :loading="loading" @click="load(false)">加载更多</van-button>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import CrmRate from '@/components/CrmRate.vue'
import { computed, h, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showSuccessToast, showFailToast } from 'vant'
import { claimPublicPoolLead, getPublicPool } from '@/api/lead'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import { useUserStore } from '@/stores/user'
import { formatDateTime } from '@/utils/date'

const router = useRouter()
const userStore = useUserStore()
const leads = ref([])
const loading = ref(false)
const loadError = ref(false)
const claimingId = ref(null)
const keyword = ref('')
const page = ref(1)
const total = ref(0)
const hasMore = ref(false)
let loadRequest = 0
let appliedKeyword = ''
const canClaim = computed(() => userStore.isInviter() || userStore.role === 'manager')
const resultLabel = computed(() => `共 ${total.value} 条，已加载 ${leads.value.length} 条`)

const load = async (reset) => {
  if (loading.value && !reset) return
  const request = ++loadRequest
  if (reset) { page.value = 1; leads.value = []; total.value = 0; hasMore.value = false; appliedKeyword = keyword.value.trim() }
  loading.value = true
  loadError.value = false
  try {
    const data = await getPublicPool({ page: page.value, page_size: 20, keyword: appliedKeyword })
    if (request !== loadRequest) return
    const items = data.leads || []
    leads.value.push(...items.filter(item => !leads.value.some(existing => existing.id === item.id)))
    total.value = Number(data.total || 0)
    hasMore.value = page.value < Number(data.total_pages || 0)
    page.value += 1
  } catch {
    if (request === loadRequest) loadError.value = true
  } finally { if (request === loadRequest) loading.value = false }
}

const claim = async (item) => {
  if (claimingId.value !== null) return
  claimingId.value = item.id
  try {
    const title = userStore.role === 'manager' ? '我来跟进' : '认领公海线索'
    await showConfirmDialog({ title, message: () => h('span', [h('span', { id: title, class: 'sr-only' }, title), `确认由您负责 ${item.customer_name || item.lead_no}？`]) })
    await claimPublicPoolLead(item.id)
    showSuccessToast('认领成功')
    await load(true)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') showFailToast('认领失败，请刷新确认线索状态后重试')
  } finally { claimingId.value = null }
}

const reasonText = (reason) => ({ unclaimed_timeout: '未领取', followup_overdue: '回访逾期', recoverable_invalid: '可回收无效', manual: '主管退回' }[reason] || reason || '-')

onMounted(() => load(true))
</script>

<style scoped>
.pool-page { min-height: 100dvh; background: var(--kx-color-canvas); color: var(--kx-color-text); }
.pool-container { max-width: var(--kx-page-max-width); margin: 0 auto; padding: var(--kx-space-6); }
.pool-header, .pool-title-row, .pool-item-head, .pool-item-foot, .pool-footer { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); }
.pool-title-row { min-width: 0; }
.pool-header h1 { margin: 0; font-size: var(--kx-font-26); font-weight: 650; letter-spacing: -.025em; line-height: 1.35; }
.pool-header p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.icon-button { display: inline-grid; flex-shrink: 0; width: var(--kx-control-current-height); height: var(--kx-control-current-height); place-items: center; border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.icon-button:hover:not(:disabled) { border-color: var(--kx-color-brand); color: var(--kx-color-brand); }
.pool-toolbar { padding: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.pool-toolbar :deep(.van-search) { max-width: 560px; padding: 0; background: transparent; }
.pool-disabled-notice { margin-top: var(--kx-space-4); padding: var(--kx-space-3) var(--kx-space-4); border-radius: var(--kx-control-radius); background: var(--kx-color-warning-soft); color: var(--kx-color-warning); font-size: var(--kx-font-13); line-height: 1.6; }
.pool-panel { min-width: 0; margin-top: var(--kx-space-6); box-shadow: var(--kx-shadow-subtle); overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
.pool-table-scroll { overflow-x: auto; }
.pool-table { width: 100%; min-width: 1120px; border-collapse: collapse; table-layout: fixed; }
.pool-table th, .pool-table td { height: 72px; padding: var(--kx-space-3) var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); color: var(--kx-color-text-secondary); text-align: left; font-size: var(--kx-font-14); line-height: 1.6; overflow-wrap: anywhere; }
.pool-table th { height: 44px; padding-block: var(--kx-space-2); background: var(--kx-table-header-bg); font-size: var(--kx-font-12); font-weight: 500; }
.pool-table th:first-child { width: 190px; }
.pool-table th:last-child { width: 92px; }
.pool-table tbody tr:hover { background: var(--kx-table-row-hover-bg); }
.pool-table strong, .pool-table small { display: block; overflow-wrap: anywhere; }
.pool-table strong { color: var(--kx-color-text); font-size: var(--kx-font-14); font-weight: 600; }
.pool-table small { margin-top: var(--kx-space-1); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.pool-mobile-list { display: none; }
.pool-footer { min-height: 64px; padding: var(--kx-space-3) var(--kx-space-4); color: var(--kx-color-text-muted); font-size: var(--kx-font-13); }
@media (max-width: 767px) {
  .pool-container { padding: var(--kx-space-4); }
  .pool-header { align-items: flex-start; gap: var(--kx-space-2); }
  .pool-title-row { gap: var(--kx-space-2); }
  .pool-title-row > div { min-width: 0; }
  .pool-header h1 { font-size: var(--kx-mobile-title-size); }
  .pool-header p { line-height: 1.5; }
  .icon-button { border: 0; border-radius: var(--kx-mobile-control-radius); color: var(--kx-color-brand); background: transparent; }
  .pool-toolbar { padding: 0; margin-bottom: var(--kx-space-4); border: 0; background: transparent; }
  .pool-table-scroll { display: none; }
  .pool-panel { margin-top: var(--kx-space-4); border: 0; background: transparent; box-shadow: none; overflow: visible; }
  .pool-mobile-list { display: grid; overflow: hidden; border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
  .pool-item { min-width: 0; padding: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-mobile-separator); }
  .pool-item:last-child { border-bottom: 0; }
  .pool-item-head { align-items: flex-start; }
  .pool-item-head strong { min-width: 0; font-size: var(--kx-mobile-heading-size); line-height: 1.4; overflow-wrap: anywhere; }
  .pool-reason { flex-shrink: 0; max-width: 48%; padding: var(--kx-space-1) var(--kx-space-2); border-radius: var(--kx-radius-1); background: var(--kx-color-mobile-fill); color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); line-height: 1.5; overflow-wrap: anywhere; }
  .pool-item-meta { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--kx-space-1) var(--kx-space-3); margin: var(--kx-space-3) 0 var(--kx-space-2); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.5; overflow-wrap: anywhere; }
  .pool-item-phone { grid-column: 1 / -1; color: var(--kx-color-text); font-size: var(--kx-font-14); font-variant-numeric: tabular-nums; }
  .pool-item-reference { display: grid; gap: var(--kx-space-1); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.5; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
  .pool-item-foot { margin-top: var(--kx-space-3); gap: var(--kx-space-4); }
  .pool-item-foot :deep(.van-button) { min-width: 88px; }
  .pool-footer { flex-wrap: wrap; min-height: 56px; padding: var(--kx-space-3) var(--kx-space-1); }
  .pool-footer-complete { display: none; }
}
</style>
