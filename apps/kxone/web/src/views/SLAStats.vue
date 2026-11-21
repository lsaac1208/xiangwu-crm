<template>
  <div class="statistics-report">
    <header class="report-page-head">
      <div><span class="report-eyebrow">管理 / 统计看板</span><h1>团队与渠道</h1><p>看清团队表现、来源质量，及时发现跟进问题。</p></div>
      <button type="button" class="report-button" aria-label="刷新数据" :disabled="statsLoading || supervisionLoading" @click="refreshStats"><van-icon name="replay" aria-hidden="true" /><span>刷新数据</span></button>
    </header>
    <section class="report-filterbar" aria-label="统计筛选">
      <div class="report-range" role="group" aria-label="线索录入日期"><button v-for="option in rangeOptions" :key="option.name" type="button" :aria-pressed="rangeKey === option.name" @click="selectRange(option.name)">{{ option.title }}</button></div>
      <div class="report-filter-actions"><button type="button" class="report-button" @click="openCalendar"><van-icon name="calendar-o" aria-hidden="true" /><span>{{ dateRangeLabel }}</span></button><button type="button" class="report-button" @click="openSalesPicker"><van-icon name="user-o" aria-hidden="true" />{{ selectedSalesLabel }}</button></div>
    </section>
    <div class="report-scope-row">
      <p class="report-scope-summary">团队/渠道按录入日期；监督看当前全部待办。<span v-if="largeSamplesOnly"> 仅比较 ≥20 条样本。</span></p>
      <details class="report-methodology"><summary>统计口径与样本筛选</summary>
        <p>按<strong>线索录入日期</strong>筛选，成交和跟进状态截至当前。<button type="button" class="report-link" @click="openLeadDrilldown('all')">核对线索明细 ↗</button><button v-if="selectedSalesUserID" type="button" class="report-link" @click="selectSales('')">清除人员筛选 ×</button></p>
        <p>团队按当前留存归属比较；邀约、谈单两组不相加。首跟率按可判断的责任记录计算，角色切换只影响人员对比。</p>
        <label><input v-model="largeSamplesOnly" type="checkbox" /> 仅比较 ≥20 条样本</label>
      </details>
    </div>
    <section v-if="!statsLoading && !loadError" class="report-brief" aria-label="本期简报">
      <div><span class="report-eyebrow">本期{{ teamRole === 'inviter' ? '邀约' : '谈单' }}归属简报</span><h2>{{ headline }}</h2><p>{{ briefSummary }}</p><p class="report-brief-facts"><button type="button" class="report-link" @click="openLeadDrilldown('all')">{{ dashboard.assignment.total }} 条线索 ↗</button><span>{{ dashboard.assignment.pending_dispatch }} 条待分配</span><span>{{ dashboard.assignment.pending_claim }} 条待领取</span><button v-if="mobileLayout && !supervisionLoading && !supervisionError" type="button" class="report-link" @click="selectMobileTab('supervision')">当前 {{ supervision.summary?.overdue_followup || 0 }} 条跟进逾期 ↗</button></p></div>
      <button type="button" class="report-brief-number" @click="openLeadDrilldown('converted')"><strong>{{ dashboard.assignment.converted }}</strong><span>范围内线索已成交 ↗</span></button>
    </section>
    <div v-if="mobileLayout" class="report-mobile-tabs" role="tablist" aria-label="统计模块">
      <button v-for="tab in mobileTabs" :id="`report-tab-${tab.key}`" :key="tab.key" type="button" role="tab" :aria-selected="mobileTab === tab.key" :aria-controls="`report-panel-${tab.key}`" :tabindex="mobileTab === tab.key ? 0 : -1" @click="selectMobileTab(tab.key)" @keydown="onTabKeydown($event, tab.key)">{{ tab.label }}</button>
    </div>
    <CrmStatePanel v-if="(statsLoading || loadError) && (!mobileLayout || mobileTab !== 'supervision')" :state="statsLoading ? 'loading' : 'error'" :title="statsLoading ? '正在加载统计数据' : '统计数据加载失败'" description="日期和人员筛选已保留，跟进监督可独立查看。" :action-label="statsLoading ? '' : '重新加载'" @action="loadStats" />
    <div class="report-brief-body">
      <div v-show="!mobileLayout || mobileTab !== 'supervision'" class="report-brief-main">
        <div v-show="!statsLoading && !loadError">
          <div id="report-panel-team" v-show="!mobileLayout || mobileTab === 'team'" :role="mobileLayout ? 'tabpanel' : undefined" :aria-labelledby="mobileLayout ? 'report-tab-team' : undefined">
          <TeamPerformance :rows="team" :role="teamRole" :sort-key="sortKey" :allowed="salesUsers" :selected="selectedSalesUserID" @role="teamRole = $event" @sort="sortKey = $event" @select="selectSales($event, teamRole)" />
          </div>
          <div id="report-panel-channels" v-show="!mobileLayout || mobileTab === 'channels'" :role="mobileLayout ? 'tabpanel' : undefined" :aria-labelledby="mobileLayout ? 'report-tab-channels' : undefined">
          <div class="report-section-label"><span>02</span><h2>来源带来了什么</h2></div>
          <ChannelPerformance :rows="sources" />
          </div>
        </div>
      </div>
      <aside id="report-panel-supervision" v-show="!mobileLayout || mobileTab === 'supervision'" :role="mobileLayout ? 'tabpanel' : undefined" :aria-labelledby="mobileLayout ? 'report-tab-supervision' : undefined"><div class="report-section-label"><span>03</span><h2>跟进监督</h2></div><FollowupSupervision :data="supervision" :loading="supervisionLoading" :error="supervisionError" :allowed="salesUsers" @retry="loadSupervision" /></aside>
    </div>
    <p v-if="dashboard.generated_at && !statsLoading && !loadError" class="report-updated">统计更新于 {{ formatDateTime(dashboard.generated_at) }} · 成交数量按线索计，未扣除退款。</p>
    <van-popup
      ref="calendarPopupRef"
      v-model:show="showCalendar"
      class="stats-calendar-popup"
      :position="mobileLayout ? 'bottom' : 'center'"
      :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }"
      aria-label="统计日期范围"
      aria-modal="true"
      round
      @keydown="onCalendarKeydown"
      @focusin="onCalendarFocus"
    >
      <div class="stats-popup-header"><h2 class="popup-title">统计日期范围</h2><button type="button" @click="showCalendar = false">关闭</button></div>
      <p class="sr-only">使用方向键移动日期，按回车或空格选择开始和结束日期。</p>
      <van-calendar
        :show="showCalendar"
        :poppable="false"
        :show-title="false"
        :show-subtitle="false"
        :lazy-render="false"
        type="range"
        allow-same-day
        :min-date="calendarMinDate"
        :max-date="calendarMaxDate"
        @confirm="onCalendarConfirm"
      >
        <template #text="{ date, text }"><span :data-calendar-date="date?.getTime()">{{ text }}</span></template>
      </van-calendar>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="salesPickerRef" aria-label="选择人员" aria-modal="true" v-model:show="showSalesPicker" class="stats-sales-popup" :position="mobileLayout ? 'bottom' : 'center'" round>
      <div class="stats-popup-header"><h2 class="popup-title">选择人员</h2><button type="button" @click="showSalesPicker = false">关闭</button></div>
      <van-search v-model="salesKeyword" placeholder="搜索姓名或 UserID" />
      <div class="sales-list">
        <van-cell
          title="全部人员"
          clickable
          class="sales-picker-option"
          :class="{ selected: isSelectedSales('') }"
          :aria-pressed="isSelectedSales('')"
          @click="selectSales('')"
          @keydown.enter.prevent="selectSales('')"
          @keydown.space.prevent="selectSales('')"
        >
          <template #right-icon>
            <van-icon aria-hidden="true" v-if="isSelectedSales('')" name="success" color="var(--kx-color-brand)" size="20" />
          </template>
        </van-cell>
        <van-cell
          v-for="item in filteredSalesUsers"
          :key="item.wecom_userid"
          :title="item.employee_name || item.wecom_userid"
          :label="item.wecom_userid"
          clickable
          class="sales-picker-option"
          :class="{ selected: isSelectedSales(item.wecom_userid) }"
          :aria-pressed="isSelectedSales(item.wecom_userid)"
          @click="selectSales(item.wecom_userid)"
          @keydown.enter.prevent="selectSales(item.wecom_userid)"
          @keydown.space.prevent="selectSales(item.wecom_userid)"
        >
          <template #right-icon>
            <van-icon
              aria-hidden="true"
              v-if="isSelectedSales(item.wecom_userid)"
              name="success"
              color="var(--kx-color-brand)"
              size="20"
            />
          </template>
        </van-cell>
        <van-empty v-if="filteredSalesUsers.length === 0" description="暂无可筛选人员" />
      </div>
    </van-popup>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, unref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { getManagementStats, getFollowupSupervision } from '@/api/stats'
