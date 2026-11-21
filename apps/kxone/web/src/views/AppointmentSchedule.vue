<template>
  <div class="schedule-page">
    <section v-if="!userStore.isAppointmentEnabled()" class="feature-disabled" role="status">
      <van-icon aria-hidden="true" name="calendar-o" size="42" />
      <h1>预约排班暂未启用</h1>
      <p>管理员可在“系统设置 → 业务规则”中启用预约排班。</p>
      <van-button type="primary" @click="router.push('/leads')">返回工作台</van-button>
    </section>
    <template v-else>
    <header class="schedule-header">
      <div>
        <h1>{{ userStore.isCloser() ? '我的谈单日程' : '预约排班中心' }}</h1>
        <p>{{ rangeLabel }} · {{ activeAppointments.length }} 个有效预约</p>
      </div>
      <div class="header-actions">
        <van-button v-if="userStore.isManager()" plain type="primary" @click="actionError = ''; showAvailability = true">可用时间</van-button>
        <van-button icon="replay" @click="loadRange">刷新</van-button>
        <van-button plain @click="router.push('/leads')">返回工作台</van-button>
      </div>
    </header>

    <section class="schedule-toolbar">
      <div class="date-navigation">
        <button type="button" :aria-label="viewMode === 'week' ? '上一周' : '上一天'" :title="viewMode === 'week' ? '上一周' : '上一天'" @click="moveRange(-1)"><van-icon aria-hidden="true" name="arrow-left" /></button>
        <input v-model="anchorDate" type="date" aria-label="选择日程日期" @change="loadRange" />
        <button type="button" :aria-label="viewMode === 'week' ? '下一周' : '下一天'" :title="viewMode === 'week' ? '下一周' : '下一天'" @click="moveRange(1)"><van-icon aria-hidden="true" name="arrow" /></button>
        <button type="button" class="today-button" @click="goToday">今天</button>
      </div>
      <div class="schedule-view-switch" role="group" aria-label="日程视图">
        <button type="button" :aria-pressed="viewMode === 'day'" @click="viewMode = 'day'; loadRange()">日视图</button>
        <button type="button" :aria-pressed="viewMode === 'week'" @click="viewMode = 'week'; loadRange()">周视图</button>
      </div>
    </section>

    <CrmStatePanel
      v-if="loadError"
      state="error"
      title="日程加载失败"
      description="已保留当前日期和视图，可直接重试。"
      action-label="重新加载"
      @action="loadRange"
    />
    <CrmStatePanel v-else-if="loading" state="loading" title="正在加载日程" />

    <section v-if="!loading && !loadError && pendingAppointments.length" class="pending-section">
      <div class="section-title"><h2>待分配谈单师</h2><span>{{ pendingAppointments.length }} 单</span></div>
      <div class="pending-grid">
        <article v-for="item in pendingAppointments" :key="item.id" class="appointment-card pending-card">
          <div class="card-time">{{ formatDay(item.start_at) }} · {{ formatTimeRange(item) }}</div>
          <h3>{{ item.customer_name || item.lead_no }}</h3>
          <p>{{ item.campus }} · {{ item.visitor_count }} 人</p>
          <small>{{ item.handoff_notes }}</small>
          <p v-if="item.handoff_pending" role="status">待 {{ item.pending_inviter_name || item.pending_inviter_userid }} 领取，领取后可确认到访</p>
          <div class="card-actions"><button v-if="userStore.isManager()" type="button" @click="openAssign(item)">分配谈单师</button><button v-if="canCancel(item)" type="button" @click="openReschedule(item)">改期</button><button v-if="canCancel(item)" type="button" @click="openAction(item, 'cancel')">取消</button></div>
        </article>
      </div>
    </section>

    <section v-if="!loading && !loadError" class="calendar-section">
      <div class="section-title"><h2>{{ viewMode === 'day' ? '当日日程' : '本周日程' }}</h2><span>已排 {{ scheduledAppointments.length }} 单</span></div>
      <p v-if="viewMode === 'week'" class="swipe-hint">左右滑动查看整周日程</p>
      <div tabindex="0" role="region" aria-label="排班日程" class="day-grid" :class="{ 'day-grid--week': viewMode === 'week' }">
        <article v-for="day in days" :key="day.key" class="day-column" :class="{ 'day-column--today': day.key === localDateKey(new Date()) }">
          <header><strong>{{ day.label }}<small v-if="day.key === localDateKey(new Date())" class="today-label">今天</small></strong><span>{{ day.items.length }} 单</span></header>
          <div v-if="day.items.length" class="day-items">
            <article v-for="item in day.items" :key="item.id" class="appointment-card scheduled-card" :class="{ 'appointment-card--selected': selected?.id === item.id && (showAssign || showReschedule || showAction || showArrival) }">
              <div class="card-time">{{ formatTimeRange(item) }}</div>
              <h3>{{ item.customer_name || item.lead_no }}</h3>
              <p>{{ item.closer_name || item.closer_userid }} · {{ item.campus }}</p>
              <small>邀约：{{ item.inviter_name || item.inviter_userid }}</small>
              <p v-if="item.handoff_pending" role="status">待 {{ item.pending_inviter_name || item.pending_inviter_userid }} 领取，领取后可确认到访</p>
              <div class="card-actions">
                <button v-if="isMobile && canConfirmArrival(item)" type="button" class="deliver" @click="openArrival(item)">确认到访并交付</button>
                <button v-if="userStore.isManager()" type="button" @click="openAssign(item)">换人</button>
                <button v-if="userStore.isManager()" type="button" @click="openReschedule(item)">改期</button>
                <button v-if="!isMobile && canConfirmArrival(item)" type="button" class="deliver" @click="openArrival(item)">确认到访并交付</button>
                <button v-if="canCancel(item)" type="button" @click="openAction(item, 'cancel')">取消</button>
                <button v-if="canNoShow(item)" type="button" class="danger" @click="openAction(item, 'no_show')">爽约</button>
              </div>
            </article>
          </div>
          <div v-else class="empty-day">暂无预约</div>
        </article>
      </div>
    </section>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showAssignPopup" aria-label="分配谈单师" aria-modal="true" v-model:show="showAssign" :close-on-click-overlay="!saving" :position="isMobile ? 'bottom' : 'center'" round class="action-popup">
      <div class="popup-heading"><h2>{{ selected?.closer_userid ? '更换谈单师' : '分配谈单师' }}</h2><button type="button" :disabled="saving" @click="showAssign = false">关闭</button></div>
      <p v-if="checkingConflicts" class="checking-status" role="status">正在检查排班冲突…</p>
      <div v-if="conflictError" class="operation-error" role="alert"><p>{{ conflictError }}</p><van-button size="small" @click="checkConflicts">重试检查</van-button></div>
      <van-radio-group v-model="assignForm.closer_userid" @change="checkConflicts">
        <van-cell-group inset>
          <van-cell v-for="closer in closers" :key="closer.wecom_userid" :title="closer.employee_name || closer.wecom_userid" :label="closer.wecom_userid" @click="assignForm.closer_userid = closer.wecom_userid">
            <template #right-icon><van-radio :name="closer.wecom_userid" :aria-label="closer.employee_name || closer.wecom_userid" /></template>
          </van-cell>
        </van-cell-group>
      </van-radio-group>
      <div v-if="conflicts.length" class="conflict-panel">
        <strong>排班冲突</strong>
        <p v-for="item in conflicts" :key="`${item.code}-${item.appointment_id}`" :class="item.level">{{ item.level === 'hard' ? '硬冲突' : '提醒' }}：{{ item.message }}</p>
      </div>
      <van-field v-if="hasSoftConflict" v-model="assignForm.override_reason" label="强制原因" type="textarea" rows="2" placeholder="主管确认软冲突后填写" />
      <p v-if="actionError" class="operation-error" role="alert">{{ actionError }} <button type="button" :disabled="saving" @click="refreshSelectedVersion">刷新预约版本（保留输入）</button></p>
      <div class="popup-submit"><van-button type="primary" block :disabled="checkingConflicts || Boolean(conflictError) || hasHardConflict || !assignForm.closer_userid" :loading="saving" @click="submitAssign">确认安排</van-button></div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showReschedulePopup" aria-label="预约改期" aria-modal="true" v-model:show="showReschedule" :close-on-click-overlay="!saving" :position="isMobile ? 'bottom' : 'center'" round class="action-popup">
      <div class="popup-heading"><h2>预约改期</h2><button type="button" :disabled="saving" @click="showReschedule = false">关闭</button></div>
      <van-cell-group inset>
        <van-field v-model="rescheduleForm.start_at" type="datetime-local" label="开始时间" />
        <van-field v-model="rescheduleForm.end_at" type="datetime-local" label="结束时间" />
        <van-field v-model="rescheduleForm.reason" label="改期原因" type="textarea" rows="2" />
        <van-field v-if="hasSoftConflict" v-model="rescheduleForm.override_reason" label="冲突确认" type="textarea" rows="2" />
      </van-cell-group>
      <div v-if="conflicts.length" class="conflict-panel">
        <strong>改期冲突</strong>
        <p v-for="item in conflicts" :key="`${item.code}-${item.appointment_id}`" :class="item.level">{{ item.level === 'hard' ? '硬冲突' : '提醒' }}：{{ item.message }}</p>
      </div>
      <p v-if="actionError" class="operation-error" role="alert">{{ actionError }} <button type="button" :disabled="saving" @click="refreshSelectedVersion">刷新预约版本（保留输入）</button></p>
      <div class="popup-submit"><van-button type="primary" block :loading="saving" @click="submitReschedule">确认改期</van-button></div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showActionPopup" aria-label="预约状态" aria-modal="true" v-model:show="showAction" :close-on-click-overlay="!saving" :position="isMobile ? 'bottom' : 'center'" round class="action-popup">
      <div class="popup-heading"><h2>{{ actionMode === 'no_show' ? '登记客户爽约' : '取消预约' }}</h2><button type="button" :disabled="saving" @click="showAction = false">关闭</button></div>
      <van-field v-model="actionReason" label="原因" type="textarea" rows="3" placeholder="必须填写" />
      <p v-if="actionError" class="operation-error" role="alert">{{ actionError }} <button type="button" :disabled="saving" @click="refreshSelectedVersion">刷新预约版本（保留输入）</button></p>
      <div class="popup-submit"><van-button :type="actionMode === 'no_show' ? 'danger' : 'warning'" block :loading="saving" @click="submitAction">确认提交</van-button></div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showArrivalPopup" aria-label="确认到访" aria-modal="true" v-model:show="showArrival" :close-on-click-overlay="!saving" :position="isMobile ? 'bottom' : 'center'" round class="action-popup">
      <div class="popup-heading"><h2>确认到访并交付</h2><button type="button" :disabled="saving" @click="showArrival = false">关闭</button></div>
      <div class="delivery-summary">
        <strong>{{ selected?.customer_name || selected?.lead_no }}</strong>
        <p>确认后线索将进入谈单阶段，并交付给 {{ selected?.closer_name || selected?.closer_userid }}。邀约师仅保留查看权限。</p>
      </div>
      <van-field v-model="arrivalConfirmation" label="交接确认" type="textarea" rows="3" maxlength="200" show-word-limit placeholder="例如：客户已到校，已与谈单师当面交接" />
      <p v-if="actionError" class="operation-error" role="alert">{{ actionError }} <button type="button" :disabled="saving" @click="refreshSelectedVersion">刷新预约版本（保留输入）</button></p>
      <div class="popup-submit"><van-button type="primary" block :loading="saving" @click="submitArrival">确认客户已到访</van-button></div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showAvailabilityPopup" aria-label="设置可用时间" aria-modal="true" v-model:show="showAvailability" :close-on-click-overlay="!saving" :position="isMobile ? 'bottom' : 'center'" round class="action-popup">
      <div class="popup-heading"><h2>设置谈单师时间</h2><button type="button" :disabled="saving" @click="showAvailability = false">关闭</button></div>
      <van-cell-group inset>
        <van-field label="谈单师">
          <template #input>
            <select v-model="availabilityForm.closer_userid" aria-label="谈单师"><option v-for="item in closers" :key="item.wecom_userid" :value="item.wecom_userid">{{ item.employee_name || item.wecom_userid }}</option></select>
          </template>
        </van-field>
        <van-field v-model="availabilityForm.start_at" type="datetime-local" label="开始时间" />
        <van-field v-model="availabilityForm.end_at" type="datetime-local" label="结束时间" />
        <van-field label="类型">
          <template #input><select v-model="availabilityForm.kind" aria-label="时间类型"><option value="working">可工作</option><option value="unavailable">不可用</option></select></template>
        </van-field>
        <van-field v-model="availabilityForm.capacity" type="digit" label="并发容量" />
        <van-field v-model="availabilityForm.campus" label="校区" />
        <van-field v-model="availabilityForm.reason" label="说明" />
      </van-cell-group>
      <p v-if="actionError" class="operation-error" role="alert">{{ actionError }} <button type="button" :disabled="saving" @click="refreshSelectedVersion">刷新预约版本（保留输入）</button></p>
      <div class="popup-submit"><van-button type="primary" block :loading="saving" @click="submitAvailability">保存时间设置</van-button></div>
      <div v-if="availabilityItems.length" class="availability-list">
        <strong>当前范围内的时间设置</strong>
        <div v-for="item in availabilityItems" :key="item.id" class="availability-item">
          <div>
            <span>{{ item.kind === 'unavailable' ? '不可用' : '可工作' }}</span>
            <small>{{ formatDay(item.start_at) }} {{ formatTimeRange(item) }} · {{ item.campus || '不限校区' }}</small>
          </div>
          <button type="button" @click="removeAvailability(item)">删除</button>
        </div>
      </div>
    </van-popup>
    </template>
  </div>
