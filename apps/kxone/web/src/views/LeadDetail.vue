<template>
  <div class="page-container" :class="{ 'desktop-detail-page': !mobileLayout }">
    <div class="detail-page-header">
      <div class="detail-header-inner">
        <button type="button" class="back-button" aria-label="返回" @click="goBack">
          <van-icon aria-hidden="true" name="arrow-left" size="18" />
          <span>返回线索列表</span>
        </button>
      </div>
    </div>

    <CrmStatePanel v-if="loadError" state="error" :title="loadError" action-label="重试" @action="fetchData" />
    <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
      <div v-if="lead" class="detail-layout-container">
      <div class="content-wrapper">
        <main class="detail-main-column">
          <LeadNotificationAlert v-if="deliveryAlert" :alert="deliveryAlert" :settings-to="userStore.isAdmin() ? '/admin/settings?section=notification' : ''" @dismiss="dismissDeliveryAlert" />
          <section class="detail-section lead-overview-card">
            <div class="overview-heading">
              <div class="overview-person-row">
                <span class="overview-avatar" aria-hidden="true">{{ customerInitial }}</span>
                <div class="overview-title-block">
                  <div class="overview-title-row"><h1>{{ displayCustomerName(lead) }}</h1></div>
                  <p class="overview-contact">{{ leadMobileText }}</p>
                </div>
                <div class="overview-heading-actions">
                  <button v-if="canEditProfile" type="button" class="edit-profile-button" aria-label="编辑资料" title="编辑资料" @click="openProfileEditor">
                    <van-icon aria-hidden="true" name="edit" />
                    <span class="sr-only">编辑资料</span>
                  </button>
                </div>
              </div>
              <div class="overview-status-row">
                <StatusBadge :status="lead.status" :payment-status="lead.payment_status" />
                <span class="stage-chip" :class="lead.business_stage || 'invitation'">{{ businessStageLabel(lead.business_stage) }}</span>
                <CrmRate :model-value="lead.star_rating || 0" readonly :size="14" color="var(--kx-color-warning)" void-color="var(--kx-color-border-strong)" />
              </div>
              <small v-if="!mobileLayout" class="overview-source">{{ sourceHeadline }} · {{ lead.lead_no || '线索号未生成' }} · {{ formatDate(lead.created_at) }}创建</small>
              <small v-else class="overview-source">{{ sourceInfo.platform }} · {{ formatDate(lead.created_at) }}创建</small>
              <div v-if="mobileLayout" class="overview-mobile-next" aria-label="下一步行动">
                <div :class="followupSchedule.tone"><span>下次回访</span><strong>{{ followupSchedule.label }}</strong><time v-if="followupSchedule.exact">{{ followupSchedule.exact }}</time></div>
                <div><span>当前负责人</span><strong>{{ currentOwnerText }}</strong></div>
                <div v-if="lead.status === 'converted'" class="overview-mobile-net"><span>净收款</span><strong>{{ formatAmount(lead.deal_amount - (lead.refund_amount || 0)) }}</strong></div>
              </div>
            </div>

            <div v-if="!mobileLayout" class="overview-info-grid">
              <div class="overview-info-item"><span>微信号</span><strong>{{ leadWechatText }}</strong></div>
              <div class="overview-info-item"><span>城市</span><strong>{{ lead.city || '未填写' }}</strong></div>
              <div class="overview-info-item"><span>咨询项目</span><strong>{{ lead.product_name || '暂未填写' }}</strong></div>
              <div class="overview-info-item"><span>邀约师</span><strong>{{ displayInviter(lead) }}</strong></div>
              <div class="overview-info-item"><span>谈单师</span><strong>{{ displayCloser(lead) }}</strong></div>
              <div class="overview-info-item"><span>创建人</span><strong>{{ creatorText }}</strong></div>
            </div>

            <details v-if="mobileLayout" class="mobile-workflow">
              <summary><span>业务进度</span><strong>{{ businessStageLabel(lead.business_stage) }}</strong><van-icon aria-hidden="true" name="arrow-down" /></summary>
              <p>{{ userStore.isWorkflowV2Enabled() ? '邀约与谈单双阶段' : '单链路跟进' }}</p>
              <div class="workflow-steps">
                <div v-for="(item, index) in workflowSteps" :key="item.key" class="workflow-step" :class="item.state">
                  <div class="workflow-node"><van-icon aria-hidden="true" v-if="['complete', 'success'].includes(item.state)" name="success" /></div>
                  <span>{{ item.label }}</span>
                  <i v-if="index < workflowSteps.length - 1" />
                </div>
              </div>
            </details>
            <div v-else class="workflow-block">
              <div class="section-inline-title"><strong>业务进度</strong><span>{{ userStore.isWorkflowV2Enabled() ? '邀约与谈单双阶段' : '单链路跟进' }}</span></div>
              <div class="workflow-steps">
                <div v-for="(item, index) in workflowSteps" :key="item.key" class="workflow-step" :class="item.state">
                  <div class="workflow-node"><van-icon aria-hidden="true" v-if="['complete', 'success'].includes(item.state)" name="success" /></div>
                  <span>{{ item.label }}</span>
                  <i v-if="index < workflowSteps.length - 1" />
                </div>
              </div>
            </div>
          </section>

          <section v-if="currentAppointment" class="detail-section appointment-card">
            <div class="section-card-header"><div><h2>当前预约</h2><span>{{ appointmentStatusLabel(currentAppointment.status) }}</span></div><button type="button" @click="router.push('/appointments')">查看排班</button></div>
            <div class="appointment-brief">
              <div class="appointment-date-tile" aria-hidden="true"><span>{{ appointmentDateParts.month }} 月</span><strong>{{ appointmentDateParts.day }}</strong></div>
              <div class="appointment-time-copy">
                <span class="sr-only">到校时间</span><strong v-if="!mobileLayout">{{ formatDateTime(currentAppointment.start_at) }} - {{ formatTime(currentAppointment.end_at) }}</strong>
                <template v-else><strong>{{ formatTime(currentAppointment.start_at) }} - {{ formatTime(currentAppointment.end_at) }}</strong><span class="appointment-calendar-date">{{ formatDateTime(currentAppointment.start_at).split(' ')[0] }}</span></template>
                <p><span>校区：{{ currentAppointment.campus || '未填写' }}</span><span>谈单师：{{ currentAppointment.closer_name || currentAppointment.closer_userid || '待主管分配' }}</span><span>到访 {{ currentAppointment.visitor_count || 1 }} 人</span></p>
              </div>
            </div>
            <p v-if="currentAppointment.handoff_pending" role="status">待 {{ currentAppointment.pending_inviter_name || currentAppointment.pending_inviter_userid }} 领取；领取后才能确认到访。</p><p>预约创建人：{{ currentAppointment.original_creator_name || '未知' }}</p>
            <div v-if="currentAppointment.customer_concerns || currentAppointment.handoff_notes || currentAppointment.special_notes" class="appointment-notes">
              <p v-if="currentAppointment.customer_concerns"><span>客户关注</span>{{ currentAppointment.customer_concerns }}</p>
              <p v-if="currentAppointment.handoff_notes"><span>交接说明</span>{{ currentAppointment.handoff_notes }}</p>
              <p v-if="currentAppointment.special_notes"><span>特别备注</span>{{ currentAppointment.special_notes }}</p>
            </div>
          </section>

          <section v-if="mobileLayout" class="detail-section mobile-profile-section" aria-label="客户资料">
            <h2>客户资料</h2>
            <dl class="mobile-profile-fields">
              <div><dt>微信号</dt><dd>{{ leadWechatText }}</dd></div>
              <div><dt>城市</dt><dd>{{ lead.city || '未填写' }}</dd></div>
              <div><dt>咨询项目</dt><dd>{{ lead.product_name || '暂未填写' }}</dd></div>
            </dl>
            <dl class="mobile-profile-owners">
              <div><dt>邀约师</dt><dd>{{ displayInviter(lead) }}</dd></div>
              <div><dt>谈单师</dt><dd>{{ displayCloser(lead) }}</dd></div>
            </dl>
            <details class="mobile-source-details">
              <summary><span>来源与创建</span><van-icon aria-hidden="true" name="arrow-down" /></summary>
              <dl class="mobile-profile-fields">
                <div><dt>线索编号</dt><dd>{{ lead.lead_no || '未生成' }}</dd></div>
                <div><dt>渠道来源</dt><dd>{{ sourceHeadline }}</dd></div>
                <div><dt>创建时间</dt><dd>{{ formatDateTime(lead.created_at) }}</dd></div>
                <div><dt>创建人</dt><dd>{{ creatorText }}</dd></div>
              </dl>
            </details>
          </section>

          <section class="detail-section detail-records-section">
            <CrmTabs v-model="activeDetailTab" :tabs="detailTabs" label="线索详情内容" id-prefix="lead-detail">
            <div v-if="activeDetailTab === 'followups'" class="followup-cards">
              <article v-for="item in followups" :key="item.id" class="followup-card">
                <div class="followup-card-header"><div><strong>{{ followupResultLabel(item.followup_result) }}</strong><CrmRate :model-value="item.star_rating || 0" readonly :size="13" color="var(--kx-color-warning)" void-color="var(--kx-color-border-strong)" /></div><time>{{ formatDateTime(item.created_at) }}</time></div>
                <p>{{ item.remark }}</p>
                <div class="followup-card-meta"><span>{{ item.sales_name || item.sales_userid || '跟进人未知' }} · {{ businessStageLabel(item.business_stage) }}</span><span v-if="item.next_followup_at">下次回访：{{ formatDateTime(item.next_followup_at) }}</span><span v-if="item.invalid_reason">无效原因：{{ item.invalid_reason }}</span></div>
              </article>
              <van-empty v-if="!followups.length" description="暂无跟进记录" />
            </div>
            <div v-else-if="activeDetailTab === 'timeline'" class="activity-timeline">
              <article v-for="item in activityTimeline" :key="item.key" class="activity-item">
                <div class="activity-marker" :class="`activity-marker--${item.tone}`" />
                <div class="activity-content">
                  <div class="activity-title-row">
                    <strong>{{ item.title }}</strong>
                    <time>{{ formatDateTime(item.createdAt) }}</time>
                  </div>
                  <p>{{ item.description }}</p>
                  <small v-if="item.remark">{{ item.remark }}</small>
                </div>
              </article>
            </div>
            <div v-else class="system-info-grid">
              <div><span>线索编号</span><strong>{{ lead.lead_no || '未生成' }}</strong></div>
              <div><span>来源平台</span><strong>{{ sourceInfo.platform }}</strong></div>
              <div><span>投放账户</span><strong>{{ sourceInfo.account || '暂无' }}</strong></div>
              <div><span>投放活动</span><strong>{{ sourceInfo.campaign || '暂无' }}</strong></div>
              <div v-if="lead.content_path"><span>来源路径</span><strong>{{ lead.content_path }}</strong></div>
              <div v-if="lead.content_entry_id"><span>CMS 内容版本</span><strong>内容 #{{ lead.content_entry_id }}<template v-if="lead.content_revision_id"> · 修订 #{{ lead.content_revision_id }}</template></strong></div>
              <div><span>创建时间</span><strong>{{ formatDateTime(lead.created_at) }}</strong></div>
              <div v-if="lead.converted_at"><span>成交时间</span><strong>{{ formatDateTime(lead.converted_at) }}</strong></div>
              <div v-if="lead.closed_at"><span>关闭时间</span><strong>{{ formatDateTime(lead.closed_at) }}</strong></div>
              <div v-if="lead.status === 'converted'"><span>付款状态</span><strong>{{ paymentStatusLabel(lead.payment_status) }}</strong></div>
              <div v-if="lead.deal_amount"><span>累计收款</span><strong>{{ formatAmount(lead.deal_amount) }}</strong></div>
              <div v-if="lead.refund_amount"><span>累计退款</span><strong>{{ formatAmount(lead.refund_amount) }}</strong></div>
              <div v-if="lead.close_reason" class="system-info-wide"><span>关闭原因</span><strong>{{ lead.close_reason }}</strong></div>
            </div>
            </CrmTabs>
          </section>
        </main>

        <aside v-if="!mobileLayout" class="detail-sidebar">
          <section class="detail-side-panel next-action-panel" :class="followupSchedule.tone">
            <div class="detail-side-title">下一步行动</div>
            <div class="next-action-time"><span>下次回访</span><strong>{{ followupSchedule.label }}</strong><time v-if="followupSchedule.exact">{{ followupSchedule.exact }}</time></div>
            <div class="detail-status-row"><span>当前负责人</span><strong>{{ currentOwnerText }}</strong></div>
            <div v-if="lead.status === 'converted'" class="detail-status-row"><span>净收款</span><strong>{{ formatAmount(lead.deal_amount - (lead.refund_amount || 0)) }}</strong></div>
            <div class="detail-status-row"><span>当前阶段</span><strong>{{ businessStageLabel(lead.business_stage) }}</strong></div>
            <div v-if="currentAppointment" class="detail-status-row"><span>预约到校</span><strong>{{ formatDateTime(currentAppointment.start_at) }}</strong></div>

            <div class="primary-action">
              <van-button v-if="canSelfClaim" type="primary" block :loading="selfClaiming" @click="onSelfClaim">我来跟进</van-button>
              <van-button v-else-if="canClaim" type="primary" block :loading="claiming" @click="onClaim">立即认领</van-button>
              <van-button v-else-if="canFollowup" type="primary" block @click="goToFollowup">{{ lead.business_stage === 'negotiation' ? '添加谈单跟进' : '添加邀约跟进' }}</van-button>
              <van-button v-else-if="canManage" type="primary" block @click="openAssignPanel">{{ lead.status === 'pending_dispatch' ? '分配线索' : '转派线索' }}</van-button>
              <van-button v-else-if="canCreateAppointment" type="primary" block @click="router.push(`/leads/${leadId}/appointment`)">创建预约单</van-button>
              <div v-else class="detail-no-actions">{{ noActionMessage }}</div>
            </div>
          </section>

          <section v-if="hasSecondaryActions" class="detail-side-panel detail-actions-panel">
            <div class="detail-side-title">辅助操作</div>
            <div class="secondary-actions">
              <van-button v-if="canManage && !isManagePrimary" type="default" @click="openAssignPanel">{{ lead.status === 'pending_dispatch' ? '分配线索' : '转派线索' }}</van-button>
              <van-button v-if="canRecommend" type="default" :loading="loadingRecommendations" @click="loadRecommendations">推荐邀约师</van-button>
              <van-button v-if="canAutoAssign" type="default" :loading="autoAssigning" @click="onAutoAssign">自动分配</van-button>
              <van-button v-if="canCreateAppointment && !isAppointmentPrimary" type="default" @click="router.push(`/leads/${leadId}/appointment`)">创建预约</van-button>
              <van-button v-if="userStore.isAppointmentEnabled()" type="default" @click="router.push('/appointments')">查看排班</van-button>
              <van-button v-if="canReassignCloser" type="default" @click="openCloserPanel">转交谈单师</van-button>
              <van-button v-if="canComplete || canEditDeal" type="success" plain @click="openPaymentEditor">{{ canEditDeal ? '编辑成交信息' : '标记成交' }}</van-button>
              <van-button v-if="canRestoreInvalid" type="primary" plain @click="openRestoreEditor">恢复为有效线索</van-button>
            </div>
            <button v-if="canReturnToPool || canClose || userStore.isAdmin()" type="button" class="more-actions-toggle" @click="showMoreActions = !showMoreActions">更多操作 <van-icon aria-hidden="true" :name="showMoreActions ? 'arrow-up' : 'arrow-down'" /></button>
            <div v-if="showMoreActions" class="risk-actions">
              <van-button v-if="canReturnToPool" type="warning" plain block @click="onReturnToPool">退回{{ userStore.poolTitle() }}</van-button>
              <van-button v-if="canClose" type="danger" plain block @click="showClosePanel = true">登记未成交</van-button>
              <van-button v-if="userStore.isAdmin()" type="danger" plain block @click="openDeletePanel">删除线索</van-button>
            </div>
          </section>
        </aside>
      </div>
      </div>

      <van-loading v-else-if="!loadError" type="spinner" size="50" vertical style="margin-top: 100px">
        加载中...
      </van-loading>
    </van-pull-refresh>

    <div v-if="mobileLayout && lead" class="mobile-detail-actionbar" aria-label="线索主操作">
      <van-button v-if="canSelfClaim" type="primary" block :loading="selfClaiming" @click="onSelfClaim">我来跟进</van-button>
      <van-button v-else-if="canClaim" type="primary" block :loading="claiming" @click="onClaim">立即认领</van-button>
      <van-button v-else-if="canFollowup" type="primary" block @click="goToFollowup">{{ lead.business_stage === 'negotiation' ? '添加谈单跟进' : '添加邀约跟进' }}</van-button>
      <van-button v-else-if="canManage" type="primary" block @click="openAssignPanel">{{ lead.status === 'pending_dispatch' ? '分配线索' : '转派线索' }}</van-button>
      <van-button v-else-if="canCreateAppointment" type="primary" block @click="router.push(`/leads/${leadId}/appointment`)">创建预约单</van-button>
      <span v-else class="mobile-detail-no-action">{{ noActionMessage }}</span>
      <button v-if="hasSecondaryActions" type="button" class="mobile-detail-more" aria-label="更多线索操作" @click="showMobileActions = true"><van-icon aria-hidden="true" name="ellipsis" />更多操作</button>
    </div>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="mobileActionsRef" v-model:show="showMobileActions" position="bottom" round class="mobile-detail-actions-popup" role="dialog" aria-modal="true" aria-label="其他线索操作">
      <div class="mobile-detail-actions-heading"><div><strong>其他操作</strong><span>{{ displayCustomerName(lead) }} · {{ businessStageLabel(lead?.business_stage) }}</span></div><button type="button" aria-label="关闭其他操作" @click="showMobileActions = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <div class="mobile-detail-action-list">
        <button v-if="canManage && !isManagePrimary" type="button" @click="showMobileActions = false; openAssignPanel()">{{ lead.status === 'pending_dispatch' ? '分配线索' : '转派线索' }}</button>
        <button v-if="canRecommend" type="button" @click="showMobileActions = false; loadRecommendations()">推荐邀约师</button>
        <button v-if="canAutoAssign" type="button" @click="showMobileActions = false; onAutoAssign()">自动分配</button>
        <button v-if="canCreateAppointment && !isAppointmentPrimary" type="button" @click="showMobileActions = false; router.push(`/leads/${leadId}/appointment`)">创建预约</button>
        <button v-if="userStore.isAppointmentEnabled()" type="button" @click="showMobileActions = false; router.push('/appointments')">查看排班</button>
        <button v-if="canReassignCloser" type="button" @click="showMobileActions = false; openCloserPanel()">转交谈单师</button>
        <button v-if="canComplete || canEditDeal" type="button" class="success" @click="showMobileActions = false; openPaymentEditor()">{{ canEditDeal ? '编辑成交信息' : '标记成交' }}</button>
        <button v-if="canRestoreInvalid" type="button" @click="showMobileActions = false; openRestoreEditor()">恢复为有效线索</button>
        <button v-if="canReturnToPool" type="button" class="warning" @click="showMobileActions = false; onReturnToPool()">退回{{ userStore.poolTitle() }}</button>
        <button v-if="canClose" type="button" class="danger" @click="showMobileActions = false; showClosePanel = true">登记未成交</button>
        <button v-if="userStore.isAdmin()" type="button" class="danger" @click="showMobileActions = false; openDeletePanel()">删除线索</button>
      </div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="assignEditorRef" v-model:show="showAssignPanel" class="assign-popup" :position="mobileLayout ? 'bottom' : 'center'" round :close-on-click-overlay="!assigning" role="dialog" aria-modal="true" aria-labelledby="assign-lead-title">
      <div class="operation-popup-heading"><h2 id="assign-lead-title">{{ lead?.status === 'pending_dispatch' ? '分配线索' : '转派线索' }}</h2><button data-autofocus type="button" aria-label="关闭分配线索" :disabled="assigning" @click="showAssignPanel = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <van-form class="crm-form" label-align="left" :disabled="assigning" @submit="onAssignSubmit">
        <van-cell-group inset title="选择邀约师或主管">
          <van-search v-model="salesKeyword" placeholder="搜索姓名或 UserID" />
          <div class="sales-list">
            <van-cell
              v-for="item in filteredSalesUsers"
              :key="item.wecom_userid || item.id"
              :label="item.is_self ? '当前主管 · 选择后直接接管并进入跟进' : `${item.role === 'manager' ? '主管' : '邀约师'} · ${item.wecom_userid} · 部门 ${item.department_id || '-'}`"
              class="sales-option"
              :class="{ 'sales-option--selected': isSalesSelected(item.wecom_userid) }"
              clickable
              role="button"
              :tabindex="assigning ? -1 : 0"
              :aria-disabled="assigning"
              :aria-pressed="isSalesSelected(item.wecom_userid)"
              @click="selectSalesUser(item)"
              @keydown.enter.prevent="selectSalesUser(item)"
              @keydown.space.prevent="selectSalesUser(item)"
            >
              <template #title>
                <div class="sales-option-title">
                  <span>{{ item.employee_name || item.wecom_userid }}</span>
                  <van-tag v-if="item.is_self" type="success" plain>由我负责</van-tag>
                  <van-tag v-else-if="item.role === 'manager'" type="warning" plain>主管</van-tag>
                  <van-tag v-if="isSalesSelected(item.wecom_userid)" type="primary" plain>
                    已选择
                  </van-tag>
                </div>
              </template>
              <template #right-icon>
                <div class="sales-option-check" :class="{ 'sales-option-check--selected': isSalesSelected(item.wecom_userid) }">
                  <van-icon aria-hidden="true" v-if="isSalesSelected(item.wecom_userid)" name="success" size="16" />
                </div>
              </template>
            </van-cell>
            <van-empty v-if="!loadingSalesUsers && filteredSalesUsers.length === 0" description="暂无可分配人员，请先在用户账户中启用邀约师或主管" />
          </div>
          <van-field
            v-model="assignForm.remark"
            name="remark"
            label="备注"
            type="textarea"
            rows="2"
            placeholder="可选"
          />
        </van-cell-group>

        <van-cell-group v-if="recommendations.length > 0" inset title="推荐邀约师" class="form-block">
          <van-cell
            v-for="item in recommendations"
            :key="item.sales_userid"
            :title="item.sales_name || item.sales_userid"
            :label="item.reason || '暂无推荐理由'"
            class="sales-option"
            :class="{ 'sales-option--selected': isSalesSelected(item.sales_userid) }"
            clickable
            role="button"
            :tabindex="assigning ? -1 : 0"
            :aria-disabled="assigning"
            :aria-pressed="isSalesSelected(item.sales_userid)"
            @click="selectRecommendation(item)"
            @keydown.enter.prevent="selectRecommendation(item)"
            @keydown.space.prevent="selectRecommendation(item)"
          >
            <template #value>
              <van-tag v-if="isSalesSelected(item.sales_userid)" type="primary" plain>已选择</van-tag>
              <van-tag v-else type="primary">{{ formatScore(item.total_score) }}</van-tag>
            </template>
          </van-cell>
        </van-cell-group>

        <div class="submit-button">
          <div v-if="selectedSalesName" class="selected-sales-summary">
            已选择：{{ selectedSalesName }}
          </div>
          <div class="payment-actions">
          <van-button native-type="button" :disabled="assigning" @click="showAssignPanel = false">取消</van-button>
          <van-button
            block
            type="primary"
            native-type="submit"
            :loading="assigning"
            :disabled="assigning || !assignForm.sales_userid"
          >
            确认{{ lead?.status === 'pending_dispatch' ? '分配' : '转派' }}
          </van-button>
          </div>
        </div>
      </van-form>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="paymentEditorRef" v-model:show="showConvertPanel" :position="mobileLayout ? 'bottom' : 'center'" round class="payment-popup" :close-on-click-overlay="!completing" role="dialog" aria-modal="true" aria-labelledby="payment-title">
      <div class="popup-title" id="payment-title">{{ conversionPanelTitle }}</div>
      <van-form class="crm-form" label-align="left" @submit="onConvertSubmit" :disabled="completing">
        <p v-if="operationError" class="operation-error" role="alert">{{ operationError }}</p>
        <van-cell-group inset>
          <van-cell title="成交路径" :value="conversionPathLabel" />
          <van-cell title="咨询项目" :value="lead?.product_name || '未填写'" />
          <van-field name="payment_status" label="付款状态" required :rules="[{ validator: () => !!completeForm.payment_status, message: '请选择付款状态' }]">
            <template #input>
              <select data-autofocus v-model="completeForm.payment_status" aria-label="付款状态" :disabled="completing" class="payment-select">
                <option disabled value="">请选择付款状态</option>
                <option value="partial">部分付款</option><option value="paid">已付全款</option><option value="refunded">已退款</option>
              </select>
            </template>
          </van-field>
          <van-field v-model="completeForm.deal_amount" name="deal_amount" label="累计收款金额" label-width="7em" type="text" inputmode="decimal" required placeholder="元，填写已实际收到的总额" :rules="[{ validator: validateDealAmount, message: '请输入大于 0 的金额，最多两位小数，上限 999999999.99 元' }]" />
          <van-field v-if="completeForm.payment_status === 'refunded'" v-model="completeForm.refund_amount" name="refund_amount" label="累计退款金额" label-width="7em" type="text" inputmode="decimal" required placeholder="元，填写实际退还的总额" :rules="[{ validator: validateRefundAmount, message: '退款金额须大于 0，且不超过累计收款金额，最多两位小数' }]" />
          <van-field v-if="canEditDeal" v-model="completeForm.reason" name="reason" label="修改原因" type="textarea" rows="2" maxlength="255" show-word-limit required placeholder="例如：金额录入错误、补齐尾款、办理退款" :rules="[{ validator: value => !!value.trim(), message: '请填写修改原因' }]" />
        </van-cell-group>
        <p class="payment-help">收款填写累计实际收款，含已退部分；净收款会扣除退款。保存后仍可修改，修改记录保留在业务过程中。</p>
        <div class="submit-button payment-actions">
          <van-button native-type="button" :disabled="completing" @click="showConvertPanel = false">取消</van-button>
          <van-button block type="primary" native-type="submit" :loading="completing" :disabled="completing">{{ canEditDeal ? '保存修改' : '确认成交' }}</van-button>
        </div>
      </van-form>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="restoreEditorRef" v-model:show="showRestorePanel" :position="mobileLayout ? 'bottom' : 'center'" round class="payment-popup" :close-on-click-overlay="!completing" role="dialog" aria-modal="true" aria-labelledby="restore-title">
      <div class="popup-title" id="restore-title">恢复为有效线索</div>
      <van-form class="crm-form" label-align="left" @submit="onRestoreSubmit" :disabled="completing">
        <p class="payment-help">恢复后可继续跟进和修改资料，原无效记录会保留。请重新安排下次回访时间。</p>
        <p v-if="operationError" class="operation-error" role="alert">{{ operationError }}</p>
        <van-cell-group inset><van-field v-model="restoreForm.reason" name="reason" label="恢复原因" type="textarea" rows="2" maxlength="255" show-word-limit required placeholder="例如：误标无效，已重新联系上客户" :rules="[{ validator: value => !!value.trim(), message: '请填写恢复原因' }]" /></van-cell-group>
        <div class="submit-button payment-actions">
          <van-button native-type="button" :disabled="completing" @click="showRestorePanel = false">取消</van-button>
          <van-button block type="primary" native-type="submit" :loading="completing" :disabled="completing">确认恢复</van-button>
        </div>
      </van-form>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="closerEditorRef" v-model:show="showCloserPanel" class="assign-popup" :position="mobileLayout ? 'bottom' : 'center'" round :close-on-click-overlay="!reassigningCloser" role="dialog" aria-modal="true" aria-labelledby="reassign-closer-title">
      <div class="operation-popup-heading"><h2 id="reassign-closer-title">转交谈单师</h2><button data-autofocus type="button" aria-label="关闭转交谈单师" :disabled="reassigningCloser" @click="showCloserPanel = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <van-cell-group inset title="选择新的谈单师">
        <van-cell v-for="item in closerUsers" :key="item.wecom_userid" :title="item.employee_name || item.wecom_userid" :label="item.wecom_userid" class="sales-option" :class="{ 'sales-option--selected': closerForm.closer_userid === item.wecom_userid }" clickable role="button" :tabindex="reassigningCloser ? -1 : 0" :aria-disabled="reassigningCloser" :aria-pressed="closerForm.closer_userid === item.wecom_userid" @click="selectCloserUser(item)" @keydown.enter.prevent="selectCloserUser(item)" @keydown.space.prevent="selectCloserUser(item)">
          <template #right-icon><div class="sales-option-check" :class="{ 'sales-option-check--selected': closerForm.closer_userid === item.wecom_userid }" aria-hidden="true"><van-icon v-if="closerForm.closer_userid === item.wecom_userid" name="success" size="16" /></div></template>
        </van-cell>
        <van-field v-model="closerForm.reason" :disabled="reassigningCloser" label="转交原因" type="textarea" rows="3" maxlength="200" show-word-limit placeholder="必须填写" />
      </van-cell-group>
      <div class="submit-button payment-actions"><van-button native-type="button" :disabled="reassigningCloser" @click="showCloserPanel = false">取消</van-button><van-button block type="primary" :loading="reassigningCloser" :disabled="reassigningCloser || !closerForm.closer_userid || closerForm.closer_userid === lead?.closer_userid" @click="onReassignCloser">确认转交</van-button></div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="closeEditorRef" v-model:show="showClosePanel" class="assign-popup" :position="mobileLayout ? 'bottom' : 'center'" round :close-on-click-overlay="!completing" role="dialog" aria-modal="true" aria-labelledby="close-lead-title">
      <div class="operation-popup-heading"><h2 id="close-lead-title">登记未成交</h2><button data-autofocus type="button" aria-label="关闭登记未成交" :disabled="completing" @click="showClosePanel = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <van-form class="crm-form" label-align="left" :disabled="completing" @submit="onCloseSubmit">
        <van-cell-group inset>
          <van-field
            v-model="completeForm.close_reason"
            name="close_reason"
            label="未成交原因"
            type="textarea"
            rows="3"
            placeholder="请输入未成交原因"
            :rules="[{ required: true, message: '请输入未成交原因' }]"
          />
        </van-cell-group>
        <div class="submit-button payment-actions">
          <van-button native-type="button" :disabled="completing" @click="showClosePanel = false">取消</van-button>
          <van-button block type="danger" native-type="submit" :loading="completing" :disabled="completing">
            确认未成交
          </van-button>
        </div>
      </van-form>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="deleteEditorRef" v-model:show="showDeletePanel" :position="mobileLayout ? 'bottom' : 'center'" round :close-on-click-overlay="!deleting" class="delete-lead-popup" role="dialog" aria-modal="true" aria-labelledby="delete-lead-title">
      <div class="delete-lead-heading"><h2 id="delete-lead-title">删除线索</h2><button data-autofocus type="button" aria-label="关闭删除线索" :disabled="deleting" @click="showDeletePanel = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <p class="delete-lead-warning">线索将从 CRM 工作列表移除，待发送的通知和提醒会取消。操作会留下审计记录，管理员可在系统设置的线索回收站恢复。</p>
      <van-form class="crm-form" label-align="left" @submit="onDeleteSubmit">
        <van-cell-group inset>
          <van-field :model-value="lead?.lead_no || ''" label="线索编号" readonly />
          <van-field v-model.trim="deleteForm.confirm_lead_no" name="confirm_lead_no" label="确认编号" placeholder="输入上方完整线索编号" autocomplete="off" :rules="[{ validator: value => value === lead?.lead_no, message: '请输入完整且一致的线索编号' }]" />
          <van-field v-model.trim="deleteForm.reason" name="reason" label="删除原因" type="textarea" rows="2" maxlength="200" show-word-limit placeholder="例如：迁移验收测试清理" :rules="[{ required: true, message: '请填写删除原因' }]" />
        </van-cell-group>
        <p v-if="deleteError" class="operation-error" role="alert">{{ deleteError }}</p>
        <div class="submit-button"><van-button block type="danger" native-type="submit" :loading="deleting">确认删除</van-button></div>
      </van-form>
    </van-popup>

    <van-popup
      :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }"
      ref="profileEditorRef"
      role="dialog" aria-modal="true" aria-label="编辑客户资料"
      v-model:show="showProfileEditor"
      :position="mobileLayout ? 'bottom' : 'center'"
      round
      :close-on-click-overlay="!profileSaving"
      :class="['profile-editor', { 'profile-editor--desktop': !mobileLayout }]"
    >
      <div class="profile-editor-header">
        <div>
          <strong>编辑客户资料</strong>
          <span>仅修正基础资料，不改变线索状态和回访时间</span>
        </div>
        <button type="button" aria-label="关闭编辑资料" :disabled="profileSaving" @click="showProfileEditor = false">
          <van-icon aria-hidden="true" name="cross" />
        </button>
      </div>
      <van-form class="crm-form" label-align="left" @submit="onProfileSubmit">
        <van-cell-group inset>
          <van-field
            v-model="profileForm.customer_name"
            name="customer_name"
            label="客户姓名"
            maxlength="64"
            show-word-limit
            placeholder="请输入客户姓名"
            :rules="[{ required: true, message: '请输入客户姓名' }]"
          />
          <van-field
            v-model="profileForm.customer_mobile"
            name="customer_mobile"
            label="联系电话"
            type="tel"
            maxlength="24"
            placeholder="可输入 +86、空格或连字符"
          />
          <van-field
            v-model="profileForm.city"
            name="city"
            label="城市"
            maxlength="64"
            show-word-limit
            placeholder="请输入城市"
          />
          <van-field
            v-model="profileForm.product_name"
            name="product_name"
            label="咨询项目"
            maxlength="128"
            show-word-limit
            placeholder="请输入咨询项目"
          />
          <LeadSourceField v-model="profileForm.source_channel" :legacy-value="lead?.source_channel || ''" :required="Boolean(lead?.source_channel)" />
        </van-cell-group>
        <p v-if="profileError" class="profile-editor-tip" role="alert">{{ profileError }} <button type="button" @click="refreshProfileVersion">刷新资料版本（保留输入）</button></p>
        <p class="profile-editor-tip">若清空电话，该线索必须已留有微信号。修改记录将保留在“业务过程”中。</p>
        <div class="profile-editor-actions">
          <van-button type="default" :disabled="profileSaving" @click="showProfileEditor = false">取消</van-button>
          <van-button type="primary" native-type="submit" :loading="profileSaving">保存资料</van-button>
        </div>
      </van-form>
    </van-popup>
  </div>