import { getLeadOwnerFilters } from '@/api/user'
import { showFailToast } from 'vant'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import TeamPerformance from '@/components/stats/TeamPerformance.vue'
import ChannelPerformance from '@/components/stats/ChannelPerformance.vue'
import FollowupSupervision from '@/components/stats/FollowupSupervision.vue'
import { formatBusinessDate, formatCalendarDate, shiftISODate } from '@/utils/date'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import '@/components/stats/statistics-report.css'

const router = useRouter()
const { isMobile: mobileLayout } = useResponsiveLayout()
const mobileTab = ref('team')
const mobileTabs = [{ key: 'team', label: '团队' }, { key: 'channels', label: '渠道' }, { key: 'supervision', label: '跟进监督' }]
const selectMobileTab = key => {
  mobileTab.value = key
  nextTick(() => document.getElementById(`report-panel-${key}`)?.scrollIntoView({ block: 'start' }))
}
const onTabKeydown = (event, key) => {
  const index = mobileTabs.findIndex(tab => tab.key === key)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? mobileTabs.length - 1 :
    event.key === 'ArrowRight' ? (index + 1) % mobileTabs.length :
      event.key === 'ArrowLeft' ? (index + mobileTabs.length - 1) % mobileTabs.length : -1
  if (next < 0) return
  event.preventDefault()
  selectMobileTab(mobileTabs[next].key)
  nextTick(() => document.getElementById(`report-tab-${mobileTabs[next].key}`)?.focus())
}

