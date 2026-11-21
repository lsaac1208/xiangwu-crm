<template>
  <div class="page-container followup-form-page" :class="{ 'desktop-form-page': !mobileLayout }">
    <van-nav-bar v-if="mobileLayout" class="mobile-task-nav" left-text="返回" left-arrow @click-left="goBack"><template #title><h1 class="mobile-page-title">添加跟进</h1></template></van-nav-bar>
    <header
      v-if="!mobileLayout"
      class="kx-desktop-header desktop-form-header"
    >
      <div class="kx-desktop-title-row">
        <button type="button" class="kx-desktop-button kx-desktop-back-button" @click="goBack">
          <van-icon aria-hidden="true" name="arrow-left" />
          <span>返回</span>
        </button>
        <div>
          <h1 class="kx-desktop-title">{{ isNegotiationStage ? '添加谈单跟进' : '添加邀约跟进' }}</h1>
          <p class="kx-desktop-subtitle">记录{{ isNegotiationStage ? '谈单' : '联系' }}结果、星级和下次跟进时间</p>
        </div>
      </div>
    </header>

    <div class="content-wrapper">
      <section v-if="lead" class="followup-customer-context" aria-label="当前跟进客户">
        <div><strong>{{ lead.customer_name || '未命名客户' }}</strong><span>{{ displayLeadMobile(lead, true) }} · {{ isNegotiationStage ? '谈单阶段' : '邀约阶段' }}</span></div>
        <RouterLink :to="`/leads/${leadId}`">核对详情</RouterLink>
      </section>
      <div v-if="submitError" class="form-error-summary" role="alert" tabindex="-1" ref="errorSummary">
        <strong>{{ submitErrorTitle }}</strong>
        <span>{{ submitError }}</span>
      </div>
      <CrmStatePanel v-if="!lead" :state="loadError ? 'error' : 'loading'" :title="loadError || '正在加载线索'" :action-label="loadError ? '重试' : ''" @action="loadLead" />
      <van-form v-else class="crm-form" label-align="left" @submit="onSubmit">
        <van-cell-group inset title="跟进记录">
          <van-field
            v-model="formData.remark"
            type="textarea"
            name="remark"
            label="跟进内容"
            required
            placeholder="请输入跟进内容"
            rows="4"
            maxlength="500"
            show-word-limit
            :rules="[{ required: true, message: '请输入跟进内容' }]"
          />

          <van-field name="followup_result" label="跟进结果">
            <template #input>
              <van-radio-group v-model="formData.followup_result" direction="horizontal">
                <van-radio name="connected" class="result-option" :class="{ selected: formData.followup_result === 'connected' }">已联系</van-radio>
                <van-radio name="no_answer" class="result-option" :class="{ selected: formData.followup_result === 'no_answer' }">未接通</van-radio>
                <van-radio name="interested" class="result-option" :class="{ selected: formData.followup_result === 'interested' }">有意向</van-radio>
                <van-radio name="not_interested" class="result-option" :class="{ selected: formData.followup_result === 'not_interested' }">无意向</van-radio>
              </van-radio-group>
            </template>
          </van-field>

        </van-cell-group>

        <van-cell-group inset title="后续安排" class="followup-settings">
          <van-field v-if="formData.is_valid" name="star_rating" label="线索星级">
            <template #input>
              <CrmRate v-model="formData.star_rating" color="var(--kx-color-warning)" void-icon="star" void-color="var(--kx-color-border-strong)" />
            </template>
          </van-field>

          <van-field name="is_valid" label="线索有效" class="switch-field">
            <template #input>
              <van-switch aria-label="线索是否有效" v-model="formData.is_valid" />
            </template>
          </van-field>

          <van-field
            v-if="!formData.is_valid"
            v-model="formData.invalid_reason"
            name="invalid_reason"
            label="无效原因"
            required
            placeholder="请输入无效原因"
            :rules="[{ required: true, message: '请输入无效原因' }]"
          />

          <van-cell v-if="!formData.is_valid && userStore.isPoolEnabled() && !isNegotiationStage" :title="`退回${userStore.poolTitle()}`" center>
            <template #right-icon>
              <van-switch aria-label="退回公海" v-model="formData.return_to_pool" />
            </template>
          </van-cell>

          <van-field
            :model-value="nextFollowupDisplay"
            name="next_followup_at"
            label="下次跟进时间"
          >
            <template #input>
              <button type="button" class="datetime-trigger" :class="{ 'is-placeholder': !nextFollowupDisplay }" aria-label="下次跟进时间" aria-haspopup="dialog" :aria-expanded="showDatePicker" @click="openDatePicker">
                <span>{{ nextFollowupDisplay || '选择下次跟进时间' }}</span>
                <van-icon name="calendar-o" aria-hidden="true" />
              </button>
            </template>
          </van-field>
        </van-cell-group>

        <div class="submit-button crm-mobile-submit">
          <van-button
            block
            type="primary"
            native-type="submit"
            :loading="submitting"
          >
            提交{{ isNegotiationStage ? '谈单' : '邀约' }}跟进
          </van-button>
        </div>
      </van-form>
    </div>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="datePickerRef" v-model:show="showDatePicker" :position="mobileLayout ? 'bottom' : 'center'" round class="followup-date-popup" role="dialog" aria-modal="true" aria-label="选择下次跟进时间" @opened="prepareDatePicker" @keydown="onDatePickerKeydown">
      <div class="datetime-picker-toolbar">
        <button type="button" class="datetime-picker-action" @click="showDatePicker = false">取消</button>
        <strong>选择下次跟进时间</strong>
        <button type="button" class="datetime-picker-action datetime-picker-confirm" @click="onDateConfirm">确认</button>
      </div>
      <van-date-picker
        v-model="currentDateParts"
        :min-date="minDate"
        :show-toolbar="false"
        :visible-option-num="mobileLayout ? 5 : 6"
      />
      <van-time-picker
        v-model="currentTimeParts"
        :show-toolbar="false"
        :visible-option-num="mobileLayout ? 5 : 6"
      />
    </van-popup>
  </div>