</template>

<script setup>
import LeadSourceField from '@/components/LeadSourceField.vue'
import CrmRate from '@/components/CrmRate.vue'
import { reactive, ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showFailToast, showSuccessToast } from 'vant'
import {
  assignLead,
  autoAssignLead,
  claimLead,
  closeLead,
  convertLead,
  deleteLead,
  getLeadDetail,
  getRecommendSales,
  enterPublicPool,
  reassignNegotiationCloser,
  reassignLead,
  selfClaimLead,
  updateLeadProfile,
  updateLeadDeal,
  restoreInvalidLead
} from '@/api/lead'
import { getAppointments } from '@/api/appointment'
import { useUserStore } from '@/stores/user'
import { getCloserUsers, getSalesUsers } from '@/api/user'
import { describeFollowupSchedule, formatDate, formatDateTime } from '@/utils/date'
import { buildLeadWorkflowSteps, detailSourceInfo, leadOwnerEmptyLabel } from '@/utils/leadDetailDisplay'
import { displayCustomerName, displayLeadCloser, displayLeadInviter, displaySourceCampaign } from '@/utils/leadDisplay'
import { followupResultLabel, localizeSystemRemark } from '@/utils/leadLocale'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { displayLeadMobile, displayLeadWechat } from '@/utils/leadContact'
import { buildLeadProfilePayload, canEditLeadProfile, profileChangeLabels, profileChangeSummary, validateLeadProfilePhone } from '@/utils/leadProfile'
import { canCorrectLead, correctionStage, paymentStatusLabel, validMoney, leadListReturnPath } from '@/utils/leadPayment'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import CrmTabs from '@/components/CrmTabs.vue'
import LeadNotificationAlert from '@/components/LeadNotificationAlert.vue'
import { notificationAlert, hasQueueFailure, dismissQueueFailure } from '@/utils/leadNotification'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const { isMobile: mobileLayout } = useResponsiveLayout()

