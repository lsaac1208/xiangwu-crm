<template>
  <section class="robot-delivery-health" aria-label="群机器人投递状态" :class="{ unhealthy: health && !health.healthy }">
    <div class="robot-delivery-heading"><strong>{{ title }}</strong><button type="button" :disabled="loading" @click="load">{{ loading ? '检查中…' : '重新检查' }}</button></div>
    <p v-if="error">暂时无法获取投递状态，不能据此判断通知正常。请稍后重试。</p>
    <template v-else-if="health">
      <p>近 24 小时：投递失败 {{ health.failed_count }} 条 · 等待重试 {{ health.retrying_count }} 条 · 已停止重试 {{ health.exhausted_count }} 条。</p>
      <p v-if="health.pending_overdue_count">已到发送时间、积压超过 10 分钟：{{ health.pending_overdue_count }} 条，请检查后台任务。</p>
      <p>最近一次成功投递：{{ health.last_success_at ? formatDateTime(health.last_success_at) : '暂无成功记录' }}</p>
      <ul v-if="health.recent_failures?.length">
        <li v-for="item in health.recent_failures" :key="item.id"><router-link v-if="item.lead_id" :to="`/leads/${item.lead_id}`">线索 {{ item.lead_id }}</router-link><span>{{ robotEventLabel(item.event_type) }} · {{ item.reason }} · {{ item.will_retry ? '将自动重试' : '已停止重试' }}</span></li>
      </ul>
    </template>
  </section>
</template>
<script setup>
import { computed, onMounted, ref } from 'vue'
import { getGroupRobotHealth } from '@/api/admin'
import { formatDateTime } from '@/utils/date'
import { robotEventLabel } from '@/utils/robotEvents'
const health = ref(null)
const loading = ref(false)
const error = ref(false)
const title = computed(() => loading.value ? '正在检查通知投递' : error.value ? '投递状态未知' : health.value?.healthy ? '近 24 小时未发现投递异常' : '群机器人投递需要关注')
const load = async () => {
  loading.value = true
  error.value = false
  try {
    const data = await getGroupRobotHealth()
    if (!data?.available) throw new Error('unavailable')
    health.value = data
  } catch {
    error.value = true
    health.value = null
  } finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.robot-delivery-health { margin: var(--kx-space-4) 0; padding: var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); line-height: 1.6; }
.robot-delivery-health.unhealthy { border-color: var(--kx-color-danger); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); }
.robot-delivery-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--kx-space-2); }
.robot-delivery-heading button { min-height: var(--kx-mobile-control-height); padding: 0 var(--kx-space-3); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-2); background: var(--kx-color-surface); color: var(--kx-color-text); font: inherit; cursor: pointer; }
.robot-delivery-health p { margin: var(--kx-space-2) 0; }
.robot-delivery-health ul { display: grid; gap: var(--kx-space-2); margin: var(--kx-space-3) 0 0; padding: 0; list-style: none; }
.robot-delivery-health li { display: flex; flex-wrap: wrap; gap: var(--kx-space-2); overflow-wrap: anywhere; }
.robot-delivery-health a { color: inherit; text-decoration: underline; }
</style>
