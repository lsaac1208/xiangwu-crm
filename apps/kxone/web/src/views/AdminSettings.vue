<template>
  <div class="admin-page">
    <van-nav-bar v-if="mobileLayout" left-text="返回" left-arrow @click-left="router.back()" />
    <div class="admin-shell">
      <header>
        <div><h1>系统设置</h1></div>
        <p>管理平台接入、线索群提醒、登录账号与业务规则</p>
      </header>

      <div class="settings-layout">
        <aside class="settings-section-nav" aria-label="设置分类">
          <button v-for="item in sectionOptions" :key="item.name" type="button" :class="{ active: activeTab === item.name }" :aria-current="activeTab === item.name ? 'page' : undefined" @click="changeSection(item.name)"><van-icon aria-hidden="true" :name="item.icon" /><span><strong>{{ item.title }}</strong><small>{{ item.description }}</small></span></button>
        </aside>
        <main class="settings-content">
          <CrmStatePanel v-if="['notification', 'rules'].includes(activeTab) && !settingsLoaded && !settingsError" state="loading" title="正在加载设置" />
          <label class="settings-mobile-select"><span>设置分类</span><select :value="activeTab" @change="onMobileSectionChange"><option v-for="item in sectionOptions" :key="item.name" :value="item.name">{{ item.title }}</option></select></label>
        <AccessAuditPanel v-if="activeTab === 'access'" />
 <AgentModelSettings v-else-if="activeTab === 'ai'" @dirty="aiDirty = $event" />
        <template v-else-if="activeTab === 'notification'">
          <CrmStatePanel v-if="settingsError" state="error" title="设置加载失败" action-label="重试" @action="loadSettings" />
          <section v-if="settingsLoaded && !settingsError" class="panel notification-panel">
            <div class="panel-title">
              <div>
                <h2>企业微信群机器人</h2>
                <p class="hint">四类通知共用同一个 Webhook，可分别启停。关闭通知不会停止回访计时或自动回公海规则。</p>
              </div>
              <van-tag :type="notification.configured ? 'success' : 'warning'">Webhook {{ notification.configured ? '已配置' : '未配置' }}</van-tag>
            </div>
            <GroupRobotHealth />
            <van-form @submit="saveNotification">
              <van-cell-group inset class="notification-switches">
                <van-cell title="启用真实 @" label="开启后消息会使用企业微信 UserID 真实提醒负责人；关闭仍发送普通通知" center>
                  <template #right-icon><van-switch aria-label="启用真实 @" v-model="notification.mentions" /></template>
                </van-cell>
                <van-cell title="新线索通知" label="广告平台、表单和 CRM 人工新建线索" center>
                  <template #right-icon><van-switch aria-label="新线索通知" v-model="notification.newLead" /></template>
                </van-cell>
                <van-cell title="派发与领取提醒" label="待派发、待领取超时提醒" center>
                  <template #right-icon><van-switch aria-label="派发与领取提醒" v-model="notification.dispatchClaim" /></template>
                </van-cell>
                <van-cell title="回访提醒" label="首次回访、星级回访及逾期提醒" center>
                  <template #right-icon><van-switch aria-label="回访提醒" v-model="notification.followup" /></template>
                </van-cell>
                <van-cell title="自动回公海通知" label="仅通知规则触发的自动回公海" center>
                  <template #right-icon><van-switch aria-label="自动回公海通知" v-model="notification.pool" /></template>
                </van-cell>
                <van-field
                  v-model.trim="notification.webhook"
                  label="Webhook"
                  type="textarea"
                  rows="3"
                  :placeholder="notification.configured ? '已配置，留空保留现有 Webhook' : 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'"
                  clearable
                />
              </van-cell-group>
              <p class="form-tip">开启任一通知前必须配置有效的企业微信群机器人 Webhook。关闭期间到期的历史消息不会在重新开启后补发。</p>
              <div class="notification-actions">
                <van-button plain type="primary" native-type="button" :loading="testingRobot" :disabled="!notification.configured" @click="testGroupRobot">发送配置测试消息</van-button>
                <van-button type="primary" native-type="submit" :loading="saving">保存提醒配置</van-button>
              </div>
            </van-form>
          </section>
        </template>

        <template v-else-if="activeTab === 'channels'">
          <CrmStatePanel v-if="channelsError" state="error" title="设置加载失败" action-label="重试" @action="loadChannelIntegrations()" />
          <section class="panel">
            <div class="panel-title">
              <div>
                <h2>广告平台账户</h2>
                <p class="hint">接收百度营销、腾讯营销、小红书和抖音线索，统一进入 CRM。</p>
              </div>
              <van-button size="small" type="primary" @click="openCreateChannel">新增账户</van-button>
            </div>
            <div class="channel-grid">
              <article v-for="item in channelIntegrations" :key="item.id" class="channel-card">
                <div class="channel-heading">
                  <div><strong>{{ platformText(item.platform) }}</strong><span>{{ item.account_name }}</span></div>
                  <van-tag :type="item.enabled ? 'success' : 'default'">{{ item.enabled ? '启用' : '停用' }}</van-tag>
                </div>
                <div class="callback-row">
                  <code :title="callbackURL(item)">{{ callbackURL(item) }}</code>
                  <button type="button" class="copy-button" :aria-label="`复制${item.account_name}的回调地址`" @click="copyCallback(item)">复制</button>
                </div>
                <div class="channel-meta">
                  <span>POST · {{ authModeText(item.auth_mode) }}</span>
                  <span>{{ channelStatusText(item) }}</span>
                </div>
                <p v-if="item.platform === 'xiaohongshu' && !item.secret_configured" class="channel-warning" role="alert">未配置 Token，平台验签将无法通过</p>
                <div class="channel-actions">
                  <div class="channel-toggle">
                    <van-switch :model-value="item.enabled" size="20" :loading="togglingChannelID === item.id" :disabled="Boolean(togglingChannelID)" :aria-label="`${item.enabled ? '停用' : '启用'}${item.account_name}`" @update:model-value="toggleChannel(item, $event)" />
                    <span>{{ item.enabled ? '接收中' : '已停用' }}</span>
                  </div>
                  <div class="channel-action-buttons">
                    <van-button size="small" plain type="primary" @click="openEditChannel(item)">编辑</van-button>
                    <van-button size="small" plain type="danger" :loading="deletingChannelID === item.id" @click="removeChannel(item)">删除</van-button>
                  </div>
                </div>
              </article>
            </div>
            <van-empty v-if="!loadingChannels && channelIntegrations.length === 0" description="暂无平台账户" />
          </section>
        </template>

        <template v-else-if="activeTab === 'users'">
          <CrmStatePanel v-if="usersError" state="error" title="设置加载失败" action-label="重试" @action="loadUsers()" />
          <section class="panel user-panel">
            <div class="panel-title user-panel-title">
				<div>
					<h2>用户与权限</h2>
					<p class="hint">集中管理登录账号、员工姓名和 CRM 角色。</p>
				</div>
				<div class="user-panel-actions">
					<span class="user-count"><strong>{{ userTotal }}</strong> 个账号</span>
					<van-button size="small" type="primary" icon="plus" aria-label="新增用户" @click="openCreate">新增用户</van-button>
				</div>
			</div>
			<div class="user-filters" aria-label="用户筛选">
				<van-search v-model="userSearch" placeholder="搜索姓名或登录账号" />
				<select v-model="userRoleFilter" aria-label="按角色筛选"><option value="">全部角色</option><option v-for="item in roles" :key="item.value" :value="item.value">{{ item.label }}</option></select>
				<select v-model="userStatusFilter" aria-label="按账号状态筛选"><option value="">全部状态</option><option value="active">已启用</option><option value="disabled">已停用</option></select>
			</div>
			<div v-if="users.length" class="user-table" role="group" aria-label="CRM 用户列表">
				<div class="user-table-head" aria-hidden="true">
					<span>用户</span><span>角色</span><span>账号状态</span><span>群机器人 @</span><span>操作</span>
				</div>
				<button v-for="item in users" :key="item.id" class="user-row" type="button" :aria-label="`编辑用户 ${item.employee_name || item.username}`" @click="openEdit(item)">
					<span class="user-identity">
						<span class="user-avatar">{{ userInitial(item) }}</span>
						<span class="user-copy"><strong>{{ item.employee_name || item.username }}</strong><small>@{{ item.username }}</small></span>
					</span>
					<span class="user-role"><i :class="`role-${item.role}`" />{{ roleText(item.role) }}</span>
					<span class="user-status" :class="item.status === 'active' ? 'active' : 'disabled'"><i />{{ item.status === 'active' ? '已启用' : '已停用' }}</span>
					<span class="mention-status" :class="mentionStatusClass(item)">{{ mentionStatusText(item) }}</span>
					<span class="user-edit">编辑 <van-icon aria-hidden="true" name="arrow" /></span>
				</button>
            </div>
			<div v-if="userTotalPages > 1" class="user-pagination"><button type="button" :disabled="userPage <= 1 || loadingUsers" @click="changeUserPage(userPage - 1)">上一页</button><span>第 {{ userPage }} / {{ userTotalPages }} 页</span><button type="button" :disabled="userPage >= userTotalPages || loadingUsers" @click="changeUserPage(userPage + 1)">下一页</button></div>
            <van-empty v-if="!loadingUsers && !usersError && users.length === 0" description="没有匹配的用户" />
          </section>
        </template>

        <template v-else-if="activeTab === 'recycle'">
          <section class="panel recycle-panel">
            <div class="panel-title">
              <div><h2>已删除线索</h2><p class="hint">删除记录保留在回收站，可由管理员恢复。待发送通知在删除时已取消，恢复后不会补发。</p></div>
              <van-button size="small" plain type="primary" :loading="loadingDeletedLeads" @click="loadDeletedLeads">刷新</van-button>
            </div>
            <CrmStatePanel v-if="deletedLeadsError" state="error" title="回收站加载失败" action-label="重试" @action="loadDeletedLeads" />
            <div v-else-if="deletedLeads.length" class="recycle-list">
              <article v-for="item in deletedLeads" :key="item.id" class="recycle-item">
                <div><strong>{{ item.customer_name || '未填写姓名' }}</strong><code>{{ item.lead_no }}</code><small>{{ item.source_channel || '未知来源' }} · {{ new Date(item.deleted_at).toLocaleString('zh-CN') }} 删除</small><small>操作人：{{ item.deleted_by || '-' }} · 原因：{{ item.reason || '-' }}</small></div>
                <van-button size="small" plain type="primary" :loading="restoringLeadID === item.id" @click="restoreLead(item)">恢复线索</van-button>
              </article>
            </div>
            <van-empty v-else-if="!loadingDeletedLeads" description="回收站暂无线索" />
            <div v-if="deletedLeadTotalPages > 1" class="user-pagination"><button type="button" :disabled="deletedLeadPage <= 1 || loadingDeletedLeads" @click="changeDeletedLeadPage(deletedLeadPage - 1)">上一页</button><span>第 {{ deletedLeadPage }} / {{ deletedLeadTotalPages }} 页</span><button type="button" :disabled="deletedLeadPage >= deletedLeadTotalPages || loadingDeletedLeads" @click="changeDeletedLeadPage(deletedLeadPage + 1)">下一页</button></div>
          </section>
        </template>

        <template v-else>
          <CrmStatePanel v-if="settingsError" state="error" title="设置加载失败" action-label="重试" @action="loadSettings" />
          <section v-if="settingsLoaded && !settingsError" class="panel rules-panel">
            <div class="panel-title">
              <div><h2>提醒、回访与公海规则</h2><p class="hint">规则保持运行；群消息是独立开关，在“线索提醒”中管理。</p></div>
            </div>
            <van-form class="rules-form" @submit="saveRules">
              <section class="rule-section">
                <div class="rule-section-heading"><strong>工作流与公海</strong><span>功能开关</span></div>
                <van-cell-group inset class="rule-switches">
                  <van-cell title="启用双阶段线索流程" center>
                    <template #right-icon><van-switch aria-label="启用双阶段线索流程" v-model="rules.workflow" /></template>
                  </van-cell>
                  <van-cell title="启用预约排班" center>
                    <template #right-icon><van-switch aria-label="启用预约排班" v-model="rules.appointment" /></template>
                  </van-cell>
                  <van-cell title="启用公海" label="开启后允许线索进入公海；关闭后仍可领取已有公海线索" center>
                    <template #right-icon><van-switch aria-label="启用公海" v-model="rules.pool" @change="onPoolEnabledChange" /></template>
                  </van-cell>
                  <van-cell title="自动回公海" label="按未领取和回访逾期宽限规则自动释放" center>
                    <template #right-icon><van-switch aria-label="自动回公海" v-model="rules.poolAutoReturn" :disabled="!rules.pool" /></template>
                  </van-cell>
                </van-cell-group>
              </section>

              <section class="rule-section">
                <div class="rule-section-heading"><strong>派发与领取提醒</strong><span>多个时间用英文逗号分隔</span></div>
                <van-cell-group inset class="rule-fields two-columns">
                  <van-field v-model.trim="rules.dispatchMinutes" label="待派发提醒" placeholder="3,5,10" :rules="csvRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                  <van-field v-model.trim="rules.claimMinutes" label="待领取提醒" placeholder="3,5,10" :rules="csvRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                </van-cell-group>
              </section>

              <section class="rule-section">
                <div class="rule-section-heading"><strong>回访规则</strong><span>领取后计算首次回访，后续按星级计算</span></div>
                <van-cell-group inset class="rule-fields three-columns">
                  <van-field v-model="rules.firstFollowup" type="digit" label="首次回访" :rules="numberRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                  <van-field v-model.trim="rules.followupReminder" label="逾期提醒偏移" placeholder="0,60,240" :rules="csvRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                  <van-field v-model="rules.star5Hours" type="digit" label="5 星回访" :rules="numberRules"><template #extra><span class="field-unit">小时</span></template></van-field>
                  <van-field v-model="rules.star4Hours" type="digit" label="4 星回访" :rules="numberRules"><template #extra><span class="field-unit">小时</span></template></van-field>
                  <van-field v-model="rules.star3Hours" type="digit" label="3 星回访" :rules="numberRules"><template #extra><span class="field-unit">小时</span></template></van-field>
                  <van-field v-model="rules.star2Hours" type="digit" label="2 星回访" :rules="numberRules"><template #extra><span class="field-unit">小时</span></template></van-field>
                  <van-field v-model="rules.star1Hours" type="digit" label="1 星回访" :rules="numberRules"><template #extra><span class="field-unit">小时</span></template></van-field>
                </van-cell-group>
              </section>

              <section class="rule-section">
                <div class="rule-section-heading"><strong>公海规则</strong><span>自动回公海仅在上方开关开启时生效</span></div>
                <van-cell-group inset class="rule-fields two-columns">
                  <van-field v-model="rules.poolMaxActive" type="digit" label="私有线索上限" :rules="numberRules"><template #extra><span class="field-unit">条</span></template></van-field>
                  <van-field v-model="rules.poolCooldown" type="digit" label="原负责人冷却" :rules="numberRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                  <van-field v-model="rules.poolUnclaimed" type="digit" label="未领取宽限" :rules="numberRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                  <van-field v-model="rules.poolFollowupGrace" type="digit" label="回访逾期宽限" :rules="numberRules"><template #extra><span class="field-unit">分钟</span></template></van-field>
                </van-cell-group>
              </section>
              <div class="rules-submit"><van-button block type="primary" native-type="submit" :loading="saving">保存业务规则</van-button></div>
            </van-form>
          </section>
        </template>
        </main>
      </div>
    </div>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="userEditorRef" v-model:show="showEditor" :position="mobileLayout ? 'bottom' : 'center'" :aria-label="editingID ? '编辑用户' : '新增用户'" aria-modal="true" :close-on-click-overlay="!savingUser" round class="settings-editor-popup">
      <div class="editor">
        <div class="editor-heading"><h2>{{ editingID ? '编辑用户' : '新增用户' }}</h2><button type="button" :disabled="savingUser" @click="showEditor = false">关闭</button></div>
        <van-form class="crm-form settings-editor-form" @submit="saveUser">
          <van-cell-group inset>
            <van-field v-model.trim="userForm.username" label="登录账号" autocomplete="off" :rules="[{ required: true, message: '请输入登录账号' }]" />
            <van-field v-model="userForm.password" label="登录密码" type="password" autocomplete="new-password" :placeholder="editingID ? '留空表示不修改' : '8-72 位'" :rules="passwordRules" />
            <van-field v-model.trim="userForm.employee_name" label="姓名" />
            <van-field name="role" label="角色">
              <template #input><van-radio-group v-model="userForm.role" direction="horizontal"><van-radio v-for="item in roles" :key="item.value" :name="item.value">{{ item.label }}</van-radio></van-radio-group></template>
            </van-field>
            <van-field v-model="userForm.department_id" type="digit" label="部门 ID" />
            <van-field v-model.trim="userForm.group_robot_mention_userid" label="群机器人 @ UserID" placeholder="选填，由管理员维护" maxlength="128" />
            <van-cell title="启用账号" center>
              <template #right-icon><van-switch aria-label="启用账号" v-model="userForm.active" /></template>
            </van-cell>
          </van-cell-group>
          <p v-if="userSaveError" class="editor-error" role="alert">{{ userSaveError }}</p>
          <van-button block type="primary" native-type="submit" :loading="savingUser">保存用户</van-button>
        </van-form>
      </div>
    </van-popup>

    <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="channelEditorRef" v-model:show="showChannelEditor" :position="mobileLayout ? 'bottom' : 'center'" :aria-label="editingChannelID ? '编辑平台账户' : '新增平台账户'" aria-modal="true" :close-on-click-overlay="!savingChannel" round class="settings-editor-popup">
      <div class="editor">
        <div class="editor-heading"><h2>{{ editingChannelID ? '编辑平台账户' : '新增平台账户' }}</h2><button type="button" :disabled="savingChannel" @click="showChannelEditor = false">关闭</button></div>
        <van-form class="crm-form settings-editor-form" @submit="saveChannel">
          <van-cell-group inset>
            <van-field name="platform" label="平台">
              <template #input>
                <van-radio-group v-model="channelForm.platform" direction="horizontal" :disabled="Boolean(editingChannelID)">
                  <van-radio name="baidu">百度营销</van-radio>
                  <van-radio name="tencent">腾讯营销</van-radio>
                  <van-radio name="xiaohongshu">小红书</van-radio>
                  <van-radio name="douyin">抖音</van-radio>
                </van-radio-group>
              </template>
            </van-field>
            <van-field v-model.trim="channelForm.account_name" label="账户名称" maxlength="128" :rules="[{ required: true, message: '请输入广告账户名称' }]" />
            <van-field
              v-if="channelForm.platform === 'xiaohongshu' || channelForm.platform === 'douyin'"
              v-model.trim="channelForm.token"
              type="password"
              :label="channelForm.platform === 'douyin' ? '密钥' : 'Token'"
              autocomplete="new-password"
              :placeholder="channelTokenPlaceholder"
              :rules="[{ validator: validateChannelToken, message: '新建小红书账户必须填写 Token' }]"
            />
            <van-cell title="启用接收" center><template #right-icon><van-switch aria-label="启用接收" v-model="channelForm.enabled" /></template></van-cell>
            <van-cell title="请求方式" value="POST" />
            <van-cell title="认证方式" :value="authModeText(channelFormAuthMode)" />
          </van-cell-group>
          <van-button block type="primary" native-type="submit" :loading="savingChannel">保存平台账户</van-button>
        </van-form>
      </div>
    </van-popup>
  </div>
