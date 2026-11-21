<template>
  <button type="button" class="lead-mobile-row" :class="{ overdue: followup.overdue }" @click="$emit('click', lead)">
    <span class="lead-mobile-row-title">
      <strong>{{ displayLeadTitle(lead) }}</strong>
      <StatusBadge :status="lead.status" :payment-status="lead.payment_status" />
      <span class="lead-mobile-stage">{{ stageLabel }}</span>
    </span>
    <span class="lead-mobile-row-subtitle">{{ lead.product_name || displayLeadDesc(lead) || '咨询项目未填写' }}</span>
    <span v-if="nextStep" class="lead-mobile-next-step">{{ nextStep }}</span>
    <span class="lead-mobile-contact">
      <span>{{ mobileText }}</span><span>{{ sourceLabel }}</span><span>{{ ownerLabel }}</span>
    </span>
    <span v-if="dispatcherName" class="lead-mobile-row-meta">分配：{{ dispatcherName }}</span>
    <span class="lead-mobile-row-footer">
      <span class="lead-mobile-due" :class="followup.tone" :title="followup.exact || ''"><van-icon aria-hidden="true" name="clock-o" /><strong>{{ followup.label }}</strong></span>
      <small>{{ formatDate(lead.created_at) }}</small>
    </span>
  </button>
</template>

<script setup>
import { computed } from 'vue'
import { describeFollowupSchedule, formatDate } from '@/utils/date'
import { displayLeadDesc, displayLeadDispatcher, displayLeadOwner, displayLeadTitle, displaySourceChannel, leadStageLabel, isLeadFinished } from '@/utils/leadDisplay'
import StatusBadge from './StatusBadge.vue'
import { displayLeadMobile } from '@/utils/leadContact'
import { leadNextStep } from '@/utils/leadTriage'

const props = defineProps({
  lead: {
    type: Object,
    required: true
  },
  showDispatcher: {
    type: Boolean,
    default: false
  },
  actorUserId: { type: String, default: '' }
})

defineEmits(['click'])

const ownerName = computed(() => displayLeadOwner(props.lead))
const dispatcherName = computed(() => props.showDispatcher ? displayLeadDispatcher(props.lead) : '')
const stageLabel = computed(() => leadStageLabel(props.lead))
const ownerLabel = computed(() => ownerName.value ? `负责：${ownerName.value}` : '负责人待确认')
const mobileText = computed(() => displayLeadMobile(props.lead, true))
const sourceLabel = computed(() => displaySourceChannel(props.lead.source_channel))
const followup = computed(() => isLeadFinished(props.lead) ? { label: '已结束', tone: 'normal', overdue: false } : describeFollowupSchedule(props.lead.next_followup_due_at))
const nextStep = computed(() => leadNextStep(props.lead, props.actorUserId))
</script>

<style scoped>
.lead-mobile-next-step { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); line-height: 1.5; }
.lead-mobile-row { position: relative; display: flex; flex-direction: column; width: 100%; min-width: 0; min-height: 112px; gap: 6px; padding: var(--kx-space-4); border: 0; border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); color: var(--kx-color-text); text-align: left; font: inherit; cursor: pointer; transition: background-color var(--kx-motion-fast); }
.lead-mobile-row:hover, .lead-mobile-row:active { background: var(--kx-color-surface-subtle); }
.lead-mobile-row-title { display: flex; flex-wrap: wrap; min-width: 0; align-items: center; gap: var(--kx-space-2); }
.lead-mobile-row-title strong { max-width: 100%; color: var(--kx-color-text); font-size: var(--kx-mobile-heading-size); font-weight: 600; line-height: 1.4; overflow-wrap: anywhere; }
.lead-mobile-stage { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); font-weight: 400; }
.lead-mobile-row-subtitle { color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.5; overflow-wrap: anywhere; }
.lead-mobile-contact, .lead-mobile-row-meta { display: flex; flex-wrap: wrap; gap: var(--kx-space-1) var(--kx-space-2); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.5; overflow-wrap: anywhere; }
.lead-mobile-contact > span + span::before { content: '·'; margin-right: var(--kx-space-2); }
.lead-mobile-contact > span:first-child { font-variant-numeric: tabular-nums; }
.lead-mobile-row-footer { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--kx-space-2); margin-top: var(--kx-space-1); color: var(--kx-color-text-muted); }
.lead-mobile-due { display: flex; min-width: 0; align-items: center; gap: var(--kx-space-1); font-size: var(--kx-font-12); overflow-wrap: anywhere; }
.lead-mobile-due strong { font-weight: 500; }
.lead-mobile-due.warning { color: var(--kx-color-warning); }
.lead-mobile-due.danger { color: var(--kx-color-danger); }
.lead-mobile-row-footer small { font-size: var(--kx-font-12); font-variant-numeric: tabular-nums; }
</style>