const lead = ref(null)
const loadError = ref('')
const followups = ref([])
const dispatchRecords = ref([])
const activities = ref([])
const notificationFailures = ref([])
const notificationStatus = ref('')
const queueFailed = ref(hasQueueFailure(route.params.id, userStore.userInfo?.wecom_userid))
const deliveryAlert = computed(() => notificationAlert({ failures: notificationFailures.value, queueFailed: queueFailed.value, status: notificationStatus.value }))
const dismissDeliveryAlert = () => { dismissQueueFailure(route.params.id, userStore.userInfo?.wecom_userid); queueFailed.value = false }
const currentAppointment = ref(null)
const refreshing = ref(false)
const claiming = ref(false)
const selfClaiming = ref(false)
const assigning = ref(false)
const autoAssigning = ref(false)
const completing = ref(false)
const loadingRecommendations = ref(false)
const showAssignPanel = ref(false)
const showConvertPanel = ref(false)
const showClosePanel = ref(false)
const showCloserPanel = ref(false)
const recommendations = ref([])
const salesUsers = ref([])
const closerUsers = ref([])
const reassigningCloser = ref(false)
const loadingSalesUsers = ref(false)
const salesKeyword = ref('')
const activeDetailTab = ref('followups')
const showMoreActions = ref(false)
const showMobileActions = ref(false)
const assignEditorRef = ref(null)
const closerEditorRef = ref(null)
const closeEditorRef = ref(null)
useOverlayFocus(showAssignPanel, assignEditorRef, () => { if (!assigning.value) showAssignPanel.value = false })
useOverlayFocus(showCloserPanel, closerEditorRef, () => { if (!reassigningCloser.value) showCloserPanel.value = false })
useOverlayFocus(showClosePanel, closeEditorRef, () => { if (!completing.value) showClosePanel.value = false })
const mobileActionsRef = ref(null)
useOverlayFocus(showMobileActions, mobileActionsRef, () => { showMobileActions.value = false })
const showDeletePanel = ref(false)
const deleteEditorRef = ref(null)
const deleting = ref(false)
useOverlayFocus(showDeletePanel, deleteEditorRef, () => { if (!deleting.value) showDeletePanel.value = false })
const deleteError = ref('')
const deleteForm = reactive({ confirm_lead_no: '', reason: '' })
const showProfileEditor = ref(false)
const profileEditorRef = ref(null)
const profileSaving = ref(false)

