<template>
  <div class="app-shell" :class="{ 'app-shell--compact': isCompactDesktop, 'app-shell--task': hideMobileNav }" :data-keyboard-open="keyboardOpen">
    <a class="skip-link" href="#crm-main-content">跳到主要内容</a>
    <aside class="app-sidebar" aria-label="CRM 主导航">
      <router-link class="app-brand" to="/leads" aria-label="KXOne CRM 首页">
        <span>KX</span>
        <strong>KXOne CRM</strong>
      </router-link>
      <div class="app-navigation-groups">
        <nav v-for="group in navigationGroups" :key="group.label" class="app-nav" :aria-label="group.label">
          <span class="app-nav-caption">{{ group.label }}</span>
          <router-link v-for="item in group.items" :key="item.key" :to="item.to" :title="item.label">
            <van-icon :name="item.icon" aria-hidden="true" />
            <span>{{ item.label }}</span>
          </router-link>
        </nav>
      </div>
      <div class="app-account">
        <button ref="accountTrigger" type="button" class="app-account-summary" aria-label="当前账户" :aria-expanded="accountOpen" aria-controls="account-menu" @click="accountOpen = !accountOpen">
          <span class="app-avatar">{{ userStore.displayName().slice(0, 1) }}</span>
          <span class="app-account-copy"><strong>{{ userStore.displayName() }}</strong><small>@{{ userStore.accountName() }} · {{ userStore.roleName() }}</small></span>
          <van-icon name="arrow-up" aria-hidden="true" />
        </button>
        <div v-if="accountOpen" id="account-menu" class="app-account-menu" @keydown.esc.prevent="closeAccount">
          <button type="button" @click="goToPassword"><van-icon name="lock" aria-hidden="true" />修改密码</button>
          <button type="button" class="danger" :disabled="loggingOut" @click="onLogout"><van-icon name="revoke" aria-hidden="true" />退出登录</button>
        </div>
      </div>
    </aside>

    <main id="crm-main-content" class="app-content" tabindex="-1"><slot /></main>

    <nav v-if="!hideMobileNav" class="app-mobile-nav" aria-label="CRM 移动端主导航">
      <router-link v-for="item in mobileNavigation" :key="item.key" :to="item.to">
        <van-icon :name="item.icon" aria-hidden="true" />
        <span>{{ item.shortLabel || item.label }}</span>
      </router-link>
      <button type="button" :class="{ 'is-current': mobileMoreActive }" :aria-expanded="moreOpen" @click="moreOpen = true">
        <van-icon name="ellipsis" aria-hidden="true" />
        <span>更多</span>
      </button>
    </nav>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="morePopup" aria-label="更多功能" aria-modal="true" v-model:show="moreOpen" position="bottom" round class="app-more-popup">
      <div class="app-more-header">
        <span class="app-avatar">{{ userStore.displayName().slice(0, 1) }}</span>
        <div><strong>{{ userStore.displayName() }}</strong><small>@{{ userStore.accountName() }} · {{ userStore.roleName() }}</small></div>
        <button type="button" aria-label="关闭更多菜单" @click="moreOpen = false"><van-icon name="cross" aria-hidden="true" /></button>
      </div>
      <div v-if="mobileMoreNavigation.length" class="app-more-grid" role="group" aria-label="更多功能">
        <button v-for="item in mobileMoreNavigation" :key="item.key" type="button" :class="{ 'is-current': isNavigationActive(item.to) }" :aria-current="isNavigationActive(item.to) ? 'page' : undefined" @click="navigate(item.to)">
          <van-icon class="app-more-icon" :name="item.icon" aria-hidden="true" /><span>{{ item.label }}</span><van-icon class="app-more-chevron" name="arrow" aria-hidden="true" />
        </button>
      </div>
      <div class="app-more-grid app-more-account" role="group" aria-label="账户操作">
        <button type="button" @click="goToPassword"><van-icon class="app-more-icon" name="lock" aria-hidden="true" /><span>修改密码</span><van-icon class="app-more-chevron" name="arrow" aria-hidden="true" /></button>
        <button type="button" class="danger" :disabled="loggingOut" @click="onLogout"><van-icon class="app-more-icon" name="revoke" aria-hidden="true" /><span>退出登录</span><van-icon class="app-more-chevron" name="arrow" aria-hidden="true" /></button>
      </div>
    </van-popup>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showFailToast } from 'vant'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { useKeyboardViewport } from '@/composables/useKeyboardViewport'
