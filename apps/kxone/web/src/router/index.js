import { createRouter, createWebHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'

const routes = [
  {
    path: '/',
    redirect: '/leads'
  },
  {
    path: '/leads',
    name: 'LeadList',
    component: () => import('@/views/LeadList.vue'),
    meta: { title: '我的线索', dispatcherAccess: true }
  },
  {
    path: '/leads/new',
    name: 'CreateLead',
    component: () => import('@/views/CreateLead.vue'),
    meta: { title: '新建线索', hideMobileNav: true, dispatcherAccess: true, roles: ['sales', 'inviter', 'closer', 'manager', 'admin', 'dispatcher'] }
  },
  {
    path: '/leads/:id',
    name: 'LeadDetail',
    component: () => import('@/views/LeadDetail.vue'),
    meta: { title: '线索详情', hideMobileNav: true, dispatcherAccess: true }
  },
  {
    path: '/leads/:id/followup',
    name: 'AddFollowup',
    component: () => import('@/views/AddFollowup.vue'),
    meta: { title: '添加跟进', hideMobileNav: true, roles: ['sales', 'inviter', 'closer', 'manager', 'admin'] }
  },
  {
    path: '/public-pool',
    name: 'PublicPool',
    component: () => import('@/views/PublicPool.vue'),
    meta: { title: '公海', roles: ['sales', 'inviter', 'manager', 'admin'] }
  },
  {
    path: '/appointments',
    name: 'AppointmentSchedule',
    component: () => import('@/views/AppointmentSchedule.vue'),
    meta: { title: '预约排班', roles: ['sales', 'inviter', 'closer', 'manager', 'admin'] }
  },
  {
    path: '/leads/:id/appointment',
    name: 'CreateAppointment',
    component: () => import('@/views/CreateAppointment.vue'),
    meta: { title: '创建预约', hideMobileNav: true, roles: ['sales', 'inviter', 'manager', 'admin'] }
  },
  {
    path: '/admin/settings',
    name: 'AdminSettings',
    component: () => import('@/views/AdminSettings.vue'),
    meta: { title: '后台配置', roles: ['admin'] }
  },
  {
    path: '/account/password',
    name: 'ChangePassword',
    component: () => import('@/views/ChangePassword.vue'),
    meta: { title: '修改密码', hideMobileNav: true, dispatcherAccess: true }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '登录 CRM', public: true }
  },
  {
    path: '/stats/sla',
    name: 'SLAStats',
    component: () => import('@/views/SLAStats.vue'),
    meta: { title: '统计看板', roles: ['manager', 'admin'] }
  },
  {
    path: '/agent/:conversationId?',
    redirect: '/stats/sla'
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFound.vue'),
    meta: { title: '页面不存在', dispatcherAccess: true, standalone: true }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to) => {
  document.title = to.meta.title || 'KXOne'
  if (to.meta.public) return true

  const userStore = useUserStore()
  if (!userStore.userInfo) {
    try {
      await userStore.fetchUserInfo()
    } catch {
      return {
        name: 'Login',
        query: { redirect: to.fullPath }
      }
    }
  }

  if (userStore.isDispatcher() && !to.meta.dispatcherAccess) {
    return { name: 'LeadList' }
  }
  if (Array.isArray(to.meta.roles) && !to.meta.roles.includes(userStore.role)) {
    return { name: 'LeadList' }
  }
  return true
})

export default router
