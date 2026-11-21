<template>
  <div class="agent-workspace" :style="viewportHeight ? { height: `${viewportHeight}px` } : undefined">
    <a class="skip-link" href="#agent-main">跳到对话内容</a>
    <div v-if="historyOpen" class="agent-history-overlay" @click="historyOpen = false"></div>
    <aside ref="historyPanel" class="agent-history" :class="{ 'agent-history--open': historyOpen }" aria-label="分析会话历史" :role="historyOpen ? 'dialog' : undefined" :aria-modal="historyOpen ? 'true' : undefined">
      <header><router-link to="/leads" class="agent-back">← 返回 CRM</router-link><button type="button" class="history-close" aria-label="关闭会话历史" @click="historyOpen = false">关闭</button></header>
      <button class="agent-new" type="button" @click="newConversation">＋ 新建对话</button>
      <label class="agent-search"><span class="sr-only">搜索历史会话</span><input v-model="search" type="search" placeholder="搜索历史会话" autocomplete="off"></label>
      <p v-if="historyLoading" class="history-state" role="status">正在读取历史…</p>
      <p v-if="historyError" class="history-state" role="alert">历史读取失败 <button type="button" @click="loadHistory()">重试</button></p>
      <nav class="agent-conversations" aria-label="会话列表">
        <div v-for="conversation in conversations" :key="conversation.id" class="agent-conversation" :class="{ active: String(conversation.id) === conversationId }">
          <router-link :to="`/agent/${conversation.id}`" @click="historyOpen = false"><span>{{ conversation.title || '新对话' }}</span></router-link>
          <button type="button" :aria-label="`管理会话 ${conversation.title || '新对话'}`" @click="editConversation(conversation)">···</button>
        </div>
        <p v-if="!historyLoading && !conversations.length" class="history-state">{{ search ? '没有匹配的会话' : '你的对话会保存在这里' }}</p>
        <button v-if="conversations.length < historyTotal" type="button" class="history-more" :disabled="historyLoading" @click="loadHistory(true)">加载更多会话</button>
      </nav>
      <footer><span>{{ userStore.displayName() }} · {{ userStore.roleName() }}</span><button type="button" @click="settingsOpen = true; historyOpen = false">分析设置</button></footer>
    </aside>

    <main id="agent-main" class="agent-main" tabindex="-1">
      <header class="agent-header"><button type="button" class="history-toggle" aria-label="打开会话历史" @click="historyOpen = true">☰</button><div><h1>AI 分析</h1><p>{{ currentConversation?.title || '与你的 CRM 数据对话' }}</p></div><button class="agent-settings-toggle" type="button" @click="settingsOpen = true">设置</button></header>
      <div v-if="settingsError" class="agent-notice" role="alert">分析状态读取失败 <button type="button" @click="loadSettings">重试</button></div>
      <div v-else-if="settings && (!settings.enabled || !settings.configured)" class="agent-notice" role="status">{{ !settings.configured ? '模型尚未配置' : '新分析暂时关闭' }}。历史对话和已有结果仍可查看{{ quickMetrics.length ? '，也可用下方常用指标直接查看数据' : '' }}。<button v-if="userStore.isAdmin()" type="button" @click="settingsOpen = true">配置模型</button></div>
      <div v-else-if="budgetReached" class="agent-notice" role="status">本月分析额度已用完，历史结果仍可查看和导出。</div>

      <div ref="messageList" class="agent-messages" tabindex="0" role="region" aria-label="分析对话" @scroll="saveScroll">
        <div class="agent-message-inner">
          <div v-if="conversationLoading" class="agent-state" role="status">正在读取对话…</div>
          <div v-else-if="conversationError" class="agent-state" role="alert"><p>{{ conversationError }}</p><button type="button" @click="loadConversation(conversationId)">重试读取对话</button></div>
          <section v-else-if="!messages.length" class="agent-welcome"><span class="agent-mark" aria-hidden="true">✦</span><h2>从一个问题开始</h2><p>用自然语言提问，AI 选择系统预定义的统计指标并写简要说明；<br>表格、图表和所有数字都由系统计算，可随时核对与导出。</p></section>
          <template v-else>
            <button v-if="hasOlderMessages" class="agent-older" type="button" :disabled="olderLoading" @click="loadOlderMessages">{{ olderLoading ? '正在读取…' : '加载更早消息' }}</button>
            <article v-for="message in visibleMessages" :key="message.id" class="agent-message" :class="`agent-message--${message.role}`" :aria-label="message.role === 'user' ? '你的问题' : 'AI 回答'">
              <div class="agent-message-label">{{ message.role === 'user' ? '你' : 'AI 分析' }}</div>
              <div v-if="message.role === 'user'" class="agent-user-content">{{ message.content }}</div>
              <div v-else class="agent-answer">
                <AgentMarkdown v-if="message.content" :text="message.content" />
                <template v-for="(block, index) in message.blocks || []" :key="index">
                  <AgentMarkdown v-if="block.type === 'markdown' && block.text !== message.content" :text="block.text" />
                  <AgentResult v-else-if="block.type === 'table' || block.type === 'chart'" :block="block" :results="results" @result="rememberResult" />
                  <details v-else-if="block.type === 'sources' && (block.sources || []).length" class="agent-evidence"><summary>{{ block.title || '依据记录' }}</summary><router-link v-for="(source, sourceIndex) in block.sources || []" :key="sourceIndex" :to="source.lead_id ? { path: `/leads/${source.lead_id}`, query: { return_to: route.fullPath } } : '#'" @click="!source.lead_id && $event.preventDefault()">{{ source.label || `记录 ${source.id}` }}</router-link></details>
                </template>
                <AgentRunEvidence v-if="message.run_id && message.status !== 'running'" :run-id="message.run_id" :run="runSummaries[message.run_id]" @result="rememberResult" />
                <div v-if="message.status !== 'running'" class="agent-answer-actions"><button type="button" @click="copyAnswer(message)">复制回答</button><button v-if="message.run_id" type="button" :disabled="exporting === message.run_id" @click="exportAnswer(message.run_id, 'xlsx')">导出 Excel</button><button v-if="message.run_id" type="button" :disabled="exporting === message.run_id" @click="exportAnswer(message.run_id, 'html')">导出报告</button></div>
              </div>
            </article>
          </template>
          <article v-if="running" class="agent-message agent-message--assistant" aria-label="当前分析进度"><div class="agent-message-label">AI 分析</div><div class="agent-answer"><p class="agent-progress" role="status"><span class="agent-progress-dot"></span>{{ progress || '正在分析…' }}</p></div></article>
          <div v-if="streamError" class="agent-inline-error" role="alert"><p>{{ streamError }}</p><button type="button" @click="resumeStream">恢复接收回答</button></div>
          <div v-if="runError" class="agent-inline-error" role="alert"><p>{{ runError }}</p><button v-if="retryMessage" type="button" :disabled="sending || running || !canAnalyze" @click="send(retryMessage)">重试分析</button></div>
          <div v-if="notice" class="agent-inline-notice" role="status">{{ notice }}</div>
        </div>
      </div>

      <footer class="agent-composer-footer">
        <button v-if="showJump" class="agent-jump" type="button" @click="scrollBottom(true)">↓ 回到最新消息</button>
        <nav v-if="quickMetrics.length" class="agent-quick-metrics" aria-label="常用指标"><span>常用指标</span><button v-for="metric in quickMetrics" :key="metric.id" type="button" :title="metric.description" :disabled="!canRunMetric" @click="runMetric(metric)">{{ metric.title }}</button><button v-if="keywordMetric" type="button" :title="keywordMetric.description" :disabled="!canRunMetric" @click="runMetric(keywordMetric)">跟进关键词</button></nav>
        <form class="agent-composer" @submit.prevent="send()"><label class="sr-only" for="agent-question">你的分析问题</label><textarea id="agent-question" ref="questionInput" v-model="draft" rows="2" maxlength="8000" placeholder="向 CRM 提问…" :disabled="conversationLoading" @compositionstart="composing = true" @compositionend="composing = false" @keydown="onQuestionKeydown"></textarea><div class="agent-composer-actions"><span>{{ running ? '分析正在进行，可以停止后调整问题' : 'Enter 发送 · Shift + Enter 换行' }}</span><button v-if="running" type="button" class="agent-stop" :disabled="stopping" @click="stop">{{ stopping ? '正在停止' : '停止生成' }}</button><button v-else type="submit" class="agent-send" :disabled="sending || !draft.trim() || !canAnalyze || conversationLoading">{{ sending ? '正在发送' : '发送' }}</button></div></form>
        <p class="agent-disclaimer">数字由系统按固定指标计算；AI 说明可能出错，请核对表格中的数据范围与依据。</p>
      </footer>
    </main>
    <AgentSettings v-if="settingsOpen" @close="settingsOpen = false" @changed="loadSettings" />
    <div v-if="editingConversation" class="agent-dialog-backdrop" @click.self="editingConversation = null"><section ref="editorPanel" class="agent-dialog" role="dialog" aria-modal="true" aria-labelledby="agent-edit-title"><h2 id="agent-edit-title">管理会话</h2><form @submit.prevent="rename"><label>会话名称<input v-model="editTitle" maxlength="100" required></label><p v-if="editError" role="alert">{{ editError }}</p><div><button type="button" @click="editingConversation = null">取消</button><button type="submit" :disabled="editingBusy || !editTitle.trim()">保存名称</button></div></form><button class="agent-delete" type="button" :disabled="editingBusy" @click="removeConversation">删除此会话</button></section></div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog } from 'vant'