import { useUserStore } from '@/stores/user'
import { logout } from '@/utils/auth'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()
const { isCompactDesktop } = useResponsiveLayout()
const { keyboardOpen } = useKeyboardViewport()
const hideMobileNav = computed(() => route.meta.hideMobileNav === true)
const accountOpen = ref(false)
const moreOpen = ref(false)
const morePopup = ref(null)
const accountTrigger = ref(null)
const loggingOut = ref(false)
useOverlayFocus(moreOpen, morePopup, () => { moreOpen.value = false })
const closeAccount = () => { accountOpen.value = false; accountTrigger.value?.focus() }
watch(() => route.fullPath, () => { accountOpen.value = false; moreOpen.value = false })

const navigation = computed(() => [
  { key: 'leads', label: '线索工作台', shortLabel: '线索', icon: 'orders-o', to: '/leads', visible: true },
  { key: 'create', label: '新建线索', shortLabel: '新建', icon: 'plus', to: '/leads/new', visible: userStore.canCreateLead() },
  { key: 'appointments', label: '预约排班', shortLabel: '日程', icon: 'calendar-o', to: '/appointments', visible: userStore.isAppointmentEnabled() && !userStore.isDispatcher() },
  { key: 'pool', label: userStore.poolTitle(), icon: 'friends-o', to: '/public-pool', visible: userStore.isInviter() || userStore.isManager() || userStore.isAdmin() },
  { key: 'stats', label: '统计看板', icon: 'chart-trending-o', to: '/stats/sla', visible: userStore.isManager() || userStore.isAdmin() },
  { key: 'settings', label: '系统设置', icon: 'setting-o', to: '/admin/settings', visible: userStore.isAdmin() }
].filter(item => item.visible))

const navigationGroups = computed(() => [
  { label: '工作空间', items: navigation.value.filter(item => !['stats', 'settings'].includes(item.key)) },
  { label: '管理', items: navigation.value.filter(item => ['stats', 'settings'].includes(item.key)) }
].filter(group => group.items.length))
const mobileNavigation = computed(() => navigation.value.filter(item => ['leads', 'create', 'appointments'].includes(item.key)).slice(0, 3))
const mobileMoreNavigation = computed(() => navigation.value.filter(item => !mobileNavigation.value.some(primary => primary.key === item.key)))
const isNavigationActive = to => route.path === to || route.path.startsWith(`${to}/`)
const mobileMoreActive = computed(() => mobileMoreNavigation.value.some(item => isNavigationActive(item.to)))

const navigate = to => {
  moreOpen.value = false
  accountOpen.value = false
  if (route.path !== to) router.push(to)
}
const goToPassword = () => navigate('/account/password')
const onLogout = async () => {
  if (loggingOut.value) return
  loggingOut.value = true
  try {
    await showConfirmDialog({ title: '退出登录', message: '确认退出当前账号？' })
    await logout()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') showFailToast('退出失败，请检查网络后重试')
  } finally { loggingOut.value = false }
}
</script>

