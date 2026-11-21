<template>
  <section class="report-panel report-followups" aria-label="当前跟进监督">
    <header class="report-panel-head"><div><span class="report-eyebrow">当前工作</span><h2>当前全部待办</h2></div></header>
    <p class="report-scope-emphasis">按当前负责人督办，不受上方录入日期、人员和样本筛选影响。</p>
    <CrmStatePanel v-if="loading || error" :state="loading ? 'loading' : 'error'" :title="loading ? '正在加载当前待办' : '当前待办加载失败'" description="当前筛选已保留，可独立刷新待办。" :action-label="loading ? '' : '重新加载待办'" @action="$emit('retry')" />
    <template v-else>
      <div class="report-supervision-summary"><div><strong :class="summary.overdue_followup ? 'report-risk-text' : ''">{{ summary.overdue_followup }}</strong><span>跟进逾期</span></div><div><strong>{{ summary.overdue_first_followup }}</strong><span>其中首跟超时</span></div><div><strong>{{ summary.pending_claim }}</strong><span>尚未领取</span></div></div>
      <p v-if="summary.pending_dispatch" class="report-supervision-note">另有 {{ summary.pending_dispatch }} 条线索待分配。</p>
      <p v-if="summary.missing_deadline || summary.unknown_first_followup" class="report-data-warning">{{ summary.missing_deadline || 0 }} 条未设置跟进截止；{{ summary.unknown_first_followup || 0 }} 条首跟责任记录不全。缺失记录不按零超时处理。</p>
      <div v-if="data.owners?.length" class="report-owner-review">
        <div class="report-owner-review-head">按当前负责人督办</div><div class="report-owner-columns" aria-hidden="true"><span>负责人</span><span>首跟超时</span><span>跟进逾期</span><span>待领</span></div>
        <div v-for="owner in data.owners" :key="owner.userid" class="report-owner-row">
          <strong>{{ owner.name }}</strong><span :aria-label="`首跟超时 ${owner.overdue_first_followup} 条`">{{ owner.overdue_first_followup }}</span><b :aria-label="`跟进逾期 ${owner.overdue_followup} 条`" :class="owner.overdue_followup ? 'report-risk-text' : ''">{{ owner.overdue_followup }}</b><span :aria-label="`待领取 ${owner.pending_claim} 条`">{{ owner.pending_claim }}</span>
          <RouterLink v-if="owner.overdue_followup && canFilter(owner.userid)" :to="{ path: '/leads', query: { sales_userid: owner.userid, status: 'followup_overdue' } }">看逾期线索 ↗</RouterLink>
          <small v-if="owner.missing_deadline || owner.unknown_first_followup" class="report-owner-record-note">截止缺失 {{ owner.missing_deadline }} · 首跟待核对 {{ owner.unknown_first_followup }}</small>
        </div>
      </div>
      <details v-if="data.tasks?.length" class="report-task-details"><summary>展开 {{ data.tasks.length }} 项优先待办与具体线索</summary><ul class="report-tasks"><li v-for="task in data.tasks" :key="`${task.type}:${task.lead_id}`"><span class="report-task-dot" :class="{ 'report-task-dot--overdue': task.due_at }" aria-hidden="true"></span><span><strong>{{ task.owner_name }} · {{ task.title }}</strong><small>{{ dueLabel(task.due_at) }}</small></span><RouterLink :to="`/leads/${task.lead_id}`">查看线索 <span aria-hidden="true">↗</span></RouterLink></li></ul></details>
      <p v-else class="report-empty">当前没有已识别的优先待办。</p>
    </template>
    <footer class="report-panel-foot"><span>首跟超时包含在跟进逾期内；按已记录的截止时间计算。优先展示最多 20 项。<span v-if="data.generated_at"> 更新于 {{ dueLabel(data.generated_at).replace('截止 ', '') }}。</span></span><RouterLink to="/leads">打开工作台 ↗</RouterLink></footer>
  </section>
</template>
<script setup>
import { computed } from 'vue'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
const props = defineProps({ data: { type: Object, default: () => ({}) }, loading: Boolean, error: Boolean, allowed: { type: Array, default: () => [] } })
defineEmits(['retry'])
const summary = computed(() => props.data.summary || {})
const canFilter = userid => props.allowed.some(user => user.wecom_userid === userid)
const dueLabel = value => value ? `截止 ${new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })}` : '进入线索核对当前安排'
</script>