</template>

<script setup>
import { createRequestKey } from '@/utils/requestKey'
import { displayLeadMobile } from '@/utils/leadContact'
import CrmRate from '@/components/CrmRate.vue'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import { computed, onBeforeUnmount, ref, unref, watch } from 'vue'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { useRoute, useRouter } from 'vue-router'
import { showFailToast, showSuccessToast } from 'vant'
import { createFollowup } from '@/api/followup'
import { getLeadDetail } from '@/api/lead'
import { useUserStore } from '@/stores/user'
import {
  combineFollowupDateParts,
  datePartsFromDate,
  formatFollowupDateTime,
  timePartsFromDate
} from '@/utils/followupDate'
import { useUnsavedForm } from '@/composables/useUnsavedForm'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const { isMobile: mobileLayout } = useResponsiveLayout()

const leadId = computed(() => String(route.params.id))
const lead = ref(null)
const loadError = ref('')
const initialRating = ref(3)
const isNegotiationStage = computed(() => lead.value?.business_stage === 'negotiation')

const emptyFollowupForm = () => ({
  remark: '',
  followup_result: 'connected',
  star_rating: 3,
  next_followup_at: '',
  is_valid: true,
  invalid_reason: '',
  return_to_pool: false
})
const formData = ref(emptyFollowupForm())

const submitting = ref(false)
const { markSaved } = useUnsavedForm(formData, submitting)

const submitError = ref('')
const submitErrorTitle = ref('跟进提交未完成')
const requestKey = ref('')
const errorSummary = ref(null)
const showDatePicker = ref(false)
const datePickerRef = ref(null)
useOverlayFocus(showDatePicker, datePickerRef, () => { showDatePicker.value = false })
const minDate = new Date()
const currentDateParts = ref(datePartsFromDate(minDate))
const currentTimeParts = ref(timePartsFromDate(minDate))
const nextFollowupDisplay = ref('')

const prepareDatePicker = () => {
  // Vant renders picker options as buttons inside a list used only for layout.
  unref(datePickerRef.value?.popupRef)?.querySelectorAll('.van-picker-column__wrapper').forEach(list => {
    list.setAttribute('role', 'presentation')
  })
}

const onDatePickerKeydown = (event) => {
  if (event.key !== 'Enter' && event.key !== ' ') return
  const option = event.target.closest('.van-picker-column__item[role="button"]')
  if (!option || option.getAttribute('aria-disabled') === 'true') return
  event.preventDefault()
  option.click()
}

const openDatePicker = () => {
  const selected = formData.value.next_followup_at
    ? new Date(formData.value.next_followup_at)
    : new Date()
  currentDateParts.value = datePartsFromDate(selected)
  currentTimeParts.value = timePartsFromDate(selected)
  showDatePicker.value = true
}

const onDateConfirm = () => {
  const date = combineFollowupDateParts(currentDateParts.value, currentTimeParts.value)
  if (date.getTime() <= Date.now()) {
    showFailToast('下次跟进时间必须晚于当前时间')
    return
  }
  formData.value.next_followup_at = date.toISOString()
  nextFollowupDisplay.value = formatFollowupDateTime(date)
  showDatePicker.value = false
}