const assignForm = reactive({
  sales_userid: '',
  remark: ''
})

const completeForm = reactive({
  deal_amount: '',
  payment_status: 'partial',
  refund_amount: '',
  reason: '',
  expected_version: null,
  close_reason: ''
})
const closerForm = reactive({ closer_userid: '', reason: '' })
const paymentEditorRef = ref(null)
const restoreEditorRef = ref(null)
const showRestorePanel = ref(false)
const restoreForm = reactive({ reason: '', expected_version: null })
const operationError = ref('')
useOverlayFocus(showConvertPanel, paymentEditorRef, () => { if (!completing.value) showConvertPanel.value = false })
useOverlayFocus(showRestorePanel, restoreEditorRef, () => { if (!completing.value) showRestorePanel.value = false })
const profileError = ref('')
let profileOriginal = {}
const profileForm = reactive({ customer_name: '', customer_mobile: '', city: '', product_name: '', source_channel: '' })

useOverlayFocus(showProfileEditor, profileEditorRef, () => {
  if (!profileSaving.value) showProfileEditor.value = false
})

const detailTabs = computed(() => [
  { value: 'followups', label: '跟进记录', count: followups.value.length },
  { value: 'timeline', label: '业务过程', count: activityTimeline.value.length },
  { value: 'system', label: '系统信息' }
])

const leadId = route.params.id

const canClaim = computed(() => {
  return (userStore.isInviter() || userStore.role === 'manager') && lead.value?.assigned_inviter_userid === userStore.userInfo?.wecom_userid && lead.value?.business_stage === 'invitation' && lead.value?.status === 'assigned_pending_claim'
})

const canSelfClaim = computed(() => userStore.role === 'manager' && lead.value?.business_stage === 'invitation' && lead.value?.status === 'pending_dispatch')

const canManage = computed(() => {
  if (userStore.isDispatcher()) {
    return lead.value?.business_stage === 'invitation' && lead.value?.status === 'pending_dispatch'
  }
  return userStore.isManager() && lead.value?.business_stage === 'invitation' && ['pending_dispatch', 'assigned_pending_claim', 'claimed', 'following'].includes(lead.value?.status)
})

const canRecommend = computed(() => userStore.isManager() && lead.value?.business_stage === 'invitation')

const canAutoAssign = computed(() => {
  return userStore.isManager() && lead.value?.status === 'pending_dispatch'
})

const canFollowup = computed(() => {
  const currentUserID = userStore.userInfo?.wecom_userid
  if (!currentUserID || !['claimed', 'following'].includes(lead.value?.status)) return false
  if (lead.value?.business_stage === 'negotiation') {
    return userStore.isCloser() && (lead.value?.closer_userid === currentUserID || lead.value?.current_owner_userid === currentUserID)
  }
  return (userStore.isInviter() || userStore.role === 'manager') && (lead.value?.inviter_userid === currentUserID || lead.value?.current_owner_userid === currentUserID)
})

const canComplete = computed(() => {
	if (lead.value?.result_status !== 'active' || !['claimed', 'following'].includes(lead.value?.status)) return false
  const currentUserID = userStore.userInfo?.wecom_userid
	if (userStore.isManager() || userStore.isAdmin()) return true
	if (!currentUserID) return false
	if (lead.value?.business_stage === 'invitation') {
		return userStore.isInviter() && [lead.value?.inviter_userid, lead.value?.current_owner_userid, lead.value?.owner_userid].includes(currentUserID)
	}
	return lead.value?.business_stage === 'negotiation' && userStore.isCloser() && [lead.value?.closer_userid, lead.value?.current_owner_userid].includes(currentUserID)
})

const canCorrect = computed(() => canCorrectLead(lead.value, userStore.role, userStore.userInfo?.wecom_userid))
const canEditDeal = computed(() => lead.value?.status === 'converted' && canCorrect.value)
const canRestoreInvalid = computed(() => lead.value?.status === 'invalid' && canCorrect.value)
const conversionPathLabel = computed(() => correctionStage(lead.value) === 'invitation' ? '邀约阶段直接成交' : '谈单阶段成交')
const conversionPanelTitle = computed(() => canEditDeal.value ? '编辑成交信息' : '登记成交')

const canClose = computed(() => {
	if (lead.value?.business_stage !== 'negotiation' || !['claimed', 'following'].includes(lead.value?.status)) return false
	const currentUserID = userStore.userInfo?.wecom_userid
	if (userStore.isAdmin()) return true
	if (userStore.role === 'manager') return Boolean(currentUserID && lead.value?.dispatch_manager_userid === currentUserID)
	return userStore.isCloser() && Boolean(currentUserID && [lead.value?.closer_userid, lead.value?.current_owner_userid].includes(currentUserID))
})

const canReassignCloser = computed(() => userStore.isManager() && lead.value?.business_stage === 'negotiation' && lead.value?.result_status === 'active')

const canReturnToPool = computed(() => {
  return userStore.isPoolEnabled() && (userStore.isManager() || userStore.isAdmin()) &&
    lead.value?.business_stage === 'invitation' &&
    lead.value?.result_status === 'active' &&
    lead.value?.ownership_state !== 'public_pool' &&
    !lead.value?.current_appointment_id &&
    !lead.value?.arrived_at &&
    !lead.value?.delivered_at &&
    !['appointment_pending', 'appointment_scheduled'].includes(lead.value?.ownership_state)
})

const canCreateAppointment = computed(() => {
  if (loadError.value || !userStore.isAppointmentEnabled() || !lead.value || currentAppointment.value) return false
  if (lead.value.business_stage !== 'invitation' || lead.value.result_status !== 'active') return false
  if (lead.value.ownership_state === 'public_pool' || !lead.value.inviter_userid) return false
  const currentUserID = userStore.userInfo?.wecom_userid
  return userStore.isManager() || userStore.isAdmin() ||
    (userStore.isInviter() && lead.value.inviter_userid === currentUserID)
})

const canEditProfile = computed(() => {
  return canEditLeadProfile(lead.value, userStore.role, userStore.userInfo?.wecom_userid)
})

const sourceInfo = computed(() => detailSourceInfo(lead.value))
const customerInitial = computed(() => Array.from(displayCustomerName(lead.value))[0] || '客')
const leadMobileText = computed(() => displayLeadMobile(lead.value, userStore.isDispatcher()))
const leadWechatText = computed(() => displayLeadWechat(lead.value, userStore.isDispatcher()))
const sourceHeadline = computed(() => [sourceInfo.value.platform, sourceInfo.value.account, sourceInfo.value.city].filter(Boolean).join(' · '))
const creatorText = computed(() => {
  if (!lead.value?.created_by_userid) return '系统接入'
  return `${lead.value.created_by_name || lead.value.created_by_userid} · ${roleLabel(lead.value.created_by_role)}`
})
const workflowSteps = computed(() => buildLeadWorkflowSteps(lead.value, currentAppointment.value, userStore.isWorkflowV2Enabled()))
const appointmentDateParts = computed(() => {
  const date = new Date(currentAppointment.value?.start_at)
  return Number.isNaN(date.getTime()) ? { month: '—', day: '—' } : { month: date.getMonth() + 1, day: date.getDate() }
})
const followupSchedule = computed(() => describeFollowupSchedule(lead.value?.next_followup_due_at))
const currentOwnerText = computed(() => {
  const owner = correctionStage(lead.value) === 'negotiation' ? displayCloser(lead.value) : displayInviter(lead.value)
  return owner && !['未分配', '未交付'].includes(owner) ? owner : leadOwnerEmptyLabel(lead.value)
})
const isManagePrimary = computed(() => canManage.value && !canSelfClaim.value && !canClaim.value && !canFollowup.value)
const isAppointmentPrimary = computed(() => canCreateAppointment.value && !canSelfClaim.value && !canClaim.value && !canFollowup.value && !canManage.value)
const hasSecondaryActions = computed(() => (
  (canManage.value && !isManagePrimary.value) ||
  canRecommend.value ||
  canAutoAssign.value ||
  (canCreateAppointment.value && !isAppointmentPrimary.value) ||
  (userStore.isAppointmentEnabled() && !userStore.isDispatcher()) ||
  canReassignCloser.value ||
  canComplete.value || canEditDeal.value || canRestoreInvalid.value ||
	canClose.value ||
  canReturnToPool.value || userStore.isAdmin()
))
const noActionMessage = computed(() => userStore.isDispatcher()
  ? '已分配的历史线索仅供查看'
  : '当前状态暂无待处理主任务')

const activityActorName = item => {
  if (item.actor_name) return item.actor_name
  const actorID = item.actor_userid
  const candidates = [
    [lead.value?.inviter_userid, lead.value?.inviter_name],
    [lead.value?.assigned_inviter_userid, lead.value?.assigned_inviter_name],
    [lead.value?.closer_userid, lead.value?.closer_name],
    [lead.value?.current_owner_userid, lead.value?.current_owner_name],
    [lead.value?.created_by_userid, lead.value?.created_by_name]
  ]
  return candidates.find(([id, name]) => actorID && id === actorID && name)?.[1] || '内部账号'
}