</template>

<script setup>
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import AgentModelSettings from '@/components/AgentModelSettings.vue'
import AccessAuditPanel from '@/components/AccessAuditPanel.vue'
import { computed, h, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import GroupRobotHealth from '@/components/GroupRobotHealth.vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showFailToast, showSuccessToast } from 'vant'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import {
  createChannelIntegration,
  createUser,
  deleteChannelIntegration,
  getAdminSettings,
  getChannelIntegrations,
  getDeletedLeads,
  getUsers,
  updateAdminSettings,
  sendGroupRobotTest,
  restoreDeletedLead,
  updateChannelIntegration,
  updateUser
} from '@/api/admin'

const confirmSettingsAction = options => showConfirmDialog({
  ...options,
  message: () => h('span', [h('span', { id: options.title, class: 'sr-only' }, options.title), options.message])
})
const router = useRouter()
const route = useRoute()
const { isMobile: mobileLayout } = useResponsiveLayout()
const sectionOptions = [
  { name: 'access', title: '资料访问记录', description: '查看访问与读取记录', icon: 'records-o' },
  { name: 'notification', title: '群机器人与通知', description: '消息开关与真实 @', icon: 'chat-o' },
  { name: 'channels', title: '平台接入', description: '广告账户与回调', icon: 'exchange' },
  { name: 'users', title: '用户与权限', description: '账号、角色与真实 @', icon: 'contact-o' },
  { name: 'recycle', title: '线索回收站', description: '已删除线索与恢复', icon: 'delete-o' },
  { name: 'rules', title: '业务与公海规则', description: '流程、回访与宽限', icon: 'todo-list-o' },
  { name: 'ai', title: 'AI 模型与预算', description: '服务地址、密钥与模型', icon: 'chat-o' }
]
const requestedSection = Array.isArray(route.query.section) ? route.query.section[0] : route.query.section
const activeTab = ref(sectionOptions.some(item => item.name === requestedSection) ? requestedSection : 'notification')
const saving = ref(false)
const aiDirty = ref(false)
const testingRobot = ref(false)
const loadingUsers = ref(false)
const savingUser = ref(false)
const showEditor = ref(false)
const userEditorRef = ref(null)
const editingID = ref(null)
const users = ref([])
const userPage = ref(1)
const userPageSize = 20
const userTotal = ref(0)
const userTotalPages = ref(0)
const userSearch = ref('')
const userRoleFilter = ref('')
const userStatusFilter = ref('')
const deletedLeads = ref([])
const deletedLeadPage = ref(1)
const deletedLeadTotalPages = ref(0)
const loadingDeletedLeads = ref(false)
const deletedLeadsError = ref(false)
const restoringLeadID = ref(null)
const channelIntegrations = ref([])
const channelPublicBaseURL = ref('https://leads.whxiangwu.com')
const loadingChannels = ref(false)
const showChannelEditor = ref(false)
const channelEditorRef = ref(null)
const editingChannelID = ref(null)
const editingChannelSecretConfigured = ref(false)
const savingChannel = ref(false)
const deletingChannelID = ref(null)
const togglingChannelID = ref(null)
const notification = reactive({
  mentions: false,
  newLead: false,
  dispatchClaim: false,
  followup: false,
  pool: false,
  webhook: '',
  configured: false
})