const loadError = ref(false), statsLoading = ref(true), dashboard = ref({})
const supervision = ref({}), supervisionLoading = ref(true), supervisionError = ref(false)
let statsRequest = 0, supervisionRequest = 0, appliedRange = '30d'
const rangeKey = ref('30d'), teamRole = ref('inviter'), sortKey = ref('converted'), largeSamplesOnly = ref(false)
const showCalendar = ref(false), calendarPopupRef = ref(null)
useOverlayFocus(showCalendar, calendarPopupRef, () => { showCalendar.value = false })
const showSalesPicker = ref(false), salesPickerRef = ref(null)
useOverlayFocus(showSalesPicker, salesPickerRef, () => { showSalesPicker.value = false })
const salesUsers = ref([]), salesKeyword = ref(''), selectedSalesUserID = ref('')
const customStartDate = ref(null), customEndDate = ref(null)
const calendarMinDate = new Date(new Date().getFullYear() - 1, 0, 1), calendarMaxDate = new Date()
// Vant's day grid has no row semantics or keyboard selection. Adapt only this
// calendar instance to named date buttons, keeping Vant's range selection intact.
let calendarObserver
const calendarElement = () => unref(calendarPopupRef.value?.popupRef)
const calendarDays = () => [...(calendarElement()?.querySelectorAll('.van-calendar__day') || [])]
  .filter(day => day.querySelector('[data-calendar-date]'))
const availableCalendarDays = () => calendarDays().filter(day => day.getAttribute('aria-disabled') !== 'true')
const updateCalendarAccessibility = () => {
  const days = calendarDays()
  const enabled = days.filter(day => !day.classList.contains('van-calendar__day--disabled'))
  const active = enabled.includes(document.activeElement) ? document.activeElement :
    enabled.find(day => /van-calendar__day--(?:start|end|selected)/.test(day.className)) || enabled.at(-1)
  for (const day of days) {
    const date = new Date(Number(day.querySelector('[data-calendar-date]').dataset.calendarDate))
    const disabled = day.classList.contains('van-calendar__day--disabled')
    day.setAttribute('role', 'button')
    day.setAttribute('aria-label', `${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日`)
    day.setAttribute('aria-disabled', String(disabled))
    day.setAttribute('aria-pressed', String(/van-calendar__day--(?:start|end|middle|selected)/.test(day.className)))
    day.tabIndex = day === active ? 0 : -1
    const month = day.closest('.van-calendar__days')
    month.setAttribute('role', 'group')
    month.setAttribute('aria-label', `${date.getFullYear()}年${date.getMonth() + 1}月`)
  }
}
const onCalendarFocus = event => {
  const day = event.target.closest('.van-calendar__day')
  if (!day || day.getAttribute('aria-disabled') === 'true') return
  for (const item of availableCalendarDays()) item.tabIndex = item === day ? 0 : -1
}
const onCalendarKeydown = event => {
  const day = event.target.closest('.van-calendar__day')
  if (!day || day.getAttribute('aria-disabled') === 'true') return
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    day.click()
    return
  }
  const offsets = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
  const days = availableCalendarDays()
  const index = days.indexOf(day)
  const date = new Date(Number(day.querySelector('[data-calendar-date]').dataset.calendarDate))
  const offset = event.key === 'Home' ? -date.getDay() : event.key === 'End' ? 6 - date.getDay() : offsets[event.key]
  if (offset === undefined) return
  event.preventDefault()
  const next = days[Math.max(0, Math.min(days.length - 1, index + offset))]
  next?.focus({ preventScroll: true })
  next?.scrollIntoView({ block: 'nearest' })
}
watch(showCalendar, async open => {
  calendarObserver?.disconnect()
  if (!open) return
  await nextTick()
  const element = calendarElement()
  if (!element) return
  updateCalendarAccessibility()
  calendarObserver = new MutationObserver(updateCalendarAccessibility)
  calendarObserver.observe(element, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] })
})
onBeforeUnmount(() => calendarObserver?.disconnect())

