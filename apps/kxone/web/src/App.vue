<template>
  <div id="app">
    <router-view v-if="ready" v-slot="{ Component }">
      <component :is="Component" v-if="standaloneRoute" :key="viewKey" />
      <AppShell v-else><component :is="Component" :key="viewKey" /></AppShell>
    </router-view>
    <CrmStatePanel v-else :state="initError ? 'error' : 'loading'" :title="initError || '正在加载工作台'" :action-label="initError ? '重新加载' : ''" @action="initialize" />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import AppShell from '@/components/AppShell.vue'
import { useUserStore } from '@/stores/user'
import { routeViewKey } from '@/utils/routeViewKey'

const userStore = useUserStore()
const route = useRoute()
const viewKey = computed(() => routeViewKey(route))
const ready = ref(false)
const standaloneRoute = computed(() => route.meta.public === true || route.meta.standalone === true)

const isPublicPage = () => {
  return window.location.pathname === '/login'
}

const initError = ref('')
const initialize = async () => {
  initError.value = ''
  if (isPublicPage()) {
    ready.value = true
    return
  }
  if (userStore.userInfo) {
    ready.value = true
    return
  }
  try {
    await userStore.fetchUserInfo()
    ready.value = true
  } catch {
    initError.value = '登录状态加载失败，请检查网络后重试'
  }
}
onMounted(initialize)
</script>

<style>
#app {
  width: 100%;
  min-height: 100vh;
  min-height: 100dvh;
  background-color: var(--kx-color-canvas);
}
</style>