import { useUserStore } from '@/stores/user'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { agentAPI } from '@/api/agent'
import AgentMarkdown from '@/components/AgentMarkdown.vue'
import AgentResult from '@/components/AgentResult.vue'
import AgentRunEvidence from '@/components/AgentRunEvidence.vue'
import AgentSettings from '@/components/AgentSettings.vue'

const route = useRoute(), router = useRouter(), userStore = useUserStore()
const conversationId = computed(() => String(route.params.conversationId || ''))
const accountKey = String(userStore.userInfo?.id || userStore.userInfo?.account_id || userStore.accountName())
const storageKey = (kind, id = conversationId.value || 'new') => `crm-agent:${accountKey}:${kind}:${id}`
const readStorage = key => { try { return sessionStorage.getItem(key) || '' } catch { return '' } }
const writeStorage = (key, value) => { try { value ? sessionStorage.setItem(key, value) : sessionStorage.removeItem(key) } catch { /* Storage can be unavailable in a private session. */ } }
const draft = ref(''), composing = ref(false), conversations = ref([]), historyTotal = ref(0), historyPage = ref(1), historyLoading = ref(false), historyError = ref(false), historyOpen = ref(false), search = ref(''), currentConversation = ref(null), conversationLoading = ref(false), conversationError = ref(''), messages = ref([]), results = ref({}), settings = ref(null), settingsError = ref(false), settingsOpen = ref(false)
const viewportHeight = ref(0), hasOlderMessages = ref(false), olderLoading = ref(false)
const runSummaries = ref({})
const sending = ref(false), stopping = ref(false), activeRun = ref(null), progress = ref(''), streamingText = ref(''), streamError = ref(''), runError = ref(''), retryMessage = ref(''), notice = ref(''), exporting = ref(null), showJump = ref(false), messageList = ref(null), questionInput = ref(null), historyPanel = ref(null), editorPanel = ref(null), editingConversation = ref(null), editTitle = ref(''), editError = ref(''), editingBusy = ref(false)
const editorOpen = computed(() => Boolean(editingConversation.value))
useOverlayFocus(historyOpen, historyPanel, () => { historyOpen.value = false })
useOverlayFocus(editorOpen, editorPanel, () => { editingConversation.value = null })
const terminalStatuses = ['completed', 'failed', 'cancelled', 'canceled', 'interrupted', 'budget_exceeded']
const running = computed(() => Boolean(activeRun.value && !terminalStatuses.includes(activeRun.value.status)))
const visibleMessages = computed(() => messages.value.filter(message => message.role !== 'assistant' || message.status !== 'running'))
const budgetReached = computed(() => Boolean(settings.value && settings.value.budget_micro <= Number(settings.value.spent_micro || 0) + Number(settings.value.reserved_micro || 0)))
const canAnalyze = computed(() => settings.value?.enabled === true && settings.value?.configured === true && !budgetReached.value && !settingsError.value)
let historyVersion = 0, conversationVersion = 0, controller, reconnectTimer, searchTimer, lastSeq = 0, reconnects = 0, disposed = false, pendingRequest = null
const metricCatalog = ref([])
const quickMetrics = computed(() => metricCatalog.value.filter(metric => metric.quick))
const keywordMetric = computed(() => metricCatalog.value.find(metric => metric.id === 'followup_keyword'))
const canRunMetric = computed(() => !sending.value && !running.value && !conversationLoading.value)
const loadMetrics = async () => { try { const data = await agentAPI.metrics(); metricCatalog.value = Array.isArray(data?.metrics) ? data.metrics : [] } catch { metricCatalog.value = [] } }
const getError = (failure, fallback) => failure.response?.data?.message || failure.message || fallback
const loadSettings = async () => {
  settingsError.value = false
  try { settings.value = await agentAPI.settings() } catch { settingsError.value = true }
}
const loadHistory = async (more = false) => {
  const version = ++historyVersion
  historyLoading.value = true; historyError.value = false
  const page = more ? historyPage.value + 1 : 1
  try {
    const data = await agentAPI.conversations(search.value, page)
    if (version !== historyVersion || disposed) return
    conversations.value = more ? [...conversations.value, ...(data.conversations || [])] : data.conversations || []
    const current = conversations.value.find(conversation => String(conversation.id) === conversationId.value)
    if (current && currentConversation.value) currentConversation.value = { ...currentConversation.value, title: current.title }
    historyTotal.value = data.total ?? conversations.value.length; historyPage.value = page
  } catch { if (version === historyVersion) historyError.value = true }
  finally { if (version === historyVersion) historyLoading.value = false }
}
const mergeMessages = incoming => {
  for (const message of incoming || []) {
    const index = messages.value.findIndex(current => String(current.id) === String(message.id))
    if (index >= 0) messages.value[index] = message
    else messages.value.push(message)
  }
}
const rememberResult = result => { results.value = { ...results.value, [result.id]: result } }
const absorbRun = data => {
  const run = data.run || data
  if (run?.id) { activeRun.value = run; runSummaries.value = { ...runSummaries.value, [run.id]: run } }
  if (Array.isArray(data.messages)) mergeMessages(data.messages)
  for (const result of data.results || []) rememberResult(result)
  return run
}
const disconnect = () => { controller?.abort(); controller = null; clearTimeout(reconnectTimer) }
const loadConversation = async id => {
  const version = ++conversationVersion
  disconnect(); activeRun.value = null; lastSeq = 0; reconnects = 0; streamingText.value = ''; progress.value = ''; streamError.value = ''; runError.value = ''; notice.value = ''; retryMessage.value = ''; messages.value = []; results.value = {}; currentConversation.value = null; conversationError.value = ''; hasOlderMessages.value = false
  draft.value = readStorage(storageKey('draft', id || 'new'))
  if (!id) { conversationLoading.value = false; await nextTick(); questionInput.value?.focus(); return }
  conversationLoading.value = true
  runSummaries.value = {}
  try {
    const data = await agentAPI.conversation(id)
    if (version !== conversationVersion || disposed) return
    currentConversation.value = data.conversation || { id }; messages.value = data.messages || []; hasOlderMessages.value = data.has_more === true
    runSummaries.value = Object.fromEntries((data.runs || []).map(run => [run.id, run]))
    const sortedRuns = [...(data.runs || [])].sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
    const latest = sortedRuns.find(run => !terminalStatuses.includes(run.status))
    if (latest) {
      activeRun.value = latest; lastSeq = Number(readStorage(storageKey('cursor', latest.id))) || 0
      await refreshRun(latest.id)
      if (running.value) subscribe(latest.id)
    } else {
      const last = sortedRuns[0]
      if (last && ['failed', 'interrupted', 'budget_exceeded'].includes(last.status)) { runError.value = last.error_message || last.error || '上次分析未完成，可调整问题后重试'; retryMessage.value = [...messages.value].reverse().find(message => message.role === 'user' && String(message.run_id) === String(last.id))?.content || '' }
    }
    await nextTick()
    if (messageList.value) messageList.value.scrollTop = Number(readStorage(storageKey('scroll', id))) || messageList.value.scrollHeight
  } catch (failure) { if (version === conversationVersion) conversationError.value = getError(failure, '对话读取失败，请重试') }
  finally { if (version === conversationVersion) conversationLoading.value = false }
}
const loadOlderMessages = async () => {
  const id = conversationId.value, first = messages.value[0]
  if (!id || !first?.created_at || olderLoading.value) return
  olderLoading.value = true
  const previousHeight = messageList.value?.scrollHeight || 0
  try {
    const data = await agentAPI.conversation(id, first.created_at)
    if (id !== conversationId.value) return
    const existing = new Set(messages.value.map(message => String(message.id)))
    messages.value = [...(data.messages || []).filter(message => !existing.has(String(message.id))), ...messages.value]
    hasOlderMessages.value = data.has_more === true
    await nextTick(); if (messageList.value) messageList.value.scrollTop += messageList.value.scrollHeight - previousHeight
  } catch (failure) { notice.value = getError(failure, '更早消息读取失败，请重试') }
  finally { olderLoading.value = false }
}
const refreshRun = async id => {
  const data = await agentAPI.run(id)
  if (String(activeRun.value?.id) !== String(id) || disposed) return
  const run = absorbRun(data)
  if (terminalStatuses.includes(run.status)) {
    streamingText.value = ''; progress.value = ''; streamError.value = ''
    if (['failed', 'interrupted', 'budget_exceeded'].includes(run.status)) {
      runError.value = run.error_message || run.error || (run.status === 'budget_exceeded' ? '分析额度不足，已完成的结果已保留' : '分析未完成，已有内容已保留，请重试')
      retryMessage.value = [...messages.value].reverse().find(message => message.role === 'user' && String(message.run_id) === String(id))?.content || ''
    }
    await loadSettings(); loadHistory()
  }
  await scrollBottom(false)
}
const subscribe = async id => {
  disconnect()
  controller = new AbortController()
  const localController = controller
  try {
    const terminal = await agentAPI.stream(id, lastSeq, localController.signal, event => {
      if (localController.signal.aborted || String(activeRun.value?.id) !== String(id)) return
      if (event.seq && Number(event.seq) <= lastSeq) return
      lastSeq = Number(event.seq || lastSeq); writeStorage(storageKey('cursor', id), String(lastSeq))
      const data = event.data || {}
      if (event.type === 'status') progress.value = typeof data === 'string' ? data : data.label || data.message || data.text || data.status || '正在分析…'
      if (event.type === 'provider_switch') { streamingText.value = ''; progress.value = `主模型暂不可用，已切换至备用模型 ${data.model || ''}`; if (activeRun.value) activeRun.value.model = data.model || activeRun.value.model }
      // Older servers may replay draft deltas; only the durable verified message is displayed.
      if (event.type === 'delta') progress.value = '正在核对回答…'
      if (event.type === 'message') { if (data.message) mergeMessages([data.message]); else if (data.id) mergeMessages([data]); streamingText.value = '' }
      if (data.run) activeRun.value = data.run
      if (event.type === 'error') runError.value = data.message || data.error || '分析未完成，请重试'
      scrollBottom(false)
    })
    if (localController.signal.aborted || disposed) return
    await refreshRun(id)
    if (!terminal && running.value) scheduleReconnect(id)
  } catch (failure) {
    if (localController.signal.aborted || disposed) return
    if (failure.status === 401 || failure.status === 403) { streamError.value = failure.message; activeRun.value = { ...activeRun.value, status: 'interrupted' }; return }
    scheduleReconnect(id)
  }
}
const scheduleReconnect = id => {
  if (reconnects >= 5) { streamError.value = '回答连接已中断，分析可能仍在进行。恢复接收会继续读取原任务，不会重复发起分析。'; return }
  reconnects++; progress.value = '连接暂时中断，正在恢复接收…'
  reconnectTimer = setTimeout(async () => {
    if (disposed || String(activeRun.value?.id) !== String(id)) return
    try { await refreshRun(id); if (running.value) subscribe(id) } catch { scheduleReconnect(id) }
  }, Math.min(1000 * 2 ** reconnects, 8000))
}
const resumeStream = async () => {
  if (!activeRun.value?.id) return
  streamError.value = ''; reconnects = 0
  try { await refreshRun(activeRun.value.id); if (running.value) subscribe(activeRun.value.id) } catch (failure) { streamError.value = getError(failure, '恢复失败，请重试') }
}
const newConversation = async () => { historyOpen.value = false; if (conversationId.value) await router.push('/agent'); else { draft.value = ''; writeStorage(storageKey('draft'), ''); messages.value = []; questionInput.value?.focus() } }
const requestId = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`
const send = async retry => {
  const text = typeof retry === 'string' ? retry.trim() : draft.value.trim()
  if (!text || sending.value || running.value || !canAnalyze.value || composing.value) return
  sending.value = true; runError.value = ''; streamError.value = ''; notice.value = ''
  let id = conversationId.value
  try {
    if (!id) {
      const data = await agentAPI.createConversation(), conversation = data.conversation || data
      id = String(conversation.id)
      await router.push(`/agent/${id}`)
      await loadConversation(id)
      // The original draft belongs to the accepted newly created conversation.
      draft.value = text; writeStorage(storageKey('draft', 'new'), '')
    }
    const storedRequest = readStorage(storageKey('pending', id))
    if (storedRequest && !pendingRequest) { try { pendingRequest = JSON.parse(storedRequest) } catch { /* invalid browser cache */ } }
    if (pendingRequest?.message !== text || pendingRequest?.conversation !== id) pendingRequest = { id: requestId(), message: text, conversation: id }
    writeStorage(storageKey('pending', id), JSON.stringify(pendingRequest))
    const data = await agentAPI.createRun(id, text, pendingRequest.id)
    if (conversationId.value !== id) return
    const run = data.run || data
    activeRun.value = run; pendingRequest = null; writeStorage(storageKey('pending', id), '')
    draft.value = ''; writeStorage(storageKey('draft', id), ''); lastSeq = 0; reconnects = 0; streamingText.value = ''; progress.value = '正在查询数据…'
    await refreshRun(run.id)
    if (running.value) subscribe(run.id)
    loadHistory(); await scrollBottom(true)
  } catch (failure) { runError.value = getError(failure, '发送失败，问题已保留，请重试'); retryMessage.value = text; if (!draft.value) draft.value = text }
  finally { sending.value = false }
}
// A fixed metric needs no model: it also works while AI analysis is off.
const runMetric = async metric => {
  if (!metric || !canRunMetric.value) return
  const params = {}
  if (metric.id === 'followup_keyword') {
    const keyword = draft.value.trim()
    if (!keyword || [...keyword].length > 20) { notice.value = '请先在输入框写入要检索的跟进关键词（不超过20字），再点击“跟进关键词”'; return }
    params.keyword = keyword
  }
  sending.value = true; runError.value = ''; streamError.value = ''; notice.value = ''
  let id = conversationId.value
  try {
    if (!id) {
      const data = await agentAPI.createConversation(), conversation = data.conversation || data
      id = String(conversation.id)
      await router.push(`/agent/${id}`)
      await loadConversation(id)
    }
    const data = await agentAPI.createMetricRun(id, metric.id, params, requestId())
    if (conversationId.value !== id) return
    const run = data.run || data
    activeRun.value = run
    if (metric.id === 'followup_keyword') { draft.value = ''; writeStorage(storageKey('draft', id), '') }
    lastSeq = 0; reconnects = 0; streamingText.value = ''; progress.value = '正在按固定指标计算…'
    await refreshRun(run.id)
    if (running.value) subscribe(run.id)
    loadHistory(); await scrollBottom(true)
  } catch (failure) { runError.value = getError(failure, '指标读取失败，请重试') }
  finally { sending.value = false }
}
const stop = async () => {
  if (!activeRun.value?.id || stopping.value) return
  stopping.value = true
  try {
    const id = activeRun.value.id
    await agentAPI.cancelRun(id); disconnect(); await refreshRun(id)
    for (let attempt = 0; attempt < 5 && running.value && String(activeRun.value?.id) === String(id); attempt++) {
      await new Promise(resolve => setTimeout(resolve, 150)); await refreshRun(id)
    }
    if (String(activeRun.value?.id) !== String(id)) return
    if (running.value) { progress.value = '正在停止分析…'; subscribe(id) }
    else notice.value = '分析已停止，已有回答和结果已保留'
  }
  catch (failure) { runError.value = getError(failure, '停止失败，请重试') }
  finally { stopping.value = false }
}
const onQuestionKeydown = event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && !composing.value) { event.preventDefault(); send() } }
const saveScroll = () => {
  const list = messageList.value
  if (!list) return
  showJump.value = list.scrollHeight - list.scrollTop - list.clientHeight > 120
  writeStorage(storageKey('scroll'), String(list.scrollTop))
}
const scrollBottom = async force => { await nextTick(); if (messageList.value && (force || !showJump.value)) { messageList.value.scrollTop = messageList.value.scrollHeight; showJump.value = false } }
const copyAnswer = async message => { try { await navigator.clipboard.writeText(message.content || (message.blocks || []).filter(block => block.type === 'markdown').map(block => block.text).join('\n')); notice.value = '回答已复制' } catch { notice.value = '复制失败，请选择回答文字手动复制' } }
const exportAnswer = async (id, format) => { exporting.value = id; notice.value = ''; try { await agentAPI.exportRun(id, format); notice.value = '结果已导出，使用回答时的数据快照' } catch (failure) { notice.value = getError(failure, '导出失败，请重试') } finally { exporting.value = null } }
const editConversation = conversation => { editingConversation.value = conversation; editTitle.value = conversation.title || '新对话'; editError.value = '' }
const rename = async () => {
  editingBusy.value = true
  try { await agentAPI.renameConversation(editingConversation.value.id, editTitle.value.trim()); if (String(editingConversation.value.id) === conversationId.value) currentConversation.value.title = editTitle.value.trim(); editingConversation.value = null; await loadHistory() }
  catch (failure) { editError.value = getError(failure, '会话重命名失败') } finally { editingBusy.value = false }
}
const removeConversation = async () => {
  try { await showConfirmDialog({ title: '删除会话', message: '会话中的回答、查询结果和导出记录将一并删除，确认删除？', confirmButtonText: '删除' }) } catch { return }
  editingBusy.value = true
  try { const id = String(editingConversation.value.id); await agentAPI.deleteConversation(id); editingConversation.value = null; writeStorage(storageKey('draft', id), ''); writeStorage(storageKey('scroll', id), ''); if (id === conversationId.value) await router.replace('/agent'); await loadHistory() }
  catch (failure) { editError.value = getError(failure, '会话删除失败') } finally { editingBusy.value = false }
}
const updateViewport = () => { viewportHeight.value = window.innerWidth <= 767 ? Math.round(window.visualViewport?.height || window.innerHeight) : 0 }
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => loadHistory(), 300) })
watch(draft, value => writeStorage(storageKey('draft'), value))
watch(conversationId, id => { pendingRequest = null; loadConversation(id) })
onMounted(() => { loadSettings(); loadMetrics(); loadHistory(); loadConversation(conversationId.value); updateViewport(); window.visualViewport?.addEventListener('resize', updateViewport); window.addEventListener('resize', updateViewport) })
onBeforeUnmount(() => { disposed = true; historyVersion++; conversationVersion++; disconnect(); clearTimeout(searchTimer); window.visualViewport?.removeEventListener('resize', updateViewport); window.removeEventListener('resize', updateViewport) })
</script>

<style scoped>
.agent-workspace { display: flex; width: 100%; height: 100vh; height: 100dvh; overflow: hidden; background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-history { display: flex; width: 270px; min-width: 270px; flex-direction: column; border-right: 1px solid var(--kx-color-border); background: var(--kx-color-surface-subtle); }
.agent-history > header { display: flex; min-height: 66px; align-items: center; justify-content: space-between; gap: 8px; padding: 0 20px; }
.agent-back { color: var(--kx-color-text-secondary); font-size: 13px; text-decoration: none; }
.agent-workspace button { font: inherit; cursor: pointer; }
.agent-new { min-height: 44px; margin: 0 16px 16px; padding: 10px 16px; border: 1px solid var(--kx-color-border-strong); border-radius: 10px; background: var(--kx-color-surface); color: var(--kx-color-text); text-align: left; font-weight: 600; }
.agent-search { padding: 0 16px 16px; }
.agent-search input { width: 100%; min-width: 0; min-height: 40px; box-sizing: border-box; padding: 8px 12px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); font: inherit; font-size: 13px; }
.agent-conversations { min-height: 0; flex: 1; overflow: auto; padding: 0 10px; }
.agent-conversation { display: flex; min-width: 0; min-height: 46px; align-items: center; gap: 2px; margin-bottom: 3px; border-radius: 8px; }
.agent-conversation:hover, .agent-conversation.active { background: var(--kx-color-border); }
.agent-conversation a { min-width: 0; flex: 1; padding: 12px 10px; color: var(--kx-color-text); text-decoration: none; font-size: 13px; }
.agent-conversation a span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-conversation > button { min-width: 40px; min-height: 40px; border: 0; background: transparent; color: var(--kx-color-text-secondary); }
.history-state { margin: 8px 16px; color: var(--kx-color-text-secondary); font-size: 13px; line-height: 1.6; }
.history-state button, .history-more { min-height: 40px; padding: 8px; border: 0; background: transparent; color: var(--kx-color-brand); }
.history-more { width: 100%; }
.agent-history > footer { display: grid; gap: 10px; padding: 16px; border-top: 1px solid var(--kx-color-border); font-size: 12px; }
.agent-history > footer span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--kx-color-text-secondary); }
.agent-history > footer button { min-height: 40px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-main { display: flex; min-width: 0; flex: 1; flex-direction: column; outline: none; }
.agent-header { display: flex; min-height: 72px; align-items: center; gap: 12px; padding: 0 28px; border-bottom: 1px solid var(--kx-color-border); }
.agent-header > div { min-width: 0; flex: 1; }
.agent-header h1 { margin: 0; font-size: 18px; }
.agent-header p { overflow: hidden; margin: 4px 0 0; color: var(--kx-color-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.agent-settings-toggle, .history-toggle, .history-close { min-height: 40px; padding: 8px 12px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text-secondary); }
.history-toggle, .history-close { display: none; }
.agent-notice { padding: 10px 24px; background: var(--kx-amber-50); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.agent-notice button { min-height: 32px; padding: 4px 8px; border: 0; background: transparent; color: var(--kx-color-brand); }
.agent-messages { min-height: 0; flex: 1; overflow: auto; overscroll-behavior: contain; }
.agent-message-inner { width: min(100%, 880px); min-height: 100%; box-sizing: border-box; margin: 0 auto; padding: 28px 32px 16px; }
.agent-state { display: grid; justify-items: center; gap: 12px; padding: 60px 16px; font-size: 14px; }
.agent-state button { min-height: 44px; padding: 10px 16px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-brand); }
.agent-older { display: block; min-height: 40px; margin: 0 auto 20px; padding: 8px 16px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text-secondary); font-size: 12px; }
.agent-welcome { display: flex; min-height: 320px; flex-direction: column; justify-content: center; align-items: center; text-align: center; }
.agent-mark { color: var(--kx-color-brand); font-size: 42px; }
.agent-welcome h2 { margin: 16px 0 12px; font-size: 26px; letter-spacing: -.5px; }
.agent-welcome p { margin: 0; color: var(--kx-color-text-secondary); font-size: 14px; line-height: 1.9; }
.agent-message { display: grid; grid-template-columns: 62px minmax(0, 1fr); gap: 12px; margin: 0 0 28px; }
.agent-message-label { padding-top: 8px; color: var(--kx-color-text-secondary); font-size: 12px; font-weight: 600; }
.agent-user-content { justify-self: end; max-width: 100%; padding: 12px 16px; border-radius: 16px 16px 4px 16px; background: var(--kx-color-surface-subtle); font-size: 14px; line-height: 1.8; white-space: pre-wrap; overflow-wrap: anywhere; }
.agent-answer { min-width: 0; font-size: 14px; line-height: 1.85; overflow-wrap: anywhere; }
.agent-answer-actions { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 12px; }
.agent-answer-actions button { min-height: 36px; padding: 6px 10px; border: 0; border-radius: 6px; background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); font-size: 12px; }
.agent-answer-actions button:disabled { opacity: .5; }
.agent-progress { display: flex; align-items: center; gap: 8px; color: var(--kx-color-text-secondary); font-size: 13px; }
.agent-progress-dot { width: 7px; height: 7px; flex: 0 0 7px; border-radius: 50%; background: var(--kx-color-brand); animation: agent-pulse 1.5s ease-in-out infinite; }
.agent-evidence { display: flex; flex-wrap: wrap; gap: 10px; margin: 12px 0; font-size: 12px; }
.agent-evidence a { color: var(--kx-color-brand); }
.agent-inline-error, .agent-inline-notice { margin-bottom: 20px; padding: 12px 16px; border-radius: 10px; background: var(--kx-color-surface-subtle); font-size: 13px; overflow-wrap: anywhere; }
.agent-inline-error { border: 1px solid var(--kx-color-danger); }
.agent-inline-error p { margin: 0 0 8px; }
.agent-inline-error button { min-height: 36px; padding: 6px 10px; border: 1px solid var(--kx-color-border); border-radius: 6px; background: var(--kx-color-surface); color: var(--kx-color-brand); }
.agent-composer-footer { position: relative; width: min(100%, 880px); box-sizing: border-box; margin: 0 auto; padding: 8px 32px max(12px, env(safe-area-inset-bottom)); }
.agent-composer { padding: 12px 14px 10px; border: 1px solid var(--kx-color-border-strong); border-radius: 16px; background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); }
.agent-composer:focus-within { border-color: var(--kx-color-brand); }
.agent-composer textarea { display: block; width: 100%; min-height: 48px; max-height: 180px; box-sizing: border-box; resize: vertical; padding: 0; border: 0; background: transparent; color: var(--kx-color-text); font: inherit; font-size: 14px; line-height: 1.65; outline: none; }
.agent-composer-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 8px; }
.agent-composer-actions span { min-width: 0; color: var(--kx-color-text-secondary); font-size: 11px; }
.agent-send, .agent-stop { min-width: 66px; min-height: 44px; padding: 8px 14px; border: 0; border-radius: 10px; background: var(--kx-color-brand); color: var(--kx-color-on-brand); white-space: nowrap; }
.agent-stop { border: 1px solid var(--kx-color-border-strong); background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-send:disabled, .agent-stop:disabled { opacity: .5; cursor: default; }
.agent-quick-metrics { display: flex; align-items: center; gap: 8px; margin: 0 0 8px; overflow-x: auto; scrollbar-width: none; white-space: nowrap; }
.agent-quick-metrics::-webkit-scrollbar { display: none; }
.agent-quick-metrics > span { flex: none; color: var(--kx-color-text-secondary); font-size: 12px; }
.agent-quick-metrics button { flex: none; min-height: 36px; padding: 6px 12px; border: 1px solid var(--kx-color-border); border-radius: 999px; background: var(--kx-color-surface); color: var(--kx-color-text); font-size: 13px; }
.agent-quick-metrics button:disabled { cursor: not-allowed; opacity: .55; }
.agent-disclaimer { margin: 9px 0 0; color: var(--kx-color-text-secondary); font-size: 11px; text-align: center; }
.agent-jump { position: absolute; right: 32px; bottom: calc(100% + 8px); min-height: 36px; padding: 6px 12px; border: 1px solid var(--kx-color-border); border-radius: 20px; background: var(--kx-color-surface); color: var(--kx-color-text); font-size: 12px; box-shadow: var(--kx-shadow-float); }
.agent-history-overlay { display: none; }
.agent-dialog-backdrop { position: fixed; z-index: 110; inset: 0; display: grid; place-items: center; padding: 16px; background: var(--kx-overlay-strong); }
.agent-dialog { width: min(400px, 100%); box-sizing: border-box; padding: 24px; border-radius: 16px; background: var(--kx-color-surface); }
.agent-dialog h2 { margin: 0 0 20px; font-size: 18px; }
.agent-dialog label { display: grid; gap: 8px; font-size: 14px; }
.agent-dialog input { width: 100%; min-width: 0; min-height: 44px; box-sizing: border-box; padding: 10px; border: 1px solid var(--kx-color-border-strong); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); font: inherit; }
.agent-dialog form > div { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.agent-dialog button { min-height: 40px; padding: 8px 12px; border: 1px solid var(--kx-color-border); border-radius: 8px; background: var(--kx-color-surface); color: var(--kx-color-text); }
.agent-dialog .agent-delete { margin-top: 24px; color: var(--kx-color-danger); }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
:deep(.agent-markdown p) { margin: 0 0 12px; }
:deep(.agent-markdown h1), :deep(.agent-markdown h2), :deep(.agent-markdown h3) { margin: 20px 0 10px; font-size: 17px; }
:deep(.agent-markdown pre) { max-width: 100%; overflow: auto; padding: 12px; border-radius: 8px; background: var(--kx-color-surface-subtle); }
:deep(.agent-markdown code) { font-size: 12px; }
:deep(.agent-markdown a) { color: var(--kx-color-brand); }
:deep(.agent-markdown table) { display: block; max-width: 100%; overflow: auto; border-collapse: collapse; }
:deep(.agent-markdown th), :deep(.agent-markdown td) { padding: 8px 12px; border: 1px solid var(--kx-color-border); }
:deep(.agent-markdown blockquote) { margin: 12px 0; padding-left: 14px; border-left: 3px solid var(--kx-color-border-strong); color: var(--kx-color-text-secondary); }
@keyframes agent-pulse { 50% { opacity: .3; } }
@media (prefers-reduced-motion: reduce) { .agent-progress-dot { animation: none; } }
@media (max-width: 1023px) { .agent-history { width: 230px; min-width: 230px; } .agent-message-inner { padding: 24px 20px; } .agent-composer-footer { padding-right: 20px; padding-left: 20px; } }
@media (max-width: 767px) {
  .agent-history { position: fixed; z-index: 80; inset: 0 auto 0 0; width: min(290px, 88vw); min-width: 0; visibility: hidden; transform: translateX(-100%); }
  .agent-history--open { visibility: visible; transform: none; }
  .agent-history-overlay { position: fixed; z-index: 75; inset: 0; display: block; background: var(--kx-overlay-strong); }
  .history-toggle, .history-close { display: inline-block; }
  .agent-header { min-height: 64px; padding: 0 12px; gap: 10px; }
  .agent-header h1 { font-size: 16px; }
  .agent-notice { padding: 8px 16px; font-size: 12px; }
  .agent-message-inner { padding: 22px 16px 8px; }
  .agent-message { grid-template-columns: minmax(0, 1fr); gap: 8px; margin-bottom: 24px; }
  .agent-message-label { padding-top: 0; }
  .agent-message--user .agent-message-label { text-align: right; }
  .agent-composer-footer { padding-right: 12px; padding-left: 12px; }
  .agent-composer-actions span { font-size: 10px; }
  .agent-welcome { min-height: 240px; }
  .agent-welcome h2 { font-size: 23px; }
  .agent-welcome p { font-size: 13px; }
  .agent-jump { right: 16px; }
}
</style>