useOverlayFocus(showEditor, userEditorRef, () => {
  if (!savingUser.value) showEditor.value = false
})
useOverlayFocus(showChannelEditor, channelEditorRef, () => {
  if (!savingChannel.value) showChannelEditor.value = false
})
const rules = reactive({
  workflow: false,
  appointment: false,
  pool: false,
  poolAutoReturn: false,
  dispatchMinutes: '3,5,10',
  claimMinutes: '3,5,10',
  firstFollowup: '120',
  followupReminder: '0,60,240',
  star1Hours: '168',
  star2Hours: '120',
  star3Hours: '72',
  star4Hours: '48',
  star5Hours: '24',
  poolMaxActive: '100',
  poolCooldown: '1440',
  poolUnclaimed: '1440',
  poolFollowupGrace: '240'
})
const userSaveError = ref('')
const userForm = reactive({ expected_version: 0, username: '', password: '', employee_name: '', role: 'inviter', department_id: '', group_robot_mention_userid: '', active: true })
const channelForm = reactive({ platform: 'baidu', account_name: '', token: '', enabled: true })
const roles = [
  { value: 'inviter', label: '邀约' },
  { value: 'closer', label: '谈单' },
  { value: 'dispatcher', label: '线索调度员' },
  { value: 'manager', label: '主管' },
  { value: 'admin', label: '管理员' }
]
const notificationSnapshot = ref('')
const rulesSnapshot = ref('')