</template>

<script setup>
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { showFailToast, showSuccessToast } from 'vant'
import {
  assignAppointmentCloser,
  cancelAppointment,
  confirmAppointmentArrival,
  createCloserAvailability,
  deleteCloserAvailability,
  detectAppointmentConflicts,
  getAppointments,
  getCloserAvailability,
  noShowAppointment,
  rescheduleAppointment
} from '@/api/appointment'
import { getCloserUsers } from '@/api/user'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import { useUserStore } from '@/stores/user'
import { localDateKey, localDateTime, businessDate, businessISO } from '@/utils/appointmentDate'

const router = useRouter()
const userStore = useUserStore()
const { isMobile } = useResponsiveLayout()
const viewMode = ref(isMobile.value ? 'day' : 'week')
const anchorDate = ref(localDateKey(new Date()))
const appointments = ref([])
const closers = ref([])
const conflicts = ref([])
const availabilityItems = ref([])
const selected = ref(null)
const saving = ref(false)
const actionError = ref('')
const loading = ref(false)
const loadError = ref(false)
const showAssign = ref(false)
const showAssignPopup = ref(null)
useOverlayFocus(showAssign, showAssignPopup, () => { if (!saving.value) showAssign.value = false })
const showReschedule = ref(false)
const showReschedulePopup = ref(null)
useOverlayFocus(showReschedule, showReschedulePopup, () => { if (!saving.value) showReschedule.value = false })
const showAction = ref(false)
const showActionPopup = ref(null)
useOverlayFocus(showAction, showActionPopup, () => { if (!saving.value) showAction.value = false })
const showArrival = ref(false)
const showArrivalPopup = ref(null)
useOverlayFocus(showArrival, showArrivalPopup, () => { if (!saving.value) showArrival.value = false })
const showAvailability = ref(false)
const showAvailabilityPopup = ref(null)
useOverlayFocus(showAvailability, showAvailabilityPopup, () => { if (!saving.value) showAvailability.value = false })
const actionMode = ref('cancel')
const actionReason = ref('')
const arrivalConfirmation = ref('')

