<template>
  <div class="agent-settings-backdrop" @click.self="close">
    <section ref="panel" class="agent-settings" role="dialog" aria-modal="true" aria-labelledby="agent-settings-title">
      <header><h2 id="agent-settings-title">分析设置</h2><button type="button" aria-label="关闭分析设置" @click="close">关闭</button></header>
      <nav aria-label="分析设置分类"><button type="button" :aria-current="tab === 'preferences' ? 'page' : undefined" @click="tab = 'preferences'">个人偏好</button><button type="button" :aria-current="tab === 'rules' ? 'page' : undefined" @click="tab = 'rules'">公共业务口径</button><button v-if="isAdmin" type="button" :aria-current="tab === 'model' ? 'page' : undefined" @click="tab = 'model'">模型与预算</button></nav>
      <div class="agent-settings-body">
        <p v-if="loading" role="status">正在读取分析设置…</p>
        <p v-if="error" class="settings-error" role="alert">{{ error }}</p>
        <form v-if="!loading && tab === 'preferences'" @submit.prevent="savePreferences">
          <p>保存后用于你后续的分析，例如常用校区、回答长度或图表偏好。</p>
          <label>我的分析偏好<textarea v-model="preferences" rows="7" maxlength="4000" placeholder="例如：先给结论，再展示数据；金额使用人民币元。"></textarea></label>
          <div class="settings-actions"><button type="submit" :disabled="busy">保存个人偏好</button><button type="button" :disabled="busy || !preferences" @click="clearPreferences">删除个人偏好</button></div>
        </form>
        <div v-if="!loading && tab === 'rules'">
          <p>公共口径由管理员审核发布。个人偏好不会覆盖权限和已发布的业务规则。</p>
          <form @submit.prevent="propose"><label>提交业务口径建议<textarea v-model="proposal" rows="4" maxlength="8000" placeholder="写清需要统一的定义或修正，并提供理由。"></textarea></label><button type="submit" :disabled="busy || !proposal.trim()">提交审核</button></form>
          <ul class="agent-rules"><li v-for="rule in rules" :key="rule.id"><div><strong>版本 {{ rule.version || rule.id }}</strong><span>{{ ruleStatus(rule.status) }}</span></div><p>{{ rule.content }}</p><div v-if="isAdmin" class="settings-actions"><button v-if="['pending', 'draft', 'proposed'].includes(rule.status)" type="button" :disabled="busy" @click="publish(rule)">审核并发布</button><button v-else-if="rule.status !== 'active' && rule.status !== 'published'" type="button" :disabled="busy" @click="rollback(rule)">恢复为当前版本</button></div></li></ul><p v-if="!rules.length">尚无公共规则。</p>
        </div>
        <AgentModelSettings v-if="tab === 'model' && isAdmin" @dirty="modelDirty = $event" @saved="$emit('changed')" />
        <p v-if="notice" role="status">{{ notice }}</p>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, ref } from 'vue'