const rangeOptions = [
  { title: '今日', name: 'today' },
  { title: '近7天', name: '7d' },
  { title: '近30天', name: '30d' },
  { title: '全部', name: 'all' },
  { title: '自定义', name: 'custom' }
]

const filteredSalesUsers = computed(() => {
  const keyword = salesKeyword.value.trim().toLowerCase()
  return salesUsers.value.filter(item => !keyword || (item.employee_name || '').toLowerCase().includes(keyword) || (item.wecom_userid || '').toLowerCase().includes(keyword))
})
const selectedSalesLabel = computed(() => !selectedSalesUserID.value ? '全部人员' : salesUsers.value.find(item => item.wecom_userid === selectedSalesUserID.value)?.employee_name || selectedSalesUserID.value)
const dateRangeLabel = computed(() => {
  const params = rangeParams()
  return params.start_date && params.end_date ? `${params.start_date} 至 ${params.end_date}` : '全部日期'
})
const roleRows = computed(() => (dashboard.value.teams || []).filter(row => row.role === teamRole.value && (!selectedSalesUserID.value || row.sales_userid === selectedSalesUserID.value)))
const team = computed(() => [...roleRows.value].filter(row => !largeSamplesOnly.value || row.assigned >= 20).sort((a,b) => b[sortKey.value] - a[sortKey.value] || b.assigned - a.assigned || a.sales_userid.localeCompare(b.sales_userid)))
const sources = computed(() => (dashboard.value.sources || []).filter(row => !largeSamplesOnly.value || row.total >= 20))
const leaders = computed(() => {
  const candidates = roleRows.value.filter(row => row.converted > 0)
  const best = Math.max(0,...candidates.map(row => row.converted))
  return candidates.filter(row => row.converted === best)
})
const headline = computed(() => {
  if (selectedSalesUserID.value) return `${selectedSalesLabel.value}的相关线索表现`
  if (!dashboard.value.assignment?.total) return '这个录入范围暂无线索'
  if (leaders.value.length === 1) return `${leaders.value[0].sales_name}的归属线索成交数领先`
  if (leaders.value.length > 1) return `${leaders.value.length} 位成员的归属线索成交数并列领先`
  return dashboard.value.assignment.converted > 0 ? '已有成交，当前样本仍需观察' : '本期线索还在推进中'
})
const briefSummary = computed(() => {
  if (selectedSalesUserID.value) return '渠道同步筛选这位人员参与的线索；跟进监督保留当前全部待办。'
  if (!dashboard.value.assignment?.total) return '换个日期范围继续查看；跟进监督仍独立展示。'
  if (leaders.value.length === 1) { const person = leaders.value[0]; return `${person.assigned} 条归属线索，${person.converted} 条已成交。${person.assigned < 20 ? '样本较少，继续观察，不据此判断成员优劣。' : '结合分配量、跟进情况和来源质量一起判断。'}` }
  return '对照同一录入范围的线索量、跟进和成交；小样本单独观察。'
})
const formatDateTime = value => new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
const rangeParams = () => {
  if (rangeKey.value === 'all') return {}
  if (rangeKey.value === 'custom') {
    if (!customStartDate.value || !customEndDate.value) {
      return {}
    }
    return {
      start_date: formatCalendarDate(customStartDate.value),
      end_date: formatCalendarDate(customEndDate.value)
    }
  }
  const today = formatBusinessDate()
  const offset = rangeKey.value === '7d' ? -6 : rangeKey.value === '30d' ? -29 : 0
  return {
    start_date: shiftISODate(today, offset),
    end_date: today
  }
}

const openLeadDrilldown = status => {
  const query = { ...rangeParams() }
  if (selectedSalesUserID.value) query.sales_userid = selectedSalesUserID.value
  if (status) query.status = status
  router.push({ path: '/leads', query })
}