const assignForm = reactive({ closer_userid: '', override_reason: '' })
const rescheduleForm = reactive({ start_at: '', end_at: '', reason: '', override_reason: '' })
const nextAvailabilityStart = new Date(Date.now() + 60 * 60 * 1000)
nextAvailabilityStart.setMinutes(0, 0, 0)
const nextAvailabilityEnd = new Date(nextAvailabilityStart.getTime() + 8 * 60 * 60 * 1000)
const availabilityForm = reactive({ closer_userid: '', start_at: localDateTime(nextAvailabilityStart), end_at: localDateTime(nextAvailabilityEnd), kind: 'working', capacity: '1', campus: '襄阳校区', reason: '' })

const dayStart = computed(() => {
  const date = businessDate(`${anchorDate.value}T00:00:00`)
  if (viewMode.value === 'week') date.setTime(date.getTime() - ((new Date(date.getTime() + 8 * 3600000).getUTCDay() + 6) % 7) * 86400000)
  return date
})
const rangeEnd = computed(() => new Date(dayStart.value.getTime() + (viewMode.value === 'week' ? 7 : 1) * 86400000))
const rangeLabel = computed(() => `${dayStart.value.toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })} - ${new Date(rangeEnd.value.getTime() - 1).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })}`)
const pendingAppointments = computed(() => appointments.value.filter(item => item.status === 'pending_assignment'))
const scheduledAppointments = computed(() => appointments.value.filter(item => item.status === 'scheduled'))
const activeAppointments = computed(() => appointments.value.filter(item => ['pending_assignment', 'scheduled'].includes(item.status)))
const hasHardConflict = computed(() => conflicts.value.some(item => item.level === 'hard'))
const hasSoftConflict = computed(() => conflicts.value.some(item => item.level === 'soft'))