const activityTimeline = computed(() => {
  if (!lead.value) return []
  const items = [{
    key: `created-${lead.value.id}`,
    title: '创建线索',
    description: createdActivityDescription(lead.value),
    remark: displaySourceCampaign(lead.value.source_campaign),
    createdAt: lead.value.created_at,
    tone: 'neutral'
  }]
  dispatchRecords.value.forEach(item => {
    const sales = item.sales_name || item.sales_userid || '销售'
    const manager = item.manager_name || item.manager_userid || '主管'
    const action = {
      assign: { title: '分配线索', description: `${manager} 分配给 ${sales}`, tone: 'info' },
      self_assign: { title: '主管亲自跟进', description: `${manager} 选择由自己负责邀约`, tone: 'success' },
      self_create: { title: '创建并直接跟进', description: `${sales} 创建线索并开始跟进`, tone: 'success' },
      reassign: { title: '转派线索', description: `${manager} 转派给 ${sales}`, tone: 'warning' },
      claim: { title: '销售领取', description: `${sales} 已领取线索`, tone: 'success' },
      cancel_assign: { title: '取消分配', description: `${manager} 取消了对 ${sales} 的分配`, tone: 'neutral' }
    }[item.action_type] || { title: '线索处理', description: `${manager} 处理了线索`, tone: 'neutral' }
    items.push({ key: `dispatch-${item.id}`, ...action, remark: localizeSystemRemark(item.remark), createdAt: item.created_at })
  })
  activities.value.forEach(item => {
    const metadata = parseActivityJSON(item.metadata_json)
    const after = parseActivityJSON(item.to_value_json)
    if (['deal_updated', 'invalid_restored'].includes(item.activity_type)) {
      const before = parseActivityJSON(item.from_value_json)
      items.push({ key: `activity-${item.id}`, title: item.activity_type === 'deal_updated' ? '修改成交信息' : '恢复有效线索',
        description: item.activity_type === 'deal_updated'
          ? `${activityActorName(item)}：${paymentStatusLabel(before.payment_status)} → ${paymentStatusLabel(after.payment_status)}；累计收款 ${formatAmount(before.deal_amount)} → ${formatAmount(after.deal_amount)}；累计退款 ${formatAmount(before.refund_amount || 0)} → ${formatAmount(after.refund_amount || 0)}`
          : `${activityActorName(item)} 恢复线索，可继续跟进`, remark: metadata.reason, createdAt: item.created_at, tone: 'info' })
    } else if (item.activity_type === 'historical_owner_cycle_repaired') {
      items.push({ key: `activity-${item.id}`, title: '历史记录核对修复', description: '依据已有交接与跟进记录补齐责任周期信息', remark: '', createdAt: item.created_at, tone: 'info' })
    } else if (item.activity_type === 'arrived_delivered') {
      items.push({ key: `activity-${item.id}`, title: '确认到访并交付', description: `线索正式交付给谈单师 ${metadata.closer_userid || after.closer_userid || lead.value.closer_userid || '-'}`, remark: metadata.confirmation || '', createdAt: item.created_at, tone: 'success' })
    } else if (item.activity_type === 'closer_reassigned') {
      items.push({ key: `activity-${item.id}`, title: '转交谈单师', description: `${metadata.previous_closer_userid || '原谈单师'} 转交给 ${after.closer_userid || '-'}`, remark: metadata.reason || '', createdAt: item.created_at, tone: 'warning' })
    } else if (item.activity_type === 'customer_profile_updated') {
      const before = parseActivityJSON(item.from_value_json)
      items.push({
        key: `activity-${item.id}`,
        title: '更新客户资料',
        description: `${activityActorName(item)} 修改了${profileChangeLabels(metadata.changed_fields).join('、') || '客户资料'}`,
        remark: profileChangeSummary(before, after),
        createdAt: item.created_at,
        tone: 'info'
      })
		} else if (['invitation_direct_convert', 'negotiation_convert'].includes(item.activity_type)) {
			const direct = item.activity_type === 'invitation_direct_convert'
			items.push({
				key: `activity-${item.id}`,
				title: direct ? '邀约阶段直接成交' : '谈单成交',
				description: `${activityActorName(item)} 登记成交，${paymentStatusLabel(after.payment_status)}，累计收款 ${formatAmount(after.deal_amount || 0)}，累计退款 ${formatAmount(after.refund_amount || 0)}`,
				remark: metadata.product_name ? `咨询项目：${metadata.product_name}` : '',
				createdAt: item.created_at,
				tone: 'success'
			})
    }
  })
  return items.sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime())
})

const assignmentCandidates = computed(() => {
  const currentUserID = userStore.userInfo?.wecom_userid
  const candidates = salesUsers.value.map(item => ({ ...item, is_self: userStore.role === 'manager' && item.wecom_userid === currentUserID }))
  if (userStore.role === 'manager' && currentUserID && !candidates.some(item => item.wecom_userid === currentUserID)) {
    candidates.unshift({
      wecom_userid: currentUserID,
      employee_name: userStore.userInfo?.employee_name || userStore.userInfo?.name || '我',
      role: 'manager',
      is_self: true
    })
  }
  return candidates
})

const filteredSalesUsers = computed(() => {
  const keyword = salesKeyword.value.trim().toLowerCase()
  if (!keyword) {
    return assignmentCandidates.value
  }
  return assignmentCandidates.value.filter(item => {
    return (item.employee_name || '').toLowerCase().includes(keyword) ||
      (item.wecom_userid || '').toLowerCase().includes(keyword)
  })
})

const selectedSalesName = computed(() => {
  const selectedUserID = assignForm.sales_userid
  if (!selectedUserID) {
    return ''
  }
  const selectedUser = assignmentCandidates.value.find(item => item.wecom_userid === selectedUserID)
  if (selectedUser) {
    return selectedUser.employee_name || selectedUser.wecom_userid
  }
  const selectedRecommendation = recommendations.value.find(item => item.sales_userid === selectedUserID)
  return selectedRecommendation?.sales_name || selectedRecommendation?.sales_userid || selectedUserID
})

const displayInviter = (item) => displayLeadInviter(item) || '未分配'
const displayCloser = (item) => {
  const explicitCloser = item?.closer_name || item?.closer_userid
  if (explicitCloser) return explicitCloser
  if (item?.business_stage === 'negotiation') return displayLeadCloser(item) || '谈单师待确认'
  const appointmentCloser = currentAppointment.value?.closer_name || currentAppointment.value?.closer_userid
  return appointmentCloser ? `${appointmentCloser}（预约）` : '尚未交付谈单师'
}
const parseActivityJSON = (value) => { try { return JSON.parse(value || '{}') } catch { return {} } }
const createdActivityDescription = item => {
  const creator = item.created_by_name || item.created_by_userid || '系统'
  if (['inviter', 'sales'].includes(item.created_by_role)) return `${creator} 创建线索并开始跟进`
  if (item.created_by_role === 'closer') return `${creator} 创建线索并指定邀约师`
  if (item.created_by_role === 'dispatcher') return `${creator} 创建线索并进入待分配队列`
  return `线索进入${item.dispatch_manager_name || item.dispatch_manager_userid || '主管'}的待分配队列`
}

const goBack = () => {
  const target = leadListReturnPath(route.query.return_to)
  if (router.options.history.state.back === target) router.back()
  else router.replace(target)
}

const openDeletePanel = () => {
  if (!userStore.isAdmin() || !lead.value) return
  deleteForm.confirm_lead_no = ''
  deleteForm.reason = ''
  deleteError.value = ''
  showDeletePanel.value = true
}

const onDeleteSubmit = async () => {
  if (deleting.value || !lead.value || deleteForm.confirm_lead_no !== lead.value.lead_no || !deleteForm.reason.trim()) return
  deleting.value = true
  deleteError.value = ''
  try {
    await deleteLead(leadId, {
      lead_no: deleteForm.confirm_lead_no,
      expected_version: lead.value.version,
      expected_profile_version: lead.value.profile_version,
      reason: deleteForm.reason.trim()
    })
    showSuccessToast('线索已删除')
    router.replace(leadListReturnPath(route.query.return_to))
  } catch (error) {
    if (error.response?.status === 409) deleteError.value = '线索资料已变化，请关闭窗口并刷新详情后重试。'
    else if (error.response?.status === 404) deleteError.value = '线索已不存在，请返回列表。'
    else deleteError.value = '删除失败，请核对权限和网络状态后重试。'
  } finally {
    deleting.value = false
  }
}

const openProfileEditor = () => {
  if (!canEditProfile.value) return
  profileError.value = ''
  profileForm.customer_name = lead.value?.customer_name || ''
  profileForm.customer_mobile = lead.value?.customer_mobile || ''
  profileForm.city = lead.value?.city || ''
  profileForm.product_name = lead.value?.product_name || ''
  profileForm.source_channel = lead.value?.source_channel || ''
  profileOriginal = { ...profileForm }
  showProfileEditor.value = true
}

const refreshProfileVersion = async () => {
  await fetchData()
  if (loadError.value) { profileError.value = '刷新失败，输入已保留，请重试'; return }
  for (const key of Object.keys(profileForm)) {
    const latest = lead.value?.[key] || ''
    if (profileForm[key] === profileOriginal[key]) profileForm[key] = latest
    profileOriginal[key] = latest
  }
  profileError.value = '已刷新最新资料；你的修改已保留，未改动字段已同步，请核对后保存。'
}
const onProfileSubmit = async () => {
  const phone = profileForm.customer_mobile.trim()
  const phoneCheck = validateLeadProfilePhone(phone, lead.value?.customer_wechat)
  if (!phoneCheck.valid && phone !== String(lead.value?.customer_mobile || '').trim()) {
    showFailToast(phone ? '请输入正确的联系电话' : '联系电话和微信号至少保留一项')
    return
  }
  profileSaving.value = true
  profileError.value = ''
  try {
    const result = await updateLeadProfile(leadId, buildLeadProfilePayload(lead.value, profileForm))
    showProfileEditor.value = false
    await fetchData()
    if ((result.warnings || []).includes('legacy_contact_needs_review')) {
      showSuccessToast('资料已保存；历史联系方式需要核对，请确认后修正')
    } else if ((result.warnings || []).includes('duplicate_mobile')) {
      showSuccessToast('资料已保存；该电话还存在于其他有效线索，请注意核对')
    } else {
      showSuccessToast('客户资料已保存')
    }
  } catch (error) {
    if (error.response?.status === 409) {
      profileError.value = '资料已被其他人更新，输入已保留。请刷新资料版本，核对后再保存。'
    } else { profileError.value = '保存失败，输入已保留，请检查内容或网络后重试' }
  } finally {
    profileSaving.value = false
  }
}

const fetchData = async () => {
  loadError.value = ''
  try {
    const data = await getLeadDetail(leadId)
    lead.value = data.lead || data
    followups.value = data.followups || []
    dispatchRecords.value = data.dispatch_records || []
    activities.value = data.activities || []
    notificationFailures.value = data.notification_failures || []
    notificationStatus.value = data.notification_status || ''
    if (userStore.isAppointmentEnabled() && !userStore.isDispatcher()) {
      const appointmentData = await getAppointments({ lead_id: leadId })
      const appointmentItems = appointmentData.appointments || []
      currentAppointment.value = appointmentItems.find(item => ['pending_assignment', 'scheduled'].includes(item.status) && item.id === lead.value.current_appointment_id) || appointmentItems.find(item => ['pending_assignment', 'scheduled'].includes(item.status)) || null
    } else {
      currentAppointment.value = null
    }
  } catch {
    loadError.value = lead.value ? '部分信息更新失败，请重试后再操作' : '线索加载失败，可能是网络异常或访问权限已变化'
  }
}

const onRefresh = async () => {
  await fetchData()
  refreshing.value = false
}

const onClaim = async () => {
  try {
    await showConfirmDialog({
      title: '确认认领',
      message: '确定要领取这条线索吗？'
    })

    claiming.value = true
    await claimLead(leadId)
    showSuccessToast('领取成功')
    await fetchData()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('领取失败:', error)
    }
  } finally {
    claiming.value = false
  }
}

const onSelfClaim = async () => {
  try {
    await showConfirmDialog({ title: '我来跟进', message: '确认后您将作为邀约师负责该线索，并立即开始计算首次回访时间。' })
    selfClaiming.value = true
    await selfClaimLead(leadId)
    showSuccessToast('已由您亲自跟进')
    await fetchData()
  } finally {
    selfClaiming.value = false
  }
}

const openAssignPanel = async () => {
  if (!canManage.value) return
  assignForm.sales_userid = lead.value?.assigned_sales_userid || lead.value?.owner_userid || ''
  assignForm.remark = ''
  showAssignPanel.value = true
  await loadSalesUsers()
}

const openCloserPanel = async () => {
  closerForm.closer_userid = lead.value?.closer_userid || ''
  closerForm.reason = ''
  showCloserPanel.value = true
  if (!closerUsers.value.length) {
    const data = await getCloserUsers()
    closerUsers.value = data.bindings || []
  }
}

const onReassignCloser = async () => {
  if (reassigningCloser.value) return
  if (!closerForm.reason.trim()) { showFailToast('请填写转交原因'); return }
  reassigningCloser.value = true
  try {
    await reassignNegotiationCloser(leadId, { closer_userid: closerForm.closer_userid, expected_version: lead.value.version, reason: closerForm.reason.trim() })
    showSuccessToast('谈单师已转交')
    showCloserPanel.value = false
    await fetchData()
  } finally { reassigningCloser.value = false }
}

const loadRecommendations = async () => {
  loadingRecommendations.value = true
  try {
    const data = await getRecommendSales(leadId)
    recommendations.value = data.recommendations || []
    if (recommendations.value.length > 0) {
      showAssignPanel.value = true
      assignForm.sales_userid = recommendations.value[0].sales_userid
      await loadSalesUsers()
      showSuccessToast('已生成推荐邀约师')
    }
  } finally {
    loadingRecommendations.value = false
  }
}

const selectRecommendation = (item) => {
  if (assigning.value) return
  assignForm.sales_userid = item.sales_userid
}

const loadSalesUsers = async () => {
  if (salesUsers.value.length > 0) {
    return
  }
  loadingSalesUsers.value = true
  try {
    const data = await getSalesUsers()
    salesUsers.value = data.bindings || []
  } finally {
    loadingSalesUsers.value = false
  }
}

const selectSalesUser = (item) => {
  if (assigning.value) return
  assignForm.sales_userid = item.wecom_userid
}

const selectCloserUser = (item) => {
  if (reassigningCloser.value) return
  closerForm.closer_userid = item.wecom_userid
}