const serializeNotification = () => JSON.stringify({
  mentions: notification.mentions,
  newLead: notification.newLead,
  dispatchClaim: notification.dispatchClaim,
  followup: notification.followup,
  pool: notification.pool,
  webhook: notification.webhook.trim()
})
const serializeRules = () => JSON.stringify({ ...rules })
const sectionDirty = computed(() => (
  activeTab.value === 'notification' && notificationSnapshot.value && serializeNotification() !== notificationSnapshot.value
) || (
  activeTab.value === 'rules' && rulesSnapshot.value && serializeRules() !== rulesSnapshot.value
) || (activeTab.value === 'ai' && aiDirty.value))

const changeSection = async name => {
  if (name === activeTab.value || !sectionOptions.some(item => item.name === name)) return
  if (sectionDirty.value) {
    try {
      await confirmSettingsAction({ title: '放弃未保存修改？', message: '当前分类的修改尚未保存，切换后将恢复上次保存的内容。', confirmButtonText: '放弃并切换' })
      await loadSettings()
    } catch {
      return
    }
  }
  activeTab.value = name
  aiDirty.value = false
  router.replace({ query: { ...route.query, section: name } })
  if (name === 'recycle') await loadDeletedLeads()
}

const loadDeletedLeads = async () => {
  loadingDeletedLeads.value = true
  deletedLeadsError.value = false
  try {
    const data = await getDeletedLeads({ page: deletedLeadPage.value, page_size: 20 })
    deletedLeads.value = data.items || []
    deletedLeadTotalPages.value = Math.max(1, Math.ceil(Number(data.total || 0) / 20))
  } catch {
    deletedLeadsError.value = true
  } finally {
    loadingDeletedLeads.value = false
  }
}

const changeDeletedLeadPage = async page => {
  deletedLeadPage.value = page
  await loadDeletedLeads()
}

const restoreLead = async item => {
  try {
    const title = '恢复线索'
    await showConfirmDialog({
      title,
      // The current Vant dialog uses the title text as its label target.
      message: () => h('span', [h('span', { id: title, class: 'sr-only' }, title), `确认恢复 ${item.lead_no}？恢复后会重新出现在 CRM 列表，已取消的通知不会补发。`]),
      confirmButtonText: '确认恢复'
    })
  } catch { return }
  restoringLeadID.value = item.id
  try {
    await restoreDeletedLead(item.id, '管理员从回收站恢复')
    showSuccessToast('线索已恢复')
    await loadDeletedLeads()
  } finally {
    restoringLeadID.value = null
  }
}
const onMobileSectionChange = async event => {
  const previous = activeTab.value
  await changeSection(event.target.value)
  event.target.value = activeTab.value || previous
}

const setting = (key, value, type, group, description, sensitive = false) => ({ key, value: String(value), value_type: type, group, description, is_sensitive: sensitive })
const asBool = value => value === true || value === 'true'
const normalizeCSV = value => String(value || '').replace(/，/g, ',').split(',').map(item => item.trim()).filter(Boolean).join(',')
const validateCSV = value => {
  const normalized = normalizeCSV(value)
  if (!normalized) return true
  return normalized.split(',').every(item => /^\d+$/.test(item))
}
const validateRuleNumber = value => /^\d+$/.test(String(value || '').trim())
const validateUserPassword = value => (Boolean(editingID.value) && !value) || (value?.length >= 8 && value?.length <= 72)
const csvRules = [{ validator: validateCSV, message: '请输入用逗号分隔的非负整数' }]
const numberRules = [{ validator: validateRuleNumber, message: '请输入非负整数' }]
const passwordRules = [{ validator: validateUserPassword, message: '登录密码长度必须为 8-72 位' }]
const notificationEnabled = () => notification.newLead || notification.dispatchClaim || notification.followup || notification.pool
const validateWebhook = value => {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && url.hostname === 'qyapi.weixin.qq.com' && url.pathname === '/cgi-bin/webhook/send' && Boolean(url.searchParams.get('key'))
  } catch {
    return false
  }
}

const settingsError = ref(false)
const usersError = ref(false)
const channelsError = ref(false)
const settingsLoaded = ref(false)
const loadSettings = async () => {
  settingsError.value = false
  try {
  const data = await getAdminSettings()
  const map = Object.fromEntries((data.settings || []).map(item => [item.key, item.value]))
  notification.newLead = asBool(map['notification.group_robot_enabled'])
  notification.mentions = asBool(map['notification.group_robot_mentions_enabled'])
  notification.dispatchClaim = asBool(map['notification.group_robot_dispatch_claim_enabled'])
  notification.followup = asBool(map['notification.group_robot_followup_enabled'])
  notification.pool = asBool(map['notification.group_robot_pool_enabled'])
  notification.configured = map['notification.group_robot_webhook_url'] === '******'
  notification.webhook = ''
  rules.workflow = asBool(map['workflow.v2_enabled'])
  rules.appointment = asBool(map['appointment.enabled'])
  rules.pool = asBool(map['pool.enabled'])
  rules.poolAutoReturn = asBool(map['pool.auto_return_enabled'])
  rules.dispatchMinutes = map['reminder.dispatch_minutes'] ?? '3,5,10'
  rules.claimMinutes = map['reminder.claim_minutes'] ?? '3,5,10'
  rules.firstFollowup = map['followup.first_due_minutes'] || '120'
  rules.followupReminder = map['followup.reminder_minutes'] ?? '0,60,240'
  rules.star1Hours = map['followup.star_1_hours'] || '168'
  rules.star2Hours = map['followup.star_2_hours'] || '120'
  rules.star3Hours = map['followup.star_3_hours'] || '72'
  rules.star4Hours = map['followup.star_4_hours'] || '48'
  rules.star5Hours = map['followup.star_5_hours'] || '24'
  rules.poolMaxActive = map['pool.max_active_leads'] || '100'
  rules.poolCooldown = map['pool.cooldown_minutes'] || '1440'
  rules.poolUnclaimed = map['pool.unclaimed_minutes'] || '1440'
  rules.poolFollowupGrace = map['pool.followup_grace_minutes'] || '240'
  notificationSnapshot.value = serializeNotification()
  rulesSnapshot.value = serializeRules()
  settingsLoaded.value = true
  } catch { settingsError.value = true }
}

let usersRequest = 0
const loadUsers = async (reset = false) => {
  const request = ++usersRequest
  usersError.value = false
	if (reset) userPage.value = 1
  loadingUsers.value = true
  try {
	const data = await getUsers({ keyword: userSearch.value.trim(), role: userRoleFilter.value, status: userStatusFilter.value, page: userPage.value, page_size: userPageSize })
	if (request !== usersRequest) return
    users.value = data.bindings || []; userTotal.value = Number(data.total || 0); userTotalPages.value = Number(data.total_pages || 0)
  } catch { if (request === usersRequest) usersError.value = true } finally { if (request === usersRequest) loadingUsers.value = false }
}
const changeUserPage = page => { userPage.value = page; loadUsers() }
let userSearchTimer
watch([userSearch, userRoleFilter, userStatusFilter], () => { clearTimeout(userSearchTimer); userSearchTimer = setTimeout(() => loadUsers(true), 300) })

const loadChannelIntegrations = async () => {
  channelsError.value = false
  loadingChannels.value = true
  try {
    const data = await getChannelIntegrations()
    channelIntegrations.value = data.integrations || []
    channelPublicBaseURL.value = data.public_base_url || 'https://leads.whxiangwu.com'
  } catch { channelsError.value = true } finally { loadingChannels.value = false }
}

