<template>
  <div class="page-container create-lead-page" :class="{ 'desktop-form-page': !mobileLayout }">
    <van-nav-bar v-if="mobileLayout" class="mobile-task-nav" left-text="返回" left-arrow @click-left="goBack"><template #title><h1 class="mobile-page-title">新建线索</h1></template></van-nav-bar>

    <header v-else class="kx-desktop-header create-header">
      <div class="kx-desktop-title-row">
        <button type="button" class="kx-desktop-button kx-desktop-back-button" @click="goBack">
          <van-icon aria-hidden="true" name="arrow-left" />
          <span>返回</span>
        </button>
        <div>
          <h1 class="kx-desktop-title">新建线索</h1>
          <p class="kx-desktop-subtitle">一次录入客户资料、咨询需求与来源，创建后可直接进入线索详情继续处理</p>
        </div>
      </div>
    </header>

    <div v-if="submitError" ref="errorSummary" class="create-error" role="alert" tabindex="-1">{{ submitError }}<van-button v-if="intentConflict" size="small" @click="startNewIntent">确认已有结果，重新录入</van-button></div>
    <van-form scroll-to-error label-align="left" class="create-form crm-form" @submit="onSubmit">
      <div class="form-layout">
        <main class="form-main">
          <section class="form-section">
            <div class="section-heading">
              <span class="section-index">1</span>
              <div><h2>客户信息</h2><p>与线索详情中的客户资料保持一致</p></div>
            </div>
            <div class="field-grid">
              <van-field
                v-model.trim="form.customer_name"
                name="customer_name"
                label="客户姓名"
                required
                maxlength="64"
                placeholder="请输入客户姓名"
                :rules="[{ required: true, message: '请输入客户姓名' }]"
              />
              <van-field
                v-model.trim="form.customer_mobile"
                name="customer_mobile"
                label="联系电话"
                type="tel"
                maxlength="32"
                placeholder="电话与微信至少填写一项"
                :rules="[{ validator: value => Boolean(value.trim() || form.customer_wechat.trim()), message: '联系电话与微信号至少填写一项' }]"
              />
              <van-field
                v-model.trim="form.customer_wechat"
                name="customer_wechat"
                label="微信号"
                maxlength="64"
                placeholder="选填，便于后续联系"
              />
              <van-field
                v-model.trim="form.city"
                name="city"
                label="城市"
                maxlength="64"
                placeholder="例如：武汉市"
              />
            </div>
          </section>

          <section class="form-section">
            <div class="section-heading">
              <span class="section-index">2</span>
              <div><h2>咨询与来源</h2><p>用于详情页概览、来源识别和后续统计</p></div>
            </div>
            <div class="field-grid">
              <van-field
                v-model.trim="form.product_name"
                name="product_name"
                label="咨询项目"
                maxlength="128"
                placeholder="例如：咨询学费、课程报名"
              />
              <LeadSourceField v-model="form.source_channel" required />
              <van-field
                v-model.trim="form.source_campaign"
                name="source_campaign"
                label="投放活动"
                maxlength="256"
                placeholder="活动、计划或素材名称，选填"
              />
              <van-field name="star_rating" label="意向星级" class="rating-field">
                <template #input>
                  <div class="rating-control">
                    <CrmRate v-model="form.star_rating" :size="20" :gutter="4" color="var(--kx-color-warning)" void-color="var(--kx-color-border-strong)" />
                    <span>{{ ratingLabel }}</span>
                  </div>
                </template>
              </van-field>
            </div>
          </section>

          <section v-if="userStore.isCloser()" class="form-section assignment-section">
            <div class="section-heading">
              <span class="section-index">3</span>
              <div><h2>流转安排</h2><p>谈单师创建线索时，需要指定后续负责邀约的人员</p></div>
            </div>
            <div class="assignment-summary" :class="{ selected: form.assigned_inviter_userid }">
              <span>指定邀约师</span>
              <strong>{{ selectedInviterName || '请选择一名邀约师' }}</strong>
            </div>
            <van-loading v-if="loadingInviters" vertical class="inviter-loading">正在加载邀约师</van-loading>
            <CrmStatePanel v-else-if="invitersError" state="error" title="邀约师加载失败" description="已填资料已保留，可重试加载人员。" action-label="重试加载邀约师" @action="loadInviters" />
            <van-radio-group v-else v-model="form.assigned_inviter_userid" class="inviter-options">
              <button
                v-for="item in inviters"
                :key="item.wecom_userid"
                type="button"
                class="inviter-option"
                :class="{ selected: form.assigned_inviter_userid === item.wecom_userid }"
                @click="form.assigned_inviter_userid = item.wecom_userid"
              >
                <span class="inviter-avatar">{{ inviterInitial(item) }}</span>
                <span><strong>{{ item.employee_name || item.wecom_userid }}</strong><small>{{ item.wecom_userid }}</small></span>
                <van-radio :name="item.wecom_userid" />
              </button>
            </van-radio-group>
            <van-empty v-if="!loadingInviters && !invitersError && inviters.length === 0" description="暂无可用邀约师，请联系主管配置" />
          </section>
        </main>

        <aside class="form-sidebar">
          <section class="preview-card" aria-label="线索创建预览">
            <div class="preview-heading">
              <span>线索预览</span>
              <van-tag type="primary" plain>邀约阶段</van-tag>
            </div>
            <h2>{{ form.customer_name || '待填写客户姓名' }}</h2>
            <p>{{ contactPreview }}</p>
            <CrmRate :model-value="form.star_rating" readonly :size="15" :gutter="3" color="var(--kx-color-warning)" void-color="var(--kx-color-border-strong)" />
            <div class="preview-grid">
              <div><span>咨询项目</span><strong>{{ form.product_name || '暂未填写' }}</strong></div>
              <div><span>城市</span><strong>{{ form.city || '暂未填写' }}</strong></div>
              <div><span>来源渠道</span><strong>{{ form.source_channel || '请选择来源渠道' }}</strong></div>
              <div><span>投放活动</span><strong>{{ form.source_campaign || '暂无' }}</strong></div>
            </div>
          </section>
          <section class="flow-card">
            <van-icon aria-hidden="true" name="info-o" />
            <div><strong>创建后的流转</strong><p>{{ creationHint }}</p></div>
          </section>
          <div class="form-actions crm-mobile-submit">
            <button type="button" class="cancel-button" :disabled="submitting" @click="goBack">取消</button>
            <van-button block type="primary" native-type="submit" :loading="submitting" class="submit-button">
              创建线索并查看详情
            </van-button>
          </div>
        </aside>
      </div>
    </van-form>
  </div>