const isSalesSelected = (salesUserID) => {
  return !!salesUserID && assignForm.sales_userid === salesUserID
}

const onAssignSubmit = async () => {
  if (assigning.value) return
  if (userStore.isDispatcher() && lead.value?.status !== 'pending_dispatch') {
    showFailToast('该线索已不在待分配状态')
    showAssignPanel.value = false
    await fetchData()
    return
  }
  if (!assignForm.sales_userid) {
    showFailToast('请选择邀约师或主管')
    return
  }
	assigning.value = true
	try {
		if (userStore.role === 'manager' && assignForm.sales_userid === userStore.userInfo?.wecom_userid) {
			const wasPending = lead.value?.status === 'pending_dispatch'
			await selfClaimLead(leadId)
			showSuccessToast(wasPending ? '已由您亲自跟进' : '线索已由您接管')
			showAssignPanel.value = false
			await fetchData()
			return
		}
		const payload = {
      sales_userid: assignForm.sales_userid,
      remark: assignForm.remark,
      expected_version: lead.value?.version
    }
    if (lead.value?.status === 'pending_dispatch') {
      await assignLead(leadId, payload)
      showSuccessToast('线索已分配')
    } else {
      await reassignLead(leadId, payload)
      showSuccessToast('线索已转派')
    }
    showAssignPanel.value = false
    await fetchData()
  } finally {
    assigning.value = false
  }
}

const onAutoAssign = async () => {
  try {
    await showConfirmDialog({
      title: '自动分配',
      message: '系统将按推荐算法选择邀约师并完成分配。'
    })
    autoAssigning.value = true
    const data = await autoAssignLead(leadId)
    showSuccessToast(`已分配给 ${data.assigned_sales_name || data.assigned_sales_userid}`)
    await fetchData()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('自动分配失败:', error)
    }
  } finally {
    autoAssigning.value = false
  }
}

const goToFollowup = () => {
  router.push({ path: `/leads/${leadId}/followup`, query: route.query })
}

const onReturnToPool = async () => {
  try {
    await showConfirmDialog({ title: `退回${userStore.poolTitle()}`, message: `确认后该线索将释放当前${userStore.isWorkflowV2Enabled() ? '邀约师' : '销售'}。` })
    await enterPublicPool(leadId, { reason: 'manual' })
    showSuccessToast(`已退回${userStore.poolTitle()}`)
    router.push('/public-pool')
  } catch (error) {
    if (error !== 'cancel') console.error('退回公海失败:', error)
  }
}

const openPaymentEditor = () => {
  completeForm.deal_amount = canEditDeal.value ? String(lead.value.deal_amount || '') : ''
  completeForm.payment_status = canEditDeal.value ? (lead.value.payment_status || '') : 'partial'
  completeForm.refund_amount = canEditDeal.value ? String(lead.value.refund_amount || '') : ''
  completeForm.reason = ''
  completeForm.expected_version = lead.value.version
  operationError.value = ''
  showConvertPanel.value = true
}

const openRestoreEditor = () => {
  restoreForm.reason = ''
  restoreForm.expected_version = lead.value.version
  operationError.value = ''
  showRestorePanel.value = true
}

const handleOperationError = error => {
  operationError.value = error?.response?.data?.error === 'lead_version_conflict'
    ? '线索已被他人修改。本次输入已保留，请取消并刷新详情后重新核对。'
    : '保存失败，已保留填写内容，请检查网络或输入后重试。'
}

const onConvertSubmit = async () => {
  if (completing.value) return
  completing.value = true
  operationError.value = ''
  try {
    const save = canEditDeal.value ? updateLeadDeal : convertLead
    await save(leadId, {
      deal_amount: Number(completeForm.deal_amount),
      payment_status: completeForm.payment_status,
      refund_amount: completeForm.payment_status === 'refunded' ? Number(completeForm.refund_amount) : 0,
      expected_version: completeForm.expected_version,
      reason: completeForm.reason.trim()
    })
    showSuccessToast(canEditDeal.value ? '成交信息已更新' : '成交信息已登记')
    showConvertPanel.value = false
    await fetchData()
  } catch (error) {
    handleOperationError(error)
  } finally {
    completing.value = false
  }
}

const onRestoreSubmit = async () => {
  if (completing.value) return
  completing.value = true
  operationError.value = ''
  try {
    await restoreInvalidLead(leadId, { expected_version: restoreForm.expected_version, reason: restoreForm.reason.trim() })
    showSuccessToast('已恢复为有效线索')
    showRestorePanel.value = false
    await fetchData()
  } catch (error) {
    handleOperationError(error)
  } finally {
    completing.value = false
  }
}

const validateDealAmount = value => validMoney(value) && Number(value) > 0
const validateRefundAmount = value => validMoney(value) && Number(value) > 0 && Number(value) <= Number(completeForm.deal_amount)

const onCloseSubmit = async () => {
  if (completing.value) return
  completing.value = true
  try {
    await closeLead(leadId, {
      close_reason: completeForm.close_reason
    })
    showSuccessToast('已登记未成交')
    showClosePanel.value = false
    completeForm.close_reason = ''
    await fetchData()
  } finally {
    completing.value = false
  }
}

const formatScore = (score) => {
  if (score === undefined || score === null) {
    return '-'
  }
  return `${Math.round(score)} 分`
}

const formatAmount = (amount) => {
  return `¥${Number(amount || 0).toFixed(2)}`
}

const formatTime = (value) => new Date(value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })

const appointmentStatusLabel = (status) => ({
  pending_assignment: '待分配谈单师',
  scheduled: '已排期',
  cancelled: '已取消',
  no_show: '客户爽约',
  arrived_delivered: '已到访并交付',
  completed: '已完成'
})[status] || status

const roleLabel = (role) => ({ admin: '管理员', manager: '销售主管', dispatcher: '线索调度员', sales: '旧销售', inviter: '邀约师', closer: '谈单师' }[role] || role || '-')

const businessStageLabel = (stage) => ({ invitation: '邀约阶段', negotiation: '谈单阶段', finished: '已结束' })[stage] || '邀约阶段'

onMounted(() => {
  fetchData()
})
</script>

<style scoped>
.assign-popup { width: min(560px, calc(100vw - 32px)); max-height: 90dvh; overflow-y: auto; padding-bottom: env(safe-area-inset-bottom); }
.operation-popup-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-4); }
.operation-popup-heading h2 { margin: 0; font-size: var(--kx-font-16); font-weight: 600; }
.operation-popup-heading button { display: grid; width: 44px; height: 44px; flex: 0 0 auto; place-items: center; border: 0; border-radius: var(--kx-radius-3); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); cursor: pointer; }
.payment-popup { width: min(560px, 100%); max-height: 90dvh; overflow-y: auto; }
.payment-select { width: 100%; min-height: 44px; padding: 0 var(--kx-space-2); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-2); color: var(--kx-color-text); background: var(--kx-color-surface); }
.payment-help, .operation-error { margin: var(--kx-space-4); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; }
.operation-error { color: var(--kx-color-danger); }
.delete-lead-popup { width: min(520px, 100%); max-height: 90dvh; overflow-y: auto; padding-bottom: max(var(--kx-space-4), env(safe-area-inset-bottom)); }
.delete-lead-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-4); }
.delete-lead-heading h2 { margin: 0; font-size: var(--kx-font-16); font-weight: 600; }
.delete-lead-heading button { display: grid; width: var(--kx-mobile-control-height); height: var(--kx-mobile-control-height); place-items: center; border: 0; border-radius: var(--kx-radius-3); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); cursor: pointer; }
.delete-lead-warning { margin: 0 var(--kx-space-4) var(--kx-space-4); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; }
.payment-actions { display: grid; grid-template-columns: 1fr 2fr; gap: var(--kx-space-3); }

.detail-page-header { position: sticky; top: 0; z-index: 20; background: var(--kx-color-toolbar); border-bottom: 1px solid var(--kx-color-border); backdrop-filter: blur(16px); -webkit-backdrop-filter: blur(16px); }

.detail-header-inner {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: center;
  min-height: 46px;
  padding: 0 12px;
}

.detail-title-block {
  min-width: 0;
  text-align: center;
}

.back-button {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  width: fit-content;
  min-height: 44px;
  padding: 0 8px 0 4px;
  border: 0;
  border-radius: var(--kx-radius-3);
  background: transparent;
  color: var(--kx-color-brand);
  cursor: pointer;
  font: inherit;
  font-size: 14px;
  font-weight: 600;
}

.back-button:hover,
.back-button:active {
  background: var(--kx-color-brand-soft);
}

.detail-page-title {
  min-width: 0;
  color: var(--kx-color-text);
  font-size: 16px;
  font-weight: 700;
}

.detail-page-subtitle {
  margin-top: 2px;
  overflow: hidden;
  color: var(--kx-color-text-secondary);
  font-size: 12px;
  font-weight: 400;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-main-column,
.detail-sidebar {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-section { overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }

.followup-section {
  margin: 0 16px;
  border-radius: var(--kx-radius-3);
  background: var(--kx-color-surface);
}

.followup-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 42px;
  padding: 0 16px;
  border-bottom: 1px solid var(--kx-color-disabled-surface);
}

.followup-section-header h2 {
  margin: 0;
  color: var(--kx-color-text);
  font-size: 14px;
  font-weight: 600;
}

.followup-section-header span {
  color: var(--kx-color-text-muted);
  font-size: 12px;
}

.followup-list {
  margin: 0;
}

.detail-sidebar {
  margin-top: 16px;
}

.detail-side-panel { margin: 0; padding: var(--kx-space-6); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }

.detail-side-title {
  margin-bottom: 12px;
  color: var(--kx-color-text);
  font-size: var(--kx-font-16);
  font-weight: 600;
}

.detail-status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: var(--kx-control-height);
  color: var(--kx-color-text-secondary);
  font-size: 13px;
}

.detail-status-row strong {
  min-width: 0;
  overflow: hidden;
  color: var(--kx-color-text);
  font-size: 13px;
  font-weight: 400;
  text-align: right;
  overflow-wrap: anywhere;
}

.action-buttons {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.desktop-detail-page .action-buttons :deep(.van-button) {
  height: var(--kx-control-height);
  border-radius: var(--kx-radius-3);
}

.detail-no-actions {
  padding: 12px;
  border: 1px dashed var(--kx-color-border-strong);
  border-radius: var(--kx-radius-3);
  color: var(--kx-color-text-secondary);
  font-size: 13px;
  text-align: center;
}

.popup-title {
  padding: var(--kx-space-6) var(--kx-space-4) var(--kx-space-4);
  font-size: 16px;
  font-weight: 600;
  color: var(--kx-color-text);
  text-align: center;
}

.form-block {
  margin-top: 16px;
}

.detail-main-column .form-block {
  margin-top: 0;
}

:deep(.van-cell__value) {
  flex: 2;
  overflow-wrap: anywhere;
}

.submit-button {
  margin: 16px;
}

.selected-sales-summary {
  margin-bottom: 10px;
  padding: 10px 12px;
  border: 1px solid var(--kx-color-border);
  border-radius: var(--kx-radius-3);
  background: var(--kx-color-brand-soft);
  color: var(--kx-color-brand);
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.timeline-section {
  padding-bottom: 4px;
  background: var(--kx-color-surface);
}

.timeline-section-header,
.followup-section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
}

.timeline-section-header h2,
.followup-section-header h2 {
  margin: 0;
  color: var(--kx-color-text);
  font-size: 14px;
}

.timeline-section-header span,
.followup-section-header span {
  color: var(--kx-color-text-secondary);
  font-size: 12px;
}

.activity-timeline {
  padding: var(--kx-space-6);
}

.activity-item {
  position: relative;
  display: grid;
  grid-template-columns: 14px minmax(0, 1fr);
  gap: 10px;
  padding-bottom: 18px;
}

.activity-item:not(:last-child)::before {
  content: '';
  position: absolute;
  top: 12px;
  bottom: 0;
  left: 5px;
  width: 1px;
  background: var(--kx-color-border-strong);
}

.activity-marker {
  position: relative;
  z-index: 1;
  width: 11px;
  height: 11px;
  margin-top: 4px;
  border: 2px solid var(--kx-color-surface);
  border-radius: 50%;
  background: var(--kx-color-text-muted);
  box-shadow: 0 0 0 1px var(--kx-color-text-muted);
}

.activity-marker--info,
.activity-marker--primary { background: var(--kx-color-brand); box-shadow: 0 0 0 1px var(--kx-color-brand); }
.activity-marker--success { background: var(--kx-color-success); box-shadow: 0 0 0 1px var(--kx-color-success); }
.activity-marker--warning { background: var(--kx-color-warning); box-shadow: 0 0 0 1px var(--kx-color-warning); }

.activity-content { min-width: 0; }
.activity-title-row { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 12px; }
.activity-title-row strong { color: var(--kx-color-text); font-size: 14px; }
.activity-title-row time { flex: none; color: var(--kx-color-text-muted); font-size: 12px; }
.activity-content p { overflow-wrap: anywhere; white-space: pre-wrap; margin: 4px 0 0; color: var(--kx-color-text-secondary); font-size: 13px; line-height: 1.5; }
.activity-content small { display: block; margin-top: 5px; color: var(--kx-color-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }

.sales-list {
  max-height: 45vh;
  overflow-y: auto;
  padding: 4px 0;
}

.sales-option {
  transition: background-color var(--kx-motion-fast), box-shadow var(--kx-motion-fast);
}

.sales-option--selected {
  background: var(--kx-color-brand-soft);
  box-shadow: inset 3px 0 0 var(--kx-color-brand);
}

.sales-option-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--kx-color-text);
  font-weight: 600;
}

