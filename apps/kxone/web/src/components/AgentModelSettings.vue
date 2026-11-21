<template>
  <section class="ai-model-panel">
    <h2>AI 模型配置</h2><p class="model-hint">从服务商控制台复制本业务空间的 API 地址，填写模型名称，可在保存前测试连接。</p>
    <p v-if="loading" role="status">正在读取 AI 配置…</p>
    <div v-else-if="loadError" role="alert"><p>AI 配置读取失败</p><button type="button" @click="load">重试</button></div>
    <form v-else @submit.prevent="save">
      <label>API 服务地址<input v-model.trim="form.base_url" type="url" name="ai-base-url" autocomplete="off" required placeholder="https://llm-xxx.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"></label>
      <label>模型名称<input v-model.trim="form.model" name="ai-model" autocomplete="off" required placeholder="qwen3.8-max"></label>
      <label>API Key<input v-model="form.api_key" type="password" name="ai-api-key" autocomplete="new-password" :placeholder="keyConfigured ? '已配置，留空保留现有 Key' : '请输入服务商 API Key'" :disabled="form.clear_api_key"></label>
      <label v-if="keyConfigured" class="model-check"><input v-model="form.clear_api_key" type="checkbox">清除已保存的 API Key</label>
      <label>每月预算（元）<input v-model="form.budget" type="number" min="1" max="10000" step="0.01" required></label>
      <label>输入单价（元 / 百万 Token）<input v-model="form.input_price_cny" type="number" min="0.001" max="10000" step="0.001" required></label>
      <label>输出单价（元 / 百万 Token）<input v-model="form.output_price_cny" type="number" min="0.001" max="10000" step="0.001" required></label>
      <p class="model-hint">更换模型时请按服务商的标准单价更新报价，用于本系统的预算预留和费用核算。</p>
      <details class="model-backup" :open="form.fallback.enabled">
        <summary>备用模型与自动切换</summary>
        <p class="model-hint">主模型过载、限流或服务不可用时自动切换一次。两者共用每月预算、单轮费用、次数及时间上限；未知用量继续保留预留。</p>
        <label class="model-check"><input v-model="form.fallback.enabled" type="checkbox">启用备用模型自动切换</label>
        <label>备用 API 服务地址<input v-model.trim="form.fallback.base_url" type="url" name="ai-fallback-url" autocomplete="off" :required="form.fallback.enabled" placeholder="https://api.deepseek.com"></label>
        <label>备用模型名称<input v-model.trim="form.fallback.model" name="ai-fallback-model" autocomplete="off" :required="form.fallback.enabled" placeholder="deepseek-flash"></label>
        <label>备用 API Key<input v-model="form.fallback.api_key" type="password" name="ai-fallback-key" autocomplete="new-password" :placeholder="fallbackKeyConfigured ? '已配置，留空保留现有 Key' : '请输入备用 API Key'" :disabled="form.fallback.clear_api_key"></label>
        <label v-if="fallbackKeyConfigured" class="model-check"><input v-model="form.fallback.clear_api_key" type="checkbox">清除备用 API Key</label>
        <label>备用输入单价（元 / 百万 Token）<input v-model="form.fallback.input_price_cny" type="number" min="0.001" max="10000" step="0.001" :required="form.fallback.enabled"></label>
        <label>备用输出单价（元 / 百万 Token）<input v-model="form.fallback.output_price_cny" type="number" min="0.001" max="10000" step="0.001" :required="form.fallback.enabled"></label>
        <button type="button" :disabled="testing || saving || form.fallback.clear_api_key || !form.fallback.base_url || !form.fallback.model" @click="testConnection('fallback')">测试备用连接</button>
      </details>
      <label class="model-check"><input v-model="form.enabled" type="checkbox">启用新分析</label>
      <p class="model-hint">{{ keyConfigured ? 'API Key 已配置' : 'API Key 尚未配置' }} · 本月按配置单价估算 ¥{{ currency(settings.spent_micro) }} · 预留 ¥{{ currency(settings.reserved_micro) }}。用量未知的调用继续占用预留；最终费用需与服务商账单核对。</p>
      <details v-if="settings.usage" class="model-usage"><summary>本月用量明细</summary><p>已结算调用 {{ settings.usage.settled_calls || 0 }} 次 · 待结算 {{ settings.usage.pending_calls || 0 }} 次<br>输入 {{ settings.usage.input_tokens || 0 }} Token · 输出 {{ settings.usage.output_tokens || 0 }} Token</p><p>报价版本：{{ settings.price_version || '未提供' }}</p></details>
      <section v-if="settings.run_health" class="model-health" aria-label="分析运行情况">
        <h3>分析运行情况 · {{ settings.run_health.month }}</h3>
        <p class="model-hint">{{ settings.run_health.scope }}</p>
        <p>失败率：{{ percentage(settings.run_health.failure_rate) }} <span class="model-hint">（统计 {{ settings.run_health.failure_sample_count }} 项）</span><br>成功分析平均耗时：{{ duration(settings.run_health.average_duration_ms) }} <span class="model-hint">（可信时间 {{ settings.run_health.duration_sample_count }} 项；缺少可信耗时 {{ settings.run_health.unknown_duration_count }} 项）</span></p>
        <p class="model-hint">共 {{ settings.run_health.total }} 项 · 已完成 {{ settings.run_health.completed }} · 失败 {{ settings.run_health.failed }} · 中断 {{ settings.run_health.interrupted }}<br>已取消 {{ settings.run_health.cancelled }} · 额度不足 {{ settings.run_health.budget_exceeded }} · 排队中 {{ settings.run_health.queued }} · 运行中 {{ settings.run_health.running }}</p>
        <p class="model-hint">失败率按失败与中断占已完成、失败和中断任务的比例计算；取消、额度不足及未结束任务不计入。平均耗时仅统计成功且起止时间可信的分析任务，包含数据查询与回答生成。</p>
      </section>
      <p v-if="settings.warning" class="model-warning" role="status">本月已用与预留额度达到 80%，请关注剩余额度。</p>
      <p v-if="error" class="model-error" role="alert">{{ error }}</p>
      <p v-if="testMessage" role="status">{{ testMessage }}</p>
      <div class="model-actions"><button type="button" :disabled="testing || saving || form.clear_api_key || !form.base_url || !form.model" @click="testConnection('primary')">{{ testing ? '正在测试…' : '测试连接' }}</button><button class="model-primary" type="submit" :disabled="saving || testing">{{ saving ? '正在保存…' : '保存 AI 配置' }}</button></div>
      <button v-if="conflicted" type="button" class="model-reload" @click="inspectLatest">读取最新配置，保留当前输入</button>
      <p v-if="latestMessage" role="status">{{ latestMessage }}</p>
    </form>
  </section>
