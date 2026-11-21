import { defineStore } from 'pinia'
import { ref } from 'vue'
import api from '@/api'

export const useUserStore = defineStore('user', () => {
  const userInfo = ref(null)
  const role = ref('')

  const fetchUserInfo = async () => {
    try {
      const data = await api.get('/user/info')
      userInfo.value = data
      role.value = data.role
      return data
    } catch (error) {
      console.error('获取用户信息失败:', error)
      throw error
    }
  }

  const isManager = () => role.value === 'manager' || role.value === 'admin'
  const isSales = () => role.value === 'sales' || role.value === 'inviter'
  const isInviter = () => role.value === 'sales' || role.value === 'inviter'
  const isCloser = () => role.value === 'closer'
  const isAdmin = () => role.value === 'admin'
  const isDispatcher = () => role.value === 'dispatcher'
  const canCreateLead = () => ['sales', 'inviter', 'closer', 'manager', 'admin', 'dispatcher'].includes(role.value)
  const canAssignLead = () => isManager() || isDispatcher()
  const isWorkflowV2Enabled = () => userInfo.value?.features?.workflow_v2_enabled === true
  const isAppointmentEnabled = () => userInfo.value?.features?.appointment_enabled === true
  const isPoolEnabled = () => userInfo.value?.features?.pool_enabled === true
  const isPoolAutoReturnEnabled = () => userInfo.value?.features?.pool_auto_return_enabled === true
  const poolTitle = () => isWorkflowV2Enabled() ? '邀约公海' : '销售公海'
	const displayName = () => userInfo.value?.employee_name || userInfo.value?.display_name || userInfo.value?.username || userInfo.value?.account_id || '当前用户'
	const accountName = () => userInfo.value?.username || userInfo.value?.account_id || '-'
	const roleName = () => ({
		admin: '管理员',
		manager: '主管',
		dispatcher: '线索调度员',
		inviter: '邀约',
		sales: '销售',
		closer: '谈单'
	}[role.value] || role.value || '用户')

  return {
    userInfo,
    role,
    fetchUserInfo,
    isManager,
    isSales,
    isInviter,
    isCloser,
    isAdmin,
    isDispatcher,
    canCreateLead,
    canAssignLead,
    isWorkflowV2Enabled,
    isAppointmentEnabled,
    isPoolEnabled,
    isPoolAutoReturnEnabled,
		poolTitle,
		displayName,
		accountName,
		roleName
  }
})