const saveNotification = async () => {
  if (!settingsLoaded.value || settingsError.value) return
  const webhook = notification.webhook.trim()
  if (webhook && !validateWebhook(webhook)) {
    showFailToast('请输入有效的企业微信群机器人 Webhook')
    return
  }
  if (notificationEnabled() && !notification.configured && !webhook) {
    showFailToast('开启通知前请先配置 Webhook')
    return
  }
  saving.value = true
  try {
    const updates = [
      setting('notification.group_robot_enabled', notification.newLead, 'boolean', 'notification', '企业微信群新线索通知'),
      setting('notification.group_robot_mentions_enabled', notification.mentions, 'boolean', 'notification', '企业微信群真实 @'),
      setting('notification.group_robot_dispatch_claim_enabled', notification.dispatchClaim, 'boolean', 'notification', '企业微信群派发与领取提醒'),
      setting('notification.group_robot_followup_enabled', notification.followup, 'boolean', 'notification', '企业微信群回访提醒'),
      setting('notification.group_robot_pool_enabled', notification.pool, 'boolean', 'notification', '企业微信群自动回公海通知')
    ]
    if (webhook) updates.push(setting('notification.group_robot_webhook_url', webhook, 'string', 'notification', '企业微信群机器人 Webhook', true))
    await updateAdminSettings(updates)
    showSuccessToast('提醒配置已保存')
    await loadSettings()
  } finally { saving.value = false }
}

const testGroupRobot = async () => {
  testingRobot.value = true
  try {
    const result = await sendGroupRobotTest()
    showSuccessToast(`测试消息已发送，真实 @ ${result.mention_count || 0} 人`)
  } finally { testingRobot.value = false }
}

const saveRules = async () => {
  if (!settingsLoaded.value || settingsError.value) return
  saving.value = true
  try {
    await updateAdminSettings([
      setting('workflow.v2_enabled', rules.workflow, 'boolean', 'workflow', '双阶段工作流'),
      setting('appointment.enabled', rules.appointment, 'boolean', 'appointment', '预约排班'),
      setting('pool.enabled', rules.pool, 'boolean', 'pool', '公海'),
      setting('pool.auto_return_enabled', rules.pool && rules.poolAutoReturn, 'boolean', 'pool', '自动回公海'),
      setting('reminder.dispatch_minutes', normalizeCSV(rules.dispatchMinutes), 'csv_int', 'business', '待派发提醒分钟'),
      setting('reminder.claim_minutes', normalizeCSV(rules.claimMinutes), 'csv_int', 'business', '待领取提醒分钟'),
      setting('followup.first_due_minutes', rules.firstFollowup, 'number', 'followup', '领取后首次回访截止分钟'),
      setting('followup.reminder_minutes', normalizeCSV(rules.followupReminder), 'csv_int', 'followup', '回访逾期提醒偏移分钟'),
      setting('followup.star_1_hours', rules.star1Hours, 'number', 'followup', '1 星回访周期小时'),
      setting('followup.star_2_hours', rules.star2Hours, 'number', 'followup', '2 星回访周期小时'),
      setting('followup.star_3_hours', rules.star3Hours, 'number', 'followup', '3 星回访周期小时'),
      setting('followup.star_4_hours', rules.star4Hours, 'number', 'followup', '4 星回访周期小时'),
      setting('followup.star_5_hours', rules.star5Hours, 'number', 'followup', '5 星回访周期小时'),
      setting('pool.max_active_leads', rules.poolMaxActive, 'number', 'pool', '邀约师私有线索上限'),
      setting('pool.cooldown_minutes', rules.poolCooldown, 'number', 'pool', '原负责人公海领取冷却分钟'),
      setting('pool.unclaimed_minutes', rules.poolUnclaimed, 'number', 'pool', '未领取自动回公海宽限分钟'),
      setting('pool.followup_grace_minutes', rules.poolFollowupGrace, 'number', 'pool', '回访逾期自动回公海宽限分钟')
    ])
    showSuccessToast('业务规则已保存')
    await loadSettings()
  } finally { saving.value = false }
}

const onPoolEnabledChange = enabled => {
  if (!enabled) rules.poolAutoReturn = false
}

const resetUserForm = () => Object.assign(userForm, { expected_version: 0, username: '', password: '', employee_name: '', role: 'inviter', department_id: '', group_robot_mention_userid: '', active: true })
const openCreate = () => { editingID.value = null; userSaveError.value = ''; resetUserForm(); showEditor.value = true }
const openEdit = item => { userSaveError.value = ''; editingID.value = item.id; Object.assign(userForm, { expected_version: item.version, username: item.username || item.wecom_userid, password: '', employee_name: item.employee_name || '', role: item.role, department_id: String(item.department_id || ''), group_robot_mention_userid: item.group_robot_mention_userid || '', active: item.status === 'active' }); showEditor.value = true }
const saveUser = async () => {
  savingUser.value = true
  try {
    const payload = { expected_version: userForm.expected_version, username: userForm.username, password: userForm.password, employee_name: userForm.employee_name, role: userForm.role, department_id: Number(userForm.department_id || 0), group_robot_mention_userid: userForm.group_robot_mention_userid, status: userForm.active ? 'active' : 'disabled' }
    if (editingID.value) await updateUser(editingID.value, payload)
    else await createUser(payload)
    showSuccessToast('用户已保存')
    showEditor.value = false
    await loadUsers()
  } catch (error) {
    userSaveError.value = error.response?.status === 409
      ? '账号已被其他操作修改。当前输入已保留，请核对最新账号资料后重新编辑，避免覆盖新密码或权限。'
      : error.response?.data?.message || '保存失败，输入已保留，请稍后重试。'
  } finally { savingUser.value = false }
}
const roleText = role => ({ inviter: '邀约', sales: '销售', closer: '谈单', dispatcher: '线索调度员', manager: '主管', admin: '管理员' }[role] || role)
const userInitial = item => String(item.employee_name || item.username || '?').trim().slice(0, 1)
const mentionStatusText = item => item.status !== 'active' ? '已停用' : (String(item.group_robot_mention_userid || '').trim() ? '真实 @ 已配置' : '真实 @ 未配置')
const mentionStatusClass = item => item.status !== 'active' ? 'disabled' : (String(item.group_robot_mention_userid || '').trim() ? 'configured' : 'missing')