const onSubmit = async () => {
  if (submitting.value || !lead.value) return
  try {
    submitting.value = true
    submitError.value = ''
    submitErrorTitle.value = '跟进提交未完成'
    if (!requestKey.value) requestKey.value = createRequestKey()

    const payload = {
      remark: formData.value.remark,
      followup_result: formData.value.followup_result,
      intention_level: legacyIntentionLevel(formData.value.star_rating),
      is_valid: formData.value.is_valid
    }

    if (formData.value.star_rating !== initialRating.value) payload.star_rating = formData.value.star_rating

    if (!formData.value.is_valid) {
      payload.invalid_reason = formData.value.invalid_reason
      payload.return_to_pool = isNegotiationStage.value ? false : formData.value.return_to_pool
    }

    if (formData.value.next_followup_at) {
      payload.next_followup_at = formData.value.next_followup_at
    }

    await createFollowup(leadId.value, payload, requestKey.value)

    markSaved()
    submitting.value = false
    showSuccessToast('跟进记录已添加')
    goBack()
  } catch (error) {
    submitErrorTitle.value = error.message?.includes('安全提交') ? '无法安全提交' : error.response ? '跟进提交未完成' : '尚未确认提交结果'
    submitError.value = error.response?.data?.message || (error.message?.includes('安全提交') ? error.message : '尚未确认本次提交结果，可安全重试；填写内容已保留。')
    requestAnimationFrame(() => errorSummary.value?.focus())
  } finally {
    submitting.value = false
  }
}

const legacyIntentionLevel = (rating) => {
  if (rating >= 4) return 'high'
  if (rating >= 2) return 'medium'
  return 'low'
}

const goBack = () => {
  const target = router.resolve({ path: `/leads/${leadId.value}`, query: route.query }).fullPath
  if (router.options.history.state.back === target) router.back()
  else router.replace(target)
}

let loadRequest = 0
const loadLead = async () => {
  const request = ++loadRequest
  const id = leadId.value
  loadError.value = ''
  lead.value = null
  try {
    const data = await getLeadDetail(id)
    if (request !== loadRequest || id !== leadId.value) return
    lead.value = data.lead || data
    initialRating.value = lead.value.star_rating >= 1 && lead.value.star_rating <= 5 ? lead.value.star_rating : 3
    formData.value.star_rating = initialRating.value
    markSaved()
  } catch { if (request === loadRequest) loadError.value = '线索加载失败，请重试后添加跟进' }
}
watch(leadId, () => {
  lead.value = null
  formData.value = emptyFollowupForm()
  requestKey.value = ''; submitError.value = ''; nextFollowupDisplay.value = ''
  markSaved()
  void loadLead()
}, { immediate: true })
onBeforeUnmount(() => { loadRequest++ })
</script>