import { showConfirmDialog } from 'vant'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { useUserStore } from '@/stores/user'
import { agentAPI } from '@/api/agent'
import AgentModelSettings from './AgentModelSettings.vue'
const emit = defineEmits(['close', 'changed'])
const userStore = useUserStore(), isAdmin = computed(() => userStore.isAdmin())
const panel = ref(null), visible = ref(false), tab = ref('preferences'), loading = ref(true), busy = ref(false), error = ref(''), notice = ref(''), preferences = ref(''), savedPreferences = ref(''), proposal = ref(''), rules = ref([]), modelDirty = ref(false)
useOverlayFocus(visible, panel, () => close())
const load = async () => {
  loading.value = true; error.value = ''
  const responses = await Promise.allSettled([agentAPI.preferences(), agentAPI.rules()])
  if (responses[0].status === 'fulfilled') preferences.value = savedPreferences.value = responses[0].value.content || ''
  else error.value = '个人偏好读取失败，请关闭设置后重试'
  if (responses[1].status === 'fulfilled') rules.value = responses[1].value.rules || []
  else error.value = '公共口径读取失败，请关闭设置后重试'
  loading.value = false
}
const perform = async (action, message) => {
  busy.value = true; error.value = ''; notice.value = ''
  try { await action(); notice.value = message; emit('changed') }
  catch (failure) { error.value = failure.response?.data?.message || '操作失败，当前输入已保留' }
  finally { busy.value = false }
}
const savePreferences = () => perform(async () => { await agentAPI.savePreferences(preferences.value); savedPreferences.value = preferences.value }, '个人偏好已保存')
const clearPreferences = () => perform(async () => { await agentAPI.deletePreferences(); preferences.value = savedPreferences.value = '' }, '个人偏好已删除')
const propose = () => perform(async () => { await agentAPI.proposeRule(proposal.value); proposal.value = ''; rules.value = (await agentAPI.rules()).rules || [] }, '建议已提交，等待管理员审核')
const publish = rule => perform(async () => { await showConfirmDialog({ title: '发布公共业务口径', message: '发布后供全体人员的新分析使用，请确认口径准确。' }); await agentAPI.publishRule(rule.id); rules.value = (await agentAPI.rules()).rules || [] }, '公共口径已发布')
const rollback = rule => perform(async () => { await showConfirmDialog({ title: '恢复历史口径', message: '确认将此版本恢复为当前公共口径？' }); await agentAPI.rollbackRule(rule.id); rules.value = (await agentAPI.rules()).rules || [] }, '已恢复公共口径')
const ruleStatus = status => ({ pending: '待审核', draft: '待审核', proposed: '待审核', active: '当前生效', published: '当前生效', archived: '历史版本', superseded: '历史版本' }[status] || status)
const close = async () => {
  if (preferences.value !== savedPreferences.value || proposal.value.trim() || modelDirty.value) {
    try { await showConfirmDialog({ title: '关闭分析设置？', message: '当前修改尚未保存，关闭后会丢失。', confirmButtonText: '放弃修改', cancelButtonText: '继续填写' }) } catch { return }
  }
  visible.value = false; await nextTick(); emit('close')
}
onMounted(() => { visible.value = true; load() })
</script>

<style scoped>
.agent-settings-backdrop { position: fixed; z-index: 100; inset: 0; display: flex; justify-content: flex-end; background: var(--kx-overlay-strong); }
.agent-settings { display: flex; flex-direction: column; width: min(640px, 100%); max-height: 100dvh; background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-settings header { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 16px 20px; border-bottom: 1px solid var(--kx-color-border); }
.agent-settings h2 { margin: 0; font-size: 18px; }
.agent-settings nav { display: flex; gap: 4px; padding: 8px 12px; border-bottom: 1px solid var(--kx-color-border); }
.agent-settings button { min-height: 44px; padding: 8px 12px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); font-size: 13px; cursor: pointer; }
.agent-settings button:disabled { opacity: .5; }
.agent-settings [aria-current=page] { background: var(--kx-color-brand-soft); color: var(--kx-color-brand); border-color: var(--kx-color-brand); }
.agent-settings-body { min-height: 0; overflow: auto; padding: 20px; }
.agent-settings-body p { font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.agent-settings-body label { display: grid; gap: 8px; font-size: 14px; font-weight: 600; }
.agent-settings textarea { width: 100%; box-sizing: border-box; padding: 12px; border: 1px solid var(--kx-color-border-strong); border-radius: 8px; resize: vertical; background: var(--kx-color-surface); color: var(--kx-color-text); font: inherit; font-weight: 400; }
.settings-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.settings-error { color: var(--kx-color-danger); }
.agent-rules { display: grid; gap: 12px; padding: 0; list-style: none; }
.agent-rules li { padding: 16px; border: 1px solid var(--kx-color-border); border-radius: 10px; }
.agent-rules li > div:first-child { display: flex; justify-content: space-between; gap: 12px; font-size: 13px; }
.agent-rules li > div:first-child span { color: var(--kx-color-text-secondary); }
@media (max-width: 375px) { .agent-settings-body { padding: 12px; } .agent-settings nav { gap: 2px; padding: 8px; } .agent-settings nav button { min-width: 0; flex: 1; padding: 8px 4px; font-size: 12px; } }
</style>
