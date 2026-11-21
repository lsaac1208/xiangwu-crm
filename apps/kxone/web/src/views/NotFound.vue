<template>
  <main class="not-found-page">
    <section class="not-found-card" aria-labelledby="not-found-title">
      <div class="error-code" aria-hidden="true">404</div>
      <p class="eyebrow">KXOne CRM</p>
      <h1 id="not-found-title">这个页面不存在</h1>
      <p class="description">链接可能已失效，或者地址输入有误。你可以返回线索列表继续工作。</p>
      <div class="actions">
        <van-button type="primary" block to="/leads">返回线索列表</van-button>
        <van-button block @click="goBack">返回上一页</van-button>
      </div>
    </section>
  </main>
</template>

<script setup>
const goBack = () => {
  if (window.history.length > 1) {
    window.history.back()
    return
  }
  window.location.assign('/leads')
}
</script>

<style scoped>
.not-found-page { min-height: 100vh; min-height: 100dvh; display: grid; place-items: center; padding: var(--kx-space-6); background: var(--kx-color-canvas); }
.not-found-card { width: min(520px, 100%); padding: var(--kx-space-8); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); text-align: center; }
.error-code { color: var(--kx-color-text-secondary); font-size: clamp(64px, 16vw, 88px); font-weight: 600; line-height: 1.1; letter-spacing: -0.04em; font-variant-numeric: tabular-nums; }
.eyebrow { margin: var(--kx-space-6) 0 var(--kx-space-2); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); font-weight: 500; }
h1 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-font-26); font-weight: 600; }
.description { max-width: 390px; margin: var(--kx-space-4) auto var(--kx-space-6); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.7; }
.actions { display: grid; grid-template-columns: 1fr 1fr; gap: var(--kx-space-3); }
.actions :deep(.van-button) { height: var(--kx-control-current-height); border-radius: var(--kx-control-radius); }
@media (max-width: 767px) {
  .not-found-page { padding: var(--kx-space-4); padding-bottom: calc(var(--kx-space-4) + env(safe-area-inset-bottom)); }
  .not-found-card { padding: var(--kx-space-6); border: 0; box-shadow: none; }
  .error-code { font-size: 64px; font-weight: 500; }
  h1 { font-size: var(--kx-mobile-title-size); letter-spacing: -0.03em; }
  .actions { grid-template-columns: 1fr; }
  .actions :deep(.van-button--default) { border: 0; background: var(--kx-color-mobile-fill); }
}
</style>