const openCreateChannel = () => {
  editingChannelID.value = null
  editingChannelSecretConfigured.value = false
  Object.assign(channelForm, { platform: 'baidu', account_name: '', token: '', enabled: true })
  showChannelEditor.value = true
}
const openEditChannel = item => {
  editingChannelID.value = item.id
  editingChannelSecretConfigured.value = item.secret_configured === true
  Object.assign(channelForm, { platform: item.platform, account_name: item.account_name, token: '', enabled: item.enabled })
  showChannelEditor.value = true
}
const validateChannelToken = value => channelForm.platform !== 'xiaohongshu' || Boolean(value?.trim()) || (Boolean(editingChannelID.value) && editingChannelSecretConfigured.value)
const saveChannel = async () => {
  savingChannel.value = true
  try {
    const payload = { platform: channelForm.platform, account_name: channelForm.account_name, token: channelForm.token, enabled: channelForm.enabled }
    if (editingChannelID.value) await updateChannelIntegration(editingChannelID.value, payload)
    else await createChannelIntegration(payload)
    showSuccessToast('平台账户已保存')
    showChannelEditor.value = false
    await loadChannelIntegrations()
  } finally { savingChannel.value = false }
}
const toggleChannel = async (item, enabled) => {
  if (!enabled) {
    try {
      await confirmSettingsAction({
        title: '停用平台账户',
        message: `停用“${platformText(item.platform)} · ${item.account_name}”后，该回调地址将立即停止接收线索。`,
        confirmButtonText: '确认停用'
      })
    } catch {
      return
    }
  }
  togglingChannelID.value = item.id
  try {
    await updateChannelIntegration(item.id, { platform: item.platform, account_name: item.account_name, enabled })
    await loadChannelIntegrations()
    showSuccessToast(enabled ? '平台账户已启用' : '平台账户已停用')
  } finally {
    togglingChannelID.value = null
  }
}
const removeChannel = async item => {
  try {
    await confirmSettingsAction({
      title: '删除平台账户',
      message: `确认删除“${platformText(item.platform)} · ${item.account_name}”？删除后该回调地址将立即停止接收线索。`,
      confirmButtonText: '确认删除',
      confirmButtonColor: 'var(--kx-color-danger)'
    })
  } catch {
    return
  }
  deletingChannelID.value = item.id
  try {
    await deleteChannelIntegration(item.id)
    await loadChannelIntegrations()
    showSuccessToast('平台账户已删除')
  } finally {
    deletingChannelID.value = null
  }
}
const callbackURL = item => `${channelPublicBaseURL.value}${item.callback_path}?account=${encodeURIComponent(item.account_name)}`
const copyCallback = async item => {
  try {
    await navigator.clipboard.writeText(callbackURL(item))
    showSuccessToast('回调地址已复制')
  } catch {
    showFailToast('复制失败，请长按回调地址手动复制')
  }
}
const platformText = platform => ({ baidu: '百度营销', tencent: '腾讯营销', xiaohongshu: '小红书', douyin: '抖音' }[platform] || platform)
const authModeText = mode => ({ hmac_sha1: 'HMAC-SHA1 签名', feiyu_signature: '飞鱼 SHA256 签名' }[mode] || '平台兼容模式')
const channelFormAuthMode = computed(() => {
  if (channelForm.platform === 'xiaohongshu') return 'hmac_sha1'
  if (channelForm.platform === 'douyin' && (Boolean(channelForm.token?.trim()) || (Boolean(editingChannelID.value) && editingChannelSecretConfigured.value))) {
    return 'feiyu_signature'
  }
  return 'platform_compatible'
})
const channelTokenPlaceholder = computed(() => {
  if (channelForm.platform === 'douyin') {
    return editingChannelSecretConfigured.value ? '留空则保留现有密钥' : '选填，对应飞鱼 http_post 密钥'
  }
  return editingChannelSecretConfigured.value ? '留空则保留现有 Token' : '请输入小红书回调 Token'
})
const channelStatusText = item => ({ accepted: '接收成功', duplicate: '重复线索', failed: '接收失败', rejected_disabled: '停用拒绝' }[item.last_status] || '等待首次接收')

const confirmLeave = async () => {
  if (!sectionDirty.value) return true
  try {
    await confirmSettingsAction({ title: '离开设置页？', message: '当前修改尚未保存，离开后将丢失。', confirmButtonText: '放弃修改' })
    return true
  } catch {
    return false
  }
}

