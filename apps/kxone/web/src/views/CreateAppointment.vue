<template>
  <div class="page-container appointment-form-page" :class="{ 'desktop-form-page': !mobileLayout }">
    <section v-if="!userStore.isAppointmentEnabled()" class="appointment-disabled" role="status">
      <van-icon aria-hidden="true" name="calendar-o" size="40" />
      <h1>预约功能暂未启用</h1>
      <p>当前无法创建预约单，请联系管理员启用预约排班。</p>
      <van-button type="primary" @click="router.push('/leads')">返回工作台</van-button>
    </section>
    <template v-else>
    <van-nav-bar v-if="mobileLayout" class="mobile-task-nav" left-text="返回" left-arrow @click-left="goBack"><template #title><h1 class="mobile-page-title">创建预约单</h1></template></van-nav-bar>
    <header v-else class="appointment-form-header">
      <button type="button" class="back-button" aria-label="返回" @click="goBack">
        <van-icon aria-hidden="true" name="arrow-left" />
      </button>
      <div>
        <h1>创建预约单</h1>
        <p>{{ lead ? `${displayCustomerName(lead)} · ${lead.lead_no}` : '填写客户到校与交接信息' }}</p>
      </div>
    </header>
    <p v-if="mobileLayout" class="appointment-context">{{ lead ? `${displayCustomerName(lead)} · ${lead.lead_no}` : '填写客户到校与交接信息' }}</p>

    <main class="appointment-form-content">
      <div v-if="submitError" ref="errorSummary" class="appointment-form-error" role="alert" tabindex="-1">
        <strong>预约单未提交</strong>
        <span>{{ submitError }}</span>
      </div>
      <CrmStatePanel v-if="!lead" :state="loadError ? 'error' : 'loading'" :title="loadError || '正在加载线索'" :action-label="loadError ? '重试' : ''" @action="loadLead" />
      <van-form v-else class="crm-form" label-align="left" @submit="submit">
        <van-cell-group inset title="到校安排" class="arrival-fields">
          <van-field v-model="form.start_at" type="datetime-local" name="start_at" label="开始时间" required :rules="[{ required: true, message: '请选择开始时间' }]" />
          <van-field v-model="form.end_at" type="datetime-local" name="end_at" label="预计结束" required :rules="[{ required: true, message: '请选择结束时间' }, { validator: value => businessDate(value) > businessDate(form.start_at), message: '结束时间必须晚于开始时间' }]" />
          <van-field v-model="form.campus" name="campus" label="校区" required placeholder="例如：襄阳校区" :rules="[{ required: true, message: '请填写校区' }]" />
          <van-field v-model="form.visitor_count" type="digit" name="visitor_count" label="到访人数" :rules="[{ validator: value => Number.isInteger(Number(value)) && Number(value) > 0, message: '请填写大于零的整数' }]" />
        </van-cell-group>

        <van-cell-group inset title="客户交接" class="form-block">
          <van-field v-model="form.customer_concerns" type="textarea" rows="2" label="客户关注点" placeholder="课程、费用、效果等" />
          <van-field v-model="form.handoff_notes" type="textarea" rows="3" label="交接说明" required placeholder="请填写已沟通情况和谈单注意事项" :rules="[{ required: true, message: '请填写交接说明' }]" />
          <van-field v-model="form.special_notes" type="textarea" rows="2" label="特殊情况" placeholder="选填" />
        </van-cell-group>

        <div class="submit-row crm-mobile-submit">
          <van-button type="primary" native-type="submit" :loading="submitting" block>提交预约，等待主管分配</van-button>
        </div>
      </van-form>
    </main>
    </template>
  </div>
</template>

<script setup>
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showSuccessToast } from 'vant'
import { createAppointment } from '@/api/appointment'
import { businessDate, businessISO, localDateTime } from '@/utils/appointmentDate'
import { getLeadDetail } from '@/api/lead'
import { displayCustomerName } from '@/utils/leadDisplay'
import { useUserStore } from '@/stores/user'
import { useUnsavedForm } from '@/composables/useUnsavedForm'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const { isMobile: mobileLayout } = useResponsiveLayout()
const lead = ref(null)
const loadError = ref('')
const submitting = ref(false)
const submitError = ref('')
const errorSummary = ref(null)

const initialStart = new Date(Date.now() + 60 * 60 * 1000)
initialStart.setMinutes(Math.ceil(initialStart.getMinutes() / 15) * 15, 0, 0)
const initialEnd = new Date(initialStart.getTime() + 90 * 60 * 1000)

const localInputValue = localDateTime

const form = reactive({
  start_at: localInputValue(initialStart),
  end_at: localInputValue(initialEnd),
  campus: '襄阳校区',
  visitor_count: '1',
  customer_concerns: '',
  handoff_notes: '',
  special_notes: ''
})

const { markSaved } = useUnsavedForm(form, submitting)

