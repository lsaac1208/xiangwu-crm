<template>
  <main class="login-page">
    <section class="login-card">
      <div class="brand"><div class="brand-mark" aria-hidden="true">CRM</div><span>KXOne CRM</span></div>
      <h1>欢迎回来</h1>
      <p>使用 CRM 账号和密码登录</p>

      <van-form class="crm-form" label-align="left" @submit="submit">
        <p v-if="submitError" class="form-error" role="alert">{{ submitError }}</p>
        <van-cell-group inset>
          <van-field
            v-model.trim="form.username"
            name="username"
            label="账号"
            required
            autocomplete="username"
            placeholder="请输入账号"
            :rules="[{ required: true, message: '请输入账号' }]"
          />
          <van-field
            v-model="form.password"
            name="password"
            label="密码"
            required
            type="password"
            autocomplete="current-password"
            placeholder="请输入密码"
            :rules="[{ required: true, message: '请输入密码' }]"
          />
        </van-cell-group>
        <van-button block type="primary" native-type="submit" :loading="loading">登录</van-button>
      </van-form>
    </section>
  </main>
</template>

<script setup>
import { reactive, ref } from 'vue'
import api from '@/api'
import { loginReturnPath } from '@/utils/auth'

const loading = ref(false)
const submitError = ref(new URLSearchParams(window.location.search).get('reason') === 'session_expired' ? '登录状态已失效，请重新登录后继续操作。' : '')
const form = reactive({ username: '', password: '' })

const submit = async () => {
  if (loading.value) return
  submitError.value = ''
  loading.value = true
  try {
    await api.post('/auth/login', form)
    const redirect = new URLSearchParams(window.location.search).get('redirect')
    window.location.assign(loginReturnPath(redirect))
  } catch (error) {
    submitError.value = error.response?.data?.message || '登录失败，请检查账号、密码和网络后重试'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.form-error { margin: 0 0 var(--kx-space-4); padding: var(--kx-space-3); color: var(--kx-color-danger); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-control-radius); background: var(--kx-color-danger-soft); font-size: var(--kx-font-13); line-height: 1.6; }
.login-page { min-height: 100vh; min-height: 100dvh; display: grid; place-items: center; padding: var(--kx-space-6); padding-top: calc(var(--kx-space-6) + env(safe-area-inset-top)); padding-bottom: calc(var(--kx-space-6) + env(safe-area-inset-bottom)); background: var(--kx-color-canvas); }
.login-card { width: min(440px, 100%); padding: var(--kx-space-8); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); border: 1px solid var(--kx-color-border); box-shadow: var(--kx-shadow-subtle); }
.brand { display: flex; align-items: center; gap: var(--kx-space-3); margin-bottom: var(--kx-space-8); color: var(--kx-color-text); font-size: var(--kx-font-16); font-weight: 600; letter-spacing: -0.02em; }
.brand-mark { display: grid; place-items: center; width: 40px; height: 40px; border-radius: var(--kx-control-radius); color: var(--kx-color-on-brand); background: var(--kx-color-brand); font-size: var(--kx-font-12); font-weight: 600; letter-spacing: 0; }
h1 { margin: 0 0 var(--kx-space-2); color: var(--kx-color-text); font-size: var(--kx-font-26); font-weight: 600; }
p { margin: 0 0 var(--kx-space-6); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.6; }
.crm-form :deep(.van-cell-group) { border: 0; border-radius: 0; margin: 0 0 var(--kx-space-6); }
.crm-form :deep(.van-field) { padding: 0 0 var(--kx-space-4); overflow: visible; }
.crm-form :deep(.van-field:last-child) { padding-bottom: 0; }
.van-button { height: var(--kx-control-current-height); border-radius: var(--kx-control-radius); }
@media (max-width: 767px) {
  .login-page { padding-right: var(--kx-space-4); padding-left: var(--kx-space-4); }
  .login-card { padding: var(--kx-space-2); border: 0; background: transparent; box-shadow: none; }
  .brand { justify-content: center; font-size: var(--kx-mobile-heading-size); }
  .brand-mark { border-radius: var(--kx-mobile-control-radius); }
  h1 { font-size: var(--kx-mobile-title-size); letter-spacing: -0.03em; text-align: center; }
  .login-card > p { text-align: center; }
  .crm-form :deep(.van-cell-group) { padding: 0 var(--kx-space-4); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
  .crm-form :deep(.van-field) { padding: var(--kx-space-3) 0; }
  .crm-form :deep(.van-field:not(:last-child)::after) { left: 0; }
  .crm-form :deep(.van-field:last-child) { padding-bottom: var(--kx-space-3); }
}
</style>