.sales-option-title span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sales-option-check {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  margin-left: 8px;
  border: 1px solid var(--kx-color-border-strong);
  border-radius: 999px;
  color: var(--kx-color-surface);
  background: var(--kx-color-surface);
}

.sales-option-check--selected {
  border-color: var(--kx-color-brand);
  background: var(--kx-color-brand);
}

.lead-overview-card,
.appointment-card,
.detail-records-section {
  padding: 0;
  background: var(--kx-color-surface);
}

.overview-heading { padding: var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); }
.overview-person-row { display: flex; align-items: center; gap: var(--kx-space-4); }
.overview-avatar { display: grid; width: 48px; height: 48px; flex: 0 0 auto; place-items: center; border-radius: 50%; background: var(--kx-color-brand-soft); color: var(--kx-color-brand); font-size: var(--kx-font-20); }
.overview-heading-actions { display: flex; flex: 0 0 auto; align-items: center; gap: var(--kx-space-3); }
.edit-profile-button { display: grid; width: var(--kx-control-height); height: var(--kx-control-height); place-items: center; border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; font-size: var(--kx-font-16); }
.edit-profile-button:hover { border-color: var(--kx-color-brand); color: var(--kx-color-brand); background: var(--kx-color-brand-soft); }

.overview-title-block { min-width: 0; flex: 1; }
.overview-title-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.overview-title-row h1 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-26); line-height: 1.3; font-weight: 600; letter-spacing: -.4px; overflow-wrap: anywhere; }
.overview-contact { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.5; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.overview-status-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--kx-space-2); margin: var(--kx-space-4) 0 var(--kx-space-3); }
.overview-source { display: block; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.7; overflow-wrap: anywhere; }

.stage-chip { display: inline-flex; align-items: center; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); font-weight: 400; }
.stage-chip.negotiation { color: var(--kx-color-text-muted); }
.stage-chip.finished { color: var(--kx-color-text-muted); }

.overview-info-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--kx-space-6); padding: var(--kx-space-6); }
.overview-mobile-next { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--kx-space-3); margin-top: var(--kx-space-4); padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-border); }
.overview-mobile-next > div { min-width: 0; }
.overview-mobile-next > div:nth-child(2) { text-align: right; }
.overview-mobile-next span, .overview-mobile-next time { display: block; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.5; }
.overview-mobile-next strong { display: block; margin-top: var(--kx-space-1); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); font-weight: 500; line-height: 1.5; overflow-wrap: anywhere; }
.overview-mobile-next .warning strong { color: var(--kx-color-warning); }
.overview-mobile-next .danger strong { color: var(--kx-color-danger); }
.overview-mobile-net { grid-column: 1 / -1; }
.overview-info-item { min-width: 0; }
.overview-info-item span,
.system-info-grid span { display: block; margin-bottom: 5px; color: var(--kx-color-text-muted); font-size: 12px; }
.overview-info-item strong,
.system-info-grid strong { display: block; overflow-wrap: anywhere; color: var(--kx-color-text); font-size: var(--kx-font-14); font-weight: 400; line-height: 1.6; }

.workflow-block { padding: var(--kx-space-6); border-top: 1px solid var(--kx-color-border); }
.section-inline-title { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 18px; }
.section-inline-title strong { color: var(--kx-color-text); font-size: 14px; }
.section-inline-title span { color: var(--kx-color-text-muted); font-size: 12px; }
.workflow-steps { display: flex; align-items: flex-start; }
.workflow-step { position: relative; display: flex; flex: 1; min-width: 0; flex-direction: column; align-items: center; gap: 8px; color: var(--kx-color-text-muted); font-size: 12px; text-align: center; }
.workflow-step i { position: absolute; z-index: 0; top: 9px; left: calc(50% + 12px); width: calc(100% - 24px); height: 2px; background: var(--kx-color-border); }
.workflow-node { position: relative; z-index: 1; display: flex; width: 20px; height: 20px; align-items: center; justify-content: center; border: 2px solid var(--kx-color-border-strong); border-radius: 50%; background: var(--kx-color-surface); font-size: var(--kx-font-12); }
.workflow-step.complete, .workflow-step.success { color: var(--kx-color-success); font-weight: 600; }
.workflow-step.complete .workflow-node, .workflow-step.success .workflow-node { border-color: var(--kx-color-success); background: var(--kx-color-success); color: var(--kx-color-surface); }
.workflow-step.complete i { background: var(--kx-color-success); }
.workflow-step.active { color: var(--kx-color-brand-hover); font-weight: 700; }
.workflow-step.active .workflow-node { border-color: var(--kx-color-brand); box-shadow: 0 0 0 4px var(--kx-color-brand-soft); }
.workflow-step.warning { color: var(--kx-color-warning); font-weight: 600; }
.workflow-step.warning .workflow-node { border-color: var(--kx-color-warning); background: var(--kx-color-warning-soft); }

.section-card-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 62px; padding: 0 20px; border-bottom: 1px solid var(--kx-color-border); }
.section-card-header > div { display: flex; align-items: baseline; gap: 10px; }
.section-card-header h2 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-16); }
.section-card-header span { color: var(--kx-color-brand); font-size: 12px; font-weight: 600; }
.section-card-header button { min-height: var(--kx-control-height); flex: 0 0 auto; border: 0; background: transparent; color: var(--kx-color-brand); cursor: pointer; font-size: 13px; }

.appointment-brief { display: flex; align-items: center; gap: var(--kx-space-4); padding: var(--kx-space-6); }
.appointment-date-tile { flex: 0 0 48px; overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); text-align: center; }
.appointment-date-tile span { display: block; padding: var(--kx-space-1); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); font-size: var(--kx-font-12); }
.appointment-date-tile strong { display: block; padding: var(--kx-space-1); color: var(--kx-color-text); font-size: var(--kx-font-24); font-weight: 500; line-height: 1.2; font-variant-numeric: tabular-nums; }
.appointment-time-copy { min-width: 0; }
.appointment-time-copy > strong { color: var(--kx-color-text); font-size: var(--kx-font-14); font-weight: 500; line-height: 1.5; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.appointment-time-copy p { display: flex; flex-wrap: wrap; gap: var(--kx-space-1) var(--kx-space-2); margin: var(--kx-space-2) 0 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.6; overflow-wrap: anywhere; }
.appointment-card > p { margin: 0 var(--kx-space-6) var(--kx-space-4); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; overflow-wrap: anywhere; }
.appointment-notes { padding: 0 var(--kx-space-6) var(--kx-space-6); }
.appointment-notes p { overflow-wrap: anywhere; white-space: pre-wrap; margin: 8px 0 0; color: var(--kx-color-text-secondary); font-size: 13px; line-height: 1.6; }
.appointment-notes span { display: inline-block; min-width: 72px; color: var(--kx-color-text-muted); }


.detail-records-section :deep(.crm-tablist) { margin: 0 var(--kx-space-6); padding-top: var(--kx-space-2); }
.followup-cards { padding: var(--kx-space-6); }
.followup-card { position: relative; padding: 0 0 var(--kx-space-6) var(--kx-space-6); }
.followup-card::before { position: absolute; top: 5px; left: 0; width: 8px; height: 8px; border: 2px solid var(--kx-color-surface); border-radius: 50%; background: var(--kx-color-brand); box-shadow: 0 0 0 1px var(--kx-color-border-strong); content: ''; }
.followup-card:not(:last-child)::after { position: absolute; top: 18px; bottom: 0; left: 5px; width: 1px; background: var(--kx-color-border); content: ''; }
.followup-card:last-child { padding-bottom: 0; }
.followup-card-header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
.followup-card-header > div { display: flex; flex-wrap: wrap; min-width: 0; align-items: center; gap: 10px; }
.followup-card-header strong { color: var(--kx-color-text); font-size: 14px; }
.followup-card-header time { font-variant-numeric: tabular-nums; color: var(--kx-color-text-muted); font-size: 12px; }
.followup-card > p { margin: 10px 0 8px; color: var(--kx-color-text-secondary); font-size: 14px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.followup-card-meta { display: flex; flex-wrap: wrap; overflow-wrap: anywhere; gap: 8px 18px; color: var(--kx-color-text-secondary); font-size: 12px; }
.system-info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--kx-space-6); padding: var(--kx-space-3) var(--kx-space-6) var(--kx-space-6); }
.system-info-grid > div { min-width: 0; padding: 12px 0; border-bottom: 1px solid var(--kx-color-disabled-surface); }
.system-info-wide { grid-column: 1 / -1; }

.next-action-panel.warning .next-action-time { margin: var(--kx-space-4) 0; padding: 0 0 var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); }
.next-action-panel.danger .next-action-time { background: transparent; }
.next-action-time { margin-bottom: 12px; padding: 12px; border-radius: var(--kx-radius-3); background: var(--kx-color-surface-subtle); }
.next-action-time span, .next-action-time time { display: block; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.next-action-time strong { display: block; margin: var(--kx-space-1) 0; color: var(--kx-color-text); font-size: var(--kx-font-24); font-weight: 600; line-height: 1.3; font-variant-numeric: tabular-nums; }
.next-action-panel.warning .next-action-time strong { color: var(--kx-color-warning); }
.next-action-panel.danger .next-action-time strong { color: var(--kx-color-danger); }
.primary-action { margin-top: 14px; }
.primary-action :deep(.van-button) { height: var(--kx-control-height); border-radius: var(--kx-radius-3); font-weight: 600; }
.secondary-actions { display: flex; flex-direction: column; gap: 0; }
.secondary-actions :deep(.van-button) { width: 100%; min-height: 44px; height: auto; margin: 0; padding: var(--kx-space-2) 0; justify-content: flex-start; border: 0; border-radius: 0; background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); text-align: left; }
.secondary-actions :deep(.van-button + .van-button) { border-top: 1px solid var(--kx-color-border); }
.secondary-actions :deep(.van-button__content) { justify-content: space-between; width: 100%; }
.secondary-actions :deep(.van-button__text) { width: 100%; }
.secondary-actions :deep(.van-button__text)::after { float: right; width: 6px; height: 6px; margin: 7px 3px 0 8px; border-top: 1px solid currentColor; border-right: 1px solid currentColor; transform: rotate(45deg); content: ''; color: var(--kx-color-text-muted); }
.more-actions-toggle { display: flex; width: 100%; min-height: 44px; align-items: center; justify-content: space-between; gap: var(--kx-space-2); margin-top: var(--kx-space-2); padding: var(--kx-space-2) 0; border: 0; border-top: 1px solid var(--kx-color-border); background: transparent; color: var(--kx-color-text-secondary); cursor: pointer; font-size: var(--kx-font-14); }
.risk-actions { display: flex; flex-direction: column; gap: 8px; padding-top: 8px; }
.risk-actions :deep(.van-button) { height: var(--kx-control-height); border-radius: var(--kx-radius-3); }