const days = computed(() => {
  const count = viewMode.value === 'week' ? 7 : 1
  return Array.from({ length: count }, (_, index) => {
    const date = new Date(dayStart.value.getTime() + index * 86400000)
    const key = localDateKey(date)
    return { key, label: date.toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai', month: 'numeric', day: 'numeric', weekday: 'short' }), items: scheduledAppointments.value.filter(item => localDateKey(item.start_at) === key) }
  })
})

let appointmentsRequest = 0
let availabilityRequest = 0
let rangeRequest = 0
const loadAll = async () => {
  const request = ++appointmentsRequest
  const data = await getAppointments({ start_at: dayStart.value.toISOString(), end_at: rangeEnd.value.toISOString() })
  if (request === appointmentsRequest) appointments.value = data.appointments || []
}

const loadClosers = async () => {
  if (!userStore.isManager()) return
  const data = await getCloserUsers()
  closers.value = data.bindings || []
  if (!availabilityForm.closer_userid && closers.value.length) availabilityForm.closer_userid = closers.value[0].wecom_userid
}

const loadAvailability = async () => {
  if (!userStore.isManager()) return
  const request = ++availabilityRequest
  const data = await getCloserAvailability({ start_at: dayStart.value.toISOString(), end_at: rangeEnd.value.toISOString() })
  if (request === availabilityRequest) availabilityItems.value = data.availability || []
}

const loadRange = async () => {
  const request = ++rangeRequest
  if (!anchorDate.value || Number.isNaN(dayStart.value.getTime())) {
    anchorDate.value = localDateKey(new Date())
  }
  loading.value = true
  loadError.value = false
  try {
    await Promise.all([loadAll(), loadAvailability()])
  } catch {
    if (request === rangeRequest) loadError.value = true
  } finally {
    if (request === rangeRequest) loading.value = false
  }
}

const moveRange = (direction) => {
  const date = businessDate(`${anchorDate.value}T00:00:00`)
  date.setTime(date.getTime() + direction * (viewMode.value === 'week' ? 7 : 1) * 86400000)
  anchorDate.value = localDateKey(date)
  loadRange()
}
const goToday = () => { anchorDate.value = localDateKey(new Date()); loadRange() }

const openAssign = (item) => {
  actionError.value = ''
  selected.value = item
  assignForm.closer_userid = item.closer_userid || ''
  assignForm.override_reason = ''
  conflicts.value = []
  showAssign.value = true
  if (assignForm.closer_userid) checkConflicts()
}
const checkingConflicts = ref(false)
const conflictError = ref('')
let conflictRequest = 0
const checkConflicts = async () => {
  const request = ++conflictRequest
  assignForm.override_reason = ''
  conflictError.value = ''
  conflicts.value = []
  if (!selected.value || !assignForm.closer_userid) { checkingConflicts.value = false; return }
  checkingConflicts.value = true
  try {
    const data = await detectAppointmentConflicts({ closer_userid: assignForm.closer_userid, start_at: selected.value.start_at, end_at: selected.value.end_at, campus: selected.value.campus, exclude_id: selected.value.id })
    if (request === conflictRequest) conflicts.value = data.conflicts || []
  } catch { if (request === conflictRequest) conflictError.value = '冲突检查失败，请重试后安排' }
  finally { if (request === conflictRequest) checkingConflicts.value = false }
}
const submitAssign = async () => {
  if (saving.value || checkingConflicts.value || conflictError.value || hasHardConflict.value || !assignForm.closer_userid) return
  actionError.value = ''
  saving.value = true
  try {
    await assignAppointmentCloser(selected.value.id, { closer_userid: assignForm.closer_userid, expected_version: selected.value.version, force_soft_conflicts: hasSoftConflict.value, override_reason: assignForm.override_reason })
    showSuccessToast('谈单师已安排')
    showAssign.value = false
    await loadAll()
  } catch (error) {
    conflicts.value = error.response?.data?.conflicts || conflicts.value
    if (error.response?.data?.error === 'appointment_soft_conflict') showFailToast('存在软冲突，请确认并填写原因')
    actionError.value = appointmentActionError(error)
  } finally { saving.value = false }
}

const openReschedule = (item) => {
  actionError.value = ''
  selected.value = item
  rescheduleForm.start_at = localDateTime(item.start_at)
  rescheduleForm.end_at = localDateTime(item.end_at)
  rescheduleForm.reason = ''
  rescheduleForm.override_reason = ''
  conflicts.value = []
  showReschedule.value = true
}
watch(showAssign, open => { if (!open) { conflictRequest++; checkingConflicts.value = false } })
watch(() => [rescheduleForm.start_at, rescheduleForm.end_at], () => { conflictRequest++; checkingConflicts.value = false; conflictError.value = ''; conflicts.value = []; rescheduleForm.override_reason = ''; actionError.value = '' }, { flush: 'sync' })
const appointmentActionError = error => {
  const code = error.response?.data?.error
  if (code === 'appointment_version_conflict' || code === 'lead_version_conflict') return '预约已被其他人修改，输入已保留。请先刷新预约版本，核对后重新提交。'
  if (code === 'appointment_hard_conflict') return '该时段已有冲突，请修改时间或人员后重新提交。'
  if (code === 'appointment_soft_conflict') return '存在排班提醒，请核对并填写确认原因。'
  return error.response ? '操作未成功，请核对当前权限和预约状态，输入已保留。' : '网络连接失败，请重试，输入已保留。'
}
const refreshSelectedVersion = async () => {
  try { await loadAll(); const latest = appointments.value.find(item => item.id === selected.value?.id); if (!latest) { actionError.value = '预约已不在当前可操作列表，请关闭后重新查看日程'; return }; selected.value = latest; conflicts.value = []; actionError.value = '已加载最新预约版本，填写内容已保留，请核对后重新提交' } catch { actionError.value = '刷新预约失败，填写内容已保留，请重试' }
}
const submitReschedule = async () => {
  if (saving.value) return
  if (!rescheduleForm.start_at || !rescheduleForm.end_at || !rescheduleForm.reason.trim()) { showFailToast('请填写预约时间和改期原因'); return }
  if (businessDate(rescheduleForm.end_at) <= businessDate(rescheduleForm.start_at)) { showFailToast('结束时间必须晚于开始时间'); return }
  if (hasHardConflict.value) { showFailToast('硬冲突不能强制改期'); return }
  if (hasSoftConflict.value && !rescheduleForm.override_reason.trim()) { showFailToast('请填写软冲突确认原因'); return }
  actionError.value = ''
  saving.value = true
  try {
    await rescheduleAppointment(selected.value.id, { start_at: businessISO(rescheduleForm.start_at), end_at: businessISO(rescheduleForm.end_at), reason: rescheduleForm.reason, expected_version: selected.value.version, force_soft_conflicts: hasSoftConflict.value, override_reason: rescheduleForm.override_reason })
    showSuccessToast('预约已改期')
    showReschedule.value = false
    await loadAll()
  } catch (error) {
    conflicts.value = error.response?.data?.conflicts || []
    actionError.value = appointmentActionError(error)
  } finally { saving.value = false }
}

const openAction = (item, mode) => { actionError.value = '';  selected.value = item; actionMode.value = mode; actionReason.value = ''; showAction.value = true }
const submitAction = async () => {
  if (saving.value) return
  if (!actionReason.value.trim()) { showFailToast('请填写原因'); return }
  actionError.value = ''
  saving.value = true
  try {
    const payload = { expected_version: selected.value.version, reason: actionReason.value }
    if (actionMode.value === 'no_show') await noShowAppointment(selected.value.id, payload)
    else await cancelAppointment(selected.value.id, payload)
    showSuccessToast(actionMode.value === 'no_show' ? '已登记爽约' : '预约已取消')
    showAction.value = false
    await loadAll()
  } catch (error) { actionError.value = appointmentActionError(error) } finally { saving.value = false }
}

const openArrival = (item) => {
  actionError.value = ''
  selected.value = item
  arrivalConfirmation.value = ''
  showArrival.value = true
}

const submitArrival = async () => {
  if (saving.value) return
  if (!arrivalConfirmation.value.trim()) { showFailToast('请填写交接确认'); return }
  actionError.value = ''
  saving.value = true
  try {
    await confirmAppointmentArrival(selected.value.id, {
      expected_version: selected.value.version,
      confirmation: arrivalConfirmation.value.trim()
    })
    showSuccessToast('客户已到访，线索已交付谈单师')
    showArrival.value = false
    await loadAll()
  } catch (error) { actionError.value = appointmentActionError(error) } finally { saving.value = false }
}

const submitAvailability = async () => {
  if (saving.value) return
  if (!availabilityForm.closer_userid || !availabilityForm.start_at || !availabilityForm.end_at) { showFailToast('请填写谈单师和时间范围'); return }
  const startAt = businessDate(availabilityForm.start_at)
  const endAt = businessDate(availabilityForm.end_at)
  if (Number.isNaN(startAt.getTime()) || Number.isNaN(endAt.getTime()) || endAt <= startAt) { showFailToast('时间范围无效'); return }
  actionError.value = ''
  saving.value = true
  try {
    await createCloserAvailability({ closer_userid: availabilityForm.closer_userid, start_at: startAt.toISOString(), end_at: endAt.toISOString(), kind: availabilityForm.kind, capacity: Number(availabilityForm.capacity || 1), campus: availabilityForm.campus, reason: availabilityForm.reason })
    showSuccessToast('时间设置已保存')
    await loadAvailability()
  } catch (error) { actionError.value = appointmentActionError(error) } finally { saving.value = false }
}

const removeAvailability = async (item) => {
  await deleteCloserAvailability(item.id)
  showSuccessToast('时间设置已删除')
  await loadAvailability()
}

const canCancel = (item) => userStore.isManager() || (!item.handoff_pending && userStore.isInviter() && item.inviter_userid === userStore.userInfo?.wecom_userid)
const canNoShow = (item) => !item.handoff_pending && new Date(item.start_at) <= new Date() && (userStore.isManager() || item.inviter_userid === userStore.userInfo?.wecom_userid || item.closer_userid === userStore.userInfo?.wecom_userid)
const canConfirmArrival = (item) => !item.handoff_pending && item.status === 'scheduled' && (userStore.isManager() || (userStore.isInviter() && item.inviter_userid === userStore.userInfo?.wecom_userid))
const formatDay = (value) => new Date(value).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai', month: 'short', day: 'numeric', weekday: 'short' })
const formatTimeRange = (item) => `${new Date(item.start_at).toLocaleTimeString('zh-CN', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hour12: false })} - ${new Date(item.end_at).toLocaleTimeString('zh-CN', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hour12: false })}`

onMounted(async () => {
  if (!userStore.isAppointmentEnabled()) return
  await Promise.all([loadClosers(), loadRange()])
})
</script>

<style scoped>
.schedule-page { min-height: 100dvh; max-width: var(--kx-page-max-width); margin: 0 auto; padding: var(--kx-space-6); background: var(--kx-color-canvas); color: var(--kx-color-text); }
.schedule-header, .schedule-toolbar, .section-title, .popup-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-4); }
.schedule-header h1 { margin: 0; font-size: var(--kx-font-26); font-weight: 650; letter-spacing: -.025em; line-height: 1.35; }
.schedule-header p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.header-actions { display: flex; flex-wrap: wrap; gap: var(--kx-space-2); }
.schedule-toolbar { flex-wrap: wrap; margin-top: var(--kx-space-6); padding: var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }
.date-navigation { display: flex; align-items: center; gap: var(--kx-space-2); }
.date-navigation button, .date-navigation input, select { min-height: var(--kx-control-current-height); min-width: 0; padding: 0 var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text); }
.date-navigation button { min-width: var(--kx-control-current-height); cursor: pointer; }
.date-navigation button:hover { border-color: var(--kx-color-brand); color: var(--kx-color-brand); }
.date-navigation .today-button { color: var(--kx-color-brand); }
.schedule-view-switch { display: inline-flex; gap: var(--kx-space-1); padding: var(--kx-space-1); border-radius: var(--kx-control-radius); background: var(--kx-color-segment); }
.schedule-view-switch button { min-height: var(--kx-control-current-height); padding: var(--kx-space-2) var(--kx-space-4); color: var(--kx-color-text-secondary); border: 0; border-radius: var(--kx-control-radius); background: transparent; cursor: pointer; }
.schedule-view-switch button[aria-pressed="true"] { color: var(--kx-color-text); background: var(--kx-color-segment-selected); box-shadow: var(--kx-shadow-selected); font-weight: 600; }
.pending-section, .calendar-section { min-width: 0; margin-top: var(--kx-space-6); }
.section-title h2 { margin: 0; font-size: var(--kx-font-16); }
.section-title span { flex-shrink: 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-13); }
.pending-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(240px, 100%), 1fr)); gap: var(--kx-space-3); margin-top: var(--kx-space-3); }
.day-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--kx-space-3); margin-top: var(--kx-space-3); }
.day-grid--week { grid-template-columns: repeat(7, minmax(220px, 1fr)); overflow-x: auto; padding-bottom: var(--kx-space-2); }
.swipe-hint { display: none; margin: var(--kx-space-2) 0 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.day-column { min-width: 0; min-height: 220px; padding: var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-toolbar); }
.day-column > header { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-2); padding: var(--kx-space-1) var(--kx-space-1) var(--kx-space-3); border-bottom: 1px solid var(--kx-color-border); }
.day-column > header strong { font-size: var(--kx-font-14); font-weight: 600; }
.day-column > header > span { flex-shrink: 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.day-column--today > header strong { color: var(--kx-color-brand); }
.today-label { display: inline-block; margin-left: var(--kx-space-2); padding: 2px var(--kx-space-1); border-radius: var(--kx-radius-1); background: var(--kx-color-brand-soft); color: var(--kx-color-brand); font-size: var(--kx-font-12); font-weight: 500; }
.day-items { display: grid; gap: var(--kx-space-3); margin-top: var(--kx-space-3); }
.empty-day { padding: var(--kx-space-8) var(--kx-space-2); color: var(--kx-color-text-muted); font-size: var(--kx-font-13); text-align: center; }
.appointment-card { min-width: 0; padding: var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); overflow-wrap: anywhere; box-shadow: var(--kx-shadow-subtle); }
.scheduled-card { background: var(--kx-color-surface); }
.appointment-card--selected { border-color: var(--kx-color-brand); background: var(--kx-color-brand-soft); }
.pending-card .card-time { color: var(--kx-color-warning); }
.appointment-card h3 { margin: var(--kx-space-2) 0 var(--kx-space-1); font-size: var(--kx-font-16); font-weight: 600; }
.appointment-card p { margin: var(--kx-space-1) 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; }
.appointment-card small { display: block; margin: var(--kx-space-1) 0 var(--kx-space-3); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.6; }
.card-time { color: var(--kx-color-brand); font-weight: 600; font-size: var(--kx-font-13); }
.card-actions { display: flex; flex-wrap: wrap; gap: var(--kx-space-2); }
.card-actions button { font-weight: 500; min-height: var(--kx-control-current-height); padding: var(--kx-space-2); border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); color: var(--kx-color-text-secondary); background: var(--kx-color-surface); cursor: pointer; font-size: var(--kx-font-13); }
.card-actions button:hover { border-color: var(--kx-color-brand); color: var(--kx-color-brand); }
.card-actions .danger { color: var(--kx-color-danger); background: var(--kx-color-danger-soft); border-color: transparent; }
.card-actions .deliver { color: var(--kx-color-on-brand); background: var(--kx-color-brand); border-color: transparent; }
.card-actions .deliver:hover { color: var(--kx-color-on-brand); background: var(--kx-color-brand-hover); }
.action-popup { width: min(var(--kx-dialog-max-width), calc(100vw - 48px)); max-height: calc(100dvh - 64px); overflow-y: auto; padding-bottom: var(--kx-space-6); border-radius: var(--kx-dialog-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); }
.popup-heading { position: sticky; top: 0; z-index: 1; padding: var(--kx-space-6); margin-bottom: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.popup-heading h2 { margin: 0; font-size: var(--kx-font-20); font-weight: 600; }
.popup-heading button, .availability-item button { flex-shrink: 0; min-height: var(--kx-control-current-height); padding: var(--kx-space-2); border: 0; border-radius: var(--kx-control-radius); color: var(--kx-color-text-secondary); background: transparent; cursor: pointer; }
.popup-heading button { background: var(--kx-color-toolbar); padding-inline: var(--kx-space-3); }
.checking-status { margin: 0 var(--kx-space-6) var(--kx-space-4); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.popup-submit { padding: var(--kx-space-6) var(--kx-space-6) 0; }
.delivery-summary, .conflict-panel, .operation-error { margin: 0 var(--kx-space-6) var(--kx-space-4); padding: var(--kx-space-4); border-radius: var(--kx-control-radius); font-size: var(--kx-font-13); line-height: 1.6; overflow-wrap: anywhere; }
.delivery-summary { background: var(--kx-color-success-soft); color: var(--kx-color-success); }
.delivery-summary p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-secondary); }
.conflict-panel { background: var(--kx-color-warning-soft); color: var(--kx-color-warning); }
.conflict-panel p { margin: var(--kx-space-2) 0 0; }
.conflict-panel .hard { color: var(--kx-color-danger); }
.operation-error { color: var(--kx-color-danger); background: var(--kx-color-danger-soft); }
.operation-error button { min-height: var(--kx-control-current-height); padding: var(--kx-space-1) var(--kx-space-2); border: 0; color: inherit; background: transparent; text-decoration: underline; cursor: pointer; }
.availability-list { margin: var(--kx-space-6) var(--kx-space-6) 0; padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-border); }
.availability-item { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-3) 0; border-bottom: 1px solid var(--kx-color-border); }
.availability-item > div { min-width: 0; overflow-wrap: anywhere; }
.availability-item small { display: block; margin-top: var(--kx-space-1); color: var(--kx-color-text-secondary); }
.availability-item button { color: var(--kx-color-danger); }
select { width: 100%; }
.feature-disabled { display: grid; min-height: calc(100dvh - 48px); place-content: center; justify-items: center; gap: var(--kx-space-3); color: var(--kx-color-text-secondary); text-align: center; }
.feature-disabled h1 { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text); font-size: var(--kx-font-26); }
.feature-disabled p { max-width: 420px; margin: 0 0 var(--kx-space-2); line-height: 1.6; }
@media (max-width: 1023px) {
  .schedule-header { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 767px) {
  .schedule-page { padding: var(--kx-space-4); }
  .schedule-header h1, .feature-disabled h1 { font-size: var(--kx-mobile-title-size); }
  .schedule-header p { line-height: 1.5; }
  .header-actions { width: 100%; }
  .header-actions :deep(.van-button) { flex: 1 1 auto; min-width: 0; padding-inline: var(--kx-space-2); border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); }
  .schedule-toolbar { display: grid; grid-template-columns: 44px minmax(0, 1fr) 44px; align-items: stretch; margin-top: var(--kx-space-4); padding: var(--kx-space-3); gap: var(--kx-space-2); border: 0; box-shadow: none; }
  .date-navigation { display: contents; }
  .date-navigation button, .date-navigation input { border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); }
  .date-navigation .today-button { grid-column: 1; grid-row: 2; padding-inline: 0; }
  .date-navigation input { width: 100%; padding-inline: var(--kx-space-2); font-size: var(--kx-font-16); }
  .schedule-view-switch { grid-column: 2 / -1; grid-row: 2; border-radius: var(--kx-mobile-control-radius); }
  .schedule-view-switch button { flex: 1; padding-inline: var(--kx-space-2); }
  .section-title h2 { font-size: var(--kx-mobile-heading-size); }
  .swipe-hint { display: block; }
  .day-grid--week { display: flex; overflow-x: auto; scroll-snap-type: x mandatory; }
  .day-grid--week .day-column { min-width: 100%; scroll-snap-align: start; }
  .day-column { min-height: 0; padding: var(--kx-space-4); border: 0; background: var(--kx-color-surface); }
  .day-column > header { padding: 0 0 var(--kx-space-3); border-color: var(--kx-color-mobile-separator); }
  .day-column > header strong { font-size: var(--kx-font-14); }
  .day-items { gap: 0; margin-top: 0; }
  .appointment-card { padding: var(--kx-space-4) 0; border: 0; border-radius: 0; box-shadow: none; }
  .appointment-card + .appointment-card { border-top: 1px solid var(--kx-color-mobile-separator); }
  .appointment-card:last-child { padding-bottom: 0; }
  .appointment-card h3 { font-size: var(--kx-mobile-heading-size); }
  .pending-card { padding: var(--kx-space-4); border-radius: var(--kx-card-radius); }
  .pending-card:last-child { padding-bottom: var(--kx-space-4); }
  .card-time { font-size: var(--kx-font-14); font-variant-numeric: tabular-nums; }
  .card-actions { gap: var(--kx-space-2); margin-top: var(--kx-space-3); }
  .card-actions button { flex: 1 1 auto; min-width: 44px; border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); }
  .card-actions .deliver { flex-basis: 100%; background: var(--kx-color-brand); }
  .card-actions .danger { background: var(--kx-color-danger-soft); }
  .action-popup { width: 100%; max-height: 88dvh; padding-bottom: calc(var(--kx-space-6) + env(safe-area-inset-bottom)); border-radius: var(--kx-mobile-sheet-radius) var(--kx-mobile-sheet-radius) 0 0; }
  .popup-heading { min-height: 56px; padding: var(--kx-space-2) var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .popup-heading h2 { min-width: 0; font-size: var(--kx-mobile-heading-size); overflow-wrap: anywhere; }
  .popup-submit { padding: var(--kx-space-4) var(--kx-space-4) 0; }
  .delivery-summary, .conflict-panel, .operation-error, .availability-list, .checking-status { margin-inline: var(--kx-space-4); }
  select { font-size: var(--kx-font-16); }
}
</style>
