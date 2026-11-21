<template>
  <span class="status-summary">
    <van-tag :type="tagType" size="medium" class="status-badge">{{ statusText }}</van-tag>
    <small v-if="status === 'converted' && !paymentStatus" class="status-note">付款待确认</small>
  </span>
</template>

<script setup>
import { computed } from 'vue'
import { paymentStatusLabel } from '@/utils/leadPayment'

const props = defineProps({
  paymentStatus: { type: String, default: '' },
  status: {
    type: String,
    required: true
  }
})

const statusMap = {
  new: { text: '新线索', type: 'default' },
  pending_dispatch: { text: '待派发', type: 'default' },
  assigned_pending_claim: { text: '待领取', type: 'warning' },
  claimed: { text: '已认领', type: 'success' },
  following: { text: '跟进中', type: 'primary' },
  public_pool: { text: '公海', type: 'warning' },
  converted: { text: '已转化', type: 'success' },
  closed: { text: '已关闭', type: 'default' },
  invalid: { text: '无效', type: 'danger' }
}

const statusText = computed(() => props.status === 'converted' ? (props.paymentStatus ? paymentStatusLabel(props.paymentStatus) : '已成交') : (statusMap[props.status]?.text || props.status))
const tagType = computed(() => props.status === 'converted' && props.paymentStatus === 'refunded' ? 'warning' : (statusMap[props.status]?.type || 'default'))
</script>

<style scoped>
.status-summary { display: inline-flex; max-width: 100%; min-width: 0; flex-direction: column; align-items: flex-start; gap: var(--kx-space-1); vertical-align: middle; }
.status-badge { max-width: 100%; padding: 3px 8px; border-radius: var(--kx-tag-radius); line-height: 1.5; font-weight: 500; white-space: normal; overflow-wrap: anywhere; }
.status-note { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); line-height: 1.5; overflow-wrap: anywhere; }
.status-badge.van-tag--success { background: var(--kx-color-success-soft); color: var(--kx-color-success); }
.status-badge.van-tag--warning { background: var(--kx-color-warning-soft); color: var(--kx-color-warning); }
.status-badge.van-tag--danger { background: var(--kx-color-danger-soft); color: var(--kx-color-danger); }
.status-badge.van-tag--primary { background: var(--kx-color-brand-soft); color: var(--kx-color-brand); }
.status-badge.van-tag--default { background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); }
</style>