<style scoped>
.followup-customer-context { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); margin-bottom: var(--kx-space-4); padding: var(--kx-space-3) var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
.followup-customer-context strong { display: block; font-size: var(--kx-font-16); }
.followup-customer-context span { display: block; margin-top: var(--kx-space-1); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.followup-customer-context a { display: inline-flex; align-items: center; min-height: var(--kx-mobile-control-height); color: var(--kx-color-brand); font-size: var(--kx-font-14); white-space: nowrap; }

.followup-date-popup { width: min(560px, 100%); max-height: 90dvh; overflow-y: auto; padding-bottom: env(safe-area-inset-bottom); }
.mobile-page-title { margin: 0; font-size: var(--kx-mobile-heading-size); font-weight: 600; letter-spacing: -0.02em; line-height: 1.3; }
.followup-form-page { min-height: 100vh; min-height: 100dvh; background: var(--kx-color-canvas); }
.desktop-form-header { max-width: 900px; margin: 0 auto; padding: var(--kx-space-6) var(--kx-space-6) 0; }
.content-wrapper { max-width: 900px; margin: 0 auto; padding: var(--kx-space-4) var(--kx-space-6) var(--kx-space-8); }
.form-error-summary { display: grid; gap: var(--kx-space-1); margin: 0 0 var(--kx-space-4); padding: var(--kx-space-3) var(--kx-space-4); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-control-radius); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); font-size: var(--kx-font-13); line-height: 1.6; }
.followup-settings { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.followup-settings :deep(.van-cell) { min-width: 0; }
.followup-settings :deep(.van-field__body) { min-height: var(--kx-control-current-height); }
.van-radio-group { display: grid; width: 100%; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--kx-space-1); padding: var(--kx-space-1); border-radius: var(--kx-control-radius); background: var(--kx-color-segment); }
.result-option { padding: var(--kx-space-2); border-radius: var(--kx-radius-2); transition: background var(--kx-motion-fast), box-shadow var(--kx-motion-fast); }
.result-option.selected { background: var(--kx-color-segment-selected); box-shadow: var(--kx-shadow-selected); }
.van-radio-group :deep(.van-radio) { min-height: var(--kx-control-current-height); margin-right: 0; }
.submit-button { display: flex; justify-content: flex-end; margin: var(--kx-space-6) 0 0; padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-border); }
.submit-button :deep(.van-button) { width: 180px; height: var(--kx-control-current-height); border-radius: var(--kx-control-radius); }
.datetime-trigger { display: flex; width: 100%; min-height: var(--kx-control-current-height); align-items: center; justify-content: space-between; gap: var(--kx-space-2); padding: var(--kx-space-2) var(--kx-space-3); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text); text-align: left; cursor: pointer; }
.datetime-trigger.is-placeholder { color: var(--kx-color-text-muted); }
.datetime-trigger span { min-width: 0; overflow-wrap: anywhere; }
.datetime-trigger .van-icon { flex: 0 0 auto; color: var(--kx-color-text-secondary); }
.datetime-picker-toolbar { display: grid; grid-template-columns: 64px minmax(0, 1fr) 64px; align-items: center; min-height: 56px; padding: 0 var(--kx-space-2); text-align: center; border-bottom: 1px solid var(--kx-color-border); }
.datetime-picker-toolbar strong { font-size: var(--kx-font-14); font-weight: 600; }
.datetime-picker-action { min-height: 44px; padding: var(--kx-space-3) var(--kx-space-2); color: var(--kx-color-text-secondary); background: transparent; border: 0; border-radius: var(--kx-control-radius); cursor: pointer; }
.datetime-picker-confirm { color: var(--kx-color-brand); font-weight: 500; }
@media (max-width: 767px) {
  .content-wrapper { padding: var(--kx-space-4) var(--kx-space-4) calc(88px + env(safe-area-inset-bottom)); }
  .followup-settings { grid-template-columns: 1fr; }
  .followup-settings :deep(.switch-field) { flex-direction: row; align-items: center; gap: var(--kx-space-3); min-height: 64px; }
  .followup-settings :deep(.switch-field .van-field__label) { flex: 1; margin: 0; font-size: var(--kx-font-16); }
  .followup-settings :deep(.switch-field .van-field__value) { flex: 0 0 auto; width: auto; }
  .crm-form :deep(.van-cell-group__title) { padding: var(--kx-space-4) var(--kx-space-4) var(--kx-space-2); font-size: var(--kx-mobile-heading-size); }
  .crm-form :deep(.van-cell-group__title:first-child) { padding-top: 0; }
  .van-radio-group { grid-template-columns: repeat(4, minmax(0, 1fr)); background: var(--kx-color-mobile-fill); }
  .van-radio-group :deep(.van-radio__icon) { display: none; }
  .van-radio-group :deep(.van-radio__label) { margin: 0; font-size: var(--kx-font-14); }
  .van-radio-group :deep(.van-radio) { justify-content: center; padding: var(--kx-space-2) var(--kx-space-1); }
  .datetime-trigger { padding: 0 var(--kx-space-3); border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); }
  .datetime-trigger.is-placeholder { color: var(--kx-color-text-secondary); }
  .datetime-picker-toolbar { position: sticky; top: 0; z-index: 1; border-color: var(--kx-color-mobile-separator); background: var(--kx-color-surface); }
  .datetime-picker-toolbar strong { font-size: var(--kx-mobile-heading-size); }
  .datetime-picker-action { color: var(--kx-color-brand); font-size: var(--kx-font-16); }
  .datetime-picker-confirm { font-weight: 600; }
  .submit-button { position: fixed; z-index: 44; right: 0; bottom: var(--kx-shell-mobile-nav-height); left: 0; margin: 0; padding: var(--kx-space-3) var(--kx-space-4) calc(var(--kx-space-3) + env(safe-area-inset-bottom)); border-color: var(--kx-color-mobile-separator); background: var(--kx-color-mobile-material); -webkit-backdrop-filter: blur(18px); backdrop-filter: blur(18px); }
  .submit-button :deep(.van-button) { width: 100%; }
}
@media (max-width: 767px) and (max-height: 500px) {
  .content-wrapper { padding-bottom: calc(var(--kx-space-8) + env(safe-area-inset-bottom)); }
  .crm-mobile-submit { position: static; margin-top: var(--kx-space-4); padding: var(--kx-space-4) 0 0; background: transparent; -webkit-backdrop-filter: none; backdrop-filter: none; }
}
</style>