const submit = async () => {
  if (submitting.value || !lead.value) return
  submitting.value = true
  submitError.value = ''
  try {
    await createAppointment({
      lead_id: Number(route.params.id),
      start_at: businessISO(form.start_at),
      end_at: businessISO(form.end_at),
      business_timezone: 'Asia/Shanghai',
      campus: form.campus,
      visitor_count: Number(form.visitor_count || 1),
      customer_concerns: form.customer_concerns,
      handoff_notes: form.handoff_notes,
      special_notes: form.special_notes
    })
    markSaved()
    submitting.value = false
    showSuccessToast('预约单已创建')
    router.push('/appointments')
  } catch (error) {
    submitError.value = error.response?.data?.message || '请检查时间与网络后重试，已填内容不会丢失。'
    requestAnimationFrame(() => errorSummary.value?.focus())
  } finally {
    submitting.value = false
  }
}

const goBack = () => {
  const target = router.resolve({ path: `/leads/${route.params.id}`, query: route.query }).fullPath
  if (router.options.history.state.back === target) router.back()
  else router.replace(target)
}

const loadLead = async () => {
  loadError.value = ''
  if (!userStore.isAppointmentEnabled()) return
  try {
    const data = await getLeadDetail(route.params.id)
    lead.value = data.lead || data
  } catch { loadError.value = '线索加载失败，请重试后填写预约' }
}
onMounted(loadLead)
</script>

<style scoped>
.appointment-form-page { min-height: 100vh; min-height: 100dvh; background: var(--kx-color-canvas); }
.appointment-disabled { display: grid; min-height: 100vh; min-height: 100dvh; place-content: center; justify-items: center; gap: var(--kx-space-3); padding: var(--kx-space-6); color: var(--kx-color-text-secondary); text-align: center; }
.appointment-disabled > .van-icon { color: var(--kx-color-brand); }
.appointment-disabled h1 { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text); font-size: var(--kx-font-26); font-weight: 600; }
.appointment-disabled p { margin: 0 0 var(--kx-space-2); line-height: 1.6; }
.appointment-form-header { display: flex; align-items: flex-start; gap: var(--kx-space-4); max-width: 900px; margin: 0 auto; padding: var(--kx-space-6) var(--kx-space-6) 0; }
.appointment-form-header > div { min-width: 0; }
.appointment-form-header h1 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-26); font-weight: 600; line-height: 1.5; }
.appointment-form-header p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; overflow-wrap: anywhere; }
.back-button { display: grid; flex: 0 0 auto; width: var(--kx-control-current-height); height: var(--kx-control-current-height); place-items: center; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.back-button:hover { background: var(--kx-color-surface-subtle); }
.appointment-form-content { max-width: 900px; margin: 0 auto; padding: var(--kx-space-4) var(--kx-space-6) var(--kx-space-8); }
.arrival-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.arrival-fields :deep(.van-field) { min-width: 0; }
.form-block { margin-top: 0; }
.submit-row { display: flex; justify-content: flex-end; margin: var(--kx-space-6) 0 0; padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-border); }
.submit-row :deep(.van-button) { width: auto; min-width: 240px; height: var(--kx-control-current-height); border-radius: var(--kx-control-radius); }
.appointment-form-error { display: grid; gap: var(--kx-space-1); margin-bottom: var(--kx-space-4); padding: var(--kx-space-3) var(--kx-space-4); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-control-radius); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); font-size: var(--kx-font-13); line-height: 1.6; }
@media (max-width: 767px) {
  .mobile-page-title { margin: 0; font-size: var(--kx-mobile-heading-size); font-weight: 600; letter-spacing: -0.02em; line-height: 1.3; }
  .appointment-disabled h1 { font-size: var(--kx-mobile-title-size); }
  .appointment-context { margin: 0; padding: var(--kx-space-4) var(--kx-space-8) 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; overflow-wrap: anywhere; }
  .appointment-form-content { padding: var(--kx-space-4) var(--kx-space-4) calc(88px + env(safe-area-inset-bottom)); }
  .crm-form :deep(.van-cell-group__title) { padding: var(--kx-space-4) var(--kx-space-4) var(--kx-space-2); font-size: var(--kx-mobile-heading-size); }
  .crm-form :deep(.van-cell-group__title:first-child) { padding-top: 0; }
  .arrival-fields { grid-template-columns: 1fr; }
  .submit-row { position: fixed; z-index: 44; right: 0; bottom: var(--kx-shell-mobile-nav-height); left: 0; margin: 0; padding: var(--kx-space-3) var(--kx-space-4) calc(var(--kx-space-3) + env(safe-area-inset-bottom)); border-color: var(--kx-color-mobile-separator); background: var(--kx-color-mobile-material); -webkit-backdrop-filter: blur(18px); backdrop-filter: blur(18px); }
  .submit-row :deep(.van-button) { width: 100%; min-width: 0; }
}
@media (max-width: 767px) and (max-height: 500px) {
  .appointment-form-content { padding-bottom: calc(var(--kx-space-8) + env(safe-area-inset-bottom)); }
  .crm-mobile-submit { position: static; margin-top: var(--kx-space-4); padding: var(--kx-space-4) 0 0; background: transparent; -webkit-backdrop-filter: none; backdrop-filter: none; }
}
</style>
