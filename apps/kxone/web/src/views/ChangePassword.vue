<template>
  <main class="password-page">
    <van-nav-bar class="mobile-task-nav" title="修改密码" :left-text="mobileLayout ? '返回' : ''" left-arrow @click-left="goBack" />
    <section class="password-card">
      <div class="heading">
        <span>账号安全</span>
        <h1>修改登录密码</h1>
        <p>修改成功后将退出当前登录，请使用新密码重新登录。</p>
      </div>
      <van-form class="crm-form" label-align="left" @submit="submit">
        <p v-if="submitError" class="form-error" role="alert">{{ submitError }}</p>
        <van-cell-group inset>
          <van-field
            v-model="form.currentPassword"
            label="当前密码"
            required
            type="password"
            autocomplete="current-password"
            placeholder="请输入当前密码"
            :rules="[{ required: true, message: '请输入当前密码' }]"
          />
          <van-field
            v-model="form.newPassword"
            label="新密码"
            required
            type="password"
            autocomplete="new-password"
            placeholder="8-72 位"
            :rules="[{ validator: validatePassword, message: '新密码长度必须为 8-72 位' }]"
          />
          <van-field
            v-model="form.confirmPassword"
            label="确认密码"
            required
            type="password"
            autocomplete="new-password"
            placeholder="请再次输入新密码"
            :rules="[{ validator: validateConfirmation, message: '两次输入的新密码不一致' }]"
          />
        </van-cell-group>
        <van-button block type="primary" native-type="submit" :loading="saving">确认修改</van-button>
      </van-form>
    </section>
  </main>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showSuccessToast } from 'vant'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import api from '@/api'

const router = useRouter()
const { isMobile: mobileLayout } = useResponsiveLayout()
const goBack = () => {
  const previous = router.options.history.state.back
  if (typeof previous === 'string' && previous.startsWith('/') && !previous.startsWith('//') && !previous.includes('\\') && previous !== '/account/password') {
    router.back()
    return
  }
  router.replace('/leads')
}
const saving = ref(false)
const submitError = ref('')
const form = reactive({ currentPassword: '', newPassword: '', confirmPassword: '' })

const validatePassword = value => value?.length >= 8 && value?.length <= 72
const validateConfirmation = value => Boolean(value) && value === form.newPassword

const submit = async () => {
  if (saving.value) return
  submitError.value = ''
  saving.value = true
  try {
    await api.put('/user/password', {
      current_password: form.currentPassword,
      new_password: form.newPassword
    })
    showSuccessToast('密码已修改，请重新登录')
    window.setTimeout(() => window.location.replace('/login'), 700)
  } catch (error) {
    submitError.value = error.response?.data?.message || '修改失败，请检查当前密码和网络后重试'
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.form-error { margin: 0 0 var(--kx-space-4); padding: var(--kx-space-3); color: var(--kx-color-danger); border: 1px solid var(--kx-color-danger); border-radius: var(--kx-control-radius); background: var(--kx-color-danger-soft); font-size: var(--kx-font-13); line-height: 1.6; }
.password-page { min-height: 100vh; min-height: 100dvh; background: var(--kx-color-canvas); color: var(--kx-color-text); padding-bottom: calc(var(--kx-space-8) + env(safe-area-inset-bottom)); }
.password-card { width: min(520px, calc(100% - 48px)); margin: var(--kx-space-8) auto 0; padding: var(--kx-space-8); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); border: 1px solid var(--kx-color-border); box-shadow: var(--kx-shadow-subtle); }
.heading span { display: inline-block; margin-bottom: var(--kx-space-3); padding: var(--kx-space-1) var(--kx-space-2); border-radius: var(--kx-radius-1); color: var(--kx-color-text-secondary); background: var(--kx-color-segment); font-size: var(--kx-font-12); font-weight: 500; }
.heading h1 { margin: 0 0 var(--kx-space-2); font-size: var(--kx-font-26); font-weight: 600; }
.heading p { margin: 0 0 var(--kx-space-6); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); line-height: 1.6; }
.crm-form :deep(.van-cell-group) { border: 0; border-radius: 0; margin: 0 0 var(--kx-space-6); }
.crm-form :deep(.van-field) { padding: 0 0 var(--kx-space-4); overflow: visible; }
.crm-form :deep(.van-field:last-child) { padding-bottom: 0; }
.van-button { height: var(--kx-control-current-height); border-radius: var(--kx-control-radius); }
@media (max-width: 767px) {
  .password-card { width: calc(100% - 32px); margin-top: var(--kx-space-4); padding: var(--kx-space-6); border: 0; box-shadow: none; }
  .heading span { padding: 0; margin-bottom: var(--kx-space-2); background: transparent; }
  .heading h1 { font-size: var(--kx-mobile-title-size); letter-spacing: -0.03em; }
  .heading p { margin-bottom: var(--kx-space-4); font-size: var(--kx-font-13); }
  .crm-form :deep(.van-field) { padding: var(--kx-space-3) 0; }
  .crm-form :deep(.van-field:not(:last-child)::after) { left: 0; }
  .crm-form :deep(.van-field:last-child) { padding-bottom: var(--kx-space-3); }
}
</style>