</template>

<script setup>
import { createRequestKey } from '@/utils/requestKey'
import { validateLeadProfilePhone } from '@/utils/leadProfile'
import LeadSourceField from '@/components/LeadSourceField.vue'
import CrmRate from '@/components/CrmRate.vue'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showFailToast, showSuccessToast } from 'vant'
import { markQueueFailure } from '@/utils/leadNotification'
import { createLead } from '@/api/lead'
import { getSalesUsers } from '@/api/user'
import { useUserStore } from '@/stores/user'
import { useUnsavedForm } from '@/composables/useUnsavedForm'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'

const router = useRouter()
const userStore = useUserStore()
const { isMobile: mobileLayout } = useResponsiveLayout()
const submitting = ref(false)
const inviters = ref([])
const loadingInviters = ref(false)
const invitersError = ref(false)
const errorSummary = ref(null)

const form = reactive({
  customer_name: '',
  customer_mobile: '',
  customer_wechat: '',
  city: '',
  product_name: '',
  source_channel: '',
  source_campaign: '',
  star_rating: 3,
  assigned_inviter_userid: ''
})

const { markSaved } = useUnsavedForm(form, submitting)

const ratingLabel = computed(() => ({ 1: '低意向', 2: '较低意向', 3: '中等意向', 4: '较高意向', 5: '高意向' }[form.star_rating] || '未评级'))
const contactPreview = computed(() => [form.customer_mobile || '未留电话', form.customer_wechat ? `微信 ${form.customer_wechat}` : '未留微信'].join(' · '))