<style scoped>
.app-shell { min-height: 100vh; min-height: 100dvh; padding-left: var(--kx-shell-sidebar-width); background: var(--kx-color-canvas); color: var(--kx-color-text); }
.app-sidebar { position: fixed; z-index: 50; inset: 0 auto 0 0; display: flex; width: var(--kx-sidebar-width); flex-direction: column; border-right: 1px solid var(--kx-color-border); background: linear-gradient(160deg, var(--kx-color-nav), var(--kx-color-nav-end)); }
.app-brand { display: flex; min-height: 88px; align-items: center; gap: var(--kx-space-3); padding: 0 var(--kx-space-6); color: var(--kx-color-text); text-decoration: none; }
.app-brand > span { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; border-radius: var(--kx-radius-3); background: var(--kx-color-brand); color: var(--kx-color-on-brand); font-size: var(--kx-font-13); font-weight: 700; }
.app-brand strong { font-size: var(--kx-font-16); font-weight: 600; letter-spacing: -0.3px; }
.app-navigation-groups { min-height: 0; overflow-y: auto; padding-bottom: var(--kx-space-4); }
.app-nav-caption { padding: var(--kx-space-2) var(--kx-space-3); color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); font-weight: 500; }
.app-nav + .app-nav { margin-top: var(--kx-space-3); border-top: 1px solid var(--kx-color-border); }
.app-nav { display: grid; gap: var(--kx-space-1); padding: var(--kx-space-2) var(--kx-space-3); }
.app-nav a { display: flex; min-height: 44px; align-items: center; gap: var(--kx-space-3); padding: 0 var(--kx-space-3); border-radius: var(--kx-radius-3); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); font-weight: 500; text-decoration: none; transition: color var(--kx-motion-fast), background var(--kx-motion-fast); }
.app-nav a:hover { background: var(--kx-color-surface-subtle); color: var(--kx-color-text); }
.app-nav a.router-link-active { font-weight: 600; background: var(--kx-color-nav-selected); color: var(--kx-color-text); box-shadow: var(--kx-shadow-selected); }
.app-nav a.router-link-active .van-icon { color: var(--kx-color-brand); }
.app-nav .van-icon { width: 20px; font-size: 19px; text-align: center; }
.app-account { position: relative; margin-top: auto; padding: var(--kx-space-3); border-top: 1px solid var(--kx-color-border); }
.app-account-summary { display: flex; width: 100%; min-height: 48px; align-items: center; gap: var(--kx-space-2); padding: 0 var(--kx-space-2); border: 0; border-radius: var(--kx-radius-3); background: transparent; color: inherit; cursor: pointer; text-align: left; }
.app-account-summary:hover { background: var(--kx-color-surface-subtle); }
.app-avatar { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; border-radius: 50%; background: var(--kx-color-segment); color: var(--kx-color-text-secondary); border: 1px solid var(--kx-color-border-strong); font-weight: 500; }
.app-account-copy { display: grid; min-width: 0; flex: 1; }
.app-account-copy strong, .app-account-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.app-account-copy strong { font-size: var(--kx-font-13); }
.app-account-copy small { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); }
.app-account-menu { position: absolute; right: var(--kx-space-3); bottom: calc(100% - 4px); left: var(--kx-space-3); overflow: hidden; padding: var(--kx-space-1); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); }
.app-account-menu button { display: flex; width: 100%; min-height: 40px; align-items: center; gap: var(--kx-space-2); padding: 0 var(--kx-space-3); border: 0; border-radius: var(--kx-radius-2); background: transparent; color: var(--kx-color-text-secondary); cursor: pointer; }
.app-account-menu button:hover { background: var(--kx-color-surface-subtle); }
.app-account-menu .danger { color: var(--kx-color-danger); }
.app-content { min-width: 0; min-height: 100vh; min-height: 100dvh; }
.app-mobile-nav { display: none; }
.app-more-popup { padding: var(--kx-space-4) var(--kx-space-4) calc(var(--kx-space-6) + env(safe-area-inset-bottom)); }
.app-more-header { display: flex; align-items: center; gap: var(--kx-space-3); padding-bottom: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); }
.app-more-header > div { display: grid; min-width: 0; flex: 1; }
.app-more-header small { color: var(--kx-color-text-muted); }
.app-more-header > button { width: 44px; height: 44px; border: 0; background: transparent; color: var(--kx-color-text-muted); }
.app-more-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--kx-space-2); padding-top: var(--kx-space-4); }
.app-more-grid button { display: grid; min-height: 72px; place-items: center; gap: var(--kx-space-1); padding: var(--kx-space-2); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); }
.app-more-grid button .van-icon { font-size: 20px; }
.app-more-grid .danger { color: var(--kx-color-danger); }

@media (min-width: 768px) and (max-width: 1023px) {
  .app-shell { padding-left: var(--kx-sidebar-compact-width); }
  .app-sidebar { width: var(--kx-sidebar-compact-width); }
  .app-brand { justify-content: center; padding: 0; }
  .app-nav-caption { display: none; }
  .app-brand strong, .app-nav a span, .app-account-copy, .app-account-summary > .van-icon { display: none; }
  .app-nav { padding: var(--kx-space-4) var(--kx-space-2); }
  .app-nav a { justify-content: center; padding: 0; }
  .app-account { padding: var(--kx-space-3) var(--kx-space-2); }
  .app-account-summary { justify-content: center; padding: 0; }
  .app-account-menu { width: 176px; right: auto; bottom: var(--kx-space-3); left: calc(100% + var(--kx-space-2)); }
}