onBeforeRouteLeave(() => confirmLeave())
const onBeforeUnload = event => {
  if (!sectionDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}
onMounted(() => {
  window.addEventListener('beforeunload', onBeforeUnload)
  void Promise.all([loadSettings(), loadUsers(), loadChannelIntegrations()])
  if (activeTab.value === 'recycle') void loadDeletedLeads()
})
onBeforeUnmount(() => { window.removeEventListener('beforeunload', onBeforeUnload); clearTimeout(userSearchTimer) })
</script>

<style scoped>
.admin-page { min-height: 100vh; min-height: 100dvh; background: var(--kx-color-canvas); color: var(--kx-color-text); }
.recycle-list { display: grid; gap: var(--kx-space-3); margin-top: var(--kx-space-6); }
.recycle-item { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-4); padding: var(--kx-space-4); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
.recycle-item > div { display: grid; gap: 4px; min-width: 0; }
.recycle-item code { overflow-wrap: anywhere; font-size: var(--kx-font-12); }
.recycle-item small { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); line-height: 1.6; overflow-wrap: anywhere; }
.recycle-item > .van-button { flex-shrink: 0; }
.admin-shell { width: min(var(--kx-page-max-width), 100%); margin: 0 auto; padding: 0 var(--kx-space-6) var(--kx-space-8); }
header { padding: var(--kx-space-6) 0 var(--kx-space-4); }
header h1 { margin: 0; font-size: var(--kx-font-26); font-weight: 650; letter-spacing: -.025em; } header p,.hint { color: var(--kx-color-text-secondary); }
header p { margin: var(--kx-space-1) 0 0; font-size: var(--kx-font-13); }
.settings-layout { display: grid; grid-template-columns: 216px minmax(0, 1fr); align-items: start; gap: var(--kx-space-4); }
.settings-section-nav { position: sticky; top: var(--kx-space-6); display: grid; gap: var(--kx-space-1); padding: var(--kx-space-2); border-radius: var(--kx-card-radius); background: var(--kx-color-nav); }
.settings-section-nav button { display: flex; min-height: 58px; align-items: center; gap: var(--kx-space-3); padding: var(--kx-space-2) var(--kx-space-3); border: 0; border-radius: var(--kx-control-radius); background: transparent; color: var(--kx-color-text-secondary); cursor: pointer; text-align: left; }
.settings-section-nav button:hover { background: var(--kx-color-surface-subtle); }
.settings-section-nav button.active { background: var(--kx-color-nav-selected); color: var(--kx-color-text); box-shadow: var(--kx-shadow-selected); }
.settings-section-nav button.active > .van-icon { color: var(--kx-color-brand); }
.settings-section-nav .van-icon { width: 20px; flex: 0 0 20px; font-size: 18px; text-align: center; }
.settings-section-nav button > span { display: grid; gap: 2px; }
.settings-section-nav strong { font-size: var(--kx-font-14); font-weight: 550; }
.settings-section-nav small { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); font-weight: 400; line-height: 1.5; }
.settings-content { min-width: 0; container-type: inline-size; container-name: settings-content; }
.settings-mobile-select { display: none; }
.panel { min-width: 0; margin: 0; padding: var(--kx-space-6); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }
.panel h2 { margin: 0 0 var(--kx-space-2); font-size: var(--kx-font-16); line-height: 1.5; }
.hint { margin: 0; font-size: var(--kx-font-13); line-height: 1.7; }
.panel-title { display: flex; justify-content: space-between; gap: var(--kx-space-4); align-items: start; flex-wrap: wrap; }
.van-cell-group { margin: var(--kx-space-6) 0; border: 1px solid var(--kx-color-border); }.van-button[block] { margin-top: var(--kx-space-6); }
.notification-switches { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); overflow: hidden; }
.notification-switches :deep(.van-cell) { border-bottom: 1px solid var(--kx-color-border); }
.notification-switches :deep(.van-cell:nth-child(odd):not(.van-field)) { border-right: 1px solid var(--kx-color-border); }
.notification-switches :deep(.van-cell:nth-child(5):not(.van-field)) { grid-column: 1 / -1; border-right: 0; }
.notification-switches :deep(.van-field) { grid-column: 1 / -1; }
.form-tip { margin: 0; color: var(--kx-color-text-secondary); font-size: 12px; line-height: 1.65; }
.notification-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: var(--kx-space-6); }
.notification-actions :deep(.van-button) { width: auto; min-width: 172px; margin-top: 0; }
.rules-form { display: grid; gap: 16px; margin-top: var(--kx-space-6); }
.rule-section { overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface-subtle); }
.rule-section-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-toolbar); }
.rule-section-heading strong { color: var(--kx-color-text); font-size: 14px; }
.rule-section-heading span { color: var(--kx-color-text-secondary); font-size: 12px; text-align: right; }
.rule-switches, .rule-fields { display: grid; margin: 0; border: 0; border-radius: 0; }
.rule-switches, .rule-fields.two-columns { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.rule-fields.three-columns { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.rule-switches :deep(.van-cell), .rule-fields :deep(.van-field) { min-height: 62px; border-bottom: 1px solid var(--kx-color-border); }
.rule-switches :deep(.van-cell:nth-child(odd)), .rule-fields.two-columns :deep(.van-field:nth-child(odd)), .rule-fields.three-columns :deep(.van-field:not(:nth-child(3n))) { border-right: 1px solid var(--kx-color-border); }
.field-unit { margin-left: 7px; color: var(--kx-color-text-muted); font-size: 12px; }
.rules-submit { position: sticky; z-index: 4; bottom: calc(var(--kx-shell-mobile-nav-height) + env(safe-area-inset-bottom)); display: flex; justify-content: flex-end; padding: var(--kx-space-3) 0; border-top: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.rules-submit :deep(.van-button) { width: min(300px, 100%); margin-top: 0; }
.user-panel { padding: 0; overflow: hidden; }
.user-panel-title { align-items: center; flex-wrap: wrap; padding: var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.user-panel-title h2 { margin-bottom: 6px; }
.user-panel-title .hint { margin: 0; }
.user-panel-actions { display: flex; align-items: center; gap: var(--kx-space-3); }
.user-count { color: var(--kx-color-text-secondary); font-size: 12px; white-space: nowrap; }
.user-count strong { color: var(--kx-color-text); font-size: 15px; }
.user-table { margin: 0; background: var(--kx-color-surface); }
.user-pagination { display: flex; min-height: 58px; align-items: center; justify-content: flex-end; gap: 12px; padding: var(--kx-space-3) var(--kx-space-6); flex-wrap: wrap; border-top: 1px solid var(--kx-color-border); }
.user-pagination span { color: var(--kx-color-text-secondary); font-size: 12px; }
.user-pagination button { min-height: var(--kx-control-current-height); padding: 0 12px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text); }
.user-table-head,
.user-row { display: grid; grid-template-columns: minmax(150px, 1.5fr) minmax(80px, .8fr) minmax(80px, .7fr) minmax(96px, .8fr) 48px; align-items: center; gap: var(--kx-space-3); }
.user-table-head { min-height: 42px; padding: 0 var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); font-size: 12px; font-weight: 600; }
.user-row { width: 100%; min-height: 76px; padding: var(--kx-space-4) var(--kx-space-6); border: 0; border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); color: var(--kx-color-text); text-align: left; cursor: pointer; transition: background var(--kx-motion-fast) var(--kx-ease-standard); }
.user-row:last-child { border-bottom: 0; }
.user-row:hover { background: var(--kx-table-row-hover-bg); }
.user-row:focus-visible { outline: 2px solid var(--kx-color-focus); outline-offset: -2px; }
.user-identity { display: flex; min-width: 0; align-items: center; gap: 12px; }
.user-avatar { display: inline-flex; flex: 0 0 auto; width: 40px; height: 40px; align-items: center; justify-content: center; border-radius: 50%; background: var(--kx-color-toolbar); color: var(--kx-color-text-secondary); font-size: 15px; font-weight: 600; }
.user-copy { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.user-copy strong,
.user-copy small { max-width: 100%; overflow-wrap: anywhere; }
.user-copy strong { color: var(--kx-color-text); font-size: 14px; line-height: 20px; }
.user-copy small { color: var(--kx-color-text-secondary); font-size: 12px; line-height: 17px; }
.user-role,
.user-status { display: inline-flex; width: fit-content; max-width: 100%; align-items: center; gap: 7px; white-space: nowrap; }
.user-role { color: var(--kx-color-text-secondary); font-size: 13px; font-weight: 600; }
.user-role i { width: 7px; height: 7px; border-radius: 50%; background: var(--kx-color-text-secondary); }
.user-role i.role-admin { background: var(--kx-color-text-secondary); }
.user-role i.role-manager { background: var(--kx-color-warning); }
.user-role i.role-dispatcher { background: var(--kx-color-brand); }
.user-role i.role-inviter,
.user-role i.role-sales { background: var(--kx-color-text-secondary); }
.user-role i.role-closer { background: var(--kx-color-text-secondary); }
.user-status { padding: 5px 9px; border-radius: var(--kx-radius-1); font-size: 12px; font-weight: 600; }
.user-status i { width: 6px; height: 6px; border-radius: 50%; }
.user-status.active { background: var(--kx-color-success-soft); color: var(--kx-color-success); }
.user-status.active i { background: var(--kx-color-success); }
.user-status.disabled { background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); }
.user-status.disabled i { background: var(--kx-color-text-muted); }
.mention-status { width: fit-content; padding: 5px 9px; border-radius: var(--kx-radius-1); font-size: 12px; font-weight: 600; white-space: nowrap; }
.mention-status.configured { background: var(--kx-color-brand-soft); color: var(--kx-color-brand); }
.mention-status.missing { background: var(--kx-color-warning-soft); color: var(--kx-color-warning); }
.mention-status.disabled { background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); }
.user-edit { display: inline-flex; align-items: center; justify-content: flex-end; gap: 5px; color: var(--kx-color-brand); font-size: 12px; font-weight: 600; white-space: nowrap; }
.user-filters { display: grid; grid-template-columns: minmax(0, 1fr) 130px 130px; gap: var(--kx-space-2); padding: var(--kx-space-3) var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.user-filters :deep(.van-search) { padding: 0; background: transparent; }
.user-filters select { height: var(--kx-control-current-height); padding: 0 var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); }
.channel-grid { display: grid; gap: 0; margin-top: var(--kx-space-6); overflow: hidden; border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); }
.channel-card { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr); align-items: center; gap: var(--kx-space-3); padding: var(--kx-space-3) var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.channel-card:last-child { border-bottom: 0; }
.channel-heading,.channel-actions { display: flex; min-width: 0; flex-wrap: wrap; justify-content: space-between; gap: 12px; align-items: center; }
.channel-action-buttons { display: flex; flex-wrap: wrap; gap: 8px; }
.channel-heading div { display: grid; gap: 3px; }.channel-heading span,.channel-meta { color: var(--kx-color-text-secondary); font-size: 13px; }
.callback-row { display: flex; align-items: stretch; gap: 8px; min-width: 0; }
.channel-card code { display: -webkit-box; flex: 1; min-width: 0; overflow: hidden; overflow-wrap: anywhere; padding: 10px; border-radius: var(--kx-control-radius); background: var(--kx-color-surface-subtle); color: var(--kx-color-text-secondary); font-size: 12px; line-height: 18px; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.copy-button { flex: 0 0 auto; min-width: 52px; padding: 0 10px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-control-radius); background: var(--kx-color-surface); color: var(--kx-color-brand); cursor: pointer; }
.channel-meta { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; }
.channel-toggle { display: flex; align-items: center; gap: 8px; color: var(--kx-color-text-secondary); font-size: 12px; }
.channel-warning { grid-column: 1 / -1; margin: -4px 0 0; padding: 8px 10px; border-radius: var(--kx-control-radius); background: var(--kx-color-warning-soft); color: var(--kx-color-warning); font-size: 12px; line-height: 18px; }
.settings-editor-popup { width: min(var(--kx-dialog-max-width), calc(100vw - 48px)); max-height: calc(100dvh - 64px); overflow-y: auto; border-radius: var(--kx-dialog-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); }
.editor { padding-bottom: var(--kx-space-6); margin: 0 auto; }
.editor-heading { position: sticky; top: 0; z-index: 1; display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-4); padding: var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); background: var(--kx-color-surface); }
.editor-heading h2 { margin: 0; font-size: var(--kx-font-20); font-weight: 600; }
.editor-heading button { flex-shrink: 0; min-height: var(--kx-control-current-height); padding: 0 var(--kx-space-3); border: 0; border-radius: var(--kx-control-radius); background: var(--kx-color-toolbar); color: var(--kx-color-text-secondary); cursor: pointer; }
.settings-editor-form { padding: var(--kx-space-4) var(--kx-space-6) 0; }
.settings-editor-form :deep(.van-cell-group) { margin: 0; border: 0; border-radius: 0; }
.settings-editor-form :deep(.van-field) { padding-inline: 0; }
.editor-error { padding: var(--kx-space-3); margin: var(--kx-space-3) 0 0; border-radius: var(--kx-control-radius); color: var(--kx-color-danger); background: var(--kx-color-danger-soft); font-size: var(--kx-font-13); line-height: 1.6; }
.editor :deep(.van-radio-group) { display: flex; flex-wrap: wrap; gap: 12px 18px; }
@container settings-content (max-width: 760px) {
  .user-table-head { display: none; }
  .user-row { grid-template-columns: minmax(0, 1fr) auto; gap: var(--kx-space-2) var(--kx-space-3); }
  .user-identity { grid-column: 1; grid-row: 1 / 3; }
  .user-role, .user-status, .mention-status { grid-column: 2; justify-self: end; }
  .user-role { grid-row: 1; }
  .user-status { grid-row: 2; }
  .mention-status { grid-row: 3; }
  .user-edit { grid-column: 1; grid-row: 3; justify-content: flex-start; }
  .user-filters { grid-template-columns: 1fr 1fr; }
  .user-filters :deep(.van-search) { grid-column: 1 / -1; }
  .rule-fields.three-columns { grid-template-columns: 1fr; }
  .rule-fields.three-columns :deep(.van-field) { border-right: 0; }
  .notification-switches, .rule-switches, .rule-fields.two-columns { grid-template-columns: 1fr; }
  .notification-switches :deep(.van-cell), .rule-switches :deep(.van-cell), .rule-fields :deep(.van-field) { border-right: 0; }
  .channel-card { grid-template-columns: minmax(0, 1fr); }
  .channel-heading, .channel-actions, .channel-meta { flex-wrap: wrap; }
}
@media (max-width: 1023px) and (min-width: 768px) {
  .settings-layout { grid-template-columns: 180px minmax(0, 1fr); }
}
@media (max-width: 767px) {
  .admin-shell { padding: 0 var(--kx-space-4) var(--kx-space-6); }
  .settings-layout { display: block; }
  .settings-section-nav { display: none; }
  .settings-mobile-select { display: flex; align-items: center; gap: var(--kx-space-3); min-height: 56px; padding: 0 var(--kx-space-4); border: 0; border-radius: var(--kx-card-radius); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); }
  .settings-mobile-select > span { flex-shrink: 0; }
  .settings-mobile-select select { flex: 1; min-width: 0; height: 56px; padding: 0; border: 0; border-radius: var(--kx-mobile-control-radius); background: transparent; color: var(--kx-color-text); font-size: var(--kx-font-16); }
  header { display: block; padding: var(--kx-space-2) 0 var(--kx-space-4); }
  header h1 { font-size: var(--kx-mobile-title-size); }
  header p { margin: var(--kx-space-2) 0 0; font-size: var(--kx-font-13); line-height: 1.55; }
  .panel { margin: var(--kx-space-4) 0; padding: var(--kx-space-4); border: 0; border-radius: var(--kx-card-radius); box-shadow: none; }
  .panel h2 { font-size: var(--kx-mobile-heading-size); }
  .panel-title { align-items: center; gap: var(--kx-space-3); }
  .notification-panel .panel-title { align-items: flex-start; flex-direction: column; }
  .notification-switches, .rule-switches, .rule-fields.two-columns, .rule-fields.three-columns { grid-template-columns: 1fr; }
  .notification-switches { margin: var(--kx-space-4) calc(-1 * var(--kx-space-4)); border: 0; border-radius: 0; }
  .notification-switches :deep(.van-cell), .rule-switches :deep(.van-cell), .rule-fields :deep(.van-field) { min-height: 56px; border: 0; border-bottom: 1px solid var(--kx-color-mobile-separator); padding: var(--kx-space-3) var(--kx-space-4); }
  .notification-switches :deep(.van-cell:not(.van-field)) { column-gap: var(--kx-space-3); }
  .notification-switches :deep(.van-cell::after), .rule-switches :deep(.van-cell::after), .rule-fields :deep(.van-field::after) { display: none; }
  .notification-switches :deep(.van-cell:last-child), .rule-switches :deep(.van-cell:last-child), .rule-fields :deep(.van-field:last-child) { border-bottom: 0; }
  .notification-switches :deep(.van-cell:nth-child(odd):not(.van-field)), .rule-switches :deep(.van-cell:nth-child(odd)), .rule-fields.two-columns :deep(.van-field:nth-child(odd)), .rule-fields.three-columns :deep(.van-field:not(:nth-child(3n))) { border-right: 0; }
  .notification-actions { display: grid; grid-template-columns: 1fr; gap: var(--kx-space-2); margin-top: var(--kx-space-4); }
  .notification-actions :deep(.van-button) { width: 100%; }
  .rule-section { border: 0; border-radius: 0; background: transparent; }
  .rule-section + .rule-section { padding-top: var(--kx-space-4); border-top: 1px solid var(--kx-color-mobile-separator); }
  .rule-section-heading { align-items: flex-start; flex-direction: column; gap: var(--kx-space-1); padding: 0 0 var(--kx-space-2); border: 0; background: transparent; }
  .rule-section-heading span { text-align: left; }
  .rule-switches, .rule-fields { margin-inline: calc(-1 * var(--kx-space-4)); }
  .rules-form { gap: var(--kx-space-4); margin-top: var(--kx-space-4); }
  .rules-submit { padding-block: var(--kx-space-3); border-color: var(--kx-color-mobile-separator); }
  .rules-submit :deep(.van-button) { width: 100%; }
  .user-panel { padding: 0; }
  .user-panel-title { align-items: flex-start; padding: var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .user-filters { grid-template-columns: 1fr 1fr; padding: var(--kx-space-3) var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .user-filters :deep(.van-search) { grid-column: 1 / -1; }
  .user-filters select { height: var(--kx-control-current-height); min-width: 0; border: 0; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); font-size: var(--kx-font-16); }
  .user-panel-actions { align-items: flex-end; flex-direction: column-reverse; gap: var(--kx-space-2); }
  .user-table-head { display: none; }
  .user-row { grid-template-columns: minmax(0, 1fr) auto; gap: var(--kx-space-2) var(--kx-space-3); min-height: 88px; padding: var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .user-row > :nth-child(2), .user-row > :nth-child(3), .user-row > :nth-child(4) { grid-column: 2; justify-self: end; }
  .user-row > :nth-child(2) { grid-row: 1; }
  .user-row > :nth-child(3) { grid-row: 2; }
  .user-row > :nth-child(4) { grid-row: 3; }
  .user-identity { grid-row: 1 / 3; }
  .user-role { justify-self: end; font-size: var(--kx-font-12); }
  .user-status { justify-self: end; padding: var(--kx-space-1) var(--kx-space-2); }
  .user-edit { display: none; }
  .user-copy strong { font-size: var(--kx-font-16); }
  .channel-grid { display: grid; grid-template-columns: 1fr; margin: var(--kx-space-4) 0 0; border: 0; border-radius: 0; }
  .channel-card { display: grid; grid-template-columns: 1fr; gap: var(--kx-space-3); padding: var(--kx-space-4) 0; border-color: var(--kx-color-mobile-separator); }
  .channel-card:first-child { padding-top: 0; }
  .channel-meta { align-items: flex-start; flex-direction: column; gap: var(--kx-space-1); }
  .channel-actions { align-items: center; }
  .channel-action-buttons { flex-wrap: wrap; justify-content: flex-end; }
  .channel-card code { background: var(--kx-color-mobile-fill); }
  .copy-button { border: 0; background: var(--kx-color-mobile-fill); }
  .recycle-list { gap: 0; margin-top: var(--kx-space-4); }
  .recycle-item { align-items: stretch; flex-direction: column; padding: var(--kx-space-4) 0; border: 0; border-top: 1px solid var(--kx-color-mobile-separator); border-radius: 0; }
  .recycle-item > .van-button { align-self: flex-start; }
  .settings-editor-popup { width: 100%; max-height: 88dvh; scroll-padding-bottom: calc(var(--kx-space-6) + env(safe-area-inset-bottom)); border-radius: var(--kx-mobile-sheet-radius) var(--kx-mobile-sheet-radius) 0 0; }
  .editor { padding-bottom: calc(var(--kx-space-6) + env(safe-area-inset-bottom)); }
  .editor-heading { min-height: 56px; padding: var(--kx-space-2) var(--kx-space-4); border-color: var(--kx-color-mobile-separator); }
  .editor-heading h2 { min-width: 0; font-size: var(--kx-mobile-heading-size); overflow-wrap: anywhere; }
  .settings-editor-form { padding-inline: var(--kx-space-4); }
}
</style>