const creationHint = computed(() => {
  if (userStore.isDispatcher()) return '创建后进入待分配队列，由您选择邀约师完成首次分配。'
  if (userStore.isCloser()) return '创建后分配给所选邀约师，等待对方领取。'
  if (userStore.isInviter()) return '创建后由您直接负责，并进入跟进中。'
  return '创建后进入待派发队列，可分配给邀约师或由主管亲自跟进。'
})

const selectedInviterName = computed(() => {
  const selected = inviters.value.find(item => item.wecom_userid === form.assigned_inviter_userid)
  return selected ? selected.employee_name || selected.wecom_userid : ''
})

const leadLevelForRating = rating => rating >= 4 ? 'high' : rating >= 3 ? 'medium' : 'low'

const submitError = ref('')
const requestKey = ref('')
const intentConflict = ref(false)
const startNewIntent = async () => {
  try {
    await showConfirmDialog({ title: '重新录入', message: '请先到线索列表确认上次提交的结果。继续会以新请求提交当前内容。' })
    requestKey.value = createRequestKey(); intentConflict.value = false; submitError.value = ''
  } catch (error) { if (error instanceof Error) submitError.value = error.message }
}
const onSubmit = async () => {
  if (submitting.value) return
  submitError.value = ''
  if (!requestKey.value) {
    try { requestKey.value = createRequestKey() } catch (error) { submitError.value = error.message; return }
  }
  if (!form.customer_mobile.trim() && !form.customer_wechat.trim()) { submitError.value = '联系电话与微信号至少填写一项'; return }
  if (!validateLeadProfilePhone(form.customer_mobile, form.customer_wechat).valid) { submitError.value = '请输入有效的联系电话，可使用座机、+86、空格或连字符'; return }
  if (userStore.isCloser() && !form.assigned_inviter_userid) {
    showFailToast('请选择邀约师')
    return
  }
  submitting.value = true
  try {
    const data = await createLead({ ...form, lead_level: leadLevelForRating(form.star_rating) }, requestKey.value)
    const lead = data.lead || data.Lead || data
    if (data.warnings?.includes('notification_failed')) markQueueFailure(lead.id, userStore.userInfo?.wecom_userid)
    requestKey.value = ''
    markSaved()
    submitting.value = false
    showSuccessToast(data.replayed ? '已恢复上次录入结果，未重复创建' : '线索已创建')
    router.replace(`/leads/${lead.id}`)
  } catch (error) {
    intentConflict.value = error.response?.data?.error === 'idempotency_conflict'
    submitError.value = error.response?.data?.message || '创建失败，请检查网络后重试，填写内容已保留'
    requestAnimationFrame(() => errorSummary.value?.focus())
  } finally {
    submitting.value = false
  }
}

const loadInviters = async () => {
  if (!userStore.isCloser()) return
  if (loadingInviters.value) return
  loadingInviters.value = true
  invitersError.value = false
  try {
    const data = await getSalesUsers()
    inviters.value = data.bindings || data.users || data.items || data || []
  } catch {
    invitersError.value = true
  } finally {
    loadingInviters.value = false
  }
}

const inviterInitial = item => (item.employee_name || item.wecom_userid || '?').trim().slice(0, 1)

const goBack = () => {
  const previous = router.options.history.state.back
  if (typeof previous === 'string' && previous.startsWith('/') && !previous.startsWith('//') && !previous.includes('\\') && previous !== '/leads/new') {
    router.back()
    return
  }
  router.replace('/leads')
}

onMounted(loadInviters)
</script>