.profile-editor { width: min(520px, calc(100vw - 32px)); overflow: hidden; background: var(--kx-color-surface-subtle); }
.profile-editor-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 20px 22px 16px; background: var(--kx-color-surface); border-bottom: 1px solid var(--kx-color-border); }
.profile-editor-header > div { min-width: 0; }
.profile-editor-header strong { display: block; color: var(--kx-color-text); font-size: 18px; }
.profile-editor-header span { display: block; margin-top: 5px; color: var(--kx-color-text-secondary); font-size: 12px; line-height: 1.5; }
.profile-editor-header button { display: flex; width: var(--kx-control-height); height: var(--kx-control-height); flex: 0 0 auto; align-items: center; justify-content: center; border: 0; border-radius: 50%; background: var(--kx-color-disabled-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.profile-editor :deep(.van-cell-group--inset) { margin: 16px 16px 0; }
.profile-editor-tip { margin: 12px 22px 0; color: var(--kx-color-text-secondary); font-size: 12px; line-height: 1.6; }
.profile-editor-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding: 18px 16px 20px; }
.profile-editor-actions :deep(.van-button) { height: var(--kx-control-height); border-radius: var(--kx-radius-3); }

.desktop-detail-page {
  min-height: 100vh;
  background: var(--kx-color-canvas);
}

.desktop-detail-page .detail-page-header { background: var(--kx-color-toolbar); border-bottom-color: var(--kx-color-border); }

.desktop-detail-page .detail-header-inner {
  display: flex;
  align-items: center;
  width: min(var(--kx-page-max-width), 100%);
  min-height: 52px;
  margin: 0 auto;
  padding: 0 var(--kx-space-6);
}

.desktop-detail-page .back-button {
  height: var(--kx-control-height);
  padding: 0 8px;
  border: 1px solid transparent;
  background: transparent;
}

.desktop-detail-page .back-button:hover {
  border-color: var(--kx-color-border);
  background: var(--kx-color-brand-soft);
}

.desktop-detail-page .detail-page-title {
  font-size: 18px;
}

.detail-layout-container { max-width: var(--kx-page-max-width); margin: 0 auto; padding: var(--kx-space-6); container: lead-detail / inline-size; }
.content-wrapper { padding: 0; }
.desktop-detail-page .content-wrapper { display: grid; grid-template-columns: minmax(0, 1fr) 280px; align-items: start; gap: var(--kx-space-6); }

.desktop-detail-page .detail-main-column,
.desktop-detail-page .detail-sidebar {
  gap: 16px;
  margin-top: 0;
}

.desktop-detail-page .detail-section,
.desktop-detail-page .detail-side-panel {
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--kx-color-border);
  border-radius: var(--kx-card-radius);
  background: var(--kx-color-surface);
}

.desktop-detail-page .detail-sidebar {
  position: sticky;
  top: var(--kx-space-6);
}

.mobile-detail-actionbar { position: fixed; z-index: 44; right: 0; bottom: var(--kx-shell-mobile-nav-height); left: 0; display: none; min-height: 68px; align-items: center; gap: var(--kx-space-3); padding: var(--kx-space-3) var(--kx-space-4) calc(var(--kx-space-3) + env(safe-area-inset-bottom)); border-top: 1px solid var(--kx-color-border); background: var(--kx-color-toolbar); backdrop-filter: blur(16px); -webkit-backdrop-filter: blur(16px); }
.mobile-detail-actions-popup { max-height: 90dvh; overflow-y: auto; padding: var(--kx-space-4) var(--kx-space-4) calc(var(--kx-space-6) + env(safe-area-inset-bottom)); }
.mobile-detail-actions-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--kx-space-3); padding-bottom: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); }
.mobile-detail-actions-heading > div { display: grid; min-width: 0; gap: var(--kx-space-1); overflow-wrap: anywhere; }
.mobile-detail-actions-heading strong { font-size: var(--kx-font-16); }
.mobile-detail-actions-heading span { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.mobile-detail-actions-heading button { width: 44px; height: 44px; flex: 0 0 auto; border: 0; background: transparent; color: var(--kx-color-text-muted); }
.mobile-detail-action-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--kx-space-2); padding-top: var(--kx-space-4); }
.mobile-detail-action-list button { min-height: 44px; padding: var(--kx-space-2); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); font: inherit; font-size: var(--kx-font-13); }
.mobile-detail-action-list .success { border-color: var(--kx-color-border); color: var(--kx-color-success); }
.mobile-detail-action-list .warning { border-color: var(--kx-color-border); color: var(--kx-color-warning); }
.mobile-detail-action-list .danger { grid-column: 1 / -1; border-color: var(--kx-color-border); color: var(--kx-color-danger); }

.desktop-detail-page .detail-main-column :deep(.van-cell-group),
.desktop-detail-page .followup-section :deep(.van-cell-group) {
  margin: 0;
}

.desktop-detail-page .detail-main-column :deep(.van-cell-group__title) {
  padding: 14px 16px 8px;
  color: var(--kx-color-text);
  font-size: 14px;
  font-weight: 700;
}

.desktop-detail-page .detail-main-column :deep(.van-cell) {
  min-height: 44px;
  padding: 10px 16px;
}

.desktop-detail-page .followup-section :deep(.van-empty) {
  padding: 26px 0 30px;
}

.desktop-detail-page .form-block,
.desktop-detail-page .section-block {
  margin-top: 0;
}

@container lead-detail (width < 900px) {
  .desktop-detail-page .content-wrapper { display: flex; flex-direction: column; }
  .desktop-detail-page .detail-main-column { display: contents; }
  .desktop-detail-page .detail-sidebar { position: static; width: 100%; order: 1; }
  .desktop-detail-page .lead-overview-card { width: 100%; order: 0; }
  .desktop-detail-page .appointment-card { width: 100%; order: 2; }
  .desktop-detail-page .detail-records-section { width: 100%; order: 3; }
  .overview-info-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 767px) {
  .page-container { padding-bottom: calc(92px + env(safe-area-inset-bottom)); }
  .detail-layout-container { padding: var(--kx-space-4); }
  .assign-popup { width: 100%; }
  .content-wrapper { display: flex; flex-direction: column; gap: var(--kx-space-4); }
  .detail-main-column { display: contents; }
  .detail-section { border: 0; box-shadow: none; }
  .lead-overview-card, .appointment-card, .detail-records-section { margin: 0; border-radius: var(--kx-card-radius); }
  .lead-overview-card { order: 0; }
  .detail-sidebar { order: 1; margin: var(--kx-space-3) var(--kx-space-4) 0; }
  .appointment-card { order: 2; }
  .mobile-profile-section { order: 3; }
  .detail-records-section { order: 4; }
  .overview-heading { padding: 20px; border-bottom: 0; }
  .overview-person-row { gap: var(--kx-space-3); }
  .overview-avatar { width: 52px; height: 52px; font-size: var(--kx-font-24); }
  .overview-title-block { flex: 1; }
  .overview-heading-actions { align-items: flex-start; gap: var(--kx-space-2); }
  .edit-profile-button { width: 44px; height: 44px; padding: 0; border: 0; border-radius: 50%; background: var(--kx-color-mobile-fill); color: var(--kx-color-brand); }
  .overview-title-row h1 { width: 100%; font-size: var(--kx-mobile-title-size); letter-spacing: -.6px; }
  .overview-contact { margin-top: var(--kx-space-1); font-size: var(--kx-font-16); }
  .overview-status-row { margin: var(--kx-space-4) 0 var(--kx-space-2); }
  .overview-mobile-next { margin-top: var(--kx-space-4); padding: var(--kx-space-3); border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-surface-subtle); }
  .overview-mobile-next > div:first-child strong { font-size: var(--kx-mobile-heading-size); font-weight: 600; }
  .overview-mobile-next > div:nth-child(2) strong { color: var(--kx-color-text); }
  .mobile-workflow { margin: 0 20px; border-top: 1px solid var(--kx-color-mobile-separator); }
  .mobile-workflow > summary, .mobile-source-details > summary { display: flex; min-height: 52px; align-items: center; gap: var(--kx-space-2); color: var(--kx-color-text); font-size: var(--kx-font-14); list-style: none; cursor: pointer; }
  .mobile-workflow > summary::-webkit-details-marker, .mobile-source-details > summary::-webkit-details-marker { display: none; }
  .mobile-workflow > summary strong { margin-left: auto; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); font-weight: 400; }
  .mobile-workflow > summary .van-icon, .mobile-source-details > summary .van-icon { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
  .mobile-workflow[open] > summary .van-icon, .mobile-source-details[open] > summary .van-icon { transform: rotate(180deg); }
  .mobile-workflow > p { margin: 0 0 var(--kx-space-4); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
  .mobile-workflow .workflow-steps { flex-direction: column; align-items: stretch; padding-bottom: var(--kx-space-2); }
  .mobile-workflow .workflow-step { flex-direction: row; min-height: 40px; gap: var(--kx-space-3); padding-bottom: var(--kx-space-3); font-size: var(--kx-font-13); }
  .mobile-workflow .workflow-step i { top: 20px; bottom: 0; left: 9px; width: 1px; height: auto; }
  .mobile-workflow .workflow-step span { text-align: left; }
  .mobile-profile-section h2 { margin: 0; padding: 20px 20px var(--kx-space-2); font-size: var(--kx-mobile-heading-size); font-weight: 600; }
  .mobile-profile-fields { margin: 0 20px; }
  .mobile-profile-fields > div { display: grid; grid-template-columns: 72px minmax(0, 1fr); align-items: baseline; gap: var(--kx-space-3); min-height: 48px; padding: var(--kx-space-3) 0; }
  .mobile-profile-fields > div + div { border-top: 1px solid var(--kx-color-mobile-separator); }
  .mobile-profile-fields dt, .mobile-profile-owners dt { color: var(--kx-color-text-muted); font-size: var(--kx-font-13); }
  .mobile-profile-fields dd, .mobile-profile-owners dd { min-width: 0; margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-14); font-weight: 400; line-height: 1.5; overflow-wrap: anywhere; }
  .mobile-profile-fields dd { text-align: right; }
  .mobile-profile-owners { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--kx-space-4); margin: 0; padding: var(--kx-space-4) 20px; border-top: 1px solid var(--kx-color-mobile-separator); }
  .mobile-profile-owners dd { margin-top: var(--kx-space-1); }
  .mobile-source-details { margin: 0 20px; border-top: 1px solid var(--kx-color-mobile-separator); }
  .mobile-source-details > summary { justify-content: space-between; }
  .mobile-source-details .mobile-profile-fields { margin: 0 0 var(--kx-space-2); }
  .mobile-source-details .mobile-profile-fields > div { grid-template-columns: 64px minmax(0, 1fr); }
  .profile-editor { width: 100%; max-height: 88dvh; overflow-y: auto; }
  .profile-editor-header { padding: 18px 16px 14px; }
  .profile-editor-header button { width: 44px; height: 44px; }
  .profile-editor-actions { position: sticky; bottom: 0; background: var(--kx-color-surface-subtle); padding-bottom: max(18px, env(safe-area-inset-bottom)); }
  .profile-editor-actions :deep(.van-button),
  .primary-action :deep(.van-button),
  .secondary-actions :deep(.van-button),
  .risk-actions :deep(.van-button) { height: 44px; }
  .more-actions-toggle { min-height: 44px; }
  .section-card-header { padding: 0 var(--kx-space-4); }
  .section-card-header > div { flex-wrap: wrap; gap: var(--kx-space-1); padding: var(--kx-space-3) 0; }
  .section-card-header button { min-height: var(--kx-mobile-control-height); }

  .appointment-brief { padding: var(--kx-space-4); }
  .appointment-time-copy > strong { font-size: var(--kx-mobile-heading-size); }
  .appointment-calendar-date { display: block; margin-top: var(--kx-space-1); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); font-variant-numeric: tabular-nums; }
  .appointment-card > p { margin-right: var(--kx-space-4); margin-left: var(--kx-space-4); }
  .appointment-notes { padding: 0 var(--kx-space-4) var(--kx-space-4); }
  .detail-side-panel { margin: 0; padding: var(--kx-space-4); }
  .detail-sidebar .primary-action, .detail-actions-panel { display: none; }
  .detail-records-section :deep(.crm-tablist) { margin: 0 var(--kx-space-4); }
      .followup-cards, .activity-timeline { padding: var(--kx-space-4); }
  .followup-card-header { align-items: flex-start; }
  .system-info-grid { grid-template-columns: 1fr; padding: 8px 16px 18px; }
  .mobile-detail-actionbar { display: flex; }
  .mobile-detail-actionbar :deep(.van-button) { min-height: 44px; height: auto; min-width: 0; flex: 2; padding: var(--kx-space-2) var(--kx-space-3); border-radius: var(--kx-mobile-control-radius); }
  .mobile-detail-no-action { min-width: 0; flex: 1; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); text-align: center; }
  .mobile-detail-more { order: -1; display: inline-flex; min-height: 44px; min-width: 0; flex: 1; align-items: center; justify-content: center; gap: var(--kx-space-1); padding: var(--kx-space-2); border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); color: var(--kx-color-brand); font-size: var(--kx-font-14); }
}

@media (min-width: 981px) {
  :global(.assign-popup .sales-list) {
    max-height: 360px;
  }
}
</style>