@media (max-width: 767px) {
  .app-shell { padding: 0 0 calc(var(--kx-shell-mobile-nav-height) + env(safe-area-inset-bottom)); }
  .app-shell--task { --kx-shell-mobile-nav-height: 0px; padding-bottom: 0; }
  .app-sidebar { display: none; }
  .app-mobile-nav { position: fixed; z-index: 45; right: 0; bottom: 0; left: 0; display: flex; min-height: var(--kx-mobile-nav-height); padding: 4px 8px env(safe-area-inset-bottom); border-top: 1px solid var(--kx-color-mobile-separator); background: var(--kx-color-toolbar); }
  .app-mobile-nav a, .app-mobile-nav button { display: grid; min-width: 0; min-height: calc(var(--kx-mobile-nav-height) - 5px); flex: 1; place-items: center; align-content: center; gap: 2px; padding: 3px 0 5px; border: 0; background: transparent; color: var(--kx-color-text-secondary); font-size: 11px; font-weight: 500; line-height: 1.2; text-decoration: none; }
  .app-mobile-nav a.router-link-active, .app-mobile-nav button.is-current { color: var(--kx-color-brand); font-weight: 600; }
  .app-mobile-nav .van-icon { display: grid; width: 44px; height: 28px; place-items: center; border-radius: 12px; font-size: 21px; transition: background var(--kx-motion-fast), color var(--kx-motion-fast); }
  .app-mobile-nav a.router-link-active .van-icon, .app-mobile-nav button.is-current .van-icon { background: var(--kx-color-brand-soft); }
  .app-mobile-nav a:active .van-icon, .app-mobile-nav button:active .van-icon { background: var(--kx-color-mobile-fill); }
  .app-more-popup { max-height: 88dvh; overflow-y: auto; padding: 20px 16px calc(24px + env(safe-area-inset-bottom)); background: var(--kx-color-canvas); }
  .app-more-header { padding: 0 4px 20px; border: 0; }
  .app-more-header .app-avatar { width: 44px; height: 44px; flex-basis: 44px; border: 0; background: var(--kx-color-surface); font-size: var(--kx-mobile-heading-size); }
  .app-more-header strong { font-size: var(--kx-mobile-heading-size); font-weight: 600; }
  .app-more-header small { margin-top: 2px; font-size: 12px; overflow-wrap: anywhere; }
  .app-more-header > button { display: grid; flex: 0 0 44px; place-items: center; border-radius: 50%; background: var(--kx-color-mobile-fill); }
  .app-more-grid { display: flex; flex-direction: column; gap: 0; overflow: hidden; padding: 0; border-radius: 16px; background: var(--kx-color-surface); }
  .app-more-grid + .app-more-grid { margin-top: 16px; }
  .app-more-grid button { position: relative; display: flex; width: 100%; min-height: 54px; align-items: center; gap: 12px; padding: 8px 16px; border: 0; border-radius: 0; background: transparent; color: var(--kx-color-text); text-align: left; }
  .app-more-grid button + button::before { content: ''; position: absolute; top: 0; right: 0; left: 56px; height: 1px; background: var(--kx-color-mobile-separator); }
  .app-more-grid button > span { flex: 1; min-width: 0; font-size: 16px; overflow-wrap: anywhere; }
  .app-more-grid button .app-more-icon { display: grid; width: 28px; height: 28px; flex: 0 0 28px; place-items: center; color: var(--kx-color-text-secondary); font-size: 20px; }
  .app-more-grid button .app-more-chevron { flex: 0 0 auto; color: var(--kx-color-text-muted); font-size: 14px; }
  .app-more-grid button.is-current, .app-more-grid button.is-current .app-more-icon { color: var(--kx-color-brand); }
  .app-more-grid button.is-current { background: var(--kx-color-brand-soft); }
  .app-more-grid button:active { background: var(--kx-color-mobile-fill); }
  .app-more-grid .danger, .app-more-grid .danger .app-more-icon { color: var(--kx-color-danger); }
}
@supports ((backdrop-filter: blur(12px)) or (-webkit-backdrop-filter: blur(12px))) {
  .app-mobile-nav { background: var(--kx-white-translucent); -webkit-backdrop-filter: blur(12px); backdrop-filter: blur(12px); }
}
@media (prefers-reduced-transparency: reduce) {
  .app-mobile-nav { background: var(--kx-color-toolbar); -webkit-backdrop-filter: none; backdrop-filter: none; }
}
</style>