<style scoped>
.mobile-page-title { margin: 0; font-size: var(--kx-mobile-heading-size); font-weight: 600; letter-spacing: -0.02em; line-height: 1.3; }
.create-error { max-width: 1032px; margin: var(--kx-space-4) auto 0; padding: var(--kx-space-3) var(--kx-space-4); color: var(--kx-color-danger); background: var(--kx-color-danger-soft); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-radius-3); line-height: 1.6; }
.create-lead-page { min-height: 100vh; min-height: 100dvh; background: var(--kx-color-canvas); }
.create-header { width: 100%; max-width: 1120px; margin: 0 auto; padding: var(--kx-space-6) var(--kx-space-6) 0; }
.create-form { width: 100%; max-width: 1120px; margin: 0 auto; padding: var(--kx-space-6) var(--kx-space-6) var(--kx-space-8); }
.form-layout { display: grid; grid-template-columns: minmax(0, 1fr) 288px; align-items: start; gap: var(--kx-space-6); }
.form-main { display: grid; min-width: 0; gap: var(--kx-space-6); }
.form-section { overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }
.section-heading { display: flex; align-items: center; gap: var(--kx-space-3); padding: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); }
.section-index { display: grid; flex: 0 0 auto; width: 32px; height: 32px; place-items: center; border-radius: var(--kx-radius-3); background: var(--kx-color-segment); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); font-weight: 600; font-variant-numeric: tabular-nums; }
.section-heading h2 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-16); font-weight: 600; }
.section-heading p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.5; }
.field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); padding: var(--kx-space-1) 0; }
.field-grid :deep(.van-field) { min-width: 0; }
.rating-control { display: flex; flex-wrap: wrap; min-height: var(--kx-control-current-height); align-items: center; gap: var(--kx-space-2); }
.rating-control span { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); white-space: nowrap; }
.assignment-summary { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--kx-space-2); margin: var(--kx-space-4) var(--kx-space-4) 0; padding: var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.assignment-summary strong { color: var(--kx-color-text); font-weight: 500; overflow-wrap: anywhere; }
.assignment-summary.selected { border-color: var(--kx-color-brand); background: var(--kx-color-brand-soft); }
.assignment-summary.selected strong { color: var(--kx-color-brand); }
.inviter-loading { padding: var(--kx-space-8) 0; }
.inviter-options { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--kx-space-3); padding: var(--kx-space-4); }
.inviter-option { display: grid; grid-template-columns: 36px minmax(0, 1fr) auto; align-items: center; gap: var(--kx-space-3); min-height: 68px; padding: var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); text-align: left; cursor: pointer; transition: border-color var(--kx-motion-fast) var(--kx-ease-standard), background var(--kx-motion-fast) var(--kx-ease-standard); }
.inviter-option:hover { background: var(--kx-color-surface-subtle); }
.inviter-option.selected { border-color: var(--kx-color-brand); background: var(--kx-color-brand-soft); }
.inviter-avatar { display: grid; width: 36px; height: 36px; place-items: center; border-radius: var(--kx-radius-3); background: var(--kx-color-brand-soft); color: var(--kx-color-brand); font-weight: 600; }
.inviter-option strong, .inviter-option small { display: block; overflow-wrap: anywhere; }
.inviter-option strong { color: var(--kx-color-text); font-size: var(--kx-font-13); font-weight: 500; }
.inviter-option small { margin-top: var(--kx-space-1); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.form-sidebar { position: sticky; top: var(--kx-space-6); display: grid; min-width: 0; gap: var(--kx-space-4); }
.preview-card, .flow-card { border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }
.preview-card { padding: var(--kx-space-6); }
.preview-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--kx-space-2); color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); }
.preview-card h2 { margin: var(--kx-space-6) 0 var(--kx-space-2); color: var(--kx-color-text); font-size: var(--kx-font-20); font-weight: 600; overflow-wrap: anywhere; }
.preview-card > p { margin: 0 0 var(--kx-space-3); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; overflow-wrap: anywhere; }
.preview-grid { display: grid; gap: var(--kx-space-3); margin-top: var(--kx-space-6); padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-border); }
.preview-grid div { display: grid; grid-template-columns: 64px minmax(0, 1fr); align-items: baseline; gap: var(--kx-space-2); }
.preview-grid span { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.preview-grid strong { color: var(--kx-color-text); font-size: var(--kx-font-13); font-weight: 400; line-height: 1.5; text-align: right; overflow-wrap: anywhere; }
.flow-card { display: flex; align-items: flex-start; gap: var(--kx-space-3); padding: var(--kx-space-4); color: var(--kx-color-brand); }
.flow-card > div { min-width: 0; }
.flow-card strong { color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); font-weight: 500; }
.flow-card p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.6; }
.form-actions { display: grid; gap: var(--kx-space-2); }
.submit-button { height: var(--kx-control-current-height); border-radius: var(--kx-radius-3); font-weight: 500; }
.cancel-button { min-height: var(--kx-control-current-height); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.cancel-button:hover { background: var(--kx-color-surface-subtle); }

@media (min-width: 768px) and (max-width: 1279px) {
  .form-layout { grid-template-columns: minmax(0, 1fr); }
  .form-sidebar { position: static; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
  .preview-card { grid-row: span 2; }
  .form-actions { align-self: end; }
}

@media (max-width: 767px) {
  .create-error { margin: var(--kx-space-4) var(--kx-space-4) 0; }
  .create-form { padding: var(--kx-space-4) var(--kx-space-4) calc(88px + env(safe-area-inset-bottom)); }
  .form-layout { display: flex; flex-direction: column; gap: var(--kx-space-4); }
  .form-main, .form-sidebar { position: static; width: 100%; min-width: 0; gap: var(--kx-space-6); }
  .form-section { border: 0; box-shadow: none; }
  .section-heading { align-items: flex-start; padding-bottom: var(--kx-space-2); border: 0; }
  .section-index { display: none; }
  .section-heading h2 { font-size: var(--kx-mobile-heading-size); }
  .section-heading > div { min-width: 0; }
  .field-grid { grid-template-columns: 1fr; padding: 0; }
  .rating-control { min-height: var(--kx-mobile-control-height); }
  .inviter-options { grid-template-columns: 1fr; }
  .preview-card { padding: var(--kx-space-4); border: 0; box-shadow: none; }
  .preview-card h2 { margin-top: var(--kx-space-3); font-size: var(--kx-mobile-heading-size); }
  .preview-grid { gap: var(--kx-space-2); margin-top: var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .flow-card { padding: 0 var(--kx-space-1); border: 0; background: transparent; box-shadow: none; }
  .form-actions {
    position: fixed;
    z-index: 44;
    right: 0;
    bottom: var(--kx-shell-mobile-nav-height);
    left: 0;
    grid-template-columns: 64px minmax(0, 1fr);
    gap: var(--kx-space-2);
    padding: var(--kx-space-3) var(--kx-space-4) calc(var(--kx-space-3) + env(safe-area-inset-bottom));
    border-top: 1px solid var(--kx-color-mobile-separator);
    background: var(--kx-color-mobile-material);
    -webkit-backdrop-filter: blur(18px);
    backdrop-filter: blur(18px);
  }
  .submit-button { order: 2; height: var(--kx-mobile-control-height); }
  .cancel-button { order: 1; min-height: var(--kx-mobile-control-height); border: 0; background: var(--kx-color-mobile-fill); color: var(--kx-color-text-secondary); }
}
@media (max-width: 767px) and (max-height: 500px) {
  .create-form { padding-bottom: calc(var(--kx-space-8) + env(safe-area-inset-bottom)); }
  .crm-mobile-submit { position: static; margin-top: var(--kx-space-4); padding: var(--kx-space-4) 0 0; background: transparent; -webkit-backdrop-filter: none; backdrop-filter: none; }
}
</style>