const loadStats = async () => {
  const request = ++statsRequest
  statsLoading.value = true; loadError.value = false; appliedRange = rangeKey.value
  const params = rangeParams()
  if (selectedSalesUserID.value) params.sales_userid = selectedSalesUserID.value
  try { const data = await getManagementStats(params); if (!data?.assignment || !Array.isArray(data.teams) || !Array.isArray(data.sources)) throw new Error('invalid_statistics_response'); if (request === statsRequest) dashboard.value = data }
  catch { if (request === statsRequest) loadError.value = true }
  finally { if (request === statsRequest) statsLoading.value = false }
}
const loadSupervision = async () => {
  const request = ++supervisionRequest
  supervisionLoading.value = true; supervisionError.value = false
  try { const data = await getFollowupSupervision(); if (!data?.summary || !Array.isArray(data.owners) || !Array.isArray(data.tasks)) throw new Error('invalid_supervision_response'); if (request === supervisionRequest) supervision.value = data }
  catch { if (request === supervisionRequest) supervisionError.value = true }
  finally { if (request === supervisionRequest) supervisionLoading.value = false }
}
const refreshStats = () => Promise.allSettled([loadStats(), loadSupervision()])
const selectRange = async value => {
  if (value === 'custom') { showCalendar.value = true; return }
  if (rangeKey.value === value) return
  rangeKey.value = value; await loadStats()
}
const openCalendar = () => { showCalendar.value = true }
watch(showCalendar, open => { if (!open) rangeKey.value = appliedRange })
const onCalendarConfirm = async ([start,end]) => { customStartDate.value = start; customEndDate.value = end; rangeKey.value = 'custom'; appliedRange = 'custom'; showCalendar.value = false; await loadStats() }
const loadSalesUsers = async () => { const data = await getLeadOwnerFilters(); salesUsers.value = data.bindings || [] }
const openSalesPicker = async () => {
  try { if (!salesUsers.value.length) await loadSalesUsers(); showSalesPicker.value = true }
  catch { showFailToast('人员列表加载失败，请重试') }
}
const selectSales = async (userid, preferredRole) => {
  if (userid) teamRole.value = preferredRole || (salesUsers.value.find(user => user.wecom_userid === userid)?.role === 'closer' ? 'closer' : 'inviter')
  selectedSalesUserID.value = userid; showSalesPicker.value = false; await loadStats()
}
const isSelectedSales = userid => selectedSalesUserID.value === userid
onMounted(() => { void refreshStats(); void loadSalesUsers().catch(() => {}) })
onBeforeUnmount(() => { statsRequest++; supervisionRequest++ })
</script>
<style scoped>
.popup-title {
  margin: 0;
  font-size: var(--kx-font-20);
  font-weight: 600;
  color: var(--kx-color-text);
  text-align: left;
}
.stats-popup-header { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-4); padding: var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); }
.stats-popup-header button { min-height: var(--kx-control-current-height); padding: 0 var(--kx-space-3); border: 0; border-radius: var(--kx-control-radius); background: var(--kx-color-toolbar); color: var(--kx-color-text-secondary); cursor: pointer; }
.stats-calendar-popup :deep(.van-calendar) { flex: 1; min-height: 0; height: auto; }
.stats-calendar-popup { display: flex; flex-direction: column; overflow: hidden; width: min(640px, calc(100vw - 48px)); height: min(720px, calc(100dvh - 64px)); max-height: calc(100dvh - 64px); border-radius: var(--kx-dialog-radius); box-shadow: var(--kx-shadow-float); }
.stats-sales-popup { width: min(520px, calc(100vw - 48px)); max-height: calc(100dvh - 64px); overflow-y: auto; border-radius: var(--kx-dialog-radius); box-shadow: var(--kx-shadow-float); }

.sales-list {
  max-height: 55vh;
  overflow-y: auto;
  padding-bottom: 16px;
}

.sales-picker-option.selected {
  background: var(--kx-color-nav-selected);
}

:deep(.van-cell__value) {
  flex: 1.2;
  overflow-wrap: anywhere;
}
@media (max-width: 767px) {
  .stats-calendar-popup { width: 100%; height: 80dvh; max-height: 88dvh; border-radius: var(--kx-mobile-sheet-radius) var(--kx-mobile-sheet-radius) 0 0; }
  .stats-sales-popup { width: 100%; max-height: 88dvh; border-radius: var(--kx-mobile-sheet-radius) var(--kx-mobile-sheet-radius) 0 0; }
  .stats-popup-header { min-height: 56px; padding: var(--kx-space-2) var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .stats-popup-header .popup-title { font-size: var(--kx-mobile-heading-size); }
}
</style>