</template>

<script setup>
import { onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { agentAPI } from '@/api/agent'
const emit = defineEmits(['saved', 'dirty'])
const loading = ref(true), loadError = ref(false), saving = ref(false), testing = ref(false), error = ref(''), testMessage = ref(''), latestMessage = ref(''), conflicted = ref(false), keyConfigured = ref(false), fallbackKeyConfigured = ref(false), settings = ref({})
const form = reactive({ base_url: '', model: '', api_key: '', clear_api_key: false, enabled: false, budget: 300, input_price_cny: 12, output_price_cny: 36, version: 0, fallback: { enabled: false, base_url: '', model: 'deepseek-flash', api_key: '', clear_api_key: false, input_price_cny: 2, output_price_cny: 8 } })
let savedForm = '', active = true
const currency = value => ((Number(value) || 0) / 1e6).toFixed(2)
const percentage = value => value == null || !Number.isFinite(Number(value)) ? '—' : `${(Number(value) * 100).toFixed(1)}%`
const duration = value => value == null || !Number.isFinite(Number(value)) ? '—' : `${(Number(value) / 1000).toFixed(2)} 秒`
const formSignature = () => JSON.stringify(form)
watch(form, () => emit('dirty', savedForm !== formSignature()))
watch(() => form.fallback.clear_api_key, clear => { if (clear) { form.fallback.enabled = false; form.fallback.api_key = '' } })
const load = async () => {
  loading.value = true; loadError.value = false
  try {
    const data = await agentAPI.settings()
    if (!active) return
    settings.value = data; fallbackKeyConfigured.value = data.fallback?.key_configured === true; keyConfigured.value = data.key_configured || data.configured || false
    Object.assign(form, { base_url: data.base_url || '', model: data.model || 'qwen3.8-max', api_key: '', clear_api_key: false, enabled: data.enabled === true, budget: (data.budget_micro ?? 300e6) / 1e6, input_price_cny: data.input_price_cny ?? 12, output_price_cny: data.output_price_cny ?? 36, version: data.version || 0 })
    Object.assign(form.fallback, { enabled: data.fallback?.enabled === true, base_url: data.fallback?.base_url || '', model: data.fallback?.model || 'deepseek-flash', api_key: '', clear_api_key: false, input_price_cny: data.fallback?.input_price_cny || 2, output_price_cny: data.fallback?.output_price_cny || 8 })
    savedForm = formSignature(); emit('dirty', false)
  } catch { loadError.value = true } finally { loading.value = false }
}
const fallbackPayload = () => ({ ...form.fallback, input_price_cny: Number(form.fallback.input_price_cny), output_price_cny: Number(form.fallback.output_price_cny) })
const payload = () => ({ fallback: fallbackPayload(), base_url: form.base_url, model: form.model, api_key: form.api_key, clear_api_key: form.clear_api_key, enabled: form.enabled, budget_micro: Math.round(Number(form.budget) * 1e6), input_price_cny: Number(form.input_price_cny), output_price_cny: Number(form.output_price_cny), expected_version: form.version })
const save = async () => {
  if (saving.value) return
  saving.value = true; error.value = ''; conflicted.value = false
  try {
    const result = await agentAPI.saveSettings(payload())
    if (!active) return
    form.api_key = ''; form.fallback.api_key = ''; savedForm = formSignature(); emit('dirty', false)
    emit('saved', result); await load(); testMessage.value = 'AI 配置已保存'
  } catch (failure) {
    conflicted.value = failure.response?.status === 409
    error.value = conflicted.value ? '配置已被其他管理员修改，当前输入已保留。请读取最新配置并核对后再保存。' : failure.response?.data?.message || '保存失败，输入已保留，请重试'
  } finally { saving.value = false }
}
const inspectLatest = async () => {
  try {
    const data = await agentAPI.settings()
    form.version = data.version
    const backup = data.fallback?.model ? `备用：${data.fallback.base_url || '未填写地址'} · ${data.fallback.model} · ${data.fallback.enabled ? '自动切换已启用' : '自动切换已关闭'} · ${data.fallback.key_configured ? 'Key 已配置' : 'Key 未配置'}。` : ''
    latestMessage.value = `最新配置：${data.base_url} · ${data.model} · ${data.enabled ? '已启用' : '已关闭'} · 预算 ¥${currency(data.budget_micro)}。${backup}当前输入仍保留，请核对后保存。`
  } catch { error.value = '最新配置读取失败，请重试' }
}
const testConnection = async (target = 'primary') => {
  testing.value = true; error.value = ''; testMessage.value = ''
  try {
    const data = await agentAPI.testSettings(target === 'fallback' ? { target, fallback: fallbackPayload() } : { base_url: form.base_url, api_key: form.api_key, model: form.model, input_price_cny: Number(form.input_price_cny), output_price_cny: Number(form.output_price_cny) })
    testMessage.value = data.ok ? `连接成功 · ${data.model || form.model} · ${data.latency_ms ?? 0} 毫秒` : data.message || '连接测试未通过'
  } catch (failure) { error.value = failure.response?.data?.message || '连接测试失败，请核对地址、模型和 API Key' }
  finally { testing.value = false }
}
onMounted(load)
onBeforeUnmount(() => { active = false; form.api_key = ''; form.fallback.api_key = '' })
</script>

<style scoped>
.ai-model-panel { min-width: 0; padding: 20px; border: 1px solid var(--kx-color-border); border-radius: 12px; background: var(--kx-color-surface); }
.ai-model-panel h2 { margin: 0 0 8px; font-size: 18px; }
.model-hint { color: var(--kx-color-text-secondary); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.ai-model-panel form { display: grid; gap: 16px; }
.ai-model-panel label { display: grid; min-width: 0; gap: 7px; font-size: 14px; font-weight: 600; }
.ai-model-panel input:not([type=checkbox]) { width: 100%; min-width: 0; min-height: 44px; box-sizing: border-box; padding: 10px 12px; border: 1px solid var(--kx-color-border-strong); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); font-size: 14px; }
.ai-model-panel .model-check { display: flex; align-items: center; gap: 8px; }
.model-check input { width: 18px; height: 18px; accent-color: var(--kx-color-brand); }
.model-backup { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--kx-color-border); border-radius: 8px; }
.model-backup summary { cursor: pointer; font-weight: 600; }
.model-backup[open] > :not(summary) { margin-top: 12px; }
.model-actions { display: flex; flex-wrap: wrap; gap: 10px; }
.ai-model-panel button { min-height: 44px; padding: 10px 16px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); cursor: pointer; }
.ai-model-panel button:disabled { opacity: .6; cursor: default; }
.ai-model-panel .model-primary { border-color: var(--kx-color-brand); background: var(--kx-color-brand); color: var(--kx-color-on-brand); }
.model-error { color: var(--kx-color-danger); font-size: 13px; }
.model-warning { color: var(--kx-color-text); background: var(--kx-amber-50); padding: 12px; border-radius: 8px; }
.model-reload { max-width: 100%; }
.model-health { min-width: 0; padding-top: 4px; border-top: 1px solid var(--kx-color-border); }
.model-health h3 { margin: 12px 0 8px; font-size: 15px; }
.model-health p { margin: 8px 0; line-height: 1.7; overflow-wrap: anywhere; }
</style>
